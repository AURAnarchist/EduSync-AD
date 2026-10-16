package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// resultatUsersImport est le résultat d'une simulation d'import d'utilisateurs.
type resultatUsersImport struct {
	Lignes  []ligneImport `json:"lignes"`
	Valides int           `json:"valides"`
	Erreurs int           `json:"erreurs"`
	Total   int           `json:"total"`
}

type ligneImport struct {
	Prenom      string `json:"prenom"`
	Nom         string `json:"nom"`
	Identifiant string `json:"identifiant"`
	Mail        string `json:"mail"`
	OUCible     string `json:"ou_cible"`
	Groupe      string `json:"groupe,omitempty"`
	Erreur      string `json:"erreur,omitempty"`
}

// NomComplet retourne "prénom nom".
func (l ligneImport) NomComplet() string {
	return l.Prenom + " " + l.Nom
}

// resultatPasswordSim est le résultat d'une simulation de génération MDP.
type resultatPasswordSim struct {
	Lignes    []lignePassword `json:"lignes"`
	Valides   int             `json:"valides"`
	Erreurs   int             `json:"erreurs"`
	Total     int             `json:"total"`
	Politique string          `json:"politique"`
	Longueur  int             `json:"longueur"`
}

type lignePassword struct {
	Identifiant string `json:"identifiant"`
	MDP         string `json:"mdp"`
	ForceChange bool   `json:"force_change"`
	Erreur      string `json:"erreur,omitempty"`
}

// resultatExportFields liste les champs d'export disponibles.
type resultatExportFields struct {
	Champs    []champExport `json:"champs"`
	Templates []string      `json:"templates"`
}

type champExport struct {
	Nom         string `json:"nom"`
	Description string `json:"description"`
}

// containsStr indique si une chaîne est présente dans un slice.
func containsStr(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

// cmdUsers gère les commandes liées aux utilisateurs (M1).
func cmdUsers(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « import-simulate »")}
	}
	switch args[0] {
	case "import-simulate":
		return cmdUsersImportSimulate(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : import-simulate)", args[0])}
	}
}

// cmdUsersImportSimulate lit un CSV et génère la simulation d'import (sans écriture AD).
func cmdUsersImportSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("users import-simulate", flag.ContinueOnError)
	fichier := fs.String("f", "", "chemin du fichier CSV d'import (obligatoire)")
	typeCompte := fs.String("type", "eleve", "type de compte : eleve | personnel")
	ouParente := fs.String("ou", "", "OU parente cible (vide = résolue depuis la classe)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *fichier == "" {
		return &codeSortie{code: 2, err: errors.New("l'option -f est obligatoire")}
	}

	cfg := config.DefaultAppConfig()
	if c, err := config.Load(""); err == nil {
		cfg = c
	}

	f, err := os.Open(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("ouverture du CSV: %w", err)}
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("lecture du CSV: %w", err)}
	}
	if len(records) < 2 {
		return &codeSortie{code: 1, err: errors.New("le CSV ne contient aucune donnée")}
	}

	header := records[0]
	indices := make(map[string]int)
	for i, h := range header {
		indices[strings.TrimSpace(strings.ToLower(h))] = i
	}
	get := func(row []string, key string) string {
		if idx, ok := indices[key]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	at := models.AccountEleve
	if *typeCompte == "personnel" {
		at = models.AccountPersonnel
	}

	var lignes []ligneImport
	var usedIdents, usedMails []string
	valides, erreurs := 0, 0

	for _, rec := range records[1:] {
		prenom := get(rec, "prenom")
		nom := get(rec, "nom")
		classe := get(rec, "classe")
		ou := get(rec, "ou")

		l := ligneImport{Prenom: prenom, Nom: nom}

		if prenom == "" || nom == "" {
			l.Erreur = "prénom/nom manquant"
			lignes = append(lignes, l)
			erreurs++
			continue
		}

		// Génération identifiant
		ident := cfg.GenerateIdentifiant(prenom, nom, at)
		if containsStr(usedIdents, ident) {
			res, _ := cfg.ResolveDuplicate(ident, usedIdents, cfg.RegleDoublons)
			ident = res
		}
		usedIdents = append(usedIdents, ident)
		l.Identifiant = ident

		// Génération mail
		domaineMail := cfg.DomaineMail
		if domaineMail == "" {
			domaineMail = "edusync.local"
		}
		adrMail := cfg.GenerateEmail(prenom, nom, domaineMail)
		if containsStr(usedMails, adrMail) {
			base := strings.Split(adrMail, "@")[0]
			domain := strings.Split(adrMail, "@")[1]
			res, _ := cfg.ResolveDuplicate(base, usedMails, cfg.RegleDoublons)
			adrMail = res + "@" + domain
		}
		usedMails = append(usedMails, adrMail)
		l.Mail = adrMail

		// OU cible
		if ou == "" && classe != "" {
			l.OUCible = config.BuildUserDN(config.SanitizeDNPart(classe), "OU=Eleves,"+config.DomaineToBaseDN("EDUSYNC.LOCAL"))
		} else if ou == "" {
			l.OUCible = "OU=Eleves," + config.DomaineToBaseDN("EDUSYNC.LOCAL")
		} else {
			l.OUCible = ou
		}
		if *ouParente != "" {
			l.OUCible = *ouParente
		}

		// Groupe classe auto
		if cfg.GroupesClasseAuto && classe != "" {
			l.Groupe = "CN=" + config.SanitizeDNPart(classe) + ",OU=Groupes," + config.DomaineToBaseDN("EDUSYNC.LOCAL")
		}

		// Vérifier que le MDP peut être généré (pas stocké en clair en CLI)
		if _, err := cfg.GeneratePassword(cfg.PolitiqueMDPEleve); err != nil {
			l.Erreur = fmt.Sprintf("génération MDP: %v", err)
			erreurs++
		} else {
			valides++
		}
		lignes = append(lignes, l)
	}

	r := resultatUsersImport{
		Lignes:  lignes,
		Valides: valides,
		Erreurs: erreurs,
		Total:   len(lignes),
	}

	if *jsonOut {
		return ecrireJSON(stdout, r)
	}
	fmt.Fprintf(stdout, "Import simulé : %d valides · %d erreurs · %d total\n", valides, erreurs, len(lignes))
	for _, l := range lignes {
		if l.Erreur != "" {
			fmt.Fprintf(stdout, "  ERREUR %s %s : %s\n", l.Identifiant, l.NomComplet(), l.Erreur)
		} else {
			fmt.Fprintf(stdout, "  OK %s %s → %s (%s)\n", l.Identifiant, l.NomComplet(), l.Mail, l.OUCible)
		}
	}
	return nil
}

// cmdPassword gère les commandes liées aux mots de passe (M4).
func cmdPassword(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate »")}
	}
	switch args[0] {
	case "simulate":
		return cmdPasswordSimulate(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate)", args[0])}
	}
}

// cmdPasswordSimulate génère des mots de passe selon une politique (sans écriture AD).
func cmdPasswordSimulate(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("password simulate", flag.ContinueOnError)
	nb := fs.Int("n", 1, "nombre de mots de passe à générer")
	typeCompte := fs.String("type", "eleve", "type de compte : eleve | personnel")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := config.DefaultAppConfig()
	if c, err := config.Load(""); err == nil {
		cfg = c
	}

	policy := cfg.PolitiqueMDPEleve
	if *typeCompte == "personnel" {
		policy = cfg.PolitiqueMDPPersonnel
	}

	var lignes []lignePassword
	valides, erreurs := 0, 0
	for i := 0; i < *nb; i++ {
		mdp, err := cfg.GeneratePassword(policy)
		l := lignePassword{Identifiant: fmt.Sprintf("user%d", i+1)}
		if err != nil {
			l.Erreur = err.Error()
			erreurs++
		} else {
			l.MDP = mdp
			valides++
		}
		lignes = append(lignes, l)
	}

	r := resultatPasswordSim{
		Lignes:    lignes,
		Valides:   valides,
		Erreurs:   erreurs,
		Total:     *nb,
		Politique: *typeCompte,
		Longueur:  policy.Longueur,
	}

	if *jsonOut {
		return ecrireJSON(stdout, r)
	}
	fmt.Fprintf(stdout, "Mot(s) de passe généré(s) : %d valides · %d erreurs\n", valides, erreurs)
	for _, l := range lignes {
		fmt.Fprintf(stdout, "  %s : %s\n", l.Identifiant, l.MDP)
	}
	return nil
}

// cmdMigration gère les commandes liées à la migration (M2).
func cmdMigration(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « plan »")}
	}
	switch args[0] {
	case "plan":
		return cmdMigrationPlan(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : plan)", args[0])}
	}
}

// cmdMigrationPlan lit un CSV de migration et retourne le plan (sans écriture AD).
func cmdMigrationPlan(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("migration plan", flag.ContinueOnError)
	fichier := fs.String("f", "", "chemin du fichier CSV de migration (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *fichier == "" {
		return &codeSortie{code: 2, err: errors.New("l'option -f est obligatoire")}
	}

	f, err := os.Open(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("ouverture du CSV: %w", err)}
	}
	defer f.Close()

	reader := csv.NewReader(f)
	reader.Comma = ';'
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true

	records, err := reader.ReadAll()
	if err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("lecture du CSV: %w", err)}
	}
	if len(records) < 2 {
		return &codeSortie{code: 1, err: errors.New("le CSV ne contient aucune donnée")}
	}

	header := records[0]
	indices := make(map[string]int)
	for i, h := range header {
		indices[strings.TrimSpace(strings.ToLower(h))] = i
	}
	get := func(row []string, key string) string {
		if idx, ok := indices[key]; ok && idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}

	var rows []ad.MigrationRow
	for _, rec := range records[1:] {
		rows = append(rows, ad.MigrationRow{
			SAMAccountName: get(rec, "identifiant"),
			SourceOU:       get(rec, "ou_source"),
			TargetOU:       get(rec, "ou_cible"),
		})
	}

	willMove := 0
	willSkip := 0
	for i := range rows {
		if rows[i].SourceOU == "" || rows[i].TargetOU == "" {
			rows[i].Error = "OU source ou cible manquante"
			willSkip++
			continue
		}
		if rows[i].SourceOU != rows[i].TargetOU {
			willMove++
		} else {
			willSkip++
		}
	}

	type lignePlan struct {
		Identifiant string `json:"identifiant"`
		OUSource    string `json:"ou_source"`
		OUCible     string `json:"ou_cible"`
		Action      string `json:"action"`
		Erreur      string `json:"erreur,omitempty"`
	}
	var lignes []lignePlan
	for _, row := range rows {
		action := "skip"
		if row.Error == "" && row.SourceOU != row.TargetOU {
			action = "move"
		}
		lignes = append(lignes, lignePlan{
			Identifiant: row.SAMAccountName,
			OUSource:    row.SourceOU,
			OUCible:     row.TargetOU,
			Action:      action,
			Erreur:      row.Error,
		})
	}

	r := struct {
		Lignes   []lignePlan `json:"lignes"`
		Total    int         `json:"total"`
		WillMove int         `json:"will_move"`
		WillSkip int         `json:"will_skip"`
	}{
		Lignes:   lignes,
		Total:    len(lignes),
		WillMove: willMove,
		WillSkip: willSkip,
	}

	if *jsonOut {
		return ecrireJSON(stdout, r)
	}
	fmt.Fprintf(stdout, "Plan de migration : %d à déplacer · %d inchangés · %d total\n", willMove, willSkip, len(lignes))
	for _, l := range lignes {
		fmt.Fprintf(stdout, "  %s : %s → %s (%s)\n", l.Identifiant, l.OUSource, l.OUCible, l.Action)
	}
	return nil
}

// cmdExport liste les champs d'export disponibles (M6).
func cmdExport(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return cmdExportFields(stdout)
	}
	switch args[0] {
	case "fields":
		return cmdExportFields(stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : fields)", args[0])}
	}
}

// cmdExportFields liste les champs d'export disponibles (M6).
func cmdExportFields(stdout io.Writer) error {
	champs := []champExport{
		{Nom: string(ad.FieldSAMAccountName), Description: "Identifiant de compte (sAMAccountName)"},
		{Nom: string(ad.FieldDisplayName), Description: "Nom complet (displayName)"},
		{Nom: string(ad.FieldGivenName), Description: "Prénom (givenName)"},
		{Nom: string(ad.FieldSN), Description: "Nom (sn)"},
		{Nom: string(ad.FieldEmail), Description: "Adresse mail (mail)"},
		{Nom: string(ad.FieldOU), Description: "Unité d'organisation (ou)"},
		{Nom: string(ad.FieldUserPrincipalName), Description: "Principal nom utilisateur (userPrincipalName)"},
		{Nom: string(ad.FieldDescription), Description: "Description"},
		{Nom: string(ad.FieldEmployeeID), Description: "Identifiant employé (employeeID)"},
		{Nom: string(ad.FieldDepartment), Description: "Département (department)"},
		{Nom: string(ad.FieldTitle), Description: "Intitulé de poste (title)"},
		{Nom: string(ad.FieldTelephone), Description: "Téléphone (telephoneNumber)"},
		{Nom: string(ad.FieldMobile), Description: "Mobile (mobile)"},
	}
	r := resultatExportFields{
		Champs:    champs,
		Templates: []string{string(ad.LabelL7160), string(ad.LabelL7163)},
	}
	return ecrireJSON(stdout, r)
}
