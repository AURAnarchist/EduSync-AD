package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

// coffreArgs retourne les options --vault/--key pointant sur un coffre isolé.
func coffreArgs(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "vault.sqlite3"), filepath.Join(dir, "secret.key")
}

func TestVaultSetGet(t *testing.T) {
	chemin, cle := coffreArgs(t)

	if _, err := execution(t, "vault", "set", "--sam", "Thomas.Martin",
		"--password", "Sup3r$ecret!", "--vault", chemin, "--key", cle); err != nil {
		t.Fatalf("set: %v", err)
	}
	out, err := execution(t, "vault", "get", "--sam", "thomas.martin", "--vault", chemin, "--key", cle)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if strings.TrimSpace(out) != "Sup3r$ecret!" {
		t.Errorf("get = %q", out)
	}

	// Insensible à la casse.
	outJSON, err := execution(t, "vault", "has", "--sam", "THOMAS.MARTIN", "--vault", chemin, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("has: %v", err)
	}
	var resultat map[string]any
	if err := json.Unmarshal([]byte(outJSON), &resultat); err != nil {
		t.Fatalf("JSON: %v\n%s", err, outJSON)
	}
	if resultat["present"] != true {
		t.Errorf("has = %v", resultat)
	}
}

func TestVaultGetInconnu(t *testing.T) {
	chemin, cle := coffreArgs(t)
	if _, err := execution(t, "vault", "get", "--sam", "inconnu", "--vault", chemin, "--key", cle); err == nil {
		t.Error("get d'un compte inconnu devrait échouer")
	}
}

func TestVaultListCountDelete(t *testing.T) {
	chemin, cle := coffreArgs(t)
	for _, sam := range []string{"eleve1", "eleve2"} {
		if _, err := execution(t, "vault", "set", "--sam", sam, "--password", "x",
			"--vault", chemin, "--key", cle); err != nil {
			t.Fatalf("set %s: %v", sam, err)
		}
	}

	out, err := execution(t, "vault", "list", "--vault", chemin, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var sams []string
	if err := json.Unmarshal([]byte(out), &sams); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(sams) != 2 || sams[0] != "eleve1" || sams[1] != "eleve2" {
		t.Errorf("liste = %v", sams)
	}

	if _, err := execution(t, "vault", "delete", "--sam", "eleve1", "--vault", chemin, "--key", cle); err != nil {
		t.Fatalf("delete: %v", err)
	}
	outCount, err := execution(t, "vault", "count", "--vault", chemin, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	var compte map[string]any
	if err := json.Unmarshal([]byte(outCount), &compte); err != nil {
		t.Fatalf("JSON: %v\n%s", err, outCount)
	}
	if compte["nombre"] != float64(1) {
		t.Errorf("count = %v", compte)
	}
}

func TestVaultClearExigeConfirmation(t *testing.T) {
	chemin, cle := coffreArgs(t)
	if _, err := execution(t, "vault", "set", "--sam", "a", "--password", "x", "--vault", chemin, "--key", cle); err != nil {
		t.Fatal(err)
	}
	if _, err := execution(t, "vault", "clear", "--vault", chemin, "--key", cle); err == nil {
		t.Error("clear sans --yes devrait échouer")
	}
	out, err := execution(t, "vault", "clear", "--yes", "--vault", chemin, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("clear: %v", err)
	}
	var resultat map[string]any
	if err := json.Unmarshal([]byte(out), &resultat); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if resultat["supprimes"] != float64(1) {
		t.Errorf("clear = %v", resultat)
	}
}
