package ui

import (
	"fmt"
	"html"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/api"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/mailer"
	"github.com/estemobs/EduSync-AD/internal/portal"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
)

// ─── API REST et webhooks (M28) ───────────────────────────────────────────

// CleAPIVue est la vue d'une clé d'API pour le gabarit : les champs optionnels
// sont rendus sous forme de chaînes (un pointeur afficherait une adresse).
type CleAPIVue struct {
	ID           string
	Nom          string
	Prefixe      string
	Permissions  []string
	Active       bool
	Statut       string
	DernierUsage string
}

// WebhookVue est la vue d'un webhook pour le gabarit.
type WebhookVue struct {
	ID              string
	Nom             string
	URL             string
	Evenements      []string
	Actif           bool
	PolitiqueRetrai string
	RetraitsMax     int
}

// LivraisonVue est la vue d'une livraison pour le gabarit.
type LivraisonVue struct {
	CommenceLe string
	WebhookID  string
	Evenement  string
	Tentative  int
	Statut     string
}

// vueCles convertit les clés pour l'affichage.
func vueCles(cles []api.CleAPIInfo, langue string) []CleAPIVue {
	vues := make([]CleAPIVue, 0, len(cles))
	for _, cle := range cles {
		vue := CleAPIVue{
			ID: cle.ID, Nom: cle.Nom, Prefixe: cle.Prefixe,
			Permissions: cle.Permissions, Active: cle.Active,
			Statut: Tr(langue, "api.statut_revoquee"),
		}
		if cle.Active {
			vue.Statut = Tr(langue, "api.statut_active")
		}
		if cle.DernierUsage != nil {
			vue.DernierUsage = *cle.DernierUsage
		}
		vues = append(vues, vue)
	}
	return vues
}

// vueWebhooks convertit les webhooks pour l'affichage.
func vueWebhooks(liste []webhooks.WebhookConfig) []WebhookVue {
	vues := make([]WebhookVue, 0, len(liste))
	for _, webhook := range liste {
		vues = append(vues, WebhookVue{
			ID: webhook.ID, Nom: webhook.Nom, URL: webhook.URL,
			Evenements: webhook.Evenements, Actif: webhook.Actif,
			PolitiqueRetrai: webhook.PolitiqueRetrai, RetraitsMax: webhook.RetraitsMax,
		})
	}
	return vues
}

// vueLivraisons convertit les livraisons pour l'affichage.
func vueLivraisons(liste []webhooks.Livraison) []LivraisonVue {
	vues := make([]LivraisonVue, 0, len(liste))
	for _, livraison := range liste {
		vue := LivraisonVue{
			CommenceLe: livraison.CommenceLe, WebhookID: livraison.WebhookID,
			Evenement: livraison.Evenement, Tentative: livraison.Tentative,
		}
		switch {
		case livraison.Statut != nil:
			vue.Statut = fmt.Sprintf("HTTP %d", *livraison.Statut)
		case livraison.Erreur != nil:
			vue.Statut = *livraison.Erreur
		}
		vues = append(vues, vue)
	}
	return vues
}

// apiEnCours regroupe le serveur de l'API et les ressources qu'il utilise : le
// magasin des webhooks et la base du portail doivent rester ouverts tant que
// le serveur écoute.
type apiEnCours struct {
	serveur    *api.Serveur
	webhooks   *webhooks.Store
	portail    *portal.Store
	dispatcher *webhooks.Dispatcher
}

// cheminConfigAPI retourne le chemin de api.json.
func (s *Server) cheminConfigAPI() string {
	if s.opts.ConfigPath != "" {
		return filepath.Join(s.dossierConfig(), api.NomFichier)
	}
	if defaut, err := api.CheminConfigDefaut(); err == nil {
		return defaut
	}
	return filepath.Join(s.dossierConfig(), api.NomFichier)
}

// cheminClesAPI retourne le chemin de api_keys.json.
func (s *Server) cheminClesAPI() string {
	if s.opts.ConfigPath != "" {
		return filepath.Join(s.dossierConfig(), api.NomFichierCles)
	}
	if defaut, err := api.CheminClesDefaut(); err == nil {
		return defaut
	}
	return filepath.Join(s.dossierConfig(), api.NomFichierCles)
}

// cheminWebhooks retourne le chemin de webhooks.json.
func (s *Server) cheminWebhooks() string {
	if s.opts.ConfigPath != "" {
		return filepath.Join(s.dossierConfig(), webhooks.NomFichier)
	}
	if defaut, err := webhooks.CheminConfigDefaut(); err == nil {
		return defaut
	}
	return filepath.Join(s.dossierConfig(), webhooks.NomFichier)
}

// cheminLivraisons retourne le chemin de webhook_deliveries.db.
func (s *Server) cheminLivraisons() string {
	if s.opts.ConfigPath != "" {
		return filepath.Join(s.dossierConfig(), webhooks.NomFichierLivraisons)
	}
	if defaut, err := webhooks.CheminLivraisonsDefaut(); err == nil {
		return defaut
	}
	return filepath.Join(s.dossierConfig(), webhooks.NomFichierLivraisons)
}

// etatAPI retourne l'état du serveur de l'API (écoute et adresse).
func (s *Server) etatAPI() (bool, string) {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if s.api == nil {
		return false, ""
	}
	return s.api.serveur.EnEcoute(), s.api.serveur.URL()
}

// fermerAPI arrête le serveur de l'API et libère ses ressources.
func (s *Server) fermerAPI() {
	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if s.api == nil {
		return
	}
	_ = s.api.serveur.Arreter()
	if s.api.dispatcher != nil {
		s.api.dispatcher.Arreter()
	}
	if s.api.webhooks != nil {
		_ = s.api.webhooks.Fermer()
	}
	if s.api.portail != nil {
		_ = s.api.portail.Fermer()
	}
	s.api = nil
}

// dependancesAPI assemble les dépendances du serveur de l'API.
//
// L'annuaire n'est branché que si une connexion AD est disponible pour la
// session : sans elle, les routes AD répondent « annuaire indisponible » et
// les autres (portail, webhooks, journal, métadonnées) restent utilisables.
func (s *Server) dependancesAPI(sess *session, langue string, magasinWebhooks *webhooks.Store,
	storePortail *portal.Store) (api.Dependances, string) {
	appConfig, err := config.Load(s.opts.ConfigPath)
	if err != nil {
		appConfig = config.DefaultAppConfig()
	}

	deps := api.Dependances{
		Webhooks:   magasinWebhooks,
		Dispatcher: webhooks.NouveauDispatcher(magasinWebhooks),
		Journal:    s.journal,
		Config:     appConfig,
		Version:    s.opts.Version,
	}
	avertissement := ""
	if pool, err := s.getPool(sess); err != nil {
		avertissement = Tr(langue, "api.avertissement_ad")
	} else {
		deps.Annuaire = api.NewAnnuaireAD(pool)
	}

	if storePortail != nil {
		cheminJournal := s.opts.AuditPath
		if cheminJournal == "" {
			cheminJournal, _ = audit.DefaultPath()
		}
		service := portal.NouveauService(portal.ChargerConfig(s.cheminConfigPortail()),
			appConfig, nil, storePortail, sess.Domaine, cheminJournal)
		service.SMTP = mailer.ChargerConfig("", "")
		deps.Portail = service
	}
	return deps, avertissement
}

func (s *Server) pageAPI(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	d := s.donnees(r, Tr(langue, "api.titre"))
	d.Connecte = true
	d.Utilisateur = sess.Utilisateur
	d.Domaine = sess.Domaine
	d.Controleur = sess.Controleur

	d.APIConfig = api.ChargerConfig(s.cheminConfigAPI())
	d.APICheminConfig = s.cheminConfigAPI()
	d.APICheminCles = s.cheminClesAPI()
	d.APICheminWebhooks = s.cheminWebhooks()
	d.APICheminLivraisons = s.cheminLivraisons()
	d.APIPermissions = api.PermissionsStandards
	d.APIEvenements = webhooks.EvenementsTries()
	d.APIEnEcoute, d.APIURL = s.etatAPI()
	d.APIRoutes = routesAffichees()

	if erreurs := d.APIConfig.Valider(); len(erreurs) > 0 {
		d.Erreurs = append(d.Erreurs, erreurs...)
	}

	magasin, err := api.OuvrirMagasinCles(s.cheminClesAPI())
	if err != nil {
		d.Erreurs = append(d.Erreurs, err.Error())
	} else {
		d.APICles = vueCles(magasin.Lister(true, time.Now().UTC()), langue)
	}

	store, err := webhooks.OuvrirStore(s.cheminWebhooks(), s.cheminLivraisons())
	if err != nil {
		d.Erreurs = append(d.Erreurs, err.Error())
	} else {
		defer func() { _ = store.Fermer() }()
		d.APIWebhooks = vueWebhooks(store.Lister(false))
		if livraisons, err := store.Livraisons("", 20); err == nil {
			d.APILivraisons = vueLivraisons(livraisons)
		} else {
			d.Erreurs = append(d.Erreurs, err.Error())
		}
		if total, err := store.CompterLivraisons(); err == nil {
			d.APINombreLivraisons = total
		}
	}
	s.rendre(w, http.StatusOK, "api.html", d)
}

// routesAffichees retourne les routes de l'API sans doublon de préfixe.
func routesAffichees() []api.Route {
	toutes := api.NouveauRouteur()
	api.EnregistrerRoutes(toutes, api.Dependances{})
	var retenues []api.Route
	for _, route := range toutes.Routes() {
		if strings.HasPrefix(route.Motif, api.PrefixeAPI) {
			retenues = append(retenues, route)
		}
	}
	return retenues
}

// apiConfigForm enregistre les réglages ; en cas d'erreur, la page est
// réaffichée avec les messages et les valeurs saisies.
func (s *Server) apiConfigForm(w http.ResponseWriter, r *http.Request, sess *session) {
	langue := s.langue(r)
	if err := r.ParseForm(); err != nil {
		s.erreur(w, Tr(langue, "erreur.formulaire"))
		return
	}

	cfg := api.ChargerConfig(s.cheminConfigAPI())
	cfg.Actif = valeurCaseACocher(r.FormValue("actif"))
	cfg.Hote = strings.TrimSpace(r.FormValue("hote"))
	cfg.Titre = strings.TrimSpace(r.FormValue("titre"))
	if port, err := strconv.Atoi(strings.TrimSpace(r.FormValue("port"))); err == nil {
		cfg.Port = port
	} else {
		cfg.Port = 0
	}
	if limite, err := strconv.Atoi(strings.TrimSpace(r.FormValue("rate_limit_rpm"))); err == nil {
		cfg.LimiteRPM = limite
	} else {
		cfg.LimiteRPM = -1
	}
	origines := []string{}
	for _, origine := range strings.Split(r.FormValue("cors_origins"), ",") {
		if origine = strings.TrimSpace(origine); origine != "" {
			origines = append(origines, origine)
		}
	}
	cfg.CORSOrigines = origines

	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		d := s.donnees(r, Tr(langue, "api.titre"))
		d.Connecte = true
		d.Utilisateur = sess.Utilisateur
		d.Domaine = sess.Domaine
		d.Controleur = sess.Controleur
		d.APIConfig = cfg
		d.APICheminConfig = s.cheminConfigAPI()
		d.APICheminCles = s.cheminClesAPI()
		d.APICheminWebhooks = s.cheminWebhooks()
		d.APICheminLivraisons = s.cheminLivraisons()
		d.APIPermissions = api.PermissionsStandards
		d.APIEvenements = webhooks.EvenementsTries()
		d.APIEnEcoute, d.APIURL = s.etatAPI()
		d.APIRoutes = routesAffichees()
		d.Erreurs = append(d.Erreurs, erreurs...)
		s.rendre(w, http.StatusUnprocessableEntity, "api.html", d)
		return
	}

	if _, err := api.SauvegarderConfig(cfg, s.cheminConfigAPI()); err != nil {
		s.erreur(w, err.Error())
		return
	}
	http.Redirect(w, r, "/app/api", http.StatusSeeOther)
}

func (s *Server) apiDemarrer(w http.ResponseWriter, r *http.Request, sess *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)

	s.apiMu.Lock()
	defer s.apiMu.Unlock()
	if s.api != nil && s.api.serveur.EnEcoute() {
		s.apiMessage(w, Tr(langue, "api.deja_actif"), false)
		return
	}

	cfg := api.ChargerConfig(s.cheminConfigAPI())
	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		s.apiMessage(w, strings.Join(erreurs, " ; "), true)
		return
	}
	magasin, err := api.OuvrirMagasinCles(s.cheminClesAPI())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	if len(magasin.Lister(true, time.Now().UTC())) == 0 {
		s.apiMessage(w, Tr(langue, "api.aucune_cle"), true)
		return
	}

	store, err := webhooks.OuvrirStore(s.cheminWebhooks(), s.cheminLivraisons())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	storePortail, err := portal.OuvrirStore(s.cheminBasePortail())
	if err != nil {
		_ = store.Fermer()
		s.apiMessage(w, err.Error(), true)
		return
	}

	deps, avertissement := s.dependancesAPI(sess, langue, store, storePortail)
	deps.Dispatcher.Demarrer()

	routeur := api.NouveauRouteur()
	api.EnregistrerRoutes(routeur, deps)
	serveur := api.NouveauServeur(cfg, routeur, magasin)
	if err := serveur.Demarrer(); err != nil {
		deps.Dispatcher.Arreter()
		_ = store.Fermer()
		_ = storePortail.Fermer()
		s.apiMessage(w, err.Error(), true)
		return
	}

	s.api = &apiEnCours{serveur: serveur, webhooks: store, portail: storePortail,
		dispatcher: deps.Dispatcher}
	message := fmt.Sprintf(Tr(langue, "api.demarre"), serveur.URL())
	if avertissement != "" {
		message += " " + avertissement
	}
	s.apiMessage(w, message, false)
}

func (s *Server) apiArreter(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	s.fermerAPI()
	s.apiMessage(w, Tr(s.langue(r), "api.arrete"), false)
}

// apiEtat publie l'état courant du serveur (fragment HTMX).
func (s *Server) apiEtat(w http.ResponseWriter, _ *http.Request, _ *session) {
	enEcoute, adresse := s.etatAPI()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<span id="api-etat" class="text-sm">%s</span>`, apiEtatTexte(enEcoute, adresse))
}

// apiEtatTexte rend l'état du serveur en HTML échappé.
func apiEtatTexte(enEcoute bool, adresse string) string {
	if !enEcoute {
		return `<span class="text-rose-600 dark:text-rose-400">arrêté</span>`
	}
	documentation := adresse + "/openapi.json"
	return `<span class="text-emerald-600 dark:text-emerald-400">en écoute</span> — <a class="font-mono underline" href="` +
		html.EscapeString(documentation) + `">` + html.EscapeString(adresse) + `</a>`
}

// apiCreerCle crée une clé d'API et l'affiche une seule fois.
func (s *Server) apiCreerCle(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	magasin, err := api.OuvrirMagasinCles(s.cheminClesAPI())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}

	permis := []string{}
	for _, permission := range r.Form["permissions"] {
		if permission = strings.TrimSpace(permission); permission != "" {
			permis = append(permis, permission)
		}
	}
	if len(permis) == 0 {
		s.apiMessage(w, Tr(langue, "api.erreur_permissions"), true)
		return
	}
	jours := 0
	if valeur, err := strconv.Atoi(strings.TrimSpace(r.FormValue("jours"))); err == nil {
		jours = valeur
	}

	cle, info, err := magasin.Creer(strings.TrimSpace(r.FormValue("nom")), permis, jours,
		time.Now().UTC())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	s.apiCleFragment(w, langue, cle, info)
}

// apiRevoquerCle révoque une clé (elle reste listée, marquée inactive).
func (s *Server) apiRevoquerCle(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	magasin, err := api.OuvrirMagasinCles(s.cheminClesAPI())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	identifiant := strings.TrimSpace(r.FormValue("id"))
	revoque, err := magasin.Revoquer(identifiant)
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	if !revoque {
		s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.cle_inconnue"), identifiant), true)
		return
	}
	s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.cle_revoquee"), identifiant), false)
}

// apiSupprimerCle supprime définitivement une clé.
func (s *Server) apiSupprimerCle(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	magasin, err := api.OuvrirMagasinCles(s.cheminClesAPI())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	identifiant := strings.TrimSpace(r.FormValue("id"))
	supprime, err := magasin.Supprimer(identifiant)
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	if !supprime {
		s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.cle_inconnue"), identifiant), true)
		return
	}
	s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.cle_supprimee"), identifiant), false)
}

// apiAjouterWebhook crée un abonnement (secret chiffré au stockage).
func (s *Server) apiAjouterWebhook(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	noms, adresse := strings.TrimSpace(r.FormValue("nom")), strings.TrimSpace(r.FormValue("url"))
	if noms == "" || adresse == "" {
		s.apiMessage(w, Tr(langue, "api.erreur_webhook"), true)
		return
	}

	store, err := webhooks.OuvrirStore(s.cheminWebhooks(), s.cheminLivraisons())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	defer func() { _ = store.Fermer() }()

	abonnement := webhooks.NouveauWebhook(webhooks.NouvelIdentifiantWebhook(), noms, adresse,
		webhooks.SéparerEvenements(r.FormValue("events")))
	abonnement.Actif = !valeurCaseACocher(r.FormValue("desactive"))
	if politique := strings.TrimSpace(r.FormValue("retry_policy")); politique != "" {
		abonnement.PolitiqueRetrai = politique
	}
	if secret := strings.TrimSpace(r.FormValue("secret")); secret != "" {
		dispatcher := webhooks.NouveauDispatcher(store)
		chiffre, err := dispatcher.ChiffrerSecret(secret)
		if err != nil {
			s.apiMessage(w, err.Error(), true)
			return
		}
		abonnement.Secret = chiffre
	}

	cree, err := store.Creer(abonnement)
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.webhook_ajoute"), cree.ID, cree.Nom), false)
}

// apiSupprimerWebhook supprime un abonnement.
func (s *Server) apiSupprimerWebhook(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	store, err := webhooks.OuvrirStore(s.cheminWebhooks(), s.cheminLivraisons())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	defer func() { _ = store.Fermer() }()

	identifiant := strings.TrimSpace(r.FormValue("id"))
	supprime, err := store.Supprimer(identifiant)
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	if !supprime {
		s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.webhook_inconnu"), identifiant), true)
		return
	}
	s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.webhook_supprime"), identifiant), false)
}

// apiTesterWebhook envoie un événement réel à un abonnement.
func (s *Server) apiTesterWebhook(w http.ResponseWriter, r *http.Request, _ *session) {
	if err := r.ParseForm(); err != nil {
		s.apiMessage(w, Tr(s.langue(r), "erreur.formulaire"), true)
		return
	}
	langue := s.langue(r)
	store, err := webhooks.OuvrirStore(s.cheminWebhooks(), s.cheminLivraisons())
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	defer func() { _ = store.Fermer() }()

	identifiant := strings.TrimSpace(r.FormValue("id"))
	abonnement, err := store.Obtenir(identifiant)
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	if abonnement == nil {
		s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.webhook_inconnu"), identifiant), true)
		return
	}
	evenement := strings.TrimSpace(r.FormValue("event"))
	if evenement == "" {
		evenement = "system.startup"
	}

	dispatcher := webhooks.NouveauDispatcher(store)
	livraison, err := dispatcher.Livrer(*abonnement, evenement,
		map[string]any{"test": true, "source": "interface"}, 0)
	if err != nil {
		s.apiMessage(w, err.Error(), true)
		return
	}
	s.apiMessage(w, fmt.Sprintf(Tr(langue, "api.webhook_test"), livraison.ID,
		livraisonTexte(livraison)), false)
}

// livraisonTexte résume l'issue d'une livraison.
func livraisonTexte(livraison *webhooks.Livraison) string {
	if livraison == nil || livraison.Statut == nil {
		if livraison != nil && livraison.Erreur != nil {
			return *livraison.Erreur
		}
		return "sans réponse"
	}
	if *livraison.Statut >= 200 && *livraison.Statut < 300 {
		return fmt.Sprintf("succès (HTTP %d)", *livraison.Statut)
	}
	return fmt.Sprintf("échec (HTTP %d)", *livraison.Statut)
}

// apiCleFragment rend la clé nouvellement créée (affichée une seule fois).
func (s *Server) apiCleFragment(w http.ResponseWriter, langue, cle string, info api.CleAPIInfo) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	var b strings.Builder
	b.WriteString(`<div class="rounded-xl border border-emerald-200 bg-emerald-50 p-4 text-sm dark:border-emerald-900 dark:bg-emerald-950/30">`)
	b.WriteString(`<p class="font-semibold">` + Tr(langue, "api.cle_creee") + `</p><dl class="mt-2 space-y-1">`)
	for _, champ := range []struct{ libelle, valeur string }{
		{Tr(langue, "api.col_nom"), info.Nom},
		{Tr(langue, "api.col_id"), info.ID},
		{Tr(langue, "api.col_permissions"), strings.Join(info.Permissions, ", ")},
	} {
		b.WriteString(fmt.Sprintf(`<div class="flex gap-2"><dt class="w-40 shrink-0 text-slate-500">%s</dt><dd class="font-mono break-all">%s</dd></div>`,
			champ.libelle, html.EscapeString(champ.valeur)))
	}
	b.WriteString(fmt.Sprintf(`<div class="flex gap-2"><dt class="w-40 shrink-0 text-slate-500">%s</dt><dd class="break-all font-mono font-semibold">%s</dd></div>`,
		Tr(langue, "api.col_cle"), html.EscapeString(cle)))
	b.WriteString(`</dl><p class="mt-2 text-xs text-slate-600 dark:text-slate-300">` +
		Tr(langue, "api.cle_unique") + `</p></div>`)
	_, _ = w.Write([]byte(b.String()))
}

// apiMessage rend un message d'erreur ou d'information (fragment HTMX).
func (s *Server) apiMessage(w http.ResponseWriter, message string, erreur bool) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	classe := "border-emerald-200 bg-emerald-50 text-emerald-800 dark:border-emerald-900 dark:bg-emerald-950/30 dark:text-emerald-200"
	if erreur {
		w.WriteHeader(http.StatusUnprocessableEntity)
		classe = "border-rose-200 bg-rose-50 text-rose-800 dark:border-rose-900 dark:bg-rose-950/30 dark:text-rose-200"
	}
	fmt.Fprintf(w, `<div class="rounded-lg border %s p-3 text-sm">%s</div>`, classe, html.EscapeString(message))
}
