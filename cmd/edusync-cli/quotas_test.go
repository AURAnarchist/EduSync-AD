package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestQuotasSimulate(t *testing.T) {
	out, err := execution(t, "quotas", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "disque",
		"--limite", "1073741824")
	if err != nil {
		t.Fatalf("quotas simulate: %v", err)
	}
	if !strings.Contains(out, "CN=Jean Dupont,OU=Users,DC=X") {
		t.Errorf("user-dn absent: %s", out)
	}
	if !strings.Contains(out, "disque") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestQuotasSimulateJSON(t *testing.T) {
	out, err := execution(t, "quotas", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "disque",
		"--limite", "1073741824",
		"--seuil", "alerte",
		"--json")
	if err != nil {
		t.Fatalf("quotas simulate --json: %v", err)
	}
	var r struct {
		UserDN   string `json:"UserDN"`
		OU       string `json:"OU"`
		Type     string `json:"Type"`
		Limite   int64  `json:"Limite"`
		Seuil    string `json:"Seuil"`
		Statut   string `json:"Statut"`
		Detaille string `json:"Detaille"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.UserDN != "CN=Jean Dupont,OU=Users,DC=X" {
		t.Errorf("UserDN = %q", r.UserDN)
	}
	if r.Type != "disque" {
		t.Errorf("Type = %q, attendu disque", r.Type)
	}
	if r.Limite != 1073741824 {
		t.Errorf("Limite = %d, attendu 1073741824", r.Limite)
	}
	if r.Statut != "attente_ad" {
		t.Errorf("Statut = %q, attendu attente_ad", r.Statut)
	}
}

func TestQuotasExecute(t *testing.T) {
	out, err := execution(t, "quotas", "execute",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "disque",
		"--limite", "536870912")
	if err != nil {
		t.Fatalf("quotas execute: %v", err)
	}
	if !strings.Contains(out, "disque") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestQuotasSansSousCommande(t *testing.T) {
	_, err := execution(t, "quotas")
	if err == nil {
		t.Fatal("sous-commande manquante doit échouer")
	}
}

func TestQuotasSansUserDNNiOU(t *testing.T) {
	_, err := execution(t, "quotas", "simulate")
	if err == nil {
		t.Fatal("--user-dn ou --ou manquant doit échouer")
	}
}
