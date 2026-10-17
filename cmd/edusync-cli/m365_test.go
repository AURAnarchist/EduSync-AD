package main

import (
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/graph"
	"github.com/estemobs/EduSync-AD/internal/m365"
)

// comptesM365JSON écrit un fichier de comptes AD minimal.
func comptesM365JSON(t *testing.T, dir string) string {
	t.Helper()
	chemin := filepath.Join(dir, "users.json")
	contenu := `[
		{"sam":"jdurand","cn":"Jean Durand"},
		{"sam":"amartin","cn":"Alice Martin (amartin)","disabled":true}
	]`
	if err := os.WriteFile(chemin, []byte(contenu), 0o600); err != nil {
		t.Fatal(err)
	}
	return chemin
}

// ecrirePNG crée une petite photo de test.
func ecrirePNG(t *testing.T, chemin string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 50, 50))
	for y := 0; y < 50; y++ {
		for x := 0; x < 50; x++ {
			img.Set(x, y, color.RGBA{R: 10, G: 120, B: 200, A: 255})
		}
	}
	fichier, err := os.Create(chemin)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fichier.Close() }()
	if err := png.Encode(fichier, img); err != nil {
		t.Fatal(err)
	}
}

func TestM365ConfigChiffreLeSecret(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := execution(t, "m365", "config", "--save",
		"--tenant-id", "11111111-2222-3333-4444-555555555555",
		"--client-id", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"--client-secret", "super-secret", "--json")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var resultat map[string]any
	if err := json.Unmarshal([]byte(out), &resultat); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if resultat["tenant_id"] != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("tenant = %v", resultat["tenant_id"])
	}
	if resultat["secret_defini"] != true {
		t.Errorf("secret_defini = %v", resultat["secret_defini"])
	}

	dirConfig, err := graph.DossierConfig()
	if err != nil {
		t.Fatal(err)
	}
	chemin := filepath.Join(dirConfig, graph.FichierConfig)
	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("fichier de config: %v", err)
	}
	if strings.Contains(string(brut), "super-secret") {
		t.Error("le secret apparaît en clair dans le fichier")
	}

	// Relecture : le secret doit être correctement déchiffré.
	relu := graph.ChargerConfig(chemin)
	if relu.ClientSecret != "super-secret" {
		t.Errorf("secret relu = %q", relu.ClientSecret)
	}
}

func TestM365UserPlans(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	src := comptesM365JSON(t, t.TempDir())

	out, err := execution(t, "m365", "user-plans", "--src", src,
		"--domain", "lycee.onmicrosoft.com",
		"--existing-upns", "JDURAND@lycee.onmicrosoft.com", "--json")
	if err != nil {
		t.Fatalf("user-plans: %v", err)
	}
	var plans []m365.PlanUtilisateurCloud
	if err := json.Unmarshal([]byte(out), &plans); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d", len(plans))
	}
	if plans[0].UPN != "jdurand@lycee.onmicrosoft.com" || !plans[0].Existing {
		t.Errorf("plan 0 = %+v", plans[0])
	}
	if plans[1].UPN != "amartin@lycee.onmicrosoft.com" || plans[1].Existing || !plans[1].Disabled {
		t.Errorf("plan 1 = %+v", plans[1])
	}
}

func TestM365GroupPlans(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	chemin := filepath.Join(dir, "groups.json")
	if err := os.WriteFile(chemin, []byte(`[{"name":"6ème A","members":["a@l","b@l"]}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := execution(t, "m365", "group-plans", "--src", chemin, "--kind", "m365", "--json")
	if err != nil {
		t.Fatalf("group-plans: %v", err)
	}
	var plans []m365.PlanGroupe
	if err := json.Unmarshal([]byte(out), &plans); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(plans) != 1 || plans[0].MailNickname != "6emea" || plans[0].Kind != "m365" {
		t.Errorf("plans = %+v", plans)
	}

	if _, err := execution(t, "m365", "group-plans", "--src", chemin, "--kind", "inconnu"); err == nil {
		t.Error("type de groupe inconnu accepté")
	}
}

func TestM365PhotoMapping(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	src := comptesM365JSON(t, dir)
	ecrirePNG(t, filepath.Join(dir, "jdurand.png"))

	out, err := execution(t, "m365", "photo-mapping", "--dir", dir, "--src", src,
		"--domain", "lycee.local", "--json")
	if err != nil {
		t.Fatalf("photo-mapping: %v", err)
	}
	var mapping map[string]string
	if err := json.Unmarshal([]byte(out), &mapping); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if _, ok := mapping["jdurand@lycee.local"]; !ok {
		t.Errorf("mapping = %v", mapping)
	}
	if len(mapping) != 1 {
		t.Errorf("mapping = %v", mapping)
	}
}

func TestM365Hybrid(t *testing.T) {
	out, err := execution(t, "m365", "hybrid", "--markdown")
	if err != nil {
		t.Fatalf("hybrid: %v", err)
	}
	if !strings.HasPrefix(out, "# Identité hybride") || !strings.Contains(out, "Entra Connect") {
		t.Errorf("rapport markdown = %s", out)
	}

	outJSON, err := execution(t, "m365", "hybrid", "--json")
	if err != nil {
		t.Fatalf("hybrid --json: %v", err)
	}
	var etapes []map[string]string
	if err := json.Unmarshal([]byte(outJSON), &etapes); err != nil {
		t.Fatalf("JSON: %v\n%s", err, outJSON)
	}
	if len(etapes) == 0 || etapes[0]["titre"] == "" || etapes[0]["detail"] == "" {
		t.Errorf("étapes = %v", etapes)
	}
}

func TestM365SousCommandeInconnue(t *testing.T) {
	if _, err := execution(t, "m365", "inconnue"); err == nil {
		t.Error("sous-commande inconnue acceptée")
	}
	if _, err := execution(t, "m365"); err == nil {
		t.Error("absence de sous-commande acceptée")
	}
}
