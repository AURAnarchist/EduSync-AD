package ui

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/web"
)

// TestGabaritsModulesEnregistres vérifie que chaque gabarit de page module
// (fichier définissant « content ») est chargé par New() et se rend sans
// erreur « page inconnue ». La liste est découverte dans web.FS : tout
// nouveau gabarit de page est couvert automatiquement.
func TestGabaritsModulesEnregistres(t *testing.T) {
	s := newTestServer(t)
	d := PageData{Langue: "fr", Theme: "clair", Version: "test"}

	attendus := fichiersGabarits(t, true)
	if len(attendus) < 5 {
		t.Fatalf("découverte des gabarits de page suspecte : %d trouvé(s)", len(attendus))
	}
	for _, page := range attendus {
		rec := httptest.NewRecorder()
		s.rendre(rec, http.StatusOK, page, d)
		if rec.Code != http.StatusOK {
			t.Errorf("rendu %s → code %d : %.120s", page, rec.Code, rec.Body.String())
		}
	}
}

// TestGabaritsRacinesConnues vérifie que les entrées principales ont bien la
// racine attendue (le rendu fragment en dépend).
func TestGabaritsRacinesConnues(t *testing.T) {
	s := newTestServer(t)
	cas := map[string]string{
		"users.html":            "content",
		"export.html":           "content",
		"audit.html":            "content",
		"explorer_results.html": "explorer_results",
		"explorer_tree.html":    "explorer_tree",
	}
	for fichier, racine := range cas {
		if got := s.racines[fichier]; got != racine {
			t.Errorf("racine de %s = %q, attendu %q", fichier, got, racine)
		}
	}
}

// TestGabaritsFragmentsRendus vérifie que les fragments HTMX sont rendus seuls,
// sans layout (document complet non attendu dans une cible innerHTML), avec
// des données réalistes pour déclencher les branches conditionnelles.
func TestGabaritsFragmentsRendus(t *testing.T) {
	s := newTestServer(t)
	d := PageData{Langue: "fr", Theme: "clair", Version: "test"}
	d.SearchResult = &ad.SearchResult{
		Entries: []ad.SearchResultEntry{{
			DN: "CN=Léa Martin,OU=Eleves,DC=EDUSYNC,DC=LOCAL", ObjectType: "user",
			DisplayName: "Martin Léa", SAMAccountName: "l.martin", Enabled: true,
		}},
		Cookie: "cookie-demo",
		Total:  1,
	}
	// Nœud avec enfant déjà chargé + nœud en attente de lazy loading.
	d.ExplorerTree = &ad.ExplorerNode{
		DN: "DC=X", Name: "Racine", Type: "container", Expanded: true,
		Children: []*ad.ExplorerNode{{
			DN: "OU=Eleves,DC=X", Name: "Eleves", Type: "ou", HasChildren: true,
			Children: []*ad.ExplorerNode{
				{DN: "CN=G1,OU=Groupes,DC=X", Name: "G1", Type: "group"},
			},
		}},
	}
	d.ExplorerUser = &ad.User{
		DN: "CN=Léa,DC=X", SAMAccountName: "l.martin", DisplayName: "Martin Léa", Enabled: true,
	}
	d.ExplorerGroup = &ad.Group{DN: "CN=G1,DC=X", CN: "G1", SAMAccountName: "G1"}
	d.ExplorerOU = &ad.OU{DN: "OU=Eleves,DC=X", Name: "Eleves", ParentDN: "DC=X"}

	for _, frag := range []string{
		"explorer_tree.html", "explorer_results.html", "explorer_details.html",
	} {
		rec := httptest.NewRecorder()
		s.rendreFragment(rec, http.StatusOK, frag, d)
		if rec.Code != http.StatusOK {
			t.Errorf("fragment %s → code %d : %.120s", frag, rec.Code, rec.Body.String())
			continue
		}
		if bytes.Contains(rec.Body.Bytes(), []byte("<!doctype html>")) {
			t.Errorf("fragment %s : le layout complet a été rendu dans le fragment", frag)
		}
		if bytes.Contains(rec.Body.Bytes(), []byte("%!")) {
			t.Errorf("fragment %s : erreur de rendu dans la sortie : %.200s", frag, rec.Body.String())
		}
	}
}

// fichiersGabarits retourne les noms de fichiers de templates/*.html,
// filtrés selon qu'ils définissent « content » (pages) ou non (fragments).
func fichiersGabarits(t *testing.T, pagesSeulement bool) []string {
	t.Helper()
	chemins, err := fs.Glob(web.FS, "templates/*.html")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	var noms []string
	for _, chemin := range chemins {
		nom := filepath.Base(chemin)
		if nom == "layout.html" {
			continue
		}
		brut, err := fs.ReadFile(web.FS, chemin)
		if err != nil {
			t.Fatalf("lecture %s: %v", nom, err)
		}
		estPage := bytes.Contains(brut, []byte(`{{define "content"}}`))
		if estPage == pagesSeulement {
			noms = append(noms, nom)
		}
	}
	return noms
}

// newTestServer construit un serveur de test (chemins temporaires).
func newTestServer(t *testing.T) *Server {
	t.Helper()
	srv, err := New(Options{
		ConfigPath: t.TempDir() + "/config.json",
		AuditPath:  t.TempDir() + "/journal.db",
		Version:    "test",
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv
}
