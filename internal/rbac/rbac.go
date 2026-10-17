// Package rbac implémente la délégation d'administration (M26).
//
// Cinq rôles pour les opérateurs de l'application :
//
//	super_admin     Tout, y compris gérer les délégués et les paramètres.
//	admin_site      Toutes les actions AD d'un site, sans gérer la délégation.
//	admin_classe    Créer/migrer/réinitialiser/désactiver… dans ses OU.
//	helpdesk        Consulter, réinitialiser un mot de passe, activer ou
//	                désactiver un compte… dans ses OU.
//	lecture_seule   Consulter et exporter, sans aucune écriture.
//
// Trois briques :
//
//   - Permissions : vocabulaire vérifié par le garde-fou de la connexion AD.
//   - Portées (délégation par OU/groupe) : un Grant associe un opérateur à un
//     rôle, à un site éventuel (M25) et à des DN cibles. Les rôles admin_classe
//     et helpdesk sont *scopés* : sans portée ils ne peuvent rien écrire, et
//     toute cible doit être incluse dans leurs portées.
//   - Résolution : ChargerPolitique lit delegations.json. Aucun délégué actif
//     implique que tout le monde est super_admin (comportement historique :
//     l'outil tourne sur le poste de l'administrateur). Délégués présents mais
//     opérateur inconnu implique un repli en lecture_seule.
//
// Le fichier delegations.json est en clair (il ne contient aucun secret, au
// contraire de domaines.json) mais il décrit qui peut faire quoi : il protège
// contre les erreurs d'un opérateur sur un poste partagé, pas contre un
// utilisateur local déterminé qui possède la machine et la clé secret.key.
package rbac

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
)

// VersionFormat est la version du fichier delegations.json.
const VersionFormat = 1

// NomFichier est le nom du fichier des délégués, partagé avec Python.
const NomFichier = "delegations.json"

// Role identifie le rôle d'un opérateur.
type Role string

// Les cinq rôles documentés.
const (
	SuperAdmin   Role = "super_admin"
	AdminSite    Role = "admin_site"
	AdminClasse  Role = "admin_classe"
	Helpdesk     Role = "helpdesk"
	LectureSeule Role = "lecture_seule"
)

// RoleLabels associe chaque rôle à son libellé français.
var RoleLabels = map[Role]string{
	SuperAdmin:   "Super-admin",
	AdminSite:    "Admin site",
	AdminClasse:  "Admin classe",
	Helpdesk:     "Helpdesk",
	LectureSeule: "Lecture seule",
}

// RoleOrder liste les rôles du plus large au plus restreint.
var RoleOrder = []Role{SuperAdmin, AdminSite, AdminClasse, Helpdesk, LectureSeule}

// Permissions est le vocabulaire de permissions (repris des métadonnées de
// plugins) : les clés sont celles vérifiées par la connexion AD.
var Permissions = map[string]string{
	"read_ad":         "Consulter l'annuaire (AD)",
	"read_user":       "Consulter les comptes",
	"read_audit":      "Consulter le journal d'actions",
	"export_data":     "Exporter des données",
	"create_user":     "Créer des comptes",
	"modify_user":     "Modifier des comptes",
	"move_user":       "Déplacer des comptes (migration)",
	"disable_account": "Activer / désactiver un compte",
	"delete_user":     "Supprimer définitivement un compte",
	"reset_password":  "Réinitialiser un mot de passe",
	"create_ou":       "Créer ou renommer une OU",
	"delete_ou":       "Supprimer une OU",
	"create_group":    "Créer un groupe",
	"modify_group":    "Modifier un groupe",
	"delete_group":    "Supprimer un groupe",
	"manage_groups":   "Gérer l'appartenance aux groupes",
	"modify_ad":       "Modifier l'annuaire (explorateur, quotas, scripts)",
	"admin":           "Administrer la délégation et les paramètres",
}

// ReadPermissions liste les permissions de lecture : exclusives des portées
// (tous les rôles lisent), seules les écritures sont limitées aux OU/groupes
// délégués.
var ReadPermissions = []string{"read_ad", "read_user", "read_audit", "export_data"}

// ScopedRoles liste les rôles dont les écritures sont restreintes aux portées.
var ScopedRoles = []Role{AdminClasse, Helpdesk}

// RolePermissions est la matrice rôle implique permissions.
var RolePermissions = construireMatrice()

// construireMatrice calcule la matrice rôle implique permissions.
func construireMatrice() map[Role]map[string]bool {
	every := map[string]bool{}
	for cle := range Permissions {
		every[cle] = true
	}
	matrice := map[Role]map[string]bool{}

	matrice[SuperAdmin] = copie(every)
	matrice[AdminSite] = sans(every, "admin")
	matrice[AdminClasse] = ens(
		"read_ad", "read_user", "read_audit", "export_data", "modify_ad",
		"create_user", "modify_user", "move_user", "disable_account",
		"reset_password", "create_ou", "create_group", "modify_group",
		"manage_groups",
	)
	matrice[Helpdesk] = ens(
		"read_ad", "read_user", "read_audit", "export_data",
		"reset_password", "disable_account", "modify_user",
	)
	matrice[LectureSeule] = ens(ReadPermissions...)
	return matrice
}

// ens construit un ensemble à partir de clés.
func ens(cles ...string) map[string]bool {
	m := make(map[string]bool, len(cles))
	for _, cle := range cles {
		m[cle] = true
	}
	return m
}

// copie retourne une copie d'un ensemble.
func copie(source map[string]bool) map[string]bool {
	m := make(map[string]bool, len(source))
	for cle := range source {
		m[cle] = true
	}
	return m
}

// sans retourne une copie sans les clés indiquées.
func sans(source map[string]bool, cles ...string) map[string]bool {
	m := copie(source)
	for _, cle := range cles {
		delete(m, cle)
	}
	return m
}

// RoleLabel retourne le libellé français d'un rôle.
func RoleLabel(role Role) string {
	if libelle, ok := RoleLabels[role]; ok {
		return libelle
	}
	return string(role)
}

// estScope indique si un rôle est soumis aux portées.
func estScope(role Role) bool {
	for _, r := range ScopedRoles {
		if r == role {
			return true
		}
	}
	return false
}

// rangRetourne retourne l'index du rôle dans RoleOrder (-1 si inconnu).
func rangRetourne(role Role) int {
	for i, r := range RoleOrder {
		if r == role {
			return i
		}
	}
	return -1
}

// ─── Portées ─────────────────────────────────────────────────────────────

var (
	espacesEgal    = regexp.MustCompile(`\s*=\s*`)
	espacesVirgule = regexp.MustCompile(`\s*,\s*`)
)

// NormalizeDN retourne le DN en minuscules, sans espaces autour des « = » et
// des virgules.
func NormalizeDN(dn string) string {
	nettoye := espacesEgal.ReplaceAllString(dn, "=")
	nettoye = espacesVirgule.ReplaceAllString(nettoye, ",")
	return strings.ToLower(strings.TrimSpace(nettoye))
}

// DNInScopes indique si dn est l'une des portées ou vit à l'intérieur
// (sous-arbre). Une portée OU=3emeA,OU=eleves,DC=lycee,DC=local couvre donc
// tous les comptes, sous-OU et groupes qui y sont rattachés.
func DNInScopes(dn string, scopes []string) bool {
	cible := NormalizeDN(dn)
	if cible == "" {
		return false
	}
	for _, scope := range scopes {
		racine := NormalizeDN(scope)
		if racine == "" {
			continue
		}
		if cible == racine || strings.HasSuffix(cible, ","+racine) {
			return true
		}
	}
	return false
}

// UniqueGrantID retourne un identifiant de délégué qui n'entre pas en collision
// avec les identifiants existants.
func UniqueGrantID(existants []Grant) string {
	pris := map[string]bool{}
	for _, grant := range existants {
		pris[grant.ID] = true
	}
	for {
		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			continue
		}
		candidat := hex.EncodeToString(buf)
		if !pris[candidat] {
			return candidat
		}
	}
}

// ─── Délégués ────────────────────────────────────────────────────────────

// Grant décrit un opérateur, son rôle, le site concerné et ses portées.
type Grant struct {
	ID        string   `json:"id"`
	Operateur string   `json:"operateur"`
	Role      Role     `json:"role"`
	Site      string   `json:"site"`
	OUs       []string `json:"ous"`
	Groupes   []string `json:"groupes"`
	Actif     bool     `json:"actif"`
}

// RoleLabel retourne le libellé du rôle du délégué.
func (g Grant) RoleLabel() string { return RoleLabel(g.Role) }

// IsScoped indique si le rôle du délégué est soumis aux portées.
func (g Grant) IsScoped() bool { return estScope(g.Role) }

// ScopeCount retourne le nombre de portées (OU + groupes) du délégué.
func (g Grant) ScopeCount() int { return len(g.OUs) + len(g.Groupes) }

// Validate retourne les messages d'erreur (vide = délégué valide).
func (g Grant) Validate() []string {
	var erreurs []string
	if strings.TrimSpace(g.Operateur) == "" {
		erreurs = append(erreurs, "Le compte de l'opérateur est obligatoire.")
	}
	if _, ok := RolePermissions[g.Role]; !ok {
		erreurs = append(erreurs, fmt.Sprintf("Rôle inconnu : %s", g.Role))
	}
	if g.IsScoped() && len(g.OUs) == 0 && len(g.Groupes) == 0 {
		erreurs = append(erreurs, fmt.Sprintf(
			"Le rôle « %s » exige au moins une portée (OU ou groupe délégué).", g.RoleLabel()))
	}
	for _, dn := range append(append([]string{}, g.OUs...), g.Groupes...) {
		if !strings.Contains(dn, "=") {
			erreurs = append(erreurs, fmt.Sprintf("Portée invalide (DN attendu) : %s", dn))
		}
	}
	return erreurs
}

// grantDepuisDict décode un délégué depuis une map brute (tolérant).
func grantDepuisDict(brut map[string]any) Grant {
	grant := Grant{
		ID:        chaine(brut["id"]),
		Operateur: chaine(brut["operateur"]),
		Role:      Role(chaine(brut["role"])),
		Site:      chaine(brut["site"]),
		OUs:       chaines(brut["ous"]),
		Groupes:   chaines(brut["groupes"]),
		Actif:     booleenDefaut(brut["actif"], true),
	}
	if grant.ID == "" {
		grant.ID = UniqueGrantID(nil)
	}
	if _, ok := RolePermissions[grant.Role]; !ok {
		grant.Role = LectureSeule
	}
	return grant
}

// chaine retourne une valeur comme chaîne (vide si absente).
func chaine(valeur any) string {
	if valeur == nil {
		return ""
	}
	if s, ok := valeur.(string); ok {
		return s
	}
	return fmt.Sprint(valeur)
}

// chaines retourne une valeur JSON comme liste de chaînes.
func chaines(valeur any) []string {
	liste, ok := valeur.([]any)
	if !ok {
		return []string{}
	}
	out := make([]string, 0, len(liste))
	for _, item := range liste {
		out = append(out, chaine(item))
	}
	return out
}

// booleenDefaut retourne un booléen, avec valeur par défaut si la clé est
// absente ; Python utilise actif=True par défaut.
func booleenDefaut(valeur any, defaut bool) bool {
	if valeur == nil {
		return defaut
	}
	b, ok := valeur.(bool)
	if !ok {
		return defaut
	}
	return b
}

// DelegationsPath retourne le chemin par défaut du fichier des délégués.
func DelegationsPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichier), nil
}

// ChargerGrants charge les délégués depuis un fichier. Un chemin vide utilise le
// chemin par défaut. Un fichier illisible est ignoré (implique aucun délégué).
func ChargerGrants(chemin string) ([]Grant, error) {
	if chemin == "" {
		defaut, err := DelegationsPath()
		if err != nil {
			return nil, err
		}
		chemin = defaut
	}
	if _, err := os.Stat(chemin); err != nil {
		return []Grant{}, nil
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return []Grant{}, nil
	}
	var payload struct {
		Grants []map[string]any `json:"grants"`
	}
	if err := json.Unmarshal(donnees, &payload); err != nil {
		return []Grant{}, nil
	}
	grants := make([]Grant, 0, len(payload.Grants))
	for _, brut := range payload.Grants {
		if brut == nil {
			continue
		}
		grants = append(grants, grantDepuisDict(brut))
	}
	return grants, nil
}

// SauvegarderGrants écrit delegations.json en clair (aucun secret).
func SauvegarderGrants(grants []Grant, chemin string) error {
	if chemin == "" {
		defaut, err := DelegationsPath()
		if err != nil {
			return err
		}
		chemin = defaut
	}
	nettoyes := make([]Grant, 0, len(grants))
	for _, grant := range grants {
		if grant.OUs == nil {
			grant.OUs = []string{}
		}
		if grant.Groupes == nil {
			grant.Groupes = []string{}
		}
		nettoyes = append(nettoyes, grant)
	}
	payload := map[string]any{"version": VersionFormat, "grants": nettoyes}
	donnees, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("rbac: encodage des délégués: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return fmt.Errorf("rbac: création du dossier: %w", err)
	}
	return os.WriteFile(chemin, donnees, 0o600)
}

// FindGrant retourne le délégué d'identifiant grantID (nil si absent).
func FindGrant(grants []Grant, grantID string) *Grant {
	if grantID == "" {
		return nil
	}
	for i := range grants {
		if grants[i].ID == grantID {
			return &grants[i]
		}
	}
	return nil
}

// ─── Résolution opérateur implique rôle ──────────────────────────────────

// normaliserOperateur accepte « admin », « LYCEE\admin » et
// « admin@lycee.local ».
func normaliserOperateur(valeur string) string {
	nettoye := strings.ToLower(strings.TrimSpace(valeur))
	if i := strings.LastIndex(nettoye, "\\"); i >= 0 {
		nettoye = nettoye[i+1:]
	}
	if i := strings.Index(nettoye, "@"); i >= 0 {
		nettoye = nettoye[:i]
	}
	return nettoye
}

// MatchingGrants retourne les délégués actifs d'un opérateur pour un site.
func MatchingGrants(grants []Grant, operateur, domain string) []Grant {
	voulu := normaliserOperateur(operateur)
	site := strings.ToLower(strings.TrimSpace(domain))
	var out []Grant
	for _, grant := range grants {
		if !grant.Actif {
			continue
		}
		if normaliserOperateur(grant.Operateur) != voulu {
			continue
		}
		if grant.Site != "" && strings.ToLower(strings.TrimSpace(grant.Site)) != site {
			continue
		}
		out = append(out, grant)
	}
	return out
}

// Policy est la décision pour un opérateur connecté à un site donné.
type Policy struct {
	Operator string
	Domain   string
	Role     Role
	OUs      []string
	Groupes  []string
	Source   string
}

// PolicyForGrants construit la politique de l'opérateur à partir des délégués.
func PolicyForGrants(operator, domain string, grants []Grant) *Policy {
	actifs := make([]Grant, 0, len(grants))
	for _, grant := range grants {
		if grant.Actif {
			actifs = append(actifs, grant)
		}
	}
	if len(actifs) == 0 {
		return &Policy{Operator: operator, Domain: domain, Role: SuperAdmin,
			Source: "aucun délégué configuré (rôle complet)"}
	}

	correspondances := MatchingGrants(grants, operator, domain)
	if len(correspondances) == 0 {
		return &Policy{Operator: operator, Domain: domain, Role: LectureSeule,
			Source: "opérateur non référencé dans la délégation"}
	}

	// Cumul : on garde le rôle le plus large (rang le plus faible) et l'union
	// des portées.
	meilleur := correspondances[0]
	for _, grant := range correspondances[1:] {
		if rangRetourne(grant.Role) < rangRetourne(meilleur.Role) {
			meilleur = grant
		}
	}
	var ous, groupes []string
	for _, grant := range correspondances {
		ous = ajouterUniques(ous, grant.OUs)
		groupes = ajouterUniques(groupes, grant.Groupes)
	}
	operateurs := make([]string, 0, len(correspondances))
	for _, grant := range correspondances {
		operateurs = append(operateurs, grant.Operateur)
	}
	sort.Strings(operateurs)
	return &Policy{
		Operator: operator,
		Domain:   domain,
		Role:     meilleur.Role,
		OUs:      ous,
		Groupes:  groupes,
		Source:   "délégation : " + strings.Join(operateurs, ", "),
	}
}

// ajouterUniques ajoute les valeurs non déjà présentes.
func ajouterUniques(base []string, valeurs []string) []string {
	for _, valeur := range valeurs {
		dejaPresent := false
		for _, existant := range base {
			if existant == valeur {
				dejaPresent = true
				break
			}
		}
		if !dejaPresent {
			base = append(base, valeur)
		}
	}
	return base
}

// Label retourne le libellé du rôle.
func (p *Policy) Label() string { return RoleLabel(p.Role) }

// Permissions retourne les permissions du rôle.
func (p *Policy) Permissions() map[string]bool {
	return RolePermissions[p.Role]
}

// IsScoped indique si le rôle est soumis aux portées.
func (p *Policy) IsScoped() bool { return estScope(p.Role) }

// Readonly indique si le rôle est en lecture seule.
func (p *Policy) Readonly() bool { return p.Role == LectureSeule }

// CanManageDelegation indique si le rôle peut gérer la délégation.
func (p *Policy) CanManageDelegation() bool { return p.Has("admin") }

// Has indique si le rôle possède une permission.
func (p *Policy) Has(permission string) bool { return p.Permissions()[permission] }

// HasAny indique si le rôle possède au moins une des permissions.
func (p *Policy) HasAny(permissions ...string) bool {
	for _, permission := range permissions {
		if p.Has(permission) {
			return true
		}
	}
	return false
}

// May indique si l'action est autorisée : permission et, pour un rôle scopé,
// inclusion des cibles dans les portées.
func (p *Policy) May(permission string, dns ...string) bool {
	if !p.Has(permission) {
		return false
	}
	var cibles []string
	for _, dn := range dns {
		if dn != "" {
			cibles = append(cibles, dn)
		}
	}
	if len(cibles) == 0 || estPermissionLecture(permission) {
		return true
	}
	if !p.IsScoped() {
		return true
	}
	if len(p.OUs) == 0 && len(p.Groupes) == 0 {
		return false
	}
	for _, dn := range cibles {
		if !DNInScopes(dn, p.OUs) && !DNInScopes(dn, p.Groupes) {
			return false
		}
	}
	return true
}

// estPermissionLecture indique si une permission est une lecture.
func estPermissionLecture(permission string) bool {
	for _, lecture := range ReadPermissions {
		if lecture == permission {
			return true
		}
	}
	return false
}

// ErrInsufficientRights signale un refus du garde-fou RBAC.
var ErrInsufficientRights = errors.New("droits insuffisants")

// Check retourne une erreur (ErrInsufficientRights) si l'action est refusée.
func (p *Policy) Check(permission string, dns ...string) error {
	libelle := Permissions[permission]
	if permission == "" || libelle == "" {
		return fmt.Errorf("%w : Action refusée « %s » : cette action n'a pas de permission RBAC déclarée (verrouillage de sécurité).",
			ErrInsufficientRights, permission)
	}
	if !p.Has(permission) {
		return fmt.Errorf("%w : Action refusée « %s » : le rôle « %s » de « %s » ne l'autorise pas.",
			ErrInsufficientRights, libelle, p.Label(), operateurOuInconnu(p.Operator))
	}
	if !p.May(permission, dns...) {
		return fmt.Errorf("%w : Action refusée « %s » : la cible est hors des OU/groupes délégués à « %s » (%s).",
			ErrInsufficientRights, libelle, p.Operator, porteesJointes(p))
	}
	return nil
}

// operateurOuInconnu retourne l'opérateur ou « opérateur inconnu ».
func operateurOuInconnu(operateur string) string {
	if operateur == "" {
		return "opérateur inconnu"
	}
	return operateur
}

// porteesJointes joint les portées de la politique (ou « aucune portée »).
func porteesJointes(p *Policy) string {
	toutes := append(append([]string{}, p.OUs...), p.Groupes...)
	if len(toutes) == 0 {
		return "aucune portée"
	}
	return strings.Join(toutes, ", ")
}

// Describe retourne le résumé affiché dans l'interface (rôle + portées).
func (p *Policy) Describe() string {
	texte := p.Label() + " — " + p.Source
	if p.IsScoped() && (len(p.OUs) > 0 || len(p.Groupes) > 0) {
		texte += fmt.Sprintf(" · %d OU, %d groupe(s)", len(p.OUs), len(p.Groupes))
	}
	return texte
}

// BuildPolicy retourne la politique de l'opérateur courant. Un chemin vide
// utilise delegations.json par défaut.
func BuildPolicy(operator, domain, chemin string) (*Policy, error) {
	grants, err := ChargerGrants(chemin)
	if err != nil {
		return nil, err
	}
	return PolicyForGrants(operator, domain, grants), nil
}
