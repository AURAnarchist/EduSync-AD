package ui

import (
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/portal"
)

// entetesSession construit l'en-tête Cookie d'une session ouverte.
func entetesSession(cookie *http.Cookie) map[string]string {
	return map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}
}

// portLibre réserve puis libère un port TCP local pour éviter les collisions
// entre tests (le portail écoute réellement pendant le test de démarrage).
func portLibre(t *testing.T) int {
	t.Helper()
	ecoute, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("port libre: %v", err)
	}
	defer ecoute.Close()
	return ecoute.Addr().(*net.TCPAddr).Port
}

// ouvrirSessionLocale ouvre une session dont le contrôleur est local : sans
// annuaire joignable, les tentatives de connexion échouent immédiatement
// (connexion refusée) au lieu d'attendre une résolution DNS.
func ouvrirSessionLocale(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	w := demander(t, s, "POST", "/login", url.Values{
		"domaine":     {"EDUSYNC.LOCAL"},
		"controleur":  {"127.0.0.1"},
		"utilisateur": {"Administrator"},
		"motdepasse":  {"TestPass123"},
	}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("connexion: code = %d (corps: %.200s)", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieSession && c.Value != "" {
			return c
		}
	}
	t.Fatal("cookie de session absent")
	return nil
}

// preparerPortail ouvre un serveur d'interface, une session et une demande de
// création de compte. `avecPool` installe un pool AD sans validation, seul
// moyen de démarrer le portail sans contrôleur de domaine réel.
func preparerPortail(t *testing.T, avecPool bool) (*Server, string, map[string]string, portal.Demande) {
	t.Helper()
	s, dir := nouveauServeur(t)
	cookie := ouvrirSessionLocale(t, s)

	if avecPool {
		pool, err := ad.NewPoolNoValidate(ad.Config{
			Domain: "EDUSYNC.LOCAL", Controller: "127.0.0.1", Port: 636,
			UseLDAPS: true, AuthMethod: ad.AuthSimple, BindDN: "Administrator",
			BindPassword: "TestPass123", TLSSkipVerify: true,
			Timeout: 2 * time.Second, MaxConns: 2,
		})
		if err != nil {
			t.Fatalf("pool sans validation: %v", err)
		}
		// Même clé que getPool (« Utilisateur@Domaine ») : le pool est ainsi
		// réutilisé tel quel par le service du portail.
		s.mu.Lock()
		s.adPools["Administrator@EDUSYNC.LOCAL"] = pool
		s.mu.Unlock()
	}

	store, err := portal.OuvrirStore(filepath.Join(dir, portal.FichierBase))
	if err != nil {
		t.Fatalf("ouverture de la base du portail: %v", err)
	}
	demande, err := store.CreerDemande(portal.ProfilEleve, "Léa", "Martin",
		"lea.martin@college.fr", "6e A", "arrivée", time.Now())
	if err != nil {
		t.Fatalf("création de la demande: %v", err)
	}
	if err := store.Fermer(); err != nil {
		t.Fatalf("fermeture de la base: %v", err)
	}
	return s, dir, entetesSession(cookie), *demande
}

// reglagesPortail écrit un fichier de réglages valide dans le dossier de test.
func reglagesPortail(t *testing.T, dir string, port int, politique string) {
	t.Helper()
	if _, err := portal.SauvegarderConfig(portal.Config{
		Hote: "127.0.0.1", Port: port, CodeMinutes: 10, TentativesMax: 5,
		EnvoisMax: 3, Politique: politique, OUAccueil: "OU=Eleves,DC=X",
		Titre: "Portail de test",
	}, filepath.Join(dir, portal.FichierConfig)); err != nil {
		t.Fatalf("réglages: %v", err)
	}
}

// TestPortailPageEtConfig vérifie le rendu de la page et l'enregistrement des
// réglages, y compris le refus des valeurs invalides.
func TestPortailPageEtConfig(t *testing.T) {
	s, dir, entetes, demande := preparerPortail(t, false)

	w := demander(t, s, "GET", "/app/portal", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	body := w.Body.String()
	for _, attendu := range []string{
		"Portail auto-service", "Réglages du portail", "Léa Martin",
		demande.ID, "6e A", "lea.martin@college.fr", "arrêté", "Comment ça marche",
	} {
		if !strings.Contains(body, attendu) {
			t.Errorf("page sans %q", attendu)
		}
	}
	// La demande ne doit jamais être traitée à la simple consultation.
	if strings.Contains(body, "Compte créé") {
		t.Error("aucun compte ne doit être créé à l'affichage de la page")
	}

	// Les réglages enregistrés par le formulaire sont relus depuis le disque.
	w = demander(t, s, "POST", "/app/portal/config", url.Values{
		"hote":              {"127.0.0.1"},
		"port":              {"18787"},
		"code_minutes":      {"8"},
		"tentatives_max":    {"3"},
		"envois_max":        {"2"},
		"politique":         {"personnel"},
		"ou_accueil":        {"OU=Eleves,DC=college,DC=fr"},
		"titre":             {"Portail du collège"},
		"notification_mail": {"1"},
		"actif":             {"1"},
	}, entetes)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303 (corps: %.200s)", w.Code, w.Body.String())
	}
	cfg := portal.ChargerConfig(filepath.Join(dir, portal.FichierConfig))
	if cfg.Port != 18787 || cfg.CodeMinutes != 8 || cfg.TentativesMax != 3 ||
		cfg.EnvoisMax != 2 || cfg.Politique != portal.ProfilPersonnel ||
		cfg.OUAccueil != "OU=Eleves,DC=college,DC=fr" || !cfg.NotificationMail || !cfg.Actif {
		t.Fatalf("réglages enregistrés = %+v", cfg)
	}

	// Un réglage invalide est refusé et n'écrase pas le fichier.
	w = demander(t, s, "POST", "/app/portal/config", url.Values{
		"hote": {"127.0.0.1"}, "port": {"0"}, "code_minutes": {"8"},
		"tentatives_max": {"3"}, "envois_max": {"2"}, "politique": {"eleve"},
		"titre": {"Portail"}, "ou_accueil": {"OU=Eleves,DC=X"},
	}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, attendu 422", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Port invalide") {
		t.Fatalf("message d'erreur absent : %.200s", body)
	}
	if relu := portal.ChargerConfig(filepath.Join(dir, portal.FichierConfig)); relu.Port != 18787 {
		t.Fatalf("le fichier ne doit pas être écrasé : port = %d", relu.Port)
	}

	// Sans session, la page est protégée.
	w = demander(t, s, "GET", "/app/portal", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303 vers /login", w.Code)
	}
}

// TestPortailSansAnnuaire vérifie que les actions qui écrivent dans l'annuaire
// échouent proprement quand aucun contrôleur n'est joignable.
func TestPortailSansAnnuaire(t *testing.T) {
	s, dir, entetes, demande := preparerPortail(t, false)
	reglagesPortail(t, dir, 18787, portal.ProfilEleve)

	w := demander(t, s, "POST", "/app/portal/demarrer", url.Values{}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("démarrage: code = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Connexion AD requise") {
		t.Fatalf("corps = %.300s", body)
	}
	if enEcoute, _ := s.etatPortail(); enEcoute {
		t.Fatal("aucun serveur ne doit écouter après un échec")
	}

	w = demander(t, s, "POST", "/app/portal/approuver", url.Values{"id": {demande.ID}}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("validation: code = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Connexion AD requise") {
		t.Fatalf("corps = %.300s", body)
	}

	// La demande reste en attente : rien n'est traité à moitié.
	store, err := portal.OuvrirStore(filepath.Join(dir, portal.FichierBase))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	defer store.Fermer()
	relue, err := store.DemandeParID(demande.ID)
	if err != nil || relue == nil || relue.Statut != portal.StatutEnAttente {
		t.Fatalf("demande = %+v (%v)", relue, err)
	}
}

// TestPortailCycleDeVieServeur vérifie le démarrage et l'arrêt réels du
// serveur du portail depuis l'interface.
func TestPortailCycleDeVieServeur(t *testing.T) {
	s, dir, entetes, _ := preparerPortail(t, true)
	port := portLibre(t)
	reglagesPortail(t, dir, port, portal.ProfilEleve)

	w := demander(t, s, "POST", "/app/portal/demarrer", url.Values{}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("démarrage: code = %d (corps: %.300s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Portail démarré") {
		t.Fatalf("corps = %.300s", body)
	}
	enEcoute, adresse := s.etatPortail()
	if !enEcoute || !strings.Contains(adresse, "127.0.0.1") {
		t.Fatalf("état = %v %q", enEcoute, adresse)
	}
	if !strings.HasSuffix(adresse, ":"+strconv.Itoa(port)) {
		t.Fatalf("adresse = %q, port attendu %d", adresse, port)
	}

	// Le serveur répond réellement.
	reponse, err := http.Get(adresse + "/etat")
	if err != nil {
		t.Fatalf("appel du portail: %v", err)
	}
	corps := make([]byte, 1024)
	n, _ := reponse.Body.Read(corps)
	_ = reponse.Body.Close()
	if reponse.StatusCode != http.StatusOK || !strings.Contains(string(corps[:n]), `"service"`) {
		t.Fatalf("réponse du portail = %d %.200s", reponse.StatusCode, corps[:n])
	}

	// La page reflète l'écoute en cours.
	w = demander(t, s, "GET", "/app/portal", nil, entetes)
	if body := w.Body.String(); !strings.Contains(body, "en écoute") ||
		!strings.Contains(body, adresse) {
		t.Fatalf("la page doit refléter l'écoute : %.300s", body)
	}

	// Un second démarrage ne crée pas de second serveur.
	w = demander(t, s, "POST", "/app/portal/demarrer", url.Values{}, entetes)
	if body := w.Body.String(); !strings.Contains(body, "déjà en écoute") {
		t.Fatalf("second démarrage : %.300s", body)
	}

	w = demander(t, s, "POST", "/app/portal/arreter", url.Values{}, entetes)
	if body := w.Body.String(); !strings.Contains(body, "Portail arrêté") {
		t.Fatalf("arrêt : %.300s", body)
	}
	if enEcoute, _ := s.etatPortail(); enEcoute {
		t.Fatal("le portail doit être arrêté")
	}
	// Le port libéré est réutilisable : la base a bien été refermée.
	ecoute, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Fatalf("le port devrait être libéré : %v", err)
	}
	_ = ecoute.Close()

	// Une configuration invalide bloque le démarrage (écrite directement : le
	// module refuse d'enregistrer une telle configuration).
	if err := os.WriteFile(filepath.Join(dir, portal.FichierConfig),
		[]byte(`{"hote":"127.0.0.1","port":18787,"politique":"inconnue"}`), 0o600); err != nil {
		t.Fatalf("écriture des réglages: %v", err)
	}
	w = demander(t, s, "POST", "/app/portal/demarrer", url.Values{}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, attendu 422", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "Politique inconnue") {
		t.Fatalf("corps = %.200s", body)
	}
}

// TestPortailRefuserDemande vérifie le refus hors ligne depuis l'interface.
func TestPortailRefuserDemande(t *testing.T) {
	s, dir, entetes, demande := preparerPortail(t, false)

	w := demander(t, s, "POST", "/app/portal/refuser", url.Values{
		"id": {demande.ID}, "note": {"doublon"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d (corps: %.200s)", w.Code, w.Body.String())
	}
	if body := w.Body.String(); !strings.Contains(body, "Demande refusée") {
		t.Fatalf("corps = %.200s", body)
	}

	store, err := portal.OuvrirStore(filepath.Join(dir, portal.FichierBase))
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	defer store.Fermer()
	relue, err := store.DemandeParID(demande.ID)
	if err != nil || relue == nil || relue.Statut != portal.StatutRefusee || relue.Note != "doublon" {
		t.Fatalf("demande = %+v (%v)", relue, err)
	}

	// Un refus de demande inconnue remonte un message affichable.
	w = demander(t, s, "POST", "/app/portal/refuser", url.Values{"id": {"inconnue"}}, entetes)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, attendu 422", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "existe plus") {
		t.Fatalf("corps = %.200s", body)
	}
}

// TestPortailEtatFragment vérifie le fragment d'état.
func TestPortailEtatFragment(t *testing.T) {
	s, _, entetes, _ := preparerPortail(t, false)

	w := demander(t, s, "GET", "/app/portal/etat", nil, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "arrêté") {
		t.Fatalf("corps = %.200s", body)
	}
}
