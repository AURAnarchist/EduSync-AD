package ui

import (
	"bytes"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/api"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/grouptemplates"
	"github.com/estemobs/EduSync-AD/internal/label"
	"github.com/estemobs/EduSync-AD/internal/mailer"
	"github.com/estemobs/EduSync-AD/internal/models"
	"github.com/estemobs/EduSync-AD/internal/portal"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
	"github.com/estemobs/EduSync-AD/web"
)

// TestGabaritsModulesEnregistres vérifie que chaque gabarit de page module
// (fichier définissant « content ») est chargé par New() et se rend sans
// erreur « page inconnue ». La liste est découverte dans web.FS : tout
// nouveau gabarit de page est couvert automatiquement.
func TestGabaritsModulesEnregistres(t *testing.T) {
	s := newTestServer(t)
	d := donneesTestCompletes()

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

// TestGabaritsExécutionSansErreur exécute chaque page (layout complet) et
// chaque fragment (racine seule) avec des données riches. Contrairement à
// rendre(), qui trace l'erreur d'exécution sur stderr, ce test échoue
// explicitement : c'est lui qui attrape les bugs du type « tr .Langue dans un
// range » (le dot y est l'élément de la liste, pas PageData).
func TestGabaritsExécutionSansErreur(t *testing.T) {
	s := newTestServer(t)
	d := donneesTestCompletes()

	for _, page := range fichiersGabarits(t, true) {
		tmpl, ok := s.pages[page]
		if !ok {
			t.Errorf("page %s non enregistrée par New()", page)
			continue
		}
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, "layout", d); err != nil {
			t.Errorf("exécution page %s : %v", page, err)
		}
	}
	for _, fragment := range fichiersGabarits(t, false) {
		tmpl, ok := s.pages[fragment]
		if !ok {
			t.Errorf("fragment %s non enregistré par New()", fragment)
			continue
		}
		racine := s.racines[fragment]
		var buf bytes.Buffer
		if err := tmpl.ExecuteTemplate(&buf, racine, d); err != nil {
			t.Errorf("exécution fragment %s (racine %s) : %v", fragment, racine, err)
		}
	}
}

// donneesTestCompletes construit un PageData rempli pour que chaque branche
// conditionnelle et chaque {{range}} des gabarits s'exécute réellement.
func donneesTestCompletes() PageData {
	maintenant := time.Now().UTC().Format(time.RFC3339)
	ligneExec := MigrationExecLine{
		Prenom: "Léa", Nom: "Martin", Identifiant: "l.martin", NomComplet: "Martin Léa",
		OUSource: "OU=Depot,DC=X", OUCible: "OU=Eleves,DC=X",
		Statut: "succes", Detail: "Créé dans OU=Eleves", NouveauMDP: "Xk9$mDp",
	}
	return PageData{
		Titre: "Test", Langue: "fr", Theme: "clair", Version: "test",
		Connecte: true, LDAPFallback: true,
		Utilisateur: "admin", Domaine: "EDUSYNC.LOCAL", Controleur: "SRV-AD1",
		Erreurs:   []string{"erreur de test"},
		NbActions: 1, StatutAD: "ok",
		CheminConfig: "/tmp/config.json", CheminDonnees: "/tmp/journal.db",
		Actions: []audit.Entry{{
			ID: 1,
			ActionLogEntry: models.ActionLogEntry{
				Timestamp: maintenant, ActionType: "connexion", Compte: "admin",
				Resultat: "succes", Detail: "Domaine EDUSYNC.LOCAL", Utilisateur: "admin",
				Domaine: "EDUSYNC.LOCAL",
			},
		}},
		Simulation: &SimulationResult{
			Lignes: []SimulatedUserLine{{
				Prenom: "Léa", Nom: "Martin", Identifiant: "l.martin", Mail: "lea.martin@x",
				MotDePasse: "Xk9$mDp", OUCible: "OU=Eleves,DC=X", Groupe: "G-Eleves",
				DoublonResolu: true,
			}},
			Valides: 1, Total: 1,
		},
		ForcerMdp: "on", SkipAd: "on",
		Resultat: &ExecuteResult{Crees: 1, Lignes: []MigrationExecLine{ligneExec}},
		SimulationPwd: &PasswordSimResult{
			Lignes: []PasswordSimLine{{
				Prenom: "Léa", Nom: "Martin", Identifiant: "l.martin",
				Mail: "lea.martin@x", NouveauMDP: "Xk9$mDp", OUCible: "OU=Eleves,DC=X",
			}},
			Valides: 1, Total: 1,
		},
		ForceChange: true,
		ResultatPwd: &PasswordExecResult{Crees: 1, Lignes: []MigrationExecLine{ligneExec}},
		PlanMigration: &MigrationPlanResult{
			Lignes: []MigrationPlanLine{{
				Identifiant: "l.martin", NomComplet: "Martin Léa",
				OUSource: "OU=Depot,DC=X", OUCible: "OU=Eleves,DC=X", Action: "move",
			}},
			Total: 1, WillMove: 1,
		},
		SimulationMigration: &MigrationSimResult{
			Lignes: []MigrationSimLine{{
				Identifiant: "l.martin", NomComplet: "Martin Léa",
				OUSource: "OU=Depot,DC=X", OUCible: "OU=Eleves,DC=X", Action: "move",
			}},
			Total: 1, Valides: 1,
		},
		MigrationExecute: &MigrationExecResult{Crees: 1, Lignes: []MigrationExecLine{ligneExec}},
		SearchResult: &ad.SearchResult{
			Entries: []ad.SearchResultEntry{{
				DN: "CN=Léa Martin,OU=Eleves,DC=X", ObjectType: "user",
				DisplayName: "Martin Léa", SAMAccountName: "l.martin", Enabled: true,
			}},
			Cookie: "cookie-demo", Total: 1,
		},
		ExplorerTree: &ad.ExplorerNode{
			DN: "DC=X", Name: "Racine", Type: "container", Expanded: true,
			Children: []*ad.ExplorerNode{{
				DN: "OU=Eleves,DC=X", Name: "Eleves", Type: "ou", HasChildren: true,
				Children: []*ad.ExplorerNode{{DN: "CN=G1,OU=Groupes,DC=X", Name: "G1", Type: "group"}},
			}},
		},
		ExplorerUser: &ad.User{
			DN: "CN=Léa,DC=X", SAMAccountName: "l.martin", DisplayName: "Martin Léa", Enabled: true,
		},
		ExplorerGroup: &ad.Group{DN: "CN=G1,DC=X", CN: "G1", SAMAccountName: "G1"},
		ExplorerOU:    &ad.OU{DN: "OU=Eleves,DC=X", Name: "Eleves", ParentDN: "DC=X"},
		ModelesLabel:  label.ModelesIntegres(),
		ModelesGT:     grouptemplates.ModelesIntegres(),
		MailLabel: mailer.MailConfig{
			Host: "smtp.exemple.fr", Port: 587, Username: "edusync", Password: "secret",
			UseTLS: true, FromAddr: "edusync@exemple.fr", FromName: "EduSync AD", Timeout: 20,
		},
		PortailConfig: portal.Config{
			Actif: true, Hote: "127.0.0.1", Port: 8787, CodeMinutes: 10,
			TentativesMax: 5, EnvoisMax: 3, Politique: portal.ProfilEleve,
			OUAccueil: "OU=Eleves,DC=X", NotificationMail: true, Titre: "Portail du collège",
		},
		PortailDemandes: []portal.Demande{{
			ID: "abc12345", Profil: portal.ProfilEleve, Prenom: "Léa", Nom: "Martin",
			Mail: "lea.martin@x", Classe: "6eA", Motif: "arrivée",
			Statut: portal.StatutEnAttente, CreatedAt: maintenant,
		}},
		PortailEnEcoute:     true,
		PortailURL:          "http://127.0.0.1:8787",
		PortailCheminConfig: "/tmp/portail.json",
		PortailCheminBase:   "/tmp/portail.db",
		APIConfig: api.Config{
			Actif: true, Hote: "127.0.0.1", Port: 8788,
			CORSOrigines: []string{"*"}, LimiteRPM: 60, Titre: "API Edusync AD",
		},
		APICles: []CleAPIVue{{
			ID: "ak_0123456789ab", Nom: "Supervision", Prefixe: "edusync_abcd",
			Permissions: []string{"users:read", "audit:read"}, Active: true,
			Statut: "active", DernierUsage: maintenant,
		}, {
			ID: "ak_abcdefabcdef", Nom: "Ancienne clé", Prefixe: "edusync_efgh",
			Permissions: []string{"*"}, Active: false, Statut: "révoquée",
		}},
		APIWebhooks: []WebhookVue{{
			ID: "w0123456789ab", Nom: "Supervision", URL: "https://supervision.exemple.fr/hook",
			Evenements: []string{"user.created", "user.deleted"}, Actif: true,
			PolitiqueRetrai: "exponential", RetraitsMax: 5,
		}},
		APILivraisons: []LivraisonVue{{
			CommenceLe: maintenant, WebhookID: "w0123456789ab",
			Evenement: "user.created", Tentative: 1, Statut: "HTTP 200",
		}, {
			CommenceLe: maintenant, WebhookID: "w0123456789ab",
			Evenement: "user.deleted", Tentative: 2, Statut: "connexion refusée",
		}},
		APINombreLivraisons: 2,
		APIEnEcoute:         true,
		APIURL:              "http://127.0.0.1:8788",
		APICheminConfig:     "/tmp/api.json",
		APICheminCles:       "/tmp/api_keys.json",
		APICheminWebhooks:   "/tmp/webhooks.json",
		APICheminLivraisons: "/tmp/webhook_deliveries.db",
		APIPermissions:      api.PermissionsStandards,
		APIEvenements:       webhooks.EvenementsTries(),
		APIRoutes: []api.Route{
			{Methode: "GET", Motif: "/api/v1/users"},
			{Methode: "POST", Motif: "/api/v1/users"},
			{Methode: "GET", Motif: "/api/v1/webhooks/{webhook_id}"},
			{Methode: "GET", Motif: "/api/v1/openapi.json", Publique: true},
		},

		// Assistant IA (M29)
		AssistantFormats:       []string{"prenom.nom", "p.nom", "prenomNom"},
		AssistantADDisponible:  true,
		AssistantAvertissement: "avertissement de test",
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
