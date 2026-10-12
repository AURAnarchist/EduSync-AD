// Package ad fournit la logique de migration/restructuration (fin d'année scolaire).
package ad

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-ldap/ldap/v3"
)

// MigrationRow représente une ligne de migration (identique à internal/models).
type MigrationRow struct {
	SAMAccountName string
	SourceOU       string
	TargetOU       string
	UserDN         string
	DisplayName    string
	Error          string
}

// MigrationPlan décrit un plan de migration complet.
type MigrationPlan struct {
	Rows     []MigrationRow
	Total    int
	WillMove int
	WillSkip int
	Errors   []string
}

// MigrationPlanFromCSV construit un plan de migration à partir de données CSV
// (prénom, nom, classe/équipe cible). La résolution se fait par
// sAMAccountName (plus fiable que prénom/nom).
func (p *Pool) MigrationPlanFromCSV(ctx context.Context, rows []MigrationRow) (*MigrationPlan, error) {
	plan := &MigrationPlan{Rows: rows, Total: len(rows)}

	for i := range plan.Rows {
		row := &plan.Rows[i]

		// Si UserDN déjà fourni, l'utiliser directement
		if row.UserDN != "" {
			if _, err := p.UserGet(ctx, row.UserDN); err != nil {
				row.Error = fmt.Sprintf("utilisateur DN introuvable: %v", err)
				plan.Errors = append(plan.Errors, fmt.Sprintf("%s: %s", row.SAMAccountName, row.Error))
				plan.WillSkip++
				continue
			}
			if row.SourceOU == "" {
				row.SourceOU = parentDN(row.UserDN)
			}
			if row.SourceOU != row.TargetOU {
				plan.WillMove++
			} else {
				plan.WillSkip++
			}
			continue
		}

		// Sinon résoudre par sAMAccountName
		if row.SAMAccountName == "" {
			row.Error = "sAMAccountName manquant"
			plan.Errors = append(plan.Errors, row.Error)
			plan.WillSkip++
			continue
		}

		filter := fmt.Sprintf("(&(objectClass=user)(sAMAccountName=%s))", ldap.EscapeFilter(row.SAMAccountName))
		req := ldap.NewSearchRequest(p.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false, filter,
			[]string{"dn", "displayName", "distinguishedName"}, nil)

		conn, err := p.Get(ctx)
		if err != nil {
			return nil, err
		}

		res, err := conn.Search(req)
		p.Put(conn)
		if err != nil {
			row.Error = fmt.Sprintf("recherche: %v", err)
			plan.Errors = append(plan.Errors, fmt.Sprintf("%s: %s", row.SAMAccountName, row.Error))
			plan.WillSkip++
			continue
		}

		if len(res.Entries) == 0 {
			row.Error = "utilisateur introuvable dans l'AD"
			plan.Errors = append(plan.Errors, fmt.Sprintf("%s: %s", row.SAMAccountName, row.Error))
			plan.WillSkip++
			continue
		}

		entry := res.Entries[0]
		row.UserDN = entry.DN
		row.DisplayName = entry.GetAttributeValue("displayName")
		row.SourceOU = parentDN(entry.DN)

		if row.SourceOU != row.TargetOU {
			plan.WillMove++
		} else {
			plan.WillSkip++
		}
	}

	return plan, nil
}

// MigrationSimulate simule l'exécution d'un plan de migration.
func (p *Pool) MigrationSimulate(ctx context.Context, plan *MigrationPlan) (*SimulateResult, error) {
	var changes []string
	for _, row := range plan.Rows {
		if row.Error != "" || row.SourceOU == row.TargetOU {
			continue
		}
		changes = append(changes,
			fmt.Sprintf("Déplacer %s (%s) de %s vers %s",
				row.SAMAccountName, row.DisplayName, row.SourceOU, row.TargetOU))
	}

	return &SimulateResult{
		Action:       "migration",
		TargetDN:     "",
		Changes:      changes,
		WouldSucceed: len(changes) > 0,
		Error:        "",
	}, nil
}

// MigrationExecute exécute le plan de migration.
func (p *Pool) MigrationExecute(ctx context.Context, plan *MigrationPlan) error {
	for i := range plan.Rows {
		row := &plan.Rows[i]

		if row.Error != "" {
			continue // déjà en erreur
		}
		if row.SourceOU == row.TargetOU {
			continue // déjà au bon endroit
		}
		if row.UserDN == "" {
			row.Error = "UserDN manquant"
			continue
		}

		if err := p.UserMove(ctx, row.UserDN, row.TargetOU); err != nil {
			row.Error = fmt.Sprintf("déplacement: %v", err)
			continue
		}

		// Mettre à jour l'appartenance aux groupes de classe si nécessaire
		// (la logique exacte dépend de la nomenclature des groupes)
		// Pour l'instant on log seulement
		fmt.Printf("ad: migration %s -> %s OK\n", row.SAMAccountName, row.TargetOU)
	}

	return nil
}

// MigrationGroupsByOU met à jour les appartenances aux groupes basées sur l'OU
// (ex: groupe "Classe_6e_A" contient tous les élèves de l'OU correspondante).
func (p *Pool) MigrationGroupsByOU(ctx context.Context, ou string, groupPrefix string) error {
	users, err := p.UserSearch(ctx, ou, "(&(objectClass=user)(objectCategory=person))", []string{"dn", "sAMAccountName"}, 0)
	if err != nil {
		return fmt.Errorf("recherche élèves: %w", err)
	}

	// Construire le nom du groupe attendu
	ouName := strings.TrimPrefix(strings.Split(ou, ",")[0], "OU=")
	groupCN := groupPrefix + "_" + ouName

	// Trouver le groupe
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	filter := fmt.Sprintf("(&(objectClass=group)(cn=%s))", ldap.EscapeFilter(groupCN))
	req := ldap.NewSearchRequest(p.cfg.BaseDN, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false, filter, []string{"dn"}, nil)
	res, err := conn.Search(req)
	if err != nil || len(res.Entries) == 0 {
		return fmt.Errorf("groupe %s introuvable", groupCN)
	}
	groupDN := res.Entries[0].DN

	// Récupérer les membres actuels
	req = ldap.NewSearchRequest(groupDN, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=group)", []string{"member"}, nil)
	res, err = conn.Search(req)
	if err != nil {
		return fmt.Errorf("lecture membres groupe: %w", err)
	}
	currentMembers := map[string]bool{}
	for _, m := range res.Entries[0].GetAttributeValues("member") {
		currentMembers[strings.ToLower(m)] = true
	}

	// Membres souhaités
	desiredMembers := map[string]bool{}
	for _, u := range users {
		desiredMembers[strings.ToLower(u.DN)] = true
	}

	// Ajouter les manquants
	for dn := range desiredMembers {
		if !currentMembers[dn] {
			if err := p.GroupAddMember(ctx, groupDN, dn); err != nil {
				fmt.Printf("ad: avertissement: ajout %s à %s: %v\n", dn, groupCN, err)
			}
		}
	}

	// Retirer les en trop
	for dn := range currentMembers {
		if !desiredMembers[dn] {
			if err := p.GroupRemoveMember(ctx, groupDN, dn); err != nil {
				fmt.Printf("ad: avertissement: retrait %s de %s: %v\n", dn, groupCN, err)
			}
		}
	}

	return nil
}
