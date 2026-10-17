package portal

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestConfigDefaut vérifie les réglages par défaut (identiques à PortalConfig).
func TestConfigDefaut(t *testing.T) {
	cfg := ConfigDefaut()
	if cfg.Hote != "127.0.0.1" || cfg.Port != 8787 || cfg.CodeMinutes != 10 ||
		cfg.TentativesMax != 5 || cfg.EnvoisMax != 3 || cfg.Politique != ProfilEleve ||
		cfg.OUAccueil != "" || !cfg.NotificationMail ||
		cfg.Titre != "Portail auto-service" || cfg.Actif {
		t.Fatalf("réglages par défaut inattendus : %+v", cfg)
	}
	if err := cfg.Valider(); len(err) != 0 {
		t.Fatalf("les défauts doivent être valides : %v", err)
	}
}

// TestConfigValider vérifie que chaque incohérence est signalée.
func TestConfigValider(t *testing.T) {
	cas := []struct {
		modifier func(*Config)
		attendu  string
	}{
		{func(c *Config) { c.Hote = " " }, "Interface d'écoute requise"},
		{func(c *Config) { c.Port = 0 }, "Port invalide"},
		{func(c *Config) { c.Port = 70000 }, "Port invalide"},
		{func(c *Config) { c.CodeMinutes = 0 }, "Durée de validité du code invalide"},
		{func(c *Config) { c.CodeMinutes = 61 }, "Durée de validité du code invalide"},
		{func(c *Config) { c.TentativesMax = 0 }, "Nombre de tentatives invalide"},
		{func(c *Config) { c.EnvoisMax = 21 }, "Nombre d'envois maximal invalide"},
		{func(c *Config) { c.Politique = "autre" }, "Politique inconnue"},
		{func(c *Config) { c.OUAccueil = "Eleves" }, "OU d'accueil invalide"},
	}
	for _, c := range cas {
		cfg := ConfigDefaut()
		c.modifier(&cfg)
		erreurs := cfg.Valider()
		trouve := false
		for _, e := range erreurs {
			if strings.Contains(e, c.attendu) {
				trouve = true
			}
		}
		if !trouve {
			t.Errorf("attendu %q dans %v", c.attendu, erreurs)
		}
	}

	// Un DN ressemblant à une OU passe.
	cfg := ConfigDefaut()
	cfg.OUAccueil = "OU=Eleves,DC=college,DC=fr"
	if erreurs := cfg.Valider(); len(erreurs) != 0 {
		t.Errorf("OU valide refusée : %v", erreurs)
	}
}

// TestConfigBaseURL vérifie la réécriture des adresses d'écoute génériques.
func TestConfigBaseURL(t *testing.T) {
	cfg := ConfigDefaut()
	if got := cfg.BaseURL(); got != "http://127.0.0.1:8787" {
		t.Errorf("BaseURL = %q", got)
	}
	cfg.Hote = "0.0.0.0"
	if got := cfg.BaseURL(); got != "http://127.0.0.1:8787" {
		t.Errorf("BaseURL = %q", got)
	}
	cfg.Hote = " 192.168.1.20 "
	cfg.Port = 8080
	if got := cfg.BaseURL(); got != "http://192.168.1.20:8080" {
		t.Errorf("BaseURL = %q", got)
	}
}

// TestConfigJSONClesPython vérifie que les clés écrites sont exactement celles
// du dataclass Python (dans le même ordre), condition d'un aller-retour sans
// perte entre les deux implémentations.
func TestConfigJSONClesPython(t *testing.T) {
	brut, err := json.MarshalIndent(ConfigDefaut(), "", "  ")
	if err != nil {
		t.Fatalf("encodage: %v", err)
	}
	attendu := `{
  "actif": false,
  "hote": "127.0.0.1",
  "port": 8787,
  "code_minutes": 10,
  "tentatives_max": 5,
  "envois_max": 3,
  "politique": "eleve",
  "ou_accueil": "",
  "notification_mail": true,
  "titre": "Portail auto-service"
}`
	if string(brut) != attendu {
		t.Fatalf("JSON inattendu :\n%s\nattendu :\n%s", brut, attendu)
	}
}

// TestChargerConfigTolerant vérifie qu'un fichier absent, tronqué ou enrichi
// de clés inconnues ne bloque jamais.
func TestChargerConfigTolerant(t *testing.T) {
	dossier := t.TempDir()

	if cfg := ChargerConfig(filepath.Join(dossier, "absent.json")); cfg.Port != 8787 {
		t.Errorf("fichier absent : port = %d", cfg.Port)
	}

	partiel := filepath.Join(dossier, "partiel.json")
	if err := os.WriteFile(partiel, []byte(`{"port": 9000, "inconnu": true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ChargerConfig(partiel)
	if cfg.Port != 9000 || cfg.Hote != "127.0.0.1" || cfg.Politique != ProfilEleve {
		t.Errorf("fusion des défauts incorrecte : %+v", cfg)
	}

	illisible := filepath.Join(dossier, "illisible.json")
	if err := os.WriteFile(illisible, []byte("{pas du json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := ChargerConfig(illisible); cfg.Port != 8787 {
		t.Errorf("fichier illisible : port = %d", cfg.Port)
	}
}

// TestSauvegarderConfigAllerRetour vérifie l'écriture puis la relecture, et le
// refus des réglages invalides.
func TestSauvegarderConfigAllerRetour(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "sous", "portail.json")
	cfg := ConfigDefaut()
	cfg.Actif = true
	cfg.OUAccueil = "OU=Eleves,DC=college,DC=fr"
	cfg.Titre = "Portail du collège Jean Moulin"

	ecrit, err := SauvegarderConfig(cfg, chemin)
	if err != nil {
		t.Fatalf("sauvegarde: %v", err)
	}
	if ecrit != chemin {
		t.Errorf("chemin = %q", ecrit)
	}
	if relu := ChargerConfig(chemin); relu != cfg {
		t.Errorf("relecture = %+v, attendu %+v", relu, cfg)
	}

	// Aucun caractère accentué n'est échappé (ensure_ascii=False côté Python).
	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(brut), "collège") {
		t.Errorf("accents échappés : %s", brut)
	}

	invalide := ConfigDefaut()
	invalide.Port = -1
	if _, err := SauvegarderConfig(invalide, chemin); err == nil {
		t.Error("une configuration invalide doit être refusée")
	}
}

// TestHorodatageISOFormatPython vérifie le format « isoformat(timespec=
// seconds) » sur un instant UTC, condition de la comparaison lexicographique
// des dates en base.
func TestHorodatageISOFormatPython(t *testing.T) {
	moment := time.Date(2026, 10, 17, 12, 34, 56, 0, time.UTC)
	if got := horodatageISO(moment); got != "2026-10-17T12:34:56+00:00" {
		t.Fatalf("horodatage = %q", got)
	}
	// Un instant exprimé dans un autre fuseau est ramené en UTC.
	paris, err := time.LoadLocation("Europe/Paris")
	if err != nil {
		t.Skipf("fuseau indisponible : %v", err)
	}
	local := time.Date(2026, 10, 17, 14, 34, 56, 0, paris) // 12:34:56 UTC (CEST)
	if got := horodatageISO(local); got != "2026-10-17T12:34:56+00:00" {
		t.Fatalf("horodatage = %q", got)
	}
	relu, err := analyserISO("2026-10-17T12:34:56+00:00")
	if err != nil || !relu.Equal(moment) {
		t.Fatalf("relecture = %v, %v", relu, err)
	}
	// Les dates écrites par Python peuvent aussi porter « Z ».
	if _, err := analyserISO("2026-10-17T12:34:56Z"); err != nil {
		t.Fatalf("relecture « Z » : %v", err)
	}
}

// TestUtilisateurNomAffichage et TestDemandeNomComplet couvrent les petits
// accesseurs utilisés par l'affichage et la validation des mots de passe.
func TestUtilisateurNomAffichage(t *testing.T) {
	if got := (Utilisateur{SAM: "l.martin"}).NomAffichage(); got != "l.martin" {
		t.Errorf("NomAffichage = %q", got)
	}
	if got := (Utilisateur{SAM: "l.martin", CN: "Léa Martin"}).NomAffichage(); got != "Léa Martin" {
		t.Errorf("NomAffichage = %q", got)
	}
}

func TestDemandeNomComplet(t *testing.T) {
	if got := (Demande{Prenom: "Léa", Nom: "Martin"}).NomComplet(); got != "Léa Martin" {
		t.Errorf("NomComplet = %q", got)
	}
	if got := (Demande{Nom: "Martin"}).NomComplet(); got != "Martin" {
		t.Errorf("NomComplet = %q", got)
	}
}
