// Package api implémente l'API REST (M28) : serveur HTTP standard, clés d'API,
// webhooks et document OpenAPI.
//
// Aucune dépendance externe : routage par motifs `{param}`, JSON
// requête/réponse, authentification par clé d'API (`X-API-Key` ou
// `Authorization: Bearer`), CORS configurable et limitation de débit par clé.
// Les formats de fichiers (`api.json`, `api_keys.json`) sont ceux de
// `core/api.py` et `core/api_auth.py` : interchangeables avec Python.
package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
)

// VersionAPI est la version exposée dans les URL et les réponses.
const VersionAPI = "v1"

// PrefixeAPI est le préfixe des routes versionnées.
const PrefixeAPI = "/api/" + VersionAPI

// CorpsMax borne le corps de requête accepté (1 Mio, comme Python).
const CorpsMax = 1024 * 1024

// NomFichier est le fichier de configuration du serveur (dossier de config).
const NomFichier = "api.json"

// ─── Erreurs ───────────────────────────────────────────────────────────────

// ErreurAPI est une erreur métier traduite en réponse JSON.
type ErreurAPI struct {
	Message string
	Statut  int
	Code    string
	Details map[string]any
}

// Error implémente error.
func (e *ErreurAPI) Error() string { return e.Message }

// nouvelleErreurAPI construit une erreur d'API.
func nouvelleErreurAPI(statut int, code, message string, details map[string]any) *ErreurAPI {
	return &ErreurAPI{Message: message, Statut: statut, Code: code, Details: details}
}

// ErrIntrouvable signale une ressource absente (404).
func ErrIntrouvable(ressource, identifiant string) *ErreurAPI {
	return nouvelleErreurAPI(http.StatusNotFound, "not_found",
		fmt.Sprintf("%s '%s' introuvable", ressource, identifiant),
		map[string]any{"resource": ressource, "identifier": identifiant})
}

// ErrNonAutorise signale une clé absente ou invalide (401).
func ErrNonAutorise(message string) *ErreurAPI {
	if message == "" {
		message = "Clé API invalide ou absente"
	}
	return nouvelleErreurAPI(http.StatusUnauthorized, "unauthorized", message, nil)
}

// ErrInterdit signale des permissions insuffisantes (403).
func ErrInterdit(message string) *ErreurAPI {
	if message == "" {
		message = "Accès refusé (permissions insuffisantes)"
	}
	return nouvelleErreurAPI(http.StatusForbidden, "forbidden", message, nil)
}

// ErrValidation signale des données invalides (422).
func ErrValidation(message string, details map[string]any) *ErreurAPI {
	return nouvelleErreurAPI(http.StatusUnprocessableEntity, "validation_error", message, details)
}

// ─── Configuration ─────────────────────────────────────────────────────────

// Config regroupe les réglages du serveur — fichier `api.json` (en clair,
// aucun secret). L'ordre des champs suit celui du dataclass Python pour que le
// JSON produit soit identique.
type Config struct {
	// Actif : démarrage automatique du serveur en même temps que la session.
	Actif bool `json:"actif"`
	// Hote est l'interface d'écoute (127.0.0.1 = poste de l'administrateur).
	Hote string `json:"hote"`
	// Port est le port d'écoute.
	Port int `json:"port"`
	// CORSOrigines liste les origines autorisées (« * » = toutes).
	CORSOrigines []string `json:"cors_origins"`
	// LimiteRPM borne les requêtes par minute et par clé (0 = illimité).
	LimiteRPM int `json:"rate_limit_rpm"`
	// Titre est affiché dans la documentation et l'interface.
	Titre string `json:"titre"`
}

// ConfigDefaut retourne les réglages par défaut de Python.
func ConfigDefaut() Config {
	return Config{
		Hote:         "127.0.0.1",
		Port:         8788,
		CORSOrigines: []string{"*"},
		LimiteRPM:    60,
		Titre:        "EduSync AD API",
	}
}

// Valider retourne les erreurs de configuration, avec les messages de
// `APIConfig.validate` côté Python.
func (c Config) Valider() []string {
	var erreurs []string
	if strings.TrimSpace(c.Hote) == "" {
		erreurs = append(erreurs, "Interface d'écoute requise (ex. 127.0.0.1).")
	}
	if c.Port < 1 || c.Port > 65535 {
		erreurs = append(erreurs, "Port invalide (1-65535).")
	}
	if c.LimiteRPM < 0 || c.LimiteRPM > 10000 {
		erreurs = append(erreurs, "Limite de taux invalide (0-10000).")
	}
	return erreurs
}

// BaseURL retourne l'adresse d'affichage du serveur.
func (c Config) BaseURL() string {
	if c.URLConfiguree() {
		return fmt.Sprintf("http://%s:%d", c.Hote, c.Port)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", c.Port)
}

// URLConfiguree indique si l'hôte peut être utilisé tel quel dans une URL.
func (c Config) URLConfiguree() bool {
	hote := strings.TrimSpace(c.Hote)
	return hote != "" && hote != "0.0.0.0" && hote != "::" && hote != "*"
}

// CheminConfigDefaut retourne `api.json` dans le dossier de configuration.
func CheminConfigDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichier), nil
}

// ChargerConfig lit `api.json`. Un fichier absent, illisible ou partiellement
// renseigné ne bloque jamais : les valeurs manquantes prennent leur défaut
// (même tolérance que `load_api_config`).
func ChargerConfig(chemin string) Config {
	cfg := ConfigDefaut()
	if chemin == "" {
		if defaut, err := CheminConfigDefaut(); err == nil {
			chemin = defaut
		} else {
			return cfg
		}
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return cfg
	}
	var brut map[string]any
	if err := json.Unmarshal(donnees, &brut); err != nil {
		return cfg
	}
	if valeur, ok := brut["actif"].(bool); ok {
		cfg.Actif = valeur
	}
	if valeur, ok := brut["hote"].(string); ok {
		cfg.Hote = valeur
	}
	if valeur, ok := brut["port"].(float64); ok {
		cfg.Port = int(valeur)
	}
	if valeur, ok := brut["cors_origins"].([]any); ok {
		origines := make([]string, 0, len(valeur))
		for _, item := range valeur {
			if origine, ok := item.(string); ok {
				origines = append(origines, origine)
			}
		}
		cfg.CORSOrigines = origines
	}
	if valeur, ok := brut["rate_limit_rpm"].(float64); ok {
		cfg.LimiteRPM = int(valeur)
	}
	if valeur, ok := brut["titre"].(string); ok {
		cfg.Titre = valeur
	}
	return cfg
}

// SauvegarderConfig valide puis écrit `api.json` (indenté, comme Python).
func SauvegarderConfig(cfg Config, chemin string) (string, error) {
	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		return "", nouvelleErreurAPI(http.StatusBadRequest, "invalid_config",
			"Configuration API invalide : "+strings.Join(erreurs, "; "), nil)
	}
	if chemin == "" {
		defaut, err := CheminConfigDefaut()
		if err != nil {
			return "", err
		}
		chemin = defaut
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return "", fmt.Errorf("api: création du dossier de configuration: %w", err)
	}
	if cfg.CORSOrigines == nil {
		cfg.CORSOrigines = []string{}
	}
	donnees, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("api: sérialisation: %w", err)
	}
	if err := os.WriteFile(chemin, donnees, 0o600); err != nil {
		return "", fmt.Errorf("api: écriture de %s: %w", chemin, err)
	}
	return chemin, nil
}

// ─── Routage ───────────────────────────────────────────────────────────────

// motifParam remplace `{nom}` par un groupe de capture nommé.
var motifParam = regexp.MustCompile(`\{([a-zA-Z_][a-zA-Z0-9_]*)\}`)

// HandlerAPI traite une requête et retourne (statut, charge JSON).
type HandlerAPI func(*Requete) (int, any)

// Route associe méthode, motif et handler.
type Route struct {
	Methode string
	Motif   string
	Handler HandlerAPI
	// Publique : route accessible sans clé d'API (documentation OpenAPI).
	Publique  bool
	regex     *regexp.Regexp
	paramNoms []string
}

// Routeur est le registre des routes de l'API.
type Routeur struct {
	routes []Route
}

// NouveauRouteur construit un routeur vide.
func NouveauRouteur() *Routeur { return &Routeur{} }

// Ajouter enregistre une route protégée par clé d'API (`/users/{sam}`).
func (r *Routeur) Ajouter(methode, motif string, handler HandlerAPI) {
	r.ajouter(methode, motif, handler, false)
}

// AjouterPublique enregistre une route accessible sans clé d'API.
func (r *Routeur) AjouterPublique(methode, motif string, handler HandlerAPI) {
	r.ajouter(methode, motif, handler, true)
}

// ajouter compile le motif et enregistre la route.
func (r *Routeur) ajouter(methode, motif string, handler HandlerAPI, publique bool) {
	compile := "^" + motifParam.ReplaceAllString(motif, `(?P<$1>[^/]+)`) + "$"
	route := Route{
		Methode:  strings.ToUpper(methode),
		Motif:    motif,
		Handler:  handler,
		Publique: publique,
		regex:    regexp.MustCompile(compile),
	}
	for _, trouve := range motifParam.FindAllStringSubmatch(motif, -1) {
		route.paramNoms = append(route.paramNoms, trouve[1])
	}
	// Les paramètres sont lus depuis les groupes nommés : `paramNoms` sert à
	// documenter la route (page UI, OpenAPI).
	r.routes = append(r.routes, route)
}

// Routes retourne les routes déclarées (documentation, page UI).
func (r *Routeur) Routes() []Route {
	return append([]Route{}, r.routes...)
}

// Resoudre retourne la route correspondante et les paramètres de chemin, ou
// (nil, nil) quand aucune route ne correspond.
func (r *Routeur) Resoudre(methode, chemin string) (*Route, map[string]string) {
	for index := range r.routes {
		route := &r.routes[index]
		if route.Methode != strings.ToUpper(methode) {
			continue
		}
		trouve := route.regex.FindStringSubmatch(chemin)
		if trouve == nil {
			continue
		}
		parametres := map[string]string{}
		for position, nom := range route.regex.SubexpNames() {
			if position == 0 || nom == "" {
				continue
			}
			parametres[nom] = trouve[position]
		}
		return route, parametres
	}
	return nil, nil
}

// Existe indique qu'un chemin correspond à au moins une route, quelle que soit
// la méthode (distinguer 404 et 405).
func (r *Routeur) Existe(chemin string) bool {
	for index := range r.routes {
		if r.routes[index].regex.MatchString(chemin) {
			return true
		}
	}
	return false
}

// ─── Requête ───────────────────────────────────────────────────────────────

// Requete encapsule la requête HTTP reçue.
type Requete struct {
	Methode   string
	Chemin    string
	Entetes   http.Header
	Corps     []byte
	Requete   url.Values
	Params    map[string]string
	CleAPI    string
	InfoCle   *CleAPIInfo
	corpsJSON any
	corpsLu   bool
}

// ValeurRequete retourne la première valeur d'un paramètre de requête.
func (r *Requete) ValeurRequete(nom, defaut string) string {
	if r.Requete == nil {
		return defaut
	}
	valeurs, ok := r.Requete[nom]
	if !ok || len(valeurs) == 0 {
		return defaut
	}
	if valeurs[0] == "" {
		return defaut
	}
	return valeurs[0]
}

// EntierRequete retourne un paramètre entier de requête.
func (r *Requete) EntierRequete(nom string, defaut int) int {
	valeur := r.ValeurRequete(nom, "")
	if valeur == "" {
		return defaut
	}
	nombre, err := strconv.Atoi(valeur)
	if err != nil {
		return defaut
	}
	return nombre
}

// JSON décode le corps JSON (nil si le corps est vide).
func (r *Requete) JSON() (any, error) {
	if r.corpsLu {
		return r.corpsJSON, nil
	}
	r.corpsLu = true
	if len(strings.TrimSpace(string(r.Corps))) == 0 {
		r.corpsJSON = nil
		return nil, nil
	}
	var valeur any
	if err := json.Unmarshal(r.Corps, &valeur); err != nil {
		return nil, ErrValidation("Corps JSON invalide : "+err.Error(), nil)
	}
	r.corpsJSON = valeur
	return valeur, nil
}

// ObjetJSON décode le corps JSON en objet (erreur si ce n'est pas un objet).
func (r *Requete) ObjetJSON() (map[string]any, error) {
	valeur, err := r.JSON()
	if err != nil {
		return nil, err
	}
	if valeur == nil {
		return map[string]any{}, nil
	}
	objet, ok := valeur.(map[string]any)
	if !ok {
		return nil, ErrValidation("Le corps doit être un objet JSON",
			map[string]any{"recu": valeur})
	}
	return objet, nil
}

// Texte retourne une chaîne d'un objet JSON (`""` si absente).
func Texte(objet map[string]any, cle string) string {
	valeur, ok := objet[cle]
	if !ok {
		return ""
	}
	texte, _ := valeur.(string)
	return strings.TrimSpace(texte)
}

// TexteOu retourne une chaîne d'un objet JSON, ou un défaut.
func TexteOu(objet map[string]any, cle, defaut string) string {
	if texte := Texte(objet, cle); texte != "" {
		return texte
	}
	return defaut
}

// Booleen retourne un booléen d'un objet JSON, ou un défaut.
func Booleen(objet map[string]any, cle string, defaut bool) bool {
	valeur, ok := objet[cle]
	if !ok {
		return defaut
	}
	drapeau, ok := valeur.(bool)
	if !ok {
		return defaut
	}
	return drapeau
}

// ─── Réponses ──────────────────────────────────────────────────────────────

// ReponseOK construit une réponse 200 `{"success":true,"data":…}`.
func ReponseOK(donnees any) (int, any) {
	reponse := map[string]any{"success": true}
	if donnees != nil {
		reponse["data"] = donnees
	}
	return http.StatusOK, reponse
}

// ReponseOKMeta construit une réponse 200 avec métadonnées.
func ReponseOKMeta(donnees, meta any) (int, any) {
	reponse := map[string]any{"success": true}
	if donnees != nil {
		reponse["data"] = donnees
	}
	if meta != nil {
		reponse["meta"] = meta
	}
	return http.StatusOK, reponse
}

// ReponseCree construit une réponse 201 `{"success":true,"data":…}`.
func ReponseCree(donnees any, emplacement string) (int, any) {
	reponse := map[string]any{"success": true, "data": donnees}
	if emplacement != "" {
		reponse["meta"] = map[string]any{"location": emplacement}
	}
	return http.StatusCreated, reponse
}

// ReponseErreur construit le corps d'une réponse d'erreur structurée.
//
// Le code de statut HTTP est fixé par l'appelant : la route le retourne
// (`repondreErreur`) ou le serveur l'écrit directement.
func ReponseErreur(code, message string, details map[string]any) any {
	if details == nil {
		details = map[string]any{}
	}
	return map[string]any{
		"success": false,
		"error": map[string]any{
			"code":    code,
			"message": message,
			"details": details,
		},
	}
}

// paginer découpe une tranche de résultats et retourne l'enveloppe JSON.
//
// `page` et `taille` sont normalisés comme dans les routes Python : la page
// commence à 1, la taille est bornée à 500 éléments.
func paginer[T any](elements []T, page, taille int) ([]T, int, int, int) {
	if page < 1 {
		page = 1
	}
	if taille <= 0 {
		taille = 50
	}
	if taille > 500 {
		taille = 500
	}
	total := len(elements)
	debut := (page - 1) * taille
	if debut > total {
		debut = total
	}
	fin := debut + taille
	if fin > total {
		fin = total
	}
	return elements[debut:fin], total, page, taille
}

// EvenementsDisponibles retourne la liste des événements de webhook.
func EvenementsDisponibles() []string {
	return webhooks.EvenementsTries()
}
