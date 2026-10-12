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
	"crypto/rand"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
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
}

// PageData est le modèle transmis aux templates.
type PageData struct {
	Titre    string
	Langue   string
	Theme    string
	Version  string
	Connecte bool

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
}

type session struct {
	Utilisateur string
	Domaine     string
	Controleur  string
	CreeLe      time.Time
}

// Server implémente http.Handler.
type Server struct {
	pages   map[string]*template.Template
	mux     *http.ServeMux
	journal *audit.Log
	opts    Options

	mu       sync.Mutex
	sessions map[string]*session
	adPools  map[string]*ad.Pool // clé = sessionID
}

// New prépare le serveur d'interface (templates, assets, journal).
func New(opts Options) (*Server, error) {
	journal, err := audit.Open(opts.AuditPath)
	if err != nil {
		return nil, fmt.Errorf("ui: ouverture du journal: %w", err)
	}

	base, err := template.New("layout.html").
		Funcs(template.FuncMap{"tr": Tr, "horodatage": horodatage}).
		ParseFS(web.FS, "templates/layout.html")
	if err != nil {
		_ = journal.Close()
		return nil, fmt.Errorf("ui: lecture du gabarit layout: %w", err)
	}

	pages := make(map[string]*template.Template, 2)
	for _, nom := range []string{"login.html", "dashboard.html"} {
		page, err := base.Clone()
		if err != nil {
			_ = journal.Close()
			return nil, fmt.Errorf("ui: duplication du gabarit: %w", err)
		}
		if _, err := page.ParseFS(web.FS, "templates/"+nom); err != nil {
			_ = journal.Close()
			return nil, fmt.Errorf("ui: lecture du gabarit %s: %w", nom, err)
		}
		pages[nom] = page
	}

	s := &Server{
		pages:    pages,
		journal:  journal,
		opts:     opts,
		sessions: map[string]*session{},
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

	mux.HandleFunc("GET /app/explorer", s.requireAuth(s.pageExplorer))
	mux.HandleFunc("POST /app/explorer/search", s.requireAuth(s.explorerSearch))
	mux.HandleFunc("POST /app/explorer/tree", s.requireAuth(s.explorerTree))

	mux.HandleFunc("GET /app/export", s.requireAuth(s.pageExport))
	mux.HandleFunc("POST /app/export/csv", s.requireAuth(s.exportCSV))
	mux.HandleFunc("POST /app/export/pdf", s.requireAuth(s.exportPDF))

	// Audit / Journal
	mux.HandleFunc("GET /app/audit", s.requireAuth(s.pageAudit))
	mux.HandleFunc("POST /app/audit/filter", s.requireAuth(s.auditFilter))

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

// connexion ouvre une session administrateur.
//
// Phase 0 : la liaison LDAP n'est pas encore câblée (pool de connexions =
// étape suivante de la roadmap) ; on valide et on journalise l'intention de
// connexion, ce qui permet de tester toute la chaîne UI → journal.
func (s *Server) connexion(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(s.langue(r), "erreur.formulaire"))
		return
	}

	langue := s.langue(r)
	domaine := strings.TrimSpace(r.PostFormValue("domaine"))
	controleur := strings.TrimSpace(r.PostFormValue("controleur"))
	utilisateur := strings.TrimSpace(r.PostFormValue("utilisateur"))

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

	sid := nouveauJeton()
	s.mu.Lock()
	s.sessions[sid] = &session{
		Utilisateur: utilisateur,
		Domaine:     domaine,
		Controleur:  controleur,
		CreeLe:      time.Now(),
	}
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
		entree := nouvelleEntree(
			"connexion", utilisateur, "succes",
			fmt.Sprintf("Domaine %s, contrôleur %s", domaine, controleur),
		)
		entree.Utilisateur = utilisateur
		entree.Domaine = domaine
		_ = s.journal.Record(entree)
	}

	rediriger(w, r, "/app")
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
	d.StatutAD = Tr(langue, "statut.ad")

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

	if pool, ok := s.adPools[sess.Utilisateur+"@"+sess.Domaine]; ok {
		return pool, nil
	}

	pool, err := ad.NewPool(ad.Config{
		Domain:        sess.Domaine,
		Controller:    sess.Controleur,
		Port:          636,
		UseLDAPS:      true,
		BindDN:        sess.Utilisateur,
		BindPassword:  "", // TODO: récupérer depuis config sécurisée
		BaseDN:        "",
		TLSSkipVerify: true, // Phase 0 : certificats auto-signés
		Timeout:       10 * time.Second,
		MaxConns:      10,
	})
	if err != nil {
		return nil, err
	}
	s.adPools[sess.Utilisateur+"@"+sess.Domaine] = pool
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

// rendre exécute un gabarit de page.
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
