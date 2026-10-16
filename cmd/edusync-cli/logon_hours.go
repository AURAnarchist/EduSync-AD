package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdLogonHours gère les commandes liées aux heures de connexion (M16).
func cmdLogonHours(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate » ou « execute »")}
	}
	switch args[0] {
	case "simulate":
		return cmdLogonHoursSimulate(args[1:], stdout)
	case "execute":
		return cmdLogonHoursExecute(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate | execute)", args[0])}
	}
}

// cmdLogonHoursSimulate simule l'application des heures de connexion (sans écriture AD).
func cmdLogonHoursSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("logon-hours simulate", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	preset := fs.String("preset", "cours", "préréglage : cours, admin ou personnalise")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}

	var heures *ad.LogonHours
	switch *preset {
	case "cours":
		h := ad.HeuresCours()
		heures = &h
	case "admin":
		h := ad.HeuresAdmin()
		heures = &h
	case "personnalise":
		heures = &ad.LogonHours{}
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("préréglage inconnu %q (attendu : cours | admin | personnalise)", *preset)}
	}

	params := ad.LogonHoursParams{
		UserDN: *userDN,
		Heures: heures,
	}

	result := ad.LogonHoursResult{
		UserDN:   params.UserDN,
		Heures:   params.Heures,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour vérifier l'existence du compte",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Préréglage  : %s\n", *preset)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}

// cmdLogonHoursExecute applique les heures de connexion (écrit logonHours dans l'AD).
func cmdLogonHoursExecute(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("logon-hours execute", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	preset := fs.String("preset", "cours", "préréglage : cours, admin ou personnalise")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}

	var heures *ad.LogonHours
	switch *preset {
	case "cours":
		h := ad.HeuresCours()
		heures = &h
	case "admin":
		h := ad.HeuresAdmin()
		heures = &h
	case "personnalise":
		heures = &ad.LogonHours{}
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("préréglage inconnu %q (attendu : cours | admin | personnalise)", *preset)}
	}

	params := ad.LogonHoursParams{
		UserDN: *userDN,
		Heures: heures,
	}

	result := ad.LogonHoursResult{
		UserDN:   params.UserDN,
		Heures:   params.Heures,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour appliquer les heures",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Préréglage  : %s\n", *preset)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}
