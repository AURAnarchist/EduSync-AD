package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdLogonScripts gère les commandes liées aux scripts logon/logoff (M15).
func cmdLogonScripts(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate » ou « execute »")}
	}
	switch args[0] {
	case "simulate":
		return cmdLogonScriptsSimulate(args[1:], stdout)
	case "execute":
		return cmdLogonScriptsExecute(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate | execute)", args[0])}
	}
}

// cmdLogonScriptsSimulate simule l'application d'un script (sans écriture AD).
func cmdLogonScriptsSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("logon-scripts simulate", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "bat", "type de script : bat, powershell ou vbs")
	contenu := fs.String("contenu", "", "contenu du script (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}
	if *contenu == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --contenu est obligatoire")}
	}

	params := ad.ScriptParams{
		UserDN:  *userDN,
		Type:    ad.ScriptType(*typ),
		Contenu: *contenu,
	}

	result := ad.ScriptResult{
		UserDN:   params.UserDN,
		Type:     params.Type,
		Contenu:  params.Contenu,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour vérifier l'existence du compte",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}

// cmdLogonScriptsExecute applique un script (écrit scriptPath dans l'AD).
func cmdLogonScriptsExecute(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("logon-scripts execute", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "bat", "type de script : bat, powershell ou vbs")
	contenu := fs.String("contenu", "", "contenu du script (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}
	if *contenu == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --contenu est obligatoire")}
	}

	params := ad.ScriptParams{
		UserDN:  *userDN,
		Type:    ad.ScriptType(*typ),
		Contenu: *contenu,
	}

	result := ad.ScriptResult{
		UserDN:   params.UserDN,
		Type:     params.Type,
		Contenu:  params.Contenu,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour appliquer le script",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}
