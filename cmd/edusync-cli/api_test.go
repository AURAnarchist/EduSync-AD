package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/api"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
)

// nouveauAPITest isole les dossiers de configuration et de données.
func nouveauAPITest(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
}

// TestAPIConfigAfficheEtEnregistre couvre la lecture et l'écriture de api.json.
func TestAPIConfigAfficheEtEnregistre(t *testing.T) {
	nouveauAPITest(t)

	out, err := execution(t, "api", "config", "--json")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var charge struct {
		Config map[string]any `json:"config"`
		File   string         `json:"file"`
		URL    string         `json:"url"`
		Errors []string       `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &charge); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if charge.Config["port"].(float64) != 8788 || charge.Config["actif"].(bool) {
		t.Fatalf("config par défaut = %+v", charge.Config)
	}
	if charge.URL != "http://127.0.0.1:8788" || len(charge.Errors) != 0 {
		t.Errorf("url = %q, erreurs = %v", charge.URL, charge.Errors)
	}
	if !strings.HasSuffix(charge.File, "api.json") {
		t.Errorf("fichier = %q", charge.File)
	}
	// Sans --save, le fichier n'est pas écrit.
	if _, err := execution(t, "api", "config", "--port", "9100"); err != nil {
		t.Fatalf("config --port: %v", err)
	}
	if _, err := os.Stat(charge.File); !os.IsNotExist(err) {
		t.Fatalf("le fichier ne doit pas exister sans --save : %v", err)
	}

	if _, err := execution(t, "api", "config", "--port", "9100", "--host", "0.0.0.0",
		"--cors", "https://admin.local, https://portail.local", "--rate-limit", "120",
		"--titre", "API du lycée", "--actif", "--save"); err != nil {
		t.Fatalf("config --save: %v", err)
	}

	out, err = execution(t, "api", "config", "--json")
	if err != nil {
		t.Fatalf("config après écriture: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &charge); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if charge.Config["port"].(float64) != 9100 || !charge.Config["actif"].(bool) ||
		charge.Config["titre"] != "API du lycée" ||
		charge.Config["rate_limit_rpm"].(float64) != 120 {
		t.Fatalf("config relue = %+v", charge.Config)
	}
	origines := charge.Config["cors_origins"].([]any)
	if len(origines) != 2 || origines[1] != "https://portail.local" {
		t.Errorf("origines = %v", origines)
	}
	// L'URL reste celle de la boucle locale pour un hôte 0.0.0.0.
	if charge.URL != "http://127.0.0.1:9100" {
		t.Errorf("url = %q", charge.URL)
	}

	// Sortie texte.
	texte, err := execution(t, "api", "config")
	if err != nil {
		t.Fatalf("config texte: %v", err)
	}
	for _, attendu := range []string{"api.json", "9100", "120 requêtes/minute"} {
		if !strings.Contains(texte, attendu) {
			t.Errorf("sortie incomplète: %q absent\n%s", attendu, texte)
		}
	}

	// --reset repart des valeurs par défaut, sans écrire.
	out, err = execution(t, "api", "config", "--reset", "--json")
	if err != nil {
		t.Fatalf("config --reset: %v", err)
	}
	var apresReset struct {
		Config map[string]any `json:"config"`
	}
	if err := json.Unmarshal([]byte(out), &apresReset); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if apresReset.Config["port"].(float64) != 8788 {
		t.Errorf("port après reset = %v", apresReset.Config["port"])
	}

	// Une configuration invalide est refusée à l'écriture.
	if _, err := execution(t, "api", "config", "--port", "99999", "--save"); err == nil {
		t.Error("une configuration invalide doit être refusée")
	}
}

// TestAPIClesCycleDeVie couvre la création, la liste, la révocation et la
// suppression des clés d'API.
func TestAPIClesCycleDeVie(t *testing.T) {
	nouveauAPITest(t)

	// Liste vide.
	out, err := execution(t, "api", "keys", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, `"total": 0`) {
		t.Errorf("liste = %s", out)
	}

	// Création sans nom : refusée.
	if _, err := execution(t, "api", "keys", "create", "--json"); err == nil {
		t.Error("un nom vide doit être refusé")
	}

	out, err = execution(t, "api", "keys", "create", "--name", "Supervision",
		"--permissions", "users:read,users:write", "--days", "30", "--json")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var creation struct {
		Cle  string         `json:"key"`
		Info api.CleAPIInfo `json:"info"`
		File string         `json:"file"`
	}
	if err := json.Unmarshal([]byte(out), &creation); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if !strings.HasPrefix(creation.Cle, "edusync_") || creation.Info.ID == "" {
		t.Fatalf("création = %+v", creation)
	}
	if len(creation.Info.Permissions) != 2 || creation.Info.ExpireLe == nil {
		t.Errorf("info = %+v", creation.Info)
	}
	if !strings.HasSuffix(creation.File, "api_keys.json") {
		t.Errorf("fichier = %q", creation.File)
	}
	// La clé complète n'est jamais écrite sur disque.
	donnees, err := os.ReadFile(creation.File)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if strings.Contains(string(donnees), creation.Cle) {
		t.Fatal("la clé complète ne doit pas être écrite")
	}

	// Relecture de la liste.
	out, err = execution(t, "api", "keys", "list", "--all", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, creation.Info.ID) {
		t.Errorf("la clé doit apparaître : %s", out)
	}
	// Sortie texte.
	texte, err := execution(t, "api", "keys", "list")
	if err != nil {
		t.Fatalf("list texte: %v", err)
	}
	if !strings.Contains(texte, "Supervision") || !strings.Contains(texte, "active") {
		t.Errorf("sortie = %s", texte)
	}

	// Permissions disponibles.
	out, err = execution(t, "api", "keys", "permissions", "--json")
	if err != nil {
		t.Fatalf("permissions: %v", err)
	}
	if !strings.Contains(out, "users:password") || !strings.Contains(out, "webhooks:write") {
		t.Errorf("permissions = %s", out)
	}

	// Révocation puis suppression.
	out, err = execution(t, "api", "keys", "revoke", "--id", creation.Info.ID, "--json")
	if err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if !strings.Contains(out, `"revoked": true`) {
		t.Errorf("revoke = %s", out)
	}
	if _, err := execution(t, "api", "keys", "revoke", "--id", "inconnu"); err == nil {
		t.Error("révoquer une clé inconnue doit échouer")
	}
	out, err = execution(t, "api", "keys", "remove", "--id", creation.Info.ID, "--json")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out, `"deleted": true`) {
		t.Errorf("remove = %s", out)
	}
	if _, err := execution(t, "api", "keys", "remove", "--id", creation.Info.ID, "--json"); err == nil {
		t.Error("supprimer une clé absente doit échouer")
	}
	if _, err := execution(t, "api", "keys", "revoke"); err == nil {
		t.Error("--id doit être obligatoire")
	}
}

// TestAPIRoutesEtSpec vérifie la liste des routes et le document OpenAPI.
func TestAPIRoutesEtSpec(t *testing.T) {
	nouveauAPITest(t)

	out, err := execution(t, "api", "routes", "--json")
	if err != nil {
		t.Fatalf("routes: %v", err)
	}
	var routes struct {
		Version string `json:"version"`
		Prefix  string `json:"prefix"`
		Total   int    `json:"total"`
		Routes  []struct {
			Methode  string `json:"method"`
			Motif    string `json:"path"`
			Publique bool   `json:"public"`
		} `json:"routes"`
	}
	if err := json.Unmarshal([]byte(out), &routes); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if routes.Version != api.VersionAPI || routes.Prefix != api.PrefixeAPI || routes.Total != len(routes.Routes) {
		t.Fatalf("routes = %+v", routes)
	}
	var publiques, avecPrefixe int
	for _, route := range routes.Routes {
		if route.Publique {
			publiques++
		}
		if strings.HasPrefix(route.Motif, api.PrefixeAPI+"/") {
			avecPrefixe++
		}
	}
	if publiques != 2 || avecPrefixe == 0 {
		t.Errorf("publiques = %d, préfixées = %d", publiques, avecPrefixe)
	}

	// Sortie texte.
	texte, err := execution(t, "api", "routes")
	if err != nil {
		t.Fatalf("routes texte: %v", err)
	}
	if !strings.Contains(texte, "POST   /api/v1/users") ||
		!strings.Contains(texte, "/openapi.json [publique]") {
		t.Errorf("sortie = %s", texte)
	}

	// Document OpenAPI écrit dans un fichier explicite.
	chemin := filepath.Join(t.TempDir(), "spec.json")
	out, err = execution(t, "api", "spec", "--out", chemin, "--json")
	if err != nil {
		t.Fatalf("spec: %v", err)
	}
	if !strings.Contains(out, "spec.json") {
		t.Errorf("sortie = %s", out)
	}
	var document map[string]any
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if err := json.Unmarshal(donnees, &document); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if document["openapi"] != "3.0.3" {
		t.Errorf("document = %v", document["openapi"])
	}
	if len(document["paths"].(map[string]any)) == 0 {
		t.Error("le document doit décrire des chemins")
	}
	title := document["info"].(map[string]any)["title"]
	if title != api.TitreDocument {
		t.Errorf("titre = %v", title)
	}

	// Document sur la sortie standard.
	texte, err = execution(t, "api", "spec", "--stdout")
	if err != nil {
		t.Fatalf("spec --stdout: %v", err)
	}
	if !strings.Contains(texte, `"openapi": "3.0.3"`) {
		t.Errorf("sortie = %s", texte[:min(len(texte), 200)])
	}

	// Écriture par défaut (api-spec.json du dossier courant).
	dossier := t.TempDir()
	precedent := os.Getenv("PWD")
	t.Cleanup(func() { _ = os.Chdir(precedent) })
	if err := os.Chdir(dossier); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	if _, err := execution(t, "api", "spec", "--json"); err != nil {
		t.Fatalf("spec par défaut: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dossier, api.NomFichierOpenAPI)); err != nil {
		t.Errorf("api-spec.json absent : %v", err)
	}
}

// TestAPIWebhooksCLI couvre les abonnements, le journal et l'envoi de test.
func TestAPIWebhooksCLI(t *testing.T) {
	nouveauAPITest(t)

	// Événements disponibles.
	out, err := execution(t, "api", "webhooks", "events", "--json")
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	if !strings.Contains(out, "user.created") || !strings.Contains(out, "portal.request_approved") {
		t.Errorf("événements = %s", out)
	}

	// Liste vide.
	out, err = execution(t, "api", "webhooks", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if !strings.Contains(out, `"total": 0`) {
		t.Errorf("liste = %s", out)
	}

	// Ajout : le secret est chiffré dans le fichier.
	serveur := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer serveur.Close()
	if _, err := execution(t, "api", "webhooks", "add", "--name", "Supervision",
		"--url", serveur.URL, "--events", "user.created, user.deleted",
		"--secret", "s3cr3t", "--json"); err != nil {
		t.Fatalf("add: %v", err)
	}
	// Sans nom ni URL : refusé.
	if _, err := execution(t, "api", "webhooks", "add", "--events", "*"); err == nil {
		t.Error("un webhook sans nom ni URL doit être refusé")
	}
	// Sans événement : refusé (validation du magasin).
	if _, err := execution(t, "api", "webhooks", "add", "--name", "Vide", "--url", serveur.URL); err == nil {
		t.Error("un webhook sans événement doit être refusé")
	}

	out, err = execution(t, "api", "webhooks", "list", "--json")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var liste struct {
		Webhooks []map[string]any `json:"webhooks"`
		Total    int              `json:"total"`
		File     string           `json:"file"`
	}
	if err := json.Unmarshal([]byte(out), &liste); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if liste.Total != 1 || len(liste.Webhooks) != 1 {
		t.Fatalf("liste = %+v", liste)
	}
	identifiant := liste.Webhooks[0]["id"].(string)
	if liste.Webhooks[0]["secret"] == "s3cr3t" {
		t.Error("le secret doit être chiffré au stockage")
	}
	if !strings.HasSuffix(liste.File, webhooks.NomFichier) {
		t.Errorf("fichier = %q", liste.File)
	}

	// Envoi de test.
	out, err = execution(t, "api", "webhooks", "test", "--id", identifiant,
		"--event", "user.created", "--json")
	if err != nil {
		t.Fatalf("test: %v", err)
	}
	if !strings.Contains(out, `"status_code": 200`) {
		t.Errorf("test = %s", out)
	}
	if _, err := execution(t, "api", "webhooks", "test", "--id", "inconnu"); err == nil {
		t.Error("un webhook inconnu doit être signalé")
	}

	// Journal des livraisons.
	out, err = execution(t, "api", "webhooks", "deliveries", "--json")
	if err != nil {
		t.Fatalf("deliveries: %v", err)
	}
	if !strings.Contains(out, `"total": 1`) || !strings.Contains(out, identifiant) {
		t.Errorf("livraisons = %s", out)
	}
	texte, err := execution(t, "api", "webhooks", "deliveries")
	if err != nil {
		t.Fatalf("deliveries texte: %v", err)
	}
	if !strings.Contains(texte, "user.created") {
		t.Errorf("sortie = %s", texte)
	}

	// Suppression.
	out, err = execution(t, "api", "webhooks", "remove", "--id", identifiant, "--json")
	if err != nil {
		t.Fatalf("remove: %v", err)
	}
	if !strings.Contains(out, `"deleted": true`) {
		t.Errorf("remove = %s", out)
	}
	if _, err := execution(t, "api", "webhooks", "remove", "--id", identifiant); err == nil {
		t.Error("supprimer un webhook absent doit échouer")
	}
}

// TestAPIServeSansCle vérifie que le serveur refuse de démarrer sans clé.
func TestAPIServeSansCle(t *testing.T) {
	nouveauAPITest(t)
	_, err := execution(t, "api", "serve", "--json")
	if err == nil {
		t.Fatal("le serveur doit refuser de démarrer sans clé d'API")
	}
	if !strings.Contains(err.Error(), "aucune clé d'API") {
		t.Errorf("erreur = %v", err)
	}
}

// TestAPIUsage vérifie les messages d'usage.
func TestAPIUsage(t *testing.T) {
	if _, err := execution(t, "api"); err == nil {
		t.Error("une sous-commande doit être exigée")
	}
	if _, err := execution(t, "api", "inconnu"); err == nil {
		t.Error("une sous-commande inconnue doit être refusée")
	}
	if _, err := execution(t, "api", "keys"); err == nil {
		t.Error("une action de clé doit être exigée")
	}
	if _, err := execution(t, "api", "keys", "inconnu"); err == nil {
		t.Error("une action inconnue doit être refusée")
	}
	if _, err := execution(t, "api", "webhooks"); err == nil {
		t.Error("une action de webhook doit être exigée")
	}
	if _, err := execution(t, "api", "webhooks", "inconnu"); err == nil {
		t.Error("une action inconnue doit être refusée")
	}
	if _, err := execution(t, "api", "keys", "create", "--name", "X", "--permissions", "bidon"); err == nil {
		t.Error("une permission inconnue doit être refusée")
	}
}
