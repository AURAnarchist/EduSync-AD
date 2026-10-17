package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/multisite"
)

// cmdMultisite gère les commandes multi-site / multi-domaine (M25).
func cmdMultisite(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « list », « add », « remove », « import-legacy » ou « ensure »")}
	}
	switch args[0] {
	case "list":
		return cmdMultisiteList(args[1:], stdout)
	case "add":
		return cmdMultisiteAdd(args[1:], stdout)
	case "remove":
		return cmdMultisiteRemove(args[1:], stdout)
	case "import-legacy":
		return cmdMultisiteImportLegacy(args[1:], stdout)
	case "ensure":
		return cmdMultisiteEnsure(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// optionsMultisite déclare les chemins du fichier de sites et de la clé.
func optionsMultisite(fs *flag.FlagSet) (*string, *string) {
	fichier := fs.String("file", "", "chemin de domaines.json (défaut : partagé)")
	cle := fs.String("key", "", "chemin de la clé secret.key (défaut : partagé)")
	return fichier, cle
}

func cmdMultisiteList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("multisite list", flag.ContinueOnError)
	fichier, cle := optionsMultisite(fs)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	profils, err := multisite.ChargerSites(*fichier, *cle)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, profils)
	}
	for _, profil := range profils {
		fmt.Fprintf(stdout, "%s\t%s\n", profil.ID, profil.DisplayName())
	}
	return nil
}

func cmdMultisiteAdd(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("multisite add", flag.ContinueOnError)
	fichier, cle := optionsMultisite(fs)
	label := fs.String("label", "", "libellé du site (obligatoire)")
	domain := fs.String("domain", "", "nom de domaine (obligatoire)")
	controleur := fs.String("controller", "", "contrôleur de domaine")
	utilisateur := fs.String("username", "", "identifiant de connexion")
	motDePasse := fs.String("password", "", "mot de passe (si mémorisé)")
	memoriser := fs.Bool("remember", false, "mémoriser le mot de passe")
	verifierCert := fs.Bool("verify-cert", true, "vérifier le certificat du contrôleur")
	caCert := fs.String("ca-cert", "", "chemin du certificat d'autorité")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*label) == "" || strings.TrimSpace(*domain) == "" {
		return &codeSortie{code: 2, err: errors.New("--label et --domain sont obligatoires")}
	}

	profils, err := multisite.ChargerSites(*fichier, *cle)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	profil := multisite.NouveauProfil(*label, *domain,
		multisite.AvecControleur(*controleur),
		multisite.AvecIdentifiant(*utilisateur),
		multisite.AvecMotDePasse(*motDePasse, *memoriser),
		multisite.AvecVerificationCertificat(*verifierCert),
		multisite.AvecCACert(*caCert),
		multisite.AvecExistants(profils),
	)
	if errs := profil.Validate(); len(errs) > 0 {
		return &codeSortie{code: 2, err: fmt.Errorf("profil invalide : %s", strings.Join(errs, " "))}
	}
	profils = append(profils, profil)
	if err := multisite.SauvegarderSites(profils, *fichier, *cle); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, profil)
	}
	fmt.Fprintf(stdout, "Site ajouté : %s (%s)\n", profil.DisplayName(), profil.ID)
	return nil
}

func cmdMultisiteRemove(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("multisite remove", flag.ContinueOnError)
	fichier, cle := optionsMultisite(fs)
	id := fs.String("id", "", "identifiant du site (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" {
		return &codeSortie{code: 2, err: errors.New("--id est obligatoire")}
	}
	profils, err := multisite.ChargerSites(*fichier, *cle)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	restants := make([]multisite.ProfilDomaine, 0, len(profils))
	supprimes := 0
	for _, profil := range profils {
		if profil.ID == *id {
			supprimes++
			continue
		}
		restants = append(restants, profil)
	}
	if supprimes == 0 {
		return &codeSortie{code: 1, err: fmt.Errorf("aucun site avec l'identifiant %s", *id)}
	}
	if err := multisite.SauvegarderSites(restants, *fichier, *cle); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"supprimes": supprimes, "restants": len(restants)})
	}
	fmt.Fprintf(stdout, "Site supprimé (%d restant).\n", len(restants))
	return nil
}

func cmdMultisiteImportLegacy(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("multisite import-legacy", flag.ContinueOnError)
	fichier, cle := optionsMultisite(fs)
	herite := fs.String("legacy", "", "chemin de connection.local.json (défaut : partagé)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	profil, err := multisite.ImporterConnexionHeritee(*fichier, *cle, *herite)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if profil == nil {
		if *jsonOut {
			return ecrireJSON(stdout, nil)
		}
		fmt.Fprintln(stdout, "Aucune connexion mémorisée à importer.")
		return nil
	}
	profils, err := multisite.ChargerSites(*fichier, *cle)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	profils = append(profils, *profil)
	if err := multisite.SauvegarderSites(profils, *fichier, *cle); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, *profil)
	}
	fmt.Fprintf(stdout, "Site importé : %s (%s)\n", profil.DisplayName(), profil.ID)
	return nil
}

func cmdMultisiteEnsure(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("multisite ensure", flag.ContinueOnError)
	fichier, cle := optionsMultisite(fs)
	herite := fs.String("legacy", "", "chemin de connection.local.json (défaut : partagé)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	profils := multisite.AssurerSites(*fichier, *cle, *herite, func(message string) {
		fmt.Fprintln(stdout, message)
	})
	if *jsonOut {
		return ecrireJSON(stdout, profils)
	}
	for _, profil := range profils {
		fmt.Fprintf(stdout, "%s\t%s\n", profil.ID, profil.DisplayName())
	}
	return nil
}
