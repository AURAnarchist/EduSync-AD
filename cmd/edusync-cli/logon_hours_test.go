package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestLogonHoursSimulate(t *testing.T) {
	out, err := execution(t, "logon-hours", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--preset", "cours")
	if err != nil {
		t.Fatalf("logon-hours simulate: %v", err)
	}
	if !strings.Contains(out, "CN=Jean Dupont,OU=Users,DC=X") {
		t.Errorf("user-dn absent: %s", out)
	}
	if !strings.Contains(out, "cours") {
		t.Errorf("preset absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestLogonHoursSimulateJSON(t *testing.T) {
	out, err := execution(t, "logon-hours", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--preset", "admin",
		"--json")
	if err != nil {
		t.Fatalf("logon-hours simulate --json: %v", err)
	}
	var r struct {
		UserDN   string `json:"UserDN"`
		Statut   string `json:"Statut"`
		Detaille string `json:"Detaille"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.UserDN != "CN=Jean Dupont,OU=Users,DC=X" {
		t.Errorf("UserDN = %q", r.UserDN)
	}
	if r.Statut != "attente_ad" {
		t.Errorf("Statut = %q, attendu attente_ad", r.Statut)
	}
}

func TestLogonHoursExecute(t *testing.T) {
	out, err := execution(t, "logon-hours", "execute",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--preset", "personnalise")
	if err != nil {
		t.Fatalf("logon-hours execute: %v", err)
	}
	if !strings.Contains(out, "personnalise") {
		t.Errorf("preset absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestLogonHoursSansSousCommande(t *testing.T) {
	_, err := execution(t, "logon-hours")
	if err == nil {
		t.Fatal("sous-commande manquante doit échouer")
	}
}

func TestLogonHoursSansUserDN(t *testing.T) {
	_, err := execution(t, "logon-hours", "simulate")
	if err == nil {
		t.Fatal("--user-dn manquant doit échouer")
	}
}

func TestLogonHoursPresetInvalide(t *testing.T) {
	_, err := execution(t, "logon-hours", "simulate",
		"--user-dn", "CN=Test,OU=Users,DC=X",
		"--preset", "invalide")
	if err == nil {
		t.Fatal("préréglage invalide doit échouer")
	}
}
