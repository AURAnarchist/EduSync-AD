package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdHomeDirs gère les commandes liées aux dossiers personnels (M17).
func cmdHomeDirs(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate » ou « execute »")}
	}
	switch args[0] {
	case "simulate":
		return cmdHomeDirsSimulate(args[1:], stdout)
	case "execute":
		return cmdHomeDirsExecute(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate | execute)", args[0])}
	}
}

// cmdHomeDirsSimulate simule l'application d'un dossier personnel (sans écriture AD).
func cmdHomeDirsSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("homedirs simulate", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "standard", "type de dossier : standard ou personnalise")
	chemin := fs.String("chemin", "\\\\srv\\homes\\%USERNAME%", "chemin du dossier personnel")
	lettre := fs.String("lettre", "H", "lettre de lecteur")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}

	params := ad.HomeDirParams{
		UserDN:        *userDN,
		Type:          ad.HomeDirType(*typ),
		Chemin:        *chemin,
		LettreLecteur: *lettre,
	}

	result := ad.HomeDirResult{
		UserDN:        params.UserDN,
		Type:          params.Type,
		Chemin:        params.Chemin,
		LettreLecteur: params.LettreLecteur,
		Statut:        "attente_ad",
		Detaille:      "connexion AD requise pour vérifier l'existence du compte",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Chemin      : %s\n", result.Chemin)
	fmt.Fprintf(stdout, "Lecteur     : %s\n", result.LettreLecteur)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}

// cmdHomeDirsExecute applique un dossier personnel (ecrit homeDirectory et homeDrive).
func cmdHomeDirsExecute(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("homedirs execute", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "standard", "type de dossier : standard ou personnalise")
	chemin := fs.String("chemin", "\\\\srv\\homes\\%USERNAME%", "chemin du dossier personnel")
	lettre := fs.String("lettre", "H", "lettre de lecteur")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}

	params := ad.HomeDirParams{
		UserDN:        *userDN,
		Type:          ad.HomeDirType(*typ),
		Chemin:        *chemin,
		LettreLecteur: *lettre,
	}

	result := ad.HomeDirResult{
		UserDN:        params.UserDN,
		Type:          params.Type,
		Chemin:        params.Chemin,
		LettreLecteur: params.LettreLecteur,
		Statut:        "attente_ad",
		Detaille:      "connexion AD requise pour appliquer le dossier personnel",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Chemin      : %s\n", result.Chemin)
	fmt.Fprintf(stdout, "Lecteur     : %s\n", result.LettreLecteur)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}
