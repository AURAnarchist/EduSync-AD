// Package classspaces implémente les espaces partagés par classe (M18) :
// configuration du partage (\\srv\classes\%GROUP%), classement profs/élèves,
// génération des scripts PowerShell de création du partage SMB + droits NTFS
// et de synchronisation membres AD ↔ droits, suivi des espaces en attente.
//
// La création effective du dossier, du partage SMB et des droits NTFS se fait
// sur le serveur de fichiers Windows : ce paquet produit les scripts, à
// l'image de la version Python (core/class_spaces.py). La configuration et
// l'état sont persistés dans class_spaces.json et class_spaces_state.json du
// dossier de configuration, au même format que la version Python.
package classspaces

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
)

const (
	// PartageRacineDefaut est le partage UNC racine par défaut.
	PartageRacineDefaut = `\\srv\classes`
	// ModeleDossierDefaut est le modèle de nom de dossier par défaut.
	ModeleDossierDefaut = "%GROUP%"

	// DroitsProfs droits NTFS des professeurs (icacls : Modification).
	DroitsProfs = "Modify"
	// DroitsEleves droits NTFS des élèves (icacls : Lecture et exécution).
	DroitsEleves = "ReadAndExecute"
	// DroitsAdmins droits NTFS des comptes d'administration (Contrôle total).
	DroitsAdmins = "FullControl"

	// RoleProf désigne un membre classé professeur.
	RoleProf = "prof"
	// RoleEleve désigne un membre classé élève.
	RoleEleve = "eleve"

	// FichierConfig est le nom du fichier de configuration.
	FichierConfig = "class_spaces.json"
	// FichierEtat est le nom du fichier d'état de synchronisation.
	FichierEtat = "class_spaces_state.json"
)

// AdminsPrincipaux liste les comptes d'administration jamais modifiés.
var AdminsPrincipaux = []string{`BUILTIN\Administrators`, `NT AUTHORITY\SYSTEM`}

// Config décrit la configuration des espaces de classe. Les noms de champs
// JSON correspondent à ceux de la version Python (parité des fichiers).
type Config struct {
	PartageRacine   string `json:"share_root"`           // ex : \\srv\classes
	ModeleDossier   string `json:"folder_template"`      // ex : %GROUP%
	GroupeProfs     string `json:"teachers_group"`       // DN du groupe AD des profs (optionnel)
	OUProfs         string `json:"teachers_ou"`          // DN de l'OU des profs (optionnel)
	AutoSurCreation bool   `json:"auto_on_group_create"` // enregistrer l'espace à la création du groupe
}

// ConfigParDefaut retourne la configuration par défaut.
func ConfigParDefaut() Config {
	return Config{
		PartageRacine:   PartageRacineDefaut,
		ModeleDossier:   ModeleDossierDefaut,
		AutoSurCreation: true,
	}
}

// ResolvePath construit le chemin UNC d'un espace pour un groupe donné.
func (c Config) ResolvePath(nomGroupe string) string {
	racine := strings.TrimRight(c.PartageRacine, `\`)
	dossier := strings.ReplaceAll(c.ModeleDossier, "%GROUP%", nomGroupe)
	return racine + `\` + dossier
}

// Validate vérifie la cohérence de la configuration et retourne la liste des
// erreurs (vide si la configuration est valide).
func (c Config) Validate() []string {
	var erreurs []string
	if strings.TrimSpace(c.PartageRacine) == "" {
		erreurs = append(erreurs, "Le partage racine est vide.")
	} else if !strings.HasPrefix(c.PartageRacine, `\\`) {
		erreurs = append(erreurs, `Le partage racine doit être un chemin UNC (\\serveur\partage).`)
	}
	if strings.TrimSpace(c.ModeleDossier) == "" {
		erreurs = append(erreurs, "Le modèle de dossier est vide.")
	} else if !strings.Contains(c.ModeleDossier, "%GROUP%") {
		erreurs = append(erreurs, "Le modèle de dossier doit contenir %GROUP%.")
	}
	return erreurs
}

// Plan décrit l'espace à créer/synchroniser pour un groupe.
type Plan struct {
	GroupDN   string
	GroupName string
	Path      string
	Profs     []string
	Eleves    []string
}

// ShareName retourne le nom du partage SMB (dernier segment du chemin).
func (p Plan) ShareName() string {
	return nomPartage(p.Path)
}

// Membres retourne la liste ordonnée des membres (profs puis élèves).
func (p Plan) Membres() []string {
	return append(append([]string{}, p.Profs...), p.Eleves...)
}

// nomPartage extrait le dernier segment d'un chemin UNC.
func nomPartage(chemin string) string {
	tronque := strings.TrimRight(chemin, `\`)
	if i := strings.LastIndex(tronque, `\`); i >= 0 {
		return tronque[i+1:]
	}
	return tronque
}

// ClasserMembre retourne le rôle d'un membre (« prof » ou « eleve ») selon le
// groupe des profs puis, à défaut, l'OU des profs.
func ClasserMembre(dn string, groupes []string, cfg Config) string {
	if cfg.GroupeProfs != "" {
		for _, g := range groupes {
			if g == cfg.GroupeProfs {
				return RoleProf
			}
		}
	}
	if cfg.OUProfs != "" {
		ou := strings.ToLower(strings.TrimSpace(cfg.OUProfs))
		if strings.HasSuffix(strings.ToLower(strings.TrimSpace(dn)), ","+ou) {
			return RoleProf
		}
	}
	return RoleEleve
}

// Diff décrit l'écart entre l'état enregistré et l'appartenance AD actuelle.
type Diff struct {
	Ajoutes   []string
	Retires   []string
	Inchanges []string
}

// PlanifierSync calcule l'écart entre deux ensembles de membres.
func PlanifierSync(precedents, actuels []string) Diff {
	prev, curr := map[string]bool{}, map[string]bool{}
	for _, m := range precedents {
		prev[m] = true
	}
	for _, m := range actuels {
		curr[m] = true
	}

	ajoutes, retires, inchanges := []string{}, []string{}, []string{}
	for m := range curr {
		if prev[m] {
			inchanges = append(inchanges, m)
		} else {
			ajoutes = append(ajoutes, m)
		}
	}
	for m := range prev {
		if !curr[m] {
			retires = append(retires, m)
		}
	}
	sort.Strings(ajoutes)
	sort.Strings(retires)
	sort.Strings(inchanges)
	return Diff{Ajoutes: ajoutes, Retires: retires, Inchanges: inchanges}
}

// psQuote échappe les apostrophes d'une valeur PowerShell.
func psQuote(valeur string) string {
	return strings.ReplaceAll(valeur, "'", "''")
}

// ScriptCreation génère le script PowerShell de dossier + partage SMB + droits
// NTFS pour une liste de plans. Le domaine, s'il est fourni, préfixe les
// identifiants des ACE (lycee.local\jdurand).
func ScriptCreation(plans []Plan, domaine string) string {
	prefix := ""
	if domaine != "" {
		prefix = domaine + `\`
	}

	var b strings.Builder
	b.WriteString("#Requires -RunAsAdministrator\n")
	b.WriteString("<#\n")
	b.WriteString(" Script généré par EduSync-AD — Espaces partagés par classe (M18)\n")
	fmt.Fprintf(&b, " Espaces : %d\n", len(plans))
	b.WriteString(" Droits : profs Modification, élèves Lecture, Administrateurs/SYSTEM Total\n")
	b.WriteString("#>\n")
	b.WriteString("$ErrorActionPreference = 'Stop'\n\n")

	for _, plan := range plans {
		path := psQuote(plan.Path)
		partage := psQuote(nomPartage(plan.Path))
		fmt.Fprintf(&b, "$path = '%s'\n", path)
		b.WriteString("if (-not (Test-Path -LiteralPath $path)) {\n")
		b.WriteString("    New-Item -Path $path -ItemType Directory -Force | Out-Null\n")
		b.WriteString("}\n")
		fmt.Fprintf(&b, "if (-not (Get-SmbShare -Name '%s' -ErrorAction SilentlyContinue)) {\n", partage)
		fmt.Fprintf(&b, "    New-SmbShare -Name '%s' -Path $path -FullAccess '%s' | Out-Null\n", partage, AdminsPrincipaux[0])
		b.WriteString("}\n")
		fmt.Fprintf(&b, "icacls $path /grant:r '%s:(OI)(CI)%s' '%s:(OI)(CI)%s' /Q\n",
			AdminsPrincipaux[0], DroitsAdmins, AdminsPrincipaux[1], DroitsAdmins)

		for _, sam := range plan.Profs {
			principal := sam
			if prefix != "" {
				principal = prefix + sam
			}
			fmt.Fprintf(&b, "icacls $path /grant:r '%s:(OI)(CI)%s' /Q\n", psQuote(principal), DroitsProfs)
		}
		for _, sam := range plan.Eleves {
			principal := sam
			if prefix != "" {
				principal = prefix + sam
			}
			fmt.Fprintf(&b, "icacls $path /grant:r '%s:(OI)(CI)%s' /Q\n", psQuote(principal), DroitsEleves)
		}
		b.WriteString("\n")
	}
	b.WriteString("Write-Host 'Espaces de classe créés / droits appliqués.'\n")
	return b.String()
}

// ScriptSync génère le script PowerShell de synchronisation membres AD ↔ droits
// NTFS. Le script interroge AD en direct (Get-ADGroupMember), classe prof/élève
// puis reconstruit les ACE explicites du dossier : les utilisateurs retirés du
// groupe perdent leurs droits, les nouveaux reçoivent le leur. Les ACE hérités
// et ceux des comptes d'administration ne sont jamais touchés.
func ScriptSync(plan Plan, domaine, groupeProfs, ouProfs string) string {
	path := psQuote(plan.Path)
	groupID := psQuote(plan.GroupDN)
	if groupID == "" {
		groupID = psQuote(plan.GroupName)
	}
	tg := psQuote(groupeProfs)
	tou := psQuote(strings.ToLower(ouProfs))

	var b strings.Builder
	b.WriteString("#Requires -RunAsAdministrator\n")
	b.WriteString("<#\n")
	b.WriteString(" Script généré par EduSync-AD — Synchronisation AD ↔ droits NTFS (M18)\n")
	fmt.Fprintf(&b, " Groupe : %s\n", plan.GroupName)
	fmt.Fprintf(&b, " Espace : %s\n", plan.Path)
	b.WriteString(" Membres AD : profs Modification, élèves Lecture ;\n")
	b.WriteString(" les utilisateurs retirés du groupe perdent leurs droits explicites.\n")
	b.WriteString("#>\n")
	b.WriteString("$ErrorActionPreference = 'Stop'\n")
	b.WriteString("Import-Module ActiveDirectory\n")
	fmt.Fprintf(&b, "$path = '%s'\n", path)
	b.WriteString("if (-not (Test-Path -LiteralPath $path)) {\n")
	b.WriteString("    New-Item -Path $path -ItemType Directory -Force | Out-Null\n")
	b.WriteString("}\n\n")

	b.WriteString("# 1. Membres actuels du groupe AD\n")
	fmt.Fprintf(&b, "$group = '%s'\n", groupID)
	b.WriteString("$members = @(Get-ADGroupMember -Identity $group -Recursive | Where-Object { $_.objectClass -eq 'user' })\n\n")

	b.WriteString("# 2. Classement profs / élèves → droits attendus par SID\n")
	b.WriteString("$desired = @{}\n")
	if tg != "" {
		fmt.Fprintf(&b, "$teachersGroup = '%s'\n", tg)
	} else {
		b.WriteString("$teachersGroup = $null\n")
	}
	if tou != "" {
		fmt.Fprintf(&b, "$teachersOU = '%s'\n", tou)
	} else {
		b.WriteString("$teachersOU = $null\n")
	}
	b.WriteString("$teacherSids = @()\n")
	b.WriteString("if ($teachersGroup) {\n")
	b.WriteString("    $teacherSids = @(Get-ADGroupMember -Identity $teachersGroup -Recursive | Select-Object -ExpandProperty SID | ForEach-Object { $_.Value })\n")
	b.WriteString("}\n")
	b.WriteString("foreach ($m in $members) {\n")
	b.WriteString("    $isTeacher = $teacherSids -contains $m.SID.Value\n")
	b.WriteString("    if (-not $isTeacher -and $teachersOU) {\n")
	b.WriteString("        $isTeacher = $m.DistinguishedName -like ('*' + $teachersOU)\n")
	b.WriteString("    }\n")
	b.WriteString("    $rights = if ($isTeacher) { 'Modify' } else { 'ReadAndExecute' }\n")
	b.WriteString("    $desired[$m.SID.Value] = $rights\n")
	b.WriteString("}\n\n")

	b.WriteString("# 3. Comptes protégés (SID ou nom) : jamais modifiés\n")
	b.WriteString("$protectedSids = @('S-1-5-18', 'S-1-5-32-544')\n")
	b.WriteString("if ($teachersGroup) { $protectedSids += (Get-ADGroup -Identity $teachersGroup).SID.Value }\n\n")

	b.WriteString("# 4. Reconstruction des ACE explicites (hérités intacts)\n")
	b.WriteString("$acl = Get-Acl -LiteralPath $path\n")
	b.WriteString("foreach ($rule in @($acl.Access)) {\n")
	b.WriteString("    if ($rule.IsInherited) { continue }\n")
	b.WriteString("    $sid = $null\n")
	b.WriteString("    try {\n")
	b.WriteString("        $sid = $rule.IdentityReference.Translate([System.Security.Principal.SecurityIdentifier]).Value\n")
	b.WriteString("    } catch { continue }\n")
	b.WriteString("    if ($protectedSids -contains $sid) { continue }\n")
	b.WriteString("    if ($rule.IdentityReference.Value -match 'Admin|SYSTEM|CREATOR OWNER') { continue }\n")
	b.WriteString("    $acl.RemoveAccessRule($rule) | Out-Null\n")
	b.WriteString("}\n")
	b.WriteString("foreach ($sid in $desired.Keys) {\n")
	b.WriteString("    $ace = New-Object System.Security.AccessControl.FileSystemAccessRule(\n")
	b.WriteString("        ([System.Security.Principal.SecurityIdentifier]::new($sid)),\n")
	b.WriteString("        $desired[$sid], 'ContainerInherit,ObjectInherit', 'None', 'Allow')\n")
	b.WriteString("    $acl.AddAccessRule($ace) | Out-Null\n")
	b.WriteString("}\n")
	b.WriteString("Set-Acl -LiteralPath $path -AclObject $acl\n")
	b.WriteString("Write-Host \"Droits synchronisés : $path ($($desired.Count) membre(s))\"\n")
	return b.String()
}

// EtatEspace décrit l'état de synchronisation d'un espace.
type EtatEspace struct {
	Path          string   `json:"path"`
	GroupName     string   `json:"group_name"`
	Pending       bool     `json:"pending"`
	Members       []string `json:"members"`
	FolderCreated bool     `json:"folder_created"`
}

// DossierConfig retourne le dossier de configuration partagé avec Python.
func DossierConfig() (string, error) {
	return config.ConfigDir()
}

// ChargerConfig lit la configuration depuis un dossier. Fichier absent ou
// corrompu : configuration par défaut.
func ChargerConfig(dir string) Config {
	donnees, err := os.ReadFile(filepath.Join(dir, FichierConfig))
	if err != nil {
		return ConfigParDefaut()
	}
	cfg := ConfigParDefaut()
	if err := json.Unmarshal(donnees, &cfg); err != nil {
		return ConfigParDefaut()
	}
	return cfg
}

// EnregistrerConfig écrit la configuration dans un dossier.
func EnregistrerConfig(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("classspaces: création dossier config: %w", err)
	}
	return ecrireJSON(filepath.Join(dir, FichierConfig), cfg)
}

// ChargerEtat lit l'état de synchronisation des espaces.
func ChargerEtat(dir string) map[string]EtatEspace {
	etat := map[string]EtatEspace{}
	donnees, err := os.ReadFile(filepath.Join(dir, FichierEtat))
	if err != nil {
		return etat
	}
	if err := json.Unmarshal(donnees, &etat); err != nil {
		return map[string]EtatEspace{}
	}
	return etat
}

// EnregistrerEtat écrit l'état de synchronisation des espaces.
func EnregistrerEtat(dir string, etat map[string]EtatEspace) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("classspaces: création dossier config: %w", err)
	}
	return ecrireJSON(filepath.Join(dir, FichierEtat), etat)
}

// ecrireJSON encode en JSON indenté (2 espaces), sans échappement HTML, comme
// la version Python (indent=2, ensure_ascii=False).
func ecrireJSON(dest string, valeur any) error {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(valeur); err != nil {
		return fmt.Errorf("classspaces: encodage JSON: %w", err)
	}
	// json.Encoder ajoute un saut de ligne final : identique à Python.
	if err := os.WriteFile(dest, []byte(b.String()), 0o600); err != nil {
		return fmt.Errorf("classspaces: écriture %s: %w", dest, err)
	}
	return nil
}

// NomGroupeDepuisDN extrait le nom (CN) d'un DN de groupe.
func NomGroupeDepuisDN(dn string) string {
	if i := strings.Index(dn, ","); i >= 0 {
		part := dn[:i]
		if j := strings.Index(part, "="); j >= 0 {
			return part[j+1:]
		}
		return part
	}
	return dn
}

// NotifierCreationGroupe enregistre un espace comme en attente à la création
// d'un groupe de classe et crée le dossier si le chemin est accessible (Windows).
// Elle retourne le chemin prévu et true si l'option est active, ("", false) sinon.
func NotifierCreationGroupe(dir, groupDN, groupName string) (string, bool) {
	cfg := ChargerConfig(dir)
	if !cfg.AutoSurCreation {
		return "", false
	}
	nom := groupName
	if nom == "" {
		nom = NomGroupeDepuisDN(groupDN)
	}
	path := cfg.ResolvePath(nom)

	etat := ChargerEtat(dir)
	entree := etat[groupDN]
	entree.Path = path
	entree.GroupName = nom
	entree.Pending = true
	if entree.Members == nil {
		entree.Members = []string{}
	}
	if runtime.GOOS == "windows" {
		if err := os.MkdirAll(path, 0o755); err == nil {
			entree.FolderCreated = true
		}
	}
	etat[groupDN] = entree
	_ = EnregistrerEtat(dir, etat)
	return path, true
}

// MarquerSynchronise enregistre l'état de synchronisation d'un espace.
func MarquerSynchronise(dir, groupDN string, membres []string) error {
	etat := ChargerEtat(dir)
	entree := etat[groupDN]
	entree.Members = append([]string{}, membres...)
	sort.Strings(entree.Members)
	entree.Pending = false
	etat[groupDN] = entree
	return EnregistrerEtat(dir, etat)
}
