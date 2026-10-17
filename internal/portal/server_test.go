package portal

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// demander envoie une requête au serveur du portail et retourne la réponse.
func demander(t *testing.T, b *bouchonTest, methode, chemin string, corps url.Values) *http.Response {
	t.Helper()
	serveur := NouveauServeur(b.service, "127.0.0.1", 0)

	var requete *http.Request
	if corps != nil {
		requete = httptest.NewRequest(methode, chemin, strings.NewReader(corps.Encode()))
		requete.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		requete = httptest.NewRequest(methode, chemin, nil)
	}
	recorder := httptest.NewRecorder()
	serveur.routeur().ServeHTTP(recorder, requete)
	return recorder.Result()
}

// lireCorps lit le corps d'une réponse et la referme.
func lireCorps(t *testing.T, reponse *http.Response) string {
	t.Helper()
	defer reponse.Body.Close()
	brut, err := io.ReadAll(reponse.Body)
	if err != nil {
		t.Fatalf("lecture du corps: %v", err)
	}
	return string(brut)
}

// TestServeurAccueil vérifie la page d'accueil et les en-têtes de sécurité.
func TestServeurAccueil(t *testing.T) {
	b := nouveauBouchonTest(t)
	reponse := demander(t, b, http.MethodGet, "/", nil)
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if got := reponse.Header.Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q", got)
	}
	if got := reponse.Header.Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q", got)
	}
	if got := reponse.Header.Get("Server"); got != "EduSyncPortail" {
		t.Errorf("Server = %q", got)
	}
	corps := lireCorps(t, reponse)
	for _, attendu := range []string{
		"Réinitialiser mon mot de passe", "Consulter mon identifiant",
		"Demander la création d'un compte", "Portail auto-service",
	} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("page d'accueil sans %q", attendu)
		}
	}
}

// TestServeurCodeParcours couvre l'affichage et le traitement du formulaire de
// demande de code.
func TestServeurCodeParcours(t *testing.T) {
	b := nouveauBouchonTest(t)

	reponse := demander(t, b, http.MethodGet, "/code?portee="+ScopeConsultation, nil)
	corps := lireCorps(t, reponse)
	if reponse.StatusCode != http.StatusOK ||
		!strings.Contains(corps, "Consulter mon identifiant") {
		t.Fatalf("statut = %d, corps = %s", reponse.StatusCode, corps)
	}
	if !strings.Contains(corps, `name="portee" value="consultation"`) {
		t.Fatalf("portée absente : %s", corps)
	}

	reponse = demander(t, b, http.MethodPost, "/code", url.Values{
		"portee":      {ScopeConsultation},
		"identifiant": {"l.martin"},
	})
	corps = lireCorps(t, reponse)
	if reponse.StatusCode != http.StatusOK ||
		!strings.Contains(corps, "Saisir le code reçu") {
		t.Fatalf("statut = %d, corps = %s", reponse.StatusCode, corps)
	}
	ticket := b.dernierCode(t) // le code a bien été envoyé
	if !strings.Contains(corps, "Saisir le code reçu") || ticket == "" {
		t.Fatalf("corps = %s", corps)
	}

	// Une portée inconnue retombe sur la réinitialisation.
	reponse = demander(t, b, http.MethodGet, "/code?portee=n-importe-quoi", nil)
	corps = lireCorps(t, reponse)
	if !strings.Contains(corps, "Réinitialiser mon mot de passe") {
		t.Fatalf("portée par défaut : %s", corps)
	}
}

// TestServeurVerificationReinitialisation couvre la vérification d'un code et
// la réinitialisation du mot de passe depuis le portail web.
func TestServeurVerificationReinitialisation(t *testing.T) {
	b := nouveauBouchonTest(t)
	_, ticket := b.service.DemanderCode("l.martin", ScopeReinitialisation)
	code := b.dernierCode(t)

	// Affichage du formulaire.
	reponse := demander(t, b, http.MethodGet,
		"/verifier?ticket="+ticket+"&portee="+ScopeReinitialisation, nil)
	corps := lireCorps(t, reponse)
	if reponse.StatusCode != http.StatusOK ||
		!strings.Contains(corps, `name="mot_de_passe"`) ||
		!strings.Contains(corps, `name="ticket" value="`+ticket+`"`) {
		t.Fatalf("statut = %d, corps = %s", reponse.StatusCode, corps)
	}
	if !strings.Contains(corps, "Politique appliquée") {
		t.Fatalf("indice de politique absent : %s", corps)
	}

	// Sans ticket : erreur explicite.
	reponse = demander(t, b, http.MethodGet, "/verifier", nil)
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut sans ticket = %d", reponse.StatusCode)
	}
	lireCorps(t, reponse)

	// Confirmations différentes.
	reponse = demander(t, b, http.MethodPost, "/verifier", url.Values{
		"ticket": {ticket}, "portee": {ScopeReinitialisation}, "code": {code},
		"mot_de_passe": {"Voyage2026!"}, "confirmation": {"Autre2026!"},
	})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if corps := lireCorps(t, reponse); !strings.Contains(corps, "diffèrent") {
		t.Fatalf("corps = %s", corps)
	}

	// Mot de passe trop faible.
	reponse = demander(t, b, http.MethodPost, "/verifier", url.Values{
		"ticket": {ticket}, "portee": {ScopeReinitialisation}, "code": {code},
		"mot_de_passe": {"court"}, "confirmation": {"court"},
	})
	if corps := lireCorps(t, reponse); !strings.Contains(corps, "au moins 8 caractères") {
		t.Fatalf("corps = %s", corps)
	}

	// Succès.
	reponse = demander(t, b, http.MethodPost, "/verifier", url.Values{
		"ticket": {ticket}, "portee": {ScopeReinitialisation}, "code": {code},
		"mot_de_passe": {"Voyage2026!"}, "confirmation": {"Voyage2026!"},
	})
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if corps := lireCorps(t, reponse); !strings.Contains(corps, "Mot de passe modifié") {
		t.Fatalf("corps = %s", corps)
	}
	if b.annuaire.mdpDefinis[utilisateurTest().DN] != "Voyage2026!" {
		t.Fatal("le mot de passe n'a pas été écrit dans l'annuaire")
	}
}

// TestServeurVerificationConsultation vérifie l'affichage des identifiants.
func TestServeurVerificationConsultation(t *testing.T) {
	b := nouveauBouchonTest(t)
	_, ticket := b.service.DemanderCode("l.martin", ScopeConsultation)
	code := b.dernierCode(t)

	reponse := demander(t, b, http.MethodPost, "/verifier", url.Values{
		"ticket": {ticket}, "portee": {ScopeConsultation}, "code": {code},
	})
	corps := lireCorps(t, reponse)
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	for _, attendu := range []string{"Vos identifiants", "l.martin",
		"lea.martin@college.fr", "Léa Martin"} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("corps sans %q : %s", attendu, corps)
		}
	}

	// Mauvais code : 400 et message d'erreur.
	reponse = demander(t, b, http.MethodPost, "/verifier", url.Values{
		"ticket": {ticket}, "portee": {ScopeConsultation}, "code": {"000000"},
	})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	lireCorps(t, reponse)
}

// TestServeurDemande couvre le formulaire de pré-inscription.
func TestServeurDemande(t *testing.T) {
	b := nouveauBouchonTest(t)

	reponse := demander(t, b, http.MethodGet, "/demande", nil)
	corps := lireCorps(t, reponse)
	if reponse.StatusCode != http.StatusOK ||
		!strings.Contains(corps, "Demande de création de compte") {
		t.Fatalf("statut = %d, corps = %s", reponse.StatusCode, corps)
	}

	// Saisie invalide : formulaire renvoyé avec l'erreur.
	reponse = demander(t, b, http.MethodPost, "/demande", url.Values{
		"profil": {ProfilEleve}, "prenom": {""}, "nom": {"Martin"},
	})
	if reponse.StatusCode != http.StatusBadRequest {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if corps := lireCorps(t, reponse); !strings.Contains(corps, "prénom et le nom") {
		t.Fatalf("corps = %s", corps)
	}

	// Saisie valide.
	reponse = demander(t, b, http.MethodPost, "/demande", url.Values{
		"profil": {ProfilEleve}, "prenom": {"Léa"}, "nom": {"Martin"},
		"mail": {"lea.martin@college.fr"}, "classe": {"6e A"}, "motif": {"arrivée"},
	})
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if corps := lireCorps(t, reponse); !strings.Contains(corps, "transmise à l") ||
		!strings.Contains(corps, "administrateur") {
		t.Fatalf("corps = %s", corps)
	}
	demandes, _ := b.store.ListerDemandes(StatutEnAttente)
	if len(demandes) != 1 {
		t.Fatalf("demandes = %+v", demandes)
	}
}

// TestServeurEtat vérifie la publication JSON de l'état du service.
func TestServeurEtat(t *testing.T) {
	b := nouveauBouchonTest(t)
	if _, err := b.store.CreerDemande(ProfilEleve, "Léa", "Martin", "", "", "",
		b.moment); err != nil {
		t.Fatal(err)
	}

	reponse := demander(t, b, http.MethodGet, "/etat", nil)
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	if got := reponse.Header.Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	var charge map[string]any
	if err := json.Unmarshal([]byte(lireCorps(t, reponse)), &charge); err != nil {
		t.Fatalf("JSON invalide : %v", err)
	}
	if charge["service"] != "portail" || charge["titre"] != "Portail auto-service" ||
		charge["code_minutes"].(float64) != 10 ||
		charge["tentatives_max"].(float64) != 5 ||
		charge["demandes_en_attente"].(float64) != 1 ||
		charge["base_url"] != "http://127.0.0.1:8787" {
		t.Fatalf("charge = %+v", charge)
	}
}

// TestServeurLimites vérifie le refus des corps trop volumineux et des formats
// non pris en charge, ainsi que la page 404.
func TestServeurLimites(t *testing.T) {
	b := nouveauBouchonTest(t)

	reponse := demander(t, b, http.MethodGet, "/inexistant", nil)
	if reponse.StatusCode != http.StatusNotFound {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	lireCorps(t, reponse)

	gros := url.Values{"identifiant": {strings.Repeat("x", CorpsMax+10)}}
	reponse = demander(t, b, http.MethodPost, "/code", gros)
	if reponse.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	lireCorps(t, reponse)

	serveur := NouveauServeur(b.service, "127.0.0.1", 0)
	requete := httptest.NewRequest(http.MethodPost, "/code",
		strings.NewReader("identifiant=x"))
	requete.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	serveur.routeur().ServeHTTP(recorder, requete)
	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("statut = %d", recorder.Code)
	}
}

// TestServeurEchappementHTML vérifie qu'aucune valeur (titre du portail,
// identifiant de l'annuaire) n'est écrite brute dans la page.
func TestServeurEchappementHTML(t *testing.T) {
	b := nouveauBouchonTest(t)
	b.service.Config.Titre = `Edu<script>alert(1)</script>`
	b.annuaire.utilisateurs["x.y"] = &Utilisateur{
		DN:   "CN=X,DC=X",
		SAM:  `x"><script>alert(2)</script>`,
		CN:   `<b>X</b>`,
		Mail: "x@college.fr",
	}

	reponse := demander(t, b, http.MethodGet, "/", nil)
	corps := lireCorps(t, reponse)
	if strings.Contains(corps, "<script>") || strings.Contains(corps, "<b>X</b>") {
		t.Fatalf("valeur non échappée : %s", corps)
	}
	if !strings.Contains(corps, "&lt;script&gt;") {
		t.Fatalf("échappement attendu absent : %s", corps)
	}

	_, ticket := b.service.DemanderCode("x.y", ScopeConsultation)
	code := b.dernierCode(t)
	reponse = demander(t, b, http.MethodPost, "/verifier", url.Values{
		"ticket": {ticket}, "portee": {ScopeConsultation}, "code": {code},
	})
	corps = lireCorps(t, reponse)
	if strings.Contains(corps, "<script>") {
		t.Fatalf("identifiant non échappé : %s", corps)
	}
}

// TestServeurCycleDeVie vérifie le démarrage (port éphémère) et l'arrêt.
func TestServeurCycleDeVie(t *testing.T) {
	b := nouveauBouchonTest(t)
	serveur := NouveauServeur(b.service, "127.0.0.1", 0)

	if serveur.EnEcoute() {
		t.Fatal("le serveur ne doit pas être en écoute au départ")
	}
	adresse, err := serveur.Demarrer()
	if err != nil {
		t.Fatalf("Demarrer: %v", err)
	}
	if !serveur.EnEcoute() {
		t.Fatal("le serveur devrait être en écoute")
	}
	if !strings.HasPrefix(adresse, "http://127.0.0.1:") ||
		strings.HasSuffix(adresse, ":0") {
		t.Fatalf("adresse = %q", adresse)
	}

	// Le serveur répond réellement.
	reponse, err := http.Get(adresse + "/etat")
	if err != nil {
		t.Fatalf("appel HTTP: %v", err)
	}
	if reponse.StatusCode != http.StatusOK {
		t.Fatalf("statut = %d", reponse.StatusCode)
	}
	_ = reponse.Body.Close()

	// Un second appel est idempotent.
	if seconde, err := serveur.Demarrer(); err != nil || seconde != adresse {
		t.Fatalf("second démarrage = %q (%v)", seconde, err)
	}

	serveur.Arreter()
	serveur.Arreter() // idempotent
	if serveur.EnEcoute() {
		t.Fatal("le serveur devrait être arrêté")
	}
}
