package rds

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ─── Fixtures ─────────────────────────────────────────────────────────────

func configUPD() Config {
	cfg := ConfigParDefaut()
	cfg.Broker = "rdcb.lycee.local"
	cfg.Domain = "LYCEE"
	cfg.ProfileMode = "upd"
	cfg.UPDPath = `\\sr01\UPD$`
	cfg.UPDSizeGB = 25
	cfg.UPDIncludePaths = []string{`AppData\Roaming`}
	cfg.UPDExcludePaths = []string{`AppData\Local\Temp`}
	return cfg
}

func configSimple() Config {
	cfg := ConfigParDefaut()
	cfg.Broker = "rdcb.lycee.local"
	cfg.Domain = "LYCEE"
	return cfg
}

func configFSLogix() Config {
	cfg := ConfigParDefaut()
	cfg.Broker = "rdcb.lycee.local"
	cfg.ProfileMode = "fslogix"
	cfg.FSLogixLocations = []string{`\\sr01\FSLogix$`, `\\sr02\FSLogix$`}
	cfg.FSLogixSizeMB = 25000
	cfg.FSLogixFlipFlop = true
	return cfg
}

func planExemple(cfg Config) PlanCollection {
	return ConstruirePlanCollection(
		"Edu-Sciences", "Salle PC sciences",
		[]string{"rdsh01.lycee.local", "rdsh01.lycee.local", "rdsh02.lycee.local;"},
		[]string{"Profs", `LYCEE\Eleves`},
		cfg,
	)
}

func appExemple() PlanRemoteApp {
	icone := 3
	return PlanRemoteApp{
		Alias:       "winword",
		DisplayName: "Word 2021",
		FilePath:    `C:\Program Files\MSOffice\WINWORD.EXE`,
		Collection:  "Edu-Sciences",
		UserGroups:  []string{`LYCEE\Profs`},
		ShowInWeb:   true,
		FolderName:  "Bureautique",
		IconIndex:   &icone,
	}
}

// ─── Configuration ────────────────────────────────────────────────────────

func TestConfigValideSansProfil(t *testing.T) {
	cfg := Config{ProfileMode: "none", Broker: "rdcb.lycee.local"}
	if errs := cfg.Validate(); len(errs) != 0 {
		t.Errorf("config simple rejetée : %v", errs)
	}
}

func TestConfigValidate(t *testing.T) {
	cas := []struct {
		nom     string
		cfg     Config
		attendu string
	}{
		{"broker manquant", Config{}, "Broker"},
		{"broker avec espace", Config{Broker: "rd cb.local"}, "FQDN"},
		{"upd non-unc", func() Config { c := configUPD(); c.UPDPath = `E:\UPD`; return c }(), "UNC"},
		{"upd taille nulle", func() Config { c := configUPD(); c.UPDSizeGB = 0; return c }(), "supérieure à 0"},
		{"fslogix sans lieu", func() Config { c := configFSLogix(); c.FSLogixLocations = nil; return c }(), "VHDLocations"},
		{"fslogix non-unc", func() Config { c := configFSLogix(); c.FSLogixLocations = []string{"/srv/x"}; return c }(), "non-UNC"},
		{"mode inconnu", Config{Broker: "b", ProfileMode: "citrix"}, "Mode de profil"},
	}
	for _, c := range cas {
		errs := c.cfg.Validate()
		trouve := false
		for _, e := range errs {
			if strings.Contains(e, c.attendu) {
				trouve = true
			}
		}
		if !trouve {
			t.Errorf("%s : %q absent de %v", c.nom, c.attendu, errs)
		}
	}
}

func TestConfigPersistence(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, FichierConfig)
	cfg := configUPD()
	cfg.Broker = "b2.lycee.local"
	if err := SauvegarderConfig(chemin, cfg); err != nil {
		t.Fatalf("SauvegarderConfig: %v", err)
	}
	if relu := ChargerConfig(chemin); !reflect.DeepEqual(relu, cfg) {
		t.Errorf("round-trip : %+v ≠ %+v", relu, cfg)
	}
}

func TestConfigAbsentEtCorrompu(t *testing.T) {
	dir := t.TempDir()
	if cfg := ChargerConfig(filepath.Join(dir, "absent.json")); !reflect.DeepEqual(cfg, ConfigParDefaut()) {
		t.Errorf("fichier absent : %+v", cfg)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := ChargerConfig(bad); !reflect.DeepEqual(cfg, ConfigParDefaut()) {
		t.Errorf("fichier corrompu : %+v", cfg)
	}
}

func TestConfigDepuisJSONPartiel(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, FichierConfig)
	if err := os.WriteFile(chemin, []byte(`{"broker":"b1","unknown":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := ChargerConfig(chemin)
	if cfg.Broker != "b1" || cfg.ProfileMode != "none" {
		t.Errorf("config partielle : %+v", cfg)
	}
}

func TestModesProfil(t *testing.T) {
	for _, mode := range []string{"none", "upd", "fslogix"} {
		if _, ok := ModesProfil[mode]; !ok {
			t.Errorf("mode %q manquant", mode)
		}
	}
	if len(ModesProfil) != 3 {
		t.Errorf("modes = %d", len(ModesProfil))
	}
}

// ─── Utilitaires ──────────────────────────────────────────────────────────

func TestSplitMulti(t *testing.T) {
	got := SplitMulti("a ; b, c;; a")
	if len(got) != 3 || got[0] != "a" || got[1] != "b" || got[2] != "c" {
		t.Errorf("SplitMulti = %v", got)
	}
	if got := SplitMulti(""); len(got) != 0 {
		t.Errorf("SplitMulti(\"\") = %v", got)
	}
}

func TestNormaliserRefGroupe(t *testing.T) {
	cas := []struct{ groupe, domaine, attendu string }{
		{"Eleves", "LYCEE", `LYCEE\Eleves`},
		{`LYCEE\Profs`, "", `LYCEE\Profs`},
		{`DOM\Groupe`, "LYCEE", `DOM\Groupe`},
		{"", "LYCEE", ""},
	}
	for _, c := range cas {
		if got := NormaliserRefGroupe(c.groupe, c.domaine); got != c.attendu {
			t.Errorf("NormaliserRefGroupe(%q,%q) = %q, attendu %q", c.groupe, c.domaine, got, c.attendu)
		}
	}
}

func TestCreerAlias(t *testing.T) {
	cas := []struct{ entree, attendu string }{
		{"Micro-Soft Office!", "micro-soft-office"},
		{"Salle Info 2", "salle-info-2"},
		{"  Déjà Vu  ", "d-j-vu"},
		{"###", ""},
	}
	for _, c := range cas {
		if got := CreerAlias(c.entree); got != c.attendu {
			t.Errorf("CreerAlias(%q) = %q, attendu %q", c.entree, got, c.attendu)
		}
	}
}

// ─── Plan de collection ───────────────────────────────────────────────────

func TestPlanCollectionDedoublonnageEtNormalisation(t *testing.T) {
	plan := planExemple(configUPD())
	if len(plan.SessionHosts) != 2 || plan.SessionHosts[0] != "rdsh01.lycee.local" || plan.SessionHosts[1] != "rdsh02.lycee.local" {
		t.Errorf("hôtes = %v", plan.SessionHosts)
	}
	if len(plan.UserGroups) != 2 || plan.UserGroups[0] != `LYCEE\Profs` || plan.UserGroups[1] != `LYCEE\Eleves` {
		t.Errorf("groupes = %v", plan.UserGroups)
	}
}

func TestPlanCollectionLabel(t *testing.T) {
	label := planExemple(configUPD()).Label()
	for _, attendu := range []string{"Edu-Sciences", "2 hôte(s)", "2 groupe(s)"} {
		if !strings.Contains(label, attendu) {
			t.Errorf("label %q sans %q", label, attendu)
		}
	}
}

func TestValiderCollection(t *testing.T) {
	if errs := ValiderCollection(planExemple(configUPD()), configSimple()); len(errs) != 0 {
		t.Errorf("plan valide rejeté : %v", errs)
	}
	errs := ValiderCollection(PlanCollection{}, configSimple())
	joint := strings.Join(errs, " ")
	if !strings.Contains(joint, "Nom de collection") || !strings.Contains(joint, "hébergeur") {
		t.Errorf("erreurs = %v", errs)
	}
	if bad := ValiderCollection(planExemple(configUPD()), Config{}); !contientContient(bad, "Broker") {
		t.Errorf("broker vide non détecté : %v", bad)
	}
}

// ─── Script de collection ─────────────────────────────────────────────────

func TestScriptCollections(t *testing.T) {
	script, err := ScriptCollections([]PlanCollection{planExemple(configUPD())}, configUPD())
	if err != nil {
		t.Fatalf("ScriptCollections: %v", err)
	}
	attendus := []string{
		"#Requires -RunAsAdministrator",
		"$broker = 'rdcb.lycee.local'",
		"Collections : 1",
		"Get-RDSessionCollection -ConnectionBroker $broker",
		"Where-Object { $_.CollectionName -eq $collectionName }",
		"New-RDSessionCollection -CollectionName $collectionName",
		"-CollectionDescription 'Salle PC sciences'",
		"-SessionHost @('rdsh01.lycee.local', 'rdsh02.lycee.local')",
		"} else {",
		"Get-RDSessionHost -CollectionName $collectionName",
		"New-RDSessionHost -CollectionName $collectionName",
		"if ($hosts -notcontains $h)",
		"-UserGroup @('LYCEE\\Profs', 'LYCEE\\Eleves')",
		"Domain Users",
		"-EnableUserProfileDisk",
		"-MaxUserProfileDiskSizeGB 25",
		`-DiskPath '\\sr01\UPD$'`,
		"-IncludeFolderPath @('AppData\\Roaming')",
		"-ExcludeFolderPath @('AppData\\Local\\Temp')",
	}
	for _, a := range attendus {
		if !strings.Contains(script, a) {
			t.Errorf("script sans %q :\n%s", a, script)
		}
	}
	if !strings.HasSuffix(strings.TrimRight(script, "\n"), "'Collections RDS configurées.'") {
		t.Errorf("fin inattendue : %q", script)
	}
}

func TestScriptCollectionsSansDescriptionNiGroupe(t *testing.T) {
	plan := ConstruirePlanCollection("C1", "", []string{"rdsh01.lycee.local"}, nil, configSimple())
	script, err := ScriptCollections([]PlanCollection{plan}, configSimple())
	if err != nil {
		t.Fatalf("ScriptCollections: %v", err)
	}
	if strings.Contains(script, "-CollectionDescription") {
		t.Error("description vide exportée")
	}
	if strings.Contains(script, "-UserGroup") {
		t.Error("groupe vide exporté")
	}
	if strings.Contains(script, "-EnableUserProfileDisk") || !strings.Contains(script, "profils : Aucun") {
		t.Error("UPD présent alors que le mode est « none »")
	}
}

func TestScriptCollectionsMultiples(t *testing.T) {
	plans := []PlanCollection{
		planExemple(configUPD()),
		ConstruirePlanCollection("Edu-Lettres", "", []string{"rdsh03.lycee.local"}, nil, configSimple()),
	}
	script, err := ScriptCollections(plans, configSimple())
	if err != nil {
		t.Fatalf("ScriptCollections: %v", err)
	}
	if strings.Count(script, "New-RDSessionCollection") != 2 {
		t.Errorf("collections = %d", strings.Count(script, "New-RDSessionCollection"))
	}
	if !strings.Contains(script, "Collections : 2") {
		t.Error("compte de collections incorrect")
	}
}

func TestScriptCollectionsErreurs(t *testing.T) {
	if _, err := ScriptCollections(nil, configUPD()); err == nil {
		t.Error("aucune collection acceptée")
	}
	brokerVide := ConfigParDefaut()
	brokerVide.ProfileMode = "upd"
	if _, err := ScriptCollections([]PlanCollection{planExemple(configUPD())}, brokerVide); err == nil || !strings.Contains(err.Error(), "Broker") {
		t.Errorf("broker vide : %v", err)
	}
	if _, err := ScriptCollections([]PlanCollection{{Name: "X"}}, configSimple()); err == nil || !strings.Contains(err.Error(), "hébergeur") {
		t.Errorf("hébergeur manquant : %v", err)
	}
}

// ─── Script RemoteApp ─────────────────────────────────────────────────────

func TestScriptRemoteApp(t *testing.T) {
	script, err := ScriptRemoteApp([]PlanRemoteApp{appExemple()}, configSimple())
	if err != nil {
		t.Fatalf("ScriptRemoteApp: %v", err)
	}
	if !strings.Contains(script, "#>") {
		t.Fatal("en-tête absent")
	}
	corps := strings.SplitN(script, "#>", 2)[1]
	for _, a := range []string{
		"Get-RDRemoteApp -CollectionName $collectionName",
		"$_.Alias -eq $alias",
		"New-RDRemoteApp -CollectionName $collectionName -Alias $alias",
		"Set-RDRemoteApp -CollectionName $collectionName -Alias $alias",
		"-DisplayName 'Word 2021'",
		`-FilePath 'C:\Program Files\MSOffice\WINWORD.EXE'`,
		"-UserGroups @('LYCEE\\Profs')",
		"-FolderName 'Bureautique'",
		"-IconIndex 3",
		"-ShowInWebAccess $true",
	} {
		if !strings.Contains(corps, a) {
			t.Errorf("corps sans %q :\n%s", a, corps)
		}
	}
}

func TestScriptRemoteAppSansGroupeEtLigneCommande(t *testing.T) {
	app := PlanRemoteApp{
		Alias: "salle1", DisplayName: "Salle 1",
		FilePath: `\\srv\apps\client.exe`, Collection: "Edu-Sciences",
		RequiredCommand: "/server=SRV1", ShowInWeb: false,
	}
	script, err := ScriptRemoteApp([]PlanRemoteApp{app}, configSimple())
	if err != nil {
		t.Fatalf("ScriptRemoteApp: %v", err)
	}
	corps := strings.SplitN(script, "#>", 2)[1]
	if strings.Contains(corps, "-UserGroups") {
		t.Error("-UserGroups présent sans groupe")
	}
	for _, a := range []string{
		"-ShowInWebAccess $false",
		"-CommandLineSetting AllowOnlySpecifiedCommandLine",
		"-RequiredCommandLine '/server=SRV1'",
	} {
		if !strings.Contains(corps, a) {
			t.Errorf("corps sans %q", a)
		}
	}
}

func TestScriptRemoteAppErreurs(t *testing.T) {
	if _, err := ScriptRemoteApp(nil, configSimple()); err == nil {
		t.Error("aucune app acceptée")
	}
	if _, err := ScriptRemoteApp([]PlanRemoteApp{appExemple()}, Config{}); err == nil || !strings.Contains(err.Error(), "Broker") {
		t.Errorf("broker vide : %v", err)
	}
	if _, err := ScriptRemoteApp([]PlanRemoteApp{{Alias: "bad alias!"}}, configSimple()); err == nil || !strings.Contains(err.Error(), "invalide") {
		t.Errorf("alias invalide : %v", err)
	}
	app := PlanRemoteApp{Alias: "a", DisplayName: "A", FilePath: `C:\a.exe`}
	if _, err := ScriptRemoteApp([]PlanRemoteApp{app}, configSimple()); err == nil || !strings.Contains(err.Error(), "collection") {
		t.Errorf("collection manquante : %v", err)
	}
}

func TestValiderRemoteApp(t *testing.T) {
	if errs := ValiderRemoteApp(appExemple()); len(errs) != 0 {
		t.Errorf("app valide rejetée : %v", errs)
	}
	joint := strings.Join(ValiderRemoteApp(PlanRemoteApp{}), " ")
	for _, attendu := range []string{"Alias", "affichage", "exécutable", "collection"} {
		if !strings.Contains(joint, attendu) {
			t.Errorf("%q absent de %q", attendu, joint)
		}
	}
	bad := ValiderRemoteApp(PlanRemoteApp{Alias: "a b", DisplayName: "X", FilePath: `C:\x.exe`, Collection: "C"})
	if !contientContient(bad, "invalide") {
		t.Errorf("alias invalide non détecté : %v", bad)
	}
}

// ─── Script FSLogix ───────────────────────────────────────────────────────

func TestScriptFSLogix(t *testing.T) {
	script, err := ScriptFSLogix(configFSLogix())
	if err != nil {
		t.Fatalf("ScriptFSLogix: %v", err)
	}
	for _, a := range []string{
		"#Requires -RunAsAdministrator",
		`'HKLM:\SOFTWARE\FSLogix\Profiles'`,
		"New-Item -Path $path -Force",
		"-Name 'Enabled' -Value 1 -Type DWord",
		"-Name 'SizeInMB' -Value 25000 -Type DWord",
		"-Name 'DeleteLocalProfileWhenVHDShouldApply'",
		"-Name 'FlipFlopProfileDir' -Value 1 -Type DWord",
		`@('\\sr01\FSLogix$', '\\sr02\FSLogix$') -Type MultiString`,
		"redémarrage",
	} {
		if !strings.Contains(script, a) {
			t.Errorf("script sans %q :\n%s", a, script)
		}
	}
}

func TestScriptFSLogixFlipFlopOptionnel(t *testing.T) {
	cfg := configFSLogix()
	cfg.FSLogixFlipFlop = false
	script, err := ScriptFSLogix(cfg)
	if err != nil {
		t.Fatalf("ScriptFSLogix: %v", err)
	}
	if strings.Contains(script, "FlipFlopProfileDir") {
		t.Error("FlipFlopProfileDir présent alors que désactivé")
	}
}

func TestScriptFSLogixErreurs(t *testing.T) {
	if _, err := ScriptFSLogix(configUPD()); err == nil || !strings.Contains(err.Error(), "pas FSLogix") {
		t.Errorf("mode non-FSLogix : %v", err)
	}
	c1 := configFSLogix()
	c1.FSLogixLocations = nil
	if _, err := ScriptFSLogix(c1); err == nil || !strings.Contains(err.Error(), "VHD") {
		t.Errorf("lieux vides : %v", err)
	}
	c2 := configFSLogix()
	c2.FSLogixLocations = []string{"//srv/x"}
	if _, err := ScriptFSLogix(c2); err == nil || !strings.Contains(err.Error(), "non-UNC") {
		t.Errorf("lieu non-UNC : %v", err)
	}
	c3 := configFSLogix()
	c3.FSLogixSizeMB = 0
	if _, err := ScriptFSLogix(c3); err == nil || !strings.Contains(err.Error(), "supérieure") {
		t.Errorf("taille nulle : %v", err)
	}
}

// ─── Persistance des plans ────────────────────────────────────────────────

func TestPlansRoundTrip(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, FichierPlans)
	plan, app := planExemple(configUPD()), appExemple()
	if err := EnregistrerPlans(chemin, []PlanCollection{plan}, []PlanRemoteApp{app}); err != nil {
		t.Fatalf("EnregistrerPlans: %v", err)
	}
	cols, apps := ChargerPlans(chemin)
	if !reflect.DeepEqual(cols, []PlanCollection{plan}) {
		t.Errorf("collections = %+v", cols)
	}
	if !reflect.DeepEqual(apps, []PlanRemoteApp{app}) {
		t.Errorf("remoteapps = %+v", apps)
	}
}

func TestPlansAbsentsEtCorrompus(t *testing.T) {
	dir := t.TempDir()
	cols, apps := ChargerPlans(filepath.Join(dir, "absent.json"))
	if len(cols) != 0 || len(apps) != 0 {
		t.Errorf("fichier absent : %v / %v", cols, apps)
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	cols, apps = ChargerPlans(bad)
	if len(cols) != 0 || len(apps) != 0 {
		t.Errorf("fichier corrompu : %v / %v", cols, apps)
	}
}

func TestPlansClesInconnuesEtDefauts(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, FichierPlans)
	contenu := `{"collections":[{"name":"C1","zzz":1}],"remoteapps":[{"alias":"a"}]}`
	if err := os.WriteFile(chemin, []byte(contenu), 0o600); err != nil {
		t.Fatal(err)
	}
	cols, apps := ChargerPlans(chemin)
	if len(cols) != 1 || cols[0].Name != "C1" || len(cols[0].SessionHosts) != 0 {
		t.Errorf("collections = %+v", cols)
	}
	if len(apps) != 1 || !apps[0].ShowInWeb {
		t.Errorf("show_in_web par défaut devrait être true : %+v", apps)
	}
}

// ─── Sauvegarde de script ─────────────────────────────────────────────────

func TestSauvegarderScript(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "sous", "script.ps1")
	if err := SauvegarderScript(dest, "Write-Host 'ok'\n"); err != nil {
		t.Fatalf("SauvegarderScript: %v", err)
	}
	donnees, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(donnees) < 3 || donnees[0] != 0xEF || donnees[1] != 0xBB || donnees[2] != 0xBF {
		t.Errorf("BOM UTF-8 absent : % x", donnees[:3])
	}
	if !strings.Contains(string(donnees), "Write-Host 'ok'") {
		t.Errorf("contenu = %q", donnees)
	}
}

// contientContient indique si une chaîne attendue apparaît dans un slice.
func contientContient(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if strings.Contains(v, cible) {
			return true
		}
	}
	return false
}
