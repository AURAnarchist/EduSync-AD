package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdProfils gère les commandes liées aux profils utilisateurs (M13).
func cmdProfils(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate » ou « execute »")}
	}
	switch args[0] {
	case "simulate":
		return cmdProfilsSimulate(args[1:], stdout)
	case "execute":
		return cmdProfilsExecute(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate | execute)", args[0])}
	}
}

// cmdProfilsSimulate simule l'application d'un profil utilisateur (sans écriture AD).
func cmdProfilsSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("profils simulate", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "itinérant", "type de profil : itinérant, local ou obligatoire")
	chemin := fs.String("chemin", "", "chemin du profil (ex: \\\\srv\\profil\\%USERNAME%)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}

	params := ad.ProfileParams{
		UserDN: *userDN,
		Type:   ad.ProfileType(*typ),
		Chemin: *chemin,
	}

	// Sans pool AD, on ne peut pas vérifier l'existence de l'utilisateur.
	// On signale que la simulation nécessite une connexion AD.
	result := ad.ProfileResult{
		UserDN:        params.UserDN,
		Type:          params.Type,
		CheminResolve: resoudreVariablesCLI(params.Chemin),
		Statut:        "attente_ad",
		Detaille:      "connexion AD requise pour vérifier l'existence du compte",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Chemin      : %s\n", result.CheminResolve)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}

// cmdProfilsExecute applique un profil utilisateur (écrit profilePath dans l'AD).
func cmdProfilsExecute(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("profils execute", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "itinérant", "type de profil : itinérant, local ou obligatoire")
	chemin := fs.String("chemin", "", "chemin du profil (ex: \\\\srv\\profil\\%USERNAME%)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}

	params := ad.ProfileParams{
		UserDN: *userDN,
		Type:   ad.ProfileType(*typ),
		Chemin: *chemin,
	}

	// Sans pool AD, on ne peut pas exécuter sur l'AD.
	result := ad.ProfileResult{
		UserDN:        params.UserDN,
		Type:          params.Type,
		CheminResolve: resoudreVariablesCLI(params.Chemin),
		Statut:        "attente_ad",
		Detaille:      "connexion AD requise pour appliquer le profil",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Chemin      : %s\n", result.CheminResolve)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}

// resoudreVariablesCLI remplace %USERNAME%, %SAM% et %OU% dans un chemin.
// Sans contexte AD (pas de pool), %OU% et %USERNAME% restent tels quels.
func resoudreVariablesCLI(chemin string) string {
	// Sans pool AD, on ne peut pas résoudre %USERNAME%, %SAM% ni %OU%.
	// On retourne le chemin tel quel pour que l'UI l'affiche.
	return chemin
}
