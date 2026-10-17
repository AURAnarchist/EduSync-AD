package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClassSpacesCreationScript(t *testing.T) {
	out, err := execution(t, "class-spaces", "creation-script",
		"--group-name", "6emeA",
		"--share-root", `\\srv\classes`,
		"--domaine", "lycee.local")
	if err != nil {
		t.Fatalf("class-spaces creation-script: %v", err)
	}
	for _, attendu := range []string{
		"#Requires -RunAsAdministrator",
		"New-SmbShare",
		"-Name '6emeA'",
		`\\srv\classes\6emeA`,
		`BUILTIN\Administrators:(OI)(CI)FullControl`,
	} {
		if !strings.Contains(out, attendu) {
			t.Errorf("script sans %q\n%s", attendu, out)
		}
	}
}

func TestClassSpacesSyncScript(t *testing.T) {
	out, err := execution(t, "class-spaces", "sync-script",
		"--group-dn", "CN=6emeA,OU=Classes,DC=lycee,DC=local",
		"--group-name", "6emeA",
		"--share-root", `\\srv\classes`)
	if err != nil {
		t.Fatalf("class-spaces sync-script: %v", err)
	}
	for _, attendu := range []string{
		"Import-Module ActiveDirectory",
		"Get-ADGroupMember",
		"Set-Acl",
		"CN=6emeA,OU=Classes,DC=lycee,DC=local",
	} {
		if !strings.Contains(out, attendu) {
			t.Errorf("script sans %q\n%s", attendu, out)
		}
	}
}

func TestClassSpacesPlan(t *testing.T) {
	out, err := execution(t, "class-spaces", "plan",
		"--group-dn", "CN=6emeA,OU=Classes,DC=lycee,DC=local",
		"--group-name", "6emeA",
		"--share-root", `\\srv\classes`,
		"--json")
	if err != nil {
		t.Fatalf("class-spaces plan: %v", err)
	}
	var r struct {
		GroupName string `json:"GroupName"`
		Path      string `json:"Path"`
		ShareName string `json:"ShareName"`
		Statut    string `json:"Statut"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.Path != `\\srv\classes\6emeA` {
		t.Errorf("Path = %q", r.Path)
	}
	if r.ShareName != "6emeA" {
		t.Errorf("ShareName = %q", r.ShareName)
	}
	if r.Statut != "attente_ad" {
		t.Errorf("Statut = %q, attendu attente_ad", r.Statut)
	}
}

func TestClassSpacesSansArgs(t *testing.T) {
	_, err := execution(t, "class-spaces")
	if err == nil {
		t.Fatal("sous-commande manquante doit échouer")
	}
}

func TestClassSpacesCreationSansGroupName(t *testing.T) {
	_, err := execution(t, "class-spaces", "creation-script", "--share-root", `\\srv\classes`)
	if err == nil {
		t.Fatal("--group-name manquant doit échouer")
	}
}
