package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/graph"
	"github.com/estemobs/EduSync-AD/internal/m365"
)

// cmdM365 gère les commandes Microsoft 365 / Entra ID (M19).
func cmdM365(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « config », « user-plans », « group-plans », « photo-mapping » ou « hybrid »")}
	}
	switch args[0] {
	case "config":
		return cmdM365Config(args[1:], stdout)
	case "user-plans":
		return cmdM365UserPlans(args[1:], stdout)
	case "group-plans":
		return cmdM365GroupPlans(args[1:], stdout)
	case "photo-mapping":
		return cmdM365PhotoMapping(args[1:], stdout)
	case "hybrid":
		return cmdM365Hybrid(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// optionsGraph déclare les options de connexion Microsoft 365.
func optionsGraph(fs *flag.FlagSet) func(graph.Config) graph.Config {
	tenant := fs.String("tenant-id", "", "tenant ID (GUID)")
	client := fs.String("client-id", "", "client ID de l'app registration")
	secret := fs.String("client-secret", "", "secret client (mode « secret »)")
	mode := fs.String("auth-mode", "", "secret | certificate")
	cert := fs.String("certificate-path", "", "fichier PEM (mode « certificate »)")
	usage := fs.String("usage-location", "", "localisation d'usage (ex : FR)")

	return func(cfg graph.Config) graph.Config {
		visites := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visites[f.Name] = true })
		if visites["tenant-id"] {
			cfg.TenantID = *tenant
		}
		if visites["client-id"] {
			cfg.ClientID = *client
		}
		if visites["client-secret"] {
			cfg.ClientSecret = *secret
		}
		if visites["auth-mode"] {
			cfg.AuthMode = *mode
		}
		if visites["certificate-path"] {
			cfg.CertificatePath = *cert
		}
		if visites["usage-location"] {
			cfg.UsageLocation = *usage
		}
		return cfg
	}
}

func cmdM365Config(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("m365 config", flag.ContinueOnError)
	appliquer := optionsGraph(fs)
	save := fs.Bool("save", false, "enregistrer la configuration (secret chiffré)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	dir, err := graph.DossierConfig()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	chemin := dir + "/" + graph.FichierConfig
	cfg := appliquer(graph.ChargerConfig(chemin))
	if *save {
		if err := graph.SauvegarderConfig(chemin, cfg); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	if *jsonOut {
		// Le secret n'est jamais recopié en sortie.
		return ecrireJSON(stdout, map[string]any{
			"tenant_id":        cfg.TenantID,
			"client_id":        cfg.ClientID,
			"auth_mode":        cfg.AuthMode,
			"certificate_path": cfg.CertificatePath,
			"usage_location":   cfg.UsageLocation,
			"secret_defini":    cfg.ClientSecret != "",
			"erreurs":          cfg.Validate(),
		})
	}
	fmt.Fprintf(stdout, "Tenant ID       : %s\n", cfg.TenantID)
	fmt.Fprintf(stdout, "Client ID       : %s\n", cfg.ClientID)
	fmt.Fprintf(stdout, "Mode d'auth     : %s\n", cfg.AuthMode)
	fmt.Fprintf(stdout, "Certificat      : %s\n", cfg.CertificatePath)
	fmt.Fprintf(stdout, "Localisation    : %s\n", cfg.UsageLocation)
	fmt.Fprintf(stdout, "Secret défini   : %v\n", cfg.ClientSecret != "")
	fmt.Fprintf(stdout, "Erreurs         : %s\n", strings.Join(cfg.Validate(), " ; "))
	return nil
}

// lireComptesM365 décode une liste de comptes AD depuis un fichier JSON.
func lireComptesM365(src string) ([]m365.CompteAD, error) {
	if src == "" {
		return nil, &codeSortie{code: 2, err: errors.New("l'option --src est obligatoire")}
	}
	donnees, err := os.ReadFile(src)
	if err != nil {
		return nil, &codeSortie{code: 1, err: fmt.Errorf("lecture des comptes %s: %w", src, err)}
	}
	var comptes []m365.CompteAD
	if err := json.Unmarshal(donnees, &comptes); err != nil {
		return nil, &codeSortie{code: 1, err: fmt.Errorf("décodage des comptes %s: %w", src, err)}
	}
	return comptes, nil
}

func cmdM365UserPlans(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("m365 user-plans", flag.ContinueOnError)
	src := fs.String("src", "", "fichier JSON des comptes AD (obligatoire)")
	domain := fs.String("domain", "", "domaine cloud (ex : lycee.onmicrosoft.com)")
	existing := fs.String("existing-upns", "", "UPN déjà présents dans Entra ID (séparés par des virgules)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	comptes, err := lireComptesM365(*src)
	if err != nil {
		return err
	}
	var existants []string
	if strings.TrimSpace(*existing) != "" {
		existants = strings.Split(*existing, ",")
	}
	plans := m365.ConstruirePlansUtilisateurs(comptes, *domain, existants)
	if *jsonOut {
		return ecrireJSON(stdout, plans)
	}
	for _, plan := range plans {
		fmt.Fprintln(stdout, plan.Label())
	}
	return nil
}

func cmdM365GroupPlans(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("m365 group-plans", flag.ContinueOnError)
	src := fs.String("src", "", "fichier JSON des groupes (name, members) (obligatoire)")
	kind := fs.String("kind", "m365", "m365 | distribution | security")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *src == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --src est obligatoire")}
	}
	donnees, err := os.ReadFile(*src)
	if err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("lecture des groupes %s: %w", *src, err)}
	}
	var specs []m365.SpecGroupe
	if err := json.Unmarshal(donnees, &specs); err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("décodage des groupes %s: %w", *src, err)}
	}
	if _, ok := m365.TypesGroupe[*kind]; !ok {
		return &codeSortie{code: 2, err: fmt.Errorf("type de groupe inconnu %q", *kind)}
	}
	plans := m365.ConstruirePlansGroupes(specs, *kind, nil)
	if *jsonOut {
		return ecrireJSON(stdout, plans)
	}
	for _, plan := range plans {
		fmt.Fprintln(stdout, plan.Label())
	}
	return nil
}

func cmdM365PhotoMapping(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("m365 photo-mapping", flag.ContinueOnError)
	dir := fs.String("dir", "", "dossier des photos (obligatoire)")
	src := fs.String("src", "", "fichier JSON des comptes AD (obligatoire)")
	domain := fs.String("domain", "", "domaine cloud")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *dir == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --dir est obligatoire")}
	}
	comptes, err := lireComptesM365(*src)
	if err != nil {
		return err
	}
	mapping, err := m365.ConstruireMappingPhotos(*dir, comptes, *domain)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, mapping)
	}
	for _, upn := range trierCles(mapping) {
		fmt.Fprintf(stdout, "%s → %s\n", upn, mapping[upn])
	}
	return nil
}

// trierCles retourne les clés d'une map triées (sortie déterministe).
func trierCles(m map[string]string) []string {
	cles := make([]string, 0, len(m))
	for k := range m {
		cles = append(cles, k)
	}
	for i := 1; i < len(cles); i++ {
		for j := i; j > 0 && cles[j] < cles[j-1]; j-- {
			cles[j], cles[j-1] = cles[j-1], cles[j]
		}
	}
	return cles
}

func cmdM365Hybrid(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("m365 hybrid", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	markdown := fs.Bool("markdown", false, "rapport Markdown")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	switch {
	case *jsonOut:
		type etape struct {
			Titre  string `json:"titre"`
			Detail string `json:"detail"`
		}
		etapes := make([]etape, 0)
		for _, e := range m365.HybridChecklist() {
			etapes = append(etapes, etape{Titre: e[0], Detail: e[1]})
		}
		return ecrireJSON(stdout, etapes)
	case *markdown:
		fmt.Fprint(stdout, m365.HybridChecklistMarkdown())
	default:
		for i, e := range m365.HybridChecklist() {
			fmt.Fprintf(stdout, "%d. %s\n   %s\n", i+1, e[0], e[1])
		}
	}
	return nil
}
