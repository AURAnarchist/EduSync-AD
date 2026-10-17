package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPhotosSimulate(t *testing.T) {
	out, err := execution(t, "photos", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "vignette",
		"--chemin", "/photos/jean.jpg")
	if err != nil {
		t.Fatalf("photos simulate: %v", err)
	}
	if !strings.Contains(out, "CN=Jean Dupont,OU=Users,DC=X") {
		t.Errorf("user-dn absent: %s", out)
	}
	if !strings.Contains(out, "vignette") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestPhotosSimulateJSON(t *testing.T) {
	out, err := execution(t, "photos", "simulate",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "passeport",
		"--chemin", "/photos/jean.jpg",
		"--json")
	if err != nil {
		t.Fatalf("photos simulate --json: %v", err)
	}
	var r struct {
		UserDN   string `json:"UserDN"`
		Type     string `json:"Type"`
		Chemin   string `json:"Chemin"`
		Taille   int64  `json:"Taille"`
		Statut   string `json:"Statut"`
		Detaille string `json:"Detaille"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.UserDN != "CN=Jean Dupont,OU=Users,DC=X" {
		t.Errorf("UserDN = %q", r.UserDN)
	}
	if r.Type != "passeport" {
		t.Errorf("Type = %q, attendu passeport", r.Type)
	}
	if r.Chemin != "/photos/jean.jpg" {
		t.Errorf("Chemin = %q, attendu /photos/jean.jpg", r.Chemin)
	}
	if r.Statut != "attente_ad" {
		t.Errorf("Statut = %q, attendu attente_ad", r.Statut)
	}
}

func TestPhotosExecute(t *testing.T) {
	out, err := execution(t, "photos", "execute",
		"--user-dn", "CN=Jean Dupont,OU=Users,DC=X",
		"--type", "vignette",
		"--chemin", "/photos/jean.jpg")
	if err != nil {
		t.Fatalf("photos execute: %v", err)
	}
	if !strings.Contains(out, "vignette") {
		t.Errorf("type absent: %s", out)
	}
	if !strings.Contains(out, "attente_ad") {
		t.Errorf("statut attendu attente_ad: %s", out)
	}
}

func TestPhotosSansSousCommande(t *testing.T) {
	_, err := execution(t, "photos")
	if err == nil {
		t.Fatal("sous-commande manquante doit echouer")
	}
}

func TestPhotosSansUserDN(t *testing.T) {
	_, err := execution(t, "photos", "simulate")
	if err == nil {
		t.Fatal("--user-dn manquant doit echouer")
	}
}

func TestPhotosSansChemin(t *testing.T) {
	_, err := execution(t, "photos", "simulate",
		"--user-dn", "CN=Test,OU=Users,DC=X")
	if err == nil {
		t.Fatal("--chemin manquant doit echouer")
	}
}
