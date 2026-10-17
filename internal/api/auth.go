package api

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
)

// NomFichierCles est le fichier des clés d'API (dossier de configuration).
const NomFichierCles = "api_keys.json"

// PrefixeCle est le préfixe commun des clés d'API.
const PrefixeCle = "edusync_"

// formatISO reproduit `datetime.isoformat(timespec="seconds")` côté Python.
const formatISO = "2006-01-02T15:04:05-07:00"

// PermissionLookup associe chaque permission à son libellé humain. L'ordre de
// la tranche est celui de `STANDARD_PERMISSIONS` côté Python.
type PermissionLookup struct {
	Code    string
	Libelle string
}

// PermissionsStandards liste les permissions reconnues.
var PermissionsStandards = []PermissionLookup{
	{"*", "Accès complet (admin API)"},
	{"users:read", "Lister/voir les utilisateurs"},
	{"users:write", "Créer/modifier/supprimer des utilisateurs"},
	{"users:password", "Réinitialiser les mots de passe"},
	{"groups:read", "Lister/voir les groupes"},
	{"groups:write", "Créer/modifier/supprimer des groupes"},
	{"ous:read", "Lister/voir les OUs"},
	{"ous:write", "Créer/modifier/supprimer des OUs"},
	{"audit:read", "Consulter le journal d'audit"},
	{"config:read", "Lire la configuration"},
	{"config:write", "Modifier la configuration"},
	{"portal:read", "Consulter le portail auto-service"},
	{"portal:write", "Gérer le portail (démarrer/arrêter, valider demandes)"},
	{"webhooks:read", "Lister les webhooks"},
	{"webhooks:write", "Créer/modifier/supprimer des webhooks"},
}

// permissionsConnues indexe les permissions valides.
var permissionsConnues = func() map[string]bool {
	index := make(map[string]bool, len(PermissionsStandards))
	for _, permission := range PermissionsStandards {
		index[permission.Code] = true
	}
	return index
}()

// CleAPIInfo décrit une clé d'API. Les noms JSON sont ceux du dataclass
// Python : un `api_keys.json` écrit par l'un se relit par l'autre.
//
// Seule l'empreinte SHA-256 de la clé est stockée : la clé complète n'existe
// qu'au moment de sa création.
type CleAPIInfo struct {
	ID           string   `json:"id"`
	Nom          string   `json:"name"`
	Empreinte    string   `json:"key_hash"`
	Prefixe      string   `json:"prefix"`
	Permissions  []string `json:"permissions"`
	CreeLe       string   `json:"created_at"`
	ExpireLe     *string  `json:"expires_at"`
	DernierUsage *string  `json:"last_used_at"`
	Active       bool     `json:"active"`
}

// Expiree indique si la clé est hors validité.
func (c CleAPIInfo) Expiree(maintenant time.Time) bool {
	if c.ExpireLe == nil || *c.ExpireLe == "" {
		return false
	}
	moment, err := time.Parse(formatISO, *c.ExpireLe)
	if err != nil {
		return false
	}
	return !moment.After(maintenant)
}

// Autorise indique si la clé porte au moins une des permissions demandées.
func (c CleAPIInfo) Autorise(permissions ...string) bool {
	if len(permissions) == 0 {
		return true
	}
	for _, portee := range c.Permissions {
		if portee == "*" {
			return true
		}
		for _, attendue := range permissions {
			if portee == attendue {
				return true
			}
		}
	}
	return false
}

// MagasinCles charge, valide et persiste les clés d'API.
type MagasinCles struct {
	chemin string

	mu       sync.Mutex
	cles     map[string]CleAPIInfo
	prefixes map[string]string
}

// CheminClesDefaut retourne `api_keys.json` dans le dossier de configuration.
func CheminClesDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichierCles), nil
}

// OuvrirMagasinCles ouvre (et crée au besoin) le magasin des clés.
func OuvrirMagasinCles(chemin string) (*MagasinCles, error) {
	if chemin == "" {
		defaut, err := CheminClesDefaut()
		if err != nil {
			return nil, err
		}
		chemin = defaut
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return nil, fmt.Errorf("api: création du dossier des clés: %w", err)
	}
	magasin := &MagasinCles{
		chemin:   chemin,
		cles:     map[string]CleAPIInfo{},
		prefixes: map[string]string{},
	}
	magasin.charger()
	return magasin, nil
}

// Chemin retourne le chemin du fichier des clés.
func (m *MagasinCles) Chemin() string { return m.chemin }

// charger relit `api_keys.json` (jamais bloquant).
func (m *MagasinCles) charger() {
	donnees, err := os.ReadFile(m.chemin)
	if err != nil {
		return
	}
	var liste []CleAPIInfo
	if err := json.Unmarshal(donnees, &liste); err != nil {
		return
	}
	for _, cle := range liste {
		if cle.ID == "" {
			continue
		}
		if cle.Permissions == nil {
			cle.Permissions = []string{}
		}
		m.cles[cle.ID] = cle
		if cle.Prefixe != "" {
			m.prefixes[cle.Prefixe] = cle.ID
		}
	}
}

// enregistrer réécrit le fichier (l'appelant doit détenir le verrou).
func (m *MagasinCles) enregistrer() error {
	liste := make([]CleAPIInfo, 0, len(m.cles))
	for _, cle := range m.cles {
		liste = append(liste, cle)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].CreeLe < liste[j].CreeLe })
	for index := range liste {
		if liste[index].Permissions == nil {
			liste[index].Permissions = []string{}
		}
	}
	donnees, err := json.MarshalIndent(liste, "", "  ")
	if err != nil {
		return fmt.Errorf("api: sérialisation des clés: %w", err)
	}
	if err := os.WriteFile(m.chemin, donnees, 0o600); err != nil {
		return fmt.Errorf("api: écriture de %s: %w", m.chemin, err)
	}
	return nil
}

// Enregistrer force la réécriture du fichier.
func (m *MagasinCles) Enregistrer() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.enregistrer()
}

// Creer génère une clé et retourne (clé complète, métadonnées). La clé complète
// n'est jamais écrite sur disque.
func (m *MagasinCles) Creer(nom string, permissions []string, joursValidite int,
	maintenant time.Time) (string, CleAPIInfo, error) {
	if strings.TrimSpace(nom) == "" {
		return "", CleAPIInfo{}, nouvelleErreurAPI(422, "validation_error", "Nom requis", nil)
	}
	valides := []string{}
	for _, permission := range permissions {
		if permissionsConnues[permission] {
			valides = append(valides, permission)
		}
	}
	if len(valides) == 0 {
		return "", CleAPIInfo{}, nouvelleErreurAPI(422, "validation_error",
			"Aucune permission valide", nil)
	}

	complet, prefixe, err := genererCle()
	if err != nil {
		return "", CleAPIInfo{}, err
	}
	identifiant, err := identifiantAleatoire("ak_", 6)
	if err != nil {
		return "", CleAPIInfo{}, err
	}

	info := CleAPIInfo{
		ID:          identifiant,
		Nom:         strings.TrimSpace(nom),
		Empreinte:   empreinteCle(complet),
		Prefixe:     prefixe,
		Permissions: valides,
		CreeLe:      maintenant.UTC().Format(formatISO),
		Active:      true,
	}
	if joursValidite > 0 {
		expiration := maintenant.UTC().AddDate(0, 0, joursValidite).Format(formatISO)
		info.ExpireLe = &expiration
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.cles[info.ID] = info
	m.prefixes[info.Prefixe] = info.ID
	if err := m.enregistrer(); err != nil {
		return "", CleAPIInfo{}, err
	}
	return complet, info, nil
}

// Valider vérifie une clé complète et met à jour sa date de dernier usage.
func (m *MagasinCles) Valider(cle string, maintenant time.Time) *CleAPIInfo {
	if !strings.HasPrefix(cle, PrefixeCle) || len(cle) < 12 {
		return nil
	}
	prefixe := cle[:12]

	m.mu.Lock()
	defer m.mu.Unlock()
	identifiant, ok := m.prefixes[prefixe]
	if !ok {
		return nil
	}
	info, ok := m.cles[identifiant]
	if !ok || !info.Active || info.Expiree(maintenant) {
		return nil
	}
	attendu := []byte(info.Empreinte)
	obtenu := []byte(empreinteCle(cle))
	if len(attendu) != len(obtenu) || subtle.ConstantTimeCompare(attendu, obtenu) != 1 {
		return nil
	}
	usage := maintenant.UTC().Format(formatISO)
	info.DernierUsage = &usage
	m.cles[info.ID] = info
	_ = m.enregistrer()
	copie := info
	return &copie
}

// Lister retourne les clés triées de la plus récente à la plus ancienne.
func (m *MagasinCles) Lister(inclureInactives bool, maintenant time.Time) []CleAPIInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	liste := make([]CleAPIInfo, 0, len(m.cles))
	for _, cle := range m.cles {
		if !inclureInactives && (!cle.Active || cle.Expiree(maintenant)) {
			continue
		}
		liste = append(liste, cle)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].CreeLe > liste[j].CreeLe })
	return liste
}

// Obtenir retourne une clé par identifiant.
func (m *MagasinCles) Obtenir(identifiant string) *CleAPIInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	cle, ok := m.cles[identifiant]
	if !ok {
		return nil
	}
	copie := cle
	return &copie
}

// Revoquer désactive une clé sans l'effacer.
func (m *MagasinCles) Revoquer(identifiant string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cle, ok := m.cles[identifiant]
	if !ok {
		return false, nil
	}
	cle.Active = false
	m.cles[identifiant] = cle
	delete(m.prefixes, cle.Prefixe)
	if err := m.enregistrer(); err != nil {
		return false, err
	}
	return true, nil
}

// Supprimer efface définitivement une clé.
func (m *MagasinCles) Supprimer(identifiant string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cle, ok := m.cles[identifiant]
	if !ok {
		return false, nil
	}
	delete(m.cles, identifiant)
	delete(m.prefixes, cle.Prefixe)
	if err := m.enregistrer(); err != nil {
		return false, err
	}
	return true, nil
}

// DefinirPermissions remplace les permissions d'une clé (jamais « aucune »).
func (m *MagasinCles) DefinirPermissions(identifiant string, permissions []string) (*CleAPIInfo, error) {
	valides := []string{}
	for _, permission := range permissions {
		if permissionsConnues[permission] {
			valides = append(valides, permission)
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	cle, ok := m.cles[identifiant]
	if !ok {
		return nil, nil
	}
	cle.Permissions = valides
	m.cles[identifiant] = cle
	if err := m.enregistrer(); err != nil {
		return nil, err
	}
	return &cle, nil
}

// genererCle produit une clé `edusync_<32 caractères>` et son préfixe (12).
//
// L'encodage est `base64.RawURLEncoding` : même alphabet et même absence de
// remplissage que `secrets.token_urlsafe(24)` côté Python.
func genererCle() (string, string, error) {
	octets := make([]byte, 24)
	if _, err := rand.Read(octets); err != nil {
		return "", "", fmt.Errorf("api: génération de la clé: %w", err)
	}
	complet := PrefixeCle + base64.RawURLEncoding.EncodeToString(octets)
	return complet, complet[:12], nil
}

// identifiantAleatoire produit un identifiant préfixé (`ak_<12 hex>`).
func identifiantAleatoire(prefixe string, octets int) (string, error) {
	tampon := make([]byte, octets)
	if _, err := rand.Read(tampon); err != nil {
		return "", fmt.Errorf("api: génération de l'identifiant: %w", err)
	}
	return prefixe + hex.EncodeToString(tampon), nil
}

// empreinteCle retourne le SHA-256 hexadécimal d'une clé.
func empreinteCle(cle string) string {
	somme := sha256.Sum256([]byte(cle))
	return hex.EncodeToString(somme[:])
}
