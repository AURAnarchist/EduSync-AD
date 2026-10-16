package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func ecrireCSV(t *testing.T, chemin string, lignes []string) {
	t.Helper()
	f, err := os.Create(chemin)
	if err != nil {
		t.Fatalf("création CSV: %v", err)
	}
	defer f.Close()
	for _, ligne := range lignes {
		_, _ = f.WriteString(ligne + "\n")
	}
}

func TestUsersImportSimulate(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "users.csv")
	ecrireCSV(t, chemin, []string{
		"prenom;nom;classe;ou;email",
		"Léa;Martin;6e_A;;lea.martin@lycee.fr",
		"Jean;Durand;6e_A;;jean.durand@lycee.fr",
		"Léa;Martin;6e_A;;lea.martin@lycee.fr", // doublon
	})

	out, err := execution(t, "users", "import-simulate", "-f", chemin)
	if err != nil {
		t.Fatalf("users import-simulate: %v", err)
	}
	if !strings.Contains(out, "3 valides") {
		t.Errorf("attendu 3 valides, sortie: %s", out)
	}
	if !strings.Contains(out, "prenom.nom002") {
		t.Errorf("doublon non résolu, sortie: %s", out)
	}
}

func TestUsersImportSimulateJSON(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "users.csv")
	ecrireCSV(t, chemin, []string{
		"prenom;nom;classe;ou;email",
		"Léa;Martin;6e_A;;lea.martin@lycee.fr",
	})

	out, err := execution(t, "users", "import-simulate", "-f", chemin, "--json")
	if err != nil {
		t.Fatalf("users import-simulate --json: %v", err)
	}
	var r resultatUsersImport
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.Total != 1 {
		t.Errorf("total = %d, attendu 1", r.Total)
	}
	if r.Valides != 1 {
		t.Errorf("valides = %d, attendu 1", r.Valides)
	}
	if r.Lignes[0].Identifiant == "" {
		t.Error("identifiant vide")
	}
	if r.Lignes[0].Mail == "" {
		t.Error("mail vide")
	}
	if !strings.Contains(r.Lignes[0].OUCible, "OU=Eleves") {
		t.Errorf("ou_cible = %q", r.Lignes[0].OUCible)
	}
}

func TestUsersImportSimulateSansFichier(t *testing.T) {
	_, err := execution(t, "users", "import-simulate")
	if err == nil {
		t.Fatal("sous-commande manquante doit échouer")
	}
	_, err = execution(t, "users", "import-simulate", "-f", "/inexistant.csv")
	if err == nil {
		t.Fatal("fichier inexistant doit échouer")
	}
}

func TestPasswordSimulate(t *testing.T) {
	out, err := execution(t, "password", "simulate", "-n", "5", "--type", "eleve")
	if err != nil {
		t.Fatalf("password simulate: %v", err)
	}
	if !strings.Contains(out, "5 valides") {
		t.Errorf("attendu 5 valides, sortie: %s", out)
	}
}

func TestPasswordSimulateJSON(t *testing.T) {
	out, err := execution(t, "password", "simulate", "-n", "2", "--type", "personnel", "--json")
	if err != nil {
		t.Fatalf("password simulate --json: %v", err)
	}
	var r resultatPasswordSim
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.Total != 2 {
		t.Errorf("total = %d, attendu 2", r.Total)
	}
	if r.Politique != "personnel" {
		t.Errorf("politique = %q, attendu personnel", r.Politique)
	}
	if r.Longueur != 12 {
		t.Errorf("longueur = %d, attendu 12", r.Longueur)
	}
	for _, l := range r.Lignes {
		if l.MDP == "" {
			t.Error("MDP vide")
		}
		if len(l.MDP) < 12 {
			t.Errorf("MDP trop court: %s", l.MDP)
		}
	}
}

func TestMigrationPlan(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "migration.csv")
	ecrireCSV(t, chemin, []string{
		"identifiant;ou_source;ou_cible",
		"l.martin;OU=6e,OU=Eleves,DC=X;OU=5e,OU=Eleves,DC=X",
		"j.durand;OU=5e,OU=Eleves,DC=X;OU=5e,OU=Eleves,DC=X",
		"sans.ou;;OU=5e,OU=Eleves,DC=X",
	})

	out, err := execution(t, "migration", "plan", "-f", chemin)
	if err != nil {
		t.Fatalf("migration plan: %v", err)
	}
	if !strings.Contains(out, "1 à déplacer") {
		t.Errorf("attendu 1 à déplacer, sortie: %s", out)
	}
	if !strings.Contains(out, "2 inchangés") {
		t.Errorf("attendu 2 inchangés, sortie: %s", out)
	}
}

func TestMigrationPlanJSON(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "migration.csv")
	ecrireCSV(t, chemin, []string{
		"identifiant;ou_source;ou_cible",
		"l.martin;OU=6e,OU=Eleves,DC=X;OU=5e,OU=Eleves,DC=X",
	})

	out, err := execution(t, "migration", "plan", "-f", chemin, "--json")
	if err != nil {
		t.Fatalf("migration plan --json: %v", err)
	}
	var r struct {
		Lignes []struct {
			Action string `json:"action"`
		} `json:"lignes"`
		WillMove int `json:"will_move"`
		WillSkip int `json:"will_skip"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.WillMove != 1 {
		t.Errorf("will_move = %d, attendu 1", r.WillMove)
	}
	if r.WillSkip != 0 {
		t.Errorf("will_skip = %d, attendu 0", r.WillSkip)
	}
	if len(r.Lignes) != 1 || r.Lignes[0].Action != "move" {
		t.Errorf("ligne inattendue: %+v", r.Lignes)
	}
}

func TestExportFields(t *testing.T) {
	out, err := execution(t, "export", "fields")
	if err != nil {
		t.Fatalf("export fields: %v", err)
	}
	if !strings.Contains(out, "sAMAccountName") {
		t.Error("champ sAMAccountName absent")
	}
	if !strings.Contains(out, "L7160") || !strings.Contains(out, "L7163") {
		t.Error("modèles d'étiquettes absents")
	}
}

func TestExportFieldsJSON(t *testing.T) {
	out, err := execution(t, "export", "fields")
	if err != nil {
		t.Fatalf("export fields: %v", err)
	}
	var r resultatExportFields
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if len(r.Champs) == 0 {
		t.Error("aucuns champs retournés")
	}
	if len(r.Templates) != 2 {
		t.Errorf("templates = %d, attendu 2", len(r.Templates))
	}
	trouveSAM := false
	for _, c := range r.Champs {
		if c.Nom == "sAMAccountName" {
			trouveSAM = true
		}
	}
	if !trouveSAM {
		t.Error("champ sAMAccountName manquant")
	}
}
