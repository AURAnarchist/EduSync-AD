// Package ad fournit les opérations sur les OU (Unités d'Organisation) Active Directory.
package ad

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-ldap/ldap/v3"
)

// OU représente une unité d'organisation.
type OU struct {
	DN          string
	Name        string
	Description string
	ParentDN    string
	GPOLinks    []string
}

// OUCreateParams décrit les paramètres de création d'une OU.
type OUCreateParams struct {
	Name        string
	Description string
	ParentDN    string
	GPOLinks    []string // DN des GPO à lier
}

// Validate vérifie la cohérence.
func (p *OUCreateParams) Validate() error {
	if p.Name == "" {
		return errors.New("ad: nom de l'OU obligatoire")
	}
	if p.ParentDN == "" {
		return errors.New("ad: DN parent obligatoire")
	}
	return nil
}

// OUCreateSimulate simule la création d'une OU.
func (p *Pool) OUCreateSimulate(ctx context.Context, params OUCreateParams) (*SimulateResult, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	filter := fmt.Sprintf("(&(objectClass=organizationalUnit)(ou=%s))", ldap.EscapeFilter(params.Name))
	req := ldap.NewSearchRequest(params.ParentDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 1, 0, false, filter, []string{"dn"}, nil)
	res, err := conn.Search(req)
	if err != nil {
		return &SimulateResult{
			Action:       "create_ou",
			TargetDN:     "",
			WouldSucceed: false,
			Error:        fmt.Sprintf("recherche existence: %v", err),
		}, nil
	}

	changes := []string{
		fmt.Sprintf("Créer OU: OU=%s,%s", params.Name, params.ParentDN),
		fmt.Sprintf("  description: %s", params.Description),
	}
	for _, gpo := range params.GPOLinks {
		changes = append(changes, fmt.Sprintf("  Lier GPO: %s", gpo))
	}

	if len(res.Entries) > 0 {
		return &SimulateResult{
			Action:       "create_ou",
			TargetDN:     res.Entries[0].DN,
			Changes:      changes,
			WouldSucceed: false,
			Error:        "une OU avec ce nom existe déjà à cet emplacement",
		}, nil
	}

	return &SimulateResult{
		Action:       "create_ou",
		TargetDN:     fmt.Sprintf("OU=%s,%s", params.Name, params.ParentDN),
		Changes:      changes,
		WouldSucceed: true,
	}, nil
}

// OUCreate crée réellement l'OU.
func (p *Pool) OUCreate(ctx context.Context, params OUCreateParams) (*OU, error) {
	if err := params.Validate(); err != nil {
		return nil, err
	}

	sim, err := p.OUCreateSimulate(ctx, params)
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

	dn := fmt.Sprintf("OU=%s,%s", params.Name, params.ParentDN)

	attrs := map[string][]string{
		"objectClass": {"top", "organizationalUnit"},
		"ou":          {params.Name},
	}
	if params.Description != "" {
		attrs["description"] = []string{params.Description}
	}

	addReq := ldap.NewAddRequest(dn, nil)
	for k, v := range attrs {
		addReq.Attribute(k, v)
	}

	if err := conn.Add(addReq); err != nil {
		return nil, fmt.Errorf("ad: création OU %s: %w", dn, err)
	}

	// Lier les GPO si fournis
	for _, gpoDN := range params.GPOLinks {
		modReq := ldap.NewModifyRequest(dn, nil)
		modReq.Add("gPLink", []string{gpoDN})
		if err := conn.Modify(modReq); err != nil {
			fmt.Printf("ad: avertissement: lien GPO %s vers OU %s: %v\n", gpoDN, dn, err)
		}
	}

	return p.OUGet(ctx, dn)
}

// OUGet récupère une OU par son DN.
func (p *Pool) OUGet(ctx context.Context, dn string) (*OU, error) {
	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	req := ldap.NewSearchRequest(dn, ldap.ScopeBaseObject, ldap.NeverDerefAliases, 1, 0, false,
		"(objectClass=organizationalUnit)",
		[]string{"dn", "ou", "description", "gPLink", "distinguishedName"},
		nil)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ad: lecture OU %s: %w", dn, err)
	}
	if len(res.Entries) == 0 {
		return nil, fmt.Errorf("ad: OU introuvable: %s", dn)
	}
	return entryToOU(res.Entries[0])
}

// entryToOU convertit une entrée LDAP en OU.
func entryToOU(e *ldap.Entry) (*OU, error) {
	return &OU{
		DN:          e.DN,
		Name:        e.GetAttributeValue("ou"),
		Description: e.GetAttributeValue("description"),
		ParentDN:    parentDN(e.DN),
		GPOLinks:    e.GetAttributeValues("gPLink"),
	}, nil
}

// OUDelete supprime une OU (doit être vide).
func (p *Pool) OUDelete(ctx context.Context, dn string) error {
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	delReq := ldap.NewDelRequest(dn, nil)
	if err := conn.Del(delReq); err != nil {
		return fmt.Errorf("ad: suppression OU %s: %w", dn, err)
	}
	return nil
}

// OUSearch liste les OU sous un parent.
func (p *Pool) OUSearch(ctx context.Context, parentDN, filter string) ([]*OU, error) {
	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	if filter == "" {
		filter = "(objectClass=organizationalUnit)"
	}

	req := ldap.NewSearchRequest(parentDN, ldap.ScopeSingleLevel, ldap.NeverDerefAliases, 0, 0, false, filter,
		[]string{"dn", "ou", "description", "gPLink"}, nil)
	res, err := conn.Search(req)
	if err != nil {
		return nil, fmt.Errorf("ad: recherche OU sous %s: %w", parentDN, err)
	}

	var ous []*OU
	for _, entry := range res.Entries {
		o, err := entryToOU(entry)
		if err != nil {
			continue
		}
		ous = append(ous, o)
	}
	return ous, nil
}

// OUMove déplace une OU vers un autre parent.
func (p *Pool) OUMove(ctx context.Context, dn, newParentDN string) error {
	conn, err := p.Get(ctx)
	if err != nil {
		return err
	}
	defer p.Put(conn)

	modDNReq := ldap.NewModifyDNRequest(dn, fmt.Sprintf("OU=%s", rdnFromDN(dn)), true, newParentDN)
	if err := conn.ModifyDN(modDNReq); err != nil {
		return fmt.Errorf("ad: déplacement OU %s vers %s: %w", dn, newParentDN, err)
	}
	return nil
}
