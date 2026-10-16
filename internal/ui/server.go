// Package ui sert l'interface graphique d'EduSync-AD : pages HTML rendues
// côté serveur (html/template) + HTMX pour les mises à jour partielles.
//
// Principe : le serveur fait le travail, le navigateur ne contient que le
// strict minimum (thème, langue). Aucun framework JavaScript, aucune API
// REST parallèle à maintenir.
//
// Toutes les routes sont accessibles en navigation normale (formulaire
// classique) : l'application reste utilisable sans JavaScript, ce qui
// facilite les tests automatisés (Go) et les tests pilotés par l'IA.
package ui

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
	"github.com/estemobs/EduSync-AD/internal/progress"
	"github.com/estemobs/EduSync-AD/internal/updater"
	"github.com/estemobs/EduSync-AD/web"
)

const (
	// cookieSession transporte l'identifiant de session (HttpOnly).
	cookieSession = "edusync_sid"
	// cookieLangue / cookieTheme gardent les préférences d'affichage
	// même hors session (page de connexion).
	cookieLangue = "edusync_langue"
	cookieTheme  = "edusync_theme"

	// dureeSession est la durée de validité d'une session administrateur.
	dureeSession = 12 * time.Hour
)

// Options permet d'injecter des chemins alternatifs (tests) et la version.
type Options struct {
	// ConfigPath vide = chemin par défaut (~/.config/EduSyncAD/config.json).
	ConfigPath string
	// AuditPath vide = chemin par défaut (~/.local/share/EduSyncAD/journal.db).
	AuditPath string
	// Version est affichée dans l'en-tête et le tableau de bord.
	Version string
	// UpdaterAPIURL est l'URL de l'API GitHub releases (vide = défaut GitHub).
	UpdaterAPIURL string
}

// noeudArbre enveloppe un nœud de l'arbre AD avec la langue de la page. Les
// gabarits imbriqués ({{template}}) repartent avec un contexte propre ($ est
// rebindé sur l'argument du gabarit appelé) : la langue doit donc voyager
// avec le nœud pour que {{tr}} fonctionne dans explorer_tree_node.
// Les champs de ExplorerNode sont promus : la syntaxe du gabarit reste
// identique ({{.DN}}, {{.Children}}…).
type noeudArbre struct {
	*ad.ExplorerNode
	Langue string
}

// noeudArbreConstruct fabrique l'enveloppe pour le gabarit explorer_tree_node.
func noeudArbreConstruct(langue string, n *ad.ExplorerNode) noeudArbre {
	return noeudArbre{ExplorerNode: n, Langue: langue}
}

// PageData est le modèle transmis aux templates.
type PageData struct {
	Titre        string
	Langue       string
	Theme        string
	Version      string
	Connecte     bool
	LDAPFallback bool // true si repli LDAP 389 effectué

	// Session courante
	Utilisateur string
	Domaine     string
	Controleur  string

	// Erreurs de formulaire (affichées sans rechargement via HTMX)
	Erreurs []string

	// Tableau de bord
	CheminConfig  string
	CheminDonnees string
	NbActions     int
	Actions       []audit.Entry
	StatutAD      string

	// Arrivées — simulation
	Simulation *SimulationResult
	ForcerMdp  string
	SkipAd     string

	// Arrivées — exécution
	Resultat *ExecuteResult

	// Password — simulation
	SimulationPwd *PasswordSimResult
	ForceChange   bool

	// Password — exécution
	ResultatPwd *PasswordExecResult

	// Migration — plan
	PlanMigration *MigrationPlanResult

	// Migration — simulation
	SimulationMigration *MigrationSimResult

	// Migration — exécution
	MigrationExecute *MigrationExecResult

	// Explorer
	SearchResult  *ad.SearchResult
	ExplorerTree  *ad.ExplorerNode
	ExplorerNodes []*ad.ExplorerNode
	ExplorerUser  *ad.User
	ExplorerGroup *ad.Group
	ExplorerOU    *ad.OU

	// Export (M6)
	ExportOU  string   // OU pré-remplie du formulaire
	ExportOUs []*ad.OU // OU de premier niveau pour le datalist

	// Progression (A12)
	Operations []*progress.Operation // opérations de masse en cours
}

// importRow représente une ligne du CSV d'import (niveau paquet pour réutilisation).
type importRow struct {
	Prenom, Nom, OU, Classe, Email string
}

// SimulationResult contient le résultat complet de la simulation
type SimulationResult struct {
	Lignes  []SimulatedUserLine
	Valides int
	Erreurs int
	Total   int
}

// SimulatedUserLine représente une ligne de simulation pour le template
type SimulatedUserLine struct {
	Prenom        string
	Nom           string
	Identifiant   string
	Mail          string
	MotDePasse    string
	OUCible       string
	Groupe        string
	DoublonResolu bool
	DoublonAD     bool
	Erreur        string
}

// ExecuteResult contient le résultat de l'exécution
type ExecuteResult struct {
	Crees  int
	Echecs int
	Lignes []MigrationExecLine
}

// ExecuteLine représente une ligne du résultat d'exécution
type ExecuteLine struct {
	Prenom, Nom, Identifiant string
	NouveauMDP               string
	Statut                   string // "succes" ou "echec"
	Detail                   string
}

// PasswordSimLine représente une ligne de simulation MDP
type PasswordSimLine struct {
	Prenom      string
	Nom         string
	Identifiant string
	Mail        string
	NouveauMDP  string
	OUCible     string
	Erreur      string
}

// PasswordSimResult contient le résultat de la simulation MDP
type PasswordSimResult struct {
	Lignes  []PasswordSimLine
	Valides int
	Erreurs int
	Total   int
}

// PasswordExecResult contient le résultat de l'exécution MDP
type PasswordExecResult struct {
	Crees  int
	Echecs int
	Lignes []MigrationExecLine
}

// MigrationPlanLine représente une ligne du plan de migration
type MigrationPlanLine struct {
	Identifiant string
	NomComplet  string
	OUSource    string
	OUCible     string
	Action      string
	Erreur      string
}

// MigrationPlanResult contient le plan de migration
type MigrationPlanResult struct {
	Lignes   []MigrationPlanLine
	Total    int
	WillMove int
	WillSkip int
}

// MigrationSimLine représente une ligne de simulation migration
type MigrationSimLine struct {
	Identifiant string
	NomComplet  string
	OUSource    string
	OUCible     string
	Action      string
	Erreur      string
}

// MigrationSimResult contient le résultat de la simulation migration
type MigrationSimResult struct {
	Lignes  []MigrationSimLine
	Total   int
	Valides int
	Erreurs int
}

// MigrationExecResult contient le résultat d'exécution migration

// MigrationExecLine représente une ligne de résultat exécution migration
type MigrationExecLine struct {
	NouveauMDP  string
	Prenom      string
	Nom         string
	Identifiant string
	NomComplet  string
	OUSource    string
	OUCible     string
	Statut      string
	Detail      string
}

// MigrationExecResult contient le résultat d'exécution migration
type MigrationExecResult struct {
	Crees  int
	Echecs int
	Lignes []MigrationExecLine
}

type session struct {
	Utilisateur               string
	Domaine                   string
	Controleur                string
	MotDePasse                string // en mémoire seulement, jamais persisté
	LDAPFallback              bool   // true si repli LDAP 389 effectué
	DerniereSimulation        []models.GeneratedUser
	DerniereSimPassword       []ad.PasswordResetResult
	DerniereSimPasswordParams ad.PasswordResetParams
	DernierPlanMigration      *ad.MigrationPlan
	CreeLe                    time.Time
}

// Server implémente http.Handler.
type Server struct {
	pages   map[string]*template.Template
	racines map[string]string // fichier → nom du premier {{define}} du fichier
	mux     *http.ServeMux
	journal *audit.Log
	opts    Options

	mu       sync.Mutex
	sessions map[string]*session
	adPools  map[string]*ad.Pool // clé = sessionID
	progress *progress.Tracker   // suivi des opérations de masse
}

// New prépare le serveur d'interface (templates, assets, journal).
func New(opts Options) (*Server, error) {
	journal, err := audit.Open(opts.AuditPath)
	if err != nil {
		return nil, fmt.Errorf("ui: ouverture du journal: %w", err)
	}

	base, err := template.New("layout.html").
		Funcs(template.FuncMap{"tr": Tr, "horodatage": horodatage, "noeud": noeudArbreConstruct}).
		ParseFS(web.FS, "templates/layout.html")
	if err != nil {
		_ = journal.Close()
		return nil, fmt.Errorf("ui: lecture du gabarit layout: %w", err)
	}

	// Découverte de tous les gabarits : les fichiers définissant « content »
	// sont des pages complètes (rendues dans le layout), les autres sont des
	// fragments partagés (explorer_tree, explorer_results…) référencés par
	// les pages via {{template}} et rendus seuls dans les cibles HTMX.
	chemins, err := fs.Glob(web.FS, "templates/*.html")
	if err != nil {
		_ = journal.Close()
		return nil, fmt.Errorf("ui: énumération des gabarits: %w", err)
	}
	var fichiersPages, fichiersFragments []string
	racines := make(map[string]string, len(chemins))
	for _, chemin := range chemins {
		nom := filepath.Base(chemin)
		if nom == "layout.html" {
			continue
		}
		brut, err := fs.ReadFile(web.FS, chemin)
		if err != nil {
			_ = journal.Close()
			return nil, fmt.Errorf("ui: lecture de %s: %w", nom, err)
		}
		racines[nom] = premierDefine(brut)
		if bytes.Contains(brut, []byte(`{{define "content"}}`)) {
			fichiersPages = append(fichiersPages, chemin)
		} else {
			fichiersFragments = append(fichiersFragments, chemin)
		}
	}
	if len(fichiersPages) == 0 {
		_ = journal.Close()
		return nil, fmt.Errorf("ui: aucun gabarit de page trouvé")
	}

	// Chaque jeu = layout + fragments partagés + le fichier courant. Les
	// fragments ne définissent jamais « content » : aucune collision possible
	// avec la page du jeu.
	pages := make(map[string]*template.Template, len(fichiersPages)+len(fichiersFragments))
	jeu := func(nom, fichier string) error {
		page, err := base.Clone()
		if err != nil {
			return fmt.Errorf("duplication du gabarit: %w", err)
		}
		if len(fichiersFragments) > 0 {
			if _, err := page.ParseFS(web.FS, fichiersFragments...); err != nil {
				return fmt.Errorf("lecture des fragments pour %s: %w", nom, err)
			}
		}
		if fichier != "" {
			if _, err := page.ParseFS(web.FS, fichier); err != nil {
				return fmt.Errorf("lecture du gabarit %s: %w", nom, err)
			}
		}
		pages[nom] = page
		return nil
	}
	// …puis un jeu par page (contenant déjà les fragments, + sa page).
	for _, chemin := range fichiersPages {
		nom := filepath.Base(chemin)
		if err := jeu(nom, chemin); err != nil {
			_ = journal.Close()
			return nil, fmt.Errorf("ui: %w", err)
		}
	}
	for _, chemin := range fichiersFragments {
		nom := filepath.Base(chemin)
		jeuFrag, err := base.Clone()
		if err != nil {
			_ = journal.Close()
			return nil, fmt.Errorf("ui: duplication du gabarit: %w", err)
		}
		if _, err := jeuFrag.ParseFS(web.FS, fichiersFragments...); err != nil {
			_ = journal.Close()
			return nil, fmt.Errorf("ui: lecture des fragments pour %s: %w", nom, err)
		}
		pages[nom] = jeuFrag
	}

	s := &Server{
		pages:    pages,
		racines:  racines,
		journal:  journal,
		opts:     opts,
		sessions: map[string]*session{},
		adPools:  map[string]*ad.Pool{},
		progress: progress.NewTracker(),
	}
	s.mux = s.routes()
	return s, nil
}

// Close libère la base du journal.
func (s *Server) Close() error {
	if s.journal == nil {
		return nil
	}
	return s.journal.Close()
}

// routes déclare les points d'entrée. Chaque route existe en GET et/ou POST
// pour rester utilisable avec des formulaires HTML purs.
func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.accueil)
	mux.HandleFunc("GET /login", s.pageLogin)
	mux.HandleFunc("POST /login", s.connexion)
	mux.HandleFunc("GET /app", s.tableauBord)
	mux.HandleFunc("POST /logout", s.deconnexion)
	mux.HandleFunc("POST /theme", s.changerTheme)
	mux.HandleFunc("POST /langue", s.changerLangue)

	// AD Modules (protégés par session)
	mux.HandleFunc("GET /app/users", s.requireAuth(s.pageUsers))
	mux.HandleFunc("POST /app/users/import", s.requireAuth(s.usersImport))
	mux.HandleFunc("POST /app/users/simulate", s.requireAuth(s.usersSimulate))
	mux.HandleFunc("POST /app/users/execute", s.requireAuth(s.usersExecute))

	mux.HandleFunc("GET /app/password", s.requireAuth(s.pagePassword))
	mux.HandleFunc("POST /app/password/simulate", s.requireAuth(s.passwordSimulate))
	mux.HandleFunc("POST /app/password/execute", s.requireAuth(s.passwordExecute))

	mux.HandleFunc("GET /app/migration", s.requireAuth(s.pageMigration))
	mux.HandleFunc("POST /app/migration/plan", s.requireAuth(s.migrationPlan))
	mux.HandleFunc("POST /app/migration/simulate", s.requireAuth(s.migrationSimulate))
	mux.HandleFunc("POST /app/migration/execute", s.requireAuth(s.migrationExecute))

	// Départs (M3)
	mux.HandleFunc("GET /app/departs", s.requireAuth(s.pageDeparts))
	mux.HandleFunc("POST /app/departs/simulate", s.requireAuth(s.departsSimulate))
	mux.HandleFunc("POST /app/departs/execute", s.requireAuth(s.departsExecute))

	// Paramètres globaux (M8)
	mux.HandleFunc("GET /app/parametres", s.requireAuth(s.pageParametres))
	mux.HandleFunc("POST /app/parametres/comptes", s.requireAuth(s.parametresComptes))
	mux.HandleFunc("POST /app/parametres/mots_de_passe", s.requireAuth(s.parametresMotsDePasse))
	mux.HandleFunc("POST /app/parametres/apparence", s.requireAuth(s.parametresApparence))

	mux.HandleFunc("GET /app/explorer", s.requireAuth(s.pageExplorer))
	mux.HandleFunc("POST /app/explorer/search", s.requireAuth(s.explorerSearch))
	mux.HandleFunc("POST /app/explorer/tree", s.requireAuth(s.explorerTree))

	mux.HandleFunc("GET /app/export", s.requireAuth(s.pageExport))
	mux.HandleFunc("POST /app/export/csv", s.requireAuth(s.exportCSV))
	mux.HandleFunc("POST /app/export/pdf", s.requireAuth(s.exportPDF))
	mux.HandleFunc("POST /app/export/trombinoscope", s.requireAuth(s.exportTrombinoscope))

	// Audit / Journal
	mux.HandleFunc("GET /app/audit", s.requireAuth(s.pageAudit))
	mux.HandleFunc("POST /app/audit/filter", s.requireAuth(s.auditFilter))

	// Mise à jour intégrée (M10)
	mux.HandleFunc("GET /app/updater/check", s.requireAuth(s.pageUpdaterCheck))
	mux.HandleFunc("POST /app/updater/install", s.requireAuth(s.pageUpdaterInstall))

	// Progression / annulation des opérations de masse (A12)
	mux.HandleFunc("GET /app/progress", s.requireAuthSession(s.pageProgress))
	mux.HandleFunc("GET /app/progress/list", s.requireAuthSession(s.progressList))
	mux.HandleFunc("POST /app/progress/cancel", s.requireAuthSession(s.progressCancel))

	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServer(http.FS(mustSub("static")))))
	return mux
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// accueil renvoie vers le tableau de bord si la session est ouverte,
// vers la connexion sinon.
func (s *Server) accueil(w http.ResponseWriter, r *http.Request) {
	if s.session(r) != nil {
		rediriger(w, r, "/app")
		return
	}
	s.rendre(w, http.StatusOK, "login.html", s.donnees(r, Tr(s.langue(r), "login.titre")))
}

func (s *Server) pageLogin(w http.ResponseWriter, r *http.Request) {
	if s.session(r) != nil {
		rediriger(w, r, "/app")
		return
	}
	s.rendre(w, http.StatusOK, "login.html", s.donnees(r, Tr(s.langue(r), "login.titre")))
}

// connexion ouvre une session administrateur et teste la liaison AD.
func (s *Server) connexion(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	langue := s.langue(r)
	domaine := strings.TrimSpace(r.PostFormValue("domaine"))
	controleur := strings.TrimSpace(r.PostFormValue("controleur"))
	utilisateur := strings.TrimSpace(r.PostFormValue("utilisateur"))
	motDePasse := r.PostFormValue("motdepasse") // jamais logué, jamais persisté sur disque

	var erreurs []string
	if domaine == "" {
		erreurs = append(erreurs, Tr(langue, "erreur.domaine"))
	}
	if controleur == "" {
		erreurs = append(erreurs, Tr(langue, "erreur.controleur"))
	}
	if utilisateur == "" {
		erreurs = append(erreurs, Tr(langue, "erreur.utilisateur"))
	}
	if len(erreurs) > 0 {
		s.erreurs(w, erreurs)
		return
	}

	// Tentative de création du pool AD (sans validation immédiate).
	// La validation réelle aura lieu lors de l'accès aux routes protégées (requireAuth).
	ldapFallback := false
	pool, err := s.creerPoolADSansValidation(domaine, controleur, utilisateur, motDePasse, true)
	if err != nil {
		// Repli LDAP 389 + StartTLS
		ldapFallback = true
		pool, err = s.creerPoolADSansValidation(domaine, controleur, utilisateur, motDePasse, false)
		if err != nil {
			// Impossible de créer le pool même en no-validate : erreur de config
			msg := formaterErreurConnexion(err, langue)
			s.erreurs(w, []string{msg})
			return
		}
	}

	sid := nouveauJeton()
	s.mu.Lock()
	s.sessions[sid] = &session{
		Utilisateur:  utilisateur,
		Domaine:      domaine,
		Controleur:   controleur,
		MotDePasse:   motDePasse,
		LDAPFallback: ldapFallback,
		CreeLe:       time.Now(),
	}
	// Stocker le pool pour cette session (validation différée)
	s.adPools[sid] = pool
	s.mu.Unlock()

	http.SetCookie(w, &http.Cookie{
		Name:     cookieSession,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(dureeSession.Seconds()),
	})

	// Traçabilité : toute connexion est journalisée (append-only).
	if s.journal != nil {
		detail := fmt.Sprintf("Domaine %s, contrôleur %s", domaine, controleur)
		if ldapFallback {
			detail += " (repli LDAP 389)"
		}
		entree := nouvelleEntree("connexion", utilisateur, "succes", detail)
		entree.Utilisateur = utilisateur
		entree.Domaine = domaine
		_ = s.journal.Record(entree)
	}

	rediriger(w, r, "/app")
}

// creerPoolAD tente de créer un pool de connexions AD (avec validation immédiate).
// Si useLDAPS est true, essaie sur le port 636 (LDAPS), sinon sur 389 (LDAP + StartTLS).
func (s *Server) creerPoolAD(domaine, controleur, utilisateur, motDePasse string, useLDAPS bool) (*ad.Pool, error) {
	port := 636
	if !useLDAPS {
		port = 389
	}
	return ad.NewPool(ad.Config{
		Domain:        domaine,
		Controller:    controleur,
		Port:          port,
		UseLDAPS:      useLDAPS,
		AuthMethod:    ad.AuthSimple,
		BindDN:        utilisateur,
		BindPassword:  motDePasse,
		TLSSkipVerify: true, // Phase 0 : certificats auto-signés (labo)
		Timeout:       10 * time.Second,
		MaxConns:      10,
	})
}

// creerPoolADSansValidation crée un pool sans pré-validation de la connexion.
// Utile pour la phase de login : on ne bloque pas l'utilisateur si l'AD est inaccessible.
func (s *Server) creerPoolADSansValidation(domaine, controleur, utilisateur, motDePasse string, useLDAPS bool) (*ad.Pool, error) {
	port := 636
	if !useLDAPS {
		port = 389
	}
	return ad.NewPoolNoValidate(ad.Config{
		Domain:        domaine,
		Controller:    controleur,
		Port:          port,
		UseLDAPS:      useLDAPS,
		AuthMethod:    ad.AuthSimple,
		BindDN:        utilisateur,
		BindPassword:  motDePasse,
		TLSSkipVerify: true, // Phase 0 : certificats auto-signés (labo)
		Timeout:       10 * time.Second,
		MaxConns:      10,
	})
}

// formaterErreurConnexion transforme une erreur AD en message utilisateur localisé.
func formaterErreurConnexion(err error, langue string) string {
	msg := err.Error()
	msgLower := strings.ToLower(msg)

	switch {
	case strings.Contains(msgLower, "certificate") || strings.Contains(msgLower, "certificat") || strings.Contains(msgLower, "tls") || strings.Contains(msgLower, "x509"):
		return Tr(langue, "erreur.connexion_ad_certificat", msg)
	case strings.Contains(msgLower, "invalid credentials") || strings.Contains(msgLower, "invalid dn") || strings.Contains(msgLower, "wrong password") || strings.Contains(msgLower, "account locked") || strings.Contains(msgLower, "logon failure") || strings.Contains(msgLower, "identifiant") || strings.Contains(msgLower, "verrouill"):
		return Tr(langue, "erreur.connexion_ad_bind")
	case strings.Contains(msgLower, "connection refused") || strings.Contains(msgLower, "timeout") || strings.Contains(msgLower, "no route") || strings.Contains(msgLower, "unreachable") || strings.Contains(msgLower, "refus") || strings.Contains(msgLower, "inaccessible") || strings.Contains(msgLower, "network"):
		return Tr(langue, "erreur.connexion_ad_reseau")
	default:
		return Tr(langue, "erreur.connexion_ad") + ": " + msg
	}
}

// tableauBord affiche la page d'accueil de l'application.
func (s *Server) tableauBord(w http.ResponseWriter, r *http.Request) {
	sess := s.session(r)
	if sess == nil {
		rediriger(w, r, "/login")
		return
	}

	langue := s.langue(r)
	d := s.donnees(r, Tr(langue, "dash.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	d.LDAPFallback = sess.LDAPFallback

	// Statut AD avec avertissement si repli LDAP
	if sess.LDAPFallback {
		d.StatutAD = Tr(langue, "avertissement.ldap_fallback")
	} else {
		d.StatutAD = Tr(langue, "statut.ad")
	}

	if s.journal != nil {
		entries, err := s.journal.List(audit.Filter{Limit: 8})
		if err == nil {
			d.Actions = entries
		}
		if n, err := s.journal.Count(); err == nil {
			d.NbActions = n
		}
	}

	s.rendre(w, http.StatusOK, "dashboard.html", d)
}

// deconnexion ferme la session.
func (s *Server) deconnexion(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(cookieSession); err == nil && c.Value != "" {
		s.mu.Lock()
		sess := s.sessions[c.Value]
		// Nettoyer le pool AD associé
		poolKey := sess.Utilisateur + "@" + sess.Domaine
		if sess.LDAPFallback {
			poolKey += ":ldap"
		}
		if pool, ok := s.adPools[poolKey]; ok {
			_ = pool.Close()
			delete(s.adPools, poolKey)
		}
		delete(s.sessions, c.Value)
		s.mu.Unlock()

		if s.journal != nil && sess != nil {
			entree := nouvelleEntree("deconnexion", sess.Utilisateur, "succes", "")
			entree.Utilisateur = sess.Utilisateur
			entree.Domaine = sess.Domaine
			_ = s.journal.Record(entree)
		}
	}
	http.SetCookie(w, &http.Cookie{Name: cookieSession, Value: "", Path: "/", MaxAge: -1})
	rediriger(w, r, "/login")
}

// changerTheme persiste le thème choisi (l'UI est basculée côté client par Alpine).
func (s *Server) changerTheme(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	valeur := strings.TrimSpace(r.PostFormValue("theme"))
	if valeur != "clair" && valeur != "sombre" {
		http.Error(w, "theme invalide", http.StatusBadRequest)
		return
	}
	if err := s.majConfig(func(c *config.AppConfig) { c.Theme = valeur }); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieTheme, Value: valeur, Path: "/", MaxAge: 31536000})
	w.WriteHeader(http.StatusNoContent)
}

// changerLangue change la langue de l'interface et recharge la page courante.
func (s *Server) changerLangue(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	valeur := strings.TrimSpace(r.PostFormValue("langue"))
	if valeur != "fr" && valeur != "en" {
		http.Error(w, "langue invalide", http.StatusBadRequest)
		return
	}
	if err := s.majConfig(func(c *config.AppConfig) { c.Langue = valeur }); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieLangue, Value: valeur, Path: "/", MaxAge: 31536000})

	vers := cheminSeul(r.Header.Get("Hx-Current-URL"))
	if vers == "" {
		vers = cheminSeul(r.Referer())
	}
	if vers == "" {
		vers = "/login"
	}
	rediriger(w, r, vers)
}

// cheminSeul réduit une URL (absolue ou relative) à son chemin local :
// seule la page courante est une cible de redirection valide, ce qui écarte
// toute redirection ouverte (open redirect) venue des en-têtes client.
func cheminSeul(u string) string {
	if u == "" {
		return ""
	}
	parsed, err := url.Parse(u)
	if err != nil || parsed.Path == "" {
		return ""
	}
	if parsed.RawQuery != "" {
		return parsed.Path + "?" + parsed.RawQuery
	}
	return parsed.Path
}

// majConfig applique une modification sur la configuration persistée.
func (s *Server) majConfig(maj func(*config.AppConfig)) error {
	cfg, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		return err
	}
	maj(&cfg)
	return config.Save(cfg, s.opts.ConfigPath)
}

// donnees construit le modèle de base d'une page.
func (s *Server) donnees(r *http.Request, titre string) PageData {
	langue := s.langue(r)
	theme := s.themeCourant(r)

	d := PageData{
		Titre:   titre,
		Langue:  langue,
		Theme:   theme,
		Version: s.opts.Version,
	}

	if chemin, err := s.cheminConfig(); err == nil {
		d.CheminConfig = chemin
	}
	// Le journal injecté (tests, CLI) prime sur le chemin par défaut : ce qui
	// s'affiche est ce qui est réellement lu.
	if s.opts.AuditPath != "" {
		d.CheminDonnees = s.opts.AuditPath
	} else if dir, err := config.DataDir(); err == nil {
		d.CheminDonnees = filepath.Join(dir, "journal.db")
	}
	return d
}

func (s *Server) cheminConfig() (string, error) {
	if s.opts.ConfigPath != "" {
		return s.opts.ConfigPath, nil
	}
	return config.DefaultPath()
}

// getPool retourne le pool AD pour la session courante, ou le crée si nécessaire.
func (s *Server) getPool(sess *session) (*ad.Pool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Clé unique incluant le fallback LDAP (port différent)
	poolKey := sess.Utilisateur + "@" + sess.Domaine
	if sess.LDAPFallback {
		poolKey += ":ldap"
	}

	if pool, ok := s.adPools[poolKey]; ok {
		return pool, nil
	}

	port := 636
	useLDAPS := true
	if sess.LDAPFallback {
		port = 389
		useLDAPS = false
	}

	pool, err := ad.NewPool(ad.Config{
		Domain:        sess.Domaine,
		Controller:    sess.Controleur,
		Port:          port,
		UseLDAPS:      useLDAPS,
		AuthMethod:    ad.AuthSimple,
		BindDN:        sess.Utilisateur,
		BindPassword:  sess.MotDePasse,
		BaseDN:        "",
		TLSSkipVerify: true, // Phase 0 : certificats auto-signés
		Timeout:       10 * time.Second,
		MaxConns:      10,
	})
	if err != nil {
		return nil, err
	}
	s.adPools[poolKey] = pool
	return pool, nil
}

// session retourne la session liée à la requête, ou nil.
func (s *Server) session(r *http.Request) *session {
	c, err := r.Cookie(cookieSession)
	if err != nil || c.Value == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.sessions[c.Value]
	if !ok {
		return nil
	}
	if time.Since(sess.CreeLe) > dureeSession {
		delete(s.sessions, c.Value)
		return nil
	}
	return sess
}

// langue lit le cookie de langue puis la configuration.
func (s *Server) langue(r *http.Request) string {
	if c, err := r.Cookie(cookieLangue); err == nil {
		if c.Value == "fr" || c.Value == "en" {
			return c.Value
		}
	}
	if cfg, err := config.Load(s.opts.ConfigPath); err == nil && (cfg.Langue == "fr" || cfg.Langue == "en") {
		return cfg.Langue
	}
	return "fr"
}

// themeCourant lit le cookie de thème puis la configuration.
func (s *Server) themeCourant(r *http.Request) string {
	if c, err := r.Cookie(cookieTheme); err == nil {
		if c.Value == "clair" || c.Value == "sombre" {
			return c.Value
		}
	}
	if cfg, err := config.Load(s.opts.ConfigPath); err == nil && (cfg.Theme == "clair" || cfg.Theme == "sombre") {
		return cfg.Theme
	}
	return "clair"
}

// rendre exécute un gabarit de page (avec layout complet).
func (s *Server) rendre(w http.ResponseWriter, status int, page string, d PageData) {
	t, ok := s.pages[page]
	if !ok {
		http.Error(w, "page inconnue: "+page, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, "layout", d); err != nil {
		// Le flux est déjà parti : on trace plutôt que de planter.
		fmt.Fprintf(os.Stderr, "ui: rendu de %s: %v\n", page, err)
	}
}

// rendreFragment exécute uniquement le gabarit racine d'un fichier (premier
// {{define}} : « content » pour une page, le nom du fragment sinon), sans
// layout. C'est le rendu destiné aux cibles HTMX (innerHTML) et au fetch()
// de l'explorateur : la réponse doit être un fragment autonome, jamais un
// document complet imbriqué.
func (s *Server) rendreFragment(w http.ResponseWriter, status int, fichier string, d PageData) {
	t, ok := s.pages[fichier]
	if !ok {
		http.Error(w, "gabarit inconnu: "+fichier, http.StatusInternalServerError)
		return
	}
	nom := s.racines[fichier]
	if nom == "" {
		http.Error(w, "gabarit sans {{define}}: "+fichier, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := t.ExecuteTemplate(w, nom, d); err != nil {
		fmt.Fprintf(os.Stderr, "ui: rendu fragment %s (%s): %v\n", fichier, nom, err)
	}
}

// regexpDefine extrait les {{define "nom"}} d'un gabarit.
var regexpDefine = regexp.MustCompile(`{{\s*define\s+"([^"]+)"\s*}}`)

// premierDefine retourne le nom du premier {{define}} d'un fichier gabarit
// (racine de ce fichier). Retourne "" si le fichier n'en définit aucun.
func premierDefine(brut []byte) string {
	// Ignorer les {{define}} imbriqués dans un commentaire Go — non applicable
	// ici : les gabarits du dépôt ne commentent pas leurs définitions.
	if m := regexpDefine.FindSubmatch(brut); m != nil {
		return string(m[1])
	}
	return ""
}

// erreurs renvoie un fragment HTML de validation (usage HTMX, cible #erreurs).
// Le statut 422 est interprété par HTMX grâce au correctif
// « htmx:beforeSwap » du gabarit layout.
func (s *Server) erreurs(w http.ResponseWriter, messages []string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnprocessableEntity)
	var b strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&b, `<p class="mt-1 text-sm font-medium text-red-600 dark:text-red-400">%s</p>`, m)
	}
	_, _ = w.Write([]byte(b.String()))
}

// erreur renvoie un message unique (variant simple de erreurs).
func (s *Server) erreur(w http.ResponseWriter, message string) {
	s.erreurs(w, []string{message})
}

// rediriger répond en HX-Redirect quand la requête vient d'HTMX, sinon par
// une redirection 303 classique (navigation de formulaire).
func rediriger(w http.ResponseWriter, r *http.Request, vers string) {
	if r.Header.Get("HX-Request") == "true" {
		w.Header().Set("HX-Redirect", vers)
		w.WriteHeader(http.StatusOK)
		return
	}
	http.Redirect(w, r, vers, http.StatusSeeOther)
}

// nouvelleEntree fabrique une entrée de journal horodatée en UTC.
func nouvelleEntree(action, compte, resultat, detail string) models.ActionLogEntry {
	return models.ActionLogEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ActionType: action,
		Compte:     compte,
		Resultat:   resultat,
		Detail:     detail,
	}
}

// nouveauJeton produit un identifiant de session aléatoire (128 bits).
func nouveauJeton() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// horodatage formate un horodatage ISO 8601 pour l'affichage
// (locales non prises en charge : format numérique explicite).
func horodatage(valeur string) string {
	t, err := time.Parse(time.RFC3339, valeur)
	if err != nil {
		return valeur
	}
	return t.Local().Format("02/01/2006 15:04:05")
}

func mustSub(name string) fs.FS {
	sub, err := fs.Sub(web.FS, name)
	if err != nil {
		panic("ui: assets manquants: " + err.Error())
	}
	return sub
}

// requireAuth vérifie la session et injecte le pool AD dans le contexte.
func (s *Server) requireAuth(handler func(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.session(r)
		if sess == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusOK)
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}

		pool, err := s.getPool(sess)
		if err != nil {
			s.erreur(w, Tr(s.langue(r), "erreur.connexion_ad")+": "+err.Error())
			return
		}

		handler(w, r, sess, pool)
	}
}

// requireAuthSession vérifie seulement la session, sans exiger de pool AD.
// Utile pour les routes qui n'ont pas besoin d'AD (progression, etc.).
func (s *Server) requireAuthSession(handler func(w http.ResponseWriter, r *http.Request, sess *session)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess := s.session(r)
		if sess == nil {
			if r.Header.Get("HX-Request") == "true" {
				w.Header().Set("HX-Redirect", "/login")
				w.WriteHeader(http.StatusOK)
			} else {
				http.Redirect(w, r, "/login", http.StatusSeeOther)
			}
			return
		}
		handler(w, r, sess)
	}
}

// ─── Page Utilisateurs ─────────────────────────────────────────────────

func (s *Server) pageUsers(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "users.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur

	// OU par défaut pour l'import
	if cfg, err := config.Load(s.opts.ConfigPath); err == nil && cfg.OUParenteClasses != "" {
		d.CheminConfig = cfg.OUParenteClasses
	}

	s.rendre(w, http.StatusOK, "users.html", d)
}

// usersImport charge un CSV et construit le plan d'import (simulation).
func (s *Server) usersImport(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.fichier_trop_gros"))
		return
	}

	file, _, err := r.FormFile("csv_file")
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.csv_manquant"))
		return
	}
	defer file.Close()

	// Parser CSV (auto-détection séparateur)
	reader := csv.NewReader(file)
	reader.Comma = s.detectCSVSeparator(file)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.csv_invalide"))
		return
	}

	if len(records) < 2 {
		s.erreur(w, Tr(s.langue(r), "erreur.csv_vide"))
		return
	}

	// Header → indices
	header := records[0]
	indices := make(map[string]int)
	for i, h := range header {
		indices[strings.TrimSpace(strings.ToLower(h))] = i
	}

	get := func(row []string, key string) string {
		if idx, ok := indices[key]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	// Construire les lignes brutes
	var rows []importRow
	for _, rec := range records[1:] {
		rows = append(rows, importRow{
			Prenom: get(rec, "prenom"),
			Nom:    get(rec, "nom"),
			OU:     get(rec, "ou"),
			Classe: get(rec, "classe"),
			Email:  get(rec, "email"),
		})
	}

	// Charger la config pour génération identifiants/MDP/mail
	cfg, _ := config.Load(s.opts.ConfigPath)

	// Simulation complète : génération + détection doublons + vérification AD
	simulated := s.simulerCreationUsers(sess, pool, rows, cfg, s.langue(r))

	// Stocker la simulation en session pour l'exécution ultérieure
	sid := s.getSessionID(r)
	if sid != "" {
		s.mu.Lock()
		if sess, ok := s.sessions[sid]; ok {
			sess.DerniereSimulation = simulated
			s.sessions[sid] = sess
		}
		s.mu.Unlock()
	}

	// Rendre le template de simulation
	s.rendreSimulationUsers(w, s.langue(r), simulated)
}

// detectCSVSeparator détecte automatiquement le séparateur (; ou ,)
func (s *Server) detectCSVSeparator(file interface {
	Seek(int64, int) (int64, error)
	Read([]byte) (int, error)
}) rune {
	file.Seek(0, 0)
	buf := make([]byte, 1024)
	n, _ := file.Read(buf)
	file.Seek(0, 0)
	line := string(buf[:n])
	if strings.Count(line, ";") > strings.Count(line, ",") {
		return ';'
	}
	return ','
}

// simulerCreationUsers effectue la simulation complète de création d'utilisateurs
func (s *Server) simulerCreationUsers(sess *session, pool *ad.Pool, rows []importRow, cfg config.AppConfig, langue string) []models.GeneratedUser {
	var simulated []models.GeneratedUser
	var usedIdents []string
	var usedEmails []string

	// Déterminer le type de compte (par défaut élève)
	accountType := models.AccountEleve

	// Charger les identifiants existants dans l'AD si pas skip_ad
	skipAD := false // TODO: lire depuis config ou paramètre
	var existingIdents map[string]bool
	if !skipAD {
		existingIdents = s.getExistingSAMAccountNames(sess, pool)
	}

	for _, row := range rows {
		gu := models.GeneratedUser{
			Source: models.RawUserRow{
				Prenom: row.Prenom, Nom: row.Nom, OU: row.OU, Classe: row.Classe, Email: row.Email,
			},
		}

		// Validation de base
		if row.Prenom == "" || row.Nom == "" {
			gu.Erreur = Tr(langue, "erreur.csv_invalide") + ": prénom/nom manquant"
			simulated = append(simulated, gu)
			continue
		}

		// Génération identifiant
		gu.Identifiant = cfg.GenerateIdentifiant(row.Prenom, row.Nom, accountType)

		// Résolution doublons CSV
		if contains(usedIdents, gu.Identifiant) {
			gu.Identifiant, gu.DoublonResolu = cfg.ResolveDuplicate(gu.Identifiant, usedIdents, cfg.RegleDoublons)
		}
		usedIdents = append(usedIdents, gu.Identifiant)

		// Vérification AD existant
		if existingIdents != nil && existingIdents[strings.ToLower(gu.Identifiant)] {
			gu.DoublonAD = true
			gu.Erreur = Tr(langue, "users.doublon_ad")
		}

		// Génération mot de passe
		mdp, _ := cfg.GeneratePassword(cfg.PolitiqueMDPEleve)
		gu.MotDePasse = mdp

		// Génération mail
		domaineMail := cfg.DomaineMail
		if domaineMail == "" {
			domaineMail = strings.ToLower(sess.Domaine)
		}
		gu.AdresseMail = cfg.GenerateEmail(row.Prenom, row.Nom, domaineMail)
		if contains(usedEmails, gu.AdresseMail) {
			// Résoudre doublon mail
			base := strings.Split(gu.AdresseMail, "@")[0]
			domain := strings.Split(gu.AdresseMail, "@")[1]
			resolved, _ := cfg.ResolveDuplicate(base, usedEmails, cfg.RegleDoublons)
			gu.AdresseMail = resolved + "@" + domain
		}
		usedEmails = append(usedEmails, gu.AdresseMail)

		// OU cible
		gu.OUCible = row.OU
		if gu.OUCible == "" && row.Classe != "" {
			gu.OUCible = config.BuildUserDN(config.SanitizeDNPart(row.Classe), "OU=Eleves,"+config.DomaineToBaseDN(sess.Domaine))
		} else if gu.OUCible == "" {
			gu.OUCible = "OU=Eleves," + config.DomaineToBaseDN(sess.Domaine)
		}

		// Groupe classe auto
		if cfg.GroupesClasseAuto && row.Classe != "" {
			gu.Groupe = "CN=" + config.SanitizeDNPart(row.Classe) + ",OU=Groupes," + config.DomaineToBaseDN(sess.Domaine)
		}

		simulated = append(simulated, gu)
	}

	return simulated
}

// getExistingSAMAccountNames récupère tous les sAMAccountName existants dans l'AD
func (s *Server) getExistingSAMAccountNames(sess *session, pool *ad.Pool) map[string]bool {
	result := make(map[string]bool)
	// Recherche dans la base du domaine
	baseDN := config.DomaineToBaseDN(sess.Domaine)
	users, err := pool.UserSearch(context.Background(), baseDN, "", nil, 1000)
	if err != nil {
		return result
	}
	for _, u := range users {
		result[strings.ToLower(u.SAMAccountName)] = true
	}
	return result
}

// contains vérifie si une chaîne est dans un slice
func contains(slice []string, item string) bool {
	for _, s := range slice {
		if strings.EqualFold(s, item) {
			return true
		}
	}
	return false
}

// getSessionID extrait l'ID de session depuis la requête
func (s *Server) getSessionID(r *http.Request) string {
	c, err := r.Cookie(cookieSession)
	if err != nil || c.Value == "" {
		return ""
	}
	return c.Value
}

// ─── Users : simulation & exécution ────────────────────────────────────

func (s *Server) usersSimulate(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	// Récupérer la simulation stockée en session
	sid := s.getSessionID(r)
	if sid == "" {
		s.erreur(w, Tr(s.langue(r), "users.aucune_simulation"))
		return
	}

	s.mu.Lock()
	sessionData, ok := s.sessions[sid]
	s.mu.Unlock()

	if !ok || sessionData.DerniereSimulation == nil {
		s.erreur(w, Tr(s.langue(r), "users.aucune_simulation"))
		return
	}

	// Convertir pour le template
	lines := make([]SimulatedUserLine, len(sessionData.DerniereSimulation))
	valides, erreurs := 0, 0
	for i, u := range sessionData.DerniereSimulation {
		lines[i] = SimulatedUserLine{
			Prenom:        u.Source.Prenom,
			Nom:           u.Source.Nom,
			Identifiant:   u.Identifiant,
			Mail:          u.AdresseMail,
			MotDePasse:    u.MotDePasse,
			OUCible:       u.OUCible,
			Groupe:        u.Groupe,
			DoublonResolu: u.DoublonResolu,
			DoublonAD:     u.DoublonAD,
			Erreur:        u.Erreur,
		}
		if u.Erreur != "" {
			erreurs++
		} else {
			valides++
		}
	}

	d := s.donnees(r, Tr(s.langue(r), "users.simulate_titre"))
	d.Simulation = &SimulationResult{
		Lignes:  lines,
		Valides: valides,
		Erreurs: erreurs,
		Total:   len(lines),
	}
	d.LDAPFallback = sess.LDAPFallback

	// Parser les options du formulaire
	if err := r.ParseForm(); err == nil {
		d.ForcerMdp = r.FormValue("forcer_mdp")
		d.SkipAd = r.FormValue("skip_ad")
	}

	s.rendre(w, http.StatusOK, "users_simulate.html", d)
}

func (s *Server) usersExecute(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	sid := s.getSessionID(r)
	if sid == "" {
		s.erreur(w, Tr(s.langue(r), "users.aucune_simulation"))
		return
	}

	// Récupérer la simulation
	s.mu.Lock()
	sessionData, ok := s.sessions[sid]
	s.mu.Unlock()

	if !ok || sessionData.DerniereSimulation == nil {
		s.erreur(w, Tr(s.langue(r), "users.aucune_simulation"))
		return
	}

	// Parser le formulaire
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	forcerMdp := r.FormValue("forcer_mdp") == "1"
	_ = r.FormValue("skip_ad") // lu pour compatibilité future

	// Créer une opération de suivi avec contexte annulable (A12).
	opID, opCtx := s.nouvelOperation("users")
	total := len(sessionData.DerniereSimulation)

	var resultLines []MigrationExecLine
	crees, echecs := 0, 0

	for i, gu := range sessionData.DerniereSimulation {
		// Vérifier l'annulation avant chaque ligne (A12).
		if progress.Annulable(opCtx) {
			s.terminerOperation(opID, false, Tr(s.langue(r), "progress.annulee"), "")
			s.rendre(w, http.StatusOK, "users_execute.html", s.donnees(r, Tr(s.langue(r), "users.execute_titre")))
			return
		}

		s.majProgression(opID, i, fmt.Sprintf("%s %s", gu.Identifiant, gu.NomComplet()))

		if gu.Erreur != "" {
			// Ligne en erreur, ne pas tenter de créer
			resultLines = append(resultLines, MigrationExecLine{
				Prenom: gu.Source.Prenom, Nom: gu.Source.Nom, Identifiant: gu.Identifiant,
				Statut: "echec", Detail: gu.Erreur,
			})
			echecs++
			continue
		}

		// Construire les paramètres de création
		params := ad.UserCreateParams{
			SAMAccountName:    gu.Identifiant,
			UserPrincipalName: gu.Identifiant + "@" + sess.Domaine,
			DisplayName:       gu.Source.Prenom + " " + gu.Source.Nom,
			GivenName:         gu.Source.Prenom,
			SN:                gu.Source.Nom,
			InitialPassword:   gu.MotDePasse,
			OU:                gu.OUCible,
			Description:       "Créé via EduSync-AD",
			Email:             gu.AdresseMail,
			Enabled:           true,
			Groups:            []string{},
		}
		if gu.Groupe != "" {
			params.Groups = []string{gu.Groupe}
		}

		// Simulation AD (vérification finale)
		simResult, err := pool.UserCreateSimulate(opCtx, params)
		if err != nil || !simResult.WouldSucceed {
			resultLines = append(resultLines, MigrationExecLine{
				Prenom: gu.Source.Prenom, Nom: gu.Source.Nom, Identifiant: gu.Identifiant,
				Statut: "echec", Detail: simResult.Error,
			})
			echecs++
			continue
		}

		// Exécution réelle
		createdUser, err := pool.UserCreate(opCtx, params)
		if err != nil {
			resultLines = append(resultLines, MigrationExecLine{
				Prenom: gu.Source.Prenom, Nom: gu.Source.Nom, Identifiant: gu.Identifiant,
				Statut: "echec", Detail: err.Error(),
			})
			echecs++
			continue
		}

		// Forcer changement MDP si demandé
		if forcerMdp {
			_ = pool.UserResetPassword(opCtx, createdUser.DN, gu.MotDePasse)
		}

		// Journaliser
		if s.journal != nil {
			entree := nouvelleEntree("creation_utilisateur", gu.Identifiant, "succes",
				fmt.Sprintf("Prénom: %s, Nom: %s, OU: %s", gu.Source.Prenom, gu.Source.Nom, gu.OUCible))
			entree.Utilisateur = sess.Utilisateur
			entree.Domaine = sess.Domaine
			_ = s.journal.Record(entree)
		}

		resultLines = append(resultLines, MigrationExecLine{
			Prenom: gu.Source.Prenom, Nom: gu.Source.Nom, Identifiant: gu.Identifiant,
			Statut: "succes", Detail: "Créé dans " + gu.OUCible,
		})
		crees++
	}

	// Marquer l'opération comme terminée.
	s.majProgression(opID, total, Tr(s.langue(r), "progress.termine"))
	s.terminerOperation(opID, true, Tr(s.langue(r), "progress.termine"), "")

	// Nettoyer la simulation
	s.mu.Lock()
	if sessionData, ok := s.sessions[sid]; ok {
		sessionData.DerniereSimulation = nil
		s.sessions[sid] = sessionData
	}
	s.mu.Unlock()

	d := s.donnees(r, Tr(s.langue(r), "users.execute_titre"))
	d.Resultat = &ExecuteResult{
		Crees:  crees,
		Echecs: echecs,
		Lignes: resultLines,
	}
	d.LDAPFallback = sess.LDAPFallback

	s.rendre(w, http.StatusOK, "users_execute.html", d)
}

// ─── Page Mot de passe ─────────────────────────────────────────────────

func (s *Server) pagePassword(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "pwd.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	s.rendre(w, http.StatusOK, "password.html", d)
}

func (s *Server) passwordSimulate(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	mode := r.FormValue("mode")
	ou := r.FormValue("ou")
	groupDN := r.FormValue("group_dn")
	policyName := r.FormValue("policy")
	forceChange := r.FormValue("force_change") == "1"

	cfg, _ := config.Load(s.opts.ConfigPath)

	// Déterminer la politique
	policy := cfg.PolitiqueMDPEleve
	if policyName == "personnel" {
		policy = cfg.PolitiqueMDPPersonnel
	}

	// Paramètres de réinitialisation
	params := ad.PasswordResetParams{
		OU:          ou,
		GroupDN:     groupDN,
		Policy:      s.configPolicyToADPolicy(policy),
		ForceChange: forceChange,
		DryRun:      true, // Simulation
	}

	var results []ad.PasswordResetResult
	var err error

	switch mode {
	case "ou":
		if ou == "" {
			s.erreur(w, Tr(s.langue(r), "pwd.erreur_ou_vide"))
			return
		}
		params.OU = ou
		results, err = pool.PasswordResetBulk(r.Context(), params)
	case "group":
		if groupDN == "" {
			s.erreur(w, Tr(s.langue(r), "pwd.erreur_groupe_vide"))
			return
		}
		params.GroupDN = groupDN
		results, err = pool.PasswordResetBulk(r.Context(), params)
	case "csv":
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			s.erreur(w, Tr(s.langue(r), "erreur.fichier_trop_gros"))
			return
		}
		file, _, err := r.FormFile("csv_file")
		if err != nil {
			s.erreur(w, Tr(s.langue(r), "erreur.csv_manquant"))
			return
		}
		defer file.Close()

		reader := csv.NewReader(file)
		reader.Comma = s.detectCSVSeparator(file)
		reader.FieldsPerRecord = -1
		reader.LazyQuotes = true

		records, err := reader.ReadAll()
		if err != nil {
			s.erreur(w, Tr(s.langue(r), "erreur.csv_invalide"))
			return
		}

		if len(records) < 2 {
			s.erreur(w, Tr(s.langue(r), "erreur.csv_vide"))
			return
		}

		header := records[0]
		indices := make(map[string]int)
		for i, h := range header {
			indices[strings.TrimSpace(strings.ToLower(h))] = i
		}

		get := func(row []string, key string) string {
			if idx, ok := indices[key]; ok && idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}

		var targetDNs []string
		for _, rec := range records[1:] {
			dn := get(rec, "identifiant")
			if dn == "" {
				dn = get(rec, "samaccountname")
			}
			if dn != "" {
				targetDNs = append(targetDNs, dn)
			}
		}

		if len(targetDNs) == 0 {
			s.erreur(w, Tr(s.langue(r), "erreur.csv_vide"))
			return
		}

		// Pour chaque DN, faire une simulation individuelle
		results = make([]ad.PasswordResetResult, 0, len(targetDNs))
		for _, dn := range targetDNs {
			singleParams := params
			singleParams.TargetDN = dn
			singleParams.OU = ""
			singleParams.GroupDN = ""
			r, err := pool.PasswordResetBulk(r.Context(), singleParams)
			if err != nil {
				results = append(results, ad.PasswordResetResult{
					UserDN: dn, Error: fmt.Sprintf("simulation: %v", err),
				})
			} else if len(r) > 0 {
				results = append(results, r[0])
			}
		}
	default:
		s.erreur(w, Tr(s.langue(r), "pwd.erreur_mode_invalide"))
		return
	}

	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.connexion_ad")+": "+err.Error())
		return
	}

	// Convertir pour le template
	lines := make([]PasswordSimLine, len(results))
	valides, erreurs := 0, 0
	for i, res := range results {
		generated := res.Generated
		if res.NewPassword == "" && generated {
			// Générer un MDP pour l'affichage (simulation)
			pwd, _ := params.Policy.Generate()
			res.NewPassword = pwd
		}
		lines[i] = PasswordSimLine{
			Prenom:      res.DisplayName, // On n'a pas le prénom/nom séparés ici
			Nom:         "",
			Identifiant: res.SAMAccountName,
			Mail:        "",
			NouveauMDP:  res.NewPassword,
			OUCible:     res.UserDN,
			Erreur:      res.Error,
		}
		if res.Error != "" {
			erreurs++
		} else {
			valides++
		}
	}

	// Stocker la simulation en session
	sid := s.getSessionID(r)
	if sid != "" {
		s.mu.Lock()
		if sess, ok := s.sessions[sid]; ok {
			sess.DerniereSimPassword = results
			sess.DerniereSimPasswordParams = params
			s.sessions[sid] = sess
		}
		s.mu.Unlock()
	}

	d := s.donnees(r, Tr(s.langue(r), "pwd.simulate_titre"))
	d.SimulationPwd = &PasswordSimResult{
		Lignes:  lines,
		Valides: valides,
		Erreurs: erreurs,
		Total:   len(lines),
	}
	d.LDAPFallback = sess.LDAPFallback
	d.ForceChange = forceChange

	s.rendre(w, http.StatusOK, "password_simulate.html", d)
}

func (s *Server) passwordExecute(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	forceChange := r.FormValue("force_change") == "1"

	sid := s.getSessionID(r)
	if sid == "" {
		s.erreur(w, Tr(s.langue(r), "pwd.aucune_simulation"))
		return
	}

	s.mu.Lock()
	sessionData, ok := s.sessions[sid]
	s.mu.Unlock()

	if !ok || sessionData.DerniereSimPassword == nil {
		s.erreur(w, Tr(s.langue(r), "pwd.aucune_simulation"))
		return
	}

	// Rejouer avec DryRun = false
	params := sessionData.DerniereSimPasswordParams
	params.DryRun = false
	params.ForceChange = forceChange

	results, err := pool.PasswordResetBulk(r.Context(), params)
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.connexion_ad")+": "+err.Error())
		return
	}

	var resultLines []MigrationExecLine
	crees, echecs := 0, 0

	for _, res := range results {
		if res.Error != "" {
			resultLines = append(resultLines, MigrationExecLine{
				Identifiant: res.SAMAccountName,
				NouveauMDP:  res.NewPassword,
				Statut:      "echec",
				Detail:      res.Error,
			})
			echecs++
		} else {
			// Journaliser
			if s.journal != nil {
				entree := nouvelleEntree("reinitialisation_mdp", res.SAMAccountName, "succes",
					fmt.Sprintf("OU: %s, généré: %v", res.UserDN, res.Generated))
				entree.Utilisateur = sess.Utilisateur
				entree.Domaine = sess.Domaine
				_ = s.journal.Record(entree)
			}
			resultLines = append(resultLines, MigrationExecLine{
				Identifiant: res.SAMAccountName,
				NouveauMDP:  res.NewPassword,
				Statut:      "succes",
				Detail:      "Réinitialisé avec succès",
			})
			crees++
		}
	}

	// Nettoyer la simulation
	s.mu.Lock()
	if sessionData, ok := s.sessions[sid]; ok {
		sessionData.DerniereSimPassword = nil
		sessionData.DerniereSimPasswordParams = ad.PasswordResetParams{}
		s.sessions[sid] = sessionData
	}
	s.mu.Unlock()

	d := s.donnees(r, Tr(s.langue(r), "pwd.execute_titre"))
	d.ResultatPwd = &PasswordExecResult{
		Crees:  crees,
		Echecs: echecs,
		Lignes: resultLines,
	}
	d.LDAPFallback = sess.LDAPFallback

	s.rendre(w, http.StatusOK, "password_execute.html", d)
}

// configPolicyToADPolicy convertit models.PasswordPolicy en ad.PasswordPolicy
func (s *Server) configPolicyToADPolicy(p models.PasswordPolicy) ad.PasswordPolicy {
	return ad.PasswordPolicy{
		Length:           p.Longueur,
		RequireUppercase: p.Majuscules,
		RequireDigits:    p.Chiffres,
		RequireSpecial:   p.CaracteresSpeciaux,
		ForbidUserInfo:   p.MotDePasseIdentique,
		MaxAgeDays:       0,
		HistorySize:      0,
		MinAgeDays:       0,
	}
}

// ─── Page Migration ────────────────────────────────────────────────────

func (s *Server) pageMigration(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "migration.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	s.rendre(w, http.StatusOK, "migration.html", d)
}

func (s *Server) migrationPlan(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseMultipartForm(10 << 20); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.fichier_trop_gros"))
		return
	}

	file, _, err := r.FormFile("csv_file")
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.csv_manquant"))
		return
	}
	defer file.Close()

	reader := csv.NewReader(file)
	reader.Comma = s.detectCSVSeparator(file)
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.csv_invalide"))
		return
	}

	if len(records) < 2 {
		s.erreur(w, Tr(s.langue(r), "erreur.csv_vide"))
		return
	}

	// Header → indices
	header := records[0]
	indices := make(map[string]int)
	for i, h := range header {
		indices[strings.TrimSpace(strings.ToLower(h))] = i
	}

	get := func(row []string, key string) string {
		if idx, ok := indices[key]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	type importRow struct {
		Identifiant, OUSource, OUCible, Prenom, Nom string
	}
	var rows []importRow
	for _, rec := range records[1:] {
		rows = append(rows, importRow{
			Identifiant: get(rec, "identifiant"),
			OUSource:    get(rec, "ou_source"),
			OUCible:     get(rec, "ou_cible"),
			Prenom:      get(rec, "prenom"),
			Nom:         get(rec, "nom"),
		})
	}

	// Construire le plan de migration
	var adRows []ad.MigrationRow
	for _, row := range rows {
		adRows = append(adRows, ad.MigrationRow{
			SAMAccountName: row.Identifiant,
			SourceOU:       row.OUSource,
			TargetOU:       row.OUCible,
		})
	}

	plan, err := pool.MigrationPlanFromCSV(r.Context(), adRows)
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.connexion_ad")+": "+err.Error())
		return
	}

	// Convertir pour le template
	lines := make([]MigrationPlanLine, len(plan.Rows))
	for i, row := range plan.Rows {
		lines[i] = MigrationPlanLine{
			Identifiant: row.SAMAccountName,
			NomComplet:  row.DisplayName,
			OUSource:    row.SourceOU,
			OUCible:     row.TargetOU,
			Action:      s.actionLabel(row, s.langue(r)),
			Erreur:      row.Error,
		}
	}

	// Stocker le plan en session
	sid := s.getSessionID(r)
	if sid != "" {
		s.mu.Lock()
		if sess, ok := s.sessions[sid]; ok {
			sess.DernierPlanMigration = plan
			s.sessions[sid] = sess
		}
		s.mu.Unlock()
	}

	d := s.donnees(r, Tr(s.langue(r), "migration.plan_titre"))
	d.PlanMigration = &MigrationPlanResult{
		Lignes:   lines,
		Total:    plan.Total,
		WillMove: plan.WillMove,
		WillSkip: plan.WillSkip,
	}
	d.LDAPFallback = sess.LDAPFallback

	s.rendre(w, http.StatusOK, "migration_plan.html", d)
}

func (s *Server) migrationSimulate(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	sid := s.getSessionID(r)
	if sid == "" {
		s.erreur(w, Tr(s.langue(r), "migration.aucune_plan"))
		return
	}

	s.mu.Lock()
	sessionData, ok := s.sessions[sid]
	s.mu.Unlock()

	if !ok || sessionData.DernierPlanMigration == nil {
		s.erreur(w, Tr(s.langue(r), "migration.aucune_plan"))
		return
	}

	plan := sessionData.DernierPlanMigration
	_, err := pool.MigrationSimulate(r.Context(), plan)
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.connexion_ad")+": "+err.Error())
		return
	}

	// Convertir pour le template
	lines := make([]MigrationSimLine, len(plan.Rows))
	valides, erreurs := 0, 0
	for i, row := range plan.Rows {
		lines[i] = MigrationSimLine{
			Identifiant: row.SAMAccountName,
			NomComplet:  row.DisplayName,
			OUSource:    row.SourceOU,
			OUCible:     row.TargetOU,
			Action:      s.actionLabel(row, s.langue(r)),
			Erreur:      row.Error,
		}
		if row.Error != "" {
			erreurs++
		} else if row.SourceOU != row.TargetOU {
			valides++
		} else {
			valides++ // skip aussi compté comme valide
		}
	}

	d := s.donnees(r, Tr(s.langue(r), "migration.simulation_titre"))
	d.SimulationMigration = &MigrationSimResult{
		Lignes:  lines,
		Total:   len(lines),
		Valides: valides,
		Erreurs: erreurs,
	}
	d.LDAPFallback = sess.LDAPFallback

	s.rendre(w, http.StatusOK, "migration_simulate.html", d)
}

func (s *Server) migrationExecute(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	sid := s.getSessionID(r)
	if sid == "" {
		s.erreur(w, Tr(s.langue(r), "migration.aucune_plan"))
		return
	}

	s.mu.Lock()
	sessionData, ok := s.sessions[sid]
	s.mu.Unlock()

	if !ok || sessionData.DernierPlanMigration == nil {
		s.erreur(w, Tr(s.langue(r), "migration.aucune_plan"))
		return
	}

	plan := sessionData.DernierPlanMigration
	err := pool.MigrationExecute(r.Context(), plan)
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.connexion_ad")+": "+err.Error())
		return
	}

	// Groupes de classe auto (post-migration)
	cfg, _ := config.Load(s.opts.ConfigPath)
	if cfg.GroupesClasseAuto {
		// Pour chaque OU cible qui a reçu des élèves, sync le groupe
		targetOUs := make(map[string]bool)
		for _, row := range plan.Rows {
			if row.Error == "" && row.SourceOU != row.TargetOU {
				targetOUs[row.TargetOU] = true
			}
		}
		for ou := range targetOUs {
			_ = pool.MigrationGroupsByOU(r.Context(), ou, "Classe")
		}
	}

	var resultLines []MigrationExecLine
	crees, echecs := 0, 0

	for _, row := range plan.Rows {
		if row.Error != "" {
			resultLines = append(resultLines, MigrationExecLine{
				Identifiant: row.SAMAccountName,
				NomComplet:  row.DisplayName,
				OUSource:    row.SourceOU,
				OUCible:     row.TargetOU,
				Statut:      "echec",
				Detail:      row.Error,
			})
			echecs++
		} else if row.SourceOU == row.TargetOU {
			resultLines = append(resultLines, MigrationExecLine{
				Identifiant: row.SAMAccountName,
				NomComplet:  row.DisplayName,
				OUSource:    row.SourceOU,
				OUCible:     row.TargetOU,
				Statut:      "skip",
				Detail:      "Déjà dans l'OU cible",
			})
		} else {
			// Journaliser
			if s.journal != nil {
				entree := nouvelleEntree("migration", row.SAMAccountName, "succes",
					fmt.Sprintf("Déplacé de %s vers %s", row.SourceOU, row.TargetOU))
				entree.Utilisateur = sess.Utilisateur
				entree.Domaine = sess.Domaine
				_ = s.journal.Record(entree)
			}
			resultLines = append(resultLines, MigrationExecLine{
				Identifiant: row.SAMAccountName,
				NomComplet:  row.DisplayName,
				OUSource:    row.SourceOU,
				OUCible:     row.TargetOU,
				Statut:      "succes",
				Detail:      "Déplacé avec succès",
			})
			crees++
		}
	}

	// Nettoyer le plan
	s.mu.Lock()
	if sessionData, ok := s.sessions[sid]; ok {
		sessionData.DernierPlanMigration = nil
		s.sessions[sid] = sessionData
	}
	s.mu.Unlock()

	d := s.donnees(r, Tr(s.langue(r), "migration.execute_titre"))
	d.MigrationExecute = &MigrationExecResult{
		Crees:  crees,
		Echecs: echecs,
		Lignes: resultLines,
	}
	d.LDAPFallback = sess.LDAPFallback

	s.rendre(w, http.StatusOK, "migration_execute.html", d)
}

// ─── Page Départs ──────────────────────────────────────────────────────

func (s *Server) pageDeparts(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "departs.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	s.rendre(w, http.StatusOK, "departs.html", d)
}

func (s *Server) departsSimulate(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	langue := s.langue(r)
	userDN := strings.TrimSpace(r.FormValue("user_dn"))
	mode := ad.DepartMode(r.FormValue("mode"))
	delaiJours := 0
	if v := r.FormValue("delai_jours"); v != "" {
		fmt.Sscanf(v, "%d", &delaiJours)
	}

	params := ad.DepartParams{
		UserDN:     userDN,
		Mode:       mode,
		DelaiJours: delaiJours,
		Motif:      r.FormValue("motif"),
	}

	result, err := pool.DepartSimulate(r.Context(), params)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
		return
	}

	s.rendreDepartResultat(w, langue, result)
}

func (s *Server) departsExecute(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	langue := s.langue(r)
	userDN := strings.TrimSpace(r.FormValue("user_dn"))
	mode := ad.DepartMode(r.FormValue("mode"))
	delaiJours := 0
	if v := r.FormValue("delai_jours"); v != "" {
		fmt.Sscanf(v, "%d", &delaiJours)
	}

	params := ad.DepartParams{
		UserDN:     userDN,
		Mode:       mode,
		DelaiJours: delaiJours,
		Motif:      r.FormValue("motif"),
	}

	result, err := pool.DepartExecute(r.Context(), params)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
		return
	}

	// Journaliser le départ
	if s.journal != nil {
		entree := nouvelleEntree("depart", result.SAMAccountName, result.Statut,
			fmt.Sprintf("Mode: %s, délai: %d jours, motif: %s", params.Mode, params.DelaiJours, params.Motif))
		entree.Utilisateur = sess.Utilisateur
		entree.Domaine = sess.Domaine
		_ = s.journal.Record(entree)
	}

	// Si mode différé, enregistrer la suppression planifiée dans le journal
	if params.Mode == ad.DepartModeDiffere && result.Statut == "succes" && s.journal != nil {
		sid := s.getSessionID(r)
		_ = s.journal.RecordPendingDeletion(result.UserDN, result.SAMAccountName, result.DisplayName, sid, params.DelaiJours)
	}

	s.rendreDepartResultat(w, langue, result)
}

// rendreDepartResultat affiche le résultat d'un départ (simulation ou exécution).
func (s *Server) rendreDepartResultat(w http.ResponseWriter, langue string, result *ad.DepartResult) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<div class="rounded-xl border border-slate-200 bg-white p-6 shadow-sm dark:border-slate-800 dark:bg-slate-900">`)
	b.WriteString(`<h2 class="text-lg font-semibold">Désactivation / Réactivation</h2>`)
	b.WriteString(`<div class="mt-2 space-y-2">`)
	b.WriteString(fmt.Sprintf(`<p class="text-sm"><strong>Identifiant:</strong> %s</p>`, result.SAMAccountName))
	b.WriteString(fmt.Sprintf(`<p class="text-sm"><strong>Nom:</strong> %s</p>`, result.DisplayName))
	b.WriteString(fmt.Sprintf(`<p class="text-sm"><strong>Mode:</strong> %s</p>`, result.Mode))
	if result.DelaiJours > 0 {
		b.WriteString(fmt.Sprintf(`<p class="text-sm"><strong>Délai avant purge:</strong> %d jours</p>`, result.DelaiJours))
	}
	b.WriteString(fmt.Sprintf(`<p class="text-sm font-medium"><strong>Statut:</strong> %s</p>`, result.Statut))
	b.WriteString(fmt.Sprintf(`<p class="text-sm text-slate-500"><strong>Détail:</strong> %s</p>`, result.Detail))
	b.WriteString(`</div></div>`)
	w.Write([]byte(b.String()))
}

// ─── Page Paramètres globaux ───────────────────────────────────────────

func (s *Server) pageParametres(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "parametres.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur

	s.rendre(w, http.StatusOK, "parametres.html", d)
}

// parametresComptes sauvegarde l'onglet Comptes (génération identifiants,
// doublons, groupes de classe, OU).
func (s *Server) parametresComptes(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	maj := func(cfg *config.AppConfig) {
		cfg.IdentifiantFormatEleve = strings.TrimSpace(r.FormValue("format_eleve"))
		cfg.IdentifiantFormatPersonnel = strings.TrimSpace(r.FormValue("format_personnel"))
		cfg.RegleDoublons = models.DoublonRule(strings.TrimSpace(r.FormValue("regle_doublons")))
		cfg.ReglePrenomCompose = models.PrenomComposeRule(strings.TrimSpace(r.FormValue("regle_prenom")))
		cfg.GroupesClasseAuto = r.FormValue("groupes_auto") == "1"
		cfg.OUParenteClasses = strings.TrimSpace(r.FormValue("ou_parente"))
		cfg.OUArchive = strings.TrimSpace(r.FormValue("ou_archive"))
		if v := r.FormValue("delai_suppression"); v != "" {
			fmt.Sscanf(v, "%d", &cfg.DelaiSuppressionJours)
		}
		cfg.DomaineMail = strings.TrimSpace(r.FormValue("domaine_mail"))
		cfg.FormatMail = strings.TrimSpace(r.FormValue("format_mail"))
	}
	if err := s.majConfig(maj); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.config")+": "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNoContent)
}

// parametresMotsDePasse sauvegarde l'onglet Mots de passe (politiques).
func (s *Server) parametresMotsDePasse(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	maj := func(cfg *config.AppConfig) {
		// Politique élèves
		if v := r.FormValue("pwd_eleve_longueur"); v != "" {
			fmt.Sscanf(v, "%d", &cfg.PolitiqueMDPEleve.Longueur)
		}
		cfg.PolitiqueMDPEleve.Majuscules = r.FormValue("pwd_eleve_majuscules") == "1"
		cfg.PolitiqueMDPEleve.Chiffres = r.FormValue("pwd_eleve_chiffres") == "1"
		cfg.PolitiqueMDPEleve.CaracteresSpeciaux = r.FormValue("pwd_eleve_speciaux") == "1"
		cfg.PolitiqueMDPEleve.MotDePasseIdentique = r.FormValue("pwd_eleve_interdit_info") == "1"

		// Politique personnels
		if v := r.FormValue("pwd_personnel_longueur"); v != "" {
			fmt.Sscanf(v, "%d", &cfg.PolitiqueMDPPersonnel.Longueur)
		}
		cfg.PolitiqueMDPPersonnel.Majuscules = r.FormValue("pwd_personnel_majuscules") == "1"
		cfg.PolitiqueMDPPersonnel.Chiffres = r.FormValue("pwd_personnel_chiffres") == "1"
		cfg.PolitiqueMDPPersonnel.CaracteresSpeciaux = r.FormValue("pwd_personnel_speciaux") == "1"
		cfg.PolitiqueMDPPersonnel.MotDePasseIdentique = r.FormValue("pwd_personnel_interdit_info") == "1"
	}
	if err := s.majConfig(maj); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.config")+": "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNoContent)
}

// parametresApparence sauvegarde l'onglet Apparence (thème, langue).
func (s *Server) parametresApparence(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	maj := func(cfg *config.AppConfig) {
		theme := strings.TrimSpace(r.FormValue("theme"))
		if theme == "clair" || theme == "sombre" {
			cfg.Theme = theme
		}
		langue := strings.TrimSpace(r.FormValue("langue"))
		if langue == "fr" || langue == "en" {
			cfg.Langue = langue
		}
	}
	if err := s.majConfig(maj); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.config")+": "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNoContent)
}

// actionLabel retourne le libellé d'action pour une ligne de migration
func (s *Server) actionLabel(row ad.MigrationRow, langue string) string {
	if row.Error != "" {
		return Tr(langue, "migration.action.erreur")
	}
	if row.SourceOU == row.TargetOU {
		return Tr(langue, "migration.action.skip")
	}
	return Tr(langue, "migration.action.move")
}

// ─── Page Explorateur ──────────────────────────────────────────────────

func (s *Server) pageExplorer(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "explorer.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur

	// Arbre rendu côté serveur (affichage immédiat, utilisable sans JS) ;
	// en cas d'échec, le client retente un chargement à vide.
	tree, err := pool.BuildTree(r.Context(), pool.BaseDN(), 3, true)
	if err != nil {
		d.Erreurs = append(d.Erreurs, Tr(s.langue(r), "erreur.connexion_ad")+": "+err.Error())
	} else {
		d.ExplorerTree = tree
	}
	s.rendre(w, http.StatusOK, "explorer.html", d)
}

func (s *Server) explorerSearch(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	langue := s.langue(r)

	filter := ad.SearchFilter{
		Query:           r.PostFormValue("q"),
		ObjectTypes:     r.Form["object_types"],
		BaseDN:          r.FormValue("base_dn"),
		Cookie:          r.PostFormValue("cookie"),
		PageSize:        100,
		IncludeDisabled: r.FormValue("include_disabled") == "1",
	}

	result, err := pool.Search(r.Context(), filter)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
		return
	}

	d := s.donnees(r, Tr(langue, "explorer.results_title"))
	d.SearchResult = result
	d.LDAPFallback = sess.LDAPFallback
	s.rendreFragment(w, http.StatusOK, "explorer_results.html", d)
}

func (s *Server) explorerTree(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	langue := s.langue(r)
	parentDN := r.FormValue("parent_dn")
	includeGroups := r.FormValue("include_groups") == "1"

	if parentDN == "" {
		tree, err := pool.BuildTree(r.Context(), pool.BaseDN(), 3, includeGroups)
		if err != nil {
			s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
			return
		}
		d := s.donnees(r, Tr(langue, "explorer.tree_title"))
		d.ExplorerTree = tree
		d.LDAPFallback = sess.LDAPFallback
		s.rendreFragment(w, http.StatusOK, "explorer_tree.html", d)
		return
	}

	nodes, err := pool.GetSubTree(r.Context(), parentDN, true)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
		return
	}

	d := s.donnees(r, "")
	d.ExplorerNodes = nodes
	d.LDAPFallback = sess.LDAPFallback
	s.rendreFragment(w, http.StatusOK, "explorer_tree.html", d)
}

func (s *Server) explorerDetails(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	dn := r.FormValue("dn")
	if dn == "" {
		s.erreur(w, Tr(s.langue(r), "explorer.erreur_dn_manquant"))
		return
	}

	langue := s.langue(r)
	user, err := pool.UserGet(r.Context(), dn)
	if err == nil {
		d := s.donnees(r, Tr(langue, "explorer.details_title"))
		d.ExplorerUser = user
		d.LDAPFallback = sess.LDAPFallback
		s.rendreFragment(w, http.StatusOK, "explorer_details.html", d)
		return
	}

	group, err := pool.GroupGet(r.Context(), dn)
	if err == nil {
		d := s.donnees(r, Tr(langue, "explorer.details_title"))
		d.ExplorerGroup = group
		d.LDAPFallback = sess.LDAPFallback
		s.rendreFragment(w, http.StatusOK, "explorer_details.html", d)
		return
	}

	// Ni utilisateur ni groupe : peut-être une OU (cas des résultats de
	// recherche qui incluent les OU).
	ou, err := pool.OUGet(r.Context(), dn)
	if err == nil {
		d := s.donnees(r, Tr(langue, "explorer.details_title"))
		d.ExplorerOU = ou
		d.LDAPFallback = sess.LDAPFallback
		s.rendreFragment(w, http.StatusOK, "explorer_details.html", d)
		return
	}

	s.erreur(w, Tr(langue, "explorer.erreur_dn_manquant")+": "+dn)
}

// ─── Page Export ───────────────────────────────────────────────────────

func (s *Server) pageExport(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "export.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur

	// Pré-remplissage OU + liste de premier niveau pour le datalist
	// (meilleure chance que la saisie libre, sans dépendre d'un arbre complet).
	d.ExportOU = "OU=Eleves," + pool.BaseDN()
	if ous, err := pool.OUSearch(r.Context(), pool.BaseDN(), ""); err == nil {
		d.ExportOUs = ous
	}
	s.rendre(w, http.StatusOK, "export.html", d)
}

// champsExport convertit les valeurs de formulaire en champs d'export valides.
// Le mot de passe et le QR code sont écartés ici : le premier n'est jamais
// lisible depuis l'AD (attribut en écriture seule), le second n'a de sens que
// dans un PDF (exportPDF l'ajoute explicitement).
func champsExport(valeurs, defauts []string) []ad.ExportField {
	permis := map[ad.ExportField]bool{}
	for _, f := range ad.AllExportFields {
		permis[f] = true
	}
	var out []ad.ExportField
	for _, v := range valeurs {
		f := ad.ExportField(v)
		if permis[f] && f != ad.FieldPassword && f != ad.FieldQRCode {
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		for _, d := range defauts {
			out = append(out, ad.ExportField(d))
		}
	}
	return out
}

// chercheUsersExport charge les utilisateurs d'une OU (sous-OU incluses).
func chercheUsersExport(r *http.Request, pool *ad.Pool, ou string) ([]*ad.User, error) {
	return pool.UserSearch(r.Context(), ou,
		"(&(objectClass=user)(objectCategory=person))",
		[]string{"dn", "sAMAccountName", "displayName", "givenName", "sn", "mail",
			"ou", "userPrincipalName", "description", "title", "department",
			"employeeID", "telephoneNumber", "mobile"},
		0)
}

// journaliseExport écrit une entrée de traçabilité pour un export généré.
func (s *Server) journaliseExport(sess *session, action, ou string, nb, nbChamps int) {
	if s.journal == nil {
		return
	}
	entree := nouvelleEntree(action, sess.Utilisateur, "succes",
		fmt.Sprintf("OU: %s, %d utilisateurs, %d champs", ou, nb, nbChamps))
	entree.Utilisateur = sess.Utilisateur
	entree.Domaine = sess.Domaine
	_ = s.journal.Record(entree)
}

func (s *Server) exportCSV(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}
	langue := s.langue(r)

	ou := strings.TrimSpace(r.FormValue("ou"))
	if ou == "" {
		s.erreur(w, Tr(langue, "export.ou_requise"))
		return
	}
	fields := champsExport(r.Form["champs"], []string{"sAMAccountName", "displayName", "mail"})

	sep := ';'
	switch r.FormValue("separateur") {
	case ",":
		sep = ','
	case "tab":
		sep = '\t'
	}

	users, err := chercheUsersExport(r, pool, ou)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
		return
	}

	params := ad.ExportParams{
		Format:        ad.FormatCSV,
		Fields:        fields,
		IncludeHeader: r.FormValue("en_tete") != "",
		Separator:     sep,
		Users:         users,
		Title:         "Export EduSync AD",
	}

	data, err := pool.ExportCSV(r.Context(), params)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.export")+": "+err.Error())
		return
	}
	s.journaliseExport(sess, "export_csv", ou, len(users), len(fields))

	nom := "export_" + time.Now().Format("20060102-1504") + ".csv"
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+nom+`"`)
	// BOM UTF-8 : Excel (Windows) ouvre alors les accents correctement.
	_, _ = w.Write([]byte("\xEF\xBB\xBF"))
	_, _ = w.Write(data)
}

func (s *Server) exportPDF(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}
	langue := s.langue(r)

	ou := strings.TrimSpace(r.FormValue("ou"))
	if ou == "" {
		s.erreur(w, Tr(langue, "export.ou_requise"))
		return
	}
	template := ad.LabelTemplate(r.FormValue("template"))
	if template == "" {
		template = ad.LabelL7160
	}
	fields := champsExport(r.Form["champs"], []string{"displayName", "sAMAccountName", "mail", "ou"})

	users, err := chercheUsersExport(r, pool, ou)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
		return
	}

	params := ad.ExportParams{
		Format:   ad.FormatPDF,
		Template: template,
		Fields:   fields,
		Users:    users,
		Title:    Tr(langue, "export.pdf_titre"),
	}
	// QR code d'identifiant (optionnelle) : contenu = UPN, sinon SAM.
	if r.FormValue("qrcode") != "" {
		fields = append(fields, ad.FieldQRCode)
		params.Fields = fields
		params.QRCodeData = func(u *ad.User) string {
			if u.UserPrincipalName != "" {
				return u.UserPrincipalName
			}
			return u.SAMAccountName
		}
	}

	data, err := pool.ExportLabelsPDF(r.Context(), params)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.export")+": "+err.Error())
		return
	}
	s.journaliseExport(sess, "export_etiquettes", ou, len(users), len(fields))

	nom := "etiquettes_" + time.Now().Format("20060102-1504") + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+nom+`"`)
	_, _ = w.Write(data)
}

// exportTrombinoscope génère un PDF trombinoscope (grille de photos+noms).
func (s *Server) exportTrombinoscope(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}
	langue := s.langue(r)

	ou := strings.TrimSpace(r.FormValue("ou"))
	if ou == "" {
		s.erreur(w, Tr(langue, "export.ou_requise"))
		return
	}
	titre := strings.TrimSpace(r.FormValue("titre"))
	if titre == "" {
		titre = "Trombinoscope"
	}

	users, err := chercheUsersExport(r, pool, ou)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.connexion_ad")+": "+err.Error())
		return
	}

	params := ad.ExportParams{
		Format:         ad.FormatPDF,
		Fields:         []ad.ExportField{ad.FieldDisplayName, ad.FieldSAMAccountName},
		Users:          users,
		Title:          titre,
		PhotoAttribute: "thumbnailPhoto",
	}

	data, err := pool.ExportTrombinoscope(r.Context(), params)
	if err != nil {
		s.erreur(w, Tr(langue, "erreur.export")+": "+err.Error())
		return
	}
	s.journaliseExport(sess, "export_trombinoscope", ou, len(users), len(params.Fields))

	nom := "trombinoscope_" + time.Now().Format("20060102-1504") + ".pdf"
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", `attachment; filename="`+nom+`"`)
	_, _ = w.Write(data)
}

// ─── Page Audit ────────────────────────────────────────────────────────

func (s *Server) pageAudit(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	d := s.donnees(r, Tr(s.langue(r), "audit.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur

	if s.journal != nil {
		entries, _ := s.journal.List(audit.Filter{Limit: 50})
		d.Actions = entries
	}
	s.rendre(w, http.StatusOK, "audit.html", d)
}

func (s *Server) auditFilter(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	f := audit.Filter{
		From:       strings.TrimSpace(r.FormValue("date_debut")),
		To:         strings.TrimSpace(r.FormValue("date_fin")),
		ActionType: strings.TrimSpace(r.FormValue("type_action")),
		Resultat:   strings.TrimSpace(r.FormValue("resultat")),
		Operateur:  strings.TrimSpace(r.FormValue("operateur")),
		Recherche:  strings.TrimSpace(r.FormValue("recherche")),
		Limit:      200,
	}

	entries, err := s.journal.List(f)
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.journal")+": "+err.Error())
		return
	}

	d := s.donnees(r, Tr(s.langue(r), "audit.titre"))
	d.Actions = entries
	d.LDAPFallback = sess.LDAPFallback
	s.rendreFragment(w, http.StatusOK, "audit_results.html", d)
}

// updaterAPIURLDefault est l'URL par défaut de l'API GitHub releases.
const updaterAPIURLDefault = "https://api.github.com/repos/estemobs/EduSync-AD/releases/latest"

// updaterInfoJSON est la structure de réponse JSON pour /app/updater/check.
type updaterInfoJSON struct {
	Version      string `json:"version"`
	Current      string `json:"current"`
	DownloadURL  string `json:"download_url"`
	ChecksumURL  string `json:"checksum_url"`
	ReleaseNotes string `json:"release_notes,omitempty"`
	AJour        bool   `json:"a_jour"`
}

// pageUpdaterCheck vérifie s'il existe une mise à jour et retourne un JSON.
func (s *Server) pageUpdaterCheck(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	apiURL := s.opts.UpdaterAPIURL
	if apiURL == "" {
		apiURL = updaterAPIURLDefault
	}

	info, err := updater.CheckForUpdate(r.Context(), apiURL, s.opts.Version)
	if err != nil || info == nil {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(updaterInfoJSON{AJour: true})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(updaterInfoJSON{
		Version:      info.Version,
		Current:      info.Current,
		DownloadURL:  info.DownloadURL,
		ChecksumURL:  info.ChecksumURL,
		ReleaseNotes: info.ReleaseNotes,
		AJour:        false,
	})
}

// pageUpdaterInstall télécharge et prépare l'installation de la mise à jour.
// finalize n'est pas exécuté ici : l'appelant (UI) doit confirmer explicitement
// avant d'appeler /app/updater/finalize — voir la note de sécurité dans
// internal/updater (le lancement est différé après confirmation utilisateur).
func (s *Server) pageUpdaterInstall(w http.ResponseWriter, r *http.Request, sess *session, pool *ad.Pool) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	downloadURL := strings.TrimSpace(r.FormValue("download_url"))
	checksumURL := strings.TrimSpace(r.FormValue("checksum_url"))
	if downloadURL == "" {
		s.erreur(w, Tr(s.langue(r), "erreur.url_manquante"))
		return
	}

	ok, finalize, err := updater.DownloadAndInstall(r.Context(), downloadURL, checksumURL)
	if err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.telechargement")+": "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"erreur": Tr(s.langue(r), "updater.echec_preparation")})
		return
	}

	// Le clic sur "Installer" constitue la confirmation explicite de
	// l'utilisateur : on lance la finalisation (installateur Windows /
	// remplacement + relance Linux).
	if finalize != nil {
		go func() {
			_ = finalize()
		}()
	}

	_ = json.NewEncoder(w).Encode(map[string]string{"ok": Tr(s.langue(r), "updater.installation_lancee")})
}

// ─── Progression / annulation (A12) ───────────────────────────────────

func (s *Server) pageProgress(w http.ResponseWriter, r *http.Request, sess *session) {
	d := s.donnees(r, Tr(s.langue(r), "progress.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	s.rendre(w, http.StatusOK, "progress.html", d)
}

func (s *Server) progressList(w http.ResponseWriter, r *http.Request, sess *session) {
	liste := s.progress.Liste()
	if liste == nil {
		liste = []*progress.Operation{}
	}
	d := s.donnees(r, "")
	d.Operations = liste
	s.rendreFragment(w, http.StatusOK, "progress_list.html", d)
}

func (s *Server) progressCancel(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	id := strings.TrimSpace(r.FormValue("id"))
	if id == "" {
		s.erreur(w, Tr(s.langue(r), "erreur.id_manquant"))
		return
	}

	if s.progress.Annuler(id) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"ok": "annule"})
		return
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]string{"erreur": Tr(s.langue(r), "progress.introuvable")})
}

// nouvelOperation enregistre une opération de masse et retourne son ID
// ainsi qu'un contexte annulable. Le serveur suit la progression via ce ID.
func (s *Server) nouvelOperation(typeOp string) (string, context.Context) {
	return s.progress.NouvelID(typeOp)
}

// majProgression met à jour la progression d'une opération. Retourne faux
// si l'opération n'existe plus ou a été annulée.
func (s *Server) majProgression(id string, actuel int, message string) bool {
	return s.progress.MiseAJour(id, actuel, message)
}

// terminerOperation marque une opération comme terminée.
func (s *Server) terminerOperation(id string, succes bool, message, erreur string) {
	s.progress.Terminer(id, succes, message, erreur)
}

// ─── Rendu simulation utilisateurs ────────────────────────────────────

func (s *Server) rendreSimulationUsers(w http.ResponseWriter, langue string, users []models.GeneratedUser) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<table class="w-full text-sm"><thead><tr class="bg-slate-50 dark:bg-slate-800"><th class="px-4 py-2 text-left">Prénom</th><th class="px-4 py-2 text-left">Nom</th><th class="px-4 py-2 text-left">Identifiant</th><th class="px-4 py-2 text-left">OU cible</th><th class="px-4 py-2 text-left">Mot de passe</th><th class="px-4 py-2 text-left">Erreur</th></tr></thead><tbody>`)
	for _, u := range users {
		b.WriteString(fmt.Sprintf(`<tr><td class="px-4 py-2">%s</td><td class="px-4 py-2">%s</td><td class="px-4 py-2 font-mono">%s</td><td class="px-4 py-2">%s</td><td class="px-4 py-2 font-mono">%s</td><td class="px-4 py-2 text-red-600">%s</td></tr>`,
			u.Source.Prenom, u.Source.Nom, u.Identifiant, u.OUCible, u.MotDePasse, u.Erreur))
	}
	b.WriteString(`</tbody></table>`)
	w.Write([]byte(b.String()))
}
