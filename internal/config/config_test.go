package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// TestDefaultAppConfigMatchPython verifie que les valeurs par defaut sont
// exactement celles du dataclass Python AppConfig().
func TestDefaultAppConfigMatchPython(t *testing.T) {
	cfg := DefaultAppConfig()

	checks := []struct {
		nom  string
		got  any
		want any
	}{
		{"identifiant_format_eleve", cfg.IdentifiantFormatEleve, "prenom.nom"},
		{"identifiant_format_personnel", cfg.IdentifiantFormatPersonnel, "p.nom"},
		{"regle_doublons", string(cfg.RegleDoublons), "suffixe_numerique"},
		{"domaine_mail", cfg.DomaineMail, ""},
		{"format_mail", cfg.FormatMail, "{P}.{N}"},
		{"regle_prenom_compose", string(cfg.ReglePrenomCompose), "premier_prenom"},
		{"groupes_classe_auto", cfg.GroupesClasseAuto, true},
		{"ou_parente_classes", cfg.OUParenteClasses, ""},
		{"ou_archive", cfg.OUArchive, ""},
		{"delai_suppression_jours", cfg.DelaiSuppressionJours, 30},
		{"theme", cfg.Theme, "clair"},
		{"langue", cfg.Langue, "fr"},
		{"startup_guide_completed", cfg.StartupGuideCompleted, false},
		{"ldaps_verifier_certificat", cfg.LDAPSVerifierCertificat, true},
		{"ldaps_chemin_certificat_ca", cfg.LDAPSCheminCertificatCA, ""},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %#v, attendu %#v", c.nom, c.got, c.want)
		}
	}

	if cfg.PolitiqueMDPEleve != (models.PasswordPolicy{
		Longueur: 8, Majuscules: false, Chiffres: true, CaracteresSpeciaux: false,
	}) {
		t.Errorf("politique_mdp_eleve = %+v, attendu longueur 8 / sans majuscules", cfg.PolitiqueMDPEleve)
	}
	if cfg.PolitiqueMDPPersonnel != (models.PasswordPolicy{
		Longueur: 12, Majuscules: true, Chiffres: true, CaracteresSpeciaux: true,
	}) {
		t.Errorf("politique_mdp_personnel = %+v, attendu longueur 12 / majuscules / speciaux", cfg.PolitiqueMDPPersonnel)
	}
}

// TestLoadFixturePython garantit qu'un config.json ecrit par la version
// Python se charge tel quel en Go (criteres de migration sans perte).
func TestLoadFixturePython(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "config_python.json"))
	if err != nil {
		t.Fatalf("chargement du fixture Python: %v", err)
	}

	if cfg.IdentifiantFormatEleve != "p.nom.annee" {
		t.Errorf("identifiant_format_eleve = %q", cfg.IdentifiantFormatEleve)
	}
	if cfg.RegleDoublons != models.DoublonLettresPrenom {
		t.Errorf("regle_doublons = %q, attendu lettres_prenom", cfg.RegleDoublons)
	}
	if cfg.ReglePrenomCompose != models.PrenomConcatenation {
		t.Errorf("regle_prenom_compose = %q, attendu concatenation", cfg.ReglePrenomCompose)
	}
	if cfg.DomaineMail != "lycee-example.fr" {
		t.Errorf("domaine_mail = %q", cfg.DomaineMail)
	}
	if cfg.GroupesClasseAuto {
		t.Error("groupes_classe_auto devrait être false")
	}
	if cfg.OUParenteClasses != "OU=Eleves,DC=lycee,DC=local" {
		t.Errorf("ou_parente_classes = %q", cfg.OUParenteClasses)
	}
	if cfg.OUArchive != "OU=Archive,DC=lycee,DC=local" {
		t.Errorf("ou_archive = %q", cfg.OUArchive)
	}
	if cfg.DelaiSuppressionJours != 45 {
		t.Errorf("delai_suppression_jours = %d, attendu 45", cfg.DelaiSuppressionJours)
	}
	if cfg.Theme != "sombre" || cfg.Langue != "en" {
		t.Errorf("theme/langue = %q/%q, attendu sombre/en", cfg.Theme, cfg.Langue)
	}
	if cfg.StartupGuideCompleted != true {
		t.Error("startup_guide_completed devrait être true")
	}
	if cfg.LDAPSVerifierCertificat {
		t.Error("ldaps_verifier_certificat devrait être false dans le fixture")
	}
	if cfg.LDAPSCheminCertificatCA != "/etc/ssl/certs/adcs-root.pem" {
		t.Errorf("ldaps_chemin_certificat_ca = %q", cfg.LDAPSCheminCertificatCA)
	}
	// « pattern_fixe » vaut null côté Python : Go doit le lire comme vide.
	if cfg.PolitiqueMDPEleve.PatternFixe != "" {
		t.Errorf("pattern_fixe = %q, attendu vide (null Python)", cfg.PolitiqueMDPEleve.PatternFixe)
	}
}

// TestSaveLoadRoundTrip verifie que la configuration ecrite par Go est
// relisible (par Go, puis par Python : meme structure de clés).
func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")

	want := DefaultAppConfig()
	want.IdentifiantFormatEleve = "prenom.nom.annee"
	want.DomaineMail = "école-example.fr" // accents UTF-8 non échappés
	want.RegleDoublons = models.DoublonAnneeSuffixe
	want.PolitiqueMDPEleve.PatternFixe = "Edu$ync2026"
	want.Theme = "sombre"

	if err := Save(want, path); err != nil {
		t.Fatalf("sauvegarde: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if strings.Contains(string(raw), `\u00e9`) {
		t.Error("les accents sont échappés : attendu UTF-8 brut (ensure_ascii=False)")
	}
	if !strings.Contains(string(raw), "école-example.fr") {
		t.Error("la valeur accentuée est absente du fichier écrit")
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("rechargement: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip différent:\n  écrit  : %+v\n  relu   : %+v", want, got)
	}
}

func TestLoadFichierAbsent(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "absent.json"))
	if err != nil {
		t.Fatalf("un fichier absent ne doit pas produire d'erreur: %v", err)
	}
	if cfg != DefaultAppConfig() {
		t.Error("un fichier absent doit rendre la configuration par défaut")
	}
}

func TestLoadJSONInvalide(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{ pas du json"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err == nil {
		t.Fatal("un JSON invalide doit remonter une erreur")
	}
	if cfg != DefaultAppConfig() {
		t.Error("un JSON invalide doit rendre la configuration par défaut complète")
	}
}

func TestLoadChampsInconnusIgnores(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	content := `{"theme":"sombre","champ_inconnu_futur":42,"autre":{"a":1}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("les champs inconnus doivent être ignorés: %v", err)
	}
	if cfg.Theme != "sombre" {
		t.Errorf("theme = %q", cfg.Theme)
	}
}

func TestSaveEcritureAtomique(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := Save(DefaultAppConfig(), path); err != nil {
		t.Fatal(err)
	}
	// Aucun fichier temporaire ne doit survivre à une sauvegarde réussie.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("fichier temporaire laissé derrière: %s", e.Name())
		}
	}
}

// TestDossiersPlatformdirs controle que les chemins systemes reproduisent
// ceux de « platformdirs » utilises par la version Python.
func TestDossiersPlatformdirs(t *testing.T) {
	// Redirige XDG vers un dossier temporaire : le test ne doit jamais
	// toucher au profil de l'utilisateur qui lance les tests.
	racine := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(racine, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(racine, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(racine, "cache"))

	for _, c := range []struct {
		nom   string
		appel func() (string, error)
	}{
		{"config", ConfigDir},
		{"data", DataDir},
		{"cache", CacheDir},
	} {
		dir, err := c.appel()
		if err != nil {
			t.Fatalf("%s: %v", c.nom, err)
		}
		if filepath.Base(dir) != AppName {
			t.Errorf("%s = %q, attendu un dossier nommé %q", c.nom, dir, AppName)
		}
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			t.Errorf("%s: le dossier %q doit exister (%v)", c.nom, dir, err)
		}
	}
}

// TestLoadIgnoreLeMauvaisTypeDeChamp : une valeur de type inattendu ne doit
// pas corrompre la configuration entière.
func TestLoadIgnoreLeMauvaisTypeDeChamp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"delai_suppression_jours":"trente"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(path)
	if err == nil {
		t.Fatal("une valeur de type invalide doit remonter une erreur")
	}
	if cfg.DelaiSuppressionJours != 30 {
		t.Errorf("delai_suppression_jours = %d, attendu la valeur par défaut 30", cfg.DelaiSuppressionJours)
	}
}

// TestJSONProduitParGoEstCompatiblePython : on vérifie que toutes les clés
// attendues par le dataclass Python sont présentes.
func TestJSONProduitParGoEstCompatiblePython(t *testing.T) {
	raw, err := marshal(DefaultAppConfig())
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]json.RawMessage
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}

	attendues := []string{
		"identifiant_format_eleve", "identifiant_format_personnel", "regle_doublons",
		"politique_mdp_eleve", "politique_mdp_personnel", "domaine_mail", "format_mail",
		"regle_prenom_compose", "groupes_classe_auto", "ou_parente_classes", "ou_archive",
		"delai_suppression_jours", "theme", "langue", "startup_guide_completed",
		"ldaps_verifier_certificat", "ldaps_chemin_certificat_ca",
	}
	for _, clee := range attendues {
		if _, ok := data[clee]; !ok {
			t.Errorf("clé absente du JSON produit: %s", clee)
		}
	}
	if len(data) != len(attendues) {
		t.Errorf("%d clés produites, %d attendues", len(data), len(attendues))
	}
}
