// Package ad fournit la gestion des dossiers personnels (Home Directory)
// selon M17 de la roadmap.
//
// Chaque operation suit le pattern : Validate() → Simulate() → Execute() → Audit()
// (ROADMAP_GO_MIGRATION.md §3). Le mode simulation est le chemin par défaut.
//
// Le dossier personnel est stocke dans les attributs LDAP homeDirectory
// (chemin UNC) et homeDrive (lettre de lecteur).
package ad

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// HomeDirType definit le type de dossier personnel.
type HomeDirType string

const (
	// HomeDirTypeStandard dossier personnel standard.
	HomeDirTypeStandard HomeDirType = "standard"
	// HomeDirTypePersonnalise dossier personnel personnalise.
	HomeDirTypePersonnalise HomeDirType = "personnalise"
)

// Valide verifie que le type de dossier personnel est connu.
func (t HomeDirType) Valide() bool {
	switch t {
	case HomeDirTypeStandard, HomeDirTypePersonnalise:
		return true
	}
	return false
}

// Sentinelles d'erreur pour les dossiers personnels.
// ErrUserDNManquant est definie dans profiles.go (meme paquet).
var (
	// ErrHomeDirTypeInvalide est retournee quand le type de dossier est inconnu.
	ErrHomeDirTypeInvalide = errors.New("ad: type de dossier personnel invalide")
	// ErrCheminHomeDirManquant est retournee quand le chemin est obligatoire mais absent.
	ErrCheminHomeDirManquant = errors.New("ad: chemin de dossier personnel obligatoire")
	// ErrLettreLecteurInvalide est retournee quand la lettre de lecteur est invalide.
	ErrLettreLecteurInvalide = errors.New("ad: lettre de lecteur invalide")
)

// HomeDirParams decrit les parametres de definition d'un dossier personnel.
type HomeDirParams struct {
	UserDN        string      // DN de l'utilisateur cible
	Type          HomeDirType // type de dossier (standard, personnalise)
	Chemin        string      // chemin UNC (ex: \\srv\homes\%USERNAME%)
	LettreLecteur string      // lettre de lecteur (ex: H)
}

// Validate verifie la cohérence des parametres.
func (p *HomeDirParams) Validate() error {
	if p.UserDN == "" {
		return ErrUserDNManquant
	}
	if !p.Type.Valide() {
		return ErrHomeDirTypeInvalide
	}
	if p.Chemin == "" {
		return ErrCheminHomeDirManquant
	}
	if p.LettreLecteur != "" && len(p.LettreLecteur) != 1 {
		return ErrLettreLecteurInvalide
	}
	return nil
}

// HomeDirResult decrit le resultat d'une operation de dossier personnel.
type HomeDirResult struct {
	UserDN         string      // DN de l'utilisateur
	SAMAccountName string      // identifiant de l'utilisateur
	Type           HomeDirType // type applique
	Chemin         string      // chemin resolu
	LettreLecteur  string      // lettre de lecteur
	Statut         string      // "succes" | "echec" | "skip"
	Detaille       string      // detail / message d'erreur
}

// resoudreVariablesHomeDir remplace les variables %USERNAME%, %SAM%, %FULLNAME%
// par leurs valeurs.
func resoudreVariablesHomeDir(chemin string, u *User) string {
	chemin = strings.ReplaceAll(chemin, "%USERNAME%", u.SAMAccountName)
	chemin = strings.ReplaceAll(chemin, "%SAM%", u.SAMAccountName)
	chemin = strings.ReplaceAll(chemin, "%FULLNAME%", u.DisplayName)
	return chemin
}

// HomeDirSimulate simule l'application d'un dossier personnel (verifie
// l'existence, resout les variables, ne modifie pas l'AD).
func (p *Pool) HomeDirSimulate(ctx context.Context, params HomeDirParams) (*HomeDirResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	u, err := p.UserGet(ctx, params.UserDN)
	if err != nil {
		return &HomeDirResult{
			UserDN:   params.UserDN,
			Type:     params.Type,
			Statut:   "echec",
			Detaille: fmt.Sprintf("utilisateur introuvable: %v", err),
		}, nil
	}

	cheminResolve := resoudreVariablesHomeDir(params.Chemin, u)

	return &HomeDirResult{
		UserDN:         u.DN,
		SAMAccountName: u.SAMAccountName,
		Type:           params.Type,
		Chemin:         cheminResolve,
		LettreLecteur:  params.LettreLecteur,
		Statut:         "succes",
		Detaille:       fmt.Sprintf("dossier personnel a appliquer pour %s (%s)", u.SAMAccountName, cheminResolve),
	}, nil
}

// HomeDirExecute applique vraiment le dossier personnel (ecrit homeDirectory
// et homeDrive sur l'utilisateur).
func (p *Pool) HomeDirExecute(ctx context.Context, params HomeDirParams) (*HomeDirResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.HomeDirSimulate(ctx, params)
	if err != nil {
		return nil, err
	}
	if sim.Statut == "echec" {
		return sim, fmt.Errorf("simulation echouee: %s", sim.Detaille)
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	modReq := ldap.NewModifyRequest(params.UserDN, nil)
	modReq.Replace("homeDirectory", []string{sim.Chemin})
	if err := conn.Modify(modReq); err != nil {
		return &HomeDirResult{
			UserDN:         sim.UserDN,
			SAMAccountName: sim.SAMAccountName,
			Type:           params.Type,
			Chemin:         sim.Chemin,
			LettreLecteur:  params.LettreLecteur,
			Statut:         "echec",
			Detaille:       fmt.Sprintf("ecriture homeDirectory: %v", err),
		}, nil
	}

	// Lettre de lecteur (homeDrive) si fournie.
	if params.LettreLecteur != "" {
		modReq = ldap.NewModifyRequest(params.UserDN, nil)
		modReq.Replace("homeDrive", []string{params.LettreLecteur + ":"})
		if err := conn.Modify(modReq); err != nil {
			return &HomeDirResult{
				UserDN:         sim.UserDN,
				SAMAccountName: sim.SAMAccountName,
				Type:           params.Type,
				Chemin:         sim.Chemin,
				LettreLecteur:  params.LettreLecteur,
				Statut:         "echec",
				Detaille:       fmt.Sprintf("ecriture homeDrive: %v", err),
			}, nil
		}
	}

	sim.Statut = "succes"
	sim.Detaille = fmt.Sprintf("dossier personnel applique pour %s (%s, lecteur %s)",
		sim.SAMAccountName, sim.Chemin, params.LettreLecteur)
	return sim, nil
}

// HomeDirBulkParams decrit une operation de dossiers personnels en masse.
type HomeDirBulkParams struct {
	UserDNs       []string    // DN des utilisateurs (vide = tous de l'OU)
	OU            string      // OU pour selectionner tous les utilisateurs
	Type          HomeDirType // type de dossier
	Chemin        string      // chemin UNC
	LettreLecteur string      // lettre de lecteur
}

// Validate verifie les parametres en masse.
func (p *HomeDirBulkParams) Validate() error {
	if len(p.UserDNs) == 0 && p.OU == "" {
		return errors.New("ad: au moins un user DN ou une OU est obligatoire")
	}
	if !p.Type.Valide() {
		return ErrHomeDirTypeInvalide
	}
	if p.Chemin == "" {
		return ErrCheminHomeDirManquant
	}
	return nil
}

// HomeDirBulkExecute applique le dossier personnel a plusieurs utilisateurs.
func (p *Pool) HomeDirBulkExecute(ctx context.Context, params HomeDirBulkParams) ([]HomeDirResult, error) {
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

	results := make([]HomeDirResult, 0, len(users))
	for _, u := range users {
		simParams := HomeDirParams{
			UserDN:        u.DN,
			Type:          params.Type,
			Chemin:        params.Chemin,
			LettreLecteur: params.LettreLecteur,
		}
		result, err := p.HomeDirExecute(ctx, simParams)
		if err != nil {
			result = &HomeDirResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				Type:           params.Type,
				Chemin:         params.Chemin,
				LettreLecteur:  params.LettreLecteur,
				Statut:         "echec",
				Detaille:       err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

var _ = ldap.EscapeFilter // conserve pour cohérence avec les autres fichiers du paquet
