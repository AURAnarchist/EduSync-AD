package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdPhotos gère les commandes liées aux photos d'identité (M12).
func cmdPhotos(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate » ou « execute »")}
	}
	switch args[0] {
	case "simulate":
		return cmdPhotosSimulate(args[1:], stdout)
	case "execute":
		return cmdPhotosExecute(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate | execute)", args[0])}
	}
}

// cmdPhotosSimulate simule l'application d'une photo (sans écriture AD).
func cmdPhotosSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("photos simulate", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "vignette", "type de photo : vignette ou passeport")
	chemin := fs.String("chemin", "", "chemin du fichier photo (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}
	if *chemin == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --chemin est obligatoire")}
	}

	params := ad.PhotoParams{
		UserDN: *userDN,
		Type:   ad.PhotoType(*typ),
		Chemin: *chemin,
	}

	result := ad.PhotoResult{
		UserDN:   params.UserDN,
		Type:     params.Type,
		Chemin:   params.Chemin,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour vérifier l'existence du compte",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Chemin      : %s\n", result.Chemin)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}

// cmdPhotosExecute applique une photo (écrit thumbnailPhoto/jpegPhoto dans l'AD).
func cmdPhotosExecute(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("photos execute", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	typ := fs.String("type", "vignette", "type de photo : vignette ou passeport")
	chemin := fs.String("chemin", "", "chemin du fichier photo (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}
	if *chemin == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --chemin est obligatoire")}
	}

	params := ad.PhotoParams{
		UserDN: *userDN,
		Type:   ad.PhotoType(*typ),
		Chemin: *chemin,
	}

	result := ad.PhotoResult{
		UserDN:   params.UserDN,
		Type:     params.Type,
		Chemin:   params.Chemin,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour appliquer la photo",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", result.UserDN)
	fmt.Fprintf(stdout, "Type        : %s\n", result.Type)
	fmt.Fprintf(stdout, "Chemin      : %s\n", result.Chemin)
	fmt.Fprintf(stdout, "Statut      : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", result.Detaille)
	return nil
}
