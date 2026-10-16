package ui

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/progress"
)

func nouveauServeur(t *testing.T) (*Server, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := New(Options{
		ConfigPath: filepath.Join(dir, "config.json"),
		AuditPath:  filepath.Join(dir, "journal.db"),
		Version:    "0.0.0-test",
	})
	if err != nil {
		t.Fatalf("création du serveur: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s, dir
}

func demander(t *testing.T, s *Server, methode, chemin string, form url.Values, entetes map[string]string) *httptest.ResponseRecorder {
	t.Helper()

	var corps io.Reader
	if form != nil {
		corps = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(methode, chemin, corps)
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	for k, v := range entetes {
		r.Header.Set(k, v)
	}

	w := httptest.NewRecorder()
	s.ServeHTTP(w, r)
	return w
}

// ouvrirSession effectue une connexion complète et renvoie le cookie de session.
func ouvrirSession(t *testing.T, s *Server) *http.Cookie {
	t.Helper()
	w := demander(t, s, "POST", "/login", url.Values{
		"domaine":     {"EDUSYNC.LOCAL"},
		"controleur":  {"dc1.edusync.local"},
		"utilisateur": {"Administrator"},
		"motdepasse":  {"TestPass123"},
	}, nil)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303 (corps: %s)", w.Code, w.Body.String())
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieSession && c.Value != "" {
			return c
		}
	}
	t.Fatal("cookie de session absent")
	return nil
}

func TestAccueilAfficheLaConnexion(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "Connexion au domaine") {
		t.Error("la page de connexion doit être affichée")
	}
	if !strings.Contains(body, "0.0.0-test") {
		t.Error("la version doit figurer dans la page")
	}
	if !strings.Contains(body, "/static/js/htmx.min.js") {
		t.Error("HTMX doit être chargé")
	}
	if !strings.Contains(body, "/static/css/tailwind.css") {
		t.Error("Tailwind doit être chargé")
	}
}

func TestConnexionRefuseeSiChampsManquants(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "POST", "/login", url.Values{}, nil)
	if w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("code = %d, attendu 422", w.Code)
	}
	body := w.Body.String()
	for _, attendu := range []string{"domaine est obligatoire", "contrôleur de domaine est obligatoire", "utilisateur est obligatoire"} {
		if !strings.Contains(body, attendu) {
			t.Errorf("message manquant: %q (corps: %s)", attendu, body)
		}
	}
}

func TestConnexionReussie(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "POST", "/login", url.Values{
		"domaine":     {"EDUSYNC.LOCAL"},
		"controleur":  {"dc1"},
		"utilisateur": {"admin"},
		"motdepasse":  {"TestPass123"},
	}, nil)

	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/app" {
		t.Errorf("Location = %q, attendu /app", loc)
	}

	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieSession {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value == "" {
		t.Fatal("cookie de session manquant")
	}
	if !cookie.HttpOnly {
		t.Error("le cookie de session doit être HttpOnly")
	}
}

// TestConnexionViaHTMX vérifie la variante utilisée par l'interface.
func TestConnexionViaHTMX(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "POST", "/login", url.Values{
		"domaine":     {"EDUSYNC.LOCAL"},
		"controleur":  {"dc1"},
		"utilisateur": {"admin"},
		"motdepasse":  {"TestPass123"},
	}, map[string]string{"HX-Request": "true"})

	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, attendu 200", w.Code)
	}
	if vers := w.Header().Get("HX-Redirect"); vers != "/app" {
		t.Errorf("HX-Redirect = %q, attendu /app", vers)
	}
}

func TestTableauBordProtege(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303", w.Code)
	}
	if loc := w.Header().Get("Location"); loc != "/login" {
		t.Errorf("Location = %q, attendu /login", loc)
	}
}

func TestTableauBordApresConnexion(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	body := w.Body.String()
	for _, attendu := range []string{"Tableau de bord", "EDUSYNC.LOCAL", "dc1.edusync.local", "0.0.0-test"} {
		if !strings.Contains(body, attendu) {
			t.Errorf("contenu manquant: %q", attendu)
		}
	}
}

func TestDeconnexion(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/logout", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/login" {
		t.Fatalf("code=%d location=%q", w.Code, w.Header().Get("Location"))
	}

	// L'ancien jeton ne doit plus ouvrir l'application.
	apres := demander(t, s, "GET", "/app", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if apres.Code != http.StatusSeeOther || apres.Header().Get("Location") != "/login" {
		t.Errorf("la session doit être invalide après déconnexion (code=%d)", apres.Code)
	}
}

// TestJournaliseConnexionEtDeconnexion : le journal est le cœur de la
// traçabilité, il doit se remplir dès la connexion.
func TestJournaliseConnexionEtDeconnexion(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	l, err := audit.Open(filepath.Join(dir, "journal.db"))
	if err != nil {
		t.Fatalf("réouverture du journal: %v", err)
	}
	defer l.Close() //nolint:errcheck

	entries, err := l.List(audit.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entrées, attendu 1 (connexion)", len(entries))
	}
	if entries[0].ActionType != "connexion" {
		t.Errorf("action = %q, attendu connexion", entries[0].ActionType)
	}
	if entries[0].Utilisateur != "Administrator" {
		t.Errorf("opérateur = %q", entries[0].Utilisateur)
	}
	if entries[0].Domaine != "EDUSYNC.LOCAL" {
		t.Errorf("domaine = %q", entries[0].Domaine)
	}

	demander(t, s, "POST", "/logout", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if n, _ := l.Count(); n != 2 {
		t.Errorf("%d entrées après déconnexion, attendu 2", n)
	}
}

func TestAssetsStatiques(t *testing.T) {
	s, _ := nouveauServeur(t)

	for _, chemin := range []string{
		"/static/js/htmx.min.js",
		"/static/js/alpine.min.js",
		"/static/js/app.js",
		"/static/css/tailwind.css",
	} {
		w := demander(t, s, "GET", chemin, nil, nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s: code = %d", chemin, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "text/css") && !strings.Contains(ct, "javascript") {
			t.Errorf("%s: Content-Type = %q", chemin, ct)
		}
	}
}

func TestChangementDeLangue(t *testing.T) {
	s, dir := nouveauServeur(t)

	w := demander(t, s, "POST", "/langue", url.Values{"langue": {"en"}}, nil)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d", w.Code)
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == cookieLangue {
			cookie = c
		}
	}
	if cookie == nil || cookie.Value != "en" {
		t.Fatal("cookie de langue absent ou incorrect")
	}

	// La préférence est persistée dans config.json (partageable avec Python).
	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Langue != "en" {
		t.Errorf("config.Langue = %q, attendu en", cfg.Langue)
	}

	page := demander(t, s, "GET", "/login", nil, map[string]string{"Cookie": cookieLangue + "=en"})
	if !strings.Contains(page.Body.String(), "Domain sign-in") {
		t.Errorf("la page doit être en anglais (corps: %s)", page.Body.String())
	}

	// Retour au français.
	anglais := demander(t, s, "POST", "/langue", url.Values{"langue": {"fr"}},
		map[string]string{"Cookie": cookieLangue + "=en"})
	if anglais.Code != http.StatusSeeOther {
		t.Errorf("code = %d", anglais.Code)
	}
}

func TestChangementDeTheme(t *testing.T) {
	s, dir := nouveauServeur(t)

	w := demander(t, s, "POST", "/theme", url.Values{"theme": {"sombre"}}, nil)
	if w.Code != http.StatusNoContent {
		t.Fatalf("code = %d, attendu 204", w.Code)
	}

	cfg, err := config.Load(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Theme != "sombre" {
		t.Errorf("config.Theme = %q, attendu sombre", cfg.Theme)
	}

	if code := demander(t, s, "POST", "/theme", url.Values{"theme": {"invalide"}}, nil).Code; code != http.StatusBadRequest {
		t.Errorf("un thème inconnu doit être refusé (code %d)", code)
	}
}

// TestPageDeConnexionRefleteLeTheme : la classe « sombre » doit être posée
// dès le premier rendu (aucun flash clair en démarrage).
func TestPageDeConnexionRefleteLeTheme(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/login", nil, map[string]string{"Cookie": cookieTheme + "=sombre"})
	if !strings.Contains(w.Body.String(), `class="sombre"`) {
		t.Error("la classe sombre doit être appliquée sur <html>")
	}
}

func TestRoutesInconnues(t *testing.T) {
	s, _ := nouveauServeur(t)
	w := demander(t, s, "GET", "/nimporte/quoi", nil, nil)
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, attendu 404", w.Code)
	}
}

// TestDictionnaireComplet garantit qu'aucune clé n'existe dans une seule
// langue : sinon un libellé s'afficherait en clé (« dash.titre »).
func TestDictionnaireComplet(t *testing.T) {
	for cle := range messages["fr"] {
		if _, ok := messages["en"][cle]; !ok {
			t.Errorf("clé absente de l'anglais: %s", cle)
		}
	}
	for cle := range messages["en"] {
		if _, ok := messages["fr"][cle]; !ok {
			t.Errorf("clé absente du français: %s", cle)
		}
	}
}

func TestTrRepli(t *testing.T) {
	if got := Tr("fr", "login.titre"); got != "Connexion au domaine" {
		t.Errorf("Tr(fr) = %q", got)
	}
	if got := Tr("zz", "login.titre"); got != "Connexion au domaine" {
		t.Errorf("une langue inconnue doit retomber sur le français (got %q)", got)
	}
	if got := Tr("fr", "cle.absente"); got != "cle.absente" {
		t.Errorf("une clé inconnue doit être renvoyée telle quelle (got %q)", got)
	}
}

// TestFichiersDeConfigEcrites controle le résultat réellement posé sur disque.
func TestFichiersDeConfigEcrites(t *testing.T) {
	s, dir := nouveauServeur(t)
	demander(t, s, "POST", "/theme", url.Values{"theme": {"sombre"}}, nil)
	demander(t, s, "POST", "/langue", url.Values{"langue": {"en"}}, nil)

	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatalf("config.json absent: %v", err)
	}
	if !strings.Contains(string(raw), `"theme": "sombre"`) || !strings.Contains(string(raw), `"langue": "en"`) {
		t.Errorf("préférences non enregistrées: %s", raw)
	}
}

// ─── Progression (A12) ───────────────────────────────────────────────

func TestProgressListVide(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/progress/list", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Aucune opération en cours") {
		t.Errorf("devrait afficher aucune opération, body: %s", w.Body.String())
	}
}

func TestProgressAnnuler(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	// Créer une opération directement via le tracker du serveur.
	opID, _ := s.nouvelOperation("import")
	s.majProgression(opID, 3, "traitement en cours")

	// Annuler l'opération.
	w := demander(t, s, "POST", "/app/progress/cancel", url.Values{"id": {opID}}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "annule") {
		t.Errorf("devrait confirmer l'annulation, body: %s", w.Body.String())
	}

	// Vérifier que l'opération est bien annulée.
	op := s.progress.Get(opID)
	if op == nil || op.Statut != progress.StatutAnnule {
		t.Errorf("statut = %q, attendu annule", op.Statut)
	}
}

func TestProgressAnnulerIDInconnu(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/progress/cancel", url.Values{"id": {"op-inexistant"}}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusNotFound {
		t.Errorf("code = %d, attendu 404", w.Code)
	}
}

func TestProgressPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/progress", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Suivi des opérations") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

// TestProgressAnnulerSansSession vérifie que l'annulation est protégée par auth.
func TestProgressAnnulerSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "POST", "/app/progress/cancel", url.Values{"id": {"op-1"}}, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}
