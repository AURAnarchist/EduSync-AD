// Package grouptemplates porte le module M23 « Modèles de groupes ».
//
// Un modèle décrit « à quoi ressemble un groupe scolaire » : OU parente +
// motif de sous-OU, groupes auto-créés (motifs {classe}…), script de
// connexion, quota FSRM, dossier personnel, espace partagé, profil, heures de
// connexion, politique de mot de passe et licences Office 365.
//
// Le paquet reproduit fidèlement `core/templates.py` : mêmes noms de clés
// JSON (`group_templates.json`), même arithmétique, export/import JSON **et**
// XML strictement équivalents (les caractères de contrôle — dont le CRLF —
// passent en base64 pour survivre au parseur XML).
//
// L'instanciation (`Instancier`) s'appuie sur l'interface `Annuaire` afin
// d'être testable sans contrôleur de domaine ; l'adaptateur `PoolAnnuaire`
// relie l'implémentation à `internal/ad`.
package grouptemplates

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// NomFichier est le fichier de persistance des modèles (partagé avec Python).
const NomFichier = "group_templates.json"

// TypesModele catalogue les types de modèles (M23).
var TypesModele = map[string]string{
	"classe_eleve":      "Classe élève",
	"classe_prof":       "Classe prof",
	"personnel_admin":   "Personnel admin",
	"service_technique": "Service technique",
	"custom":            "Personnalisé",
}

// PorteesGroupe catalogue les portées de groupe AD.
var PorteesGroupe = map[string]string{
	"global":      "Globale",
	"domainlocal": "Locale au domaine",
	"universal":   "Universelle",
}

// ValeursTypeGroupe associe une portée à la valeur `groupType` AD
// (0x80000000 « sécurité » | portée).
var ValeursTypeGroupe = map[string]int32{
	"global":      -2147483646, // 0x80000002
	"domainlocal": -2147483644, // 0x80000004
	"universal":   -2147483640, // 0x80000008
}

var (
	reIDModele   = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	reChampMotif = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	reNomScript  = regexp.MustCompile(`[^\p{L}\p{N}_\-. ]`)
	reSamGroupe  = regexp.MustCompile(`[\\/\[\]:;|=,+*?"<>]`)
	reNomSur     = regexp.MustCompile(`[^A-Za-z0-9._-]+`)
)

// -- Motifs {champ} ---------------------------------------------------------

// analyserMotif parcourt un motif `{champ}` et retourne les champs rencontrés
// dans l'ordre d'apparition (les champs vides `{}` sont conservés pour que la
// validation les rejette comme le fait `string.Formatter`). `ok` vaut false si
// les accolades sont déséquilibrées ; les champs trouvés avant l'erreur sont
// tout de même retournés (comportement de `_pattern_fields`).
func analyserMotif(motif string) (champs []string, ok bool) {
	for i := 0; i < len(motif); {
		switch motif[i] {
		case '{':
			if i+1 < len(motif) && motif[i+1] == '{' {
				i += 2
				continue
			}
			j := i + 1
			for j < len(motif) && motif[j] != '}' {
				j++
			}
			if j >= len(motif) {
				return champs, false
			}
			nom := motif[i+1 : j]
			if k := strings.IndexAny(nom, "!:"); k >= 0 {
				nom = nom[:k]
			}
			champs = append(champs, nom)
			i = j + 1
		case '}':
			if i+1 < len(motif) && motif[i+1] == '}' {
				i += 2
				continue
			}
			return champs, false
		default:
			i++
		}
	}
	return champs, true
}

// validerMotif contrôle syntaxiquement un motif `{champ}`.
func validerMotif(motif, libelle string) []string {
	champs, ok := analyserMotif(motif)
	if !ok {
		return []string{fmt.Sprintf("%s : accolades non équilibrées.", libelle)}
	}
	var erreurs []string
	for _, champ := range champs {
		if !reChampMotif.MatchString(champ) {
			erreurs = append(erreurs, fmt.Sprintf("%s : champ « {%s} » invalide.", libelle, champ))
		}
	}
	return erreurs
}

// ChampsMotif retourne les champs uniques utilisés par un motif.
func ChampsMotif(motif string) []string {
	bruts, _ := analyserMotif(motif)
	var trouves []string
	for _, nom := range bruts {
		if nom == "" {
			continue
		}
		if !contientChaine(trouves, nom) {
			trouves = append(trouves, nom)
		}
	}
	return trouves
}

// RendreMotif rend un motif avec les valeurs fournies. Une valeur manquante
// produit une erreur contenant le nom du champ (comme le `ValueError` Python).
func RendreMotif(motif string, valeurs map[string]string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(motif); {
		switch motif[i] {
		case '{':
			if i+1 < len(motif) && motif[i+1] == '{' {
				b.WriteByte('{')
				i += 2
				continue
			}
			j := i + 1
			for j < len(motif) && motif[j] != '}' {
				j++
			}
			if j >= len(motif) {
				return "", fmt.Errorf("Impossible de rendre « %s » : accolades non équilibrées", motif)
			}
			nom := motif[i+1 : j]
			if k := strings.IndexAny(nom, "!:"); k >= 0 {
				nom = nom[:k]
			}
			valeur, ok := valeurs[nom]
			if !ok {
				return "", fmt.Errorf("Impossible de rendre « %s » : %s", motif, nom)
			}
			b.WriteString(valeur)
			i = j + 1
		case '}':
			if i+1 < len(motif) && motif[i+1] == '}' {
				b.WriteByte('}')
				i += 2
				continue
			}
			return "", fmt.Errorf("Impossible de rendre « %s » : accolade isolée", motif)
		default:
			b.WriteByte(motif[i])
			i++
		}
	}
	return b.String(), nil
}

// SamGroupe calcule le sAMAccountName d'un groupe à partir de son CN
// (caractères interdits remplacés par « - »).
func SamGroupe(nom string) string {
	sam := strings.TrimSpace(reSamGroupe.ReplaceAllString(nom, "-"))
	sam = strings.TrimRight(sam, ".")
	if sam == "" {
		return "groupe"
	}
	return sam
}

// -- Groupe -----------------------------------------------------------------

// SpecGroupe décrit un groupe auto-créé par le modèle.
type SpecGroupe struct {
	MotifNom    string `json:"name_pattern"`
	Portee      string `json:"scope"`
	Description string `json:"description"`
}

// ToDict sérialise le groupe au format Python.
func (s SpecGroupe) ToDict() map[string]any {
	return map[string]any{
		"name_pattern": s.MotifNom,
		"scope":        s.Portee,
		"description":  s.Description,
	}
}

// DepuisDict construit un groupe depuis un dictionnaire Python.
func SpecGroupeDepuisDict(data map[string]any) SpecGroupe {
	return SpecGroupe{
		MotifNom:    chaineDefaut(data["name_pattern"], ""),
		Portee:      chaineDefaut(data["scope"], "global"),
		Description: chaineDefaut(data["description"], ""),
	}
}

// Validate vérifie la cohérence du groupe.
func (s SpecGroupe) Validate() []string {
	var erreurs []string
	if strings.TrimSpace(s.MotifNom) == "" {
		erreurs = append(erreurs, "Nom de groupe vide.")
	}
	if _, ok := PorteesGroupe[s.Portee]; !ok {
		erreurs = append(erreurs, fmt.Sprintf(
			"Portée inconnue « %s » (attendu : %s).", s.Portee, clesTrieesPortees()))
	}
	erreurs = append(erreurs, validerMotif(s.MotifNom, "Groupe")...)
	return erreurs
}

// -- Configuration des sous-modules ------------------------------------------

// ScriptLogon décrit un script de connexion/déconnexion porté par le modèle.
type ScriptLogon struct {
	Nom      string `json:"name"`
	Type     string `json:"kind"`
	Moment   string `json:"timing"`
	Portee   string `json:"scope_type"`
	PorteeDN string `json:"scope_dn"`
	Contenu  string `json:"content"`
}

// TypesScript liste les types de scripts supportés.
var TypesScript = []string{"bat", "ps1", "vbs"}

// MomentsScript liste les moments d'exécution supportés.
var MomentsScript = []string{"logon", "logoff"}

// NomFichierScript retourne le nom de fichier déployé (extension normalisée).
func (s ScriptLogon) NomFichierScript() string {
	base := strings.TrimSpace(reNomScript.ReplaceAllString(s.Nom, "_"))
	if base == "" {
		base = "script"
	}
	suffixe := "." + s.Type
	if strings.HasSuffix(strings.ToLower(base), suffixe) {
		return base
	}
	return base + suffixe
}

// ToDict sérialise le script.
func (s ScriptLogon) ToDict() map[string]any {
	return map[string]any{
		"name":       s.Nom,
		"kind":       s.Type,
		"timing":     s.Moment,
		"scope_type": s.Portee,
		"scope_dn":   s.PorteeDN,
		"content":    s.Contenu,
	}
}

// ScriptDepuisDict construit un script depuis un dictionnaire Python.
func ScriptDepuisDict(data map[string]any) ScriptLogon {
	return ScriptLogon{
		Nom:      chaineDefaut(data["name"], ""),
		Type:     chaineDefaut(data["kind"], "bat"),
		Moment:   chaineDefaut(data["timing"], "logon"),
		Portee:   chaineDefaut(data["scope_type"], "default"),
		PorteeDN: chaineDefaut(data["scope_dn"], ""),
		Contenu:  chaineDefaut(data["content"], ""),
	}
}

// Validate vérifie la cohérence du script.
func (s ScriptLogon) Validate() []string {
	var erreurs []string
	if strings.TrimSpace(s.Nom) == "" {
		erreurs = append(erreurs, "Le nom du script est vide.")
	}
	if strings.ContainsAny(s.Nom, `/\`) {
		erreurs = append(erreurs, "Le nom du script ne doit pas contenir de séparateur de chemin.")
	}
	if !contientChaine(TypesScript, s.Type) {
		erreurs = append(erreurs, fmt.Sprintf(
			"Type de script non supporté : %s (attendu : %s).", s.Type, strings.Join(TypesScript, ", ")))
	}
	if !contientChaine(MomentsScript, s.Moment) {
		erreurs = append(erreurs, fmt.Sprintf(
			"Moment non supporté : %s (attendu : %s).", s.Moment, strings.Join(MomentsScript, ", ")))
	}
	if s.Portee != "default" && s.Portee != "ou" && s.Portee != "group" {
		erreurs = append(erreurs, fmt.Sprintf("Portée non supportée : %s.", s.Portee))
	}
	if (s.Portee == "ou" || s.Portee == "group") && s.PorteeDN == "" {
		erreurs = append(erreurs, "La portée OU/groupe ne désigne aucun objet.")
	}
	if strings.TrimSpace(s.Contenu) == "" {
		erreurs = append(erreurs, "Le contenu du script est vide.")
	}
	return erreurs
}

// ParametresQuota décrit un quota de dossier personnel (M14).
type ParametresQuota struct {
	TailleGo    float64 `json:"size_gb"`
	SeuilAlerte int     `json:"warning_percent"`
	LimiteDure  bool    `json:"hard_limit"`
}

// TailleQuotaGoParDefaut et SeuilAlerteParDefaut reprennent les valeurs Python.
const (
	TailleQuotaGoParDefaut = 10.0
	SeuilAlerteParDefaut   = 80
	GoEnOctets             = int64(1024 * 1024 * 1024)
)

// QuotaParDefaut retourne les paramètres de quota par défaut.
func QuotaParDefaut() ParametresQuota {
	return ParametresQuota{TailleGo: TailleQuotaGoParDefaut, SeuilAlerte: SeuilAlerteParDefaut, LimiteDure: true}
}

// TailleOctets retourne la taille du quota en octets.
func (q ParametresQuota) TailleOctets() int64 {
	return int64(q.TailleGo * float64(GoEnOctets))
}

// OctetsAlerte retourne le seuil d'alerte en octets.
func (q ParametresQuota) OctetsAlerte() int64 {
	return int64(float64(q.TailleOctets()) * float64(q.SeuilAlerte) / 100.0)
}

// NomModele retourne le nom du template FSRM déduit des paramètres.
func (q ParametresQuota) NomModele() string {
	taille := formatageCompact(q.TailleGo)
	taille = strings.ReplaceAll(taille, ".", "p")
	mode := "dur"
	if !q.LimiteDure {
		mode = "souple"
	}
	return fmt.Sprintf("EduSync-%sGo-%dpct-%s", taille, q.SeuilAlerte, mode)
}

// ToDict sérialise le quota.
func (q ParametresQuota) ToDict() map[string]any {
	return map[string]any{
		"size_gb":         q.TailleGo,
		"warning_percent": q.SeuilAlerte,
		"hard_limit":      q.LimiteDure,
	}
}

// QuotaDepuisDict construit un quota depuis un dictionnaire Python.
func QuotaDepuisDict(data map[string]any) ParametresQuota {
	return ParametresQuota{
		TailleGo:    nombreDefaut(data["size_gb"], TailleQuotaGoParDefaut),
		SeuilAlerte: entierDefaut(data["warning_percent"], SeuilAlerteParDefaut),
		LimiteDure:  booleenDefaut(data["hard_limit"], true),
	}
}

// Validate vérifie la cohérence du quota.
func (q ParametresQuota) Validate() []string {
	var erreurs []string
	if q.TailleGo <= 0 {
		erreurs = append(erreurs, "La taille du quota doit être strictement positive.")
	}
	if q.SeuilAlerte <= 0 || q.SeuilAlerte >= 100 {
		erreurs = append(erreurs, "Le seuil d'alerte doit être compris entre 1 et 99 %.")
	}
	return erreurs
}

// ConfigDossierPerso décrit les dossiers personnels (M17).
type ConfigDossierPerso struct {
	RacinePartage     string `json:"share_root"`
	LettreLecteur     string `json:"drive_letter"`
	ModeleDossier     string `json:"folder_template"`
	AppliquerCreation bool   `json:"apply_on_create"`
}

// Valeurs par défaut des dossiers personnels (identiques au Python).
const (
	RacinePartagePersoDefaut = `\\srv\homes`
	LettreLecteurDefaut      = "H:"
	ModeleDossierPersoDefaut = "%USERNAME%"
)

// DossierPersoParDefaut retourne la configuration par défaut.
func DossierPersoParDefaut() ConfigDossierPerso {
	return ConfigDossierPerso{
		RacinePartage:     RacinePartagePersoDefaut,
		LettreLecteur:     LettreLecteurDefaut,
		ModeleDossier:     ModeleDossierPersoDefaut,
		AppliquerCreation: true,
	}
}

// ToDict sérialise la configuration.
func (c ConfigDossierPerso) ToDict() map[string]any {
	return map[string]any{
		"share_root":      c.RacinePartage,
		"drive_letter":    c.LettreLecteur,
		"folder_template": c.ModeleDossier,
		"apply_on_create": c.AppliquerCreation,
	}
}

// DossierPersoDepuisDict construit la configuration depuis un dictionnaire.
func DossierPersoDepuisDict(data map[string]any) ConfigDossierPerso {
	return ConfigDossierPerso{
		RacinePartage:     chaineDefaut(data["share_root"], RacinePartagePersoDefaut),
		LettreLecteur:     chaineDefaut(data["drive_letter"], LettreLecteurDefaut),
		ModeleDossier:     chaineDefaut(data["folder_template"], ModeleDossierPersoDefaut),
		AppliquerCreation: booleenDefaut(data["apply_on_create"], true),
	}
}

// Validate vérifie la cohérence de la configuration.
func (c ConfigDossierPerso) Validate() []string {
	var erreurs []string
	racine := strings.TrimSpace(c.RacinePartage)
	if racine == "" {
		erreurs = append(erreurs, "Le partage racine est vide.")
	} else if !strings.HasPrefix(racine, `\\`) {
		erreurs = append(erreurs, `Le partage racine doit être un chemin UNC (\\serveur\partage).`)
	}
	lecteur := strings.TrimSpace(c.LettreLecteur)
	if len(lecteur) != 2 || !strings.HasSuffix(strings.ToUpper(lecteur), ":") {
		erreurs = append(erreurs, "La lettre de lecteur doit être de la forme « H: ».")
	}
	if strings.TrimSpace(c.ModeleDossier) == "" {
		erreurs = append(erreurs, "Le modèle de dossier est vide.")
	}
	return erreurs
}

// CheminPour résout le chemin UNC d'un dossier personnel.
func (c ConfigDossierPerso) CheminPour(nomUtilisateur, sam, ouDN, domaine string) string {
	dossier := c.ModeleDossier
	for variable, valeur := range map[string]string{
		"%USERNAME%": nomUtilisateur,
		"%SAM%":      sam,
		"%OU%":       nomOU(ouDN),
		"%DOMAIN%":   domaine,
	} {
		dossier = strings.ReplaceAll(dossier, variable, valeur)
	}
	return strings.TrimRight(c.RacinePartage, `\`) + `\` + dossier
}

// ConfigEspaceClasse décrit les espaces partagés par classe (M18).
type ConfigEspaceClasse struct {
	RacinePartage   string `json:"share_root"`
	ModeleDossier   string `json:"folder_template"`
	GroupeProfs     string `json:"teachers_group"`
	OUProfs         string `json:"teachers_ou"`
	AutoSurCreation bool   `json:"auto_on_group_create"`
}

// Valeurs par défaut des espaces de classe (identiques au Python).
const (
	RacinePartageClassesDefaut = `\\srv\classes`
	ModeleDossierClassesDefaut = "%GROUP%"
)

// EspaceClasseParDefaut retourne la configuration par défaut.
func EspaceClasseParDefaut() ConfigEspaceClasse {
	return ConfigEspaceClasse{
		RacinePartage:   RacinePartageClassesDefaut,
		ModeleDossier:   ModeleDossierClassesDefaut,
		AutoSurCreation: true,
	}
}

// ToDict sérialise la configuration.
func (c ConfigEspaceClasse) ToDict() map[string]any {
	return map[string]any{
		"share_root":           c.RacinePartage,
		"folder_template":      c.ModeleDossier,
		"teachers_group":       c.GroupeProfs,
		"teachers_ou":          c.OUProfs,
		"auto_on_group_create": c.AutoSurCreation,
	}
}

// EspaceClasseDepuisDict construit la configuration depuis un dictionnaire.
func EspaceClasseDepuisDict(data map[string]any) ConfigEspaceClasse {
	return ConfigEspaceClasse{
		RacinePartage:   chaineDefaut(data["share_root"], RacinePartageClassesDefaut),
		ModeleDossier:   chaineDefaut(data["folder_template"], ModeleDossierClassesDefaut),
		GroupeProfs:     chaineDefaut(data["teachers_group"], ""),
		OUProfs:         chaineDefaut(data["teachers_ou"], ""),
		AutoSurCreation: booleenDefaut(data["auto_on_group_create"], true),
	}
}

// Validate vérifie la cohérence de la configuration.
func (c ConfigEspaceClasse) Validate() []string {
	var erreurs []string
	racine := strings.TrimSpace(c.RacinePartage)
	if racine == "" {
		erreurs = append(erreurs, "Le partage racine est vide.")
	} else if !strings.HasPrefix(racine, `\\`) {
		erreurs = append(erreurs, `Le partage racine doit être un chemin UNC (\\serveur\partage).`)
	}
	if strings.TrimSpace(c.ModeleDossier) == "" {
		erreurs = append(erreurs, "Le modèle de dossier est vide.")
	}
	return erreurs
}

// CheminPour résout le chemin de l'espace d'un groupe.
func (c ConfigEspaceClasse) CheminPour(nomGroupe string) string {
	dossier := strings.ReplaceAll(c.ModeleDossier, "%GROUP%", nomGroupe)
	return strings.TrimRight(c.RacinePartage, `\`) + `\` + dossier
}

// ConfigProfil décrit un profil utilisateur (M13).
type ConfigProfil struct {
	TypeProfil        string `json:"profile_type"`
	CheminItinerant   string `json:"roaming_path"`
	CheminLocal       string `json:"local_path"`
	CheminObligatoire string `json:"mandatory_path"`
}

// TypesProfil liste les types de profils supportés.
var TypesProfil = []string{"local", "roaming", "mandatory"}

// ToDict sérialise la configuration.
func (c ConfigProfil) ToDict() map[string]any {
	return map[string]any{
		"profile_type":   c.TypeProfil,
		"roaming_path":   c.CheminItinerant,
		"local_path":     c.CheminLocal,
		"mandatory_path": c.CheminObligatoire,
	}
}

// ProfilDepuisDict construit la configuration depuis un dictionnaire.
func ProfilDepuisDict(data map[string]any) ConfigProfil {
	return ConfigProfil{
		TypeProfil:        chaineDefaut(data["profile_type"], "local"),
		CheminItinerant:   chaineDefaut(data["roaming_path"], ""),
		CheminLocal:       chaineDefaut(data["local_path"], ""),
		CheminObligatoire: chaineDefaut(data["mandatory_path"], ""),
	}
}

// Validate vérifie la cohérence de la configuration.
func (c ConfigProfil) Validate() []string {
	if !contientChaine(TypesProfil, c.TypeProfil) {
		return []string{fmt.Sprintf("Type de profil inconnu « %s ».", c.TypeProfil)}
	}
	return nil
}

// -- Modèle -----------------------------------------------------------------

// ModeleGroupe décrit la structure AD et les politiques d'un modèle.
type ModeleGroupe struct {
	ID              string
	Nom             string
	Type            string
	Description     string
	OUParenteDN     string
	MotifOU         string
	Groupes         []SpecGroupe
	ScriptLogon     *ScriptLogon
	Quota           *ParametresQuota
	Dossier         *ConfigDossierPerso
	Espace          *ConfigEspaceClasse
	Profil          *ConfigProfil
	PrereglageHeure string
	Politique       *models.PasswordPolicy
	Licences        []string
}

// MarshalJSON sérialise le modèle au format Python (via ToDict).
func (m ModeleGroupe) MarshalJSON() ([]byte, error) {
	return json.Marshal(m.ToDict())
}

// UnmarshalJSON construit un modèle depuis le format Python.
func (m *ModeleGroupe) UnmarshalJSON(data []byte) error {
	var brut map[string]any
	if err := json.Unmarshal(data, &brut); err != nil {
		return err
	}
	*m = ModeleDepuisDict(brut)
	return nil
}

// ToDict sérialise le modèle au format Python.
func (m ModeleGroupe) ToDict() map[string]any {
	groupes := make([]any, 0, len(m.Groupes))
	for _, g := range m.Groupes {
		groupes = append(groupes, g.ToDict())
	}
	licences := make([]any, 0, len(m.Licences))
	for _, l := range m.Licences {
		licences = append(licences, l)
	}

	var script any
	if m.ScriptLogon != nil {
		script = m.ScriptLogon.ToDict()
	}
	var quota any
	if m.Quota != nil {
		quota = m.Quota.ToDict()
	}
	var dossier any
	if m.Dossier != nil {
		dossier = m.Dossier.ToDict()
	}
	var espace any
	if m.Espace != nil {
		espace = m.Espace.ToDict()
	}
	var profil any
	if m.Profil != nil {
		profil = m.Profil.ToDict()
	}
	var politique any
	if m.Politique != nil {
		politique = PolitiqueVersDict(*m.Politique)
	}

	return map[string]any{
		"id":              m.ID,
		"name":            m.Nom,
		"kind":            m.Type,
		"description":     m.Description,
		"ou_parent_dn":    m.OUParenteDN,
		"ou_rdn_pattern":  m.MotifOU,
		"groups":          groupes,
		"logon_script":    script,
		"quota":           quota,
		"home":            dossier,
		"space":           espace,
		"profile":         profil,
		"hours_preset":    m.PrereglageHeure,
		"password_policy": politique,
		"licenses":        licences,
	}
}

// ModeleDepuisDict construit un modèle depuis un dictionnaire Python.
func ModeleDepuisDict(data map[string]any) ModeleGroupe {
	modele := ModeleGroupe{
		ID:              chaineDefaut(data["id"], ""),
		Nom:             chaineDefaut(data["name"], ""),
		Type:            chaineDefaut(data["kind"], "custom"),
		Description:     chaineDefaut(data["description"], ""),
		OUParenteDN:     chaineDefaut(data["ou_parent_dn"], ""),
		MotifOU:         chaineDefaut(data["ou_rdn_pattern"], ""),
		Groupes:         []SpecGroupe{},
		PrereglageHeure: chaineDefaut(data["hours_preset"], ""),
		Licences:        []string{},
	}
	for _, item := range listeCartes(data["groups"]) {
		modele.Groupes = append(modele.Groupes, SpecGroupeDepuisDict(item))
	}
	if carte, ok := carteAny(data["logon_script"]); ok {
		script := ScriptDepuisDict(carte)
		modele.ScriptLogon = &script
	}
	if carte, ok := carteAny(data["quota"]); ok {
		quota := QuotaDepuisDict(carte)
		modele.Quota = &quota
	}
	if carte, ok := carteAny(data["home"]); ok {
		dossier := DossierPersoDepuisDict(carte)
		modele.Dossier = &dossier
	}
	if carte, ok := carteAny(data["space"]); ok {
		espace := EspaceClasseDepuisDict(carte)
		modele.Espace = &espace
	}
	if carte, ok := carteAny(data["profile"]); ok {
		profil := ProfilDepuisDict(carte)
		modele.Profil = &profil
	}
	if carte, ok := carteAny(data["password_policy"]); ok {
		politique := PolitiqueDepuisDict(carte)
		modele.Politique = &politique
	}
	for _, sku := range listeAny(data["licenses"]) {
		modele.Licences = append(modele.Licences, valeurChaine(sku))
	}
	return modele
}

// Clone réalise une copie profonde du modèle.
func (m ModeleGroupe) Clone() ModeleGroupe {
	return ModeleDepuisDict(m.ToDict())
}

// Validate vérifie la cohérence complète du modèle.
func (m ModeleGroupe) Validate() []string {
	var erreurs []string
	if !reIDModele.MatchString(m.ID) {
		erreurs = append(erreurs, fmt.Sprintf(
			"Identifiant invalide « %s » (minuscules, chiffres, « _ » et « - » uniquement).", m.ID))
	}
	if strings.TrimSpace(m.Nom) == "" {
		erreurs = append(erreurs, "Le nom du modèle est vide.")
	}
	if _, ok := TypesModele[m.Type]; !ok {
		erreurs = append(erreurs, fmt.Sprintf("Type de modèle inconnu « %s ».", m.Type))
	}
	if m.MotifOU != "" {
		erreurs = append(erreurs, validerMotif(m.MotifOU, "Motif d'OU")...)
	}
	if len(m.Groupes) == 0 {
		erreurs = append(erreurs, "Aucun groupe défini.")
	}
	vus := map[string]bool{}
	for index, spec := range m.Groupes {
		for _, err := range spec.Validate() {
			erreurs = append(erreurs, fmt.Sprintf("Groupe %d : %s", index+1, err))
		}
		cle := strings.TrimSpace(spec.MotifNom)
		if cle != "" && vus[cle] {
			erreurs = append(erreurs, fmt.Sprintf("Groupe %d : motif dupliqué « %s ».", index+1, cle))
		}
		vus[cle] = true
	}
	if m.ScriptLogon != nil {
		erreurs = append(erreurs, m.ScriptLogon.Validate()...)
	}
	if m.Quota != nil {
		erreurs = append(erreurs, m.Quota.Validate()...)
	}
	if m.Dossier != nil {
		erreurs = append(erreurs, m.Dossier.Validate()...)
	}
	if m.Espace != nil {
		erreurs = append(erreurs, m.Espace.Validate()...)
	}
	if m.PrereglageHeure != "" && !contientChaine(nomsPrereglages(), m.PrereglageHeure) {
		erreurs = append(erreurs, fmt.Sprintf("Préréglage d'heures inconnu « %s ».", m.PrereglageHeure))
	}
	if m.Politique != nil && (m.Politique.Longueur < 6 || m.Politique.Longueur > 128) {
		erreurs = append(erreurs, "Longueur de mot de passe hors bornes (6 à 128).")
	}
	for _, sku := range m.Licences {
		if strings.TrimSpace(sku) == "" {
			erreurs = append(erreurs, "Licence O365 vide.")
		}
	}
	return erreurs
}

// Placeholders retourne les champs `{…}` utilisés par les motifs.
func (m ModeleGroupe) Placeholders() []string {
	var trouves []string
	motifs := make([]string, 0, len(m.Groupes)+1)
	if m.MotifOU != "" {
		motifs = append(motifs, m.MotifOU)
	}
	for _, g := range m.Groupes {
		motifs = append(motifs, g.MotifNom)
	}
	for _, motif := range motifs {
		for _, nom := range ChampsMotif(motif) {
			if !contientChaine(trouves, nom) {
				trouves = append(trouves, nom)
			}
		}
	}
	return trouves
}

// ValiderValeurs retourne les valeurs manquantes pour rendre les motifs.
func ValiderValeurs(modele ModeleGroupe, valeurs map[string]string) []string {
	var erreurs []string
	for _, nom := range modele.Placeholders() {
		if strings.TrimSpace(valeurs[nom]) == "" {
			erreurs = append(erreurs, fmt.Sprintf("Valeur manquante pour « %s ».", nom))
		}
	}
	return erreurs
}

// -- Politique de mot de passe ----------------------------------------------

// PolitiqueVersDict sérialise une politique au format `asdict` Python (le
// champ `pattern_fixe` vaut `null` lorsqu'il est vide).
func PolitiqueVersDict(p models.PasswordPolicy) map[string]any {
	var motif any
	if p.PatternFixe != "" {
		motif = p.PatternFixe
	}
	return map[string]any{
		"longueur":               p.Longueur,
		"majuscules":             p.Majuscules,
		"chiffres":               p.Chiffres,
		"caracteres_speciaux":    p.CaracteresSpeciaux,
		"mot_de_passe_identique": p.MotDePasseIdentique,
		"pattern_fixe":           motif,
	}
}

// PolitiqueDepuisDict construit une politique depuis un dictionnaire Python
// (les clés inconnues sont ignorées comme le fait `_policy_from_dict`).
func PolitiqueDepuisDict(data map[string]any) models.PasswordPolicy {
	p := models.DefaultPasswordPolicy()
	p.Longueur = entierDefaut(data["longueur"], p.Longueur)
	p.Majuscules = booleenDefaut(data["majuscules"], p.Majuscules)
	p.Chiffres = booleenDefaut(data["chiffres"], p.Chiffres)
	p.CaracteresSpeciaux = booleenDefaut(data["caracteres_speciaux"], p.CaracteresSpeciaux)
	p.MotDePasseIdentique = booleenDefaut(data["mot_de_passe_identique"], p.MotDePasseIdentique)
	p.PatternFixe = chaineDefaut(data["pattern_fixe"], "")
	return p
}

// -- Modèles intégrés -------------------------------------------------------

// ModelesIntegres retourne les 4 modèles fournis par défaut (chemins serveur
// à adapter).
func ModelesIntegres() []ModeleGroupe {
	politique := func(longueur int) *models.PasswordPolicy {
		p := models.PasswordPolicy{
			Longueur:           longueur,
			Majuscules:         true,
			Chiffres:           true,
			CaracteresSpeciaux: true,
		}
		return &p
	}
	quotaClasse := ParametresQuota{TailleGo: 5, SeuilAlerte: 80, LimiteDure: false}
	dossier := DossierPersoParDefaut()
	espace := EspaceClasseParDefaut()
	espaceService := EspaceClasseParDefaut()
	espaceService.RacinePartage = `\\srv\services`
	profilItinerant := ConfigProfil{TypeProfil: "roaming", CheminItinerant: `\\srv\profils\%USERNAME%`}

	scriptMapping := ScriptLogon{
		Nom:    "mapping-lecteur-h",
		Type:   "bat",
		Moment: "logon",
		Portee: "default",
		Contenu: "@echo off\r\n" +
			"if not \"%HOMEDIR%\"==\"\" (\r\n" +
			"    net use H: \"%HOMEDIR%\" /persistent:yes\r\n" +
			")\r\n",
	}
	scriptAccueil := ScriptLogon{
		Nom:    "message-accueil",
		Type:   "bat",
		Moment: "logon",
		Portee: "default",
		Contenu: "@echo off\r\n" +
			"echo ============================================\r\n" +
			"echo  Bienvenue %FULLNAME% (%USERNAME%)\r\n" +
			"echo  Classe / OU : %OU%\r\n" +
			"echo  Mail : %EMAIL%\r\n" +
			"echo ============================================\r\n",
	}

	return []ModeleGroupe{
		{
			ID:   "classe_eleve",
			Nom:  "Classe élève",
			Type: "classe_eleve",
			Description: "OU de classe, groupe des élèves, espace partagé en écriture, " +
				"quota 5 Go, profil itinérant, lecteur H:, heures de cours.",
			MotifOU: "Eleves-{classe}",
			Groupes: []SpecGroupe{
				{MotifNom: "{classe}", Portee: "global", Description: "Groupe de classe (élèves + espace)"},
			},
			ScriptLogon:     &scriptMapping,
			Quota:           &quotaClasse,
			Dossier:         &dossier,
			Espace:          &espace,
			Profil:          &profilItinerant,
			PrereglageHeure: "Heures cours",
			Politique:       politique(12),
			Licences:        []string{},
		},
		{
			ID:   "classe_prof",
			Nom:  "Classe prof",
			Type: "classe_prof",
			Description: "Groupe des enseignants affectés à une classe, larges heures " +
				"de connexion, profil itinérant, message d'accueil.",
			MotifOU: "Profs-{classe}",
			Groupes: []SpecGroupe{
				{MotifNom: "{classe}-Profs", Portee: "global", Description: "Enseignants de la classe"},
			},
			ScriptLogon:     &scriptAccueil,
			Dossier:         &dossier,
			Profil:          &profilItinerant,
			PrereglageHeure: "Heures admin",
			Politique:       politique(14),
			Licences:        []string{},
		},
		{
			ID:   "personnel_admin",
			Nom:  "Personnel admin",
			Type: "personnel_admin",
			Description: "Personnel administratif et direction : OU dédiée, deux groupes, " +
				"politique de mot de passe renforcée, heures administrateur.",
			MotifOU: "Personnel-Admin",
			Groupes: []SpecGroupe{
				{MotifNom: "Personnel-Admin", Portee: "global", Description: "Personnel administratif"},
				{MotifNom: "Administrateurs", Portee: "universal", Description: "Administrateurs de l'établissement"},
			},
			Dossier:         &dossier,
			Profil:          &profilItinerant,
			PrereglageHeure: "Heures admin",
			Politique:       politique(16),
			Licences:        []string{},
		},
		{
			ID:   "service_technique",
			Nom:  "Service technique",
			Type: "service_technique",
			Description: "Service technique / informatique : aucun filtrage horaire " +
				"(exigence de permanence), mot de passe renforcé, espace de service.",
			MotifOU: "Services-Techniques",
			Groupes: []SpecGroupe{
				{MotifNom: "Service-Technique", Portee: "global", Description: "Agents du service technique"},
			},
			Espace:    &espaceService,
			Dossier:   &dossier,
			Profil:    &profilItinerant,
			Politique: politique(16),
			Licences:  []string{},
		},
	}
}

// -- Persistance ------------------------------------------------------------

// CheminParDefaut retourne le chemin du fichier de modèles.
func CheminParDefaut(dossierConfig string) string {
	return filepath.Join(dossierConfig, NomFichier)
}

// ChargerModeles charge les modèles du fichier ; fichier absent ou corrompu →
// modèles intégrés.
func ChargerModeles(chemin string) []ModeleGroupe {
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		return ModelesIntegres()
	}
	var racine any
	if err := json.Unmarshal(contenu, &racine); err != nil {
		return ModelesIntegres()
	}
	var items []any
	switch valeur := racine.(type) {
	case map[string]any:
		if modeles, ok := valeur["modeles"]; ok {
			items = listeAny(modeles)
		} else {
			return ModelesIntegres()
		}
	case []any:
		items = valeur
	default:
		return ModelesIntegres()
	}
	modeles := make([]ModeleGroupe, 0, len(items))
	for _, item := range items {
		carte, ok := carteAny(item)
		if !ok {
			continue
		}
		modeles = append(modeles, ModeleDepuisDict(carte))
	}
	valides := modeles[:0]
	for _, modele := range modeles {
		if strings.TrimSpace(modele.ID) == "" && strings.TrimSpace(modele.Nom) == "" {
			continue
		}
		valides = append(valides, modele)
	}
	return valides
}

// SauvegarderModeles écrit les modèles au format Python et retourne le chemin.
func SauvegarderModeles(dossierConfig string, modeles []ModeleGroupe) (string, error) {
	chemin := CheminParDefaut(dossierConfig)
	if err := os.MkdirAll(filepath.Dir(chemin), 0o750); err != nil {
		return "", fmt.Errorf("grouptemplates: création du dossier: %w", err)
	}
	items := make([]any, 0, len(modeles))
	for _, modele := range modeles {
		items = append(items, modele.ToDict())
	}
	donnees, err := json.MarshalIndent(map[string]any{"modeles": items}, "", "  ")
	if err != nil {
		return "", fmt.Errorf("grouptemplates: sérialisation: %w", err)
	}
	if err := os.WriteFile(chemin, donnees, 0o600); err != nil {
		return "", fmt.Errorf("grouptemplates: écriture: %w", err)
	}
	return chemin, nil
}

// IDModeleUnique retourne un identifiant libre pour une duplication
// (base, base-2, base-3…).
func IDModeleUnique(modeles []ModeleGroupe, base string) string {
	plie := plierAccents(strings.ToLower(strings.TrimSpace(base)))
	netoye := strings.Trim(regexp.MustCompile(`[^a-z0-9_-]+`).ReplaceAllString(plie, "-"), "-")
	if netoye == "" {
		netoye = "modele"
	}
	if !reIDModele.MatchString(netoye) {
		netoye = "modele"
	}
	existants := map[string]bool{}
	for _, modele := range modeles {
		existants[modele.ID] = true
	}
	candidat := netoye
	for compteur := 2; existants[candidat]; compteur++ {
		candidat = fmt.Sprintf("%s-%d", netoye, compteur)
	}
	return candidat
}

// DupliquerModele copie un modèle avec un nouvel id et un nouveau nom.
func DupliquerModele(modeles []ModeleGroupe, modele ModeleGroupe, nouveauNom string) ModeleGroupe {
	clone := modele.Clone()
	clone.ID = IDModeleUnique(modeles, modele.ID)
	clone.Nom = strings.TrimSpace(nouveauNom)
	if clone.Nom == "" {
		clone.Nom = modele.Nom + " (copie)"
	}
	return clone
}

// OctetsJSON retourne la représentation JSON d'un modèle (format Python).
func OctetsJSON(modele ModeleGroupe) ([]byte, error) {
	donnees, err := json.MarshalIndent(modele.ToDict(), "", "  ")
	if err != nil {
		return nil, fmt.Errorf("grouptemplates: sérialisation: %w", err)
	}
	return donnees, nil
}

// ExporterModele écrit un modèle en JSON et retourne le chemin.
func ExporterModele(modele ModeleGroupe, chemin string) (string, error) {
	if err := os.MkdirAll(filepath.Dir(chemin), 0o750); err != nil {
		return "", fmt.Errorf("grouptemplates: création du dossier: %w", err)
	}
	donnees, err := OctetsJSON(modele)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(chemin, donnees, 0o600); err != nil {
		return "", fmt.Errorf("grouptemplates: écriture: %w", err)
	}
	return chemin, nil
}

// ImporterModele lit un modèle JSON (échoue s'il est invalide).
func ImporterModele(chemin string) (ModeleGroupe, error) {
	contenu, err := os.ReadFile(chemin)
	if err != nil {
		return ModeleGroupe{}, fmt.Errorf("Lecture du modèle impossible : %w", err)
	}
	var racine any
	if err := json.Unmarshal(contenu, &racine); err != nil {
		return ModeleGroupe{}, fmt.Errorf("Lecture du modèle impossible : %w", err)
	}
	carte, ok := carteAny(racine)
	if !ok {
		return ModeleGroupe{}, fmt.Errorf("Format de modèle invalide (objet JSON attendu).")
	}
	modele := ModeleDepuisDict(carte)
	if erreurs := modele.Validate(); len(erreurs) > 0 {
		return ModeleGroupe{}, fmt.Errorf("Modèle invalide : %s", strings.Join(erreurs, "; "))
	}
	return modele, nil
}

// -- Utilitaires ------------------------------------------------------------

func contientChaine(liste []string, valeur string) bool {
	for _, item := range liste {
		if item == valeur {
			return true
		}
	}
	return false
}

func clesTrieesPortees() string {
	cles := make([]string, 0, len(PorteesGroupe))
	for cle := range PorteesGroupe {
		cles = append(cles, cle)
	}
	sort.Strings(cles)
	return strings.Join(cles, ", ")
}

func nomOU(dn string) string {
	for _, partie := range strings.Split(dn, ",") {
		partie = strings.TrimSpace(partie)
		if len(partie) >= 3 && strings.EqualFold(partie[:3], "OU=") {
			return partie[3:]
		}
	}
	return ""
}

// formatageCompact reproduit `f"{value:g}"` de Python.
func formatageCompact(valeur float64) string {
	return strconv.FormatFloat(valeur, 'g', -1, 64)
}

func valeurChaine(valeur any) string {
	switch v := valeur.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return fmt.Sprint(v)
	}
}

func chaineDefaut(valeur any, defaut string) string {
	if texte, ok := valeur.(string); ok {
		return texte
	}
	return defaut
}

func entierDefaut(valeur any, defaut int) int {
	switch v := valeur.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return defaut
}

func booleenDefaut(valeur any, defaut bool) bool {
	if b, ok := valeur.(bool); ok {
		return b
	}
	return defaut
}

func nombreDefaut(valeur any, defaut float64) float64 {
	switch v := valeur.(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case json.Number:
		if n, err := v.Float64(); err == nil {
			return n
		}
	}
	return defaut
}

func carteAny(valeur any) (map[string]any, bool) {
	carte, ok := valeur.(map[string]any)
	return carte, ok
}

func listeAny(valeur any) []any {
	if liste, ok := valeur.([]any); ok {
		return liste
	}
	return nil
}

// listeCartes accepte aussi bien `[]any` que `[]map[string]any`.
func listeCartes(valeur any) []map[string]any {
	var cartes []map[string]any
	switch v := valeur.(type) {
	case []any:
		for _, item := range v {
			if carte, ok := carteAny(item); ok {
				cartes = append(cartes, carte)
			}
		}
	case []map[string]any:
		cartes = v
	}
	return cartes
}

// tableAccents associe les lettres latines décomposables à leur base (résultat
// de NFKD + suppression des diacritiques pour les langues latines).
var tableAccents = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c",
	'ď': "d", 'đ': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ĝ': "g", 'ğ': "g", 'ġ': "g", 'ģ': "g",
	'ĥ': "h", 'ħ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i", 'ı': "i",
	'ĵ': "j",
	'ķ': "k",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ŀ': "l", 'ł': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ø': "o", 'ō': "o", 'ŏ': "o", 'ő': "o",
	'ŕ': "r", 'ŗ': "r", 'ř': "r",
	'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s",
	'ţ': "t", 'ť': "t", 'ŧ': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w",
	'ý': "y", 'ÿ': "y", 'ŷ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
}

// plierAccents remplace les lettres accentuées par leur base (la casse doit
// déjà être abaissée avant l'appel).
func plierAccents(texte string) string {
	var b strings.Builder
	for _, r := range texte {
		if base, ok := tableAccents[r]; ok {
			b.WriteString(base)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}
