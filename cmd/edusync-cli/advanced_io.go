package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdAdvancedIO gère l'import/export avancés (M22).
func cmdAdvancedIO(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « validate-filter », « source », « import », « export », « merge-html », « gep », « config » ou « run-tool »")}
	}
	switch args[0] {
	case "validate-filter":
		return cmdAdvIOValidateFilter(args[1:], stdout)
	case "source":
		return cmdAdvIOSource(args[1:], stdout)
	case "import":
		return cmdAdvIOImport(args[1:], stdout)
	case "export":
		return cmdAdvIOExport(args[1:], stdout)
	case "merge-html":
		return cmdAdvIOMergeHTML(args[1:], stdout)
	case "gep":
		return cmdAdvIOGEP(args[1:], stdout)
	case "config":
		return cmdAdvIOConfig(args[1:], stdout)
	case "run-tool":
		return cmdAdvIORunTool(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// enveloppeValiderFiltre porte la réponse JSON de validate-filter.
type enveloppeValiderFiltre struct {
	Filtre  string   `json:"filter"`
	Valide  bool     `json:"valid"`
	Erreurs []string `json:"errors"`
}

func cmdAdvIOValidateFilter(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advanced-io validate-filter", flag.ContinueOnError)
	filtre := fs.String("filter", "", "filtre LDAP à valider")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	erreurs := ad.ValiderFiltreLDAP(*filtre)
	if *jsonOut {
		if err := ecrireJSON(stdout, enveloppeValiderFiltre{Filtre: *filtre, Valide: len(erreurs) == 0, Erreurs: erreurs}); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	} else if len(erreurs) == 0 {
		fmt.Fprintln(stdout, "Filtre valide.")
	} else {
		for _, e := range erreurs {
			fmt.Fprintf(stdout, "- %s\n", e)
		}
	}
	if len(erreurs) > 0 {
		return &codeSortie{code: 1, err: errors.New("filtre invalide")}
	}
	return nil
}

// cmdAdvIOSource construit et valide une source LDAP.
func cmdAdvIOSource(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advanced-io source", flag.ContinueOnError)
	ou := fs.String("ou", "", "DN de l'OU (mode ou)")
	groupe := fs.String("group", "", "DN du groupe (mode groupe)")
	filtre := fs.String("filter", ad.FiltreUtilisateurs, "filtre LDAP (mode filtre)")
	base := fs.String("base", "", "base DN (modes groupe et filtre)")
	mode := fs.String("mode", ad.SourceLDAPOU, "ou | groupe | filtre")
	limite := fs.Int("limit", 5000, "limite d'entrées")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	source := ad.SourceLDAP{Mode: *mode, OUDN: *ou, GroupDN: *groupe, Filtre: *filtre, BaseDN: *base, Attributs: append([]string(nil), ad.AttributsExportDefaut...), Limite: *limite}
	erreurs := source.Validate()
	baseRequete, filtreRequete := source.RequeteLDAP()

	type reponse struct {
		Source  SourceLDAPJSON `json:"source"`
		Base    string         `json:"base_dn"`
		Filtre  string         `json:"ldap_filter"`
		Valide  bool           `json:"valid"`
		Erreurs []string       `json:"errors"`
	}
	rep := reponse{Source: versJSON(source), Base: baseRequete, Filtre: filtreRequete, Valide: len(erreurs) == 0, Erreurs: erreurs}
	if *jsonOut {
		if err := ecrireJSON(stdout, rep); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	} else {
		fmt.Fprintf(stdout, "Base   : %s\n", rep.Base)
		fmt.Fprintf(stdout, "Filtre : %s\n", rep.Filtre)
		if len(erreurs) == 0 {
			fmt.Fprintln(stdout, "Source valide.")
		} else {
			for _, e := range erreurs {
				fmt.Fprintf(stdout, "- %s\n", e)
			}
		}
	}
	if len(erreurs) > 0 {
		return &codeSortie{code: 1, err: errors.New("source LDAP invalide")}
	}
	return nil
}

// SourceLDAPJSON est la représentation JSON d'une source LDAP.
type SourceLDAPJSON struct {
	Mode      string   `json:"mode"`
	BaseDN    string   `json:"base_dn"`
	OUDN      string   `json:"ou_dn"`
	GroupDN   string   `json:"group_dn"`
	Filtre    string   `json:"ldap_filter"`
	Attributs []string `json:"attributes"`
	Limite    int      `json:"size_limit"`
}

// versJSON convertit une SourceLDAP pour la sortie JSON.
func versJSON(s ad.SourceLDAP) SourceLDAPJSON {
	return SourceLDAPJSON{Mode: s.Mode, BaseDN: s.BaseDN, OUDN: s.OUDN, GroupDN: s.GroupDN, Filtre: s.Filtre, Attributs: s.Attributs, Limite: s.Limite}
}

func cmdAdvIOImport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advanced-io import", flag.ContinueOnError)
	ou := fs.String("ou", "", "DN de l'OU (mode ou)")
	groupe := fs.String("group", "", "DN du groupe (mode groupe)")
	filtre := fs.String("filter", ad.FiltreUtilisateurs, "filtre LDAP (mode filtre)")
	base := fs.String("base", "", "base DN (modes groupe et filtre)")
	mode := fs.String("mode", ad.SourceLDAPOU, "ou | groupe | filtre")
	limite := fs.Int("limit", 5000, "limite d'entrées")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	source := ad.SourceLDAP{Mode: *mode, OUDN: *ou, GroupDN: *groupe, Filtre: *filtre, BaseDN: *base, Attributs: append([]string(nil), ad.AttributsExportDefaut...), Limite: *limite}
	type reponse struct {
		Source   SourceLDAPJSON `json:"source"`
		Base     string         `json:"base_dn"`
		Filtre   string         `json:"ldap_filter"`
		Statut   string         `json:"statut"`
		Detaille string         `json:"detail"`
	}
	baseRequete, filtreRequete := source.RequeteLDAP()
	rep := reponse{Source: versJSON(source), Base: baseRequete, Filtre: filtreRequete, Statut: "attente_ad", Detaille: "connexion AD requise pour importer"}
	if *jsonOut {
		return ecrireJSON(stdout, rep)
	}
	fmt.Fprintf(stdout, "Statut : %s\n", rep.Statut)
	fmt.Fprintf(stdout, "Détail : %s\n", rep.Detaille)
	return nil
}

func cmdAdvIOExport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advanced-io export", flag.ContinueOnError)
	src := fs.String("src", "", "fichier JSON des entrées")
	dest := fs.String("dest", "", "fichier de sortie")
	format := fs.String("format", "csv", "csv | ldif | vcard")
	changetype := fs.String("changetype", "", "changetype LDIF (ex. add)")
	photo := fs.Bool("photo", false, "inclure les photos (vCard)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *src == "" || *dest == "" {
		return &codeSortie{code: 2, err: errors.New("les options --src et --dest sont obligatoires")}
	}

	entrees, err := lireEntreesJSON(*src)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	var produit string
	switch *format {
	case "csv":
		produit, err = ad.ExportEntreesCSV(entrees, *dest)
	case "ldif":
		produit, err = ad.ExportLDIF(entrees, *dest, nil, *changetype)
	case "vcard":
		produit, err = ad.ExportVCards(entrees, *dest, *photo)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("format inconnu %q", *format)}
	}
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"format": *format, "dest": produit, "entrees": len(entrees)})
	}
	fmt.Fprintf(stdout, "%d entrée(s) exportée(s) vers %s\n", len(entrees), produit)
	return nil
}

func cmdAdvIOMergeHTML(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advanced-io merge-html", flag.ContinueOnError)
	template := fs.String("template", "", "fichier gabarit HTML (défaut : gabarit fourni)")
	src := fs.String("src", "", "fichier JSON des entrées")
	dest := fs.String("dest", "", "dossier de sortie")
	pattern := fs.String("pattern", "{sAMAccountName}_document.html", "motif de nom de fichier")
	noIndex := fs.Bool("no-index", false, "ne pas générer index.html")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *src == "" || *dest == "" {
		return &codeSortie{code: 2, err: errors.New("les options --src et --dest sont obligatoires")}
	}

	entrees, err := lireEntreesJSON(*src)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	gabarit := ad.GabaritHTMLDefaut
	if *template != "" {
		donnees, err := os.ReadFile(*template)
		if err != nil {
			return &codeSortie{code: 1, err: fmt.Errorf("lecture du gabarit: %w", err)}
		}
		gabarit = string(donnees)
	}

	chemins, err := ad.MergeHTML(gabarit, entrees, *dest, *pattern, !*noIndex)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"fichiers": chemins, "dossier": *dest})
	}
	for _, chemin := range chemins {
		fmt.Fprintln(stdout, chemin)
	}
	return nil
}

func cmdAdvIOGEP(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande GEP manquante : « mapping », « parse » ou « convert »")}
	}
	switch args[0] {
	case "mapping":
		fs := flag.NewFlagSet("advanced-io gep mapping", flag.ContinueOnError)
		headers := fs.String("headers", "", "en-têtes séparés par des virgules")
		jsonOut := fs.Bool("json", false, "sortie JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return &codeSortie{code: 2, err: err}
		}
		var liste []string
		for _, h := range strings.Split(*headers, ",") {
			if h = strings.TrimSpace(h); h != "" {
				liste = append(liste, h)
			}
		}
		mapping := ad.SuggererMappingGEP(liste)
		if *jsonOut {
			return ecrireJSON(stdout, mapping)
		}
		for _, col := range ad.ColonnesImportEduSync {
			fmt.Fprintf(stdout, "%s = %s\n", col, mapping[col])
		}
		return nil
	case "parse":
		fs := flag.NewFlagSet("advanced-io gep parse", flag.ContinueOnError)
		src := fs.String("src", "", "fichier CSV GEP")
		dest := fs.String("dest", "", "fichier CSV EduSync (optionnel)")
		jsonOut := fs.Bool("json", false, "sortie JSON")
		if err := fs.Parse(args[1:]); err != nil {
			return &codeSortie{code: 2, err: err}
		}
		if *src == "" {
			return &codeSortie{code: 2, err: errors.New("l'option --src est obligatoire")}
		}
		donnees, err := os.ReadFile(*src)
		if err != nil {
			return &codeSortie{code: 1, err: fmt.Errorf("lecture CSV GEP: %w", err)}
		}
		resultat := ad.ParseGEPCSV(string(donnees), nil)
		var fichier string
		if *dest != "" {
			if fichier, err = ad.GEPToEduSyncCSV(resultat.Lignes, *dest); err != nil {
				return &codeSortie{code: 1, err: err}
			}
		}
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{
				"mapping":             resultat.Mapping,
				"delimiter":           resultat.Separateur,
				"rows":                len(resultat.Lignes),
				"skipped_row_numbers": resultat.LignesIgnorees,
				"edusync_csv":         fichier,
			})
		}
		fmt.Fprintf(stdout, "Séparateur : %s\n", resultat.Separateur)
		fmt.Fprintf(stdout, "Lignes     : %d (ignorées : %d)\n", len(resultat.Lignes), len(resultat.LignesIgnorees))
		if fichier != "" {
			fmt.Fprintf(stdout, "Export     : %s\n", fichier)
		}
		return nil
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande GEP inconnue %q", args[0])}
	}
}

func cmdAdvIOConfig(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advanced-io config", flag.ContinueOnError)
	tool := fs.String("tool", "", "chemin de l'outil tiers GEP (écrit la configuration si fourni)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	dir, err := ad.DossierConfigAdvIO()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *tool != "" {
		if err := ad.EnregistrerConfigAdvIO(dir, ad.ConfigAdvIO{CheminOutilGEP: *tool}); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	cfg := ad.ChargerConfigAdvIO(dir)
	if *jsonOut {
		return ecrireJSON(stdout, cfg)
	}
	fmt.Fprintf(stdout, "Outil GEP : %s\n", cfg.CheminOutilGEP)
	return nil
}

func cmdAdvIORunTool(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("advanced-io run-tool", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	dir, err := ad.DossierConfigAdvIO()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	cfg := ad.ChargerConfigAdvIO(dir)
	err = ad.LancerOutilExterne(cfg)
	if *jsonOut {
		statut := "lance"
		detail := ""
		if err != nil {
			statut = "erreur"
			detail = err.Error()
		}
		return ecrireJSON(stdout, map[string]string{"statut": statut, "detail": detail, "outil": cfg.CheminOutilGEP})
	}
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	fmt.Fprintln(stdout, "Outil lancé.")
	return nil
}

// lireEntreesJSON lit un tableau JSON d'objets et le normalise en EntreeLDAP.
func lireEntreesJSON(chemin string) ([]ad.EntreeLDAP, error) {
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil, fmt.Errorf("lecture JSON %s: %w", chemin, err)
	}
	var brutes []map[string]any
	if err := json.Unmarshal(donnees, &brutes); err != nil {
		return nil, fmt.Errorf("décodage JSON %s: %w", chemin, err)
	}
	entrees := make([]ad.EntreeLDAP, 0, len(brutes))
	for _, brute := range brutes {
		entree := ad.EntreeLDAP{}
		for k, v := range brute {
			entree[k] = normaliserValeur(v)
		}
		entrees = append(entrees, entree)
	}
	return entrees, nil
}

// normaliserValeur convertit une valeur JSON en type exploitable (string,
// []string) : les nombres deviennent des chaînes, les listes de chaînes.
func normaliserValeur(v any) any {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		if t == math.Trunc(t) {
			return strconv.FormatInt(int64(t), 10)
		}
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []any:
		liste := make([]string, 0, len(t))
		for _, item := range t {
			liste = append(liste, valeurChaine(normaliserValeur(item)))
		}
		return liste
	default:
		return fmt.Sprint(v)
	}
}

// valeurChaine rend une chaîne d'une valeur normalisée.
func valeurChaine(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case []string:
		return strings.Join(t, ", ")
	default:
		return fmt.Sprint(v)
	}
}
