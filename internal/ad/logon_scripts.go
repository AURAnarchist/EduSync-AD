// Package ad fournit la gestion des scripts logon/logoff selon M15 de la
// roadmap.
//
// Chaque opération suit le pattern : Validate() → Simulate() → Execute() → Audit()
// (ROADMAP_GO_MIGRATION.md §3). Le mode simulation est le chemin par défaut.
//
// Les scripts sont stockés dans l'attribut scriptPath de l'utilisateur et
// supportent les variables %USERNAME%, %FULLNAME%, %OU%, %GROUP%, %EMAIL%,
// %HOMEDIR%.
package ad

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// ScriptType définit le type de script supporté.
type ScriptType string

const (
	// ScriptTypeBat script batch Windows.
	ScriptTypeBat ScriptType = "bat"
	// ScriptTypePowerShell script PowerShell.
	ScriptTypePowerShell ScriptType = "powershell"
	// ScriptTypeVBS script VBScript.
	ScriptTypeVBS ScriptType = "vbs"
)

// Valide vérifie que le type de script est connu.
func (t ScriptType) Valide() bool {
	switch t {
	case ScriptTypeBat, ScriptTypePowerShell, ScriptTypeVBS:
		return true
	}
	return false
}

// Sentinelles d'erreur pour les scripts logon/logoff.
// ErrUserDNManquant est définie dans profiles.go (même package).
var (
	// ErrScriptTypeInvalide est retournée quand le type de script est inconnu.
	ErrScriptTypeInvalide = errors.New("ad: type de script invalide")
	// ErrScriptContenuVide est retournée quand le contenu du script est vide.
	ErrScriptContenuVide = errors.New("ad: contenu du script obligatoire")
)

// ScriptParams décrit les paramètres de définition d'un script logon/logoff.
type ScriptParams struct {
	UserDN  string     // DN de l'utilisateur cible
	Type    ScriptType // type de script (bat, powershell, vbs)
	Contenu string     // contenu du script
}

// Validate vérifie la cohérence des paramètres.
func (p *ScriptParams) Validate() error {
	if p.UserDN == "" {
		return ErrUserDNManquant
	}
	if !p.Type.Valide() {
		return ErrScriptTypeInvalide
	}
	if strings.TrimSpace(p.Contenu) == "" {
		return ErrScriptContenuVide
	}
	return nil
}

// ScriptResult décrit le résultat d'une opération de script.
type ScriptResult struct {
	UserDN   string     // DN de l'utilisateur
	Type     ScriptType // type appliqué
	Contenu  string     // contenu après résolution des variables
	Statut   string     // "succes" | "echec" | "skip"
	Detaille string     // détail / message d'erreur
}

// resoudreVariablesScript remplace les variables %USERNAME%, %FULLNAME%, %OU%,
// %GROUP%, %EMAIL%, %HOMEDIR% par leurs valeurs.
func resoudreVariablesScript(contenu string, u *User) string {
	contenu = strings.ReplaceAll(contenu, "%USERNAME%", u.SAMAccountName)
	contenu = strings.ReplaceAll(contenu, "%FULLNAME%", u.DisplayName)
	contenu = strings.ReplaceAll(contenu, "%OU%", extraireNomOU(u.OU))
	contenu = strings.ReplaceAll(contenu, "%EMAIL%", u.Email)
	contenu = strings.ReplaceAll(contenu, "%HOMEDIR%", repertoirePerso(u))

	// %GROUP% : premier groupe de classe si disponible.
	groupe := ""
	if len(u.MemberOf) > 0 {
		groupe = extraireNomCN(u.MemberOf[0])
	}
	contenu = strings.ReplaceAll(contenu, "%GROUP%", groupe)

	return contenu
}

// extraireNomCN extrait le CN depuis un DN de groupe.
func extraireNomCN(dn string) string {
	parts := strings.Split(dn, ",")
	if len(parts) > 0 {
		premiere := strings.TrimSpace(parts[0])
		if strings.HasPrefix(strings.ToUpper(premiere), "CN=") {
			return strings.TrimPrefix(premiere, "CN=")
		}
	}
	return ""
}

// repertoirePerso retourne le dossier personnel par défaut.
// Format : \\srv\homes\%USERNAME%
func repertoirePerso(u *User) string {
	return "\\\\srv\\homes\\" + u.SAMAccountName
}

// ScriptSimulate simule l'application d'un script (vérifie l'existence,
// résout les variables, ne modifie pas l'AD).
func (p *Pool) ScriptSimulate(ctx context.Context, params ScriptParams) (*ScriptResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	u, err := p.UserGet(ctx, params.UserDN)
	if err != nil {
		return &ScriptResult{
			UserDN:   params.UserDN,
			Type:     params.Type,
			Statut:   "echec",
			Detaille: fmt.Sprintf("utilisateur introuvable: %v", err),
		}, nil
	}

	contenuResolve := resoudreVariablesScript(params.Contenu, u)

	return &ScriptResult{
		UserDN:   u.DN,
		Type:     params.Type,
		Contenu:  contenuResolve,
		Statut:   "succes",
		Detaille: fmt.Sprintf("script %s à appliquer pour %s", params.Type, u.SAMAccountName),
	}, nil
}

// ScriptExecute applique réellement le script (écrit l'attribut scriptPath).
func (p *Pool) ScriptExecute(ctx context.Context, params ScriptParams) (*ScriptResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.ScriptSimulate(ctx, params)
	if err != nil {
		return nil, err
	}
	if sim.Statut == "echec" {
		return sim, fmt.Errorf("simulation échouée: %s", sim.Detaille)
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	modReq := ldap.NewModifyRequest(params.UserDN, nil)
	modReq.Replace("scriptPath", []string{sim.Contenu})
	if err := conn.Modify(modReq); err != nil {
		return &ScriptResult{
			UserDN:   sim.UserDN,
			Type:     params.Type,
			Contenu:  sim.Contenu,
			Statut:   "echec",
			Detaille: fmt.Sprintf("écriture scriptPath: %v", err),
		}, nil
	}

	sim.Statut = "succes"
	sim.Detaille = fmt.Sprintf("script %s appliqué sur %s", params.Type, sim.UserDN)
	return sim, nil
}

// ScriptBulkParams décrit une opération de scripts en masse.
type ScriptBulkParams struct {
	UserDNs []string   // DN des utilisateurs (vide = tous de l'OU)
	OU      string     // OU pour sélectionner tous les utilisateurs
	Type    ScriptType // type de script
	Contenu string     // contenu du script
}

// Validate vérifie les paramètres en masse.
func (p *ScriptBulkParams) Validate() error {
	if len(p.UserDNs) == 0 && p.OU == "" {
		return errors.New("ad: au moins un user DN ou une OU est obligatoire")
	}
	if !p.Type.Valide() {
		return errors.New("ad: type de script invalide")
	}
	if strings.TrimSpace(p.Contenu) == "" {
		return errors.New("ad: contenu du script obligatoire")
	}
	return nil
}

// ScriptBulkExecute applique le script à plusieurs utilisateurs.
func (p *Pool) ScriptBulkExecute(ctx context.Context, params ScriptBulkParams) ([]ScriptResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	var users []*User
	var err error

	if len(params.UserDNs) > 0 {
		for _, dn := range params.UserDNs {
			u, e := p.UserGet(ctx, dn)
			if e == nil {
				users = append(users, u)
			}
		}
	} else {
		users, err = p.UserSearch(ctx, params.OU,
			"(&(objectClass=user)(objectCategory=person))",
			[]string{"dn", "sAMAccountName", "displayName"}, 0)
		if err != nil {
			return nil, fmt.Errorf("recherche utilisateurs dans %s: %w", params.OU, err)
		}
	}

	results := make([]ScriptResult, 0, len(users))
	for _, u := range users {
		simParams := ScriptParams{
			UserDN:  u.DN,
			Type:    params.Type,
			Contenu: params.Contenu,
		}
		result, err := p.ScriptExecute(ctx, simParams)
		if err != nil {
			result = &ScriptResult{
				UserDN:   u.DN,
				Type:     params.Type,
				Contenu:  params.Contenu,
				Statut:   "echec",
				Detaille: err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

// extensionScript retourne l'extension de fichier pour un type de script.
func extensionScript(t ScriptType) string {
	switch t {
	case ScriptTypeBat:
		return ".bat"
	case ScriptTypePowerShell:
		return ".ps1"
	case ScriptTypeVBS:
		return ".vbs"
	}
	return ""
}

var _ = ldap.EscapeFilter // conservé pour cohérence avec les autres fichiers du paquet
