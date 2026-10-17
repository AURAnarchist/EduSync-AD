package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// Collecteur rassemble l'inventaire de l'annuaire analysé par l'assistant.
type Collecteur interface {
	// Collecter retourne l'instantané de l'annuaire sous `baseDN`
	// (vide = racine du domaine).
	Collecter(ctx context.Context, baseDN string) (Inventaire, error)
}

// CollecteurAD collecte l'inventaire depuis un pool Active Directory.
type CollecteurAD struct {
	pool *ad.Pool
}

// NouveauCollecteurAD retourne un collecteur adossé à un pool AD.
func NouveauCollecteurAD(pool *ad.Pool) *CollecteurAD {
	return &CollecteurAD{pool: pool}
}

// Collecter interroge l'annuaire : comptes, groupes et unités d'organisation.
//
// Les OU sont parcourues récursivement : `OUSearch` n'interroge qu'un niveau,
// or la détection des OU vides et des comptes orphelins a besoin de tout
// l'arbre.
func (c *CollecteurAD) Collecter(ctx context.Context, baseDN string) (Inventaire, error) {
	if c.pool == nil {
		return Inventaire{}, fmt.Errorf("assistant: pool AD absent")
	}
	if baseDN == "" {
		baseDN = c.pool.BaseDN()
	}
	if baseDN == "" {
		return Inventaire{}, fmt.Errorf("assistant: base DN introuvable")
	}

	utilisateurs, err := c.pool.UserSearch(ctx, baseDN, "", nil, 0)
	if err != nil {
		return Inventaire{}, fmt.Errorf("assistant: collecte des comptes: %w", err)
	}
	groupes, err := c.pool.GroupSearch(ctx, baseDN, "", nil, 0)
	if err != nil {
		return Inventaire{}, fmt.Errorf("assistant: collecte des groupes: %w", err)
	}
	unites, err := c.collecterOUs(ctx, baseDN)
	if err != nil {
		return Inventaire{}, fmt.Errorf("assistant: collecte des OU: %w", err)
	}

	inventaire := Inventaire{BaseDN: baseDN}
	for _, utilisateur := range utilisateurs {
		nomComplet := utilisateur.DisplayName
		if nomComplet == "" {
			nomComplet = utilisateur.CN
		}
		inventaire.Comptes = append(inventaire.Comptes, Compte{
			DN:          utilisateur.DN,
			Identifiant: utilisateur.SAMAccountName,
			NomComplet:  nomComplet,
			Mail:        utilisateur.Email,
			Actif:       utilisateur.Enabled,
			OU:          utilisateur.OU,
			Groupes:     utilisateur.MemberOf,
		})
	}
	for _, groupe := range groupes {
		inventaire.Groupes = append(inventaire.Groupes, Groupe{
			DN:          groupe.DN,
			Nom:         groupe.CN,
			Identifiant: groupe.SAMAccountName,
			Description: groupe.Description,
			Membres:     groupe.Members,
			OU:          groupe.OU,
		})
	}
	for _, unite := range unites {
		nom := unite.Name
		if nom == "" {
			nom = valeurRDN(unite.DN)
		}
		inventaire.Unites = append(inventaire.Unites, Unite{
			DN:       unite.DN,
			Nom:      nom,
			ParentDN: unite.ParentDN,
		})
	}
	return inventaire, nil
}

// collecterOUs parcourt récursivement l'arbre des unités d'organisation.
func (c *CollecteurAD) collecterOUs(ctx context.Context, baseDN string) ([]*ad.OU, error) {
	var collectees []*ad.OU
	pile := []string{baseDN}
	vues := map[string]bool{}

	for len(pile) > 0 {
		parent := pile[len(pile)-1]
		pile = pile[:len(pile)-1]
		if vues[strings.ToLower(parent)] {
			continue
		}
		vues[strings.ToLower(parent)] = true

		unites, err := c.pool.OUSearch(ctx, parent, "")
		if err != nil {
			return nil, err
		}
		for _, unite := range unites {
			collectees = append(collectees, unite)
			pile = append(pile, unite.DN)
		}
	}
	return collectees, nil
}

// valeurRDN retourne la valeur du premier RDN d'un DN (`OU=6e_A,DC=…` → `6e_A`).
func valeurRDN(dn string) string {
	premier := dn
	if index := strings.IndexByte(dn, ','); index >= 0 {
		premier = dn[:index]
	}
	if index := strings.IndexByte(premier, '='); index >= 0 {
		return strings.TrimSpace(premier[index+1:])
	}
	return strings.TrimSpace(premier)
}
