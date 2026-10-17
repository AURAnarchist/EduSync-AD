package graph

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const (
	locataire = "11111111-2222-3333-4444-555555555555"
	appClient = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"
)

// reponseFixe décrit une réponse simulée.
type reponseFixe struct {
	status int
	corps  []byte
}

// appelEnregistre capture une requête observée par le faux transport.
type appelEnregistre struct {
	methode string
	url     string
	entetes http.Header
	corps   []byte
}

// fauxDoer est un transport HTTP piloté par une file de réponses.
type fauxDoer struct {
	reponses []any // reponseFixe ou error
	appels   []appelEnregistre
}

func (f *fauxDoer) do(req *http.Request) (*http.Response, error) {
	var corps []byte
	if req.Body != nil {
		corps, _ = io.ReadAll(req.Body)
	}
	f.appels = append(f.appels, appelEnregistre{
		methode: req.Method, url: req.URL.String(), entetes: req.Header.Clone(), corps: corps,
	})
	if len(f.reponses) == 0 {
		return nil, &url.Error{Op: req.Method, URL: req.URL.String(), Err: io.EOF}
	}
	item := f.reponses[0]
	f.reponses = f.reponses[1:]
	if err, ok := item.(error); ok {
		return nil, err
	}
	r := item.(reponseFixe)
	return &http.Response{
		StatusCode: r.status,
		Body:       io.NopCloser(bytes.NewReader(r.corps)),
		Header:     http.Header{},
	}, nil
}

// clientAvec construit un client Graph branché sur un faux transport.
func clientAvec(reponses []any, cfg Config) (*Client, *fauxDoer) {
	doer := &fauxDoer{reponses: reponses}
	return NewClientAvecDoer(cfg, doer.do, nil, 0), doer
}

// jsonBytes encode une valeur JSON (utile pour les corps simulés).
func jsonBytes(t *testing.T, valeur any) []byte {
	t.Helper()
	donnees, err := json.Marshal(valeur)
	if err != nil {
		t.Fatalf("encodage JSON: %v", err)
	}
	return donnees
}

func configTest() Config {
	cfg := ConfigParDefaut()
	cfg.TenantID = locataire
	cfg.ClientID = appClient
	cfg.ClientSecret = "s3cret"
	return cfg
}

var reponseJeton = map[string]any{
	"token_type": "Bearer", "expires_in": 3600, "access_token": "jeton-fake",
}

// ─── Validation ───────────────────────────────────────────────────────────

func TestConfigValide(t *testing.T) {
	if errs := configTest().Validate(); len(errs) != 0 {
		t.Errorf("config valide rejetée : %v", errs)
	}
}

func TestConfigValidate(t *testing.T) {
	cas := []struct {
		nom     string
		cfg     Config
		attendu string
	}{
		{"locataire vide", Config{ClientID: appClient, ClientSecret: "x"}, "locataire"},
		{"locataire non guid", Config{TenantID: "mon-école", ClientID: appClient, ClientSecret: "x"}, "GUID"},
		{"client id vide", Config{TenantID: locataire, ClientSecret: "x"}, "application"},
		{"secret vide", func() Config { c := configTest(); c.ClientSecret = ""; return c }(), "secret"},
		{"certificat vide", func() Config {
			c := configTest()
			c.AuthMode = "certificate"
			c.CertificatePath = ""
			c.ClientSecret = ""
			return c
		}(), "certificat"},
		{"certificat introuvable", func() Config {
			c := configTest()
			c.AuthMode = "certificate"
			c.CertificatePath = "/inexistant.pem"
			c.ClientSecret = ""
			return c
		}(), "introuvable"},
		{"lieu invalide", func() Config { c := configTest(); c.UsageLocation = "Français"; return c }(), "pays"},
	}
	for _, c := range cas {
		if !contientChaine(c.cfg.Validate(), c.attendu) {
			t.Errorf("%s : %q absent de %v", c.nom, c.attendu, c.cfg.Validate())
		}
	}
}

// ─── Persistance ──────────────────────────────────────────────────────────

func TestConfigPersistenceChiffreSecret(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	chemin := filepath.Join(t.TempDir(), FichierConfig)

	cfg := configTest()
	cfg.AuthMode = "certificate"
	cfg.CertificatePath = "/cert.pem"
	if err := SauvegarderConfig(chemin, cfg); err != nil {
		t.Fatalf("SauvegarderConfig: %v", err)
	}

	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(brut, []byte("s3cret")) {
		t.Error("le secret apparaît en clair sur le disque")
	}

	relu := ChargerConfig(chemin)
	if relu.TenantID != locataire || relu.ClientSecret != "s3cret" || relu.AuthMode != "certificate" {
		t.Errorf("round-trip = %+v", relu)
	}
}

func TestConfigAbsentEtCorrompu(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if cfg := ChargerConfig(filepath.Join(t.TempDir(), "absent.json")); cfg.TenantID != "" || cfg.UsageLocation != "FR" {
		t.Errorf("fichier absent : %+v", cfg)
	}
	bad := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(bad, []byte("{oops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg := ChargerConfig(bad); cfg.TenantID != "" || cfg.UsageLocation != "FR" {
		t.Errorf("fichier corrompu : %+v", cfg)
	}
}

// ─── Jeton ────────────────────────────────────────────────────────────────

func TestJetonPayloadEtCache(t *testing.T) {
	client, doer := clientAvec([]any{reponseFixe{200, jsonBytes(t, reponseJeton)}}, configTest())
	token, err := client.GetToken(false)
	if err != nil {
		t.Fatalf("GetToken: %v", err)
	}
	if token != "jeton-fake" {
		t.Errorf("jeton = %q", token)
	}
	if len(doer.appels) != 1 {
		t.Fatalf("appels = %d", len(doer.appels))
	}
	appel := doer.appels[0]
	attendu := "https://login.microsoftonline.com/" + locataire + "/oauth2/v2.0/token"
	if appel.url != attendu {
		t.Errorf("url = %q, attendu %q", appel.url, attendu)
	}
	form, err := url.ParseQuery(string(appel.corps))
	if err != nil {
		t.Fatalf("corps illisible : %v", err)
	}
	if form.Get("grant_type") != "client_credentials" || form.Get("client_secret") != "s3cret" {
		t.Errorf("formulaire = %v", form)
	}
	if form.Get("scope") != GraphScope {
		t.Errorf("scope = %q", form.Get("scope"))
	}

	// Deuxième appel : le jeton est en cache, aucune requête supplémentaire.
	if _, err := client.GetToken(false); err != nil {
		t.Fatal(err)
	}
	if len(doer.appels) != 1 {
		t.Errorf("jeton non mis en cache : %d appels", len(doer.appels))
	}
}

func TestJetonRafraichiApresExpiration(t *testing.T) {
	horloge := []float64{1000}
	doer := &fauxDoer{reponses: []any{
		reponseFixe{200, jsonBytes(t, reponseJeton)},
		reponseFixe{200, jsonBytes(t, reponseJeton)},
	}}
	client := NewClientAvecDoer(configTest(), doer.do, func() float64 { return horloge[0] }, 0)
	if _, err := client.GetToken(false); err != nil {
		t.Fatal(err)
	}
	horloge[0] += 4000
	if _, err := client.GetToken(false); err != nil {
		t.Fatal(err)
	}
	if len(doer.appels) != 2 {
		t.Errorf("appels = %d, attendu 2 (rafraîchissement)", len(doer.appels))
	}
}

func TestJetonErreur(t *testing.T) {
	corps := jsonBytes(t, map[string]any{"error": map[string]any{"code": "invalid_client", "message": "Client inconnu"}})
	client, _ := clientAvec([]any{reponseFixe{401, corps}}, configTest())
	_, err := client.GetToken(false)
	ge, ok := err.(*GraphError)
	if !ok {
		t.Fatalf("erreur = %v", err)
	}
	if ge.Status != 401 || !strings.Contains(ge.Message, "Client inconnu") {
		t.Errorf("erreur = %+v", ge)
	}
}

func TestConfigIncompleteBloqueeAvantRequete(t *testing.T) {
	client, doer := clientAvec(nil, ConfigParDefaut())
	if _, err := client.GetToken(false); err == nil || !strings.Contains(err.Error(), "Configuration incomplète") {
		t.Fatalf("erreur = %v", err)
	}
	if len(doer.appels) != 0 {
		t.Errorf("requête envoyée malgré la config incomplète : %d", len(doer.appels))
	}
}

// ─── Requêtes ─────────────────────────────────────────────────────────────

func clientAutorise(reponses []any) (*Client, *fauxDoer) {
	return clientAvec(append([]any{reponseFixe{200, jsonBytesPad(reponseJeton)}}, reponses...), configTest())
}

func jsonBytesPad(valeur any) []byte {
	donnees, _ := json.Marshal(valeur)
	return donnees
}

func TestEnteteAuthorization(t *testing.T) {
	client, doer := clientAutorise([]any{reponseFixe{200, []byte(`{"value":[]}`)}})
	if _, err := client.Organization(); err != nil {
		t.Fatal(err)
	}
	if doer.appels[1].entetes.Get("Authorization") != "Bearer jeton-fake" {
		t.Errorf("entête = %q", doer.appels[1].entetes.Get("Authorization"))
	}
}

func TestErreurGraphDecodee(t *testing.T) {
	corps := jsonBytesPad(map[string]any{"error": map[string]any{"code": "BadRequest", "message": "Champ invalide"}})
	client, _ := clientAutorise([]any{reponseFixe{400, corps}})
	_, err := client.CreateUser(ParamsCreationUtilisateur{UserPrincipalName: "x@l", DisplayName: "X"})
	ge, ok := err.(*GraphError)
	if !ok {
		t.Fatalf("erreur = %v", err)
	}
	if ge.Status != 400 || ge.Code != "BadRequest" || !strings.Contains(ge.Message, "Champ invalide") {
		t.Errorf("erreur = %+v", ge)
	}
}

func TestMessagesLimiteDebitEtPermissions(t *testing.T) {
	corps429 := jsonBytesPad(map[string]any{"error": map[string]any{"code": "TooManyRequests", "message": "ralentir"}})
	client, _ := clientAutorise([]any{reponseFixe{429, corps429}})
	if _, err := client.ListUsers(); err == nil || !strings.Contains(err.Error(), "Limite de débit") {
		t.Errorf("message 429 = %v", err)
	}

	corps403 := jsonBytesPad(map[string]any{"error": map[string]any{"code": "Authorization_RequestDenied", "message": "refusé"}})
	client, _ = clientAutorise([]any{reponseFixe{403, corps403}})
	if _, err := client.ListUsers(); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Errorf("message 403 = %v", err)
	}
}

func TestErreurReseau(t *testing.T) {
	client, _ := clientAvec([]any{&url.Error{Op: "Post", URL: "x", Err: io.ErrUnexpectedEOF}}, configTest())
	if _, err := client.GetToken(false); err == nil || !strings.Contains(err.Error(), "Connexion impossible") {
		t.Errorf("erreur réseau = %v", err)
	}
}

func TestPaginationSuitNextLink(t *testing.T) {
	page1 := jsonBytesPad(map[string]any{
		"value":           []map[string]any{{"id": "1"}},
		"@odata.nextLink": "https://graph.microsoft.com/v1.0/users?$skiptoken=a",
	})
	page2 := jsonBytesPad(map[string]any{"value": []map[string]any{{"id": "2"}}})
	client, doer := clientAutorise([]any{reponseFixe{200, page1}, reponseFixe{200, page2}})
	users, err := client.ListUsers()
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 2 || users[0]["id"] != "1" || users[1]["id"] != "2" {
		t.Errorf("utilisateurs = %v", users)
	}
	if len(doer.appels) != 3 {
		t.Errorf("appels = %d, attendu 3", len(doer.appels))
	}
}

func TestCorpsVideRetourneDict(t *testing.T) {
	client, _ := clientAutorise([]any{reponseFixe{204, nil}})
	reponse, err := client.Request(http.MethodDelete, "/users/1", OptionsRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if objet, ok := reponse.(map[string]any); !ok || len(objet) != 0 {
		t.Errorf("réponse = %#v", reponse)
	}
}

// ─── Points de terminaison ────────────────────────────────────────────────

func TestCreateUserPayload(t *testing.T) {
	client, doer := clientAutorise([]any{reponseFixe{201, []byte(`{"id":"u-1"}`)}})
	if _, err := client.CreateUser(ParamsCreationUtilisateur{
		UserPrincipalName: "jdurand@lycee.local",
		DisplayName:       "Jean Durand",
		MailNickname:      "jdurand",
		Password:          "Passw0rd!123",
		GivenName:         "Jean",
		Surname:           "Durand",
		AccountEnabled:    true,
		UsageLocation:     "FR",
	}); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doer.appels[1].corps, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["accountEnabled"] != true || payload["usageLocation"] != "FR" {
		t.Errorf("payload = %v", payload)
	}
	if payload["userPrincipalName"] != "jdurand@lycee.local" {
		t.Errorf("upn = %v", payload["userPrincipalName"])
	}
	profil, _ := payload["passwordProfile"].(map[string]any)
	if profil["password"] != "Passw0rd!123" || profil["forceChangePasswordNextSignIn"] != false {
		t.Errorf("passwordProfile = %v", profil)
	}
}

func TestSubscribedSkusMapping(t *testing.T) {
	corps := []byte(`{"value":[{"skuPartNumber":"M365EDU_A1","skuId":"sku-1"},{"skuPartNumber":"EXCHANGESTANDARD","skuId":"sku-2"}]}`)
	client, _ := clientAutorise([]any{reponseFixe{200, corps}})
	skus, err := client.SubscribedSkus()
	if err != nil {
		t.Fatal(err)
	}
	if skus["M365EDU_A1"] != "sku-1" || skus["EXCHANGESTANDARD"] != "sku-2" {
		t.Errorf("skus = %v", skus)
	}
}

func TestAssignLicensePayload(t *testing.T) {
	client, doer := clientAutorise([]any{reponseFixe{200, []byte(`{"id":"u-1"}`)}})
	if _, err := client.AssignLicense("u-1", "sku-1"); err != nil {
		t.Fatal(err)
	}
	var payload map[string]any
	if err := json.Unmarshal(doer.appels[1].corps, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["removeLicenses"] == nil {
		t.Errorf("removeLicenses absent : %v", payload)
	}
	ajouts, _ := payload["addLicenses"].([]any)
	if len(ajouts) != 1 {
		t.Fatalf("addLicenses = %v", payload["addLicenses"])
	}
	premier, _ := ajouts[0].(map[string]any)
	if premier["skuId"] != "sku-1" {
		t.Errorf("skuId = %v", premier["skuId"])
	}
}

func TestCreateGroupKinds(t *testing.T) {
	client, doer := clientAutorise([]any{
		reponseFixe{201, []byte(`{"id":"g-1"}`)},
		reponseFixe{201, []byte(`{"id":"g-2"}`)},
	})
	if _, err := client.CreateGroup(ParamsCreationGroupe{DisplayName: "6eme A", MailNickname: "6emea", Kind: "m365"}); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CreateGroup(ParamsCreationGroupe{DisplayName: "Profs", MailNickname: "profs", Kind: "security"}); err != nil {
		t.Fatal(err)
	}
	var premier, second map[string]any
	_ = json.Unmarshal(doer.appels[1].corps, &premier)
	_ = json.Unmarshal(doer.appels[2].corps, &second)
	types, _ := premier["groupTypes"].([]any)
	if len(types) != 1 || types[0] != "Unified" {
		t.Errorf("groupTypes m365 = %v", premier["groupTypes"])
	}
	if premier["mailEnabled"] != true || premier["securityEnabled"] != false {
		t.Errorf("m365 = %v", premier)
	}
	types2, _ := second["groupTypes"].([]any)
	if len(types2) != 0 {
		t.Errorf("groupTypes security = %v", second["groupTypes"])
	}
	if second["mailEnabled"] != false || second["securityEnabled"] != true {
		t.Errorf("security = %v", second)
	}
}

func TestAddGroupMemberOdataRef(t *testing.T) {
	client, doer := clientAutorise([]any{reponseFixe{204, nil}})
	if err := client.AddGroupMember("g-1", "u-1"); err != nil {
		t.Fatal(err)
	}
	appel := doer.appels[1]
	if !strings.HasSuffix(appel.url, "/groups/g-1/members/$ref") {
		t.Errorf("url = %q", appel.url)
	}
	var payload map[string]any
	if err := json.Unmarshal(appel.corps, &payload); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(Chaine(payload["@odata.id"]), "/users/u-1") {
		t.Errorf("odata.id = %v", payload["@odata.id"])
	}
}

func TestUploadPhotoRawJPEG(t *testing.T) {
	client, doer := clientAutorise([]any{reponseFixe{204, nil}})
	if err := client.UploadUserPhoto("u-1", []byte("\xff\xd8jpeg")); err != nil {
		t.Fatal(err)
	}
	appel := doer.appels[1]
	if !strings.HasSuffix(appel.url, "/users/u-1/photo/$content") {
		t.Errorf("url = %q", appel.url)
	}
	if appel.entetes.Get("Content-Type") != "image/jpeg" {
		t.Errorf("content-type = %q", appel.entetes.Get("Content-Type"))
	}
	if !bytes.Equal(appel.corps, []byte("\xff\xd8jpeg")) {
		t.Errorf("corps = %v", appel.corps)
	}
}

// ─── Authentification par certificat ──────────────────────────────────────

// pemAutosigne génère une clé RSA + certificat autosigné PEM (PKCS#8 + CERT).
func pemAutosigne(t *testing.T) string {
	t.Helper()
	cle, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("génération de clé: %v", err)
	}
	modele := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "EduSync-AD"},
		NotBefore:    time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		NotAfter:     time.Date(2034, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	der, err := x509.CreateCertificate(rand.Reader, modele, modele, &cle.PublicKey, cle)
	if err != nil {
		t.Fatalf("création du certificat: %v", err)
	}
	clePKCS8, err := x509.MarshalPKCS8PrivateKey(cle)
	if err != nil {
		t.Fatalf("encodage de clé: %v", err)
	}
	contenu := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: clePKCS8})
	contenu = append(contenu, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)

	chemin := filepath.Join(t.TempDir(), "app.pem")
	if err := os.WriteFile(chemin, contenu, 0o600); err != nil {
		t.Fatal(err)
	}
	return chemin
}

func TestClientAssertionJWT(t *testing.T) {
	chemin := pemAutosigne(t)
	cfg := configTest()
	cfg.AuthMode = "certificate"
	cfg.CertificatePath = chemin
	cfg.ClientSecret = ""

	client, doer := clientAvec([]any{reponseFixe{200, jsonBytes(t, reponseJeton)}}, cfg)
	if token, err := client.GetToken(false); err != nil || token != "jeton-fake" {
		t.Fatalf("GetToken = %q, %v", token, err)
	}
	form, err := url.ParseQuery(string(doer.appels[0].corps))
	if err != nil {
		t.Fatal(err)
	}
	if form.Get("client_assertion_type") != ClientAssertionType {
		t.Errorf("client_assertion_type = %q", form.Get("client_assertion_type"))
	}
	assertion := form.Get("client_assertion")
	if parts := strings.Split(assertion, "."); len(parts) != 3 {
		t.Errorf("assertion JWT = %q", assertion)
	}
	if strings.Contains(form.Encode(), "client_secret") {
		t.Error("client_secret présent en mode certificat")
	}
}

func TestPEMIllisible(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(bad, []byte("pas un PEM"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := configTest()
	cfg.AuthMode = "certificate"
	cfg.CertificatePath = bad
	cfg.ClientSecret = ""
	client, _ := clientAvec([]any{reponseFixe{200, jsonBytes(t, reponseJeton)}}, cfg)
	if _, err := client.GetToken(false); err == nil || !strings.Contains(err.Error(), "PEM") {
		t.Errorf("erreur = %v", err)
	}
}

// contientChaine indique si une chaîne attendue apparaît dans un slice.
func contientChaine(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if strings.Contains(v, cible) {
			return true
		}
	}
	return false
}
