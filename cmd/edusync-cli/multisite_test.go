package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/crypto"
	"github.com/estemobs/EduSync-AD/internal/multisite"
)

// cheminsMultisite retourne un fichier de sites et une clé isolés.
func cheminsMultisite(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	return filepath.Join(dir, "domaines.json"), filepath.Join(dir, "secret.key")
}

func TestMultisiteAddListRemove(t *testing.T) {
	fichier, cle := cheminsMultisite(t)

	out, err := execution(t, "multisite", "add", "--label", "Lycée Victor Hugo",
		"--domain", "lycee-victor-hugo.local", "--controller", "10.0.0.5",
		"--username", "admin", "--remember", "--password", "S3cret!",
		"--file", fichier, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	var profil multisite.ProfilDomaine
	if err := json.Unmarshal([]byte(out), &profil); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if profil.ID == "" || profil.Domain != "lycee-victor-hugo.local" {
		t.Errorf("profil = %+v", profil)
	}

	// Le fichier ne doit jamais contenir les données en clair.
	brut, err := os.ReadFile(fichier)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(brut), "lycee-victor-hugo.local") || strings.Contains(string(brut), "S3cret!") {
		t.Error("données en clair dans domaines.json")
	}

	outList, err := execution(t, "multisite", "list", "--file", fichier, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var profils []multisite.ProfilDomaine
	if err := json.Unmarshal([]byte(outList), &profils); err != nil {
		t.Fatalf("JSON: %v\n%s", err, outList)
	}
	if len(profils) != 1 || profils[0].Password != "S3cret!" {
		t.Errorf("profils = %+v", profils)
	}

	if _, err := execution(t, "multisite", "remove", "--id", profil.ID, "--file", fichier, "--key", cle); err != nil {
		t.Fatalf("remove: %v", err)
	}
	outVide, err := execution(t, "multisite", "list", "--file", fichier, "--key", cle, "--json")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(outVide) != "[]" && strings.TrimSpace(outVide) != "null" {
		t.Errorf("liste après suppression = %s", outVide)
	}
}

func TestMultisiteAddValideLesChamps(t *testing.T) {
	fichier, cle := cheminsMultisite(t)
	if _, err := execution(t, "multisite", "add", "--domain", "a.local", "--file", fichier, "--key", cle); err == nil {
		t.Error("label manquant accepté")
	}
	if _, err := execution(t, "multisite", "add", "--label", "S", "--domain", "domaine invalide!", "--file", fichier, "--key", cle); err == nil {
		t.Error("domaine invalide accepté")
	}
	if _, err := execution(t, "multisite", "remove", "--id", "inconnu", "--file", fichier, "--key", cle); err == nil {
		t.Error("suppression d'un site inconnu acceptée")
	}
}

func TestMultisiteImportLegacy(t *testing.T) {
	fichier, cle := cheminsMultisite(t)
	herite := filepath.Join(filepath.Dir(fichier), "connection.local.json")

	k, err := crypto.LoadOrCreateKey(cle)
	if err != nil {
		t.Fatal(err)
	}
	payload := map[string]string{}
	for champ, valeur := range map[string]string{
		"domaine": "college.local", "controleur": "dc01", "utilisateur": "admin", "mot_de_passe": "Bienvenue01",
	} {
		jeton, err := crypto.Encrypt(k, valeur)
		if err != nil {
			t.Fatal(err)
		}
		payload[champ] = jeton
	}
	donnees, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(herite, donnees, 0o600); err != nil {
		t.Fatal(err)
	}

	out, err := execution(t, "multisite", "import-legacy", "--legacy", herite, "--file", fichier, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("import-legacy: %v", err)
	}
	var profil multisite.ProfilDomaine
	if err := json.Unmarshal([]byte(out), &profil); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if profil.Domain != "college.local" || profil.Password != "Bienvenue01" || !profil.RememberPassword {
		t.Errorf("profil = %+v", profil)
	}

	// Deuxième import : plus rien à migrer.
	out2, err := execution(t, "multisite", "import-legacy", "--legacy", herite, "--file", fichier, "--key", cle, "--json")
	if err != nil {
		t.Fatalf("import-legacy 2: %v", err)
	}
	if strings.TrimSpace(out2) != "null" {
		t.Errorf("second import = %s", out2)
	}
}
