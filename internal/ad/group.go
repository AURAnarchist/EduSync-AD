// Package ad fournit les opérations sur les groupes Active Directory.
package ad

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

// Group représente un groupe AD.
type Group struct {
	DN             string
	CN             string
	SAMAccountName string
	Description    string
	Members        []string
	GroupType      string
	OU             string
}

// GroupCreateParams décrit les paramètres de création d'un groupe.
type GroupCreateParams struct {
	CN             string
	SAMAccountName string
	Description    string
	OU             string
	GroupType      string   // "security" ou "distribution"
	Scope          string   // "" (global), "global", "domainlocal" ou "universal"
	Members        []string // DN des membres initiaux
}

// Validate vérifie la cohérence.
func (p *GroupCreateParams) Validate() error {
	if p.CN == "" {
		return errors.New("ad: CN obligatoire")
	}
	if p.SAMAccountName == "" {
		return errors.New("ad: sAMAccountName obligatoire")
	}
	if p.OU == "" {
		return errors.New("ad: OU parente obligatoire")
	}
	if p.GroupType != "security" && p.GroupType != "distribution" {
		return errors.New("ad: GroupType doit être 'security' ou 'distribution'")
	}
	if p.Scope != "" && p.Scope != "global" && p.Scope != "domainlocal" && p.Scope != "universal" {
		return errors.New("ad: Scope doit être 'global', 'domainlocal' ou 'universal'")
	}
	return nil
}

// porteeGroupe retourne la portée effective (défaut : globale).
func (p *GroupCreateParams) porteeGroupe() string {
	if p.Scope == "" {
		return "global"
	}
	return p.Scope
}

// GroupTypeValue retourne la valeur groupType pour LDAP.
// 2 = Global Security, 8 = Universal Security, 4 = Domain Local Security
// Pour distribution : même valeurs sans le bit SECURITY_ENABLED (0x80000000).
func GroupTypeValue(groupType string, scope string) int {
	// Par défaut : groupe de sécurité global (2)
	val := 2 | 0x80000000 // SECURITY_ENABLED
	if groupType == "distribution" {
		val = 2 // sans SECURITY_ENABLED
	}
	if scope == "universal" {
		val = (val &^ 0xF) | 8
	} else if scope == "domainlocal" {
		val = (val &^ 0xF) | 4
	}
	return val
}

// GroupCreateSimulate simule la création d'un groupe.
func (p *Pool) GroupCreateSimulate(ctx context.Context, params GroupCreateParams) (*SimulateResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	filter := fmt.Sprintf("(&(objectClass=group)(cn=%s))", ldap.EscapeFilter(params.CN))
	req := ldap.NewSearchRequest(params.OU, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 1, 0, false, filter, []string{"dn"}, nil)
	res, err := conn.Search(req)
	if err != nil {
		return &SimulateResult{
			Action:       "create_group",
			TargetDN:     "",
			WouldSucceed: false,
			Error:        fmt.Sprintf("recherche existence: %v", err),
		}, nil
	}

	changes := []string{
		fmt.Sprintf("Créer groupe: CN=%s,%s", params.CN, params.OU),
		fmt.Sprintf("  sAMAccountName: %s", params.SAMAccountName),
		fmt.Sprintf("  groupType: %s (%d)", params.porteeGroupe(), GroupTypeValue(params.GroupType, params.porteeGroupe())),
	}
	if params.Description != "" {
		changes = append(changes, fmt.Sprintf("  description: %s", params.Description))
	}
	for _, m := range params.Members {
		changes = append(changes, fmt.Sprintf("  Ajouter membre: %s", m))
	}

	if len(res.Entries) > 0 {
		return &SimulateResult{
			Action:       "create_group",
			TargetDN:     res.Entries[0].DN,
			Changes:      changes,
			WouldSucceed: false,
			Error:        "un groupe avec ce CN existe déjà dans cette OU",
		}, nil
	}

	return &SimulateResult{
		Action:       "create_group",
		TargetDN:     fmt.Sprintf("CN=%s,%s", params.CN, params.OU),
		Changes:      changes,
		WouldSucceed: true,
	}, nil
}

// GroupCreate crée réellement le groupe.
func (p *Pool) GroupCreate(ctx context.Context, params GroupCreateParams) (*Group, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}
	if err := p.verifier("create_group", fmt.Sprintf("CN=%s,%s", params.CN, params.OU), params.OU); err != nil {
		return nil, err
	}

	sim, err := p.GroupCreateSimulate(ctx, params)
	if err != nil {
		return nil, err
	}
	if !sim.WouldSucceed {
		return nil, errors.New(sim.Error)
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	dn := fmt.Sprintf("CN=%s,%s", params.CN, params.OU)

	attrs := map[string][]string{
		"objectClass":    {"top", "group"},
		"cn":             {params.CN},
		"sAMAccountName": {params.SAMAccountName},
		"groupType":      {fmt.Sprint(GroupTypeValue(params.GroupType, params.porteeGroupe()))},
	}
	if params.Description != "" {
		attrs["description"] = []string{params.Description}
	}

	addReq := ldap.NewAddRequest(dn, nil)
	for k, v := range attrs {
		addReq.Attribute(k, v)
	}

	if err := conn.Add(addReq); err != nil {
		return nil, fmt.Errorf("ad: création groupe %s: %w", dn, err)
	}

	// Ajouter les membres initiaux
	for _, memberDN := range params.Members {
		modReq := ldap.NewModifyRequest(dn, nil)
		modReq.Add("member", []string{memberDN})
		if err := conn.Modify(modReq); err != nil {
			fmt.Printf("ad: avertissement: ajout membre %s au groupe %s: %v\n", memberDN, dn, err)
		}
	}

	return p.GroupGet(ctx, dn)
}

// GroupGet récupère un groupe par son DN.
func (p *Pool) GroupGet(ctx context.Context, dn string) (*Group, error) {
	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	req := ldap.NewSearchRequest(dn, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=group)",
		[]string{"dn", "cn", "sAMAccountName", "description", "member", "groupType", "distinguishedName"},
		nil)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ad: lecture groupe %s: %w", dn, err)
	}
	if len(res.Entries) == 0 {
		return nil, fmt.Errorf("ad: groupe introuvable: %s", dn)
	}
	return entryToGroup(res.Entries[0])
}

// entryToGroup convertit une entrée LDAP en Group.
func entryToGroup(e *ldap.Entry) (*Group, error) {
	return &Group{
		DN:             e.DN,
		CN:             e.GetAttributeValue("cn"),
		SAMAccountName: e.GetAttributeValue("sAMAccountName"),
		Description:    e.GetAttributeValue("description"),
		Members:        e.GetAttributeValues("member"),
		GroupType:      e.GetAttributeValue("groupType"),
		OU:             parentDN(e.DN),
	}, nil
}

// GroupAddMember ajoute un membre à un groupe.
func (p *Pool) GroupAddMember(ctx context.Context, groupDN, memberDN string) error {
	if err := p.verifier("manage_groups", groupDN); err != nil {
		return err
	}
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	modReq := ldap.NewModifyRequest(groupDN, nil)
	modReq.Add("member", []string{memberDN})
	if err := conn.Modify(modReq); err != nil {
		return fmt.Errorf("ad: ajout membre %s au groupe %s: %w", memberDN, groupDN, err)
	}
	return nil
}

// GroupRemoveMember retire un membre d'un groupe.
func (p *Pool) GroupRemoveMember(ctx context.Context, groupDN, memberDN string) error {
	if err := p.verifier("manage_groups", groupDN); err != nil {
		return err
	}
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	modReq := ldap.NewModifyRequest(groupDN, nil)
	modReq.Delete("member", []string{memberDN})
	if err := conn.Modify(modReq); err != nil {
		return fmt.Errorf("ad: retrait membre %s du groupe %s: %w", memberDN, groupDN, err)
	}
	return nil
}

// GroupDelete supprime un groupe.
func (p *Pool) GroupDelete(ctx context.Context, dn string) error {
	if err := p.verifier("delete_group", dn); err != nil {
		return err
	}
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	delReq := ldap.NewDelRequest(dn, nil)
	if err := conn.Del(delReq); err != nil {
		return fmt.Errorf("ad: suppression groupe %s: %w", dn, err)
	}
	return nil
}

// GroupSearch recherche des groupes.
func (p *Pool) GroupSearch(ctx context.Context, ou, filter string, attrs []string, pageSize int) ([]*Group, error) {
	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	if filter == "" {
		filter = "(objectClass=group)"
	}
	if len(attrs) == 0 {
		attrs = []string{"dn", "cn", "sAMAccountName", "description", "member", "groupType"}
	}

	var allGroups []*Group
	pageSize = max(pageSize, 100)

	for {
		req := ldap.NewSearchRequest(ou, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filter, attrs, nil)
		if pageSize > 0 {
			req.Controls = []ldap.Control{ldap.NewControlPaging(uint32(pageSize))}
		}

		res, err := conn.Search(req)
		if err != nil {
			return nil, fmt.Errorf("ad: recherche groupes dans %s: %w", ou, err)
		}

		for _, entry := range res.Entries {
			g, err := entryToGroup(entry)
			if err != nil {
				continue
			}
			allGroups = append(allGroups, g)
		}

		var cookie string
		for _, ctrl := range res.Controls {
			if paging, ok := ctrl.(*ldap.ControlPaging); ok {
				cookie = string(paging.Cookie)
				break
			}
		}
		if cookie == "" {
			break
		}
	}

	return allGroups, nil
}
