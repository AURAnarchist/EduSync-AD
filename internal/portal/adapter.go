package portal

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/go-ldap/ldap/v3"
)

// errAnnuaireAbsent est retournée quand aucun pool LDAP n'est fourni.
var errAnnuaireAbsent = errors.New("portal: aucun annuaire configuré")

// attributsUtilisateur liste les attributs lus pour identifier un compte
// (mêmes attributs que `core/portal.py::AdDirectory`).
var attributsUtilisateur = []string{
	"cn", "sAMAccountName", "displayName", "mail", "userAccountControl",
}

// PoolAnnuaire adapte un pool LDAP du paquet ad à l'interface Annuaire.
//
// Toutes les écritures passent par le pool : le garde-fou RBAC
// (`Pool.SetPolicy`) s'applique donc automatiquement, comme pour les autres
// modules. C'est l'annuaire injecté en production ; les tests utilisent une
// implémentation en mémoire.
type PoolAnnuaire struct {
	// Pool est le pool LDAP partagé avec l'application.
	Pool *ad.Pool
	// BaseDN restreint les recherches ; vide = racine configurée du pool.
	BaseDN string
	// Contexte est le contexte des appels LDAP ; nil = context.Background().
	Contexte context.Context
}

// contexte retourne le contexte à utiliser.
func (a *PoolAnnuaire) contexte() context.Context {
	if a.Contexte != nil {
		return a.Contexte
	}
	return context.Background()
}

// racine retourne la base de recherche.
func (a *PoolAnnuaire) racine() string {
	if a.BaseDN != "" {
		return a.BaseDN
	}
	if a.Pool != nil {
		return a.Pool.BaseDN()
	}
	return ""
}

// nomComplet retourne le cn du compte, à défaut son displayName.
func nomComplet(u *ad.User) string {
	if u.CN != "" {
		return u.CN
	}
	return u.DisplayName
}

// TrouverUtilisateur résout un identifiant ou une adresse de messagerie.
//
// Une valeur contenant « @ » est cherchée sur l'attribut mail, sinon sur
// sAMAccountName — comme `find_user` côté Python.
func (a *PoolAnnuaire) TrouverUtilisateur(requete string) (*Utilisateur, error) {
	if a.Pool == nil {
		return nil, errAnnuaireAbsent
	}
	valeur := strings.TrimSpace(requete)
	if valeur == "" {
		return nil, nil
	}

	attribut := "sAMAccountName"
	if strings.Contains(valeur, "@") {
		attribut = "mail"
	}
	filtre := fmt.Sprintf("(&(objectClass=user)(objectCategory=person)(%s=%s))",
		attribut, ldap.EscapeFilter(valeur))

	utilisateurs, err := a.Pool.UserSearch(a.contexte(), a.racine(), filtre,
		attributsUtilisateur, 2)
	if err != nil {
		return nil, err
	}
	if len(utilisateurs) == 0 {
		return nil, nil
	}
	utilisateur := utilisateurs[0]
	if utilisateur.SAMAccountName == "" {
		return nil, nil
	}
	return &Utilisateur{
		DN:        utilisateur.DN,
		SAM:       utilisateur.SAMAccountName,
		CN:        nomComplet(utilisateur),
		Mail:      utilisateur.Email,
		Desactive: !utilisateur.Enabled,
	}, nil
}

// DefinirMotDePasse réinitialise le mot de passe d'un compte.
func (a *PoolAnnuaire) DefinirMotDePasse(dn, motDePasse string) error {
	if a.Pool == nil {
		return errAnnuaireAbsent
	}
	return a.Pool.UserResetPassword(a.contexte(), dn, motDePasse)
}

// IdentifiantsExistants retourne les sAMAccountName déjà attribués, en
// minuscules (comparaison insensible à la casse).
func (a *PoolAnnuaire) IdentifiantsExistants() (map[string]bool, error) {
	if a.Pool == nil {
		return nil, errAnnuaireAbsent
	}
	utilisateurs, err := a.Pool.UserSearch(a.contexte(), a.racine(), "",
		[]string{"sAMAccountName"}, 0)
	if err != nil {
		return nil, err
	}
	existants := make(map[string]bool, len(utilisateurs))
	for _, utilisateur := range utilisateurs {
		if utilisateur.SAMAccountName != "" {
			existants[strings.ToLower(utilisateur.SAMAccountName)] = true
		}
	}
	return existants, nil
}

// CreerCompte crée le compte validé puis exige le changement du mot de passe à
// la première connexion (pwdLastSet = 0), comme
// `create_user(..., force_password_change=True)` côté Python.
//
// Si le compte a bien été créé mais que la suite échoue, il est supprimé :
// mieux vaut un échec net qu'un compte orphelin sans mot de passe.
func (a *PoolAnnuaire) CreerCompte(compte CompteCreation, motDePasse string) error {
	if a.Pool == nil {
		return errAnnuaireAbsent
	}
	utilisateur, err := a.Pool.UserCreate(a.contexte(), ad.UserCreateParams{
		SAMAccountName:    compte.Identifiant,
		UserPrincipalName: compte.UPN,
		DisplayName:       compte.Nom,
		GivenName:         compte.Prenom,
		SN:                compte.Famille,
		Email:             compte.Mail,
		InitialPassword:   motDePasse,
		OU:                compte.OU,
		Enabled:           true,
	})
	if err != nil {
		return err
	}
	if err := a.Pool.PasswordExpire(a.contexte(), utilisateur.DN); err != nil {
		// Suppression du compte orphelin (échec possible si le rôle délégué
		// n'autorise pas la suppression : le journal le signalera).
		_ = a.Pool.UserDelete(a.contexte(), utilisateur.DN)
		return err
	}
	return nil
}
