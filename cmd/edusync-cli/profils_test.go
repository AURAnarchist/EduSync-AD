package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestProfilsSimulate(t *testing.T) {
	out, err := execution(t, "profils", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "itinérant",
		"--chemin", "\\\\srv\\profil\\%USERNAME%")
	if err != nil {
		t.Fatalf("profils simulate: %v", err)
	}
	if !strings.Contains(out, "CN=Jean Dupont,OU=Users,DC=X") {
		t.Errorf("user-dn absent: %s", out)
	}
	if !strings.Contains(out, "itinérant") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestProfilsSimulateJSON(t *testing.T) {
	out, err := execution(t, "profils", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "local",
		"--json")
	if err != nil {
		t.Fatalf("profils simulate --json: %v", err)
	}
	var r struct {
		UserDN string `json:"UserDN"`
		Type   string `json:"Type"`
		Statut string `json:"Statut"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.UserDN != "CN=Jean Dupont,OU=Users,DC=X" {
		t.Errorf("UserDN = %q", r.UserDN)
	}
	if r.Type != "local" {
		t.Errorf("Type = %q, attendu local", r.Type)
	}
	if r.Statut != "attente_ad" {
		t.Errorf("Statut = %q, attendu attente_ad", r.Statut)
	}
}

func TestProfilsExecute(t *testing.T) {
	out, err := execution(t, "profils", "execute",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "obligatoire",
		"--chemin", "\\\\srv\\profils\\%USERNAME%.man")
	if err != nil {
		t.Fatalf("profils execute: %v", err)
	}
	if !strings.Contains(out, "obligatoire") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestProfilsSansSousCommande(t *testing.T) {
	_, err := execution(t, "profils")
	if err == nil {
		t.Fatal("sous-commande manquante doit échouer")
	}
}

func TestProfilsSansUserDN(t *testing.T) {
	_, err := execution(t, "profils", "simulate")
	if err == nil {
		t.Fatal("--user-dn manquant doit échouer")
	}
}
