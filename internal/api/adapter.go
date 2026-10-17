package api

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// AnnuaireAD adapte un pool Active Directory à l'interface `Annuaire` de
// l'API. Toutes les erreurs du paquet `ad` sont traduites en erreurs d'API
// (404 pour une ressource absente, 503 pour un annuaire indisponible).
type AnnuaireAD struct {
	Pool *ad.Pool
}

// NewAnnuaireAD construit l'adaptateur d'un pool existant.
func NewAnnuaireAD(pool *ad.Pool) *AnnuaireAD {
	return &AnnuaireAD{Pool: pool}
}

// BaseDN retourne la racine du domaine connecté.
func (a *AnnuaireAD) BaseDN() string {
	if a == nil || a.Pool == nil {
		return ""
	}
	return a.Pool.BaseDN()
}

// ─── Utilisateurs ──────────────────────────────────────────────────────────

// ChercherUtilisateurs liste les comptes dont le SAM, le CN, le nom affiché ou
// l'adresse contiennent la recherche (`q` vide = tous les comptes).
func (a *AnnuaireAD) ChercherUtilisateurs(ctx context.Context, requete string,
	limite int) ([]Utilisateur, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	comptes, err := a.Pool.UserSearch(ctx, a.BaseDN(), filtreUtilisateurs(requete), nil, 0)
	if err != nil {
		return nil, traduireErreur(err)
	}
	if limite > 0 && len(comptes) > limite {
		comptes = comptes[:limite]
	}
	utilisateurs := make([]Utilisateur, 0, len(comptes))
	for _, compte := range comptes {
		utilisateurs = append(utilisateurs, versUtilisateur(compte))
	}
	return utilisateurs, nil
}

// ObtenirUtilisateur retrouve un compte par son SAMAccountName.
func (a *AnnuaireAD) ObtenirUtilisateur(ctx context.Context, sam string) (*Utilisateur, error) {
	compte, err := a.chercherUtilisateur(ctx, sam)
	if err != nil || compte == nil {
		return nil, err
	}
	utilisateur := versUtilisateur(compte)
	return &utilisateur, nil
}

// CreerUtilisateur crée un compte dans l'OU demandée.
func (a *AnnuaireAD) CreerUtilisateur(ctx context.Context,
	params CreationUtilisateur) (*Utilisateur, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	// Un compte existant n'est jamais recréé en silence : la création échoue.
	existant, err := a.chercherUtilisateur(ctx, params.SAM)
	if err != nil {
		return nil, err
	}
	if existant != nil {
		return nil, nouvelleErreurAPI(409, "conflict",
			fmt.Sprintf("Le compte '%s' existe déjà", params.SAM), nil)
	}

	domaine := a.Pool.BaseDN()
	upn := params.SAM + "@" + domaineDNS(domaine)
	cree, err := a.Pool.UserCreate(ctx, ad.UserCreateParams{
		SAMAccountName:    params.SAM,
		UserPrincipalName: upn,
		DisplayName:       params.CN,
		GivenName:         params.Prenom,
		SN:                params.Nom,
		InitialPassword:   params.MotDePasse,
		OU:                params.OU,
		Description:       params.Description,
		Email:             params.Mail,
		Enabled:           true,
	})
	if err != nil {
		return nil, traduireErreur(err)
	}
	utilisateur := versUtilisateur(cree)
	return &utilisateur, nil
}

// ModifierUtilisateur applique les champs fournis (cn, mail, description,
// given_name, sn, ou, active).
func (a *AnnuaireAD) ModifierUtilisateur(ctx context.Context, sam string,
	champs map[string]string) (*Utilisateur, error) {
	compte, err := a.chercherUtilisateur(ctx, sam)
	if err != nil {
		return nil, err
	}
	if compte == nil {
		return nil, ErrIntrouvable("Utilisateur", sam)
	}

	if nouvelleOU := champs["ou"]; nouvelleOU != "" && nouvelleOU != compte.OU {
		if err := a.Pool.UserMove(ctx, compte.DN, nouvelleOU); err != nil {
			return nil, traduireErreur(err)
		}
	}

	majs := make([]ad.AttributeUpdate, 0, 4)
	for _, cle := range []struct{ api, ldap string }{
		{"cn", "displayName"},
		{"mail", "mail"},
		{"description", "description"},
		{"given_name", "givenName"},
		{"sn", "sn"},
	} {
		if valeur, ok := champs[cle.api]; ok {
			majs = append(majs, ad.AttributeUpdate{Nom: cle.ldap, Valeurs: []string{valeur}})
		}
	}
	if len(majs) > 0 {
		if err := a.Pool.UserSetAttributes(ctx, compte.DN, majs); err != nil {
			return nil, traduireErreur(err)
		}
	}
	if valeur, ok := champs["active"]; ok {
		if err := a.Pool.UserEnable(ctx, compte.DN, valeur == "true"); err != nil {
			return nil, traduireErreur(err)
		}
	}

	// Relire l'état réel plutôt que de recomposer une réponse approximative.
	compte, err = a.Pool.UserGet(ctx, compte.DN)
	if err != nil {
		return nil, traduireErreur(err)
	}
	if compte == nil {
		return nil, ErrIntrouvable("Utilisateur", sam)
	}
	utilisateur := versUtilisateur(compte)
	return &utilisateur, nil
}

// SupprimerUtilisateur efface un compte.
func (a *AnnuaireAD) SupprimerUtilisateur(ctx context.Context, sam string) error {
	compte, err := a.chercherUtilisateur(ctx, sam)
	if err != nil {
		return err
	}
	if compte == nil {
		return ErrIntrouvable("Utilisateur", sam)
	}
	return traduireErreur(a.Pool.UserDelete(ctx, compte.DN))
}

// ReinitialiserMotDePasse applique un nouveau mot de passe sans exiger
// l'ancien (droit de réinitialisation).
func (a *AnnuaireAD) ReinitialiserMotDePasse(ctx context.Context, sam, motDePasse string) error {
	compte, err := a.chercherUtilisateur(ctx, sam)
	if err != nil {
		return err
	}
	if compte == nil {
		return ErrIntrouvable("Utilisateur", sam)
	}
	return traduireErreur(a.Pool.UserResetPassword(ctx, compte.DN, motDePasse))
}

// chercherUtilisateur retrouve un compte par SAM (nil si absent).
func (a *AnnuaireAD) chercherUtilisateur(ctx context.Context, sam string) (*ad.User, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	if strings.TrimSpace(sam) == "" {
		return nil, ErrValidation("sam requis", nil)
	}
	comptes, err := a.Pool.UserSearch(ctx, a.BaseDN(),
		fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s))", ldap.EscapeFilter(sam)), nil, 0)
	if err != nil {
		return nil, traduireErreur(err)
	}
	if len(comptes) == 0 {
		return nil, nil
	}
	return comptes[0], nil
}

// ─── Groupes ───────────────────────────────────────────────────────────────

// ChercherGroupes liste les groupes dont le CN ou le SAM contient la recherche.
func (a *AnnuaireAD) ChercherGroupes(ctx context.Context, requete string,
	limite int) ([]Groupe, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	groupes, err := a.Pool.GroupSearch(ctx, a.BaseDN(), filtreGroupes(requete), nil, 0)
	if err != nil {
		return nil, traduireErreur(err)
	}
	if limite > 0 && len(groupes) > limite {
		groupes = groupes[:limite]
	}
	resultat := make([]Groupe, 0, len(groupes))
	for _, groupe := range groupes {
		resultat = append(resultat, versGroupe(groupe, false))
	}
	return resultat, nil
}

// ObtenirGroupe retrouve un groupe par son CN (membres inclus).
func (a *AnnuaireAD) ObtenirGroupe(ctx context.Context, cn string) (*Groupe, error) {
	groupe, err := a.chercherGroupe(ctx, cn)
	if err != nil || groupe == nil {
		return nil, err
	}
	resultat := versGroupe(groupe, true)
	return &resultat, nil
}

// CreerGroupe crée un groupe dans l'OU demandée.
func (a *AnnuaireAD) CreerGroupe(ctx context.Context, params CreationGroupe) (*Groupe, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	existant, err := a.chercherGroupe(ctx, params.CN)
	if err != nil {
		return nil, err
	}
	if existant != nil {
		return nil, nouvelleErreurAPI(409, "conflict",
			fmt.Sprintf("Le groupe '%s' existe déjà", params.CN), nil)
	}
	sam := params.SAM
	if sam == "" {
		sam = params.CN
	}
	cree, err := a.Pool.GroupCreate(ctx, ad.GroupCreateParams{
		CN:             params.CN,
		SAMAccountName: sam,
		Description:    params.Description,
		OU:             params.OU,
		GroupType:      params.Type,
		Scope:          params.Portee,
	})
	if err != nil {
		return nil, traduireErreur(err)
	}
	resultat := versGroupe(cree, true)
	return &resultat, nil
}

// chercherGroupe retrouve un groupe par CN (nil si absent).
func (a *AnnuaireAD) chercherGroupe(ctx context.Context, cn string) (*ad.Group, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	if strings.TrimSpace(cn) == "" {
		return nil, ErrValidation("cn requis", nil)
	}
	groupes, err := a.Pool.GroupSearch(ctx, a.BaseDN(),
		fmt.Sprintf("(&(objectClass=group)(cn=%s))", ldap.EscapeFilter(cn)), nil, 0)
	if err != nil {
		return nil, traduireErreur(err)
	}
	if len(groupes) == 0 {
		return nil, nil
	}
	return groupes[0], nil
}

// ─── Unités d'organisation ─────────────────────────────────────────────────

// ChercherOUs liste les OU sous la racine du domaine (un niveau).
func (a *AnnuaireAD) ChercherOUs(ctx context.Context, requete string, limite int) ([]Unite, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	unites, err := a.Pool.OUSearch(ctx, a.BaseDN(), filtreOUs(requete))
	if err != nil {
		return nil, traduireErreur(err)
	}
	if limite > 0 && len(unites) > limite {
		unites = unites[:limite]
	}
	resultat := make([]Unite, 0, len(unites))
	for _, unite := range unites {
		resultat = append(resultat, versUnite(unite))
	}
	return resultat, nil
}

// ObtenirOU retrouve une OU par son DN.
func (a *AnnuaireAD) ObtenirOU(ctx context.Context, dn string) (*Unite, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	if strings.TrimSpace(dn) == "" {
		return nil, ErrValidation("dn requis", nil)
	}
	unite, err := a.Pool.OUGet(ctx, dn)
	if err != nil {
		return nil, traduireErreur(err)
	}
	if unite == nil {
		return nil, nil
	}
	resultat := versUnite(unite)
	return &resultat, nil
}

// CreerOU crée une unité d'organisation sous une OU parente.
func (a *AnnuaireAD) CreerOU(ctx context.Context, nom, description, parentDN string) (*Unite, error) {
	if a == nil || a.Pool == nil {
		return nil, errAnnuaireIndisponible()
	}
	if parentDN == "" {
		parentDN = a.BaseDN()
	}
	creee, err := a.Pool.OUCreate(ctx, ad.OUCreateParams{
		Name:        nom,
		Description: description,
		ParentDN:    parentDN,
	})
	if err != nil {
		return nil, traduireErreur(err)
	}
	resultat := versUnite(creee)
	return &resultat, nil
}

// ─── Conversions ───────────────────────────────────────────────────────────

// versUtilisateur convertit un compte AD en vue d'API.
func versUtilisateur(compte *ad.User) Utilisateur {
	if compte == nil {
		return Utilisateur{}
	}
	return Utilisateur{
		SAM:   compte.SAMAccountName,
		CN:    compte.CN,
		Mail:  compte.Email,
		DN:    compte.DN,
		UPN:   compte.UserPrincipalName,
		OU:    compte.OU,
		Actif: compte.Enabled,
	}
}

// versGroupe convertit un groupe AD en vue d'API.
func versGroupe(groupe *ad.Group, avecMembres bool) Groupe {
	if groupe == nil {
		return Groupe{}
	}
	resultat := Groupe{
		CN:          groupe.CN,
		SAM:         groupe.SAMAccountName,
		DN:          groupe.DN,
		Description: groupe.Description,
		OU:          groupe.OU,
	}
	if avecMembres {
		resultat.Membres = append([]string{}, groupe.Members...)
	}
	return resultat
}

// versUnite convertit une OU AD en vue d'API.
func versUnite(unite *ad.OU) Unite {
	if unite == nil {
		return Unite{}
	}
	return Unite{DN: unite.DN, Nom: unite.Name, Description: unite.Description}
}

// ─── Filtres ───────────────────────────────────────────────────────────────

// filtreUtilisateurs construit le filtre LDAP de recherche textuelle.
func filtreUtilisateurs(requete string) string {
	base := "(&(objectClass=user)(objectCategory=person)"
	if strings.TrimSpace(requete) == "" {
		return base + ")"
	}
	motif := "*" + ldap.EscapeFilter(requete) + "*"
	return fmt.Sprintf("%s(|(sAMAccountName=%s)(cn=%s)(displayName=%s)(mail=%s)(givenName=%s)(sn=%s)))",
		base, motif, motif, motif, motif, motif, motif)
}

// filtreGroupes construit le filtre LDAP des groupes.
func filtreGroupes(requete string) string {
	if strings.TrimSpace(requete) == "" {
		return "(objectClass=group)"
	}
	motif := "*" + ldap.EscapeFilter(requete) + "*"
	return fmt.Sprintf("(&(objectClass=group)(|(cn=%s)(sAMAccountName=%s)(description=%s)))",
		motif, motif, motif)
}

// filtreOUs construit le filtre LDAP des unités d'organisation.
func filtreOUs(requete string) string {
	if strings.TrimSpace(requete) == "" {
		return "(objectClass=organizationalUnit)"
	}
	motif := "*" + ldap.EscapeFilter(requete) + "*"
	return fmt.Sprintf("(&(objectClass=organizationalUnit)(|(ou=%s)(description=%s)))", motif, motif)
}

// domaineDNS convertit un DN (`DC=edusync,DC=local`) en nom DNS
// (`edusync.local`).
func domaineDNS(baseDN string) string {
	var morceaux []string
	for _, element := range strings.Split(baseDN, ",") {
		element = strings.TrimSpace(element)
		if len(element) > 3 && strings.EqualFold(element[:3], "DC=") {
			morceaux = append(morceaux, element[3:])
		}
	}
	return strings.Join(morceaux, ".")
}

// ─── Erreurs ───────────────────────────────────────────────────────────────

// errAnnuaireIndisponible signale l'absence de connexion à l'annuaire.
func errAnnuaireIndisponible() error {
	return nouvelleErreurAPI(503, "annuaire_indisponible",
		"Aucune connexion à l'annuaire n'est configurée.", nil)
}

// traduireErreur convertit une erreur du paquet `ad` en erreur d'API.
//
// Une erreur de recherche sur un objet absent devient un 404 : le message de
// l'annuaire (`no such object`) ne doit pas fuiter tel quel dans l'API.
func traduireErreur(err error) error {
	if err == nil {
		return nil
	}
	var apiErr *ErreurAPI
	if errors.As(err, &apiErr) {
		return err
	}
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "no such object"),
		strings.Contains(message, "introuvable"),
		strings.Contains(message, "not found"):
		return ErrIntrouvable("Ressource", err.Error())
	case strings.Contains(message, "indisponible"),
		strings.Contains(message, "connexion"),
		strings.Contains(message, "connection refused"),
		strings.Contains(message, "timeout"):
		return nouvelleErreurAPI(503, "annuaire_indisponible", err.Error(), nil)
	default:
		return nouvelleErreurAPI(500, "annuaire_error", err.Error(), nil)
	}
}
