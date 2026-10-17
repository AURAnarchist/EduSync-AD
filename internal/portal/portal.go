// Package portal implémente le portail auto-service (M27) : réinitialisation
// de mot de passe et consultation d'identifiant par code à usage unique envoyé
// par courriel, et demandes de création de compte validées par un
// administrateur.
//
// Le module est indépendant de l'interface (aucune dépendance au paquet ui) :
// l'annuaire, l'envoi de courriel, l'horloge et le magasin sont injectés, ce
// qui permet de tout tester sans contrôleur de domaine ni serveur SMTP.
//
// Choix de conception repris de la version Python (`core/portal.py`) :
//   - aucune dépendance externe : SQLite (modernc.org/sqlite) et net/http ;
//   - les codes ne sont jamais stockés en clair : dérivation PBKDF2-HMAC-SHA256
//     (sel aléatoire par ticket), à usage unique, avec expiration et plafond
//     d'essais ;
//   - pas d'énumération de comptes : la réponse est identique que le compte
//     existe ou non.
package portal

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
)

// FichierConfig est le nom du fichier de réglages (dossier de configuration).
const FichierConfig = "portail.json"

// FichierBase est le nom du fichier SQLite du portail (dossier de données).
const FichierBase = "portail.db"

// LongueurCode est le nombre de chiffres du code envoyé par courriel
// (6 chiffres = 10^6 combinaisons).
const LongueurCode = 6

// FenetreLimitationMinutes borne le nombre d'envois par identifiant.
const FenetreLimitationMinutes = 15

// iterationsPBKDF2 rend volontairement coûteuse la dérivation d'un code
// (quelques dizaines de millisecondes).
const iterationsPBKDF2 = 60_000

// MessageCodeGenerique est la réponse unique, quel que soit le compte demandé
// (anti-énumération).
const MessageCodeGenerique = "Si un compte correspond, un code vient d'être envoyé à " +
	"l'adresse de messagerie associée."

// Portées de vérification d'un code.
const (
	ScopeReinitialisation = "reinitialisation"
	ScopeConsultation     = "consultation"
)

// Scopes liste les portées acceptées.
var Scopes = []string{ScopeReinitialisation, ScopeConsultation}

// Profils d'une demande de création de compte.
const (
	ProfilEleve     = "eleve"
	ProfilPersonnel = "personnel"
)

// Profils liste les profils acceptés.
var Profils = []string{ProfilEleve, ProfilPersonnel}

// Statuts d'une demande de création de compte.
const (
	StatutEnAttente = "en_attente"
	StatutApprouvee = "approuvee"
	StatutRefusee   = "refusee"
)

// LabelsPolitique associe chaque profil à son libellé (politique de mot de
// passe appliquée aux réinitialisations).
var LabelsPolitique = map[string]string{
	ProfilEleve:     "Élèves",
	ProfilPersonnel: "Personnels",
}

// formatISO est le format d'horodatage écrit en base. Il reproduit exactement
// `datetime.isoformat(timespec="seconds")` sur un instant UTC côté Python
// (« 2026-10-17T12:34:56+00:00 ») : les dates écrites par l'une et l'autre
// implémentation sont donc interchangeables et comparables telles quelles.
const formatISO = "2006-01-02T15:04:05-07:00"

// ErreurPortail est une erreur métier affichable (message déjà en français).
type ErreurPortail struct{ Message string }

// Error implémente error.
func (e *ErreurPortail) Error() string { return e.Message }

// nouvelleErreurPortail construit une erreur métier.
func nouvelleErreurPortail(format string, args ...any) *ErreurPortail {
	return &ErreurPortail{Message: fmt.Sprintf(format, args...)}
}

// ─── Configuration ─────────────────────────────────────────────────────────

// Config regroupe les réglages du portail — fichier `portail.json` (en clair,
// aucun secret). L'ordre des champs suit celui du dataclass Python pour que le
// JSON produit soit identique.
type Config struct {
	// Actif : démarrage automatique du serveur en même temps que la session.
	Actif bool `json:"actif"`
	// Hote est l'interface d'écoute : 127.0.0.1 = machine de l'administrateur,
	// 0.0.0.0 = tout le réseau interne.
	Hote string `json:"hote"`
	Port int    `json:"port"`
	// CodeMinutes est la durée de validité d'un code.
	CodeMinutes int `json:"code_minutes"`
	// TentativesMax est le nombre maximal de tentatives (bonnes ou mauvaises)
	// par code.
	TentativesMax int `json:"tentatives_max"`
	// EnvoisMax est le nombre maximal de codes demandés pour un même
	// identifiant.
	EnvoisMax int `json:"envois_max"`
	// Politique de mot de passe appliquée aux réinitialisations.
	Politique string `json:"politique"`
	// OUAccueil est l'OU dans laquelle un compte est créé lors de la validation
	// d'une demande.
	OUAccueil string `json:"ou_accueil"`
	// NotificationMail envoie les identifiants générés à la validation.
	NotificationMail bool   `json:"notification_mail"`
	Titre            string `json:"titre"`
}

// ConfigDefaut retourne les réglages par défaut (identiques à PortalConfig()).
func ConfigDefaut() Config {
	return Config{
		Actif:            false,
		Hote:             "127.0.0.1",
		Port:             8787,
		CodeMinutes:      10,
		TentativesMax:    5,
		EnvoisMax:        3,
		Politique:        ProfilEleve,
		OUAccueil:        "",
		NotificationMail: true,
		Titre:            "Portail auto-service",
	}
}

// Valider retourne la liste des réglages incohérents (vide = valide).
func (c Config) Valider() []string {
	var erreurs []string
	if strings.TrimSpace(c.Hote) == "" {
		erreurs = append(erreurs, "Interface d'écoute requise (ex. 127.0.0.1).")
	}
	if c.Port < 1 || c.Port > 65535 {
		erreurs = append(erreurs, "Port invalide (1-65535).")
	}
	if c.CodeMinutes < 1 || c.CodeMinutes > 60 {
		erreurs = append(erreurs, "Durée de validité du code invalide (1-60 minutes).")
	}
	if c.TentativesMax < 1 || c.TentativesMax > 10 {
		erreurs = append(erreurs, "Nombre de tentatives invalide (1-10).")
	}
	if c.EnvoisMax < 1 || c.EnvoisMax > 20 {
		erreurs = append(erreurs, "Nombre d'envois maximal invalide (1-20).")
	}
	if _, ok := LabelsPolitique[c.Politique]; !ok {
		erreurs = append(erreurs, fmt.Sprintf("Politique inconnue : %s", c.Politique))
	}
	if c.OUAccueil != "" && !strings.Contains(c.OUAccueil, "=") {
		erreurs = append(erreurs, fmt.Sprintf(
			"OU d'accueil invalide (DN attendu) : %s", c.OUAccueil))
	}
	return erreurs
}

// BaseURL est l'adresse à communiquer aux usagers.
func (c Config) BaseURL() string {
	hote := strings.TrimSpace(c.Hote)
	if hote == "0.0.0.0" || hote == "::" || hote == "*" {
		hote = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", hote, c.Port)
}

// CheminConfigDefaut retourne le chemin de `portail.json`.
func CheminConfigDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FichierConfig), nil
}

// ChargerConfig lit `portail.json`. Un fichier absent, illisible ou
// partiellement renseigné ne bloque jamais : les valeurs manquantes prennent
// leur défaut (même tolérance que `load_portal_config`).
func ChargerConfig(chemin string) Config {
	if chemin == "" {
		if defaut, err := CheminConfigDefaut(); err == nil {
			chemin = defaut
		} else {
			return ConfigDefaut()
		}
	}
	cfg := ConfigDefaut()
	brut, err := os.ReadFile(chemin)
	if err != nil {
		return cfg
	}
	// Les clés inconnues sont ignorées, les clés absentes conservent le défaut.
	if err := json.Unmarshal(brut, &cfg); err != nil {
		return ConfigDefaut()
	}
	return cfg
}

// SauvegarderConfig écrit `portail.json` après validation. Le JSON indenté
// (deux espaces, UTF-8 non échappé) est relu tel quel par la version Python.
func SauvegarderConfig(cfg Config, chemin string) (string, error) {
	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		return "", nouvelleErreurPortail("Configuration du portail invalide : %s",
			strings.Join(erreurs, "; "))
	}
	if chemin == "" {
		defaut, err := CheminConfigDefaut()
		if err != nil {
			return "", err
		}
		chemin = defaut
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return "", fmt.Errorf("portal: création du dossier de configuration: %w", err)
	}
	brut, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("portal: encodage de %s: %w", FichierConfig, err)
	}
	if err := os.WriteFile(chemin, brut, 0o600); err != nil {
		return "", fmt.Errorf("portal: écriture de %s: %w", chemin, err)
	}
	return chemin, nil
}

// ─── Compte lu dans l'annuaire ─────────────────────────────────────────────

// Utilisateur est la projection minimale d'un compte pour le portail.
type Utilisateur struct {
	DN        string `json:"dn"`
	SAM       string `json:"sam"`
	CN        string `json:"cn,omitempty"`
	Mail      string `json:"mail,omitempty"`
	Desactive bool   `json:"disabled"`
}

// NomAffichage retourne le nom à afficher (CN, à défaut l'identifiant).
func (u Utilisateur) NomAffichage() string {
	if u.CN != "" {
		return u.CN
	}
	return u.SAM
}

// ─── Demandes de création de compte ────────────────────────────────────────

// Demande est une pré-inscription soumise depuis le portail.
type Demande struct {
	ID        string `json:"id"`
	Profil    string `json:"profil"`
	Prenom    string `json:"prenom"`
	Nom       string `json:"nom"`
	Mail      string `json:"mail"`
	Classe    string `json:"classe"`
	Motif     string `json:"motif"`
	Statut    string `json:"statut"`
	CreatedAt string `json:"created_at"`
	TraiteLe  string `json:"traite_le"`
	TraitePar string `json:"traite_par"`
	Note      string `json:"note"`
}

// NomComplet retourne « prénom nom ».
func (d Demande) NomComplet() string {
	return strings.TrimSpace(d.Prenom + " " + d.Nom)
}

// horodatageISO formate un instant comme la version Python.
func horodatageISO(moment time.Time) string {
	return moment.UTC().Format(formatISO)
}

// analyserISO relit un horodatage écrit par Go ou par Python.
func analyserISO(valeur string) (time.Time, error) {
	return time.Parse(time.RFC3339, valeur)
}

// chiffresAleatoires retourne une chaîne de n chiffres décimaux (chaque
// chiffre tiré uniformément), comme `secrets.randbelow(10**n)` mis en forme.
func chiffresAleatoires(n int) (string, error) {
	limite := 1
	for i := 0; i < n; i++ {
		limite *= 10
	}
	valeur, err := entierAleatoire(limite)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%0*d", n, valeur), nil
}

// identifiantAleatoireHex retourne n caractères hexadécimaux minuscules
// (équivalent de `uuid.uuid4().hex[:n]` pour un identifiant opaque).
func identifiantAleatoireHex(n int) (string, error) {
	octets, err := octetsAleatoires((n + 1) / 2)
	if err != nil {
		return "", err
	}
	hexa := fmt.Sprintf("%x", octets)
	return hexa[:n], nil
}
