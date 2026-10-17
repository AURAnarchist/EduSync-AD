package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/classspaces"
)

// cmdClassSpaces gère les commandes des espaces partagés par classe (M18).
func cmdClassSpaces(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « plan », « creation-script », « sync-script », « notify » ou « config »")}
	}
	switch args[0] {
	case "plan":
		return cmdClassSpacesPlan(args[1:], stdout)
	case "creation-script":
		return cmdClassSpacesCreationScript(args[1:], stdout)
	case "sync-script":
		return cmdClassSpacesSyncScript(args[1:], stdout)
	case "notify":
		return cmdClassSpacesNotify(args[1:], stdout)
	case "config":
		return cmdClassSpacesConfig(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// chargerConfigClassSpaces charge la configuration partagée, avec surcharge
// éventuelle du partage racine et du modèle de dossier.
func chargerConfigClassSpaces(racine, modele string) classspaces.Config {
	cfg := classspaces.ConfigParDefaut()
	if dir, err := classspaces.DossierConfig(); err == nil {
		cfg = classspaces.ChargerConfig(dir)
	}
	if racine != "" {
		cfg.PartageRacine = racine
	}
	if modele != "" {
		cfg.ModeleDossier = modele
	}
	return cfg
}

// cmdClassSpacesPlan simule la construction d'un plan d'espace de classe.
func cmdClassSpacesPlan(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("class-spaces plan", flag.ContinueOnError)
	groupDN := fs.String("group-dn", "", "DN du groupe de classe (obligatoire)")
	groupName := fs.String("group-name", "", "nom du groupe (obligatoire)")
	racine := fs.String("share-root", "", "partage racine UNC (défaut : configuration)")
	modele := fs.String("modele", "", "modèle de dossier (défaut : configuration)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *groupDN == "" || *groupName == "" {
		return &codeSortie{code: 2, err: errors.New("les options --group-dn et --group-name sont obligatoires")}
	}

	cfg := chargerConfigClassSpaces(*racine, *modele)
	plan := classspaces.Plan{
		GroupDN:   *groupDN,
		GroupName: *groupName,
		Path:      cfg.ResolvePath(*groupName),
	}

	type resultat struct {
		GroupDN   string
		GroupName string
		Path      string
		ShareName string
		Statut    string
		Detaille  string
	}
	result := resultat{
		GroupDN:   plan.GroupDN,
		GroupName: plan.GroupName,
		Path:      plan.Path,
		ShareName: plan.ShareName(),
		Statut:    "attente_ad",
		Detaille:  "connexion AD requise pour lister et classer les membres",
	}

	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Groupe    : %s\n", result.GroupName)
	fmt.Fprintf(stdout, "DN        : %s\n", result.GroupDN)
	fmt.Fprintf(stdout, "Chemin    : %s\n", result.Path)
	fmt.Fprintf(stdout, "Partage   : %s\n", result.ShareName)
	fmt.Fprintf(stdout, "Statut    : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail    : %s\n", result.Detaille)
	return nil
}

// cmdClassSpacesCreationScript génère le script PowerShell de création
// (dossier + partage SMB + droits NTFS).
func cmdClassSpacesCreationScript(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("class-spaces creation-script", flag.ContinueOnError)
	groupDN := fs.String("group-dn", "", "DN du groupe de classe (optionnel)")
	groupName := fs.String("group-name", "", "nom du groupe (obligatoire)")
	racine := fs.String("share-root", "", "partage racine UNC (défaut : configuration)")
	modele := fs.String("modele", "", "modèle de dossier (défaut : configuration)")
	domaine := fs.String("domaine", "", "domaine pour préfixer les ACE (ex : lycee.local)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *groupName == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --group-name est obligatoire")}
	}

	cfg := chargerConfigClassSpaces(*racine, *modele)
	if errs := cfg.Validate(); len(errs) > 0 {
		return &codeSortie{code: 2, err: fmt.Errorf("configuration invalide : %v", errs)}
	}
	plan := classspaces.Plan{
		GroupDN:   *groupDN,
		GroupName: *groupName,
		Path:      cfg.ResolvePath(*groupName),
	}
	fmt.Fprint(stdout, classspaces.ScriptCreation([]classspaces.Plan{plan}, *domaine))
	return nil
}

// cmdClassSpacesSyncScript génère le script PowerShell de synchronisation
// membres AD ↔ droits NTFS.
func cmdClassSpacesSyncScript(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("class-spaces sync-script", flag.ContinueOnError)
	groupDN := fs.String("group-dn", "", "DN du groupe de classe (obligatoire)")
	groupName := fs.String("group-name", "", "nom du groupe (obligatoire)")
	chemin := fs.String("chemin", "", "chemin UNC de l'espace (défaut : configuration)")
	racine := fs.String("share-root", "", "partage racine UNC (défaut : configuration)")
	modele := fs.String("modele", "", "modèle de dossier (défaut : configuration)")
	domaine := fs.String("domaine", "", "domaine pour préfixer les ACE")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *groupDN == "" || *groupName == "" {
		return &codeSortie{code: 2, err: errors.New("les options --group-dn et --group-name sont obligatoires")}
	}

	cfg := chargerConfigClassSpaces(*racine, *modele)
	path := *chemin
	if path == "" {
		path = cfg.ResolvePath(*groupName)
	}
	plan := classspaces.Plan{GroupDN: *groupDN, GroupName: *groupName, Path: path}
	fmt.Fprint(stdout, classspaces.ScriptSync(plan, *domaine, cfg.GroupeProfs, cfg.OUProfs))
	return nil
}

// cmdClassSpacesNotify enregistre un espace comme en attente à la création d'un
// groupe de classe (hook « création auto »).
func cmdClassSpacesNotify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("class-spaces notify", flag.ContinueOnError)
	groupDN := fs.String("group-dn", "", "DN du groupe de classe (obligatoire)")
	groupName := fs.String("group-name", "", "nom du groupe (déduit du DN si absent)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *groupDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --group-dn est obligatoire")}
	}

	dir, err := classspaces.DossierConfig()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	path, actif := classspaces.NotifierCreationGroupe(dir, *groupDN, *groupName)

	type resultat struct {
		GroupDN string
		Path    string
		Actif   bool
	}
	result := resultat{GroupDN: *groupDN, Path: path, Actif: actif}
	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	if !actif {
		fmt.Fprintln(stdout, "Création automatique désactivée : aucune action.")
		return nil
	}
	fmt.Fprintf(stdout, "Espace enregistré en attente : %s\n", path)
	return nil
}

// cmdClassSpacesConfig affiche la configuration des espaces de classe.
func cmdClassSpacesConfig(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("class-spaces config", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	dir, err := classspaces.DossierConfig()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	cfg := classspaces.ChargerConfig(dir)
	if *jsonOut {
		return ecrireJSON(stdout, cfg)
	}
	fmt.Fprintf(stdout, "Partage racine     : %s\n", cfg.PartageRacine)
	fmt.Fprintf(stdout, "Modèle de dossier  : %s\n", cfg.ModeleDossier)
	fmt.Fprintf(stdout, "Groupe des profs   : %s\n", cfg.GroupeProfs)
	fmt.Fprintf(stdout, "OU des profs       : %s\n", cfg.OUProfs)
	fmt.Fprintf(stdout, "Auto sur création  : %v\n", cfg.AutoSurCreation)
	return nil
}
