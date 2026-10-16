// Package ad fournit la gestion des départs (désactivation + archivage
// planifié) des comptes utilisateurs Active Directory.
//
// Chaque opération suit le pattern : Validate() → Simulate() → Execute() → Audit()
// (ROADMAP_GO_MIGRATION.md §3). Le mode simulation est le chemin par défaut.
package ad

import (
	"context"
	"errors"
	"fmt"
)

// DepartMode définit le mode de traitement d'un départ.
type DepartMode string

const (
	DepartModeImmediat  DepartMode = "immediat"  // désactivation immédiate
	DepartModeDiffere   DepartMode = "differe"   // désactivation + purge planifiée
	DepartModeRestaurer DepartMode = "restaurer" // réactivation d'un compte
)

// DepartParams décrit les paramètres d'un départ individuel.
type DepartParams struct {
	UserDN     string     // DN de l'utilisateur
	Mode       DepartMode // mode de traitement
	DelaiJours int        // délai avant purge (jours, 0 = défaut 30)
	Motif      string     // motif du départ (audit)
}

// Validate vérifie la cohérence des paramètres.
func (p *DepartParams) Validate() error {
	if p.UserDN == "" {
		return errors.New("ad: user DN obligatoire pour un départ")
	}
	switch p.Mode {
	case DepartModeImmediat, DepartModeDiffere, DepartModeRestaurer:
	default:
		return errors.New("ad: mode de départ invalide")
	}
	if p.Mode == DepartModeDiffere && p.DelaiJours <= 0 {
		p.DelaiJours = 30 // défaut : 30 jours
	}
	return nil
}

// DepartResult décrit le résultat d'une opération de départ.
type DepartResult struct {
	UserDN         string
	SAMAccountName string
	DisplayName    string
	Mode           DepartMode
	DelaiJours     int
	Statut         string // "succes" | "echec" | "skip"
	Detail         string
}

// DepartSimulate simule un départ (vérifie l'existence et l'état du compte).
func (p *Pool) DepartSimulate(ctx context.Context, params DepartParams) (*DepartResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	u, err := p.UserGet(ctx, params.UserDN)
	if err != nil {
		return &DepartResult{
			UserDN: params.UserDN,
			Statut: "echec",
			Detail: err.Error(),
		}, nil
	}

	alreadyDisabled := !u.Enabled

	switch params.Mode {
	case DepartModeImmediat:
		if alreadyDisabled {
			return &DepartResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				DisplayName:    u.DisplayName,
				Mode:           params.Mode,
				Statut:         "skip",
				Detail:         "compte déjà désactivé",
			}, nil
		}
		return &DepartResult{
			UserDN:         u.DN,
			SAMAccountName: u.SAMAccountName,
			DisplayName:    u.DisplayName,
			Mode:           params.Mode,
			Statut:         "succes",
			Detail:         "désactivation immédiate",
		}, nil

	case DepartModeDiffere:
		if alreadyDisabled {
			return &DepartResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				DisplayName:    u.DisplayName,
				Mode:           params.Mode,
				DelaiJours:     params.DelaiJours,
				Statut:         "skip",
				Detail:         "compte déjà désactivé",
			}, nil
		}
		return &DepartResult{
			UserDN:         u.DN,
			SAMAccountName: u.SAMAccountName,
			DisplayName:    u.DisplayName,
			Mode:           params.Mode,
			DelaiJours:     params.DelaiJours,
			Statut:         "succes",
			Detail:         fmt.Sprintf("désactivation + purge dans %d jours", params.DelaiJours),
		}, nil

	case DepartModeRestaurer:
		if !alreadyDisabled {
			return &DepartResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				DisplayName:    u.DisplayName,
				Mode:           params.Mode,
				Statut:         "skip",
				Detail:         "compte déjà actif",
			}, nil
		}
		return &DepartResult{
			UserDN:         u.DN,
			SAMAccountName: u.SAMAccountName,
			DisplayName:    u.DisplayName,
			Mode:           params.Mode,
			Statut:         "succes",
			Detail:         "réactivation du compte",
		}, nil
	}

	return &DepartResult{
		UserDN: params.UserDN,
		Statut: "echec",
		Detail: "mode non géré",
	}, nil
}

// DepartExecute exécute un départ (désactivation / réactivation).
func (p *Pool) DepartExecute(ctx context.Context, params DepartParams) (*DepartResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.DepartSimulate(ctx, params)
	if err != nil {
		return nil, err
	}
	if sim.Statut == "skip" {
		return sim, nil
	}
	if sim.Statut == "echec" {
		return sim, fmt.Errorf("simulation échouée: %s", sim.Detail)
	}

	switch params.Mode {
	case DepartModeImmediat:
		if err := p.UserEnable(ctx, params.UserDN, false); err != nil {
			return &DepartResult{
				UserDN: params.UserDN,
				Statut: "echec",
				Detail: err.Error(),
			}, nil
		}

	case DepartModeDiffere:
		if err := p.UserEnable(ctx, params.UserDN, false); err != nil {
			return &DepartResult{
				UserDN: params.UserDN,
				Statut: "echec",
				Detail: err.Error(),
			}, nil
		}

	case DepartModeRestaurer:
		if err := p.UserEnable(ctx, params.UserDN, true); err != nil {
			return &DepartResult{
				UserDN: params.UserDN,
				Statut: "echec",
				Detail: err.Error(),
			}, nil
		}
	}

	sim.Statut = "succes"
	return sim, nil
}

// DepartBulkParams décrit une opération de départs en masse (par OU ou liste de DNs).
type DepartBulkParams struct {
	UserDNs    []string // DN des utilisateurs (vide = tous de l'OU)
	OU         string   // OU pour sélectionner tous les utilisateurs
	Mode       DepartMode
	DelaiJours int
	Motif      string
}

// Validate vérifie les paramètres en masse.
func (p *DepartBulkParams) Validate() error {
	if len(p.UserDNs) == 0 && p.OU == "" {
		return errors.New("ad: au moins un user DN ou une OU est obligatoire")
	}
	switch p.Mode {
	case DepartModeImmediat, DepartModeDiffere, DepartModeRestaurer:
	default:
		return errors.New("ad: mode de départ invalide")
	}
	if p.Mode == DepartModeDiffere && p.DelaiJours <= 0 {
		p.DelaiJours = 30
	}
	return nil
}

// DepartBulkExecute traite plusieurs départs en une opération.
func (p *Pool) DepartBulkExecute(ctx context.Context, params DepartBulkParams) ([]DepartResult, error) {
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
			[]string{"dn", "sAMAccountName", "displayName", "userAccountControl"}, 0)
		if err != nil {
			return nil, fmt.Errorf("recherche utilisateurs dans %s: %w", params.OU, err)
		}
	}

	results := make([]DepartResult, 0, len(users))
	for _, u := range users {
		// Skip les comptes déjà dans l'état cible
		alreadyDisabled := !u.Enabled
		if params.Mode == DepartModeImmediat && alreadyDisabled {
			results = append(results, DepartResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				DisplayName:    u.DisplayName,
				Mode:           params.Mode,
				Statut:         "skip",
				Detail:         "déjà désactivé",
			})
			continue
		}
		if params.Mode == DepartModeRestaurer && !alreadyDisabled {
			results = append(results, DepartResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				DisplayName:    u.DisplayName,
				Mode:           params.Mode,
				Statut:         "skip",
				Detail:         "déjà actif",
			})
			continue
		}

		simParams := DepartParams{
			UserDN:     u.DN,
			Mode:       params.Mode,
			DelaiJours: params.DelaiJours,
			Motif:      params.Motif,
		}
		result, err := p.DepartExecute(ctx, simParams)
		if err != nil {
			result = &DepartResult{
				UserDN:         u.DN,
				SAMAccountName: u.SAMAccountName,
				DisplayName:    u.DisplayName,
				Mode:           params.Mode,
				Statut:         "echec",
				Detail:         err.Error(),
			}
		}
		results = append(results, *result)
	}

	return results, nil
}
