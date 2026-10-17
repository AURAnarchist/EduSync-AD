package classspaces

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	groupeDN = "CN=6emeA,OU=Classes,DC=lycee,DC=local"
	grpProfs = "CN=Profs,DC=lycee,DC=local"
	ouProfs  = "OU=Profs,DC=lycee,DC=local"
	eleve    = "CN=Alice Martin,OU=Classes,DC=lycee,DC=local"
	prof     = "CN=Jean Durand,OU=Profs,DC=lycee,DC=local"
)

func TestConfigResolvePath(t *testing.T) {
	cfg := ConfigParDefaut()
	if got := cfg.ResolvePath("6emeA"); got != `\\srv\classes\6emeA` {
		t.Errorf("ResolvePath = %q, attendu %q", got, `\\srv\classes\6emeA`)
	}

	cfg = Config{PartageRacine: `\\fs01\partages\\`, ModeleDossier: `Classes\%GROUP%`}
	if got := cfg.ResolvePath("6emeB"); got != `\\fs01\partages\Classes\6emeB` {
		t.Errorf("ResolvePath personnalisé = %q", got)
	}
}

func TestConfigValidate(t *testing.T) {
	if errs := ConfigParDefaut().Validate(); len(errs) != 0 {
		t.Errorf("config par défaut invalide : %v", errs)
	}
	if errs := (Config{PartageRacine: " "}).Validate(); len(errs) == 0 {
		t.Error("partage racine vide attendu en erreur")
	}
	if errs := (Config{PartageRacine: "D:/classes", ModeleDossier: "%GROUP%"}).Validate(); !containsMot(errs, "UNC") {
		t.Errorf("chemin non UNC attendu en erreur : %v", errs)
	}
	if errs := (Config{PartageRacine: `\\srv\c`, ModeleDossier: "classes"}).Validate(); !containsMot(errs, "%GROUP%") {
		t.Errorf("modèle sans %%GROUP%% attendu en erreur : %v", errs)
	}
}

func containsMot(liste []string, mot string) bool {
	for _, s := range liste {
		if strings.Contains(s, mot) {
			return true
		}
	}
	return false
}

// TestConfigJSONParity vérifie que les clés JSON correspondent à la version Python.
func TestConfigJSONParity(t *testing.T) {
	donnees, err := json.Marshal(ConfigParDefaut())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, cle := range []string{"share_root", "folder_template", "teachers_group", "teachers_ou", "auto_on_group_create"} {
		if !strings.Contains(string(donnees), `"`+cle+`"`) {
			t.Errorf("clé JSON %q absente : %s", cle, donnees)
		}
	}
}

func TestClasserMembre(t *testing.T) {
	cfg := Config{GroupeProfs: grpProfs}
	if got := ClasserMembre(prof, []string{grpProfs}, cfg); got != RoleProf {
		t.Errorf("membre du groupe profs = %q, attendu prof", got)
	}

	cfg = Config{OUProfs: ouProfs}
	if got := ClasserMembre(prof, nil, cfg); got != RoleProf {
		t.Errorf("membre de l'OU profs = %q, attendu prof", got)
	}

	// Insensible à la casse sur l'OU.
	cfg = Config{OUProfs: strings.ToUpper(ouProfs)}
	if got := ClasserMembre(strings.ToLower(prof), nil, cfg); got != RoleProf {
		t.Errorf("OU insensible à la casse = %q, attendu prof", got)
	}

	cfg = Config{GroupeProfs: grpProfs, OUProfs: ouProfs}
	if got := ClasserMembre(eleve, nil, cfg); got != RoleEleve {
		t.Errorf("élève = %q, attendu eleve", got)
	}

	// Aucune règle → eleve.
	if got := ClasserMembre(eleve, nil, ConfigParDefaut()); got != RoleEleve {
		t.Errorf("sans règle = %q, attendu eleve", got)
	}
}

func TestPlanShareNameEtMembres(t *testing.T) {
	plan := Plan{
		GroupDN:   groupeDN,
		GroupName: "6emeA",
		Path:      `\\srv\classes\6emeA`,
		Profs:     []string{"jdurand"},
		Eleves:    []string{"amartin"},
	}
	if got := plan.ShareName(); got != "6emeA" {
		t.Errorf("ShareName = %q, attendu 6emeA", got)
	}
	membres := plan.Membres()
	if len(membres) != 2 || membres[0] != "jdurand" || membres[1] != "amartin" {
		t.Errorf("Membres = %v, attendu [jdurand amartin]", membres)
	}
}

func TestPlanifierSync(t *testing.T) {
	diff := PlanifierSync([]string{"a", "b", "c"}, []string{"b", "c", "d"})
	if strings.Join(diff.Ajoutes, ",") != "d" {
		t.Errorf("Ajoutes = %v", diff.Ajoutes)
	}
	if strings.Join(diff.Retires, ",") != "a" {
		t.Errorf("Retires = %v", diff.Retires)
	}
	if strings.Join(diff.Inchanges, ",") != "b,c" {
		t.Errorf("Inchanges = %v", diff.Inchanges)
	}

	diff = PlanifierSync([]string{}, []string{"x", "y"})
	if strings.Join(diff.Ajoutes, ",") != "x,y" || len(diff.Retires) != 0 {
		t.Errorf("premier sync = %+v", diff)
	}
}

func planTest() Plan {
	return Plan{
		GroupDN:   groupeDN,
		GroupName: "6emeA",
		Path:      `\\srv\classes\6emeA`,
		Profs:     []string{"jdurand"},
		Eleves:    []string{"amartin", "bbernard"},
	}
}

func TestScriptCreation(t *testing.T) {
	script := ScriptCreation([]Plan{planTest()}, "lycee.local")
	for _, attendu := range []string{
		"New-Item",
		"New-SmbShare",
		"-Name '6emeA'",
		`\\srv\classes\6emeA`,
		`lycee.local\jdurand:(OI)(CI)Modify`,
		`lycee.local\amartin:(OI)(CI)ReadAndExecute`,
		`BUILTIN\Administrators:(OI)(CI)FullControl`,
		`NT AUTHORITY\SYSTEM:(OI)(CI)FullControl`,
	} {
		if !strings.Contains(script, attendu) {
			t.Errorf("script de création ne contient pas %q\n%s", attendu, script)
		}
	}
	if !strings.HasPrefix(script, "#Requires -RunAsAdministrator") {
		t.Error("le script doit commencer par #Requires -RunAsAdministrator")
	}
}

func TestScriptCreationMultiPlans(t *testing.T) {
	plans := []Plan{
		planTest(),
		{GroupDN: groupeDN, GroupName: "5emeB", Path: `\\srv\classes\5emeB`},
	}
	script := ScriptCreation(plans, "lycee.local")
	if n := strings.Count(script, "New-SmbShare"); n != 2 {
		t.Errorf("New-SmbShare = %d occurrences, attendu 2", n)
	}
	if !strings.Contains(script, "5emeB") {
		t.Error("le second espace doit apparaître")
	}
}

func TestScriptSync(t *testing.T) {
	script := ScriptSync(planTest(), "lycee.local", "", "")
	for _, attendu := range []string{
		"Import-Module ActiveDirectory",
		"Get-ADGroupMember",
		groupeDN,
		"Modify",
		"ReadAndExecute",
		"Get-Acl",
		"Set-Acl",
		"RemoveAccessRule",
		"AddAccessRule",
		"S-1-5-32-544",
		"S-1-5-18",
		"IsInherited",
		"$teachersGroup = $null",
		"$teachersOU = $null",
	} {
		if !strings.Contains(script, attendu) {
			t.Errorf("script de synchro ne contient pas %q", attendu)
		}
	}
}

func TestScriptSyncReglesProfs(t *testing.T) {
	script := ScriptSync(planTest(), "lycee.local", grpProfs, ouProfs)
	if !strings.Contains(script, grpProfs) {
		t.Error("le groupe des profs doit être injecté")
	}
	if !strings.Contains(script, strings.ToLower(ouProfs)) {
		t.Error("l'OU des profs doit être injectée en minuscules")
	}
}

func TestPersistanceConfig(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{PartageRacine: `\\fs\cls`, ModeleDossier: "%GROUP%", AutoSurCreation: false}
	if err := EnregistrerConfig(dir, cfg); err != nil {
		t.Fatalf("EnregistrerConfig: %v", err)
	}
	if got := ChargerConfig(dir); got != cfg {
		t.Errorf("config relue = %+v, attendu %+v", got, cfg)
	}

	// Fichier absent.
	if got := ChargerConfig(t.TempDir()); got != ConfigParDefaut() {
		t.Errorf("fichier absent = %+v, attendu défaut", got)
	}

	// Fichier corrompu.
	mauvais := t.TempDir()
	if err := os.WriteFile(filepath.Join(mauvais, FichierConfig), []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := ChargerConfig(mauvais); got != ConfigParDefaut() {
		t.Errorf("fichier corrompu = %+v, attendu défaut", got)
	}
}

func TestPersistanceEtat(t *testing.T) {
	dir := t.TempDir()
	if err := MarquerSynchronise(dir, groupeDN, []string{"b", "a"}); err != nil {
		t.Fatalf("MarquerSynchronise: %v", err)
	}
	etat := ChargerEtat(dir)
	entree, ok := etat[groupeDN]
	if !ok {
		t.Fatal("entrée absente de l'état")
	}
	if strings.Join(entree.Members, ",") != "a,b" {
		t.Errorf("membres = %v, attendu [a b]", entree.Members)
	}
	if entree.Pending {
		t.Error("l'espace synchronisé ne doit plus être en attente")
	}
}

func TestNotifierCreationGroupe(t *testing.T) {
	dir := t.TempDir()

	// Option désactivée.
	if err := EnregistrerConfig(dir, Config{PartageRacine: `\\srv\classes`, ModeleDossier: "%GROUP%", AutoSurCreation: false}); err != nil {
		t.Fatal(err)
	}
	if _, ok := NotifierCreationGroupe(dir, groupeDN, "6emeA"); ok {
		t.Error("option désactivée : aucune création attendue")
	}
	if len(ChargerEtat(dir)) != 0 {
		t.Error("option désactivée : aucun état attendu")
	}

	// Option activée.
	if err := EnregistrerConfig(dir, ConfigParDefaut()); err != nil {
		t.Fatal(err)
	}
	path, ok := NotifierCreationGroupe(dir, groupeDN, "6emeA")
	if !ok || path != `\\srv\classes\6emeA` {
		t.Errorf("chemin = %q (ok=%v)", path, ok)
	}
	etat := ChargerEtat(dir)
	entree := etat[groupeDN]
	if !entree.Pending || entree.Path != path || entree.GroupName != "6emeA" {
		t.Errorf("état = %+v", entree)
	}

	// Idempotent.
	NotifierCreationGroupe(dir, groupeDN, "6emeA")
	if len(ChargerEtat(dir)) != 1 {
		t.Errorf("l'appel doit rester idempotent, état = %d entrées", len(ChargerEtat(dir)))
	}
}

func TestNotifierNomDepuisDN(t *testing.T) {
	dir := t.TempDir()
	if err := EnregistrerConfig(dir, ConfigParDefaut()); err != nil {
		t.Fatal(err)
	}
	path, ok := NotifierCreationGroupe(dir, "CN=5emeB,OU=Classes,DC=lycee,DC=local", "")
	if !ok || path != `\\srv\classes\5emeB` {
		t.Errorf("chemin = %q, attendu %q", path, `\\srv\classes\5emeB`)
	}
}

func TestNomGroupeDepuisDN(t *testing.T) {
	if got := NomGroupeDepuisDN("CN=6emeA,OU=Classes,DC=lycee,DC=local"); got != "6emeA" {
		t.Errorf("NomGroupeDepuisDN = %q", got)
	}
	if got := NomGroupeDepuisDN("6emeA"); got != "6emeA" {
		t.Errorf("NomGroupeDepuisDN sans DN = %q", got)
	}
}
