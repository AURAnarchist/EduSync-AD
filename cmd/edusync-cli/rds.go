package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/rds"
)

// cmdRDS gère les commandes RDS / Bureau à distance (M21).
func cmdRDS(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « config », « collection-script », « remoteapp-script », « fslogix-script » ou « plans »")}
	}
	switch args[0] {
	case "config":
		return cmdRDSConfig(args[1:], stdout)
	case "collection-script":
		return cmdRDSCollectionScript(args[1:], stdout)
	case "remoteapp-script":
		return cmdRDSRemoteAppScript(args[1:], stdout)
	case "fslogix-script":
		return cmdRDSFSLogixScript(args[1:], stdout)
	case "plans":
		return cmdRDSPlans(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// optionsRDS regroupe les options de configuration communes à toutes les
// sous-commandes RDS.
type optionsRDS struct {
	broker     *string
	domain     *string
	profil     *string
	updPath    *string
	updTaille  *int
	updInclure *string
	updExclure *string
	fsLieux    *string
	fsTaille   *int
	fsFlipFlop *bool
}

// enregistrerOptionsRDS déclare les options de configuration sur l'ensemble de
// drapeaux.
func enregistrerOptionsRDS(fs *flag.FlagSet) *optionsRDS {
	return &optionsRDS{
		broker:     fs.String("broker", "", "Connection Broker (FQDN)"),
		domain:     fs.String("domain", "", "domaine (préfixe des groupes)"),
		profil:     fs.String("profile-mode", "", "none | upd | fslogix"),
		updPath:    fs.String("upd-path", "", "partage UNC du disque de profil"),
		updTaille:  fs.Int("upd-size-gb", 0, "taille UPD en Go"),
		updInclure: fs.String("upd-include", "", "dossiers inclus UPD (séparés par , ou ;)"),
		updExclure: fs.String("upd-exclude", "", "dossiers exclus UPD (séparés par , ou ;)"),
		fsLieux:    fs.String("fslogix-locations", "", "partages UNC FSLogix (séparés par , ou ;)"),
		fsTaille:   fs.Int("fslogix-size-mb", 0, "taille de profil FSLogix en Mo"),
		fsFlipFlop: fs.Bool("fslogix-flipflop", true, "dossier de profil = nom utilisateur FSLogix"),
	}
}

// appliquer fusionne les options fournies avec la configuration chargée.
func (o *optionsRDS) appliquer(cfg rds.Config, visites map[string]bool) rds.Config {
	if visites["broker"] {
		cfg.Broker = *o.broker
	}
	if visites["domain"] {
		cfg.Domain = *o.domain
	}
	if visites["profile-mode"] {
		cfg.ProfileMode = *o.profil
	}
	if visites["upd-path"] {
		cfg.UPDPath = *o.updPath
	}
	if visites["upd-size-gb"] {
		cfg.UPDSizeGB = *o.updTaille
	}
	if visites["upd-include"] {
		cfg.UPDIncludePaths = rds.SplitMulti(*o.updInclure)
	}
	if visites["upd-exclude"] {
		cfg.UPDExcludePaths = rds.SplitMulti(*o.updExclure)
	}
	if visites["fslogix-locations"] {
		cfg.FSLogixLocations = rds.SplitMulti(*o.fsLieux)
	}
	if visites["fslogix-size-mb"] {
		cfg.FSLogixSizeMB = *o.fsTaille
	}
	if visites["fslogix-flipflop"] {
		cfg.FSLogixFlipFlop = *o.fsFlipFlop
	}
	return cfg
}

// visitesOptions retourne l'ensemble des drapeaux réellement passés.
func visitesOptions(fs *flag.FlagSet) map[string]bool {
	visites := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visites[f.Name] = true })
	return visites
}

// chargerConfigRDS charge la configuration partagée puis applique les options.
func chargerConfigRDS(fs *flag.FlagSet, opts *optionsRDS) rds.Config {
	return opts.appliquer(rds.ChargerConfig(""), visitesOptions(fs))
}

// dirPath renvoie le chemin d'un fichier de configuration dans un dossier.
func dirPath(dir string) string { return dir + "/" + rds.FichierConfig }

// ecrireScriptRDS écrit le script sur disque (avec BOM) ou sur la sortie.
func ecrireScriptRDS(dest, script string, stdout io.Writer) error {
	if dest == "" {
		fmt.Fprint(stdout, script)
		return nil
	}
	if err := rds.SauvegarderScript(dest, script); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	fmt.Fprintf(stdout, "Script écrit : %s\n", dest)
	return nil
}

func cmdRDSConfig(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rds config", flag.ContinueOnError)
	opts := enregistrerOptionsRDS(fs)
	save := fs.Bool("save", false, "enregistrer la configuration")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	dir, err := rds.DossierConfig()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	cfg := opts.appliquer(rds.ChargerConfig(dirPath(dir)), visitesOptions(fs))
	if *save {
		if err := rds.SauvegarderConfig(dirPath(dir), cfg); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	if *jsonOut {
		return ecrireJSON(stdout, cfg)
	}
	fmt.Fprintf(stdout, "Broker        : %s\n", cfg.Broker)
	fmt.Fprintf(stdout, "Domaine       : %s\n", cfg.Domain)
	fmt.Fprintf(stdout, "Mode de profil: %s\n", cfg.ProfileMode)
	fmt.Fprintf(stdout, "Erreurs       : %s\n", strings.Join(cfg.Validate(), " ; "))
	return nil
}

func cmdRDSCollectionScript(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rds collection-script", flag.ContinueOnError)
	opts := enregistrerOptionsRDS(fs)
	name := fs.String("name", "", "nom de la collection (obligatoire)")
	description := fs.String("description", "", "description de la collection")
	hosts := fs.String("hosts", "", "hébergeurs (séparés par , ou ;)")
	groups := fs.String("groups", "", "groupes autorisés (séparés par , ou ;)")
	dest := fs.String("dest", "", "écrire le script dans ce fichier (défaut : stdout)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *name == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --name est obligatoire")}
	}

	cfg := chargerConfigRDS(fs, opts)
	plan := rds.ConstruirePlanCollection(*name, *description, rds.SplitMulti(*hosts), rds.SplitMulti(*groups), cfg)
	script, err := rds.ScriptCollections([]rds.PlanCollection{plan}, cfg)
	if err != nil {
		return &codeSortie{code: 2, err: err}
	}
	return ecrireScriptRDS(*dest, script, stdout)
}

func cmdRDSRemoteAppScript(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rds remoteapp-script", flag.ContinueOnError)
	opts := enregistrerOptionsRDS(fs)
	alias := fs.String("alias", "", "alias RemoteApp (déduit du nom si absent)")
	displayName := fs.String("display-name", "", "nom affiché (obligatoire)")
	filePath := fs.String("file-path", "", "chemin de l'exécutable (obligatoire)")
	collection := fs.String("collection", "", "collection cible (obligatoire)")
	groups := fs.String("groups", "", "groupes autorisés (séparés par , ou ;)")
	folder := fs.String("folder", "", "dossier de classement dans RD Web Access")
	icone := fs.Int("icon", -1, "index d'icône")
	required := fs.String("required-command", "", "ligne de commande imposée")
	noWeb := fs.Bool("no-show-in-web", false, "masquer dans RD Web Access")
	dest := fs.String("dest", "", "écrire le script dans ce fichier (défaut : stdout)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *displayName == "" || *filePath == "" || *collection == "" {
		return &codeSortie{code: 2, err: errors.New("les options --display-name, --file-path et --collection sont obligatoires")}
	}

	cfg := chargerConfigRDS(fs, opts)
	plan := rds.PlanRemoteApp{
		Alias:           aliasOu(*alias, *displayName),
		DisplayName:     *displayName,
		FilePath:        *filePath,
		Collection:      *collection,
		UserGroups:      normaliserGroupes(rds.SplitMulti(*groups), cfg.Domain),
		ShowInWeb:       !*noWeb,
		FolderName:      *folder,
		RequiredCommand: *required,
	}
	if *icone >= 0 {
		plan.IconIndex = icone
	}
	script, err := rds.ScriptRemoteApp([]rds.PlanRemoteApp{plan}, cfg)
	if err != nil {
		return &codeSortie{code: 2, err: err}
	}
	return ecrireScriptRDS(*dest, script, stdout)
}

func cmdRDSFSLogixScript(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rds fslogix-script", flag.ContinueOnError)
	opts := enregistrerOptionsRDS(fs)
	dest := fs.String("dest", "", "écrire le script dans ce fichier (défaut : stdout)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := chargerConfigRDS(fs, opts)
	script, err := rds.ScriptFSLogix(cfg)
	if err != nil {
		return &codeSortie{code: 2, err: err}
	}
	return ecrireScriptRDS(*dest, script, stdout)
}

func cmdRDSPlans(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("rds plans", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	dir, err := rds.DossierConfig()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	chemin := dir + "/" + rds.FichierPlans
	collections, remoteapps := rds.ChargerPlans(chemin)

	type resultat struct {
		Collections []rds.PlanCollection `json:"collections"`
		RemoteApps  []rds.PlanRemoteApp  `json:"remoteapps"`
	}
	if *jsonOut {
		return ecrireJSON(stdout, resultat{Collections: collections, RemoteApps: remoteapps})
	}
	fmt.Fprintf(stdout, "Collections : %d\n", len(collections))
	for _, c := range collections {
		fmt.Fprintf(stdout, "  - %s\n", c.Label())
	}
	fmt.Fprintf(stdout, "RemoteApp   : %d\n", len(remoteapps))
	for _, a := range remoteapps {
		fmt.Fprintf(stdout, "  - %s\n", a.Label())
	}
	return nil
}

// aliasOu retourne l'alias fourni, sinon un alias déduit du nom affiché.
func aliasOu(alias, displayName string) string {
	if strings.TrimSpace(alias) != "" {
		return strings.TrimSpace(alias)
	}
	return rds.CreerAlias(displayName)
}

// normaliserGroupes applique le préfixe DOMAINE aux groupes sans domaine.
func normaliserGroupes(groupes []string, domaine string) []string {
	var resultat []string
	for _, groupe := range groupes {
		normalise := rds.NormaliserRefGroupe(groupe, domaine)
		if normalise != "" && !contientChaine(resultat, normalise) {
			resultat = append(resultat, normalise)
		}
	}
	if resultat == nil {
		resultat = []string{}
	}
	return resultat
}

// contientChaine indique si un slice de chaînes contient une valeur.
func contientChaine(valeurs []string, cible string) bool {
	for _, v := range valeurs {
		if v == cible {
			return true
		}
	}
	return false
}
