package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/rbac"
)

// cmdRBAC gère la délégation d'administration (M26).
func cmdRBAC(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « roles », « list », « add », « remove » ou « policy »")}
	}
	switch args[0] {
	case "roles":
		return cmdRBACRoles(args[1:], stdout)
	case "list":
		return cmdRBACList(args[1:], stdout)
	case "add":
		return cmdRBACAdd(args[1:], stdout)
	case "remove":
		return cmdRBACRemove(args[1:], stdout)
	case "policy":
		return cmdRBACPolicy(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// listeDrapeau collecte un drapeau répétable (--ou, --groupe).
type listeDrapeau []string

// String implémente flag.Value.
func (l *listeDrapeau) String() string { return strings.Join(*l, ",") }

// Set ajoute une valeur.
func (l *listeDrapeau) Set(valeur string) error {
	*l = append(*l, valeur)
	return nil
}

func cmdRBACRoles(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rbac roles", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *jsonOut {
		type roleDef struct {
			Role        string   `json:"role"`
			Label       string   `json:"label"`
			Permissions []string `json:"permissions"`
			Scoped      bool     `json:"scoped"`
		}
		roles := make([]roleDef, 0, len(rbac.RoleOrder))
		for _, role := range rbac.RoleOrder {
			perms := rbac.RolePermissions[role]
			liste := make([]string, 0, len(perms))
			for permission := range perms {
				liste = append(liste, permission)
			}
			scoped := false
			for _, s := range rbac.ScopedRoles {
				if s == role {
					scoped = true
				}
			}
			roles = append(roles, roleDef{Role: string(role), Label: rbac.RoleLabel(role), Permissions: liste, Scoped: scoped})
		}
		return ecrireJSON(stdout, roles)
	}
	for _, role := range rbac.RoleOrder {
		fmt.Fprintf(stdout, "%-14s %s (%d permissions)\n", role, rbac.RoleLabel(role), len(rbac.RolePermissions[role]))
	}
	return nil
}

func cmdRBACList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rbac list", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de delegations.json (défaut : partagé)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	grants, err := rbac.ChargerGrants(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, grants)
	}
	for _, grant := range grants {
		etat := "actif"
		if !grant.Actif {
			etat = "inactif"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%d portée(s)\n",
			grant.ID, grant.Operateur, grant.Role, etat, grant.ScopeCount())
	}
	return nil
}

func cmdRBACAdd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rbac add", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de delegations.json (défaut : partagé)")
	operateur := fs.String("operateur", "", "compte de l'opérateur (obligatoire)")
	role := fs.String("role", string(rbac.LectureSeule), "rôle (super_admin, admin_site, admin_classe, helpdesk, lecture_seule)")
	site := fs.String("site", "", "domaine AD concerné (vide = tous les sites)")
	inactif := fs.Bool("inactive", false, "créer le délégué inactif")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	var ous, groupes listeDrapeau
	fs.Var(&ous, "ou", "DN d'OU déléguée (répétable)")
	fs.Var(&groupes, "groupe", "DN de groupe délégué (répétable)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*operateur) == "" {
		return &codeSortie{code: 2, err: errors.New("--operateur est obligatoire")}
	}

	grants, err := rbac.ChargerGrants(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	grant := rbac.Grant{
		ID:        rbac.UniqueGrantID(grants),
		Operateur: *operateur,
		Role:      rbac.Role(*role),
		Site:      *site,
		OUs:       ous,
		Groupes:   groupes,
		Actif:     !*inactif,
	}
	if errs := grant.Validate(); len(errs) > 0 {
		return &codeSortie{code: 2, err: fmt.Errorf("délégué invalide : %s", strings.Join(errs, " "))}
	}
	grants = append(grants, grant)
	if err := rbac.SauvegarderGrants(grants, *fichier); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, grant)
	}
	fmt.Fprintf(stdout, "Délégué ajouté : %s (%s, %s)\n", grant.Operateur, grant.Role, grant.ID)
	return nil
}

func cmdRBACRemove(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rbac remove", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de delegations.json (défaut : partagé)")
	id := fs.String("id", "", "identifiant du délégué (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" {
		return &codeSortie{code: 2, err: errors.New("--id est obligatoire")}
	}
	grants, err := rbac.ChargerGrants(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	restants := make([]rbac.Grant, 0, len(grants))
	supprimes := 0
	for _, grant := range grants {
		if grant.ID == *id {
			supprimes++
			continue
		}
		restants = append(restants, grant)
	}
	if supprimes == 0 {
		return &codeSortie{code: 1, err: fmt.Errorf("aucun délégué avec l'identifiant %s", *id)}
	}
	if err := rbac.SauvegarderGrants(restants, *fichier); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"supprimes": supprimes, "restants": len(restants)})
	}
	fmt.Fprintf(stdout, "Délégué supprimé (%d restant(s)).\n", len(restants))
	return nil
}

func cmdRBACPolicy(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rbac policy", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de delegations.json (défaut : partagé)")
	operateur := fs.String("operateur", "", "compte de l'opérateur (obligatoire)")
	domain := fs.String("domain", "", "domaine AD (site)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*operateur) == "" {
		return &codeSortie{code: 2, err: errors.New("--operateur est obligatoire")}
	}
	policy, err := rbac.BuildPolicy(*operateur, *domain, *fichier)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		perms := make([]string, 0, len(policy.Permissions()))
		for permission := range policy.Permissions() {
			perms = append(perms, permission)
		}
		return ecrireJSON(stdout, map[string]any{
			"operateur":             policy.Operator,
			"domaine":               policy.Domain,
			"role":                  string(policy.Role),
			"label":                 policy.Label(),
			"source":                policy.Source,
			"scoped":                policy.IsScoped(),
			"readonly":              policy.Readonly(),
			"can_manage_delegation": policy.CanManageDelegation(),
			"ous":                   policy.OUs,
			"groupes":               policy.Groupes,
			"permissions":           perms,
		})
	}
	fmt.Fprintln(stdout, policy.Describe())
	return nil
}

// decoderRBACPolicy est un utilitaire de test : décode la sortie JSON de
// « rbac policy ».
func decoderRBACPolicy(donnees []byte) (map[string]any, error) {
	var out map[string]any
	if err := json.Unmarshal(donnees, &out); err != nil {
		return nil, err
	}
	return out, nil
}
