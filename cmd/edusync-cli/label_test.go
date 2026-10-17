package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/label"
)

func csvComptesLabel(t *testing.T, dir string, lignes [][]string) string {
	t.Helper()
	chemin := filepath.Join(dir, "comptes.csv")
	plates := make([]string, 0, len(lignes))
	for _, ligne := range lignes {
		plates = append(plates, strings.Join(ligne, ";"))
	}
	ecrireCSV(t, chemin, plates)
	return chemin
}

func TestLabelTemplatesEtShow(t *testing.T) {
	out, err := execution(t, "label", "templates", "--json")
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	var modeles []map[string]any
	if err := json.Unmarshal([]byte(out), &modeles); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(modeles) != 4 {
		t.Fatalf("modeles = %d", len(modeles))
	}
	if modeles[0]["id"] != "etiquette_classe" {
		t.Errorf("premier modèle = %v", modeles[0]["id"])
	}

	outShow, err := execution(t, "label", "show", "--id", "badge_porteur", "--json")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var modele map[string]any
	if err := json.Unmarshal([]byte(outShow), &modele); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if modele["sheet"] != "badge_85x55" {
		t.Errorf("planche = %v", modele["sheet"])
	}
}

func TestLabelRenderProduitPDF(t *testing.T) {
	dir := t.TempDir()
	csvPath := csvComptesLabel(t, dir, [][]string{
		{"identifiant", "nom_complet", "prenom", "nom", "classe", "mail"},
		{"eleve1", "Eleve Un", "Prenom1", "Nom1", "3emeA", "e1@lycee.fr"},
		{"eleve2", "Eleve Deux", "Prenom2", "Nom2", "3emeA", "e2@lycee.fr"},
	})
	out := filepath.Join(dir, "etiquettes.pdf")
	stdout, err := execution(t, "label", "render", "--id", "etiquette_classe", "--csv", csvPath, "--out", out, "--json")
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	var resume map[string]any
	if err := json.Unmarshal([]byte(stdout), &resume); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if resume["comptes"].(float64) != 2 {
		t.Errorf("comptes = %v", resume["comptes"])
	}
	donnees, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(donnees), "%PDF-") {
		t.Error("sortie non PDF")
	}
}

func TestLabelTrombinoscopeProduitPDF(t *testing.T) {
	dir := t.TempDir()
	csvPath := csvComptesLabel(t, dir, [][]string{
		{"identifiant", "nom_complet", "classe"},
		{"e1", "Eleve Un", "3emeA"},
		{"e2", "Eleve Deux", "3emeA"},
	})
	out := filepath.Join(dir, "trombi.pdf")
	if _, err := execution(t, "label", "trombinoscope", "--csv", csvPath, "--out", out,
		"--page", "A4", "--colonnes", "4", "--titre", "3emeA"); err != nil {
		t.Fatalf("trombinoscope: %v", err)
	}
	donnees, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(donnees), "%PDF-") {
		t.Error("sortie non PDF")
	}
}

func TestLabelExportImportRemove(t *testing.T) {
	dir := t.TempDir()
	store := filepath.Join(dir, label.NomFichier)
	exporte := filepath.Join(dir, "modele.json")

	if _, err := execution(t, "label", "export", "--id", "carte_visite", "--out", exporte, "--file", store); err != nil {
		t.Fatalf("export: %v", err)
	}
	// Le magasin n'existe pas encore : on part des intégrés.
	if _, err := execution(t, "label", "remove", "--id", "carte_visite", "--file", store); err != nil {
		t.Fatalf("remove: %v", err)
	}
	// Import du modèle exporté dans le magasin réduit.
	if _, err := execution(t, "label", "import", "--file", exporte, "--store", store); err != nil {
		t.Fatalf("import: %v", err)
	}
	out, err := execution(t, "label", "templates", "--file", store, "--json")
	if err != nil {
		t.Fatalf("templates: %v", err)
	}
	var modeles []map[string]any
	if err := json.Unmarshal([]byte(out), &modeles); err != nil {
		t.Fatal(err)
	}
	trouve := false
	for _, m := range modeles {
		if m["id"] == "carte_visite" {
			trouve = true
		}
	}
	if !trouve {
		t.Errorf("carte_visite absente après import: %v", modeles)
	}
}

func TestLabelMailSetEtConfig(t *testing.T) {
	dir := t.TempDir()
	mailFile := filepath.Join(dir, "label_mail.json")
	cle := filepath.Join(dir, "secret.key")
	if _, err := execution(t, "label", "mail-set", "--host", "smtp.lycee.fr", "--port", "587",
		"--from", "edusync@lycee.fr", "--username", "edusync", "--password", "Secret-123",
		"--mail-config", mailFile, "--key", cle); err != nil {
		t.Fatalf("mail-set: %v", err)
	}
	brut, err := os.ReadFile(mailFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(brut), "Secret-123") {
		t.Error("mot de passe en clair sur disque")
	}
	out, err := execution(t, "label", "mail-config", "--mail-config", mailFile, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("mail-config: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg["host"] != "smtp.lycee.fr" || cfg["mot_de_passe_defini"] != true {
		t.Errorf("config = %v", cfg)
	}
}

func TestLabelSendSansConfigEchoue(t *testing.T) {
	dir := t.TempDir()
	mailFile := filepath.Join(dir, "absent.json")
	_, err := execution(t, "label", "send", "--id", "etiquette_classe", "--to", "a@b.fr",
		"--mail-config", mailFile, "--key", filepath.Join(dir, "k"))
	if err == nil {
		t.Fatal("envoi sans configuration SMTP accepté")
	}
}

func TestLabelArgsObligatoires(t *testing.T) {
	if _, err := execution(t, "label", "render", "--out", "x.pdf"); err == nil {
		t.Error("--id manquant accepté")
	}
	if _, err := execution(t, "label", "show"); err == nil {
		t.Error("--id manquant accepté")
	}
	if _, err := execution(t, "label", "inconnu"); err == nil {
		t.Error("sous-commande inconnue acceptée")
	}
}
