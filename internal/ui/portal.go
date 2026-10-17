package ui

import (
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/mailer"
	"github.com/estemobs/EduSync-AD/internal/portal"
)

// ─── Portail auto-service (M27) ───────────────────────────────────────────

// portailEnCours regroupe le serveur HTTP du portail et la base qu'il utilise :
// la base doit rester ouverte tant que le serveur écoute.
type portailEnCours struct {
	serveur *portal.Serveur
	store   *portal.Store
}

// cheminConfigPortail retourne le chemin de portail.json.
func (s *Server) cheminConfigPortail() string {
	if s.opts.ConfigPath != "" {
		return filepath.Join(s.dossierConfig(), portal.FichierConfig)
	}
	if defaut, err := portal.CheminConfigDefaut(); err == nil {
		return defaut
	}
	return filepath.Join(s.dossierConfig(), portal.FichierConfig)
}

// cheminBasePortail retourne le chemin de portail.db.
func (s *Server) cheminBasePortail() string {
	if s.opts.ConfigPath != "" {
		return filepath.Join(s.dossierConfig(), portal.FichierBase)
	}
	if defaut, err := portal.CheminBaseDefaut(); err == nil {
		return defaut
	}
	return filepath.Join(s.dossierConfig(), portal.FichierBase)
}

// servicePortail assemble le service du portail pour la session courante.
// `annuaire` vaut false pour les actions hors ligne (configuration, refus).
func (s *Server) servicePortail(sess *session, store *portal.Store, annuaire bool) (*portal.Service, error) {
	appConfig, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		return nil, err
	}
	cheminJournal := s.opts.AuditPath
	if cheminJournal == "" {
		if cheminJournal, err = audit.DefaultPath(); err != nil {
			return nil, err
		}
	}

	var annuaireAD portal.Annuaire
	domaine := ""
	if annuaire {
		pool, err := s.getPool(sess)
		if err != nil {
			return nil, err
		}
		annuaireAD = &portal.PoolAnnuaire{Pool: pool}
		domaine = sess.Domaine
	}

	service := portal.NouveauService(portal.ChargerConfig(s.cheminConfigPortail()),
		appConfig, annuaireAD, store, domaine, cheminJournal)
	// Le portail réutilise la configuration SMTP commune (comme core/mailer.py).
	service.SMTP = mailer.ChargerConfig("", "")
	return service, nil
}

// etatPortail retourne l'état du serveur du portail (écoute et adresse).
func (s *Server) etatPortail() (bool, string) {
	s.portailMu.Lock()
	defer s.portailMu.Unlock()
	if s.portail == nil {
		return false, ""
	}
	return s.portail.serveur.EnEcoute(), s.portail.serveur.URL()
}

// fermerPortail arrête le serveur du portail et libère sa base.
func (s *Server) fermerPortail() {
	s.portailMu.Lock()
	defer s.portailMu.Unlock()
	if s.portail == nil {
		return
	}
	s.portail.serveur.Arreter()
	_ = s.portail.store.Fermer()
	s.portail = nil
}

func (s *Server) pagePortail(w http.ResponseWriter, r *http.Request, sess *session) {
	d := s.donnees(r, Tr(s.langue(r), "portal.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur
	d.PortailConfig = portal.ChargerConfig(s.cheminConfigPortail())
	d.PortailCheminConfig = s.cheminConfigPortail()
	d.PortailCheminBase = s.cheminBasePortail()
	d.PortailEnEcoute, d.PortailURL = s.etatPortail()

	if erreurs := d.PortailConfig.Valider(); len(erreurs) > 0 {
		d.Erreurs = append(d.Erreurs, erreurs...)
	}
	store, err := portal.OuvrirStore(s.cheminBasePortail())
	if err != nil {
		d.Erreurs = append(d.Erreurs, err.Error())
	} else {
		if demandes, err := store.ListerDemandes(portal.StatutEnAttente); err == nil {
			d.PortailDemandes = demandes
		} else {
			d.Erreurs = append(d.Erreurs, err.Error())
		}
		_ = store.Fermer()
	}
	s.rendre(w, http.StatusOK, "portal.html", d)
}

// portailConfigForm enregistre les réglages ; en cas d'erreur, la page est
// réaffichée avec les messages et les valeurs saisies.
func (s *Server) portailConfigForm(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(langue, "erreur.formulaire"))
		return
	}

	cfg := portal.ChargerConfig(s.cheminConfigPortail())
	cfg.Actif = valeurCaseACocher(r.FormValue("actif"))
	cfg.NotificationMail = valeurCaseACocher(r.FormValue("notification_mail"))
	cfg.Hote = strings.TrimSpace(r.FormValue("hote"))
	cfg.Politique = strings.ToLower(strings.TrimSpace(r.FormValue("politique")))
	cfg.OUAccueil = strings.TrimSpace(r.FormValue("ou_accueil"))
	cfg.Titre = strings.TrimSpace(r.FormValue("titre"))
	if port, err := strconv.Atoi(strings.TrimSpace(r.FormValue("port"))); err == nil {
		cfg.Port = port
	} else {
		cfg.Port = 0
	}
	if minutes, err := strconv.Atoi(strings.TrimSpace(r.FormValue("code_minutes"))); err == nil {
		cfg.CodeMinutes = minutes
	} else {
		cfg.CodeMinutes = 0
	}
	if tentatives, err := strconv.Atoi(strings.TrimSpace(r.FormValue("tentatives_max"))); err == nil {
		cfg.TentativesMax = tentatives
	} else {
		cfg.TentativesMax = 0
	}
	if envois, err := strconv.Atoi(strings.TrimSpace(r.FormValue("envois_max"))); err == nil {
		cfg.EnvoisMax = envois
	} else {
		cfg.EnvoisMax = 0
	}

	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		d := s.donnees(r, Tr(langue, "portal.titre"))
		d.Connecte = true
		d.Utilisateur = sess.Utilisateur
		d.Domaine = sess.Domaine
		d.Controleur = sess.Controleur
		d.PortailConfig = cfg
		d.PortailCheminConfig = s.cheminConfigPortail()
		d.PortailCheminBase = s.cheminBasePortail()
		d.PortailEnEcoute, d.PortailURL = s.etatPortail()
		d.Erreurs = append(d.Erreurs, erreurs...)
		s.rendre(w, http.StatusUnprocessableEntity, "portal.html", d)
		return
	}

	if _, err := portal.SauvegarderConfig(cfg, s.cheminConfigPortail()); err != nil {
		s.erreur(w, err.Error())
		return
	}
	http.Redirect(w, r, "/app/portal", http.StatusSeeOther)
}

func (s *Server) portailDemarrer(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := r.ParseForm(); err != nil {
		s.portailMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)

	s.portailMu.Lock()
	defer s.portailMu.Unlock()
	if s.portail != nil && s.portail.serveur.EnEcoute() {
		s.portailMessage(w, Tr(langue, "portal.deja_actif"), false)
		return
	}

	cfg := portal.ChargerConfig(s.cheminConfigPortail())
	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		s.portailMessage(w, strings.Join(erreurs, " ; "), true)
		return
	}
	store, err := portal.OuvrirStore(s.cheminBasePortail())
	if err != nil {
		s.portailMessage(w, err.Error(), true)
		return
	}
	service, err := s.servicePortail(sess, store, true)
	if err != nil {
		_ = store.Fermer()
		s.portailMessage(w, Tr(langue, "portal.erreur_ad"), true)
		return
	}
	serveur := portal.NouveauServeur(service, cfg.Hote, cfg.Port)
	adresse, err := serveur.Demarrer()
	if err != nil {
		_ = store.Fermer()
		s.portailMessage(w, err.Error(), true)
		return
	}
	s.portail = &portailEnCours{serveur: serveur, store: store}
	s.portailMessage(w, fmt.Sprintf(Tr(langue, "portal.demarre"), adresse), false)
}

func (s *Server) portailArreter(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.portailMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	s.fermerPortail()
	s.portailMessage(w, Tr(s.langue(r), "portal.arrete"), false)
}

// portailEtat publie l'état courant du serveur (fragment HTMX).
func (s *Server) portailEtat(w http.ResponseWriter, _ *http.Request, _ *session) {
	enEcoute, adresse := s.etatPortail()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span id="portail-etat" class="text-sm">%s</span>`, portalEtatTexte(enEcoute, adresse))
}

// portalEtatTexte rend l'état du serveur en HTML échappé.
func portalEtatTexte(enEcoute bool, adresse string) string {
	if !enEcoute {
		return `<span class="text-rose-600 dark:text-rose-400">arrêté</span>`
	}
	return `<span class="text-emerald-600 dark:text-emerald-400">en écoute</span> — <a class="font-mono underline" href="` +
		html.EscapeString(adresse) + `">` + html.EscapeString(adresse) + `</a>`
}

func (s *Server) portailApprouver(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := r.ParseForm(); err != nil {
		s.portailMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	store, err := portal.OuvrirStore(s.cheminBasePortail())
	if err != nil {
		s.portailMessage(w, err.Error(), true)
		return
	}
	defer store.Fermer()

	service, err := s.servicePortail(sess, store, true)
	if err != nil {
		s.portailMessage(w, Tr(langue, "portal.erreur_ad"), true)
		return
	}
	infos, err := service.ApprouverDemande(strings.TrimSpace(r.FormValue("id")))
	if err != nil {
		s.portailMessage(w, err.Error(), true)
		return
	}

	// Le mot de passe est conservé dans le coffre local, comme côté Python :
	// l'administrateur peut le réafficher après avoir fermé la page.
	coffre, errCoffre := s.ouvrirCoffreUI()
	if errCoffre != nil {
		s.portailMessage(w, errCoffre.Error(), true)
		return
	}
	if err := coffre.Store(infos["identifiant"], infos["mot_de_passe"]); err != nil {
		_ = coffre.Close()
		s.portailMessage(w, err.Error(), true)
		return
	}
	_ = coffre.Close()

	s.portailCompteFragment(w, langue, infos)
}

func (s *Server) portailRefuser(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.portailMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	store, err := portal.OuvrirStore(s.cheminBasePortail())
	if err != nil {
		s.portailMessage(w, err.Error(), true)
		return
	}
	defer store.Fermer()

	service, err := s.servicePortail(nil, store, false)
	if err != nil {
		s.portailMessage(w, err.Error(), true)
		return
	}
	if err := service.RefuserDemande(strings.TrimSpace(r.FormValue("id")),
		strings.TrimSpace(r.FormValue("note"))); err != nil {
		s.portailMessage(w, err.Error(), true)
		return
	}
	s.portailMessage(w, Tr(langue, "portal.demande_refusee"), false)
}

// portailCompteFragment rend les identifiants du compte créé.
func (s *Server) portailCompteFragment(w http.ResponseWriter, langue string, infos map[string]string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm dark:border-emerald-900 dark:bg-emerald-950/30">`)
	b.WriteString(`<p class="font-semibold">` + Tr(langue, "portal.compte_cree") + `</p><dl class="mt-2 space-y-1">`)
	for _, champ := range []struct{ cle, libelle string }{
		{"identifiant", "portal.mot_de_passe.identifiant"},
		{"mot_de_passe", "portal.mot_de_passe.mdp"},
		{"mail", "portal.mot_de_passe.mail"},
		{"dn", "portal.mot_de_passe.dn"},
	} {
		b.WriteString(fmt.Sprintf(`<div class="flex gap-2"><dt class="w-40 shrink-0 text-slate-500">%s</dt><dd class="font-mono break-all">%s</dd></div>`,
			Tr(langue, champ.libelle), html.EscapeString(infos[champ.cle])))
	}
	b.WriteString(`</dl><p class="mt-2 text-xs text-slate-600 dark:text-slate-300">` +
		Tr(langue, "portal.mot_de_passe.coffre") + `</p></div>`)
	_, _ = w.Write([]byte(b.String()))
}

// portailMessage rend un message d'erreur ou d'information (fragment HTMX).
func (s *Server) portailMessage(w http.ResponseWriter, message string, erreur bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	classe := "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200"
	if erreur {
		w.WriteHeader(http.StatusUnprocessableEntity)
		classe = "border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-200"
	}
	fmt.Fprintf(w, `<div class="rounded-lg border %s p-3 text-sm">%s</div>`, classe, html.EscapeString(message))
}
