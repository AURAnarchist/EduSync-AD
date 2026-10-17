package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/portal"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
)

// ─── Types exposés ─────────────────────────────────────────────────────────

// Utilisateur est la vue API d'un compte AD.
type Utilisateur struct {
	SAM   string `json:"sam"`
	CN    string `json:"cn"`
	Mail  string `json:"mail"`
	DN    string `json:"dn"`
	UPN   string `json:"upn"`
	OU    string `json:"ou"`
	Actif bool   `json:"actif"`
}

// Groupe est la vue API d'un groupe AD.
type Groupe struct {
	CN          string   `json:"cn"`
	SAM         string   `json:"sam"`
	Mail        string   `json:"mail"`
	DN          string   `json:"dn"`
	Description string   `json:"description"`
	OU          string   `json:"ou"`
	Membres     []string `json:"members,omitempty"`
}

// Unite est la vue API d'une unité d'organisation.
type Unite struct {
	DN          string `json:"dn"`
	Nom         string `json:"name"`
	Description string `json:"description"`
}

// CreationUtilisateur décrit la création d'un compte depuis l'API.
type CreationUtilisateur struct {
	SAM         string
	CN          string
	Mail        string
	OU          string
	Prenom      string
	Nom         string
	MotDePasse  string
	Description string
}

// CreationGroupe décrit la création d'un groupe depuis l'API.
type CreationGroupe struct {
	CN          string
	SAM         string
	Description string
	OU          string
	Type        string
	Portee      string
}

// Annuaire regroupe les opérations exposées par l'API. L'interface découple
// les routes de `internal/ad` : les tests utilisent une doublure, le serveur
// un adaptateur de `*ad.Pool`.
type Annuaire interface {
	ChercherUtilisateurs(ctx context.Context, requete string, limite int) ([]Utilisateur, error)
	ObtenirUtilisateur(ctx context.Context, sam string) (*Utilisateur, error)
	CreerUtilisateur(ctx context.Context, params CreationUtilisateur) (*Utilisateur, error)
	ModifierUtilisateur(ctx context.Context, sam string, champs map[string]string) (*Utilisateur, error)
	SupprimerUtilisateur(ctx context.Context, sam string) error
	ReinitialiserMotDePasse(ctx context.Context, sam, motDePasse string) error
	CreerGroupe(ctx context.Context, params CreationGroupe) (*Groupe, error)
	ChercherGroupes(ctx context.Context, requete string, limite int) ([]Groupe, error)
	ObtenirGroupe(ctx context.Context, cn string) (*Groupe, error)
	CreerOU(ctx context.Context, nom, description, parentDN string) (*Unite, error)
	ChercherOUs(ctx context.Context, requete string, limite int) ([]Unite, error)
	ObtenirOU(ctx context.Context, dn string) (*Unite, error)
	BaseDN() string
}

// LecteurJournal lit le journal d'actions (interface : `*audit.Log`).
type LecteurJournal interface {
	List(filtre audit.Filter) ([]audit.Entry, error)
}

// Dependances regroupe ce dont les routes ont besoin.
type Dependances struct {
	// Routeur est renseigné par `EnregistrerRoutes` (document OpenAPI).
	Routeur *Routeur
	// Annuaire expose l'annuaire (nil = routes AD en erreur explicite).
	Annuaire Annuaire
	// Portail expose les demandes de pré-inscription (nil = routes désactivées).
	Portail Portail
	// Webhooks persiste les abonnements et les livraisons.
	Webhooks *webhooks.Store
	// Dispatcher livre les événements (nil = test de webhook indisponible).
	Dispatcher *webhooks.Dispatcher
	// Journal expose le journal d'actions (nil = route d'audit désactivée).
	Journal LecteurJournal
	// Config fournit les politiques de mot de passe (création de compte).
	Config config.AppConfig
	// Version est la version de l'application renvoyée par /health et /info.
	Version string
}

// Portail expose les demandes de pré-inscription (interface : `*portal.Service`).
type Portail interface {
	ListerDemandes(statut string) ([]portal.Demande, error)
	ApprouverDemande(demandeID string) (map[string]string, error)
	RefuserDemande(demandeID, note string) error
}

// ─── Enregistrement ────────────────────────────────────────────────────────

// EnregistrerRoutes déclare toutes les routes de l'API sur le routeur, avec et
// sans le préfixe `/api/v1` (comme `api_routes.py`).
func EnregistrerRoutes(routeur *Routeur, deps Dependances) {
	// Le document OpenAPI est généré à partir du routeur : il doit être
	// accessible depuis les handlers.
	deps.Routeur = routeur
	prefixes := []string{"", PrefixeAPI}

	enregistrer := func(methode, motif string, handler HandlerAPI) {
		for _, prefixe := range prefixes {
			routeur.Ajouter(methode, prefixe+motif, handler)
		}
	}

	// ── Utilisateurs ──────────────────────────────────────────────────────
	enregistrer(http.MethodGet, "/users", proteger(deps.listerUtilisateurs, "users:read"))
	enregistrer(http.MethodPost, "/users", proteger(deps.creerUtilisateur, "users:write"))
	enregistrer(http.MethodGet, "/users/{sam}", proteger(deps.obtenirUtilisateur, "users:read"))
	enregistrer(http.MethodPatch, "/users/{sam}", proteger(deps.modifierUtilisateur, "users:write"))
	enregistrer(http.MethodDelete, "/users/{sam}", proteger(deps.supprimerUtilisateur, "users:write"))
	enregistrer(http.MethodPost, "/users/{sam}/password",
		proteger(deps.reinitialiserMotDePasse, "users:password"))

	// ── Groupes ───────────────────────────────────────────────────────────
	enregistrer(http.MethodGet, "/groups", proteger(deps.listerGroupes, "groups:read"))
	enregistrer(http.MethodPost, "/groups", proteger(deps.creerGroupe, "groups:write"))
	enregistrer(http.MethodGet, "/groups/{cn}", proteger(deps.obtenirGroupe, "groups:read"))

	// ── Unités d'organisation ─────────────────────────────────────────────
	enregistrer(http.MethodGet, "/ous", proteger(deps.listerOUs, "ous:read"))
	enregistrer(http.MethodPost, "/ous", proteger(deps.creerOU, "ous:write"))
	enregistrer(http.MethodGet, "/ous/{dn}", proteger(deps.obtenirOU, "ous:read"))

	// ── Portail auto-service ──────────────────────────────────────────────
	enregistrer(http.MethodGet, "/portal/requests", proteger(deps.listerDemandes, "portal:read"))
	enregistrer(http.MethodPost, "/portal/requests/{demande_id}/approve",
		proteger(deps.approuverDemande, "portal:write"))
	enregistrer(http.MethodPost, "/portal/requests/{demande_id}/refuse",
		proteger(deps.refuserDemande, "portal:write"))

	// ── Webhooks ──────────────────────────────────────────────────────────
	// `/webhooks/events` est déclaré **avant** `/webhooks/{webhook_id}` :
	// sinon « events » serait interprété comme un identifiant de webhook.
	enregistrer(http.MethodGet, "/webhooks/events", proteger(deps.evenements, "webhooks:read"))
	enregistrer(http.MethodGet, "/webhooks", proteger(deps.listerWebhooks, "webhooks:read"))
	enregistrer(http.MethodPost, "/webhooks", proteger(deps.creerWebhook, "webhooks:write"))
	enregistrer(http.MethodGet, "/webhooks/{webhook_id}",
		proteger(deps.obtenirWebhook, "webhooks:read"))
	enregistrer(http.MethodPatch, "/webhooks/{webhook_id}",
		proteger(deps.modifierWebhook, "webhooks:write"))
	enregistrer(http.MethodDelete, "/webhooks/{webhook_id}",
		proteger(deps.supprimerWebhook, "webhooks:write"))
	enregistrer(http.MethodPost, "/webhooks/{webhook_id}/test",
		proteger(deps.testerWebhook, "webhooks:write"))

	// ── Journal d'actions ─────────────────────────────────────────────────
	enregistrer(http.MethodGet, "/audit", proteger(deps.listerAudit, "audit:read"))

	// ── Métadonnées ───────────────────────────────────────────────────────
	enregistrer(http.MethodGet, "/health", proteger(deps.sante, ""))
	enregistrer(http.MethodGet, "/info", proteger(deps.informations, ""))

	// OpenAPI : documentation publique (aucun secret n'y figure).
	for _, chemin := range []string{"/openapi.json", PrefixeAPI + "/openapi.json"} {
		routeur.AjouterPublique(http.MethodGet, chemin, deps.openapi)
	}
}

// proteger exige une clé valide portant l'une des permissions.
func proteger(handler HandlerAPI, permissions ...string) HandlerAPI {
	return func(requete *Requete) (int, any) {
		if requete.InfoCle == nil {
			return repondreErreur(ErrNonAutorise(""))
		}
		if !requete.InfoCle.Autorise(permissions...) {
			return repondreErreur(ErrInterdit(
				"Permission(s) requise(s) : " + strings.Join(permissions, ", ")))
		}
		return handler(requete)
	}
}

// repondreErreur traduit une erreur en réponse JSON (500 par défaut).
func repondreErreur(err error) (int, any) {
	var apiErr *ErreurAPI
	if errors.As(err, &apiErr) {
		return apiErr.Statut, ReponseErreur(apiErr.Code, apiErr.Message, apiErr.Details)
	}
	// Erreurs métier des modules appelés : leur message est déjà rédigé pour
	// l'utilisateur, on le transmet en 422 plutôt qu'en 500 générique.
	var errPortail *portal.ErreurPortail
	if errors.As(err, &errPortail) {
		return http.StatusUnprocessableEntity,
			ReponseErreur("regle_metier", errPortail.Message, nil)
	}
	var errWebhooks *webhooks.ErreurWebhooks
	if errors.As(err, &errWebhooks) {
		return http.StatusUnprocessableEntity,
			ReponseErreur("regle_metier", errWebhooks.Message, nil)
	}
	return http.StatusInternalServerError,
		ReponseErreur("internal_error", "Erreur interne du serveur", nil)
}

// annuaireOuErreur refuse la route si l'annuaire n'est pas configuré.
func (d Dependances) annuaireOuErreur() (Annuaire, error) {
	if d.Annuaire == nil {
		return nil, nouvelleErreurAPI(http.StatusServiceUnavailable, "annuaire_indisponible",
			"Aucune connexion à l'annuaire n'est configurée.", nil)
	}
	return d.Annuaire, nil
}

// ─── Utilisateurs ──────────────────────────────────────────────────────────

func (d Dependances) listerUtilisateurs(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	page := requete.EntierRequete("page", 1)
	taille := requete.EntierRequete("page_size", 50)
	recherche := requete.ValeurRequete("q", "")

	utilisateurs, err := annuaire.ChercherUtilisateurs(context.Background(), recherche, taille*page)
	if err != nil {
		return repondreErreur(err)
	}
	tranche, total, page, taille := paginer(utilisateurs, page, taille)
	return ReponseOK(map[string]any{
		"items":     tranche,
		"total":     total,
		"page":      page,
		"page_size": taille,
		"filtre":    recherche,
	})
}

func (d Dependances) obtenirUtilisateur(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	sam := requete.Params["sam"]
	utilisateur, err := annuaire.ObtenirUtilisateur(context.Background(), sam)
	if err != nil {
		return repondreErreur(err)
	}
	if utilisateur == nil {
		return repondreErreur(ErrIntrouvable("Utilisateur", sam))
	}
	return ReponseOK(utilisateur)
}

func (d Dependances) creerUtilisateur(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}

	params := CreationUtilisateur{
		SAM:         Texte(objet, "sam"),
		CN:          TexteOu(objet, "cn", Texte(objet, "display_name")),
		Mail:        Texte(objet, "mail"),
		OU:          Texte(objet, "ou"),
		Prenom:      Texte(objet, "given_name"),
		Nom:         Texte(objet, "sn"),
		MotDePasse:  Texte(objet, "password"),
		Description: Texte(objet, "description"),
	}
	if params.SAM == "" || params.Mail == "" {
		return repondreErreur(ErrValidation("sam et mail obligatoires",
			map[string]any{"received": objet}))
	}
	if params.CN == "" {
		params.CN = params.SAM
	}
	if params.Prenom == "" {
		params.Prenom = params.CN
	}
	if params.Nom == "" {
		params.Nom = params.SAM
	}
	if params.OU == "" {
		params.OU = annuaire.BaseDN()
	}

	motDePasseGenere := false
	if params.MotDePasse == "" {
		// Politique « personnel » : les comptes créés par l'API sont des
		// comptes d'administration ou de service.
		genere, err := d.Config.GenererMotDePasse(d.Config.PolitiqueMDPPersonnel,
			params.Prenom, params.Nom)
		if err != nil {
			return repondreErreur(nouvelleErreurAPI(http.StatusInternalServerError,
				"generation_mot_de_passe", err.Error(), nil))
		}
		params.MotDePasse = genere
		motDePasseGenere = true
	}

	utilisateur, err := annuaire.CreerUtilisateur(context.Background(), params)
	if err != nil {
		return repondreErreur(err)
	}
	donnees := map[string]any{"utilisateur": utilisateur}
	if motDePasseGenere {
		// Le mot de passe n'est renvoyé qu'une fois, à la création.
		donnees["mot_de_passe"] = params.MotDePasse
	}
	return ReponseCree(donnees, PrefixeAPI+"/users/"+params.SAM)
}

func (d Dependances) modifierUtilisateur(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	sam := requete.Params["sam"]
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}

	champs := map[string]string{}
	for _, cle := range []string{"cn", "mail", "description", "given_name", "sn", "ou"} {
		if valeur, ok := objet[cle]; ok {
			texte, estTexte := valeur.(string)
			if !estTexte {
				return repondreErreur(ErrValidation(fmt.Sprintf("Le champ %s doit être une chaîne", cle),
					map[string]any{"received": valeur}))
			}
			champs[cle] = strings.TrimSpace(texte)
		}
	}
	if drapeau, ok := objet["active"]; ok {
		actif, estBooleen := drapeau.(bool)
		if !estBooleen {
			return repondreErreur(ErrValidation("Le champ active doit être un booléen",
				map[string]any{"received": drapeau}))
		}
		if actif {
			champs["active"] = "true"
		} else {
			champs["active"] = "false"
		}
	}
	if len(champs) == 0 {
		return repondreErreur(ErrValidation("Aucun champ modifiable fourni",
			map[string]any{"champs_acceptes": []string{"cn", "mail", "description",
				"given_name", "sn", "ou", "active"}}))
	}

	utilisateur, err := annuaire.ModifierUtilisateur(context.Background(), sam, champs)
	if err != nil {
		return repondreErreur(err)
	}
	return ReponseOK(utilisateur)
}

func (d Dependances) supprimerUtilisateur(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	sam := requete.Params["sam"]
	if err := annuaire.SupprimerUtilisateur(context.Background(), sam); err != nil {
		return repondreErreur(err)
	}
	return ReponseOK(map[string]any{"sam": sam, "deleted": true})
}

func (d Dependances) reinitialiserMotDePasse(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	sam := requete.Params["sam"]
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}

	motDePasse := Texte(objet, "password")
	genere := false
	if motDePasse == "" {
		utilisateur, err := annuaire.ObtenirUtilisateur(context.Background(), sam)
		if err != nil {
			return repondreErreur(err)
		}
		if utilisateur == nil {
			return repondreErreur(ErrIntrouvable("Utilisateur", sam))
		}
		motDePasse, err = d.Config.GenererMotDePasse(d.Config.PolitiqueMDPPersonnel,
			utilisateur.CN, utilisateur.SAM)
		if err != nil {
			return repondreErreur(nouvelleErreurAPI(http.StatusInternalServerError,
				"generation_mot_de_passe", err.Error(), nil))
		}
		genere = true
	}
	if err := annuaire.ReinitialiserMotDePasse(context.Background(), sam, motDePasse); err != nil {
		return repondreErreur(err)
	}
	donnees := map[string]any{"sam": sam, "reset": true}
	if genere {
		donnees["mot_de_passe"] = motDePasse
	}
	return ReponseOK(donnees)
}

// ─── Groupes ───────────────────────────────────────────────────────────────

func (d Dependances) listerGroupes(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	page := requete.EntierRequete("page", 1)
	taille := requete.EntierRequete("page_size", 50)
	recherche := requete.ValeurRequete("q", "")

	groupes, err := annuaire.ChercherGroupes(context.Background(), recherche, taille*page)
	if err != nil {
		return repondreErreur(err)
	}
	tranche, total, page, taille := paginer(groupes, page, taille)
	return ReponseOK(map[string]any{
		"items":     tranche,
		"total":     total,
		"page":      page,
		"page_size": taille,
		"filtre":    recherche,
	})
}

func (d Dependances) obtenirGroupe(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	cn := requete.Params["cn"]
	groupe, err := annuaire.ObtenirGroupe(context.Background(), cn)
	if err != nil {
		return repondreErreur(err)
	}
	if groupe == nil {
		return repondreErreur(ErrIntrouvable("Groupe", cn))
	}
	return ReponseOK(groupe)
}

func (d Dependances) creerGroupe(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}
	params := CreationGroupe{
		CN:          Texte(objet, "cn"),
		SAM:         Texte(objet, "sam"),
		Description: Texte(objet, "description"),
		OU:          Texte(objet, "ou"),
		Type:        TexteOu(objet, "type", "security"),
		Portee:      Texte(objet, "scope"),
	}
	if params.CN == "" {
		return repondreErreur(ErrValidation("cn requis", map[string]any{"received": objet}))
	}
	if params.OU == "" {
		params.OU = annuaire.BaseDN()
	}
	groupe, err := annuaire.CreerGroupe(context.Background(), params)
	if err != nil {
		return repondreErreur(err)
	}
	return ReponseCree(groupe, PrefixeAPI+"/groups/"+params.CN)
}

// ─── Unités d'organisation ─────────────────────────────────────────────────

func (d Dependances) listerOUs(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	page := requete.EntierRequete("page", 1)
	taille := requete.EntierRequete("page_size", 50)
	recherche := requete.ValeurRequete("q", "")

	unites, err := annuaire.ChercherOUs(context.Background(), recherche, taille*page)
	if err != nil {
		return repondreErreur(err)
	}
	tranche, total, page, taille := paginer(unites, page, taille)
	return ReponseOK(map[string]any{
		"items":     tranche,
		"total":     total,
		"page":      page,
		"page_size": taille,
		"filtre":    recherche,
	})
}

func (d Dependances) obtenirOU(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	dn := requete.Params["dn"]
	unite, err := annuaire.ObtenirOU(context.Background(), dn)
	if err != nil {
		return repondreErreur(err)
	}
	if unite == nil {
		return repondreErreur(ErrIntrouvable("OU", dn))
	}
	return ReponseOK(unite)
}

// creerOU ajoute une unité d'organisation sous une OU parente.
func (d Dependances) creerOU(requete *Requete) (int, any) {
	annuaire, err := d.annuaireOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}
	nom := Texte(objet, "name")
	if nom == "" {
		return repondreErreur(ErrValidation("name requis", map[string]any{"received": objet}))
	}
	parent := TexteOu(objet, "parent", annuaire.BaseDN())
	unite, err := annuaire.CreerOU(context.Background(), nom, Texte(objet, "description"), parent)
	if err != nil {
		return repondreErreur(err)
	}
	return ReponseCree(unite, PrefixeAPI+"/ous/"+unite.DN)
}

// ─── Portail ───────────────────────────────────────────────────────────────

func (d Dependances) portailOuErreur() (Portail, error) {
	if d.Portail == nil {
		return nil, nouvelleErreurAPI(http.StatusServiceUnavailable, "portail_indisponible",
			"Le portail auto-service n'est pas configuré.", nil)
	}
	return d.Portail, nil
}

func (d Dependances) listerDemandes(requete *Requete) (int, any) {
	portail, err := d.portailOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	statut := requete.ValeurRequete("statut", "")
	page := requete.EntierRequete("page", 1)
	taille := requete.EntierRequete("page_size", 20)

	demandes, err := portail.ListerDemandes(statut)
	if err != nil {
		return repondreErreur(err)
	}
	tranche, total, page, taille := paginer(demandes, page, taille)
	return ReponseOK(map[string]any{
		"items":     tranche,
		"total":     total,
		"page":      page,
		"page_size": taille,
		"statut":    statut,
	})
}

func (d Dependances) approuverDemande(requete *Requete) (int, any) {
	portail, err := d.portailOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}
	identifiant := requete.Params["demande_id"]
	infos, err := portail.ApprouverDemande(identifiant)
	if err != nil {
		return repondreErreur(err)
	}
	return ReponseOK(map[string]any{
		"demande_id": identifiant,
		"approved":   true,
		"notify":     Booleen(objet, "notify", true),
		"infos":      infos,
	})
}

func (d Dependances) refuserDemande(requete *Requete) (int, any) {
	portail, err := d.portailOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}
	identifiant := requete.Params["demande_id"]
	note := Texte(objet, "note")
	if err := portail.RefuserDemande(identifiant, note); err != nil {
		return repondreErreur(err)
	}
	return ReponseOK(map[string]any{
		"demande_id": identifiant,
		"refused":    true,
		"note":       note,
	})
}

// ─── Webhooks ──────────────────────────────────────────────────────────────

func (d Dependances) webhooksOuErreur() (*webhooks.Store, error) {
	if d.Webhooks == nil {
		return nil, nouvelleErreurAPI(http.StatusServiceUnavailable, "webhooks_indisponibles",
			"Aucun magasin de webhooks n'est configuré.", nil)
	}
	return d.Webhooks, nil
}

func (d Dependances) listerWebhooks(requete *Requete) (int, any) {
	store, err := d.webhooksOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	actifsSeulement := requete.ValeurRequete("active", "") == "true"
	liste := store.Lister(actifsSeulement)
	donnees := make([]map[string]any, 0, len(liste))
	for _, webhook := range liste {
		donnees = append(donnees, webhook.Dict())
	}
	return ReponseOK(donnees)
}

func (d Dependances) obtenirWebhook(requete *Requete) (int, any) {
	store, err := d.webhooksOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	identifiant := requete.Params["webhook_id"]
	webhook, err := store.Obtenir(identifiant)
	if err != nil {
		return repondreErreur(err)
	}
	if webhook == nil {
		return repondreErreur(ErrIntrouvable("Webhook", identifiant))
	}
	return ReponseOK(webhook.Dict())
}

func (d Dependances) creerWebhook(requete *Requete) (int, any) {
	store, err := d.webhooksOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}
	nom := Texte(objet, "name")
	adresse := Texte(objet, "url")
	if nom == "" || adresse == "" {
		return repondreErreur(ErrValidation("name et url requis", map[string]any{"received": objet}))
	}
	evenements := chainesJSON(objet, "events")
	if len(evenements) == 0 {
		return repondreErreur(ErrValidation("Au moins un événement requis", nil))
	}

	identifiant, err := identifiantWebhook()
	if err != nil {
		return repondreErreur(err)
	}
	webhook := webhooks.NouveauWebhook(identifiant, nom, adresse, evenements)
	if entetes, ok := objet["headers"].(map[string]any); ok {
		for cle, valeur := range entetes {
			if texte, ok := valeur.(string); ok {
				webhook.Entetes[cle] = texte
			}
		}
	}
	if secret := Texte(objet, "secret"); secret != "" {
		if d.Dispatcher == nil {
			return repondreErreur(nouvelleErreurAPI(http.StatusServiceUnavailable,
				"chiffrement_indisponible", "Le chiffrement des secrets est indisponible.", nil))
		}
		chiffre, err := d.Dispatcher.ChiffrerSecret(secret)
		if err != nil {
			return repondreErreur(err)
		}
		webhook.Secret = chiffre
	}
	if valeur, ok := objet["active"].(bool); ok {
		webhook.Actif = valeur
	}
	if valeur, ok := objet["retry_policy"].(string); ok && valeur != "" {
		webhook.PolitiqueRetrai = valeur
	}
	if valeur, ok := objet["max_retries"].(float64); ok {
		webhook.RetraitsMax = int(valeur)
	}
	if valeur, ok := objet["timeout_seconds"].(float64); ok {
		webhook.DelaiSecondes = valeur
	}

	cree, err := store.Creer(webhook)
	if err != nil {
		return repondreErreur(ErrValidation(err.Error(), nil))
	}
	return ReponseCree(cree.Dict(), PrefixeAPI+"/webhooks/"+cree.ID)
}

func (d Dependances) modifierWebhook(requete *Requete) (int, any) {
	store, err := d.webhooksOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	identifiant := requete.Params["webhook_id"]
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}

	modifie, err := store.MettreAJour(identifiant, func(cible *webhooks.WebhookConfig) {
		if nom := Texte(objet, "name"); nom != "" {
			cible.Nom = nom
		}
		if adresse := Texte(objet, "url"); adresse != "" {
			cible.URL = adresse
		}
		if evenements := chainesJSON(objet, "events"); len(evenements) > 0 {
			cible.Evenements = evenements
		}
		if valeur, ok := objet["active"].(bool); ok {
			cible.Actif = valeur
		}
		if valeur, ok := objet["retry_policy"].(string); ok && valeur != "" {
			cible.PolitiqueRetrai = valeur
		}
		if valeur, ok := objet["max_retries"].(float64); ok {
			cible.RetraitsMax = int(valeur)
		}
		if valeur, ok := objet["timeout_seconds"].(float64); ok {
			cible.DelaiSecondes = valeur
		}
		if entetes, ok := objet["headers"].(map[string]any); ok {
			cible.Entetes = map[string]string{}
			for cle, valeur := range entetes {
				if texte, ok := valeur.(string); ok {
					cible.Entetes[cle] = texte
				}
			}
		}
	})
	if err != nil {
		return repondreErreur(ErrValidation(err.Error(), nil))
	}
	if modifie == nil {
		return repondreErreur(ErrIntrouvable("Webhook", identifiant))
	}
	return ReponseOK(modifie.Dict())
}

func (d Dependances) supprimerWebhook(requete *Requete) (int, any) {
	store, err := d.webhooksOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	identifiant := requete.Params["webhook_id"]
	supprime, err := store.Supprimer(identifiant)
	if err != nil {
		return repondreErreur(err)
	}
	if !supprime {
		return repondreErreur(ErrIntrouvable("Webhook", identifiant))
	}
	return ReponseOK(map[string]any{"id": identifiant, "deleted": true})
}

// testerWebhook envoie un événement de test au webhook.
func (d Dependances) testerWebhook(requete *Requete) (int, any) {
	store, err := d.webhooksOuErreur()
	if err != nil {
		return repondreErreur(err)
	}
	if d.Dispatcher == nil {
		return repondreErreur(nouvelleErreurAPI(http.StatusServiceUnavailable,
			"livraison_indisponible", "La livraison des webhooks est indisponible.", nil))
	}
	identifiant := requete.Params["webhook_id"]
	webhook, err := store.Obtenir(identifiant)
	if err != nil {
		return repondreErreur(err)
	}
	if webhook == nil {
		return repondreErreur(ErrIntrouvable("Webhook", identifiant))
	}
	objet, err := requete.ObjetJSON()
	if err != nil {
		return repondreErreur(err)
	}
	evenement := TexteOu(objet, "event", "system.startup")
	if !contientEvenement(webhook.Evenements, evenement) {
		return repondreErreur(ErrValidation(
			fmt.Sprintf("Le webhook n'est pas abonné à l'événement %s", evenement), nil))
	}

	livraison, err := d.Dispatcher.Livrer(*webhook, evenement,
		map[string]any{"test": true}, 0)
	if err != nil {
		return repondreErreur(err)
	}
	return ReponseOK(livraison)
}

// evenements liste les événements de webhook disponibles.
func (d Dependances) evenements(_ *Requete) (int, any) {
	return ReponseOK(map[string]any{"items": webhooks.EvenementsTries()})
}

// ─── Journal d'actions ─────────────────────────────────────────────────────

func (d Dependances) listerAudit(requete *Requete) (int, any) {
	if d.Journal == nil {
		return repondreErreur(nouvelleErreurAPI(http.StatusServiceUnavailable,
			"journal_indisponible", "Le journal d'actions n'est pas accessible.", nil))
	}
	page := requete.EntierRequete("page", 1)
	taille := requete.EntierRequete("page_size", 50)
	filtre := audit.Filter{
		ActionType: requete.ValeurRequete("action", ""),
		Resultat:   requete.ValeurRequete("resultat", ""),
		Domaine:    requete.ValeurRequete("domaine", ""),
		From:       requete.ValeurRequete("date_debut", ""),
		To:         requete.ValeurRequete("date_fin", ""),
		Recherche:  requete.ValeurRequete("q", ""),
		Limit:      taille * page,
	}
	entrees, err := d.Journal.List(filtre)
	if err != nil {
		return repondreErreur(err)
	}

	items := make([]map[string]any, 0, len(entrees))
	for _, entree := range entrees {
		items = append(items, map[string]any{
			"id":          entree.ID,
			"action":      entree.ActionType,
			"utilisateur": entree.Utilisateur,
			"resultat":    entree.Resultat,
			"compte":      entree.Compte,
			"ou_source":   entree.OUSource,
			"domaine":     entree.Domaine,
			"detail":      entree.Detail,
			"simulation":  entree.Simulation,
			"timestamp":   entree.Timestamp,
		})
	}
	tranche, total, page, taille := paginer(items, page, taille)
	return ReponseOK(map[string]any{
		"items":     tranche,
		"total":     total,
		"page":      page,
		"page_size": taille,
		"filtres": map[string]any{
			"date_debut": filtre.From,
			"date_fin":   filtre.To,
			"action":     filtre.ActionType,
			"resultat":   filtre.Resultat,
			"domaine":    filtre.Domaine,
		},
	})
}

// ─── Métadonnées ───────────────────────────────────────────────────────────

func (d Dependances) sante(_ *Requete) (int, any) {
	version := d.Version
	if version == "" {
		version = VersionAPI
	}
	return ReponseOK(map[string]any{
		"status":      "ok",
		"service":     "edusync-ad-api",
		"version":     version,
		"api_version": VersionAPI,
	})
}

func (d Dependances) informations(_ *Requete) (int, any) {
	version := d.Version
	if version == "" {
		version = VersionAPI
	}
	return ReponseOK(map[string]any{
		"name":        "EduSync AD",
		"version":     version,
		"api_version": VersionAPI,
		"endpoints":   PointsEntree(),
	})
}

// PointsEntree décrit les points d'entrée (route `/info`).
func PointsEntree() map[string]string {
	return map[string]string{
		"users":           PrefixeAPI + "/users",
		"user":            PrefixeAPI + "/users/{sam}",
		"user_password":   PrefixeAPI + "/users/{sam}/password",
		"groups":          PrefixeAPI + "/groups",
		"groups_single":   PrefixeAPI + "/groups/{cn}",
		"ous":             PrefixeAPI + "/ous",
		"ous_single":      PrefixeAPI + "/ous/{dn}",
		"portal_requests": PrefixeAPI + "/portal/requests",
		"portal_approve":  PrefixeAPI + "/portal/requests/{demande_id}/approve",
		"portal_refuse":   PrefixeAPI + "/portal/requests/{demande_id}/refuse",
		"webhooks":        PrefixeAPI + "/webhooks",
		"webhook_test":    PrefixeAPI + "/webhooks/{webhook_id}/test",
		"webhook_events":  PrefixeAPI + "/webhooks/events",
		"audit":           PrefixeAPI + "/audit",
		"health":          PrefixeAPI + "/health",
		"info":            PrefixeAPI + "/info",
		"openapi":         PrefixeAPI + "/openapi.json",
	}
}

// chainesJSON lit une liste de chaînes dans un objet JSON.
func chainesJSON(objet map[string]any, cle string) []string {
	brut, ok := objet[cle].([]any)
	if !ok {
		return nil
	}
	resultat := make([]string, 0, len(brut))
	for _, item := range brut {
		if texte, ok := item.(string); ok {
			resultat = append(resultat, texte)
		}
	}
	return resultat
}

// contientEvenement indique si la liste contient l'événement (ou le joker).
func contientEvenement(evenements []string, evenement string) bool {
	for _, abonnement := range evenements {
		if abonnement == webhooks.Joker || abonnement == evenement {
			return true
		}
	}
	return false
}

// identifiantWebhook génère un identifiant de webhook (`w<12 hex>`).
func identifiantWebhook() (string, error) {
	return webhooks.NouvelIdentifiantWebhook(), nil
}
