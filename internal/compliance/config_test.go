package compliance

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigDefaut vérifie les valeurs par défaut et leur validité.
func TestConfigDefaut(t *testing.T) {
	cfg := ConfigDefaut()
	if cfg.MFAActif {
		t.Error("le MFA ne doit pas être activé par défaut (verrouillage d'une installation existante)")
	}
	if cfg.MFAMethode != MethodeTOTP {
		t.Errorf("méthode = %q", cfg.MFAMethode)
	}
	if !cfg.SessionUnique {
		t.Error("la session unique doit être exigée par défaut")
	}
	if cfg.ConservationJours != ConservationDefautJours {
		t.Errorf("conservation = %d", cfg.ConservationJours)
	}
	if cfg.MFACodesSecours == nil {
		t.Error("la liste des codes de secours doit être non nil (sérialisée en [])")
	}
	if erreurs := cfg.Valider(); len(erreurs) != 0 {
		t.Fatalf("configuration par défaut invalide : %v", erreurs)
	}
}

func TestConfigValider(t *testing.T) {
	cfg := ConfigDefaut()
	cfg.MFAMethode = "sms"
	if erreurs := cfg.Valider(); len(erreurs) == 0 {
		t.Error("une méthode inconnue doit être refusée")
	}

	cfg = ConfigDefaut()
	cfg.DureeSessionMinutes = 1
	if erreurs := cfg.Valider(); len(erreurs) == 0 {
		t.Error("une durée de session trop courte doit être refusée")
	}
	cfg = ConfigDefaut()
	cfg.DureeSessionMinutes = 24*60 + 1
	if erreurs := cfg.Valider(); len(erreurs) == 0 {
		t.Error("une durée de session trop longue doit être refusée")
	}

	cfg = ConfigDefaut()
	cfg.ConservationJours = 10
	if erreurs := cfg.Valider(); len(erreurs) == 0 {
		t.Error("une conservation trop courte doit être refusée")
	}

	// MFA TOTP activé sans secret : incohérent.
	cfg = ConfigDefaut()
	cfg.MFAActif = true
	if erreurs := cfg.Valider(); len(erreurs) == 0 {
		t.Error("MFA TOTP sans secret doit être signalé")
	}
	// MFA par clé matérielle : pas de secret à exiger.
	cfg.MFAMethode = MethodeCleLog
	if erreurs := cfg.Valider(); len(erreurs) != 0 {
		t.Errorf("configuration cle_log valide : %v", erreurs)
	}
}

func TestCheminsDefaut(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cheminConfig, err := CheminConfigDefaut()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cheminConfig) != NomFichier {
		t.Errorf("chemin de configuration = %q", cheminConfig)
	}
	cheminCle, err := CheminCleDefaut()
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cheminCle) != NomFichierCle {
		t.Errorf("chemin de clé = %q", cheminCle)
	}
}

func TestChargerConfigTolerant(t *testing.T) {
	dir := t.TempDir()

	// Fichier absent : valeurs par défaut.
	absent := filepath.Join(dir, "absent.json")
	if cfg := ChargerConfig(absent); !memeConfig(cfg, ConfigDefaut()) {
		t.Errorf("configuration absente = %+v", cfg)
	}

	// Fichier partiel : les champs absents gardent leur défaut.
	partiel := filepath.Join(dir, "partiel.json")
	if err := os.WriteFile(partiel, []byte(`{"mfa_actif": true, "mfa_methode": "totp"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ChargerConfig(partiel)
	if !cfg.MFAActif || cfg.MFAMethode != MethodeTOTP {
		t.Errorf("valeurs lues = %+v", cfg)
	}
	if !cfg.SessionUnique || cfg.DureeSessionMinutes != 480 || cfg.ConservationJours != ConservationDefautJours {
		t.Errorf("défauts perdus = %+v", cfg)
	}
	if cfg.MFACodesSecours == nil {
		t.Error("codes de secours non initialisés")
	}

	// Fichier illisible : retour aux défauts.
	corrompu := filepath.Join(dir, "corrompu.json")
	if err := os.WriteFile(corrompu, []byte("{pas du json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := ChargerConfig(corrompu); !memeConfig(cfg, ConfigDefaut()) {
		t.Errorf("configuration corrompue = %+v", cfg)
	}

	// Chemin vide : fichier de configuration par défaut (absent ici).
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if cfg := ChargerConfig(""); !memeConfig(cfg, ConfigDefaut()) {
		t.Errorf("configuration par défaut = %+v", cfg)
	}
}

func TestSauvegarderConfig(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "sous", NomFichier)
	cfg := ConfigDefaut()
	cfg.MFAActif = true
	cfg.MFAMethode = MethodeCleLog
	cfg.ConservationJours = 730
	cfg.MFACodesSecours = []string{"empreinte"}

	obtenu, err := SauvegarderConfig(cfg, chemin)
	if err != nil {
		t.Fatal(err)
	}
	if obtenu != chemin {
		t.Errorf("chemin = %q", obtenu)
	}
	info, err := os.Stat(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %v, attendu 0600", info.Mode().Perm())
	}

	if relu := ChargerConfig(chemin); !memeConfig(relu, cfg) {
		t.Errorf("aller-retour : %+v ≠ %+v", relu, cfg)
	}

	// Configuration invalide : refusée avant écriture.
	invalide := ConfigDefaut()
	invalide.MFAMethode = "inconnue"
	if _, err := SauvegarderConfig(invalide, filepath.Join(t.TempDir(), NomFichier)); err == nil {
		t.Error("une configuration invalide doit être refusée")
	}

	// Chemin vide : écrit dans le dossier de configuration.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	cheminDefaut, err := SauvegarderConfig(ConfigDefaut(), "")
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(cheminDefaut) != NomFichier {
		t.Errorf("chemin par défaut = %q", cheminDefaut)
	}
}

// memeConfig compare deux configurations champ à champ (la structure contient
// une tranche et n'est donc pas comparable avec `==`).
func memeConfig(a, b Config) bool {
	if a.MFAActif != b.MFAActif || a.MFAMethode != b.MFAMethode || a.MFASecret != b.MFASecret ||
		a.SessionUnique != b.SessionUnique || a.DureeSessionMinutes != b.DureeSessionMinutes ||
		a.ConservationJours != b.ConservationJours {
		return false
	}
	if len(a.MFACodesSecours) != len(b.MFACodesSecours) {
		return false
	}
	for index := range a.MFACodesSecours {
		if a.MFACodesSecours[index] != b.MFACodesSecours[index] {
			return false
		}
	}
	return true
}
