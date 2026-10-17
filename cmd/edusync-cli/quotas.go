package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdQuotas gère les commandes liées aux quotas de disque (M14).
func cmdQuotas(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate » ou « execute »")}
	}
	switch args[0] {
	case "simulate":
		return cmdQuotasSimulate(args[1:], stdout)
	case "execute":
		return cmdQuotasExecute(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate | execute)", args[0])}
	}
}

// cmdQuotasSimulate simule l'application d'un quota (sans écriture AD).
func cmdQuotasSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("quotas simulate", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	ou := fs.String("ou", "", "DN de l'OU cible (vide = quota utilisateur)")
	typ := fs.String("type", "disque", "type de quota : disque ou fichiers")
	limite := fs.Int64("limite", 0, "limite en octets (disque) ou en fichiers")
	seuil := fs.String("seuil", "", "seuil : alerte ou blocage")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" && *ou == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn ou --ou est obligatoire")}
	}

	params := ad.QuotaParams{
		UserDN: *userDN,
		OU:     *ou,
		Type:   ad.QuotaType(*typ),
		Limite: *limite,
		Seuil:  ad.QuotaSeuil(*seuil),
	}

	result := ad.QuotaResult{
		UserDN:   params.UserDN,
		OU:       params.OU,
		Type:     params.Type,
		Limite:   params.Limite,
		Seuil:    params.Seuil,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour vérifier l'existence du compte",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "OU          : %s\n", result.OU)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Limite      : %s\n", ad.FormateTaille(params.Limite))
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}

// cmdQuotasExecute applique un quota (écrit msDS-QuotaAmount dans l'AD).
func cmdQuotasExecute(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("quotas execute", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	ou := fs.String("ou", "", "DN de l'OU cible (vide = quota utilisateur)")
	typ := fs.String("type", "disque", "type de quota : disque ou fichiers")
	limite := fs.Int64("limite", 0, "limite en octets (disque) ou en fichiers")
	seuil := fs.String("seuil", "", "seuil : alerte ou blocage")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" && *ou == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn ou --ou est obligatoire")}
	}

	params := ad.QuotaParams{
		UserDN: *userDN,
		OU:     *ou,
		Type:   ad.QuotaType(*typ),
		Limite: *limite,
		Seuil:  ad.QuotaSeuil(*seuil),
	}

	result := ad.QuotaResult{
		UserDN:   params.UserDN,
		OU:       params.OU,
		Type:     params.Type,
		Limite:   params.Limite,
		Seuil:    params.Seuil,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour appliquer le quota",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "OU          : %s\n", result.OU)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Limite      : %s\n", ad.FormateTaille(params.Limite))
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}
