package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/grouptemplates"
)

// cmdGroupTemplates gère les modèles de groupes (M23).
func cmdGroupTemplates(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « list », « show », « validate », « duplicate », « remove », « export », « import », « builtins » ou « plan »")}
	}
	switch args[0] {
	case "list":
		return cmdGroupTemplatesList(args[1:], stdout)
	case "show":
		return cmdGroupTemplatesShow(args[1:], stdout)
	case "validate":
		return cmdGroupTemplatesValidate(args[1:], stdout)
	case "duplicate":
		return cmdGroupTemplatesDuplicate(args[1:], stdout)
	case "remove":
		return cmdGroupTemplatesRemove(args[1:], stdout)
	case "export":
		return cmdGroupTemplatesExport(args[1:], stdout)
	case "import":
		return cmdGroupTemplatesImport(args[1:], stdout)
	case "builtins":
		return cmdGroupTemplatesBuiltins(args[1:], stdout)
	case "plan":
		return cmdGroupTemplatesPlan(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// chargerModelesGT charge les modèles depuis le dossier de configuration.
func chargerModelesGT() (string, []grouptemplates.ModeleGroupe, error) {
	dossier, err := config.ConfigDir()
	if err != nil {
		return "", nil, err
	}
	return dossier, grouptemplates.ChargerModeles(grouptemplates.CheminParDefaut(dossier)), nil
}

// trouverModeleGT retourne le modèle d'identifiant donné.
func trouverModeleGT(modeles []grouptemplates.ModeleGroupe, id string) (grouptemplates.ModeleGroupe, bool) {
	for _, modele := range modeles {
		if modele.ID == id {
			return modele, true
		}
	}
	return grouptemplates.ModeleGroupe{}, false
}

// analyserValeurs transforme « cle=valeur » en dictionnaire de motifs.
func analyserValeurs(entrees []string) (map[string]string, error) {
	valeurs := map[string]string{}
	for _, entree := range entrees {
		parties := strings.SplitN(entree, "=", 2)
		if len(parties) != 2 || strings.TrimSpace(parties[0]) == "" {
			return nil, fmt.Errorf("grouptemplates: valeur attendue au format champ=valeur (reçu %q)", entree)
		}
		valeurs[strings.TrimSpace(parties[0])] = parties[1]
	}
	return valeurs, nil
}

func cmdGroupTemplatesList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates list", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	_, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}

	type resumeModele struct {
		ID       string   `json:"id"`
		Nom      string   `json:"name"`
		Type     string   `json:"kind"`
		Groupes  int      `json:"groups"`
		Champs   []string `json:"placeholders"`
		Heures   string   `json:"hours_preset"`
		Licences int      `json:"licenses"`
		OUMotif  string   `json:"ou_rdn_pattern"`
	}
	resumes := make([]resumeModele, 0, len(modeles))
	for _, modele := range modeles {
		resumes = append(resumes, resumeModele{
			ID:       modele.ID,
			Nom:      modele.Nom,
			Type:     modele.Type,
			Groupes:  len(modele.Groupes),
			Champs:   modele.Placeholders(),
			Heures:   modele.PrereglageHeure,
			Licences: len(modele.Licences),
			OUMotif:  modele.MotifOU,
		})
	}

	if *jsonOut {
		return ecrireJSON(stdout, resumes)
	}
	for _, resume := range resumes {
		fmt.Fprintf(stdout, "%s\t%s\t%d groupe(s)\tmotif OU : %s\n",
			resume.ID, resume.Nom, resume.Groupes, resume.OUMotif)
	}
	return nil
}

func cmdGroupTemplatesShow(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates show", flag.ContinueOnError)
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *id == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --id est obligatoire")}
	}

	_, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}
	modele, ok := trouverModeleGT(modeles, *id)
	if !ok {
		return &codeSortie{code: 2, err: fmt.Errorf("modèle inconnu %q", *id)}
	}
	if *jsonOut {
		return ecrireJSON(stdout, modele.ToDict())
	}
	fmt.Fprintf(stdout, "%s — %s (%s)\n", modele.ID, modele.Nom, modele.Type)
	fmt.Fprintf(stdout, "OU : %s\n", modele.MotifOU)
	for _, groupe := range modele.Groupes {
		fmt.Fprintf(stdout, "  groupe : %s (%s)\n", groupe.MotifNom, groupe.Portee)
	}
	fmt.Fprintf(stdout, "Champs : %s\n", strings.Join(modele.Placeholders(), ", "))
	return nil
}

func cmdGroupTemplatesValidate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates validate", flag.ContinueOnError)
	id := fs.String("id", "", "identifiant du modèle (sinon tous)")
	var valeursFlag listeDrapeau
	fs.Var(&valeursFlag, "value", "valeur de motif champ=valeur (répétable)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	valeurs, err := analyserValeurs(valeursFlag)
	if err != nil {
		return &codeSortie{code: 2, err: err}
	}

	_, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}
	if *id != "" {
		modele, ok := trouverModeleGT(modeles, *id)
		if !ok {
			return &codeSortie{code: 2, err: fmt.Errorf("modèle inconnu %q", *id)}
		}
		modeles = []grouptemplates.ModeleGroupe{modele}
	}

	type resultatValidation struct {
		ID      string   `json:"id"`
		Valide  bool     `json:"valid"`
		Erreurs []string `json:"errors"`
	}
	resultats := make([]resultatValidation, 0, len(modeles))
	valideGlobal := true
	for _, modele := range modeles {
		erreurs := append([]string{}, modele.Validate()...)
		erreurs = append(erreurs, grouptemplates.ValiderValeurs(modele, valeurs)...)
		if len(erreurs) > 0 {
			valideGlobal = false
		}
		resultats = append(resultats, resultatValidation{
			ID: modele.ID, Valide: len(erreurs) == 0, Erreurs: erreurs,
		})
	}

	var erreurSortie error
	if !valideGlobal {
		erreurSortie = errors.New("grouptemplates: au moins un modèle est invalide")
	}

	if *jsonOut {
		if err := ecrireJSON(stdout, resultats); err != nil {
			return err
		}
		return erreurSortie
	}
	for _, resultat := range resultats {
		if resultat.Valide {
			fmt.Fprintf(stdout, "OK   %s\n", resultat.ID)
			continue
		}
		fmt.Fprintf(stdout, "ÉCHEC %s\n", resultat.ID)
		for _, erreur := range resultat.Erreurs {
			fmt.Fprintf(stdout, "  - %s\n", erreur)
		}
	}
	return erreurSortie
}

func cmdGroupTemplatesDuplicate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates duplicate", flag.ContinueOnError)
	id := fs.String("id", "", "identifiant du modèle source (obligatoire)")
	nom := fs.String("name", "", "nom de la copie")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *id == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --id est obligatoire")}
	}

	dossier, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}
	source, ok := trouverModeleGT(modeles, *id)
	if !ok {
		return &codeSortie{code: 2, err: fmt.Errorf("modèle inconnu %q", *id)}
	}
	clone := grouptemplates.DupliquerModele(modeles, source, *nom)
	modeles = append(modeles, clone)
	if _, err := grouptemplates.SauvegarderModeles(dossier, modeles); err != nil {
		return err
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"id": clone.ID, "name": clone.Nom})
	}
	fmt.Fprintf(stdout, "Copie créée : %s (%s)\n", clone.ID, clone.Nom)
	return nil
}

func cmdGroupTemplatesRemove(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates remove", flag.ContinueOnError)
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *id == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --id est obligatoire")}
	}

	dossier, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}
	restants := make([]grouptemplates.ModeleGroupe, 0, len(modeles))
	for _, modele := range modeles {
		if modele.ID != *id {
			restants = append(restants, modele)
		}
	}
	if len(restants) == len(modeles) {
		return &codeSortie{code: 2, err: fmt.Errorf("modèle inconnu %q", *id)}
	}
	if _, err := grouptemplates.SauvegarderModeles(dossier, restants); err != nil {
		return err
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"removed": *id, "remaining": len(restants)})
	}
	fmt.Fprintf(stdout, "Modèle supprimé : %s (%d restant(s))\n", *id, len(restants))
	return nil
}

func cmdGroupTemplatesExport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates export", flag.ContinueOnError)
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	fichier := fs.String("file", "", "fichier de destination (obligatoire)")
	format := fs.String("format", "json", "json ou xml")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *id == "" || *fichier == "" {
		return &codeSortie{code: 2, err: errors.New("les options --id et --file sont obligatoires")}
	}

	_, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}
	modele, ok := trouverModeleGT(modeles, *id)
	if !ok {
		return &codeSortie{code: 2, err: fmt.Errorf("modèle inconnu %q", *id)}
	}

	var chemin string
	switch *format {
	case "json":
		chemin, err = grouptemplates.ExporterModele(modele, *fichier)
	case "xml":
		chemin, err = grouptemplates.ExporterModeleXML(modele, *fichier)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("format inconnu %q (attendu : json | xml)", *format)}
	}
	if err != nil {
		return err
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"id": modele.ID, "file": chemin, "format": *format})
	}
	fmt.Fprintf(stdout, "Modèle exporté : %s (%s)\n", chemin, *format)
	return nil
}

func cmdGroupTemplatesImport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates import", flag.ContinueOnError)
	fichier := fs.String("file", "", "fichier source (obligatoire)")
	format := fs.String("format", "auto", "auto, json ou xml")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *fichier == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --file est obligatoire")}
	}

	charger := func() (grouptemplates.ModeleGroupe, error) {
		switch *format {
		case "json":
			return grouptemplates.ImporterModele(*fichier)
		case "xml":
			return grouptemplates.ImporterModeleXML(*fichier)
		default:
			modele, err := grouptemplates.ImporterModele(*fichier)
			if err == nil {
				return modele, nil
			}
			return grouptemplates.ImporterModeleXML(*fichier)
		}
	}
	modele, err := charger()
	if err != nil {
		return err
	}

	dossier, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}
	remplace := false
	remplaces := make([]grouptemplates.ModeleGroupe, 0, len(modeles)+1)
	for _, existant := range modeles {
		if existant.ID == modele.ID {
			remplace = true
			remplaces = append(remplaces, modele)
			continue
		}
		remplaces = append(remplaces, existant)
	}
	if !remplace {
		remplaces = append(remplaces, modele)
	}
	if _, err := grouptemplates.SauvegarderModeles(dossier, remplaces); err != nil {
		return err
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"id": modele.ID, "name": modele.Nom, "replaced": remplace,
		})
	}
	if remplace {
		fmt.Fprintf(stdout, "Modèle remplacé : %s (%s)\n", modele.ID, modele.Nom)
	} else {
		fmt.Fprintf(stdout, "Modèle importé : %s (%s)\n", modele.ID, modele.Nom)
	}
	return nil
}

func cmdGroupTemplatesBuiltins(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates builtins", flag.ContinueOnError)
	force := fs.Bool("force", false, "remplace le fichier par les modèles intégrés")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	modeles := grouptemplates.ModelesIntegres()
	var chemin string
	if *force {
		dossier, err := config.ConfigDir()
		if err != nil {
			return err
		}
		chemin, err = grouptemplates.SauvegarderModeles(dossier, modeles)
		if err != nil {
			return err
		}
	}

	if *jsonOut {
		ids := make([]string, 0, len(modeles))
		for _, modele := range modeles {
			ids = append(ids, modele.ID)
		}
		return ecrireJSON(stdout, map[string]any{"ids": ids, "written": chemin})
	}
	for _, modele := range modeles {
		fmt.Fprintf(stdout, "%s\t%s\n", modele.ID, modele.Nom)
	}
	if chemin != "" {
		fmt.Fprintf(stdout, "Modèles intégrés écrits : %s\n", chemin)
	}
	return nil
}

// annuairePlan simule un annuaire vide : rien n'existe et rien n'est écrit.
type annuairePlan struct {
	creationsOU     []string
	creationsGroupe []string
}

func (a *annuairePlan) ListerOUEnfants(context.Context, string) ([]string, error) {
	return nil, nil
}

func (a *annuairePlan) CreerOU(_ context.Context, parentDN, rdn string) error {
	a.creationsOU = append(a.creationsOU, "ou="+rdn+","+parentDN)
	return nil
}

func (a *annuairePlan) ListerGroupes(context.Context, string) ([]grouptemplates.GroupeExistant, error) {
	return nil, nil
}

func (a *annuairePlan) CreerGroupe(_ context.Context, dn, _, _ string) error {
	a.creationsGroupe = append(a.creationsGroupe, dn)
	return nil
}

func cmdGroupTemplatesPlan(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("group-templates plan", flag.ContinueOnError)
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	parent := fs.String("parent-dn", "", "OU parente (obligatoire)")
	artefacts := fs.String("artifacts", "", "répertoire de génération des artefacts")
	domaine := fs.String("domain", "", "domaine utilisé pour préfixer les ACE")
	scope := fs.Bool("scope", false, "enregistrer aussi les configs scopées profil/quota")
	homeGlobal := fs.Bool("home-global", false, "remplacer la config globale des dossiers personnels")
	var valeursFlag listeDrapeau
	fs.Var(&valeursFlag, "value", "valeur de motif champ=valeur (répétable)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *id == "" || *parent == "" {
		return &codeSortie{code: 2, err: errors.New("les options --id et --parent-dn sont obligatoires")}
	}

	valeurs, err := analyserValeurs(valeursFlag)
	if err != nil {
		return &codeSortie{code: 2, err: err}
	}
	_, modeles, err := chargerModelesGT()
	if err != nil {
		return err
	}
	modele, ok := trouverModeleGT(modeles, *id)
	if !ok {
		return &codeSortie{code: 2, err: fmt.Errorf("modèle inconnu %q", *id)}
	}

	opts := grouptemplates.OptionsInstantiation{
		OUParente:          parent,
		DossierArtefacts:   *artefacts,
		SansScope:          !*scope,
		DossierPersoGlobal: *homeGlobal,
		Domaine:            *domaine,
	}
	resultat, err := grouptemplates.Instancier(context.Background(), &annuairePlan{}, modele, valeurs, opts)
	if err != nil {
		return err
	}

	if *jsonOut {
		donnees := resultat.ToDict()
		donnees["resume"] = resultat.Resume()
		return ecrireJSON(stdout, donnees)
	}
	fmt.Fprintln(stdout, resultat.Resume())
	for _, artefact := range resultat.Artefacts {
		fmt.Fprintf(stdout, "Artefact : %s\n", artefact)
	}
	return nil
}
