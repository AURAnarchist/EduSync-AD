package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestAuthentificationObligatoire vérifie qu'une clé valide est exigée.
func TestAuthentificationObligatoire(t *testing.T) {
	b := nouveauBanc(t)

	// Sans clé : 401.
	sansCle := httptest.NewRecorder()
	requete := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	b.serveur.servir(sansCle, requete)
	if sansCle.Code != http.StatusUnauthorized {
		t.Fatalf("statut sans clé = %d", sansCle.Code)
	}
	if code := codeErreur(t, sansCle); code != "unauthorized" {
		t.Errorf("code = %q", code)
	}

	// Clé inconnue : 401.
	inconnue := httptest.NewRecorder()
	requete = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	requete.Header.Set("X-API-Key", "edusync_inexistante")
	b.serveur.servir(inconnue, requete)
	if inconnue.Code != http.StatusUnauthorized {
		t.Fatalf("statut clé inconnue = %d", inconnue.Code)
	}

	// Clé valide par `Authorization: Bearer`.
	valide := httptest.NewRecorder()
	requete = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	requete.Header.Set("Authorization", "Bearer "+b.cle)
	b.serveur.servir(valide, requete)
	if valide.Code != http.StatusOK {
		t.Fatalf("statut avec Bearer = %d (%s)", valide.Code, valide.Body.String())
	}
	bloc := donnees(t, valide)
	if bloc["status"] != "ok" || bloc["api_version"] != VersionAPI || bloc["version"] != "4.0.0" {
		t.Errorf("données = %v", bloc)
	}

	// Le document OpenAPI est public.
	publique := httptest.NewRecorder()
	b.serveur.servir(publique, httptest.NewRequest(http.MethodGet, "/openapi.json", nil))
	if publique.Code != http.StatusOK {
		t.Fatalf("statut OpenAPI sans clé = %d", publique.Code)
	}
}

// TestPermissionsRefusees vérifie le contrôle de permission par clé.
func TestPermissionsRefusees(t *testing.T) {
	b := nouveauBanc(t)
	cle, _, err := b.magasin.Creer("Lecture seule", []string{"users:read"}, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("création de la clé: %v", err)
	}
	b.cle = cle

	// Lecture autorisée.
	if enregistreur := b.appeler(t, http.MethodGet, "/api/v1/users", ""); enregistreur.Code != http.StatusOK {
		t.Fatalf("statut GET = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	// Écriture refusée : 403 avec la permission attendue.
	enregistreur := b.appeler(t, http.MethodPost, "/api/v1/users",
		`{"sam":"x.y","mail":"x@y.fr"}`)
	if enregistreur.Code != http.StatusForbidden {
		t.Fatalf("statut POST = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	bloc := charge(t, enregistreur)["error"].(map[string]any)
	if bloc["code"] != "forbidden" ||
		!strings.Contains(bloc["message"].(string), "users:write") {
		t.Errorf("erreur = %v", bloc)
	}
}

// TestRoutageInconnu vérifie les réponses 404 et 405.
func TestRoutageInconnu(t *testing.T) {
	b := nouveauBanc(t)

	if enregistreur := b.appeler(t, http.MethodGet, "/api/v1/inconnu", ""); enregistreur.Code != http.StatusNotFound {
		t.Errorf("statut 404 = %d", enregistreur.Code)
	}
	// `/api/v1/users` existe en GET et POST : DELETE doit répondre 405.
	if enregistreur := b.appeler(t, http.MethodDelete, "/api/v1/users", ""); enregistreur.Code != http.StatusMethodNotAllowed {
		t.Errorf("statut 405 = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	// La barre oblique finale est ignorée.
	if enregistreur := b.appeler(t, http.MethodGet, "/api/v1/users/", ""); enregistreur.Code != http.StatusOK {
		t.Errorf("statut avec « / » final = %d", enregistreur.Code)
	}
	// Les routes sont aussi exposées sans préfixe (`api_routes.py`).
	if enregistreur := b.appeler(t, http.MethodGet, "/users", ""); enregistreur.Code != http.StatusOK {
		t.Errorf("statut sans préfixe = %d", enregistreur.Code)
	}
}

// TestEnTetesSecuriteEtCORS vérifie les en-têtes de sécurité et le CORS.
func TestEnTetesSecuriteEtCORS(t *testing.T) {
	b := nouveauBanc(t)

	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/health", "")
	if enregistreur.Header().Get("X-Content-Type-Options") != "nosniff" ||
		enregistreur.Header().Get("X-Frame-Options") != "DENY" ||
		enregistreur.Header().Get("Cache-Control") != "no-store" ||
		enregistreur.Header().Get("Content-Type") != "application/json; charset=utf-8" {
		t.Errorf("en-têtes = %v", enregistreur.Header())
	}

	// Origine autorisée (`*` par défaut).
	avecOrigine := httptest.NewRecorder()
	requete := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	requete.Header.Set("X-API-Key", b.cle)
	requete.Header.Set("Origin", "https://admin.edusync.local")
	b.serveur.servir(avecOrigine, requete)
	if obtenu := avecOrigine.Header().Get("Access-Control-Allow-Origin"); obtenu != "*" {
		t.Errorf("Access-Control-Allow-Origin = %q", obtenu)
	}

	// Origine refusée.
	b.serveur.Config.CORSOrigines = []string{"https://autre.local"}
	refusee := httptest.NewRecorder()
	requete = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	requete.Header.Set("X-API-Key", b.cle)
	requete.Header.Set("Origin", "https://admin.edusync.local")
	b.serveur.servir(refusee, requete)
	if obtenu := refusee.Header().Get("Access-Control-Allow-Origin"); obtenu != "" {
		t.Errorf("origine refusée = %q", obtenu)
	}

	// Préflight.
	preflight := httptest.NewRecorder()
	requete = httptest.NewRequest(http.MethodOptions, "/api/v1/users", nil)
	requete.Header.Set("Origin", "https://autre.local")
	b.serveur.servir(preflight, requete)
	if preflight.Code != http.StatusNoContent ||
		preflight.Header().Get("Access-Control-Allow-Methods") == "" {
		t.Errorf("préflight = %d (%v)", preflight.Code, preflight.Header())
	}
}

// TestLimitationDebit vérifie le plafond par minute et par clé.
func TestLimitationDebit(t *testing.T) {
	b := nouveauBanc(t)
	b.serveur.Config.LimiteRPM = 2

	for index := 1; index <= 2; index++ {
		enregistreur := b.appeler(t, http.MethodGet, "/api/v1/health", "")
		if enregistreur.Code != http.StatusOK {
			t.Fatalf("appel %d = %d", index, enregistreur.Code)
		}
		if obtenu := enregistreur.Header().Get("X-RateLimit-Limit"); obtenu != "2" {
			t.Errorf("X-RateLimit-Limit = %q", obtenu)
		}
	}
	refuse := b.appeler(t, http.MethodGet, "/api/v1/health", "")
	if refuse.Code != http.StatusTooManyRequests {
		t.Fatalf("statut = %d (%s)", refuse.Code, refuse.Body.String())
	}
	if refuse.Header().Get("Retry-After") != "60" {
		t.Errorf("Retry-After = %q", refuse.Header().Get("Retry-After"))
	}
	if code := codeErreur(t, refuse); code != "rate_limited" {
		t.Errorf("code = %q", code)
	}
	// Une limite nulle désactive le plafond.
	b.serveur.Config.LimiteRPM = 0
	if enregistreur := b.appeler(t, http.MethodGet, "/api/v1/health", ""); enregistreur.Code != http.StatusOK {
		t.Errorf("sans limite = %d", enregistreur.Code)
	}
}

// TestCorpsTropVolumineux vérifie le refus au-delà de `CorpsMax`.
func TestCorpsTropVolumineux(t *testing.T) {
	b := nouveauBanc(t)
	enorme := `{"sam":"` + strings.Repeat("x", CorpsMax) + `"}`
	enregistreur := b.appeler(t, http.MethodPost, "/api/v1/users", enorme)
	if enregistreur.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if code := codeErreur(t, enregistreur); code != "payload_too_large" {
		t.Errorf("code = %q", code)
	}
}

// TestCorpsIllisible vérifie qu'un corps non JSON produit une erreur 422.
func TestCorpsIllisible(t *testing.T) {
	b := nouveauBanc(t)
	enregistreur := b.appeler(t, http.MethodPost, "/api/v1/users", "pas du json")
	if enregistreur.Code != http.StatusUnprocessableEntity {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if code := codeErreur(t, enregistreur); code != "validation_error" {
		t.Errorf("code = %q", code)
	}
}

// TestSansAnnuaire vérifie le message explicite quand l'annuaire est absent.
func TestSansAnnuaire(t *testing.T) {
	b := nouveauBanc(t)
	deps := b.deps
	deps.Annuaire = nil
	routeur := NouveauRouteur()
	EnregistrerRoutes(routeur, deps)
	serveur := NouveauServeur(ConfigDefaut(), routeur, b.magasin)

	enregistreur := httptest.NewRecorder()
	requete := httptest.NewRequest(http.MethodGet, "/api/v1/users", nil)
	requete.Header.Set("X-API-Key", b.cle)
	serveur.servir(enregistreur, requete)
	if enregistreur.Code != http.StatusServiceUnavailable {
		t.Fatalf("statut = %d (%s)", enregistreur.Code, enregistreur.Body.String())
	}
	if code := codeErreur(t, enregistreur); code != "annuaire_indisponible" {
		t.Errorf("code = %q", code)
	}
}

// TestCycleDeVieServeur démarre le serveur sur un port réel et l'arrête.
func TestCycleDeVieServeur(t *testing.T) {
	b := nouveauBanc(t)
	b.serveur.Config.Port = portLibre(t)
	if b.serveur.EnEcoute() {
		t.Fatal("le serveur ne doit pas être en écoute avant le démarrage")
	}
	if err := b.serveur.Demarrer(); err != nil {
		t.Fatalf("démarrage: %v", err)
	}
	defer func() {
		if err := b.serveur.Arreter(); err != nil {
			t.Errorf("arrêt: %v", err)
		}
	}()
	if !b.serveur.EnEcoute() || b.serveur.Config.Port == 0 {
		t.Fatalf("état = %v, port = %d", b.serveur.EnEcoute(), b.serveur.Config.Port)
	}
	// Un second démarrage est sans effet (idempotent).
	if err := b.serveur.Demarrer(); err != nil {
		t.Fatalf("second démarrage: %v", err)
	}

	adresse := b.serveur.URL() + PrefixeAPI + "/health"
	requete, err := http.NewRequest(http.MethodGet, adresse, nil)
	if err != nil {
		t.Fatalf("requête: %v", err)
	}
	requete.Header.Set("X-API-Key", b.cle)
	reponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(requete)
	if err != nil {
		t.Fatalf("appel HTTP: %v", err)
	}
	defer reponse.Body.Close()
	corps, _ := io.ReadAll(reponse.Body)
	if reponse.StatusCode != http.StatusOK || !strings.Contains(string(corps), `"status":"ok"`) {
		t.Fatalf("réponse = %d %s", reponse.StatusCode, corps)
	}

	// Sans clé, le serveur refuse.
	reponse, err = http.Get(b.serveur.URL() + PrefixeAPI + "/health")
	if err != nil {
		t.Fatalf("appel sans clé: %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusUnauthorized {
		t.Errorf("statut sans clé = %d", reponse.StatusCode)
	}

	if err := b.serveur.Arreter(); err != nil {
		t.Fatalf("arrêt: %v", err)
	}
	if b.serveur.EnEcoute() {
		t.Error("le serveur ne doit plus être en écoute")
	}
	if err := b.serveur.Arreter(); err != nil {
		t.Errorf("second arrêt: %v", err)
	}
}

// TestDemarrageConfigurationInvalide vérifie le refus de démarrer.
func TestDemarrageConfigurationInvalide(t *testing.T) {
	b := nouveauBanc(t)
	b.serveur.Config.Port = 0
	b.serveur.Config.Hote = ""
	if err := b.serveur.Demarrer(); err == nil {
		t.Fatal("une configuration invalide doit empêcher le démarrage")
	}
}

// TestClientTest vérifie que le client en mémoire partage la même chaîne.
func TestClientTest(t *testing.T) {
	b := nouveauBanc(t)
	client := b.serveur.ClientTest(b.cle)
	reponse, err := client.Get("http://api.test" + PrefixeAPI + "/health")
	if err != nil {
		t.Fatalf("appel: %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	var charge map[string]any
	if err := json.NewDecoder(reponse.Body).Decode(&charge); err != nil {
		t.Fatalf("décodage: %v", err)
	}
	if charge["success"] != true {
		t.Errorf("charge = %v", charge)
	}

	// Sans clé : refus.
	anonyme := b.serveur.ClientTest("")
	reponse, err = anonyme.Get("http://api.test" + PrefixeAPI + "/health")
	if err != nil {
		t.Fatalf("appel: %v", err)
	}
	defer reponse.Body.Close()
	if reponse.StatusCode != http.StatusUnauthorized {
		t.Errorf("statut sans clé = %d", reponse.StatusCode)
	}
}

// TestSansMagasinCles vérifie le refus quand aucune clé ne peut être validée.
func TestSansMagasinCles(t *testing.T) {
	b := nouveauBanc(t)
	b.serveur.Magasin = nil
	enregistreur := b.appeler(t, http.MethodGet, "/api/v1/health", "")
	if enregistreur.Code != http.StatusUnauthorized {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
}

// TestRecuperationPanique vérifie qu'un panique devient un 500 JSON.
func TestRecuperationPanique(t *testing.T) {
	routeur := NouveauRouteur()
	routeur.AjouterPublique(http.MethodGet, "/boum", func(*Requete) (int, any) {
		panic("erreur volontaire")
	})
	serveur := NouveauServeur(ConfigDefaut(), routeur, nil)
	enregistreur := httptest.NewRecorder()
	serveur.servir(enregistreur, httptest.NewRequest(http.MethodGet, "/boum", nil))
	if enregistreur.Code != http.StatusInternalServerError {
		t.Fatalf("statut = %d", enregistreur.Code)
	}
	if code := codeErreur(t, enregistreur); code != "internal_error" {
		t.Errorf("code = %q", code)
	}
	// La réponse reste du JSON valide.
	charge(t, enregistreur)
}

// portLibre réserve puis libère un port TCP local : le serveur de test écoute
// ainsi sur un port réel sans risquer de collision avec une autre exécution.
func portLibre(t *testing.T) int {
	t.Helper()
	ecouteur, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("réservation d'un port: %v", err)
	}
	port := ecouteur.Addr().(*net.TCPAddr).Port
	if err := ecouteur.Close(); err != nil {
		t.Fatalf("libération du port: %v", err)
	}
	return port
}

// codeErreur extrait le code d'erreur d'une réponse.
func codeErreur(t *testing.T, enregistreur *httptest.ResponseRecorder) string {
	t.Helper()
	bloc, ok := charge(t, enregistreur)["error"].(map[string]any)
	if !ok {
		t.Fatalf("bloc d'erreur absent: %s", enregistreur.Body.String())
	}
	code, _ := bloc["code"].(string)
	return code
}

// httptestGet exécute une requête GET sans clé d'API.
func httptestGet(t *testing.T, b *banc, chemin string) *httptest.ResponseRecorder {
	t.Helper()
	enregistreur := httptest.NewRecorder()
	b.serveur.servir(enregistreur, httptest.NewRequest(http.MethodGet, chemin, nil))
	return enregistreur
}
