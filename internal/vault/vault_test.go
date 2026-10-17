package vault

import (
	"os"
	"path/filepath"
	"testing"
)

// coffreTest ouvre un coffre isolé dans un dossier temporaire.
func coffreTest(t *testing.T) *Vault {
	t.Helper()
	dir := t.TempDir()
	v, err := New(filepath.Join(dir, "vault.sqlite3"), filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = v.Close() })
	return v
}

func TestStoreAndGetRoundTrip(t *testing.T) {
	v := coffreTest(t)
	if err := v.Store("thomas.martin", "Sup3r$ecret!"); err != nil {
		t.Fatal(err)
	}
	motDePasse, ok := v.Get("thomas.martin")
	if !ok || motDePasse != "Sup3r$ecret!" {
		t.Errorf("Get = %q, %v", motDePasse, ok)
	}
}

func TestGetInconnu(t *testing.T) {
	v := coffreTest(t)
	if motDePasse, ok := v.Get("inconnu"); ok || motDePasse != "" {
		t.Errorf("Get(inconnu) = %q, %v", motDePasse, ok)
	}
}

func TestRechercheInsensibleALaCasse(t *testing.T) {
	v := coffreTest(t)
	if err := v.Store("Thomas.Martin", "abc123"); err != nil {
		t.Fatal(err)
	}
	if motDePasse, ok := v.Get("thomas.martin"); !ok || motDePasse != "abc123" {
		t.Errorf("Get = %q, %v", motDePasse, ok)
	}
	if !v.Has("THOMAS.MARTIN") {
		t.Error("Has insensible à la casse attendu")
	}
}

func TestStoreEcraseLePrecedent(t *testing.T) {
	v := coffreTest(t)
	if err := v.Store("thomas.martin", "premier"); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("thomas.martin", "second"); err != nil {
		t.Fatal(err)
	}
	if motDePasse, _ := v.Get("thomas.martin"); motDePasse != "second" {
		t.Errorf("Get = %q", motDePasse)
	}
}

func TestDelete(t *testing.T) {
	v := coffreTest(t)
	if err := v.Store("thomas.martin", "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete("thomas.martin"); err != nil {
		t.Fatal(err)
	}
	if _, ok := v.Get("thomas.martin"); ok {
		t.Error("entrée non supprimée")
	}
	if v.Has("thomas.martin") {
		t.Error("Has vrai après suppression")
	}
}

func TestMotDePasseJamaisEnClairSurDisque(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "vault.sqlite3")
	v, err := New(chemin, filepath.Join(dir, "secret.key"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v.Close() }()
	if err := v.Store("thomas.martin", "MotDePasseTresSecret"); err != nil {
		t.Fatal(err)
	}
	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if containsBytes(brut, []byte("MotDePasseTresSecret")) {
		t.Error("le mot de passe apparaît en clair dans le fichier")
	}
}

func TestClearAll(t *testing.T) {
	v := coffreTest(t)
	if err := v.Store("eleve1", "a"); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("eleve2", "b"); err != nil {
		t.Fatal(err)
	}
	supprimes, err := v.ClearAll()
	if err != nil {
		t.Fatal(err)
	}
	if supprimes != 2 {
		t.Errorf("supprimés = %d", supprimes)
	}
	if nombre, _ := v.Count(); nombre != 0 {
		t.Errorf("count = %d", nombre)
	}
	if _, ok := v.Get("eleve1"); ok {
		t.Error("entrée encore présente après purge")
	}
}

func TestStoreIgnoreLesVides(t *testing.T) {
	v := coffreTest(t)
	if err := v.Store("", "abc123"); err != nil {
		t.Fatal(err)
	}
	if err := v.Store("thomas.martin", ""); err != nil {
		t.Fatal(err)
	}
	if nombre, _ := v.Count(); nombre != 0 {
		t.Errorf("count = %d, attendu 0", nombre)
	}
}

// TestCompatiblePython vérifie que le schéma et le chiffrement sont relus par un
// coffre ouvert séparément avec la même clé (format partagé avec Python).
func TestCompatiblePython(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "vault.sqlite3")
	clePath := filepath.Join(dir, "secret.key")

	v1, err := New(chemin, clePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := v1.Store("eleve", "UnMotDePasse"); err != nil {
		t.Fatal(err)
	}
	if err := v1.Close(); err != nil {
		t.Fatal(err)
	}

	v2, err := New(chemin, clePath)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = v2.Close() }()
	if motDePasse, ok := v2.Get("eleve"); !ok || motDePasse != "UnMotDePasse" {
		t.Errorf("relecture = %q, %v", motDePasse, ok)
	}
}

// containsBytes indique si « aiguille » apparaît dans « botte ».
func containsBytes(botte, aiguille []byte) bool {
	if len(aiguille) == 0 {
		return true
	}
	for i := 0; i+len(aiguille) <= len(botte); i++ {
		if string(botte[i:i+len(aiguille)]) == string(aiguille) {
			return true
		}
	}
	return false
}
