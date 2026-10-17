package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestGroupTemplatesListEtShow(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := execution(t, "group-templates", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var modeles []map[string]any
	if err := json.Unmarshal([]byte(out), &modeles); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(modeles) != 4 {
		t.Fatalf("modeles = %d", len(modeles))
	}
	if modeles[0]["id"] != "classe_eleve" {
		t.Errorf("premier modèle = %v", modeles[0]["id"])
	}

	outShow, err := execution(t, "group-templates", "show", "--id", "classe_eleve", "--json")
	if err != nil {
		t.Fatalf("show: %v", err)
	}
	var modele map[string]any
	if err := json.Unmarshal([]byte(outShow), &modele); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if modele["ou_rdn_pattern"] != "Eleves-{classe}" {
		t.Errorf("motif OU = %v", modele["ou_rdn_pattern"])
	}
	if modele["hours_preset"] != "Heures cours" {
		t.Errorf("préréglage = %v", modele["hours_preset"])
	}
}

func TestGroupTemplatesValidate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := execution(t, "group-templates", "validate", "--id", "classe_eleve", "--value", "classe=3emeA", "--json")
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	var resultats []map[string]any
	if err := json.Unmarshal([]byte(out), &resultats); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(resultats) != 1 || resultats[0]["valid"] != true {
		t.Fatalf("validation inattendue : %v", resultats)
	}

	// Valeur manquante → invalide.
	if _, err := execution(t, "group-templates", "validate", "--id", "classe_eleve", "--value", "classe=", "--json"); err == nil {
		t.Fatal("une valeur vide doit rendre le modèle invalide")
	}
}

func TestGroupTemplatesDuplicateEtRemove(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := execution(t, "group-templates", "duplicate", "--id", "classe_eleve", "--json")
	if err != nil {
		t.Fatalf("duplicate: %v", err)
	}
	var copie map[string]any
	if err := json.Unmarshal([]byte(out), &copie); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if copie["id"] != "classe_eleve-2" || copie["name"] != "Classe élève (copie)" {
		t.Fatalf("copie inattendue : %v", copie)
	}

	outList, err := execution(t, "group-templates", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var apres []map[string]any
	if err := json.Unmarshal([]byte(outList), &apres); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(apres) != 5 {
		t.Fatalf("5 modèles attendus après duplication, obtenu %d", len(apres))
	}

	if _, err := execution(t, "group-templates", "remove", "--id", "classe_eleve-2", "--json"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	outFinal, err := execution(t, "group-templates", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var finaux []map[string]any
	if err := json.Unmarshal([]byte(outFinal), &finaux); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if len(finaux) != 4 {
		t.Fatalf("4 modèles attendus après suppression, obtenu %d", len(finaux))
	}
}

func TestGroupTemplatesExportImport(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	jsonPath := filepath.Join(dir, "modele.json")
	xmlPath := filepath.Join(dir, "modele.xml")
	if _, err := execution(t, "group-templates", "export", "--id", "classe_eleve", "--file", jsonPath, "--format", "json"); err != nil {
		t.Fatalf("export json: %v", err)
	}
	if _, err := execution(t, "group-templates", "export", "--id", "classe_eleve", "--file", xmlPath, "--format", "xml"); err != nil {
		t.Fatalf("export xml: %v", err)
	}
	for _, chemin := range []string{jsonPath, xmlPath} {
		if _, err := os.Stat(chemin); err != nil {
			t.Fatalf("fichier exporté absent : %v", err)
		}
	}

	// Suppression puis réimport XML : le modèle revient.
	if _, err := execution(t, "group-templates", "remove", "--id", "classe_eleve"); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if _, err := execution(t, "group-templates", "import", "--file", xmlPath, "--format", "xml", "--json"); err != nil {
		t.Fatalf("import xml: %v", err)
	}
	out, err := execution(t, "group-templates", "show", "--id", "classe_eleve", "--json")
	if err != nil {
		t.Fatalf("show après import: %v", err)
	}
	var modele map[string]any
	if err := json.Unmarshal([]byte(out), &modele); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if modele["ou_rdn_pattern"] != "Eleves-{classe}" {
		t.Fatalf("modèle mal restauré : %v", modele["ou_rdn_pattern"])
	}
}

func TestGroupTemplatesPlan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	artefacts := filepath.Join(dir, "artefacts")

	out, err := execution(t, "group-templates", "plan",
		"--id", "classe_eleve",
		"--parent-dn", "ou=Eleves,dc=lycee,dc=local",
		"--value", "classe=3emeA",
		"--artifacts", artefacts,
		"--json")
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	var resultat map[string]any
	if err := json.Unmarshal([]byte(out), &resultat); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	attenduDN := "ou=Eleves-3emeA,ou=Eleves,dc=lycee,dc=local"
	if resultat["ou_dn"] != attenduDN {
		t.Fatalf("OU = %v", resultat["ou_dn"])
	}
	if resultat["resume"] == "" {
		t.Fatal("résumé manquant")
	}
	groupes, _ := resultat["groupes"].([]any)
	if len(groupes) != 1 {
		t.Fatalf("groupes = %v", groupes)
	}

	entrees, err := os.ReadDir(artefacts)
	if err != nil {
		t.Fatalf("artefacts: %v", err)
	}
	var noms []string
	for _, entree := range entrees {
		noms = append(noms, entree.Name())
	}
	sort.Strings(noms)
	attendus := []string{"espaces_classes.ps1", "instantiation-3emeA.json", "mapping-lecteur-h.bat", "quotas_fsr.ps1"}
	if strings.Join(noms, ",") != strings.Join(attendus, ",") {
		t.Fatalf("artefacts = %v", noms)
	}

	// Sans --scope, aucune config scopée n'est enregistrée.
	if _, err := os.Stat(filepath.Join(dir, "EduSyncAD", "profile_configs.json")); !os.IsNotExist(err) {
		t.Fatal("les configs scopées ne doivent pas être écrites sans --scope")
	}

	// Avec --scope, les configs scopées sont écrites.
	artefacts2 := filepath.Join(dir, "artefacts2")
	if _, err := execution(t, "group-templates", "plan",
		"--id", "classe_eleve",
		"--parent-dn", "ou=Eleves,dc=lycee,dc=local",
		"--value", "classe=3emeB",
		"--artifacts", artefacts2,
		"--scope",
		"--json"); err != nil {
		t.Fatalf("plan --scope: %v", err)
	}
	profils := filepath.Join(dir, "EduSyncAD", "profile_configs.json")
	if _, err := os.Stat(profils); err != nil {
		t.Fatalf("profile_configs.json attendu avec --scope : %v", err)
	}
}

func TestGroupTemplatesBuiltinsForce(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	out, err := execution(t, "group-templates", "builtins", "--force", "--json")
	if err != nil {
		t.Fatalf("builtins: %v", err)
	}
	var resultat map[string]any
	if err := json.Unmarshal([]byte(out), &resultat); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if resultat["written"] == "" {
		t.Fatal("chemin d'écriture manquant")
	}
	if _, err := os.Stat(filepath.Join(dir, "EduSyncAD", "group_templates.json")); err != nil {
		t.Fatalf("fichier de modèles absent : %v", err)
	}
}
