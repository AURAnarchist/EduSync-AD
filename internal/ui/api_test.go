package ui

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/api"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
)

// preparerAPI ouvre un serveur d'interface et une session locale, puis crée
// une clé d'API dans le dossier de test. Le pool AD n'est pas installé : le
// serveur de l'API doit démarrer malgré tout (routes AD en 503).
func preparerAPI(t *testing.T) (*Server, string, map[string]string, string) {
	t.Helper()
	s, dir := nouveauServeur(t)
	cookie := ouvrirSessionLocale(t, s)

	magasin, err := api.OuvrirMagasinCles(filepath.Join(dir, api.NomFichierCles))
	if err != nil {
		t.Fatalf("ouverture du magasin de clés: %v", err)
	}
	cle, _, err := magasin.Creer("Supervision", []string{"*"}, 0, time.Now().UTC())
	if err != nil {
		t.Fatalf("création de la clé: %v", err)
	}
	return s, dir, entetesSession(cookie), cle
}

// TestAPIPageEtConfig vérifie le rendu de la page et l'enregistrement des
// réglages, y compris le refus des valeurs invalides.
func TestAPIPageEtConfig(t *testing.T) {
	s, dir, entetes, _ := preparerAPI(t)

	w := demander(t, s, "GET", "/app/api", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	body := w.Body.String()
	for _, attendu := range []string{
		"API REST et webhooks", "Clés d&#39;accès", "Webhooks", "Supervision",
		"api_keys.json", "webhooks.json", "webhook_deliveries.db",
		"users:password", "arrêté", "Comment ça marche",
		"/api/v1/users", "/openapi.json",
	} {
		if !strings.Contains(body, attendu) {
			t.Errorf("page sans %q", attendu)
		}
	}
	// La clé complète n'apparaît jamais dans la page : seul le préfixe.
	if !strings.Contains(body, "edusync_") {
		t.Error("le préfixe de la clé devrait apparaître")
	}

	// Les réglages enregistrés par le formulaire sont relus depuis le disque.
	w = demander(t, s, "POST", "/app/api/config", url.Values{
		"hote":           {"127.0.0.1"},
		"port":           {"18788"},
		"cors_origins":   {"https://admin.local, https://portail.local"},
		"rate_limit_rpm": {"120"},
		"titre":          {"API du lycée"},
		"actif":          {"1"},
	}, entetes)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303 (corps: %.300s)", w.Code, w.Body.String())
	}
	cfg := api.ChargerConfig(filepath.Join(dir, api.NomFichier))
	if cfg.Port != 18788 || cfg.LimiteRPM != 120 || cfg.Titre != "API du lycée" ||
		!cfg.Actif || len(cfg.CORSOrigines) != 2 {
		t.Fatalf("réglages enregistrés = %+v", cfg)
	}

	// Un réglage invalide est refusé et n'écrase pas le fichier.
	w = demander(t, s, "POST", "/app/api/config", url.Values{
		"hote": {"127.0.0.1"}, "port": {"0"}, "cors_origins": {"*"},
		"rate_limit_rpm": {"60"}, "titre": {"API"},
	}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, attendu 422", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Port invalide") {
		t.Fatalf("message d'erreur absent : %.300s", body)
	}
	if relu := api.ChargerConfig(filepath.Join(dir, api.NomFichier)); relu.Port != 18788 {
		t.Fatalf("le fichier ne doit pas être écrasé : port = %d", relu.Port)
	}

	// Sans session, la page est protégée.
	if w := demander(t, s, "GET", "/app/api", nil, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303 vers /login", w.Code)
	}
}

// TestAPIClesInterface vérifie la création, la révocation et la suppression
// d'une clé depuis l'interface.
func TestAPIClesInterface(t *testing.T) {
	s, dir, entetes, _ := preparerAPI(t)

	// Sans permission : refusé, rien n'est créé.
	w := demander(t, s, "POST", "/app/api/cles/creer", url.Values{
		"nom": {"Script"}, "jours": {"30"},
	}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d (corps: %.200s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "au moins une permission") {
		t.Fatalf("corps = %.300s", body)
	}

	// Sans nom : refusé par le magasin.
	w = demander(t, s, "POST", "/app/api/cles/creer", url.Values{
		"nom": {"  "}, "permissions": {"audit:read"},
	}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d (corps: %.200s)", w.Code, w.Body.String())
	}

	// Création réussie : la clé complète est affichée une fois.
	w = demander(t, s, "POST", "/app/api/cles/creer", url.Values{
		"nom": {"Script de sauvegarde"}, "jours": {"30"},
		"permissions": {"audit:read", "users:read"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "Clé créée") || !strings.Contains(body, "edusync_") {
		t.Fatalf("corps = %.300s", body)
	}
	// Extraire la clé affichée pour vérifier qu'elle est bien unique sur disque.
	debut := strings.Index(body, "edusync_")
	if debut < 0 {
		t.Fatal("clé absente du fragment")
	}
	cle := body[debut:]
	if fin := strings.IndexAny(cle, "<\n \""); fin >= 0 {
		cle = cle[:fin]
	}

	magasin, err := api.OuvrirMagasinCles(filepath.Join(dir, api.NomFichierCles))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	info := magasin.Valider(cle, time.Now().UTC())
	if info == nil || info.Nom != "Script de sauvegarde" || len(info.Permissions) != 2 {
		t.Fatalf("clé créée = %+v", info)
	}
	donnees, err := os.ReadFile(filepath.Join(dir, api.NomFichierCles))
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if strings.Contains(string(donnees), cle) {
		t.Fatal("la clé complète ne doit jamais être écrite sur disque")
	}

	// Révocation puis suppression.
	w = demander(t, s, "POST", "/app/api/cles/revoquer", url.Values{"id": {info.ID}}, entetes)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "révoquée") {
		t.Fatalf("révocation = %d (corps: %.200s)", w.Code, w.Body.String())
	}
	w = demander(t, s, "POST", "/app/api/cles/supprimer", url.Values{"id": {info.ID}}, entetes)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "supprimée") {
		t.Fatalf("suppression = %d (corps: %.200s)", w.Code, w.Body.String())
	}
	// Une seconde suppression remonte une erreur affichable.
	w = demander(t, s, "POST", "/app/api/cles/supprimer", url.Values{"id": {info.ID}}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Clé inconnue") {
		t.Fatalf("corps = %.200s", body)
	}
	// Une révocation inconnue également.
	if w := demander(t, s, "POST", "/app/api/cles/revoquer",
		url.Values{"id": {"inconnue"}}, entetes); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d", w.Code)
	}
}

// TestAPIWebhooksInterface vérifie l'ajout, le test et la suppression d'un
// webhook depuis l'interface.
func TestAPIWebhooksInterface(t *testing.T) {
	s, dir, entetes, _ := preparerAPI(t)

	receveur := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer receveur.Close()

	// Nom ou URL manquants.
	w := demander(t, s, "POST", "/app/api/webhooks/ajouter",
		url.Values{"events": {"*"}}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "obligatoires") {
		t.Fatalf("corps = %.200s", body)
	}
	// Sans événement : la validation du magasin refuse.
	w = demander(t, s, "POST", "/app/api/webhooks/ajouter",
		url.Values{"nom": {"Vide"}, "url": {receveur.URL}}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d (corps: %.200s)", w.Code, w.Body.String())
	}

	// Ajout complet, avec secret chiffré.
	w = demander(t, s, "POST", "/app/api/webhooks/ajouter", url.Values{
		"nom":          {"Supervision"},
		"url":          {receveur.URL},
		"events":       {"user.created, user.deleted"},
		"secret":       {"s3cr3t"},
		"retry_policy": {"fixed"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Webhook") {
		t.Fatalf("corps = %.300s", body)
	}

	store, err := webhooks.OuvrirStore(filepath.Join(dir, webhooks.NomFichier),
		filepath.Join(dir, webhooks.NomFichierLivraisons))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	defer func() { _ = store.Fermer() }()
	liste := store.Lister(false)
	if len(liste) != 1 || len(liste[0].Evenements) != 2 || liste[0].PolitiqueRetrai != "fixed" {
		t.Fatalf("webhooks = %+v", liste)
	}
	if liste[0].Secret == "s3cr3t" || liste[0].Secret == "" {
		t.Fatalf("secret non chiffré = %q", liste[0].Secret)
	}
	identifiant := liste[0].ID

	// La page affiche l'abonnement.
	w = demander(t, s, "GET", "/app/api", nil, entetes)
	if body := w.Body.String(); !strings.Contains(body, identifiant) ||
		!strings.Contains(body, "user.created") {
		t.Fatalf("page = %.300s", body)
	}

	// Envoi de test : une livraison est journalisée.
	w = demander(t, s, "POST", "/app/api/webhooks/tester", url.Values{
		"id": {identifiant}, "event": {"system.startup"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("test = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "succès") {
		t.Fatalf("corps = %.300s", body)
	}
	livraisons, err := store.Livraisons(identifiant, 10)
	if err != nil || len(livraisons) != 1 {
		t.Fatalf("livraisons = %+v (%v)", livraisons, err)
	}

	// Webhook inconnu.
	if w := demander(t, s, "POST", "/app/api/webhooks/tester",
		url.Values{"id": {"inconnu"}}, entetes); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d", w.Code)
	}

	// Suppression.
	w = demander(t, s, "POST", "/app/api/webhooks/supprimer",
		url.Values{"id": {identifiant}}, entetes)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "supprimé") {
		t.Fatalf("suppression = %d (corps: %.200s)", w.Code, w.Body.String())
	}
	if w := demander(t, s, "POST", "/app/api/webhooks/supprimer",
		url.Values{"id": {identifiant}}, entetes); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("seconde suppression = %d", w.Code)
	}
}

// TestAPIDemarrageSansCle vérifie qu'aucun serveur ne peut démarrer sans clé.
func TestAPIDemarrageSansCle(t *testing.T) {
	s, dir, entetes, _ := preparerAPI(t)
	if err := os.Remove(filepath.Join(dir, api.NomFichierCles)); err != nil {
		t.Fatalf("suppression des clés: %v", err)
	}

	w := demander(t, s, "POST", "/app/api/demarrer", url.Values{}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "au moins une clé") {
		t.Fatalf("corps = %.300s", body)
	}
	if enEcoute, _ := s.etatAPI(); enEcoute {
		t.Fatal("aucun serveur ne doit écouter après un échec")
	}
}

// TestAPICycleDeVieServeur vérifie le démarrage et l'arrêt réels du serveur.
func TestAPICycleDeVieServeur(t *testing.T) {
	s, _, entetes, cle := preparerAPI(t)
	port := portLibre(t)

	w := demander(t, s, "POST", "/app/api/config", url.Values{
		"hote": {"127.0.0.1"}, "port": {strconv.Itoa(port)},
		"cors_origins": {"*"}, "rate_limit_rpm": {"60"}, "titre": {"API de test"},
	}, entetes)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("réglages: code = %d (corps: %.300s)", w.Code, w.Body.String())
	}

	w = demander(t, s, "POST", "/app/api/demarrer", url.Values{}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("démarrage: code = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	body := w.Body.String()
	if !strings.Contains(body, "API démarrée") {
		t.Fatalf("corps = %.300s", body)
	}
	// Sans connexion AD, l'avertissement accompagne le démarrage.
	if !strings.Contains(body, "Aucune connexion AD") {
		t.Errorf("avertissement AD absent : %.300s", body)
	}
	enEcoute, adresse := s.etatAPI()
	if !enEcoute || !strings.HasSuffix(adresse, ":"+strconv.Itoa(port)) {
		t.Fatalf("état = %v %q", enEcoute, adresse)
	}

	// Le serveur répond réellement : santé avec la clé, refus sans clé.
	requete, err := http.NewRequest(http.MethodGet, adresse+"/api/v1/health", nil)
	if err != nil {
		t.Fatalf("requête: %v", err)
	}
	requete.Header.Set("X-API-Key", cle)
	reponse, err := (&http.Client{Timeout: 5 * time.Second}).Do(requete)
	if err != nil {
		t.Fatalf("appel de l'API: %v", err)
	}
	corps := make([]byte, 1024)
	n, _ := reponse.Body.Read(corps)
	_ = reponse.Body.Close()
	if reponse.StatusCode != http.StatusOK || !strings.Contains(string(corps[:n]), `"status"`) {
		t.Fatalf("réponse = %d %.200s", reponse.StatusCode, corps[:n])
	}
	anonyme, err := http.Get(adresse + "/api/v1/health")
	if err != nil {
		t.Fatalf("appel anonyme: %v", err)
	}
	_ = anonyme.Body.Close()
	if anonyme.StatusCode != http.StatusUnauthorized {
		t.Errorf("statut sans clé = %d", anonyme.StatusCode)
	}
	// La publication (routes AD) répond 503 tant qu'aucune connexion AD n'est
	// disponible : le serveur reste stable.
	requete, _ = http.NewRequest(http.MethodGet, adresse+"/api/v1/users", nil)
	requete.Header.Set("X-API-Key", cle)
	reponse, err = (&http.Client{Timeout: 5 * time.Second}).Do(requete)
	if err != nil {
		t.Fatalf("appel /users: %v", err)
	}
	_ = reponse.Body.Close()
	if reponse.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("statut /users sans AD = %d", reponse.StatusCode)
	}

	// La page reflète l'écoute en cours.
	w = demander(t, s, "GET", "/app/api", nil, entetes)
	if body := w.Body.String(); !strings.Contains(body, "en écoute") ||
		!strings.Contains(body, adresse) {
		t.Fatalf("la page doit refléter l'écoute : %.300s", body)
	}

	// Un second démarrage ne crée pas de second serveur.
	w = demander(t, s, "POST", "/app/api/demarrer", url.Values{}, entetes)
	if body := w.Body.String(); !strings.Contains(body, "déjà en écoute") {
		t.Fatalf("second démarrage : %.300s", body)
	}

	w = demander(t, s, "POST", "/app/api/arreter", url.Values{}, entetes)
	if body := w.Body.String(); !strings.Contains(body, "API arrêtée") {
		t.Fatalf("arrêt : %.300s", body)
	}
	if enEcoute, _ := s.etatAPI(); enEcoute {
		t.Fatal("l'API doit être arrêtée")
	}
	// Le port est libéré : les ressources ont bien été refermées.
	if ecoute, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port)); err != nil {
		t.Fatalf("le port devrait être libéré : %v", err)
	} else {
		_ = ecoute.Close()
	}
}

// TestAPIEtatFragment vérifie le fragment d'état.
func TestAPIEtatFragment(t *testing.T) {
	s, _, entetes, _ := preparerAPI(t)

	w := demander(t, s, "GET", "/app/api/etat", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, `id="api-etat"`) ||
		!strings.Contains(body, "arrêté") {
		t.Fatalf("fragment = %.200s", body)
	}
}

// TestAPIArretSansServeur vérifie qu'arrêter un serveur inexistant reste sans
// effet.
func TestAPIArretSansServeur(t *testing.T) {
	s, _, entetes, _ := preparerAPI(t)
	w := demander(t, s, "POST", "/app/api/arreter", url.Values{}, entetes)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "API arrêtée") {
		t.Fatalf("code = %d (corps: %.200s)", w.Code, w.Body.String())
	}
}
