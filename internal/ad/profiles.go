// Package ad fournit la gestion des profils utilisateurs (itinérant, local,
// obligatoire) selon M13 de la roadmap.
//
// Chaque opération suit le pattern : Validate() → Simulate() → Execute() → Audit()
// (ROADMAP_GO_MIGRATION.md §3). Le mode simulation est le chemin par défaut.
package ad

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// ProfileType définit le type de profil utilisateur.
type ProfileType string

const (
	// ProfileTypeItinerant : profil itinérant stocké sur un serveur.
	ProfileTypeItinerant ProfileType = "itinérant"
	// ProfileTypeLocal : profil stocké localement sur la machine.
	ProfileTypeLocal ProfileType = "local"
	// ProfileTypeObligatoire : profil obligatoire (.man), non modifiable.
	ProfileTypeObligatoire ProfileType = "obligatoire"
)

// Valide vérifie que le type de profil est connu.
func (t ProfileType) Valide() bool {
	switch t {
	case ProfileTypeItinerant, ProfileTypeLocal, ProfileTypeObligatoire:
		return true
	}
	return false
}

// Sentinelles d'erreur pour les profils.
var (
	// ErrUserDNManquant est retournée quand le DN de l'utilisateur est absent.
	ErrUserDNManquant = errors.New("ad: DN de l'utilisateur obligatoire pour le profil")
	// ErrProfileTypeInvalide est retournée quand le type de profil est inconnu.
	ErrProfileTypeInvalide = errors.New("ad: type de profil invalide")
	// ErrCheminProfilManquant est retournée quand le chemin est obligatoire mais absent.
	ErrCheminProfilManquant = errors.New("ad: chemin de profil obligatoire pour ce type")
)

// ProfileParams décrit les paramètres de définition d'un profil utilisateur.
type ProfileParams struct {
	UserDN string      // DN de l'utilisateur cible
	Type   ProfileType // type de profil (itinérant, local, obligatoire)
	Chemin string      // chemin du profil (ex: \\srv\profil\%USERNAME%)
}

// Validate vérifie la cohérence des paramètres.
func (p *ProfileParams) Validate() error {
	if p.UserDN == "" {
		return ErrUserDNManquant
	}
	if !p.Type.Valide() {
		return ErrProfileTypeInvalide
	}
	// Le chemin est obligatoire pour itinérant et obligatoire.
	if (p.Type == ProfileTypeItinerant || p.Type == ProfileTypeObligatoire) && p.Chemin == "" {
		return ErrCheminProfilManquant
	}
	return nil
}

// ProfileResult décrit le résultat d'une opération de profil.
type ProfileResult struct {
	UserDN         string      // DN de l'utilisateur
	SAMAccountName string      // identifiant de l'utilisateur
	Type           ProfileType // type appliqué
	CheminResolve  string      // chemin après résolution des variables
	Statut         string      // "succes" | "echec" | "skip"
	Detaille       string      // détail / message d'erreur
}

// resoudreVariables remplace %USERNAME%, %SAM% et %OU% par leurs valeurs.
func resoudreVariables(chemin, samAccountName, ouNom string) string {
	chemin = strings.ReplaceAll(chemin, "%USERNAME%", samAccountName)
	chemin = strings.ReplaceAll(chemin, "%SAM%", samAccountName)
	chemin = strings.ReplaceAll(chemin, "%OU%", ouNom)
	return chemin
}

// extraireNomOU extrait le nom de l'OU depuis un DN (ex: "CN=X,OU=Eleves,DC=..." → "Eleves").
func extraireNomOU(dn string) string {
	parts := strings.Split(dn, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(strings.ToUpper(part), "OU=") {
			return strings.TrimPrefix(part, "OU=")
		}
	}
	return ""
}

// ProfileSimulate simule l'application d'un profil (vérifie l'existence, résout les variables).
func (p *Pool) ProfileSimulate(ctx context.Context, params ProfileParams) (*ProfileResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	u, err := p.UserGet(ctx, params.UserDN)
	if err != nil {
		return &ProfileResult{
			UserDN:   params.UserDN,
			Type:     params.Type,
			Statut:   "echec",
			Detaille: fmt.Sprintf("utilisateur introuvable: %v", err),
		}, nil
	}

	cheminResolve := resoudreVariables(params.Chemin, u.SAMAccountName, extraireNomOU(u.DN))

	return &ProfileResult{
		UserDN:         u.DN,
		SAMAccountName: u.SAMAccountName,
		Type:           params.Type,
		CheminResolve:  cheminResolve,
		Statut:         "succes",
		Detaille:       fmt.Sprintf("profil %s à appliquer: %s", params.Type, cheminResolve),
	}, nil
}

// ProfileExecute applique réellement le profil (écrit l'attribut profilePath sur l'utilisateur).
func (p *Pool) ProfileExecute(ctx context.Context, params ProfileParams) (*ProfileResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.ProfileSimulate(ctx, params)
	if err != nil {
		return nil, err
	}
	if sim.Statut == "echec" {
		return sim, fmt.Errorf("simulation échouée: %s", sim.Detaille)
	}

	// Déterminer la valeur à écrire sur l'attribut profilePath.
	var profilePath string
	switch params.Type {
	case ProfileTypeLocal:
		profilePath = "" // profil local : pas de chemin de profil
	case ProfileTypeObligatoire:
		// Le chemin doit se terminer par .man pour être obligatoire.
		if !strings.HasSuffix(strings.ToLower(sim.CheminResolve), ".man") {
			sim.CheminResolve += ".man"
		}
		profilePath = sim.CheminResolve
	default: // ProfileTypeItinerant
		profilePath = sim.CheminResolve
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	modReq := ldap.NewModifyRequest(params.UserDN, nil)
	modReq.Replace("profilePath", []string{profilePath})
	if err := conn.Modify(modReq); err != nil {
		return &ProfileResult{
			UserDN:         params.UserDN,
			SAMAccountName: sim.SAMAccountName,
			Type:           params.Type,
			CheminResolve:  sim.CheminResolve,
			Statut:         "echec",
			Detaille:       fmt.Sprintf("écriture profilePath: %v", err),
		}, nil
	}

	sim.Statut = "succes"
	sim.Detaille = fmt.Sprintf("profil %s appliqué: %s", params.Type, profilePath)
	return sim, nil
}

// ProfileBulkParams décrit une opération de profils en masse (par OU ou liste de DNs).
type ProfileBulkParams struct {
	UserDNs []string // DN des utilisateurs (vide = tous de l'OU)
	OU      string   // OU pour sélectionner tous les utilisateurs
	Type    ProfileType
	Chemin  string
}

// Validate vérifie les paramètres en masse.
func (p *ProfileBulkParams) Validate() error {
	if len(p.UserDNs) == 0 && p.OU == "" {
		return errors.New("ad: au moins un user DN ou une OU est obligatoire")
	}
	if !p.Type.Valide() {
		return errors.New("ad: type de profil invalide")
	}
	if (p.Type == ProfileTypeItinerant || p.Type == ProfileTypeObligatoire) && p.Chemin == "" {
		return errors.New("ad: chemin de profil obligatoire pour ce type")
	}
	return nil
}

// ProfileBulkExecute applique le profil à plusieurs utilisateurs.
func (p *Pool) ProfileBulkExecute(ctx context.Context, params ProfileBulkParams) ([]ProfileResult, error) {
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

	results := make([]ProfileResult, 0, len(users))
	for _, u := range users {
		simParams := ProfileParams{
			UserDN: u.DN,
			Type:   params.Type,
			Chemin: params.Chemin,
		}
		result, err := p.ProfileExecute(ctx, simParams)
		if err != nil {
			result = &ProfileResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				Type:           params.Type,
				Statut:         "echec",
				Detaille:       err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

var _ = ldap.EscapeFilter // conservé pour cohérence avec les autres fichiers du paquet
