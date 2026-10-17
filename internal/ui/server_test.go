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
	"github.com/estemobs/EduSync-AD/internal/classspaces"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/label"
	"github.com/estemobs/EduSync-AD/internal/mailer"
	"github.com/estemobs/EduSync-AD/internal/multisite"
	"github.com/estemobs/EduSync-AD/internal/progress"
	"github.com/estemobs/EduSync-AD/internal/rbac"
	"github.com/estemobs/EduSync-AD/internal/vault"
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

// ─── Profils (M13) ───────────────────────────────────────────────

func TestProfilsPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/profils", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Profils utilisateurs") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestProfilsSimule(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/profils/simulate", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"itinérant"},
		"chemin":  {"\\\\srv\\profil\\%USERNAME%"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "itinérant") {
		t.Errorf("type absent, body: %s", w.Body.String())
	}
}

func TestProfilsExecute(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/profils/execute", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"local"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "local") {
		t.Errorf("type absent, body: %s", w.Body.String())
	}
}

func TestProfilsSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/profils", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Heures de connexion (M16) ───────────────────────────────────────────────

func TestLogonHoursPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/logon-hours", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Heures de connexion") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestLogonHoursSimule(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/logon-hours/simulate", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"preset":  {"cours"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestLogonHoursExecute(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/logon-hours/execute", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"preset":  {"admin"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestLogonHoursSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/logon-hours", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Scripts logon/logoff (M15) ───────────────────────────────────────────────

func TestLogonScriptsPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/logon-scripts", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Scripts logon/logoff") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestLogonScriptsSimule(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/logon-scripts/simulate", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"bat"},
		"contenu": {"echo hello"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestLogonScriptsExecute(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/logon-scripts/execute", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"powershell"},
		"contenu": {"Write-Host hello"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestLogonScriptsSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/logon-scripts", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Quotas de disque (M14) ───────────────────────────────────────────────

func TestQuotasPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/quotas", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Quotas de disque") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestQuotasSimule(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/quotas/simulate", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"disque"},
		"limite":  {"1073741824"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestQuotasExecute(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/quotas/execute", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"disque"},
		"limite":  {"536870912"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestQuotasSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/quotas", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Dossiers personnels (M17) ───────────────────────────────────────────────

func TestHomeDirsPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/homedirs", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Dossiers personnels") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestHomeDirsSimule(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/homedirs/simulate", url.Values{
		"user_dn":        {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":           {"standard"},
		"chemin":         {"\\\\srv\\homes\\%USERNAME%"},
		"lettre_lecteur": {"H"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestHomeDirsExecute(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/homedirs/execute", url.Values{
		"user_dn":        {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":           {"standard"},
		"chemin":         {"\\\\srv\\homes\\%USERNAME%"},
		"lettre_lecteur": {"H"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestHomeDirsSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/homedirs", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Photos d'identite (M12) ───────────────────────────────────────────────

func TestPhotosPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/photos", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Photos d") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestPhotosSimule(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/photos/simulate", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"vignette"},
		"chemin":  {"/photos/jean.jpg"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestPhotosExecute(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/photos/execute", url.Values{
		"user_dn": {"CN=Jean Dupont,OU=Users,DC=X"},
		"type":    {"passeport"},
		"chemin":  {"/photos/jean.jpg"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", w.Body.String())
	}
}

func TestPhotosSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/photos", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Espaces partagés par classe (M18) ────────────────────────────────────

func TestClassSpacesPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/class-spaces", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "Espaces partag") {
		t.Errorf("titre absent, body: %s", corps)
	}
	if !strings.Contains(corps, "share_root") {
		t.Errorf("formulaire de configuration absent")
	}
}

func TestClassSpacesConfig(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/class-spaces/config", url.Values{
		"share_root":     {`\\fs01\classes`},
		"modele_dossier": {"%GROUP%"},
		"teachers_group": {"CN=Profs,DC=lycee,DC=local"},
		"teachers_ou":    {"OU=Profs,DC=lycee,DC=local"},
		"auto_creation":  {"1"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}

	cfg := classspaces.ChargerConfig(dir)
	if cfg.PartageRacine != `\\fs01\classes` {
		t.Errorf("PartageRacine = %q", cfg.PartageRacine)
	}
	if cfg.GroupeProfs != "CN=Profs,DC=lycee,DC=local" {
		t.Errorf("GroupeProfs = %q", cfg.GroupeProfs)
	}
	if !cfg.AutoSurCreation {
		t.Error("AutoSurCreation devrait être vrai")
	}
}

func TestClassSpacesConfigInvalide(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/class-spaces/config", url.Values{
		"share_root":     {"D:/classes"}, // non UNC
		"modele_dossier": {"%GROUP%"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	// Configuration invalide : rien ne doit être enregistré.
	if cfg := classspaces.ChargerConfig(dir); cfg.PartageRacine == "D:/classes" {
		t.Error("une configuration invalide ne doit pas être enregistrée")
	}
}

func TestClassSpacesPlan(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/class-spaces/plan", url.Values{
		"group_dn":   {"CN=6emeA,OU=Classes,DC=lycee,DC=local"},
		"group_name": {"6emeA"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "attente_ad") {
		t.Errorf("statut attendu attente_ad, body: %s", corps)
	}
	if !strings.Contains(corps, `classes\6emeA`) {
		t.Errorf("chemin de l'espace absent, body: %s", corps)
	}
}

func TestClassSpacesCreationScript(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/class-spaces/creation-script", url.Values{
		"group_name": {"6emeA"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "New-SmbShare") {
		t.Errorf("script de création absent, body: %s", w.Body.String())
	}
}

func TestClassSpacesSyncScript(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/class-spaces/sync-script", url.Values{
		"group_dn":   {"CN=6emeA,OU=Classes,DC=lycee,DC=local"},
		"group_name": {"6emeA"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Get-ADGroupMember") {
		t.Errorf("script de synchro absent, body: %s", w.Body.String())
	}
}

func TestClassSpacesSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/class-spaces", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

func TestClassSpacesMarkSyncedSansAd(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/class-spaces/mark-synced", url.Values{
		"group_dn":   {"CN=6emeA,OU=Classes,DC=lycee,DC=local"},
		"group_name": {"6emeA"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "connexion AD requise") {
		t.Errorf("message attendu, body: %s", w.Body.String())
	}
}

// ─── Cache AD SQLite (T2) ─────────────────────────────────────────────────

func TestADCachePage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/ad-cache", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "Cache AD local") {
		t.Errorf("titre absent, body: %s", corps)
	}
	if !strings.Contains(corps, "ad-cache/search") {
		t.Errorf("formulaire de recherche absent")
	}
}

func TestADCacheSearchVide(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/ad-cache/search", url.Values{"query": {"ali"}},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Aucun résultat") {
		t.Errorf("cache vide attendu, body: %s", w.Body.String())
	}
}

func TestADCacheSearchSansQuery(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/ad-cache/search", url.Values{"query": {""}},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "obligatoire") {
		t.Errorf("message d'erreur attendu, body: %s", w.Body.String())
	}
}

func TestADCacheSyncSansAd(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/ad-cache/sync", url.Values{"complet": {"1"}},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Connexion AD requise") {
		t.Errorf("message attente_ad attendu, body: %s", w.Body.String())
	}
}

func TestADCacheSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/ad-cache", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Import/Export avancés (M22) ─────────────────────────────────────────

func TestAdvancedIOPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/advanced-io", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Import / Export avancés") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestAdvancedIOValidateFilter(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/advanced-io/validate-filter", url.Values{"filtre": {"(&(objectClass=user))"}},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Filtre valide") {
		t.Errorf("filtre valide attendu, body: %s", w.Body.String())
	}

	w = demander(t, s, "POST", "/app/advanced-io/validate-filter", url.Values{"filtre": {"objectClass=user"}},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if !strings.Contains(w.Body.String(), "parenthèses") {
		t.Errorf("message d'erreur attendu, body: %s", w.Body.String())
	}
}

func TestAdvancedIOSource(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/advanced-io/source", url.Values{
		"mode":  {"ou"},
		"ou_dn": {"OU=Eleves,DC=lycee,DC=local"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "OU=Eleves,DC=lycee,DC=local") || !strings.Contains(corps, "Source valide") {
		t.Errorf("source attendue, body: %s", corps)
	}
}

func TestAdvancedIOImportSansAd(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/advanced-io/import", url.Values{
		"mode":  {"ou"},
		"ou_dn": {"OU=Eleves,DC=lycee,DC=local"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d, body: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "Connexion AD requise") {
		t.Errorf("message attente_ad attendu, body: %s", w.Body.String())
	}
}

func TestAdvancedIOGEPMapping(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/advanced-io/gep-mapping", url.Values{"headers": {"NOM,PRENOM,CLASSE"}},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "NOM") || !strings.Contains(corps, "PRENOM") {
		t.Errorf("mapping attendu, body: %s", corps)
	}
}

func TestAdvancedIOSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/advanced-io", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── RDS / Bureau à distance (M21) ───────────────────────────────────────

func TestRDSPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/rds", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "RDS / Bureau à distance") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestRDSConfigEnregistrer(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/rds/config", url.Values{
		"broker":       {"rdcb.lycee.local"},
		"domain":       {"LYCEE"},
		"profile_mode": {"none"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Configuration enregistrée") {
		t.Errorf("confirmation absente, body: %s", w.Body.String())
	}
}

func TestRDSCollectionScript(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	if w := demander(t, s, "POST", "/app/rds/config", url.Values{
		"broker":       {"rdcb.lycee.local"},
		"domain":       {"LYCEE"},
		"profile_mode": {"none"},
	}, entetes); w.Code != http.StatusOK {
		t.Fatalf("config: code = %d", w.Code)
	}

	w := demander(t, s, "POST", "/app/rds/collection-script", url.Values{
		"name":   {"Edu-Sciences"},
		"hosts":  {"rdsh01.lycee.local,rdsh02.lycee.local"},
		"groups": {"Profs;Eleves"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	for _, attendu := range []string{"New-RDSessionCollection", "rdsh01.lycee.local", "LYCEE\\Profs"} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("script sans %q", attendu)
		}
	}
}

func TestRDSCollectionScriptNomObligatoire(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/rds/collection-script", url.Values{
		"hosts": {"rdsh01"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if !strings.Contains(w.Body.String(), "obligatoire") {
		t.Errorf("erreur attendue, body: %s", w.Body.String())
	}
}

func TestRDSFSLogixErreurSansConfig(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/rds/fslogix-script", url.Values{},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if !strings.Contains(w.Body.String(), "FSLogix") {
		t.Errorf("erreur FSLogix attendue, body: %s", w.Body.String())
	}
}

func TestRDSSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/rds", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Microsoft Exchange (M20) ─────────────────────────────────────────────

func TestExchangePage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/exchange", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Microsoft Exchange") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestExchangeConfigEnregistrer(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/exchange/config", url.Values{
		"mode":            {"onprem"},
		"smtp_domain":     {"lycee.fr"},
		"alias_pattern":   {"{prenom}.{nom}"},
		"mailbox_quota":   {"10GB"},
		"exchange_server": {"exch01.lycee.local"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Configuration enregistrée") {
		t.Errorf("confirmation absente, body: %s", w.Body.String())
	}
}

func TestExchangePlans(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	if w := demander(t, s, "POST", "/app/exchange/config", url.Values{
		"mode": {"onprem"}, "smtp_domain": {"lycee.fr"}, "exchange_server": {"exch01.lycee.local"},
	}, entetes); w.Code != http.StatusOK {
		t.Fatalf("config: code = %d", w.Code)
	}

	w := demander(t, s, "POST", "/app/exchange/plans", url.Values{
		"domain":  {"lycee.local"},
		"comptes": {"jdurand;Jean Durand;CN=Jean Durand,OU=Profs,DC=lycee,DC=local"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	for _, attendu := range []string{"jdurand@lycee.fr", "jean.durand@lycee.fr", "jdurand@lycee.local"} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("plans sans %q : %s", attendu, corps)
		}
	}
}

func TestExchangeOnpremScript(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	if w := demander(t, s, "POST", "/app/exchange/config", url.Values{
		"mode": {"onprem"}, "smtp_domain": {"lycee.fr"}, "exchange_server": {"exch01.lycee.local"},
	}, entetes); w.Code != http.StatusOK {
		t.Fatalf("config: code = %d", w.Code)
	}

	w := demander(t, s, "POST", "/app/exchange/onprem-script", url.Values{
		"comptes": {"jdurand;Jean Durand"},
	}, entetes)
	if !strings.Contains(w.Body.String(), "New-PSSession -ConfigurationName Microsoft.Exchange") {
		t.Errorf("script sur site absent, body: %s", w.Body.String())
	}
}

func TestExchangePolicyScript(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	if w := demander(t, s, "POST", "/app/exchange/config", url.Values{
		"mode": {"onprem"}, "smtp_domain": {"lycee.fr"}, "exchange_server": {"exch01.lycee.local"},
	}, entetes); w.Code != http.StatusOK {
		t.Fatalf("config: code = %d", w.Code)
	}

	w := demander(t, s, "POST", "/app/exchange/policy-script", url.Values{}, entetes)
	if !strings.Contains(w.Body.String(), "New-EmailAddressPolicy") {
		t.Errorf("politique absente, body: %s", w.Body.String())
	}
}

func TestExchangeSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/exchange", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Microsoft 365 / Entra ID (M19) ──────────────────────────────────────

func TestM365Page(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/m365", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Microsoft 365") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestM365ConfigEnregistrer(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/m365/config", url.Values{
		"tenant_id":      {"11111111-2222-3333-4444-555555555555"},
		"client_id":      {"aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"},
		"auth_mode":      {"secret"},
		"client_secret":  {"super-secret"},
		"usage_location": {"FR"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Configuration enregistrée") {
		t.Errorf("confirmation absente, body: %s", w.Body.String())
	}

	// Le secret ne doit jamais être réaffiché en clair, même dans un commentaire.
	w2 := demander(t, s, "GET", "/app/m365", nil, entetes)
	if strings.Contains(w2.Body.String(), "super-secret") {
		t.Error("le secret est réaffiché dans la page")
	}
}

func TestM365UserPlans(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/m365/user-plans", url.Values{
		"domain":  {"lycee.onmicrosoft.com"},
		"comptes": {"jdurand ; Jean Durand\namartin ; Alice Martin"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	for _, attendu := range []string{"jdurand@lycee.onmicrosoft.com", "amartin@lycee.onmicrosoft.com", "2 plan(s)"} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("plans sans %q : %s", attendu, corps)
		}
	}
}

func TestM365GroupPlans(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/m365/group-plans", url.Values{
		"kind":    {"m365"},
		"groupes": {"6ème A ; a@lycee.local ; b@lycee.local"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	if !strings.Contains(corps, "à créer") || !strings.Contains(corps, "2 membre(s)") {
		t.Errorf("plan de groupe inattendu : %s", corps)
	}
}

func TestM365PlansSansDonnees(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/m365/user-plans", url.Values{"comptes": {""}}, entetes)
	if !strings.Contains(w.Body.String(), "au moins un compte") {
		t.Errorf("message d'erreur absent : %s", w.Body.String())
	}
}

func TestM365SansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/m365", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Coffre-fort local des mots de passe ─────────────────────────────────

func TestVaultPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/vault", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Coffre-fort") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestVaultRechercherEtSupprimer(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	// On remplit le coffre du même dossier que celui utilisé par l'UI.
	v, err := vault.New(filepath.Join(dir, vault.NomFichier), filepath.Join(dir, vault.NomFichierCle))
	if err != nil {
		t.Fatalf("ouverture du coffre: %v", err)
	}
	if err := v.Store("eleve1", "Secret123!"); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	w := demander(t, s, "POST", "/app/vault/rechercher", url.Values{"sam": {"eleve1"}}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("rechercher: code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Secret123!") {
		t.Errorf("mot de passe absent, body: %s", w.Body.String())
	}

	wAbsent := demander(t, s, "POST", "/app/vault/rechercher", url.Values{"sam": {"inconnu"}}, entetes)
	if !strings.Contains(wAbsent.Body.String(), "Aucun mot de passe") {
		t.Errorf("message d'absence manquant : %s", wAbsent.Body.String())
	}

	wSuppr := demander(t, s, "POST", "/app/vault/supprimer", url.Values{"sam": {"eleve1"}}, entetes)
	if !strings.Contains(wSuppr.Body.String(), "Entrée supprimée") {
		t.Errorf("confirmation de suppression manquante : %s", wSuppr.Body.String())
	}
}

func TestVaultPurger(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	v, err := vault.New(filepath.Join(dir, vault.NomFichier), filepath.Join(dir, vault.NomFichierCle))
	if err != nil {
		t.Fatal(err)
	}
	if err := v.Store("a", "x"); err != nil {
		t.Fatal(err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	w := demander(t, s, "POST", "/app/vault/purger", url.Values{}, entetes)
	if !strings.Contains(w.Body.String(), "Coffre purgé") {
		t.Errorf("confirmation de purge manquante : %s", w.Body.String())
	}
}

func TestVaultSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/vault", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Multisite / multi-domaine (M25) ─────────────────────────────────────

func TestMultisitePage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/multisite", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Multisite") {
		t.Errorf("titre absent, body: %s", w.Body.String())
	}
}

func TestMultisiteAjouterEtSupprimer(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/multisite/ajouter", url.Values{
		"label":             {"Lycée Victor Hugo"},
		"domain":            {"lycee-victor-hugo.local"},
		"controller":        {"10.0.0.5"},
		"username":          {"admin"},
		"remember_password": {"1"},
		"password":          {"S3cret!"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("ajouter: code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Site ajouté") {
		t.Errorf("confirmation absente : %s", w.Body.String())
	}

	profils, err := multisite.ChargerSites(
		filepath.Join(dir, multisite.NomFichier), filepath.Join(dir, multisite.NomFichierCle))
	if err != nil {
		t.Fatalf("ChargerSites: %v", err)
	}
	if len(profils) != 1 || profils[0].Password != "S3cret!" {
		t.Fatalf("profils = %+v", profils)
	}

	wSuppr := demander(t, s, "POST", "/app/multisite/supprimer", url.Values{"id": {profils[0].ID}}, entetes)
	if !strings.Contains(wSuppr.Body.String(), "Site supprimé") {
		t.Errorf("confirmation de suppression absente : %s", wSuppr.Body.String())
	}
}

func TestMultisiteProfilInvalide(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/multisite/ajouter", url.Values{"domain": {"a.local"}}, entetes)
	if !strings.Contains(w.Body.String(), "libellé") {
		t.Errorf("message d'erreur absent : %s", w.Body.String())
	}
}

func TestMultisiteSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/multisite", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── RBAC — délégation d'administration (M26) ────────────────────────────

func TestRBACPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/rbac", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	for _, attendu := range []string{"RBAC", "Super-admin", "Helpdesk"} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("page sans %q", attendu)
		}
	}
}

func TestRBACAjouterEtSupprimer(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/rbac/ajouter", url.Values{
		"operateur": {"alice"},
		"role":      {"admin_classe"},
		"site":      {"lycee.local"},
		"ous":       {"OU=3emeA,OU=eleves,DC=lycee,DC=local"},
		"actif":     {"1"},
	}, entetes)
	if w.Code != http.StatusOK {
		t.Fatalf("ajouter: code = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "Délégué ajouté") {
		t.Errorf("confirmation absente : %s", w.Body.String())
	}

	grants, err := rbac.ChargerGrants(filepath.Join(dir, rbac.NomFichier))
	if err != nil {
		t.Fatalf("ChargerGrants: %v", err)
	}
	if len(grants) != 1 || grants[0].Role != rbac.AdminClasse || !grants[0].Actif {
		t.Fatalf("grants = %+v", grants)
	}

	wSuppr := demander(t, s, "POST", "/app/rbac/supprimer", url.Values{"id": {grants[0].ID}}, entetes)
	if !strings.Contains(wSuppr.Body.String(), "Délégué supprimé") {
		t.Errorf("confirmation de suppression absente : %s", wSuppr.Body.String())
	}
	restants, err := rbac.ChargerGrants(filepath.Join(dir, rbac.NomFichier))
	if err != nil {
		t.Fatal(err)
	}
	if len(restants) != 0 {
		t.Errorf("restants = %+v", restants)
	}
}

func TestRBACRoleScopeSansPorteeRefuse(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/rbac/ajouter", url.Values{
		"operateur": {"alice"},
		"role":      {"helpdesk"},
		"actif":     {"1"},
	}, entetes)
	if !strings.Contains(w.Body.String(), "au moins une portée") {
		t.Errorf("message d'erreur absent : %s", w.Body.String())
	}
}

func TestRBACSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/rbac", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}

// ─── Étiquettes avancées (M24) ───────────────────────────────────────────

func TestLabelPage(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "GET", "/app/label", nil, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	corps := w.Body.String()
	for _, attendu := range []string{"badge_porteur", "etiquette_classe", "SMTP"} {
		if !strings.Contains(corps, attendu) {
			t.Errorf("page sans %q", attendu)
		}
	}
}

func TestLabelApercuProduitPDF(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/label/apercu", url.Values{
		"modele": {"etiquette_classe"},
		"nb":     {"6"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK {
		t.Fatalf("code = %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("content-type = %q", ct)
	}
	if !strings.HasPrefix(w.Body.String(), "%PDF-") {
		t.Error("aperçu non PDF")
	}
}

func TestLabelTrombinoscopeProduitPDF(t *testing.T) {
	s, _ := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	w := demander(t, s, "POST", "/app/label/trombinoscope", url.Values{
		"page":     {"A4"},
		"colonnes": {"4"},
		"titre":    {"3emeA"},
		"nb":       {"9"},
	}, map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusOK || !strings.HasPrefix(w.Body.String(), "%PDF-") {
		t.Fatalf("code = %d", w.Code)
	}
}

func TestLabelMailEnregistre(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)
	entetes := map[string]string{"Cookie": cookie.Name + "=" + cookie.Value}

	w := demander(t, s, "POST", "/app/label/mail", url.Values{
		"host":      {"smtp.lycee.fr"},
		"port":      {"587"},
		"from":      {"edusync@lycee.fr"},
		"from_name": {"EduSync AD"},
		"username":  {"edusync"},
		"password":  {"Secret-SMTP-123"},
	}, entetes)
	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303", w.Code)
	}
	brut, err := os.ReadFile(filepath.Join(dir, mailer.NomFichier))
	if err != nil {
		t.Fatalf("fichier SMTP: %v", err)
	}
	if strings.Contains(string(brut), "Secret-SMTP-123") || !strings.Contains(string(brut), "password_token") {
		t.Error("mot de passe non chiffré sur disque")
	}
}

func TestLabelSupprimerModelePersonnalise(t *testing.T) {
	s, dir := nouveauServeur(t)
	cookie := ouvrirSession(t, s)

	store := filepath.Join(dir, label.NomFichier)
	modeles := label.ModelesIntegres()
	perso := modeles[0].Clone()
	perso.ID = "mon_modele"
	perso.Nom = "Mon modèle"
	modeles = append(modeles, perso)
	if _, err := label.SauvegarderModeles(modeles, store); err != nil {
		t.Fatal(err)
	}

	w := demander(t, s, "POST", "/app/label/supprimer", url.Values{"id": {"mon_modele"}},
		map[string]string{"Cookie": cookie.Name + "=" + cookie.Value})
	if w.Code != http.StatusSeeOther {
		t.Fatalf("code = %d, attendu 303", w.Code)
	}
	restants := label.ChargerModeles(store)
	for _, m := range restants {
		if m.ID == "mon_modele" {
			t.Error("modèle personnalisé non supprimé")
		}
	}
}

func TestLabelSansSession(t *testing.T) {
	s, _ := nouveauServeur(t)

	w := demander(t, s, "GET", "/app/label", nil, nil)
	if w.Code != http.StatusSeeOther {
		t.Errorf("code = %d, attendu 303 (redirection login)", w.Code)
	}
}
