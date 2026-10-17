// Package graph fournit un client Microsoft Graph / Microsoft Entra ID (M19).
//
// Connexion via « app registration » (identifiant client) en OAuth2
// client_credentials, avec deux modes d'authentification :
//
//   - « secret » — client_secret classique ;
//   - « certificate » — client_assertion JWT signé RS256 (clé + certificat PEM).
//
// Le secret est chiffré sur disque (AES-GCM, clé locale via crypto), jamais
// stocké en clair. Toutes les requêtes passent par Client.Request qui
// centralise le jeton d'accès (mis en cache), la pagination @odata.nextLink et
// la traduction des erreurs Graph en GraphError (dont les limites de débit 429).
//
// Le format du fichier m365.json et la logique reprennent
// src/edusync_ad/core/graph.py pour rester compatibles avec Python.
package graph

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
	edcrypto "github.com/estemobs/EduSync-AD/internal/crypto"
)

const (
	// GraphBaseURL est la racine de l'API Microsoft Graph v1.0.
	GraphBaseURL = "https://graph.microsoft.com/v1.0"
	// LoginBaseURL est la racine du point de connexion OAuth2.
	LoginBaseURL = "https://login.microsoftonline.com"
	// GraphScope est la portée demandée (permissions d'application).
	GraphScope = "https://graph.microsoft.com/.default"
	// ClientAssertionType est le type d'assertion JWT pour l'authentification par certificat.
	ClientAssertionType = "urn:ietf:params:oauth:client-assertion-type:jwt-bearer"

	// FichierConfig est le nom du fichier de configuration Microsoft 365.
	FichierConfig = "m365.json"
	// FichierCle est le nom du fichier de clé locale (chiffrement du secret).
	FichierCle = "secret.key"

	// DefaultTimeout est le délai par requête HTTP (secondes).
	DefaultTimeout = 25
	// TokenMargin est la marge (secondes) avant expiration pour rafraîchir le jeton.
	TokenMargin = 60
)

// UserFields est la liste des champs utilisateur demandés à Graph.
const UserFields = "id,userPrincipalName,displayName,givenName,surname,mail,accountEnabled,usageLocation"

// GroupFields est la liste des champs de groupe demandés à Graph.
const GroupFields = "id,displayName,mailNickname,mail,groupTypes,securityEnabled"

// GraphError est une erreur renvoyée par Microsoft Graph ou par le point de
// connexion OAuth2.
type GraphError struct {
	Message string
	Status  int
	Code    string
}

// Error implémente error.
func (e *GraphError) Error() string { return e.Message }

// Config décrit les paramètres de connexion à Microsoft Entra ID.
type Config struct {
	TenantID        string `json:"tenant_id"`
	ClientID        string `json:"client_id"`
	ClientSecret    string `json:"-"`
	AuthMode        string `json:"auth_mode"` // "secret" | "certificate"
	CertificatePath string `json:"certificate_path"`
	UsageLocation   string `json:"usage_location"`
}

// ConfigParDefaut retourne une configuration avec les valeurs par défaut.
func ConfigParDefaut() Config {
	return Config{AuthMode: "secret", UsageLocation: "FR"}
}

// TokenURL construit l'URL du point de connexion OAuth2 du locataire.
func (c Config) TokenURL() string {
	return fmt.Sprintf("%s/%s/oauth2/v2.0/token", LoginBaseURL, c.TenantID)
}

// Validate vérifie la cohérence de la configuration.
func (c Config) Validate() []string {
	var erreurs []string
	locataire := strings.TrimSpace(c.TenantID)
	if locataire == "" {
		erreurs = append(erreurs, "L'identifiant de locataire (tenant ID) est vide.")
	} else if len(locataire) != 36 {
		erreurs = append(erreurs, "Le tenant ID doit être un GUID (ex : 00000000-0000-0000-0000-000000000000).")
	}
	if strings.TrimSpace(c.ClientID) == "" {
		erreurs = append(erreurs, "L'identifiant d'application (client ID) est vide.")
	}
	if c.AuthMode == "certificate" {
		if strings.TrimSpace(c.CertificatePath) == "" {
			erreurs = append(erreurs, "Le chemin du fichier de certificat (PEM) est vide.")
		} else if _, err := os.Stat(expandHome(c.CertificatePath)); err != nil {
			erreurs = append(erreurs, fmt.Sprintf("Fichier introuvable : %s", c.CertificatePath))
		}
	} else if strings.TrimSpace(c.ClientSecret) == "" {
		erreurs = append(erreurs, "Le secret client est vide.")
	}
	if len(strings.TrimSpace(c.UsageLocation)) != 2 {
		erreurs = append(erreurs, "La localisation d'usage doit être un code pays ISO 2 (ex : FR).")
	}
	return erreurs
}

// expandHome développe un chemin commençant par « ~ ».
func expandHome(chemin string) string {
	if chemin == "~" || strings.HasPrefix(chemin, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(chemin, "~"))
		}
	}
	return chemin
}

// ToDict retourne la représentation JSON de la configuration. Le secret n'est
// inclus (chiffré) que si includeSecret est vrai.
func (c Config) ToDict(includeSecret bool) map[string]any {
	donnees := map[string]any{
		"tenant_id":        c.TenantID,
		"client_id":        c.ClientID,
		"auth_mode":        c.AuthMode,
		"certificate_path": c.CertificatePath,
		"usage_location":   c.UsageLocation,
	}
	if includeSecret {
		donnees["client_secret_enc"] = scellerSecret(c.ClientSecret)
	}
	return donnees
}

// ConfigDepuisDict construit une configuration depuis une représentation JSON.
func ConfigDepuisDict(donnees map[string]any) Config {
	cfg := ConfigParDefaut()
	cfg.TenantID = Chaine(donnees["tenant_id"])
	cfg.ClientID = Chaine(donnees["client_id"])
	cfg.ClientSecret = descellerSecret(Chaine(donnees["client_secret_enc"]))
	if mode := Chaine(donnees["auth_mode"]); mode != "" {
		cfg.AuthMode = mode
	}
	cfg.CertificatePath = Chaine(donnees["certificate_path"])
	if lieu := Chaine(donnees["usage_location"]); lieu != "" {
		cfg.UsageLocation = lieu
	}
	return cfg
}

// Chaine retourne la valeur comme chaîne (vide si absente).
func Chaine(valeur any) string {
	if valeur == nil {
		return ""
	}
	if s, ok := valeur.(string); ok {
		return s
	}
	return fmt.Sprint(valeur)
}

// ─── Chiffrement du secret client ─────────────────────────────────────────

// cheminCle retourne le chemin du fichier secret.key partagé avec Python.
func cheminCle() string {
	if dir, err := config.ConfigDir(); err == nil {
		return filepath.Join(dir, FichierCle)
	}
	return FichierCle
}

// scellerSecret chiffre le secret (chaîne vide si la clé est indisponible —
// jamais bloquant, cohérent avec Python).
func scellerSecret(secret string) string {
	if secret == "" {
		return ""
	}
	cle, err := edcrypto.LoadOrCreateKey(cheminCle())
	if err != nil {
		return ""
	}
	jeton, err := edcrypto.Encrypt(cle, secret)
	if err != nil {
		return ""
	}
	return jeton
}

// descellerSecret déchiffre le secret (chaîne vide si la clé a changé).
func descellerSecret(jeton string) string {
	if jeton == "" {
		return ""
	}
	cle, err := edcrypto.LoadOrCreateKey(cheminCle())
	if err != nil {
		return ""
	}
	secret, err := edcrypto.Decrypt(cle, jeton)
	if err != nil {
		return ""
	}
	return secret
}

// DossierConfig retourne le dossier de configuration partagé avec Python.
func DossierConfig() (string, error) {
	return config.ConfigDir()
}

// ChargerConfig lit la configuration M365 (défaut si absente ou illisible).
func ChargerConfig(chemin string) Config {
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return ConfigParDefaut()
		}
		chemin = filepath.Join(dir, FichierConfig)
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return ConfigParDefaut()
	}
	var brut map[string]any
	if err := json.Unmarshal(donnees, &brut); err != nil {
		return ConfigParDefaut()
	}
	return ConfigDepuisDict(brut)
}

// SauvegarderConfig écrit la configuration M365 (secret chiffré, JSON indenté).
func SauvegarderConfig(chemin string, cfg Config) error {
	if chemin == "" {
		dir, err := DossierConfig()
		if err != nil {
			return err
		}
		chemin = filepath.Join(dir, FichierConfig)
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o755); err != nil {
		return fmt.Errorf("graph: création du dossier de configuration: %w", err)
	}
	f, err := os.Create(chemin)
	if err != nil {
		return fmt.Errorf("graph: écriture %s: %w", chemin, err)
	}
	defer func() { _ = f.Close() }()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(cfg.ToDict(true)); err != nil {
		return fmt.Errorf("graph: encodage JSON: %w", err)
	}
	return nil
}

// ─── Client ───────────────────────────────────────────────────────────────

// Doer exécute une requête HTTP (injectable pour les tests).
type Doer func(req *http.Request) (*http.Response, error)

// Client accède à Microsoft Graph avec un jeton mis en cache.
type Client struct {
	Config  Config
	doer    Doer
	timeout time.Duration
	now     func() float64

	mu             sync.Mutex
	token          string
	tokenExpiresAt float64
}

// NewClient construit un client Graph utilisant le transport HTTP par défaut.
func NewClient(cfg Config) *Client {
	client := &http.Client{Timeout: DefaultTimeout * time.Second}
	return &Client{
		Config:  cfg,
		doer:    client.Do,
		timeout: DefaultTimeout * time.Second,
		now:     func() float64 { return float64(time.Now().UnixNano()) / 1e9 },
	}
}

// NewClientAvecDoer construit un client Graph avec un transport injecté
// (utilisé par les tests).
func NewClientAvecDoer(cfg Config, doer Doer, now func() float64, timeout time.Duration) *Client {
	if now == nil {
		now = func() float64 { return float64(time.Now().UnixNano()) / 1e9 }
	}
	if timeout <= 0 {
		timeout = DefaultTimeout * time.Second
	}
	return &Client{Config: cfg, doer: doer, timeout: timeout, now: now}
}

// b64url encode en base64url sans remplissage.
func b64url(donnees []byte) string {
	return base64.RawURLEncoding.EncodeToString(donnees)
}

// errorFromPayload traduit une erreur Graph/OAuth2 en GraphError.
func errorFromPayload(payload []byte, status int) *GraphError {
	message, code := "", ""
	var donnees map[string]any
	if err := json.Unmarshal(payload, &donnees); err == nil {
		var errPart map[string]any
		if brut, ok := donnees["error"].(map[string]any); ok {
			errPart = brut
		} else {
			errPart = donnees
		}
		message = Chaine(errPart["message"])
		code = Chaine(errPart["code"])
	}
	if message == "" && len(payload) > 0 {
		message = string(tronquer(payload, 300))
	}
	switch status {
	case 429:
		message = "Limite de débit Graph atteinte (429) — réessayez plus tard. " + message
	case 401, 403:
		message = "Accès refusé — vérifiez les permissions (Application) de l'app " +
			"registration et les rôles attribués. " + message
	}
	if strings.TrimSpace(message) == "" {
		message = fmt.Sprintf("Erreur Graph (HTTP %d).", status)
	}
	return &GraphError{Message: strings.TrimSpace(message), Status: status, Code: code}
}

// tronquer retourne les n premiers octets.
func tronquer(donnees []byte, n int) []byte {
	if len(donnees) <= n {
		return donnees
	}
	return donnees[:n]
}

// assertClient construit le JWT client_assertion (RS256).
func (c *Client) assertClient() (string, error) {
	raw, err := os.ReadFile(expandHome(c.Config.CertificatePath))
	if err != nil {
		return "", &GraphError{Message: fmt.Sprintf("Certificat/clé PEM illisible : %v", err)}
	}
	signer, err := chargerClePrivee(raw)
	if err != nil {
		return "", &GraphError{Message: fmt.Sprintf("Certificat/clé PEM illisible : %v", err)}
	}

	entete := map[string]any{"alg": "RS256", "typ": "JWT"}
	if cert, err := chargerCertificat(raw); err == nil {
		empreinte := sha1.Sum(cert.Raw)
		entete["x5t"] = b64url(empreinte[:])
	}

	maintenant := int64(c.now())
	charge := map[string]any{
		"aud": c.Config.TokenURL(),
		"iss": c.Config.ClientID,
		"sub": c.Config.ClientID,
		"jti": jetonAleatoire(),
		"iat": maintenant,
		"nbf": maintenant,
		"exp": maintenant + 600,
	}
	enteteJSON, err := json.Marshal(entete)
	if err != nil {
		return "", &GraphError{Message: fmt.Sprintf("Échec de signature du jeton : %v", err)}
	}
	chargeJSON, err := json.Marshal(charge)
	if err != nil {
		return "", &GraphError{Message: fmt.Sprintf("Échec de signature du jeton : %v", err)}
	}
	signInput := b64url(enteteJSON) + "." + b64url(chargeJSON)
	somme := sha256.Sum256([]byte(signInput))
	signature, err := signer.Sign(rand.Reader, somme[:], crypto.SHA256)
	if err != nil {
		return "", &GraphError{Message: fmt.Sprintf("Échec de signature du jeton : %v", err)}
	}
	return signInput + "." + b64url(signature), nil
}

// chargerClePrivee décode une clé privée PEM (PKCS#8, PKCS#1 ou EC).
func chargerClePrivee(raw []byte) (crypto.Signer, error) {
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, errors.New("bloc PEM introuvable")
	}
	if cle, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if signer, ok := cle.(crypto.Signer); ok {
			return signer, nil
		}
	}
	if cle, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return cle, nil
	}
	if cle, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
		return cle, nil
	}
	return nil, errors.New("clé privée PEM illisible")
}

// chargerCertificat décode le premier certificat PEM du fichier.
func chargerCertificat(raw []byte) (*x509.Certificate, error) {
	for bloque, reste := pem.Decode(raw); bloque != nil; bloque, reste = pem.Decode(reste) {
		if bloque.Type != "CERTIFICATE" {
			continue
		}
		return x509.ParseCertificate(bloque.Bytes)
	}
	return nil, errors.New("certificat PEM introuvable")
}

// jetonAleatoire retourne un identifiant de jeton unique (hexadécimal).
func jetonAleatoire() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return fmt.Sprintf("%x", buf)
}

// tokenData construit le corps de la requête de jeton.
func (c *Client) tokenData() (url.Values, error) {
	donnees := url.Values{
		"grant_type": {"client_credentials"},
		"client_id":  {c.Config.ClientID},
		"scope":      {GraphScope},
	}
	if c.Config.AuthMode == "certificate" {
		assertion, err := c.assertClient()
		if err != nil {
			return nil, err
		}
		donnees.Set("client_assertion_type", ClientAssertionType)
		donnees.Set("client_assertion", assertion)
	} else {
		donnees.Set("client_secret", c.Config.ClientSecret)
	}
	return donnees, nil
}

// GetToken retourne un jeton d'accès (mis en cache, rafraîchi si expiré).
func (c *Client) GetToken(force bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !force && c.token != "" && c.now() < c.tokenExpiresAt-TokenMargin {
		return c.token, nil
	}

	if erreurs := c.Config.Validate(); len(erreurs) > 0 {
		return "", &GraphError{Message: "Configuration incomplète :\n- " + strings.Join(erreurs, "\n- ")}
	}

	donnees, err := c.tokenData()
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, c.Config.TokenURL(), strings.NewReader(donnees.Encode()))
	if err != nil {
		return "", &GraphError{Message: fmt.Sprintf("Requête d'authentification invalide : %v", err)}
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	status, payload, err := c.call(req)
	if err != nil {
		return "", err
	}
	if status >= 400 {
		return "", errorFromPayload(payload, status)
	}
	var reponse map[string]any
	if err := json.Unmarshal(payload, &reponse); err != nil {
		return "", &GraphError{Message: "Réponse d'authentification illisible."}
	}
	token := Chaine(reponse["access_token"])
	if token == "" {
		return "", &GraphError{Message: "Aucun jeton d'accès retourné par Microsoft."}
	}
	expiresIn := float64(3600)
	if v, ok := reponse["expires_in"].(float64); ok {
		expiresIn = v
	}
	c.token = token
	c.tokenExpiresAt = c.now() + expiresIn
	return c.token, nil
}

// call exécute une requête HTTP et retourne (statut, corps).
func (c *Client) call(req *http.Request) (int, []byte, error) {
	resp, err := c.doer(req)
	if err != nil {
		if ue, ok := err.(*url.Error); ok {
			return 0, nil, &GraphError{Message: fmt.Sprintf("Connexion impossible : %v", ue.Err)}
		}
		return 0, nil, &GraphError{Message: fmt.Sprintf("Connexion impossible : %v", err)}
	}
	defer func() { _ = resp.Body.Close() }()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, nil, &GraphError{Message: fmt.Sprintf("Erreur réseau : %v", err)}
	}
	status := resp.StatusCode
	if status == 0 {
		status = 200
	}
	return status, payload, nil
}

// OptionsRequest décrit les options d'une requête Graph.
type OptionsRequest struct {
	Data        any
	Raw         []byte
	ContentType string
	Absolute    bool
}

// Request exécute une requête authentifiée vers Graph et retourne le JSON décodé.
func (c *Client) Request(method, path string, opts OptionsRequest) (any, error) {
	urlCible := path
	if !opts.Absolute {
		urlCible = GraphBaseURL + path
	}

	var corps []byte
	if opts.Raw != nil {
		corps = opts.Raw
	} else if opts.Data != nil {
		encode, err := json.Marshal(opts.Data)
		if err != nil {
			return nil, &GraphError{Message: fmt.Sprintf("Encodage JSON impossible : %v", err)}
		}
		corps = encode
	}

	token, err := c.GetToken(false)
	if err != nil {
		return nil, err
	}

	var lecteur io.Reader
	if corps != nil {
		lecteur = bytes.NewReader(corps)
	}
	req, err := http.NewRequest(strings.ToUpper(method), urlCible, lecteur)
	if err != nil {
		return nil, &GraphError{Message: fmt.Sprintf("Requête invalide : %v", err)}
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	if corps != nil {
		typeContenu := opts.ContentType
		if typeContenu == "" {
			typeContenu = "application/json"
		}
		req.Header.Set("Content-Type", typeContenu)
	}

	status, payload, err := c.call(req)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		return nil, errorFromPayload(payload, status)
	}
	if len(payload) == 0 {
		return map[string]any{}, nil
	}
	var reponse any
	if err := json.Unmarshal(payload, &reponse); err != nil {
		return nil, &GraphError{Message: fmt.Sprintf("Réponse JSON illisible (HTTP %d).", status)}
	}
	return reponse, nil
}

// getAll suit la pagination @odata.nextLink et retourne tous les éléments.
func (c *Client) getAll(path string) ([]map[string]any, error) {
	resultats := []map[string]any{}
	urlCourante := GraphBaseURL + path
	for urlCourante != "" {
		page, err := c.Request(http.MethodGet, urlCourante, OptionsRequest{Absolute: true})
		if err != nil {
			return nil, err
		}
		objet, ok := page.(map[string]any)
		if !ok {
			break
		}
		if valeurs, ok := objet["value"].([]any); ok {
			for _, v := range valeurs {
				if entree, ok := v.(map[string]any); ok {
					resultats = append(resultats, entree)
				}
			}
		}
		urlCourante = Chaine(objet["@odata.nextLink"])
	}
	return resultats, nil
}

// mapObjet convertit une réponse JSON en map (vide si autre type).
func mapObjet(reponse any) map[string]any {
	if objet, ok := reponse.(map[string]any); ok {
		return objet
	}
	return map[string]any{}
}

// ─── Organisation / utilisateurs ──────────────────────────────────────────

// Organization retourne les informations du locataire.
func (c *Client) Organization() (map[string]any, error) {
	reponse, err := c.Request(http.MethodGet, "/organization", OptionsRequest{})
	if err != nil {
		return nil, err
	}
	objet := mapObjet(reponse)
	if valeurs, ok := objet["value"].([]any); ok && len(valeurs) > 0 {
		if premier, ok := valeurs[0].(map[string]any); ok {
			return premier, nil
		}
	}
	return map[string]any{}, nil
}

// ListUsers retourne les comptes du locataire (toutes pages).
func (c *Client) ListUsers() ([]map[string]any, error) {
	return c.getAll(fmt.Sprintf("/users?$select=%s&$top=999", UserFields))
}

// GetUser retourne un compte par identifiant ou UPN.
func (c *Client) GetUser(idOuUPN string) (map[string]any, error) {
	reponse, err := c.Request(http.MethodGet, fmt.Sprintf("/users/%s?$select=%s", idOuUPN, UserFields), OptionsRequest{})
	if err != nil {
		return nil, err
	}
	return mapObjet(reponse), nil
}

// ParamsCreationUtilisateur décrit les paramètres de création d'un compte cloud.
type ParamsCreationUtilisateur struct {
	UserPrincipalName   string
	DisplayName         string
	MailNickname        string
	Password            string
	GivenName           string
	Surname             string
	AccountEnabled      bool
	UsageLocation       string
	ForceChangePassword bool
}

// CreateUser crée un compte dans Entra ID.
func (c *Client) CreateUser(params ParamsCreationUtilisateur) (map[string]any, error) {
	lieu := params.UsageLocation
	if lieu == "" {
		lieu = "FR"
	}
	payload := map[string]any{
		"accountEnabled":    params.AccountEnabled,
		"displayName":       params.DisplayName,
		"mailNickname":      params.MailNickname,
		"userPrincipalName": params.UserPrincipalName,
		"usageLocation":     lieu,
		"passwordProfile": map[string]any{
			"password":                      params.Password,
			"forceChangePasswordNextSignIn": params.ForceChangePassword,
		},
	}
	if params.GivenName != "" {
		payload["givenName"] = params.GivenName
	}
	if params.Surname != "" {
		payload["surname"] = params.Surname
	}
	reponse, err := c.Request(http.MethodPost, "/users", OptionsRequest{Data: payload})
	if err != nil {
		return nil, err
	}
	return mapObjet(reponse), nil
}

// DeleteUser supprime un compte.
func (c *Client) DeleteUser(id string) error {
	_, err := c.Request(http.MethodDelete, "/users/"+id, OptionsRequest{})
	return err
}

// ─── Groupes ──────────────────────────────────────────────────────────────

// ListGroups retourne les groupes du locataire (toutes pages).
func (c *Client) ListGroups() ([]map[string]any, error) {
	return c.getAll(fmt.Sprintf("/groups?$select=%s&$top=999", GroupFields))
}

// ParamsCreationGroupe décrit les paramètres de création d'un groupe.
type ParamsCreationGroupe struct {
	DisplayName  string
	MailNickname string
	Description  string
	Kind         string // "m365" | "distribution" | "security"
}

// CreateGroup crée un groupe Entra.
func (c *Client) CreateGroup(params ParamsCreationGroupe) (map[string]any, error) {
	typesGroupe := []string{}
	if params.Kind == "m365" {
		typesGroupe = []string{"Unified"}
	}
	payload := map[string]any{
		"displayName":     params.DisplayName,
		"mailNickname":    params.MailNickname,
		"mailEnabled":     params.Kind != "security",
		"securityEnabled": params.Kind == "security",
		"groupTypes":      typesGroupe,
	}
	if params.Description != "" {
		payload["description"] = params.Description
	}
	reponse, err := c.Request(http.MethodPost, "/groups", OptionsRequest{Data: payload})
	if err != nil {
		return nil, err
	}
	return mapObjet(reponse), nil
}

// ListGroupMembers retourne les membres d'un groupe.
func (c *Client) ListGroupMembers(groupID string) ([]map[string]any, error) {
	return c.getAll(fmt.Sprintf("/groups/%s/members?$select=id,userPrincipalName,displayName&$top=999", groupID))
}

// AddGroupMember ajoute un membre à un groupe.
func (c *Client) AddGroupMember(groupID, memberID string) error {
	payload := map[string]any{"@odata.id": fmt.Sprintf("%s/users/%s", GraphBaseURL, memberID)}
	_, err := c.Request(http.MethodPost, fmt.Sprintf("/groups/%s/members/$ref", groupID), OptionsRequest{Data: payload})
	return err
}

// RemoveGroupMember retire un membre d'un groupe.
func (c *Client) RemoveGroupMember(groupID, memberID string) error {
	_, err := c.Request(http.MethodDelete, fmt.Sprintf("/groups/%s/members/%s/$ref", groupID, memberID), OptionsRequest{})
	return err
}

// CreateTeam crée l'équipe Teams rattachée à un groupe Microsoft 365.
func (c *Client) CreateTeam(groupID string) error {
	payload := map[string]any{"memberSettings": map[string]any{"allowCreateUpdateChannels": true}}
	_, err := c.Request(http.MethodPut, fmt.Sprintf("/groups/%s/team", groupID), OptionsRequest{Data: payload})
	return err
}

// ─── Licences ─────────────────────────────────────────────────────────────

// SubscribedSkus retourne la correspondance skuPartNumber → skuId.
func (c *Client) SubscribedSkus() (map[string]string, error) {
	resultat := map[string]string{}
	skus, err := c.getAll("/subscribedSkus")
	if err != nil {
		return nil, err
	}
	for _, sku := range skus {
		part := Chaine(sku["skuPartNumber"])
		skuID := Chaine(sku["skuId"])
		if part != "" && skuID != "" {
			resultat[part] = skuID
		}
	}
	return resultat, nil
}

// AssignLicense attribue une licence à un compte.
func (c *Client) AssignLicense(userID, skuID string) (map[string]any, error) {
	payload := map[string]any{
		"addLicenses":    []map[string]any{{"skuId": skuID}},
		"removeLicenses": []string{},
	}
	reponse, err := c.Request(http.MethodPost, fmt.Sprintf("/users/%s/assignLicense", userID), OptionsRequest{Data: payload})
	if err != nil {
		return nil, err
	}
	return mapObjet(reponse), nil
}

// RemoveLicense retire une licence à un compte.
func (c *Client) RemoveLicense(userID, skuID string) (map[string]any, error) {
	payload := map[string]any{
		"addLicenses":    []map[string]any{},
		"removeLicenses": []string{skuID},
	}
	reponse, err := c.Request(http.MethodPost, fmt.Sprintf("/users/%s/assignLicense", userID), OptionsRequest{Data: payload})
	if err != nil {
		return nil, err
	}
	return mapObjet(reponse), nil
}

// ─── Photos ───────────────────────────────────────────────────────────────

// UploadUserPhoto dépose la photo de profil (JPEG, ≤ 4 Mo).
func (c *Client) UploadUserPhoto(userID string, jpeg []byte) error {
	_, err := c.Request(http.MethodPut, fmt.Sprintf("/users/%s/photo/$content", userID), OptionsRequest{
		Raw:         jpeg,
		ContentType: "image/jpeg",
	})
	return err
}
