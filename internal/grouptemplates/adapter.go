package grouptemplates

import (
	"context"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// PoolAnnuaire relie un pool AD (`*ad.Pool`) à l'interface `Annuaire` attendue
// par `Instancier`. Le paquet `internal/ad` n'importe pas `grouptemplates`, il
// n'y a donc pas de cycle d'import.
type PoolAnnuaire struct {
	Pool *ad.Pool
}

// NouvelAnnuaire construit un adaptateur autour d'un pool AD.
func NouvelAnnuaire(pool *ad.Pool) *PoolAnnuaire {
	return &PoolAnnuaire{Pool: pool}
}

// ListerOUEnfants retourne les DN des OU filles de parentDN.
func (a *PoolAnnuaire) ListerOUEnfants(ctx context.Context, parentDN string) ([]string, error) {
	ous, err := a.Pool.OUSearch(ctx, parentDN, "")
	if err != nil {
		return nil, err
	}
	dns := make([]string, 0, len(ous))
	for _, ou := range ous {
		dns = append(dns, ou.DN)
	}
	return dns, nil
}

// CreerOU crée l'OU nommée rdn sous parentDN.
func (a *PoolAnnuaire) CreerOU(ctx context.Context, parentDN, rdn string) error {
	_, err := a.Pool.OUCreate(ctx, ad.OUCreateParams{Name: rdn, ParentDN: parentDN})
	return err
}

// ListerGroupes retourne les groupes présents dans un conteneur.
func (a *PoolAnnuaire) ListerGroupes(ctx context.Context, conteneur string) ([]GroupeExistant, error) {
	groupes, err := a.Pool.GroupSearch(ctx, conteneur, "", []string{"dn", "sAMAccountName"}, 0)
	if err != nil {
		return nil, err
	}
	resultat := make([]GroupeExistant, 0, len(groupes))
	for _, groupe := range groupes {
		resultat = append(resultat, GroupeExistant{DN: groupe.DN, SAM: groupe.SAMAccountName})
	}
	return resultat, nil
}

// CreerGroupe crée un groupe de sécurité avec la portée demandée.
func (a *PoolAnnuaire) CreerGroupe(ctx context.Context, dn, sam, portee string) error {
	cn := nomRelatifDN(dn)
	_, err := a.Pool.GroupCreate(ctx, ad.GroupCreateParams{
		CN:             cn,
		SAMAccountName: sam,
		OU:             parentDeDN(dn),
		GroupType:      "security",
		Scope:          portee,
	})
	return err
}

// nomRelatifDN extrait la valeur du premier composant d'un DN (`cn=x` → `x`).
func nomRelatifDN(dn string) string {
	index := premierSeparateur(dn)
	if index < 0 {
		return valeurPremierComposant(dn)
	}
	return valeurPremierComposant(dn[:index])
}

// parentDeDN retourne le DN privé de son premier composant.
func parentDeDN(dn string) string {
	index := premierSeparateur(dn)
	if index < 0 {
		return ""
	}
	return dn[index+1:]
}

// premierSeparateur retourne l'index de la première virgule non échappée.
func premierSeparateur(dn string) int {
	echappe := false
	for index, r := range dn {
		if echappe {
			echappe = false
			continue
		}
		if r == '\\' {
			echappe = true
			continue
		}
		if r == ',' {
			return index
		}
	}
	return -1
}

// valeurPremierComposant retourne la partie après le premier « = ».
func valeurPremierComposant(composant string) string {
	for index := 0; index < len(composant); index++ {
		if composant[index] == '=' {
			return decomposerEchappement(composant[index+1:])
		}
	}
	return composant
}

// decomposerEchappement retire l'échappement RFC 4514 (utile pour les CN
// contenant « , » ou « \ »).
func decomposerEchappement(valeur string) string {
	if len(valeur) == 0 {
		return valeur
	}
	resultat := make([]rune, 0, len(valeur))
	echappe := false
	for _, r := range valeur {
		if echappe {
			resultat = append(resultat, r)
			echappe = false
			continue
		}
		if r == '\\' {
			echappe = true
			continue
		}
		resultat = append(resultat, r)
	}
	return string(resultat)
}
