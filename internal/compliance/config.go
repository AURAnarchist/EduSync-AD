// Package compliance implémente le module M30 « Conformité & sécurité » :
// rapport RGPD et droit à l'oubli, double authentification de
// l'administrateur (MFA/TOTP), politique de session unique, granularité des
// politiques de mots de passe (PSO Active Directory) et signature des exports.
//
// La version Python (`core/compliance/`) est une ébauche : imports cassés
// (`PortalUser`, `Path` non importé, paramètre accentué) et fonctions qui
// retournent des valeurs en dur. Go fournit ici une implémentation réelle tout
// en conservant les mêmes noms de sorties (`mfa_status`, `details`,
// `recommandations`, `inventaire`, `droit_oubli`) là où c'est utile.
package compliance

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
)

const (
	// NomFichier est le fichier de configuration du module.
	NomFichier = "conformite.json"
	// NomFichierCle est la clé AES locale partagée (secret.key).
	NomFichierCle = "secret.key"
)

// Méthodes de double authentification acceptées.
const (
	// MethodeTOTP est un code à usage unique dérivé du temps (RFC 6238).
	MethodeTOTP = "totp"
	// MethodeCleLog est une clé matérielle de connexion (WebAuthn/clé de
	// sécurité) : l'application ne fait que vérifier que la méthode est
	// déclarée, la cérémonie est assurée par le poste de l'administrateur.
	MethodeCleLog = "cle_log"
)

// Config rassemble les réglages de conformité et de sécurité administrateur.
//
// Le fichier `conformite.json` n'a **pas** d'équivalent Python (le module
// Python lit un attribut `mfa_config` jamais alimenté) : il est propre à Go et
// ne modifie donc aucun fichier partagé.
type Config struct {
	// MFAActif indique que la double authentification est exigée pour
	// l'administration.
	MFAActif bool `json:"mfa_actif"`
	// MFAMethode est la méthode de double authentification (`totp` ou
	// `cle_log`).
	MFAMethode string `json:"mfa_methode"`
	// MFASecret est le secret TOTP, chiffré en AES-256-GCM (base64).
	MFASecret string `json:"mfa_secret"`
	// MFACodesSecours contient les empreintes SHA-256 des codes de secours.
	// Les codes en clair ne sont affichés qu'une fois, à leur création.
	MFACodesSecours []string `json:"codes_secours"`
	// SessionUnique interdit plusieurs sessions simultanées par opérateur.
	SessionUnique bool `json:"session_unique"`
	// MFARequisPourAdmin exige le MFA pour les accès administrateur.
	MFARequisPourAdmin bool `json:"mfa_requis_admin"`
	// SignatureExports active la signature des exports de données.
	SignatureExports bool `json:"signature_exports"`
	// DureeSessionMinutes borne la durée d'une session administrateur.
	DureeSessionMinutes int `json:"duree_session_minutes"`
	// ConservationJours est la durée de conservation des données
	// personnelles (utilisée par le rapport RGPD).
	ConservationJours int `json:"conservation_jours"`
}

// Sauvegarder écrit la configuration dans le fichier spécifié.
func (cfg Config) Sauvegarder(chemin string) (string, error) {
	return SauvegarderConfig(cfg, chemin)
}

// ConfigDefaut retourne la configuration par défaut : MFA désactivé (pour ne
// pas verrouiller une installation existante), session unique exigée.
func ConfigDefaut() Config {
	return Config{
		MFAActif:            false,
		MFAMethode:          MethodeTOTP,
		MFASecret:           "",
		MFACodesSecours:     []string{},
		SessionUnique:       true,
		MFARequisPourAdmin:  true,
		SignatureExports:    true,
		DureeSessionMinutes: 480,
		ConservationJours:   365,
	}
}

// Valider vérifie la cohérence des réglages et retourne les messages d'erreur.
func (c Config) Valider() []string {
	erreurs := []string{}
	if c.MFAMethode != MethodeTOTP && c.MFAMethode != MethodeCleLog {
		erreurs = append(erreurs, fmt.Sprintf("méthode MFA inconnue : %q", c.MFAMethode))
	}
	if c.DureeSessionMinutes < 5 || c.DureeSessionMinutes > 24*60 {
		erreurs = append(erreurs, "la durée de session doit être comprise entre 5 et 1440 minutes")
	}
	if c.ConservationJours < 30 {
		erreurs = append(erreurs, "la durée de conservation doit être d'au moins 30 jours")
	}
	if c.MFAActif && c.MFAMethode == MethodeTOTP && c.MFASecret == "" {
		erreurs = append(erreurs, "MFA TOTP activé sans secret : générez un secret avant d'activer")
	}
	return erreurs
}

// CheminConfigDefaut retourne `conformite.json` dans le dossier de
// configuration utilisateur.
func CheminConfigDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichier), nil
}

// CheminCleDefaut retourne le chemin de la clé AES locale (secret.key).
func CheminCleDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichierCle), nil
}

// ChargerConfig lit `conformite.json`. Un fichier absent ou illisible ne
// bloque jamais : les valeurs manquantes prennent leur défaut (même tolérance
// que les autres modules).
func ChargerConfig(chemin string) Config {
	cfg := ConfigDefaut()
	if chemin == "" {
		defaut, err := CheminConfigDefaut()
		if err != nil {
			return cfg
		}
		chemin = defaut
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return cfg
	}
	// Les champs absents conservent la valeur par défaut : on décode donc
	// par-dessus la configuration par défaut.
	if err := json.Unmarshal(donnees, &cfg); err != nil {
		return ConfigDefaut()
	}
	if cfg.MFACodesSecours == nil {
		cfg.MFACodesSecours = []string{}
	}
	return cfg
}

// SauvegarderConfig écrit `conformite.json` avec des permissions restrictives
// (0600) et retourne le chemin utilisé.
func SauvegarderConfig(cfg Config, chemin string) (string, error) {
	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		return "", fmt.Errorf("compliance: configuration invalide : %s", strings.Join(erreurs, "; "))
	}
	if chemin == "" {
		defaut, err := CheminConfigDefaut()
		if err != nil {
			return "", err
		}
		chemin = defaut
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return "", fmt.Errorf("compliance: création du dossier de configuration: %w", err)
	}
	if cfg.MFACodesSecours == nil {
		cfg.MFACodesSecours = []string{}
	}
	donnees, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("compliance: sérialisation: %w", err)
	}
	if err := os.WriteFile(chemin, donnees, 0o600); err != nil {
		return "", fmt.Errorf("compliance: écriture de %s: %w", chemin, err)
	}
	return chemin, nil
}
