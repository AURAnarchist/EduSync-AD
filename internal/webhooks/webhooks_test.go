package webhooks

import (
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// nouveauMagasin ouvre un magasin dans un dossier temporaire.
func nouveauMagasin(t *testing.T) (*Store, string) {
	t.Helper()
	dossier := t.TempDir()
	store, err := OuvrirStore(filepath.Join(dossier, NomFichier),
		filepath.Join(dossier, NomFichierLivraisons))
	if err != nil {
		t.Fatalf("ouverture du magasin: %v", err)
	}
	t.Cleanup(func() { _ = store.Fermer() })
	return store, dossier
}

// TestValiderMessages vérifie les messages de validation, identiques à Python.
func TestValiderMessages(t *testing.T) {
	cas := []struct {
		nom      string
		webhook  WebhookConfig
		attendre string
	}{
		{
			"nom vide",
			WebhookConfig{URL: "https://x.fr", Evenements: []string{"user.created"},
				RetraitsMax: 5, DelaiSecondes: 10},
			"Nom requis",
		},
		{
			"url vide",
			WebhookConfig{Nom: "n", Evenements: []string{"user.created"},
				RetraitsMax: 5, DelaiSecondes: 10},
			"URL requise",
		},
		{
			"schéma inconnu",
			WebhookConfig{Nom: "n", URL: "ftp://x.fr", Evenements: []string{"user.created"},
				RetraitsMax: 5, DelaiSecondes: 10},
			"URL doit être http:// ou https://",
		},
		{
			"événements vides",
			WebhookConfig{Nom: "n", URL: "https://x.fr",
				RetraitsMax: 5, DelaiSecondes: 10},
			"Au moins un événement requis",
		},
		{
			"événement inconnu",
			WebhookConfig{Nom: "n", URL: "https://x.fr", Evenements: []string{"bidon"},
				RetraitsMax: 5, DelaiSecondes: 10},
			"Événement inconnu : bidon",
		},
		{
			"retraits",
			WebhookConfig{Nom: "n", URL: "https://x.fr", Evenements: []string{"*"},
				RetraitsMax: 21, DelaiSecondes: 10},
			"Max retries invalide (0-20)",
		},
		{
			"timeout",
			WebhookConfig{Nom: "n", URL: "https://x.fr", Evenements: []string{"*"},
				RetraitsMax: 5, DelaiSecondes: 0},
			"Timeout invalide (1-60s)",
		},
	}
	for _, test := range cas {
		erreurs := test.webhook.Valider()
		if len(erreurs) == 0 || !contient(erreurs, test.attendre) {
			t.Errorf("%s : erreurs = %v, attendu %q", test.nom, erreurs, test.attendre)
		}
	}
	if erreurs := NouveauWebhook("w1", "n", "https://x.fr", []string{"*"}).Valider(); len(erreurs) != 0 {
		t.Errorf("webhook par défaut invalide: %v", erreurs)
	}
	if erreurs := NouveauWebhook("w1", "n", "https://x.fr", Evenements).Valider(); len(erreurs) != 0 {
		t.Errorf("tous les événements standards doivent être valides: %v", erreurs)
	}
}

func contient(liste []string, attendu string) bool {
	for _, item := range liste {
		if item == attendu {
			return true
		}
	}
	return false
}

// TestAllerRetourJSON vérifie que les clés écrites sont exactement celles de
// `asdict(WebhookConfig)` côté Python.
func TestAllerRetourJSON(t *testing.T) {
	webhook := NouveauWebhook("w1", "Serveur", "https://x.fr/hook", []string{"user.created"})
	webhook.Secret = "chiffré"
	webhook.Entetes = map[string]string{"X-Client": "edusync"}

	donnees, err := ToJSON([]WebhookConfig{webhook})
	if err != nil {
		t.Fatalf("sérialisation: %v", err)
	}

	var bruts []map[string]any
	if err := json.Unmarshal(donnees, &bruts); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	clesAttendues := []string{"id", "name", "url", "events", "secret", "headers",
		"active", "created_at", "last_triggered_at", "last_status", "last_error",
		"retry_policy", "max_retries", "timeout_seconds"}
	if len(bruts) != 1 || len(bruts[0]) != len(clesAttendues) {
		t.Fatalf("clés = %v", bruts[0])
	}
	for _, cle := range clesAttendues {
		if _, ok := bruts[0][cle]; !ok {
			t.Errorf("clé %q absente", cle)
		}
	}

	relus := DepuisJSON(donnees)
	if len(relus) != 1 {
		t.Fatalf("relus = %d", len(relus))
	}
	relu := relus[0]
	if relu.ID != "w1" || relu.Nom != "Serveur" || relu.URL != "https://x.fr/hook" ||
		!relu.Actif || relu.RetraitsMax != 5 || relu.DelaiSecondes != 10 ||
		relu.PolitiqueRetrai != RetraitExponentiel || relu.DernierDecl != nil ||
		relu.DernierStatut != nil || relu.DerniereErreur != nil ||
		relu.Entetes["X-Client"] != "edusync" || len(relu.Evenements) != 1 {
		t.Fatalf("relu = %+v", relu)
	}
}

// TestDepuisJSONTolerant vérifie qu'un fichier illisible ou partiel ne bloque
// jamais (même tolérance que `_load`).
func TestDepuisJSONTolerant(t *testing.T) {
	if obtenu := DepuisJSON([]byte("pas du json")); obtenu != nil {
		t.Errorf("json invalide = %v", obtenu)
	}
	if obtenu := DepuisJSON([]byte(`{"pas": "une liste"}`)); obtenu != nil {
		t.Errorf("objet au lieu de liste = %v", obtenu)
	}
	partiel := DepuisJSON([]byte(`[{"id":"w1","url":"https://x.fr","events":["*"]}]`))
	if len(partiel) != 1 || !partiel[0].Actif || partiel[0].RetraitsMax != 5 ||
		partiel[0].DelaiSecondes != 10 || partiel[0].PolitiqueRetrai != RetraitExponentiel {
		t.Fatalf("valeurs par défaut = %+v", partiel)
	}
}

// TestStoreCycleDeVie couvre création, mise à jour, listage et suppression.
func TestStoreCycleDeVie(t *testing.T) {
	store, dossier := nouveauMagasin(t)

	webhook, err := store.Creer(NouveauWebhook("w1", "Serveur", "https://x.fr", []string{"*"}))
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	if webhook.CreeLe == "" {
		t.Error("la date de création doit être renseignée")
	}

	// Le fichier est écrit en 0600 et relisible.
	chemin := filepath.Join(dossier, NomFichier)
	infos, err := os.Stat(chemin)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if infos.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %v", infos.Mode().Perm())
	}
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if len(DepuisJSON(contenu)) != 1 {
		t.Fatalf("fichier relu = %s", contenu)
	}

	if _, err := store.Creer(WebhookConfig{ID: "w2", URL: "https://x.fr",
		Evenements: []string{"*"}, RetraitsMax: 5, DelaiSecondes: 10}); err == nil {
		t.Error("un webhook sans nom doit être refusé")
	}

	if _, err := store.MettreAJour("w1", func(cible *WebhookConfig) { cible.Actif = false }); err != nil {
		t.Fatalf("mise à jour: %v", err)
	}
	if actifs := store.Lister(true); len(actifs) != 0 {
		t.Fatalf("webhooks actifs = %d", len(actifs))
	}
	if tous := store.Lister(false); len(tous) != 1 || tous[0].Actif {
		t.Fatalf("webhooks = %+v", tous)
	}
	if _, err := store.MettreAJour("w1", func(cible *WebhookConfig) { cible.URL = "ftp://x" }); err == nil {
		t.Error("une mise à jour invalide doit être refusée")
	}
	if _, err := store.MettreAJour("inconnu", nil); err != nil {
		t.Errorf("mise à jour d'un webhook inconnu: %v", err)
	}

	supprime, err := store.Supprimer("w1")
	if err != nil || !supprime {
		t.Fatalf("suppression = %v (%v)", supprime, err)
	}
	if supprime, _ := store.Supprimer("w1"); supprime {
		t.Error("une seconde suppression doit retourner faux")
	}
	if obtenu, _ := store.Obtenir("w1"); obtenu != nil {
		t.Error("le webhook doit être absent")
	}
}

// TestDispatcherSignature vérifie le corps, les entêtes et la signature HMAC.
func TestDispatcherSignature(t *testing.T) {
	store, dossier := nouveauMagasin(t)
	dispatcher := NouveauDispatcher(store)
	dispatcher.CheminCle = filepath.Join(dossier, NomCleTest)

	secret, err := dispatcher.ChiffrerSecret("s3cr3t")
	if err != nil {
		t.Fatalf("chiffrement: %v", err)
	}
	if secret == "" || secret == "s3cr3t" {
		t.Fatalf("le secret doit être chiffré (obtenu %q)", secret)
	}

	var (
		mu      sync.Mutex
		recu    []byte
		entetes http.Header
	)
	serveur := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		corps := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(corps)
		mu.Lock()
		recu, entetes = corps, r.Header.Clone()
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer serveur.Close()

	webhook := NouveauWebhook("w1", "Serveur", serveur.URL, []string{"user.created"})
	webhook.Secret = secret
	webhook.Entetes = map[string]string{"X-Client": "edusync"}
	if _, err := store.Creer(webhook); err != nil {
		t.Fatalf("création: %v", err)
	}

	livraisons, err := dispatcher.Diffuser("user.created", map[string]any{"sam": "l.martin"})
	if err != nil {
		t.Fatalf("diffusion: %v", err)
	}
	if len(livraisons) != 1 {
		t.Fatalf("livraisons = %d", len(livraisons))
	}
	livraison := livraisons[0]
	if livraison.Statut == nil || *livraison.Statut != http.StatusAccepted {
		t.Fatalf("statut = %v", livraison.Statut)
	}
	if livraison.Erreur != nil {
		t.Errorf("erreur = %v", *livraison.Erreur)
	}
	if livraison.Tentative != 1 || livraison.TermineLe == nil || livraison.ProchaineTent != nil {
		t.Fatalf("livraison = %+v", livraison)
	}

	mu.Lock()
	defer mu.Unlock()
	var charge map[string]any
	if err := json.Unmarshal(recu, &charge); err != nil {
		t.Fatalf("corps reçu: %v (%s)", err, recu)
	}
	if charge["event"] != "user.created" || charge["timestamp"] == "" {
		t.Errorf("charge = %v", charge)
	}
	donnees, _ := charge["data"].(map[string]any)
	if donnees["sam"] != "l.martin" {
		t.Errorf("data = %v", donnees)
	}
	mac := hmac.New(sha256.New, []byte("s3cr3t"))
	mac.Write(recu)
	attendu := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if entetes.Get("X-Webhook-Signature") != attendu {
		t.Errorf("signature = %q, attendu %q", entetes.Get("X-Webhook-Signature"), attendu)
	}
	if entetes.Get("X-Webhook-Event") != "user.created" ||
		entetes.Get("X-Webhook-Delivery") != livraison.ID ||
		entetes.Get("User-Agent") != "EduSync-Webhook/1.0" ||
		entetes.Get("X-Client") != "edusync" {
		t.Errorf("entêtes = %v", entetes)
	}

	// Le journal est relisible en base, au schéma Python.
	base, err := sql.Open("sqlite", dsnSQLite(filepath.Join(dossier, NomFichierLivraisons)))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	defer base.Close()
	var (
		id, webhookID, evenement, charge2, commence string
		tentative                                   int
	)
	if err := base.QueryRow(`SELECT id, webhook_id, event, payload, attempt, started_at
		FROM deliveries`).Scan(&id, &webhookID, &evenement, &charge2, &tentative, &commence); err != nil {
		t.Fatalf("lecture du journal: %v", err)
	}
	if id != livraison.ID || webhookID != "w1" || evenement != "user.created" ||
		tentative != 1 || commence != livraison.CommenceLe {
		t.Errorf("ligne = %s %s %s %d %s", id, webhookID, evenement, tentative, commence)
	}
}

// TestDispatcherEchecEtRetrait vérifie la journalisation d'un échec HTTP et la
// planification d'une nouvelle tentative.
func TestDispatcherEchecEtRetrait(t *testing.T) {
	store, _ := nouveauMagasin(t)
	dispatcher := NouveauDispatcher(store)

	// Horloge contrôlée : le rejeu n'attend pas réellement deux minutes.
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	dispatcher.Horloge = func() time.Time { return moment }

	appels := 0
	statut := http.StatusInternalServerError
	serveur := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appels++
		w.WriteHeader(statut)
	}))
	defer serveur.Close()

	webhook := NouveauWebhook("w1", "Serveur", serveur.URL, []string{"*"})
	if _, err := store.Creer(webhook); err != nil {
		t.Fatalf("création: %v", err)
	}

	livraisons, err := dispatcher.Diffuser("group.created", nil)
	if err != nil {
		t.Fatalf("diffusion: %v", err)
	}
	if len(livraisons) != 1 {
		t.Fatalf("livraisons = %d", len(livraisons))
	}
	if livraisons[0].Erreur == nil || *livraisons[0].Erreur != "HTTP 500" {
		t.Fatalf("erreur = %v", livraisons[0].Erreur)
	}
	if livraisons[0].ProchaineTent == nil {
		t.Fatal("une nouvelle tentative doit être planifiée")
	}
	// Retrait exponentiel : 2^1 * 60 = 120 s.
	prochaine, err := time.Parse(formatISO, *livraisons[0].ProchaineTent)
	if err != nil {
		t.Fatalf("échéance: %v", err)
	}
	if ecart := prochaine.Sub(moment); ecart != 120*time.Second {
		t.Errorf("échéance dans %v, attendu 120s", ecart)
	}

	// L'état du webhook est mis à jour.
	relu, err := store.Obtenir("w1")
	if err != nil || relu == nil {
		t.Fatalf("relu = %v (%v)", relu, err)
	}
	if relu.DernierStatut == nil || *relu.DernierStatut != 500 ||
		relu.DerniereErreur == nil || *relu.DerniereErreur != "HTTP 500" ||
		relu.DernierDecl == nil {
		t.Fatalf("webhook = %+v", relu)
	}

	// Rien n'est rejoué tant que l'échéance n'est pas atteinte.
	if effectuees, err := dispatcher.ExecuterRetraits(); err != nil || len(effectuees) != 0 {
		t.Fatalf("retraits avant échéance = %+v (%v)", effectuees, err)
	}
	moment = moment.Add(2 * time.Minute)
	if enAttente, err := store.TentativesEnAttente(moment, 10); err != nil ||
		len(enAttente) != 1 {
		t.Fatalf("tentatives en attente = %d (%v)", len(enAttente), err)
	}

	// Le rejeu aboutit et ne laisse aucune ligne de plus.
	statut = http.StatusOK
	effectuees, err := dispatcher.ExecuterRetraits()
	if err != nil {
		t.Fatalf("retraits: %v", err)
	}
	if len(effectuees) != 1 || effectuees[0].Tentative != 2 {
		t.Fatalf("effectuées = %+v", effectuees)
	}
	if enAttente, _ := store.TentativesEnAttente(moment.Add(time.Hour), 10); len(enAttente) != 0 {
		t.Errorf("plus aucune tentative ne doit être en attente: %+v", enAttente)
	}
	if nombre, err := store.CompterLivraisons(); err != nil || nombre != 1 {
		t.Errorf("livraisons journalisées = %d (%v)", nombre, err)
	}
	if appels != 2 {
		t.Errorf("appels HTTP = %d, attendu 2", appels)
	}

	// Le webhook désactivé arrête les réessais sans erreur.
	_, _ = store.MettreAJour("w1", func(cible *WebhookConfig) { cible.Actif = false })
	statut = http.StatusInternalServerError
	if _, err := dispatcher.Diffuser("group.created", nil); err != nil {
		t.Fatalf("diffusion: %v", err)
	}
	moment = moment.Add(time.Hour)
	if _, err := dispatcher.ExecuterRetraits(); err != nil {
		t.Fatalf("retraits: %v", err)
	}
}

// TestDiffuserSansAbonne vérifie qu'aucun webhook abonné ne produit aucune
// livraison.
func TestDiffuserSansAbonne(t *testing.T) {
	store, _ := nouveauMagasin(t)
	dispatcher := NouveauDispatcher(store)

	if _, err := store.Creer(NouveauWebhook("w1", "Serveur", "https://x.fr",
		[]string{"user.created"})); err != nil {
		t.Fatalf("création: %v", err)
	}
	livraisons, err := dispatcher.Diffuser("group.deleted", map[string]any{"cn": "X"})
	if err != nil {
		t.Fatalf("diffusion: %v", err)
	}
	if len(livraisons) != 0 {
		t.Fatalf("livraisons = %+v", livraisons)
	}
	// Le joker reçoit tout.
	if _, err := store.Creer(NouveauWebhook("w2", "Tout", "https://x.fr", []string{"*"})); err != nil {
		t.Fatalf("création: %v", err)
	}
	dispatcher.Client = &http.Client{Timeout: time.Second}
	livraisons, err = dispatcher.Diffuser("group.deleted", nil)
	if err != nil {
		t.Fatalf("diffusion: %v", err)
	}
	if len(livraisons) != 1 || livraisons[0].WebhookID != "w2" {
		t.Fatalf("livraisons = %+v", livraisons)
	}
	if livraisons[0].Erreur == nil {
		t.Error("une URL injoignable doit être journalisée en échec")
	}
	if livraisons[0].Statut == nil || *livraisons[0].Statut != 0 {
		t.Errorf("statut = %v", livraisons[0].Statut)
	}
}

// TestSecretEnClair vérifie qu'un secret non chiffré (écrit par l'interface
// Python) reste utilisable pour signer.
func TestSecretEnClair(t *testing.T) {
	store, dossier := nouveauMagasin(t)
	dispatcher := NouveauDispatcher(store)
	dispatcher.CheminCle = filepath.Join(dossier, NomCleTest)

	var recu []byte
	serveur := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		recu = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(recu)
		w.WriteHeader(http.StatusOK)
	}))
	defer serveur.Close()

	webhook := NouveauWebhook("w1", "Serveur", serveur.URL, []string{"*"})
	webhook.Secret = "en-clair"
	if _, err := store.Creer(webhook); err != nil {
		t.Fatalf("création: %v", err)
	}
	if _, err := dispatcher.Diffuser("system.startup", nil); err != nil {
		t.Fatalf("diffusion: %v", err)
	}
	mac := hmac.New(sha256.New, []byte("en-clair"))
	mac.Write(recu)
	if obtenu, attendu := dispatcher.signer(webhook, recu), hex.EncodeToString(mac.Sum(nil)); obtenu != attendu {
		t.Errorf("signature = %q, attendu %q", obtenu, attendu)
	}
}

// TestBoucleRetrait vérifie le démarrage et l'arrêt de la boucle de réessai.
func TestBoucleRetrait(t *testing.T) {
	store, _ := nouveauMagasin(t)
	dispatcher := NouveauDispatcher(store)
	dispatcher.IntervalleRetrait = 10 * time.Millisecond

	dispatcher.Demarrer()
	dispatcher.Demarrer() // idempotent
	dispatcher.Arreter()
	dispatcher.Arreter() // idempotent
}

// TestEvenementsStables protège la liste des événements (contrat d'API).
func TestEvenementsStables(t *testing.T) {
	if len(Evenements) != 24 {
		t.Fatalf("événements = %d, attendu 24", len(Evenements))
	}
	for _, attendu := range []string{"user.created", "group.member_added", "ou.renamed",
		"portal.request_approved", "audit.entry", "system.shutdown"} {
		if !contient(Evenements, attendu) {
			t.Errorf("événement %q absent", attendu)
		}
	}
	if triés := EvenementsTries(); len(triés) != len(Evenements) {
		t.Errorf("triés = %d", len(triés))
	}
}

// NomCleTest est le nom du fichier de clé utilisé par les tests.
const NomCleTest = "secret.key"
