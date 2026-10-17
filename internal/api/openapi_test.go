package api

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRouteurResoudre vérifie la compilation des motifs et les paramètres.
func TestRouteurResoudre(t *testing.T) {
	routeur := NouveauRouteur()
	appele := ""
	routeur.Ajouter("get", "/users/{sam}/groupes/{cn}", func(requete *Requete) (int, any) {
		appele = requete.Params["sam"] + "/" + requete.Params["cn"]
		return ReponseOK(nil)
	})
	routeur.AjouterPublique("GET", "/openapi.json", func(*Requete) (int, any) {
		return ReponseOK(nil)
	})

	route, parametres := routeur.Resoudre("GET", "/users/l.martin/groupes/Eleves")
	if route == nil {
		t.Fatal("la route doit être trouvée")
	}
	if route.Publique {
		t.Error("la route ne doit pas être publique")
	}
	if parametres["sam"] != "l.martin" || parametres["cn"] != "Eleves" {
		t.Fatalf("paramètres = %v", parametres)
	}
	if _, charge := route.Handler(&Requete{Params: parametres}); charge == nil {
		t.Error("le handler doit répondre")
	}
	if appele != "l.martin/Eleves" {
		t.Errorf("appel = %q", appele)
	}

	// La casse de la méthode est normalisée.
	if route, _ := routeur.Resoudre("get", "/users/x/groupes/y"); route == nil {
		t.Error("la méthode doit être insensible à la casse")
	}
	// Un segment vide ne correspond pas à un paramètre.
	if route, _ := routeur.Resoudre("GET", "/users//groupes/Eleves"); route != nil {
		t.Error("un paramètre vide ne doit pas correspondre")
	}
	if route, _ := routeur.Resoudre("GET", "/inconnu"); route != nil {
		t.Error("une route inconnue doit retourner nil")
	}
	// Publique.
	if route, _ := routeur.Resoudre("GET", "/openapi.json"); route == nil || !route.Publique {
		t.Error("la route OpenAPI doit être publique")
	}
	// Existe : le chemin est connu, même avec une autre méthode.
	if !routeur.Existe("/users/x/groupes/y") {
		t.Error("le chemin doit exister")
	}
	if routeur.Existe("/jamais") {
		t.Error("un chemin inconnu ne doit pas exister")
	}
	if len(routeur.Routes()) != 2 {
		t.Errorf("routes = %d", len(routeur.Routes()))
	}
}

// TestDocumentOpenAPI vérifie la structure du document et sa couverture.
func TestDocumentOpenAPI(t *testing.T) {
	b := nouveauBanc(t)
	document := DocumentOpenAPI("4.0.0", b.serveur.Routeur)

	if document["openapi"] != "3.0.3" {
		t.Errorf("openapi = %v", document["openapi"])
	}
	info := document["info"].(map[string]any)
	if info["title"] != TitreDocument || info["version"] != "4.0.0" {
		t.Errorf("info = %v", info)
	}
	serveurs := document["servers"].([]map[string]any)
	if len(serveurs) != 1 || serveurs[0]["url"] != PrefixeAPI {
		t.Errorf("servers = %v", serveurs)
	}

	chemins := document["paths"].(map[string]any)
	// Les chemins sont décrits sans le préfixe `/api/v1` et sans doublon.
	for chemin := range chemins {
		if strings.HasPrefix(chemin, PrefixeAPI) {
			t.Errorf("chemin préfixé : %q", chemin)
		}
		if !strings.HasPrefix(chemin, "/") {
			t.Errorf("chemin invalide : %q", chemin)
		}
	}
	// La documentation ne se documente pas elle-même.
	if _, present := chemins["/openapi.json"]; present {
		t.Error("la route OpenAPI ne doit pas figurer dans le document")
	}

	// Quelques routes attendues.
	for _, attendu := range []string{"/users", "/users/{sam}", "/users/{sam}/password",
		"/groups", "/groups/{cn}", "/ous", "/ous/{dn}", "/portal/requests",
		"/portal/requests/{demande_id}/approve", "/portal/requests/{demande_id}/refuse",
		"/webhooks", "/webhooks/{webhook_id}", "/webhooks/{webhook_id}/test",
		"/webhooks/events", "/audit", "/health", "/info"} {
		if _, present := chemins[attendu]; !present {
			t.Errorf("chemin %q absent du document", attendu)
		}
	}

	// `/webhooks/events` ne doit pas être confondu avec un paramètre.
	operations := chemins["/users/{sam}"].(map[string]any)
	detail, ok := operations["get"].(map[string]any)
	if !ok {
		t.Fatalf("opération GET absente : %v", operations)
	}
	if detail["operationId"] != "get_users_sam" {
		t.Errorf("operationId = %v", detail["operationId"])
	}
	if detail["summary"] != "Détail d'un utilisateur" {
		t.Errorf("summary = %v", detail["summary"])
	}
	parametres, ok := detail["parameters"].([]map[string]any)
	if !ok || len(parametres) != 1 || parametres[0]["name"] != "sam" ||
		parametres[0]["in"] != "path" || parametres[0]["required"] != true {
		t.Errorf("paramètres = %v", detail["parameters"])
	}
	if operations["post"] != nil {
		t.Errorf("POST ne doit pas exister sur /users/{sam} : %v", operations["post"])
	}

	// Une opération d'écriture déclare un corps et une réponse 422.
	creation := chemins["/users"].(map[string]any)["post"].(map[string]any)
	if creation["requestBody"] == nil {
		t.Error("le corps de requête doit être décrit")
	}
	if creation["responses"].(map[string]any)["422"] == nil {
		t.Error("la réponse 422 doit être décrite")
	}
	if creation["tags"].([]string)[0] != "users" {
		t.Errorf("étiquettes = %v", creation["tags"])
	}

	// Les schémas et les schémas de sécurité sont présents.
	composants := document["components"].(map[string]any)
	if composants["securitySchemes"].(map[string]any)["ApiKeyHeader"] == nil {
		t.Error("le schéma de sécurité ApiKeyHeader est absent")
	}
	if composants["schemas"].(map[string]any)["Utilisateur"] == nil {
		t.Error("le schéma Utilisateur est absent")
	}
}

// TestOpenAPIRoute vérifie que la route sert un document valide.
func TestOpenAPIRoute(t *testing.T) {
	b := nouveauBanc(t)
	enregistreur := b.appeler(t, "GET", "/api/v1/openapi.json", "")
	if enregistreur.Code != 200 {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	document := donnees(t, enregistreur)
	if document["openapi"] != "3.0.3" {
		t.Errorf("document = %v", document["openapi"])
	}
	// Sans clé également (documentation publique).
	anonyme := httptestGet(t, b, "/openapi.json")
	if anonyme.Code != 200 {
		t.Errorf("statut sans clé = %d", anonyme.Code)
	}
}

// TestEcrireDocumentOpenAPI vérifie l'écriture du document sur disque.
func TestEcrireDocumentOpenAPI(t *testing.T) {
	b := nouveauBanc(t)
	chemin := filepath.Join(t.TempDir(), NomFichierOpenAPI)
	obtenu, err := EcrireDocumentOpenAPI(chemin, "4.0.0", b.serveur.Routeur)
	if err != nil {
		t.Fatalf("écriture: %v", err)
	}
	if obtenu != chemin {
		t.Errorf("chemin = %q", obtenu)
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	var document map[string]any
	if err := json.Unmarshal(donnees, &document); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if document["openapi"] != "3.0.3" {
		t.Errorf("document = %v", document["openapi"])
	}
	infos, err := os.Stat(chemin)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if infos.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %v", infos.Mode().Perm())
	}
}

// TestDocumentOpenAPISansRouteur vérifie la tolérance à un routeur absent.
func TestDocumentOpenAPISansRouteur(t *testing.T) {
	document := DocumentOpenAPI("", nil)
	if document["openapi"] != "3.0.3" {
		t.Fatalf("document = %v", document)
	}
	if len(document["paths"].(map[string]any)) != 0 {
		t.Errorf("chemins = %v", document["paths"])
	}
	if document["info"].(map[string]any)["version"] != VersionAPI {
		t.Errorf("info = %v", document["info"])
	}
}
