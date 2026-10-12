// Package config gère la configuration locale multi-profils d'EduSync-AD.
//
// Les chemins reprennent ceux de « platformdirs » utilisées par la version
// Python (APP_NAME = "EduSyncAD") :
//
//	Linux   : ~/.config/EduSyncAD, ~/.local/share/EduSyncAD, ~/.cache/EduSyncAD
//	macOS   : ~/Library/Application Support/EduSyncAD, ~/Library/Caches/EduSyncAD
//	Windows : %APPDATA%\EduSyncAD, %LOCALAPPDATA%\EduSyncAD
//
// Un fichier config.json écrit par Python se charge tel quel en Go et
// inversement — cf. ROADMAP_GO_MIGRATION.md §8 (migration sans perte).
package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// AppName est le nom de l'application dans les dossiers système.
const AppName = "EduSyncAD"

// AppConfig — paramètres globaux. Les tags JSON correspondent exactement aux
// clés produites par AppConfig.to_dict() en Python.
type AppConfig struct {
	// Nomenclature des identifiants
	IdentifiantFormatEleve     string `json:"identifiant_format_eleve"`
	IdentifiantFormatPersonnel string `json:"identifiant_format_personnel"`

	// Règle de résolution des doublons
	RegleDoublons models.DoublonRule `json:"regle_doublons"`

	// Politiques de mot de passe (distinctes élèves / personnels)
	PolitiqueMDPEleve     models.PasswordPolicy `json:"politique_mdp_eleve"`
	PolitiqueMDPPersonnel models.PasswordPolicy `json:"politique_mdp_personnel"`

	// Adresses mail — domaine_mail vide = domaine AD connecté
	DomaineMail string `json:"domaine_mail"`
	FormatMail  string `json:"format_mail"`

	// Prénoms composés
	ReglePrenomCompose models.PrenomComposeRule `json:"regle_prenom_compose"`

	// Groupes de classe automatiques
	GroupesClasseAuto bool `json:"groupes_classe_auto"`
	// OU parente des OU de classe ; vide = racine du domaine.
	OUParenteClasses string `json:"ou_parente_classes"`

	// Gestion des départs
	OUArchive             string `json:"ou_archive"`
	DelaiSuppressionJours int    `json:"delai_suppression_jours"`

	// Apparence
	Theme  string `json:"theme"`  // "clair" | "sombre"
	Langue string `json:"langue"` // "fr" | "en"

	// Guide de démarrage
	StartupGuideCompleted bool `json:"startup_guide_completed"`

	// Sécurité de la connexion LDAPS : un AD interne utilise presque toujours
	// un certificat émis par sa propre autorité (AD CS), absente du magasin de
	// confiance du poste. Indiquer le certificat racine ici évite de désactiver
	// la vérification.
	LDAPSVerifierCertificat bool   `json:"ldaps_verifier_certificat"`
	LDAPSCheminCertificatCA string `json:"ldaps_chemin_certificat_ca"`
}

// DefaultAppConfig retourne les valeurs par défaut (identiques à la version
// Python : AppConfig() sans argument).
func DefaultAppConfig() AppConfig {
	return AppConfig{
		IdentifiantFormatEleve:     "prenom.nom",
		IdentifiantFormatPersonnel: "p.nom",
		RegleDoublons:              models.DoublonSuffixeNumerique,
		PolitiqueMDPEleve: models.PasswordPolicy{
			Longueur: 8, Majuscules: false, Chiffres: true, CaracteresSpeciaux: false,
		},
		PolitiqueMDPPersonnel: models.PasswordPolicy{
			Longueur: 12, Majuscules: true, Chiffres: true, CaracteresSpeciaux: true,
		},
		DomaineMail:             "",
		FormatMail:              "{P}.{N}",
		ReglePrenomCompose:      models.PrenomPremierPrenom,
		GroupesClasseAuto:       true,
		OUParenteClasses:        "",
		OUArchive:               "",
		DelaiSuppressionJours:   30,
		Theme:                   "clair",
		Langue:                  "fr",
		StartupGuideCompleted:   false,
		LDAPSVerifierCertificat: true,
		LDAPSCheminCertificatCA: "",
	}
}

// ConfigDir retourne (et crée) le dossier de configuration.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: dossier de configuration utilisateur: %w", err)
	}
	return ensureDir(filepath.Join(base, AppName))
}

// DataDir retourne (et crée) le dossier de données (journal SQLite…).
func DataDir() (string, error) {
	var base string

	switch runtime.GOOS {
	case "linux":
		// platformdirs : XDG_DATA_HOME puis ~/.local/share
		if v := os.Getenv("XDG_DATA_HOME"); v != "" {
			base = v
		} else {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("config: dossier utilisateur: %w", err)
			}
			base = filepath.Join(home, ".local", "share")
		}
	default:
		// macOS / Windows : les données vivent dans le dossier de config.
		var err error
		base, err = os.UserConfigDir()
		if err != nil {
			return "", fmt.Errorf("config: dossier de configuration utilisateur: %w", err)
		}
	}
	return ensureDir(filepath.Join(base, AppName))
}

// CacheDir retourne (et crée) le dossier de cache.
func CacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", fmt.Errorf("config: dossier de cache utilisateur: %w", err)
	}
	return ensureDir(filepath.Join(base, AppName))
}

// DefaultPath retourne le chemin de config.json dans le dossier courant.
func DefaultPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

// Load charge la configuration.
//
// Comportement aligné sur Python :
//   - fichier absent  → (valeurs par défaut, nil)
//   - JSON invalide   → (valeurs par défaut, erreur) — l'appelant décide
//     s'il veut signaler le problème à l'utilisateur ;
//   - champs inconnus → ignorés (compatibilité ascendante des deux sens).
func Load(path string) (AppConfig, error) {
	cfg := DefaultAppConfig()

	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return cfg, err
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return cfg, fmt.Errorf("config: lecture de %s: %w", path, err)
	}

	if err := json.Unmarshal(data, &cfg); err != nil {
		// On neutralise toute valeur partiellement appliquée : les défauts
		// doivent être complets.
		return DefaultAppConfig(), fmt.Errorf("config: %s illisible (JSON invalide): %w", path, err)
	}
	return cfg, nil
}

// Save écrit la configuration (JSON indenté 2, UTF-8 non échappé — mêmes
// réglages que json.dumps(..., indent=2, ensure_ascii=False)).
func Save(cfg AppConfig, path string) error {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return err
		}
	}

	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("config: création du dossier %s: %w", dir, err)
		}
	}

	var buf []byte
	{
		var err error
		buf, err = marshal(cfg)
		if err != nil {
			return err
		}
	}

	// Écriture atomique : on ne remplace jamais un fichier sain par un
	// fichier partiellement écrit.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return fmt.Errorf("config: écriture de %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("config: remplacement de %s: %w", path, err)
	}
	return nil
}

// marshal produit le JSON final (indentation 2 espaces, UTF-8 non échappé,
// sans saut de ligne final) — mêmes réglages que
// json.dumps(..., indent=2, ensure_ascii=False) côté Python.
func marshal(cfg AppConfig) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg); err != nil {
		return nil, fmt.Errorf("config: sérialisation: %w", err)
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

func ensureDir(path string) (string, error) {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("config: création de %s: %w", path, err)
	}
	return path, nil
}
