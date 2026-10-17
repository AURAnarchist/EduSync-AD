// Package ad fournit la gestion des quotas de disque (FSRM) selon M14 de la
// roadmap.
//
// Chaque opération suit le pattern : Validate() → Simulate() → Execute() → Audit()
// (ROADMAP_GO_MIGRATION.md §3). Le mode simulation est le chemin par défaut.
//
// Les quotas sont stockés dans les attributs LDAP msDS-QuotaAmount (limite en
// octets) sur l'utilisateur ou sur l'OU cible.
package ad

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// QuotaType définit le type de quota.
type QuotaType string

const (
	// QuotaTypeDisque quota d'espace disque (octets).
	QuotaTypeDisque QuotaType = "disque"
	// QuotaTypeFichiers quota de nombre de fichiers.
	QuotaTypeFichiers QuotaType = "fichiers"
)

// Valide vérifie que le type de quota est connu.
func (t QuotaType) Valide() bool {
	switch t {
	case QuotaTypeDisque, QuotaTypeFichiers:
		return true
	}
	return false
}

// QuotaSeuil définit le type de seuil d'alerte.
type QuotaSeuil string

const (
	// QuotaSeuilAlerte seuil d'alerte (warning).
	QuotaSeuilAlerte QuotaSeuil = "alerte"
	// QuotaSeuilBlocage seuil de blocage (hard limit).
	QuotaSeuilBlocage QuotaSeuil = "blocage"
)

// Valide vérifie que le seuil est connu.
func (s QuotaSeuil) Valide() bool {
	switch s {
	case QuotaSeuilAlerte, QuotaSeuilBlocage:
		return true
	}
	return false
}

// Sentinelles d'erreur pour les quotas.
// ErrUserDNManquant est définie dans profiles.go (même package).
var (
	// ErrQuotaTypeInvalide est retournée quand le type de quota est inconnu.
	ErrQuotaTypeInvalide = errors.New("ad: type de quota invalide")
	// ErrQuotaLimiteInvalide est retournée quand la limite est négative.
	ErrQuotaLimiteInvalide = errors.New("ad: limite de quota invalide")
	// ErrQuotaSeuilInvalide est retournée quand le seuil est inconnu.
	ErrQuotaSeuilInvalide = errors.New("ad: seuil de quota invalide")
)

// QuotaParams décrit les paramètres de définition d'un quota.
type QuotaParams struct {
	UserDN string     // DN de l'utilisateur cible
	OU     string     // DN de l'OU cible (vide = quota utilisateur)
	Type   QuotaType  // type de quota (disque, fichiers)
	Limite int64      // limite en octets (disque) ou en fichiers
	Seuil  QuotaSeuil // seuil d'alerte (vide = pas de seuil)
}

// Validate vérifie la cohérence des paramètres.
func (p *QuotaParams) Validate() error {
	if p.UserDN == "" && p.OU == "" {
		return ErrUserDNManquant
	}
	if !p.Type.Valide() {
		return ErrQuotaTypeInvalide
	}
	if p.Limite < 0 {
		return ErrQuotaLimiteInvalide
	}
	if p.Seuil != "" && !p.Seuil.Valide() {
		return ErrQuotaSeuilInvalide
	}
	return nil
}

// QuotaResult décrit le résultat d'une opération de quota.
type QuotaResult struct {
	UserDN   string     // DN de l'utilisateur (vide si quota OU)
	OU       string     // DN de l'OU (vide si quota utilisateur)
	Type     QuotaType  // type appliqué
	Limite   int64      // limite configurée
	Seuil    QuotaSeuil // seuil configuré
	Statut   string     // "succes" | "echec" | "skip"
	Detaille string     // détail / message d'erreur
}

// FormateTaille formate une taille en octets de façon lisible.
func FormateTaille(octets int64) string {
	const (
		ko  = 1024
		mo  = ko * 1024
		go_ = mo * 1024
		to  = go_ * 1024
	)
	var valeur string
	switch {
	case octets >= to:
		valeur = fmt.Sprintf("%.2f To", float64(octets)/float64(to))
	case octets >= go_:
		valeur = fmt.Sprintf("%.2f Go", float64(octets)/float64(go_))
	case octets >= mo:
		valeur = fmt.Sprintf("%.2f Mo", float64(octets)/float64(mo))
	case octets >= ko:
		valeur = fmt.Sprintf("%.2f Ko", float64(octets)/float64(ko))
	default:
		valeur = fmt.Sprintf("%d o", octets)
	}
	return strings.ReplaceAll(valeur, ".", ",")
}

// QuotaSimulate simule l'application d'un quota (vérifie l'existence,
// ne modifie pas l'AD).
func (p *Pool) QuotaSimulate(ctx context.Context, params QuotaParams) (*QuotaResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	// Si OU donnée, vérifier qu'elle existe.
	if params.OU != "" {
		_, err := p.OUGet(ctx, params.OU)
		if err != nil {
			return &QuotaResult{
				OU:       params.OU,
				Type:     params.Type,
				Limite:   params.Limite,
				Seuil:    params.Seuil,
				Statut:   "echec",
				Detaille: fmt.Sprintf("OU introuvable: %v", err),
			}, nil
		}
		return &QuotaResult{
			OU:       params.OU,
			Type:     params.Type,
			Limite:   params.Limite,
			Seuil:    params.Seuil,
			Statut:   "succes",
			Detaille: fmt.Sprintf("quota %s à appliquer sur l'OU %s", params.Type, params.OU),
		}, nil
	}

	// Sinon vérifier l'utilisateur.
	u, err := p.UserGet(ctx, params.UserDN)
	if err != nil {
		return &QuotaResult{
			UserDN:   params.UserDN,
			Type:     params.Type,
			Limite:   params.Limite,
			Seuil:    params.Seuil,
			Statut:   "echec",
			Detaille: fmt.Sprintf("utilisateur introuvable: %v", err),
		}, nil
	}

	return &QuotaResult{
		UserDN:   u.DN,
		Type:     params.Type,
		Limite:   params.Limite,
		Seuil:    params.Seuil,
		Statut:   "succes",
		Detaille: fmt.Sprintf("quota %s à appliquer sur %s (%s)", params.Type, u.SAMAccountName, FormateTaille(params.Limite)),
	}, nil
}

// QuotaExecute applique réellement le quota (écrit msDS-QuotaAmount).
func (p *Pool) QuotaExecute(ctx context.Context, params QuotaParams) (*QuotaResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.QuotaSimulate(ctx, params)
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

	cible := params.UserDN
	if params.OU != "" {
		cible = params.OU
	}

	modReq := ldap.NewModifyRequest(cible, nil)
	modReq.Replace("msDS-QuotaAmount", []string{fmt.Sprintf("%d", params.Limite)})
	if err := conn.Modify(modReq); err != nil {
		return &QuotaResult{
			UserDN:   sim.UserDN,
			OU:       sim.OU,
			Type:     params.Type,
			Limite:   params.Limite,
			Seuil:    params.Seuil,
			Statut:   "echec",
			Detaille: fmt.Sprintf("écriture msDS-QuotaAmount: %v", err),
		}, nil
	}

	sim.Statut = "succes"
	sim.Detaille = fmt.Sprintf("quota %s appliqué: %s", params.Type, FormateTaille(params.Limite))
	return sim, nil
}

// QuotaBulkParams décrit une opération de quotas en masse.
type QuotaBulkParams struct {
	UserDNs []string   // DN des utilisateurs (vide = tous de l'OU)
	OU      string     // OU pour sélectionner tous les utilisateurs
	Type    QuotaType  // type de quota
	Limite  int64      // limite
	Seuil   QuotaSeuil // seuil
}

// Validate vérifie les paramètres en masse.
func (p *QuotaBulkParams) Validate() error {
	if len(p.UserDNs) == 0 && p.OU == "" {
		return errors.New("ad: au moins un user DN ou une OU est obligatoire")
	}
	if !p.Type.Valide() {
		return errors.New("ad: type de quota invalide")
	}
	if p.Limite < 0 {
		return ErrQuotaLimiteInvalide
	}
	if p.Seuil != "" && !p.Seuil.Valide() {
		return ErrQuotaSeuilInvalide
	}
	return nil
}

// QuotaBulkExecute applique le quota à plusieurs utilisateurs.
func (p *Pool) QuotaBulkExecute(ctx context.Context, params QuotaBulkParams) ([]QuotaResult, error) {
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

	results := make([]QuotaResult, 0, len(users))
	for _, u := range users {
		simParams := QuotaParams{
			UserDN: u.DN,
			Type:   params.Type,
			Limite: params.Limite,
			Seuil:  params.Seuil,
		}
		result, err := p.QuotaExecute(ctx, simParams)
		if err != nil {
			result = &QuotaResult{
				UserDN:   u.DN,
				Type:     params.Type,
				Limite:   params.Limite,
				Seuil:    params.Seuil,
				Statut:   "echec",
				Detaille: err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}

var _ = ldap.EscapeFilter // conservé pour cohérence avec les autres fichiers du paquet
