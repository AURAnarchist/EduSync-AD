package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHomeDirsSimulate(t *testing.T) {
	out, err := execution(t, "homedirs", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "standard",
		"--chemin", "\\\\srv\\homes\\%USERNAME%",
		"--lettre", "H")
	if err != nil {
		t.Fatalf("homedirs simulate: %v", err)
	}
	if !strings.Contains(out, "CN=Jean Dupont,OU=Users,DC=X") {
		t.Errorf("user-dn absent: %s", out)
	}
	if !strings.Contains(out, "standard") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestHomeDirsSimulateJSON(t *testing.T) {
	out, err := execution(t, "homedirs", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "personnalise",
		"--chemin", "\\\\srv\\homes\\%USERNAME%",
		"--lettre", "Z",
		"--json")
	if err != nil {
		t.Fatalf("homedirs simulate --json: %v", err)
	}
	var r struct {
		UserDN        string `json:"UserDN"`
		Type          string `json:"Type"`
		Chemin        string `json:"Chemin"`
		LettreLecteur string `json:"LettreLecteur"`
		Statut        string `json:"Statut"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.UserDN != "CN=Jean Dupont,OU=Users,DC=X" {
		t.Errorf("UserDN = %q", r.UserDN)
	}
	if r.Type != "personnalise" {
		t.Errorf("Type = %q, attendu personnalise", r.Type)
	}
	if r.Statut != "attente_ad" {
		t.Errorf("Statut = %q, attendu attente_ad", r.Statut)
	}
}

func TestHomeDirsExecute(t *testing.T) {
	out, err := execution(t, "homedirs", "execute",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "standard",
		"--chemin", "\\\\srv\\homes\\%USERNAME%",
		"--lettre", "H")
	if err != nil {
		t.Fatalf("homedirs execute: %v", err)
	}
	if !strings.Contains(out, "standard") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestHomeDirsSansSousCommande(t *testing.T) {
	_, err := execution(t, "homedirs")
	if err == nil {
		t.Fatal("sous-commande manquante doit echouer")
	}
}

func TestHomeDirsSansUserDN(t *testing.T) {
	_, err := execution(t, "homedirs", "simulate")
	if err == nil {
		t.Fatal("--user-dn manquant doit echouer")
	}
}
