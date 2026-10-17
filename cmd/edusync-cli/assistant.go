package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/assistant"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/multisite"
)

// cmdAssistant gère l'assistant IA de M29 : anomalies, nettoyage de fin
// d'année, identifiant intelligent, analyse de sécurité et du journal.
func cmdAssistant(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New(
			"sous-commande manquante : « anomalies », « cleanup », « smart-id », « security » ou « journal »")}
	}
	switch args[0] {
	case "anomalies":
		return cmdAssistantAnomalies(args[1:], stdout)
	case "cleanup":
		return cmdAssistantCleanup(args[1:], stdout)
	case "smart-id":
		return cmdAssistantSmartID(args[1:], stdout)
	case "security":
		return cmdAssistantSecurity(args[1:], stdout)
	case "journal":
		return cmdAssistantJournal(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// optionsInventaire ajoute les options communes de collecte AD (mêmes options
// que « portal approve » : site, fichier chiffré, LDAPS).
func optionsInventaire(fs *flag.FlagSet) (fichierSites, cleSites, site *string, ldaps, jsonOut *bool) {
	fichierSites = fs.String("sites-file", "", "chemin de domaines.json (défaut : partagé)")
	cleSites = fs.String("sites-key", "", "chemin de la clé des sites (défaut : partagé)")
	site = fs.String("site", "", "identifiant du site enregistré (défaut : le premier)")
	ldaps = fs.Bool("ldaps", true, "connexion LDAPS (port 636)")
	jsonOut = fs.Bool("json", false, "sortie JSON")
	return
}

// collecterInventaire construit un pool depuis un site enregistré puis collecte
// l'inventaire de l'annuaire.
func collecterInventaire(site, fichierSites, cleSites string, ldaps bool) (assistant.Inventaire, error) {
	profils, err := multisite.ChargerSites(fichierSites, cleSites)
	if err != nil {
		return assistant.Inventaire{}, &codeSortie{code: 1, err: err}
	}
	var profil *multisite.ProfilDomaine
	if site != "" {
		profil = multisite.TrouverProfil(profils, site)
	} else if len(profils) > 0 {
		profil = &profils[0]
	}
	if profil == nil {
		return assistant.Inventaire{}, &codeSortie{code: 2, err: errors.New(
			"aucun site enregistré : utilisez --site ou « multisite add »")}
	}

	pool, err := poolDepuisProfil(*profil, ldaps)
	if err != nil {
		return assistant.Inventaire{}, err
	}
	defer func() { _ = pool.Close() }()

	ctx, annuler := context.WithTimeout(context.Background(), 20*time.Second)
	defer annuler()

	inventaire, err := assistant.NouveauCollecteurAD(pool).Collecter(ctx, "")
	if err != nil {
		return assistant.Inventaire{}, &codeSortie{code: 1, err: err}
	}
	return inventaire, nil
}

// cmdAssistantAnomalies détecte les anomalies de l'annuaire d'un site.
func cmdAssistantAnomalies(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("assistant anomalies", flag.ContinueOnError)
	fichierSites, cleSites, site, ldaps, jsonOut := optionsInventaire(fs)
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	inventaire, err := collecterInventaire(*site, *fichierSites, *cleSites, *ldaps)
	if err != nil {
		return err
	}
	anomalies := assistant.DetecterAnomalies(inventaire, assistant.OptionsAnomaliesToutes())
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"anomalies": anomalies,
			"total":     len(anomalies),
		})
	}
	if len(anomalies) == 0 {
		fmt.Fprintln(stdout, "Aucune anomalie détectée.")
		return nil
	}
	for _, anomalie := range anomalies {
		fmt.Fprintf(stdout, "[%s] %s (%s)\n", anomalie.Gravite, anomalie.Message, anomalie.Type)
		for _, objet := range anomalie.Objets {
			fmt.Fprintf(stdout, "  - %s\n", objet)
		}
	}
	return nil
}

// cmdAssistantCleanup suggère le nettoyage de fin d'année.
func cmdAssistantCleanup(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("assistant cleanup", flag.ContinueOnError)
	fichierSites, cleSites, site, ldaps, jsonOut := optionsInventaire(fs)
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	inventaire, err := collecterInventaire(*site, *fichierSites, *cleSites, *ldaps)
	if err != nil {
		return err
	}
	suggestions := assistant.SuggererNettoyage(inventaire, assistant.OptionsNettoyageToutes())
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"suggestions": suggestions,
			"total":       len(suggestions),
		})
	}
	if len(suggestions) == 0 {
		fmt.Fprintln(stdout, "Aucun nettoyage suggéré.")
		return nil
	}
	for _, suggestion := range suggestions {
		fmt.Fprintf(stdout, "%-20s %s (%s)\n", suggestion.Action, suggestion.Message, suggestion.Type)
	}
	return nil
}

// cmdAssistantSmartID génère un identifiant unique selon la charte.
func cmdAssistantSmartID(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("assistant smart-id", flag.ContinueOnError)
	prenom := fs.String("prenom", "", "prénom de l'usager (obligatoire)")
	nom := fs.String("nom", "", "nom de l'usager (obligatoire)")
	format := fs.String("format", "", "format d'identifiant (défaut : format configuré)")
	existants := fs.String("existing", "", "identifiants déjà pris, séparés par des virgules")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*prenom) == "" || strings.TrimSpace(*nom) == "" {
		return &codeSortie{code: 2, err: errors.New("les options --prenom et --nom sont obligatoires")}
	}

	cfg := config.DefaultAppConfig()
	if chargee, err := config.Load(""); err == nil {
		cfg = chargee
	}

	pris := map[string]bool{}
	for _, identifiant := range strings.Split(*existants, ",") {
		if identifiant = strings.TrimSpace(identifiant); identifiant != "" {
			pris[strings.ToLower(identifiant)] = true
		}
	}

	identifiant, doublon, err := assistant.GenererIdentifiantIntelligent(
		cfg, *prenom, *nom, *format, pris)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"identifiant": identifiant,
			"doublon":     doublon,
			"format":      formatEffectif(cfg, *format),
		})
	}
	fmt.Fprintf(stdout, "%s\n", identifiant)
	if doublon {
		fmt.Fprintln(stdout, "(doublon résolu)")
	}
	return nil
}

// formatEffectif retourne le format utilisé (utile en sortie JSON).
func formatEffectif(cfg config.AppConfig, format string) string {
	if strings.TrimSpace(format) != "" {
		return strings.TrimSpace(format)
	}
	return cfg.IdentifiantFormatEleve
}

// cmdAssistantSecurity produit une analyse de sécurité.
func cmdAssistantSecurity(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("assistant security", flag.ContinueOnError)
	role := fs.String("role", "", "rôle de l'opérateur (super_admin, admin_site, …)")
	porteesVides := fs.Bool("portees-vides", false, "une délégation ne définit aucune portée")
	comptesTotal := fs.Int("comptes-total", 0, "nombre de comptes de l'annuaire")
	comptesDesactives := fs.Int("comptes-desactives", 0, "nombre de comptes désactivés")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := config.DefaultAppConfig()
	if chargee, err := config.Load(""); err == nil {
		cfg = chargee
	}

	rapport := assistant.AnalyserSecurite(cfg, assistant.ContexteSecurite{
		Role:              *role,
		PorteesVides:      *porteesVides,
		ComptesTotal:      *comptesTotal,
		ComptesDesactives: *comptesDesactives,
	})
	if *jsonOut {
		return ecrireJSON(stdout, rapport)
	}
	fmt.Fprintf(stdout, "Score de sécurité : %d/100\n", rapport.Score)
	if len(rapport.Recommandations) == 0 {
		fmt.Fprintln(stdout, "Aucune recommandation.")
		return nil
	}
	for _, recommandation := range rapport.Recommandations {
		fmt.Fprintf(stdout, "[%s] %s (%s)\n", recommandation.Niveau, recommandation.Message, recommandation.Type)
	}
	return nil
}

// cmdAssistantJournal analyse le journal d'actions.
func cmdAssistantJournal(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("assistant journal", flag.ContinueOnError)
	fichier := fs.String("journal", "", "chemin du journal (défaut : partagé)")
	limite := fs.Int("limit", 500, "nombre maximum d'entrées analysées")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	chemin := *fichier
	if chemin == "" {
		var err error
		if chemin, err = audit.DefaultPath(); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	journal, err := audit.Open(chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = journal.Close() }()

	entrees, err := journal.List(audit.Filter{Limit: *limite})
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	rapport := assistant.AnalyserJournal(entrees)
	if *jsonOut {
		return ecrireJSON(stdout, rapport)
	}
	fmt.Fprintf(stdout, "Journal : %d entrées · %d simulations · %d échecs · %d hors horaires\n",
		rapport.Total, rapport.Simulations, rapport.Echecs, rapport.HorsHoraires)
	for _, recommandation := range rapport.Recommandations {
		fmt.Fprintf(stdout, "[%s] %s\n", recommandation.Niveau, recommandation.Message)
	}
	return nil
}
