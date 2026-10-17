package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/rds"
)

func TestRDSCollectionScript(t *testing.T) {
	out, err := execution(t, "rds", "collection-script",
		"--broker", "rdcb.lycee.local", "--domain", "LYCEE",
		"--name", "Edu-Sciences", "--description", "Salle PC sciences",
		"--hosts", "rdsh01.lycee.local,rdsh02.lycee.local", "--groups", "Profs;Eleves")
	if err != nil {
		t.Fatalf("collection-script: %v", err)
	}
	for _, attendu := range []string{
		"#Requires -RunAsAdministrator",
		"New-RDSessionCollection -CollectionName $collectionName",
		"-SessionHost @('rdsh01.lycee.local', 'rdsh02.lycee.local')",
		"-UserGroup @('LYCEE\\Profs', 'LYCEE\\Eleves')",
	} {
		if !strings.Contains(out, attendu) {
			t.Errorf("script sans %q", attendu)
		}
	}
}

func TestRDSCollectionScriptFichier(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "collections.ps1")
	out, err := execution(t, "rds", "collection-script",
		"--broker", "rdcb.lycee.local", "--name", "C1", "--hosts", "h1", "--dest", dest)
	if err != nil {
		t.Fatalf("collection-script: %v", err)
	}
	if !strings.Contains(out, dest) {
		t.Errorf("confirmation absente : %q", out)
	}
	donnees, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if len(donnees) < 3 || donnees[0] != 0xEF || donnees[1] != 0xBB || donnees[2] != 0xBF {
		t.Error("BOM UTF-8 absent")
	}
}

func TestRDSCollectionScriptErreurs(t *testing.T) {
	if _, err := execution(t, "rds", "collection-script", "--broker", "b", "--hosts", "h1"); err == nil {
		t.Error("nom de collection manquant accepté")
	}
	if _, err := execution(t, "rds", "collection-script", "--name", "C1", "--hosts", "h1"); err == nil {
		t.Error("broker manquant accepté")
	}
}

func TestRDSRemoteAppScript(t *testing.T) {
	out, err := execution(t, "rds", "remoteapp-script",
		"--broker", "rdcb.lycee.local", "--domain", "LYCEE",
		"--display-name", "Word 2021", "--file-path", `C:\Program Files\MSOffice\WINWORD.EXE`,
		"--collection", "Edu-Sciences", "--groups", "Profs", "--folder", "Bureautique", "--icon", "3")
	if err != nil {
		t.Fatalf("remoteapp-script: %v", err)
	}
	for _, attendu := range []string{
		"$alias = 'word-2021'",
		"-DisplayName 'Word 2021'",
		"-IconIndex 3",
		"-ShowInWebAccess $true",
		"-UserGroups @('LYCEE\\Profs')",
	} {
		if !strings.Contains(out, attendu) {
			t.Errorf("script sans %q :\n%s", attendu, out)
		}
	}
}

func TestRDSRemoteAppScriptMasque(t *testing.T) {
	out, err := execution(t, "rds", "remoteapp-script",
		"--broker", "b", "--display-name", "App", "--file-path", `C:\a.exe`,
		"--collection", "C", "--no-show-in-web", "--required-command", "/x=1")
	if err != nil {
		t.Fatalf("remoteapp-script: %v", err)
	}
	if !strings.Contains(out, "-ShowInWebAccess $false") || !strings.Contains(out, "-RequiredCommandLine '/x=1'") {
		t.Errorf("options absentes :\n%s", out)
	}
}

func TestRDSFSLogixScript(t *testing.T) {
	out, err := execution(t, "rds", "fslogix-script",
		"--broker", "rdcb.lycee.local", "--profile-mode", "fslogix",
		"--fslogix-locations", `\\sr01\FSLogix$;\\sr02\FSLogix$`, "--fslogix-size-mb", "25000")
	if err != nil {
		t.Fatalf("fslogix-script: %v", err)
	}
	for _, attendu := range []string{
		`'HKLM:\SOFTWARE\FSLogix\Profiles'`,
		"-Name 'Enabled' -Value 1 -Type DWord",
		"-Name 'SizeInMB' -Value 25000 -Type DWord",
		`@('\\sr01\FSLogix$', '\\sr02\FSLogix$') -Type MultiString`,
	} {
		if !strings.Contains(out, attendu) {
			t.Errorf("script sans %q :\n%s", attendu, out)
		}
	}
	if _, err := execution(t, "rds", "fslogix-script", "--broker", "b"); err == nil {
		t.Error("mode non-FSLogix accepté")
	}
}

func TestRDSConfigEtPlans(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := execution(t, "rds", "config", "--json")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var cfg rds.Config
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if cfg.ProfileMode != "none" || cfg.Broker != "" {
		t.Errorf("config initiale = %+v", cfg)
	}

	if _, err := execution(t, "rds", "config", "--broker", "rdcb.lycee.local", "--domain", "LYCEE", "--save"); err != nil {
		t.Fatalf("sauvegarde: %v", err)
	}
	out, err = execution(t, "rds", "config", "--json")
	if err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Broker != "rdcb.lycee.local" || cfg.Domain != "LYCEE" {
		t.Errorf("config relue = %+v", cfg)
	}

	out, err = execution(t, "rds", "plans", "--json")
	if err != nil {
		t.Fatalf("plans: %v", err)
	}
	var plans struct {
		Collections []rds.PlanCollection `json:"collections"`
		RemoteApps  []rds.PlanRemoteApp  `json:"remoteapps"`
	}
	if err := json.Unmarshal([]byte(out), &plans); err != nil {
		t.Fatalf("JSON plans: %v", err)
	}
	if len(plans.Collections) != 0 || len(plans.RemoteApps) != 0 {
		t.Errorf("plans initiaux = %+v", plans)
	}
}

func TestRDSSousCommandeInconnue(t *testing.T) {
	if _, err := execution(t, "rds", "bogus"); err == nil {
		t.Error("sous-commande inconnue acceptée")
	}
}
