package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/exchange"
)

// comptesJSON écrit un fichier de comptes de test et retourne son chemin.
func comptesJSON(t *testing.T, dir string) string {
	t.Helper()
	chemin := filepath.Join(dir, "users.json")
	contenu := `[
		{"sam":"jdurand","cn":"Jean Durand","dn":"CN=Jean Durand,OU=Profs,DC=lycee,DC=local"},
		{"sam":"amartin","cn":"Alice Martin (amartin)","dn":"CN=Alice Martin,OU=6A,DC=lycee,DC=local"}
	]`
	if err := os.WriteFile(chemin, []byte(contenu), 0o600); err != nil {
		t.Fatal(err)
	}
	return chemin
}

func TestExchangePlans(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	src := comptesJSON(t, dir)

	out, err := execution(t, "exchange", "plans", "--src", src, "--smtp-domain", "lycee.fr",
		"--domain", "lycee.local", "--json")
	if err != nil {
		t.Fatalf("plans: %v", err)
	}
	var plans []exchange.PlanBal
	if err := json.Unmarshal([]byte(out), &plans); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(plans) != 2 {
		t.Fatalf("plans = %d", len(plans))
	}
	if plans[0].Primaire != "jdurand@lycee.fr" || plans[0].UPN != "jdurand@lycee.local" {
		t.Errorf("plan 0 = %+v", plans[0])
	}
	if len(plans[0].Alias) != 1 || plans[0].Alias[0] != "jean.durand@lycee.fr" {
		t.Errorf("alias = %v", plans[0].Alias)
	}
}

func TestExchangeOnpremScript(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	src := comptesJSON(t, dir)

	out, err := execution(t, "exchange", "onprem-script", "--src", src,
		"--smtp-domain", "lycee.fr", "--exchange-server", "exch01.lycee.local",
		"--domain", "lycee.local")
	if err != nil {
		t.Fatalf("onprem-script: %v", err)
	}
	for _, attendu := range []string{
		"#Requires -RunAsAdministrator",
		"New-PSSession -ConfigurationName Microsoft.Exchange",
		"-ComputerName 'exch01.lycee.local'",
		"-ProhibitSendReceiveQuota '10GB'",
	} {
		if !strings.Contains(out, attendu) {
			t.Errorf("script sans %q", attendu)
		}
	}
}

func TestExchangeOnlineScriptFichier(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	src := comptesJSON(t, dir)
	dest := filepath.Join(dir, "online.ps1")

	out, err := execution(t, "exchange", "online-script", "--src", src,
		"--smtp-domain", "lycee.fr", "--tenant-id", "11111111-2222-3333-4444-555555555555",
		"--client-id", "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"--certificate-thumbprint", "ABC123DEF456", "--organization", "lycee.onmicrosoft.com",
		"--dest", dest)
	if err != nil {
		t.Fatalf("online-script: %v", err)
	}
	if !strings.Contains(out, dest) {
		t.Errorf("confirmation absente : %q", out)
	}
	donnees, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if donnees[0] != 0xEF || donnees[1] != 0xBB || donnees[2] != 0xBF {
		t.Error("BOM UTF-8 absent")
	}
	if !strings.Contains(string(donnees), "Connect-ExchangeOnline") {
		t.Error("connexion app-only absente")
	}
}

func TestExchangePolicyScript(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	out, err := execution(t, "exchange", "policy-script", "--smtp-domain", "lycee.fr",
		"--exchange-server", "exch01.lycee.local")
	if err != nil {
		t.Fatalf("policy-script: %v", err)
	}
	for _, attendu := range []string{"New-EmailAddressPolicy", "Update-EmailAddressPolicy", "-IncludedDomains 'lycee.fr'"} {
		if !strings.Contains(out, attendu) {
			t.Errorf("politique sans %q", attendu)
		}
	}
}

func TestExchangeReport(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	src := comptesJSON(t, dir)
	dest := filepath.Join(dir, "adresses.csv")

	if _, err := execution(t, "exchange", "report", "--src", src, "--smtp-domain", "lycee.fr",
		"--domain", "lycee.local", "--dest", dest); err != nil {
		t.Fatalf("report: %v", err)
	}
	donnees, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	contenu := strings.TrimPrefix(string(donnees), "\xEF\xBB\xBF")
	lignes := strings.Split(strings.TrimRight(contenu, "\n"), "\n")
	if lignes[0] != "Identifiant;Type;SMTP principal;Aliases" {
		t.Errorf("en-tête = %q", lignes[0])
	}
	if !strings.Contains(lignes[1], "jdurand;user;jdurand@lycee.fr;jean.durand@lycee.fr") {
		t.Errorf("ligne 1 = %q", lignes[1])
	}
}

func TestExchangeConfig(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := execution(t, "exchange", "config", "--json")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var cfg exchange.Config
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if cfg.Mode != "onprem" || cfg.MailboxQuota != "10GB" {
		t.Errorf("config par défaut = %+v", cfg)
	}

	if _, err := execution(t, "exchange", "config", "--smtp-domain", "lycee.fr",
		"--exchange-server", "exch01.lycee.local", "--save"); err != nil {
		t.Fatalf("sauvegarde: %v", err)
	}
	out, err = execution(t, "exchange", "config", "--json")
	if err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.SMTPDomain != "lycee.fr" || cfg.ExchangeServer != "exch01.lycee.local" {
		t.Errorf("config relue = %+v", cfg)
	}
}

func TestExchangeSousCommandeInconnue(t *testing.T) {
	if _, err := execution(t, "exchange", "bogus"); err == nil {
		t.Error("sous-commande inconnue acceptée")
	}
}
