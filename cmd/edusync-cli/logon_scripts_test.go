package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogonScriptsSimulate(t *testing.T) {
	out, err := execution(t, "logon-scripts", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "bat",
		"--contenu", "echo hello")
	if err != nil {
		t.Fatalf("logon-scripts simulate: %v", err)
	}
	if !strings.Contains(out, "CN=Jean Dupont,OU=Users,DC=X") {
		t.Errorf("user-dn absent: %s", out)
	}
	if !strings.Contains(out, "bat") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestLogonScriptsSimulateJSON(t *testing.T) {
	out, err := execution(t, "logon-scripts", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "powershell",
		"--contenu", "Write-Host hello",
		"--json")
	if err != nil {
		t.Fatalf("logon-scripts simulate --json: %v", err)
	}
	var r struct {
		UserDN   string `json:"UserDN"`
		Type     string `json:"Type"`
		Statut   string `json:"Statut"`
		Detaille string `json:"Detaille"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.UserDN != "CN=Jean Dupont,OU=Users,DC=X" {
		t.Errorf("UserDN = %q", r.UserDN)
	}
	if r.Type != "powershell" {
		t.Errorf("Type = %q, attendu powershell", r.Type)
	}
	if r.Statut != "attente_ad" {
		t.Errorf("Statut = %q, attendu attente_ad", r.Statut)
	}
}

func TestLogonScriptsExecute(t *testing.T) {
	out, err := execution(t, "logon-scripts", "execute",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "vbs",
		"--contenu", "MsgBox hello")
	if err != nil {
		t.Fatalf("logon-scripts execute: %v", err)
	}
	if !strings.Contains(out, "vbs") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestLogonScriptsSansSousCommande(t *testing.T) {
	_, err := execution(t, "logon-scripts")
	if err == nil {
		t.Fatal("sous-commande manquante doit échouer")
	}
}

func TestLogonScriptsSansUserDN(t *testing.T) {
	_, err := execution(t, "logon-scripts", "simulate")
	if err == nil {
		t.Fatal("--user-dn manquant doit échouer")
	}
}

func TestLogonScriptsSansContenu(t *testing.T) {
	_, err := execution(t, "logon-scripts", "simulate",
		"--user-dn", "CN=Test,OU=Users,DC=X")
	if err == nil {
		t.Fatal("--contenu manquant doit échouer")
	}
}
