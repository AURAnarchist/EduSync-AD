package main

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// peuplerCache insère quelques lignes dans un cache de test.
func peuplerCache(t *testing.T, chemin string) {
	t.Helper()
	cache, err := ad.NewADCache(chemin, 0)
	if err != nil {
		t.Fatalf("NewADCache: %v", err)
	}
	_ = cache.Close()

	db, err := sql.Open("sqlite", "file:"+chemin+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	base := "DC=lycee,DC=local"
	if _, err := db.Exec("INSERT INTO ous (dn, name, parent_dn, depth) VALUES (?, ?, ?, ?)",
		"OU=Classes,"+base, "Classes", base, 0); err != nil {
		t.Fatalf("insert OU: %v", err)
	}
	if _, err := db.Exec("INSERT INTO users (dn, sam, cn, ou_dn, user_account_control) VALUES (?, ?, ?, ?, ?)",
		"CN=Alice Martin,OU=Classes,"+base, "amartin", "Alice Martin", "OU=Classes,"+base, 512); err != nil {
		t.Fatalf("insert user: %v", err)
	}
}

func TestADCacheStatsVide(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), ad.NomFichierCache)
	out, err := execution(t, "ad-cache", "stats", "--cache", chemin, "--json")
	if err != nil {
		t.Fatalf("ad-cache stats: %v", err)
	}
	var stats ad.CacheStats
	if err := json.Unmarshal([]byte(out), &stats); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if stats.OUsCount != 0 || stats.UsersCount != 0 {
		t.Errorf("cache vide attendu, obtenu %+v", stats)
	}
}

func TestADCacheSearchPopulee(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), ad.NomFichierCache)
	peuplerCache(t, chemin)

	out, err := execution(t, "ad-cache", "search", "--cache", chemin, "--query", "ali", "--json")
	if err != nil {
		t.Fatalf("ad-cache search: %v", err)
	}
	var users []ad.CacheUser
	if err := json.Unmarshal([]byte(out), &users); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if len(users) != 1 || users[0].SAM != "amartin" {
		t.Errorf("search = %+v", users)
	}

	out, err = execution(t, "ad-cache", "ous", "--cache", chemin, "--json")
	if err != nil {
		t.Fatalf("ad-cache ous: %v", err)
	}
	var ous []ad.CacheOU
	if err := json.Unmarshal([]byte(out), &ous); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if len(ous) != 1 || ous[0].Name != "Classes" {
		t.Errorf("ous = %+v", ous)
	}
}

func TestADCacheSyncAttenteAD(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), ad.NomFichierCache)
	out, err := execution(t, "ad-cache", "sync", "--cache", chemin, "--full", "--json")
	if err != nil {
		t.Fatalf("ad-cache sync: %v", err)
	}
	var r struct {
		Mode   string `json:"Mode"`
		Statut string `json:"Statut"`
	}
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	if r.Statut != "attente_ad" || r.Mode != "complet" {
		t.Errorf("résultat = %+v", r)
	}
}

func TestADCacheUsersSansOU(t *testing.T) {
	if _, err := execution(t, "ad-cache", "users", "--cache", filepath.Join(t.TempDir(), ad.NomFichierCache)); err == nil {
		t.Fatal("--ou manquant doit échouer")
	}
}

func TestADCacheSousCommandeInconnue(t *testing.T) {
	if _, err := execution(t, "ad-cache", "bogus"); err == nil {
		t.Fatal("sous-commande inconnue doit échouer")
	}
}
