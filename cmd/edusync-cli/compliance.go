package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/assistant"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/compliance"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/crypto"
	"github.com/estemobs/EduSync-AD/internal/multisite"
	"github.com/estemobs/EduSync-AD/internal/portal"
	"github.com/estemobs/EduSync-AD/internal/vault"
)

// cmdCompliance gère le module M30 « Conformité & sécurité » : MFA admin,
// session unique, granularité des mots de passe, signature des exports et
// rapport RGPD.
func cmdCompliance(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New(
			"sous-commande manquante : « mfa », « secret », « session », « pso », « sign », « verify » ou « rgpd »")}
	}
	switch args[0] {
	case "mfa":
		return cmdComplianceMFA(args[1:], stdout)
	case "secret":
		return cmdComplianceSecret(args[1:], stdout)
	case "session":
		return cmdComplianceSession(args[1:], stdout)
	case "pso":
		return cmdCompliancePSO(args[1:], stdout)
	case "sign":
		return cmdComplianceSign(args[1:], stdout)
	case "verify":
		return cmdComplianceVerify(args[1:], stdout)
	case "rgpd":
		return cmdComplianceRGPD(args[1:], stdout, stderr)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// chargerConfigConformite lit `conformite.json` (chemin par défaut si vide).
func chargerConfigConformite(chemin string) compliance.Config {
	return compliance.ChargerConfig(chemin)
}

// cmdComplianceMFA vérifie que la configuration de double authentification est
// complète.
func cmdComplianceMFA(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("compliance mfa", flag.ContinueOnError)
	fichier := fs.String("config", "", "chemin de conformite.json (défaut : partagé)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := chargerConfigConformite(*fichier)
	rapport := compliance.VerifierMFAAdmin(cfg)

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"mfa_status":      rapport.Statut,
			"details":         rapport.Details,
			"recommandations": rapport.Recommandations,
		})
	}
	fmt.Fprintf(stdout, "Statut MFA : %s\n", rapport.Statut)
	for _, detail := range rapport.Details {
		fmt.Fprintf(stdout, "  - %s\n", detail)
	}
	for _, recommandation := range rapport.Recommandations {
		fmt.Fprintf(stdout, "  → %s\n", recommandation)
	}
	return nil
}

// cmdComplianceSecret génère un secret TOTP et des codes de secours. Seules les
// empreintes des codes de secours sont affichées en plus des codes en clair :
// l'opérateur doit les conserver hors ligne, ils ne sont plus affichés ensuite.
func cmdComplianceSecret(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("compliance secret", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	secret, err := compliance.GenererSecretTOTP()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	codes, empreintes, err := compliance.GenererCodesSecours()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"methode":          compliance.MethodeTOTP,
			"mfa_secret":       secret,
			"codes_secours":    codes,
			"codes_empreintes": empreintes,
		})
	}
	fmt.Fprintf(stdout, "Secret TOTP      : %s\n", secret)
	fmt.Fprintln(stdout, "Codes de secours :")
	for _, code := range codes {
		fmt.Fprintf(stdout, "  %s\n", code)
	}
	fmt.Fprintln(stdout, "Conservez ces codes hors ligne : ils ne seront plus affichés.")
	return nil
}

// cmdComplianceSession contrôle la politique de session unique à partir du
// journal d'actions (sessions distinctes par opérateur).
func cmdComplianceSession(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("compliance session", flag.ContinueOnError)
	fichier := fs.String("config", "", "chemin de conformite.json (défaut : partagé)")
	journal := fs.String("journal", "", "chemin du journal (défaut : partagé)")
	limite := fs.Int("limit", 500, "nombre maximum d'entrées analysées")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := chargerConfigConformite(*fichier)

	cheminJournal := *journal
	if cheminJournal == "" {
		var err error
		if cheminJournal, err = audit.DefaultPath(); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	journalOuvert, err := audit.Open(cheminJournal)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = journalOuvert.Close() }()

	entrees, err := journalOuvert.List(audit.Filter{Limit: *limite})
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	// Une entrée par session : les sessions distinctes de chaque opérateur.
	vues := map[string]bool{}
	sessions := []compliance.Session{}
	for _, entree := range entrees {
		if entree.SessionID == "" {
			continue
		}
		cle := strings.ToLower(entree.Utilisateur + "@" + entree.Domaine + "#" + entree.SessionID)
		if vues[cle] {
			continue
		}
		vues[cle] = true
		sessions = append(sessions, compliance.Session{
			Operateur: entree.Utilisateur, Domaine: entree.Domaine,
			SessionID: entree.SessionID, Debut: entree.Timestamp,
		})
	}

	rapport := compliance.VerifierSessionUnique(cfg, sessions)
	if *jsonOut {
		return ecrireJSON(stdout, rapport)
	}
	fmt.Fprintf(stdout, "Statut : %s (%d sessions)\n", rapport.Statut, rapport.SessionsActives)
	for _, doublon := range rapport.Doublons {
		fmt.Fprintf(stdout, "  - %s\n", doublon)
	}
	for _, recommandation := range rapport.Recommandations {
		fmt.Fprintf(stdout, "  → %s\n", recommandation)
	}
	return nil
}

// cmdCompliancePSO analyse la granularité des politiques de mots de passe et
// génère le script PowerShell de publication.
func cmdCompliancePSO(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("compliance pso", flag.ContinueOnError)
	fichierConfig := fs.String("config", "", "chemin de config.json (défaut : partagé)")
	script := fs.String("script", "", "chemin d'écriture du script PowerShell")
	baseDN := fs.String("base-dn", "", "DN de base du domaine (commentaire du script)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg, err := config.Load(*fichierConfig)
	if err != nil {
		cfg = config.DefaultAppConfig()
	}
	analyse := compliance.AnalyserGranulariteMDP(cfg)

	cheminScript := ""
	if *script != "" {
		contenu := compliance.ScriptPSO(analyse, *baseDN)
		if err := os.WriteFile(*script, []byte(contenu), 0o600); err != nil {
			return &codeSortie{code: 1, err: fmt.Errorf("écriture du script: %w", err)}
		}
		cheminScript = *script
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"politiques":       analyse.Politiques,
			"differences":      analyse.Differences,
			"recommandations":  analyse.Recommandations,
			"score_conformite": analyse.Score,
			"script":           cheminScript,
		})
	}
	fmt.Fprintf(stdout, "Score de conformité : %d/100\n", analyse.Score)
	for _, politique := range analyse.Politiques {
		fmt.Fprintf(stdout, "  %s (%s) : %d caractères\n",
			politique.Nom, strings.Join(politique.Profils, ", "), politique.Longueur)
	}
	for _, difference := range analyse.Differences {
		fmt.Fprintf(stdout, "  écart : %s\n", difference)
	}
	for _, recommandation := range analyse.Recommandations {
		fmt.Fprintf(stdout, "  → %s\n", recommandation)
	}
	if cheminScript != "" {
		fmt.Fprintf(stdout, "Script écrit : %s\n", cheminScript)
	}
	return nil
}

// cmdComplianceSign signe un export JSON avec la clé AES locale.
func cmdComplianceSign(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("compliance sign", flag.ContinueOnError)
	fichier := fs.String("fichier", "", "fichier JSON à signer (obligatoire)")
	clePath := fs.String("cle", "", "chemin de secret.key (défaut : partagé)")
	sortie := fs.String("sortie", "", "fichier où écrire la signature (défaut : sortie standard)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*fichier) == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --fichier est obligatoire")}
	}

	cheminCle, err := cheminCle(*clePath)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	cle, err := crypto.LoadOrCreateKey(cheminCle)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	donnees, err := lireJSONFichier(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	signature, err := compliance.SignerExport(donnees, cle, time.Now())
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	if *sortie != "" {
		contenu, err := json.MarshalIndent(signature, "", "  ")
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		if err := os.WriteFile(*sortie, contenu, 0o600); err != nil {
			return &codeSortie{code: 1, err: fmt.Errorf("écriture de la signature: %w", err)}
		}
	}
	if *jsonOut || *sortie != "" {
		return ecrireJSON(stdout, signature)
	}
	fmt.Fprintf(stdout, "signature %s\nhorodatage %s\ndata_hash %s\n",
		signature.Signature, signature.Horodatage, signature.DataHash)
	return nil
}

// cmdComplianceVerify vérifie la signature d'un export JSON.
func cmdComplianceVerify(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("compliance verify", flag.ContinueOnError)
	fichier := fs.String("fichier", "", "fichier JSON signé (obligatoire)")
	signaturePath := fs.String("signature", "", "fichier de signature (obligatoire)")
	clePath := fs.String("cle", "", "chemin de secret.key (défaut : partagé)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*fichier) == "" || strings.TrimSpace(*signaturePath) == "" {
		return &codeSortie{code: 2, err: errors.New("les options --fichier et --signature sont obligatoires")}
	}

	cheminCle, err := cheminCle(*clePath)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	cle, err := crypto.LoadOrCreateKey(cheminCle)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	donnees, err := lireJSONFichier(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	brutSignature, err := os.ReadFile(*signaturePath)
	if err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("lecture de la signature: %w", err)}
	}
	var signature compliance.SignatureExport
	if err := json.Unmarshal(brutSignature, &signature); err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("signature illisible: %w", err)}
	}

	if err := compliance.VerifierExport(donnees, signature, cle); err != nil {
		if *jsonOut {
			_ = ecrireJSON(stdout, map[string]any{"valide": false, "erreur": err.Error()})
			return &codeSortie{code: 3, err: err}
		}
		return &codeSortie{code: 3, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"valide": true, "horodatage": signature.Horodatage})
	}
	fmt.Fprintf(stdout, "Signature valide (horodatée %s)\n", signature.Horodatage)
	return nil
}

// cmdComplianceRGPD génère le rapport RGPD à partir du journal, du portail, du
// coffre et, si un site est enregistré, de l'annuaire.
func cmdComplianceRGPD(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("compliance rgpd", flag.ContinueOnError)
	fichierSites := fs.String("sites-file", "", "chemin de domaines.json (défaut : partagé)")
	cleSites := fs.String("sites-key", "", "chemin de la clé des sites (défaut : partagé)")
	site := fs.String("site", "", "identifiant du site enregistré (défaut : le premier)")
	ldaps := fs.Bool("ldaps", true, "connexion LDAPS (port 636)")
	journal := fs.String("journal", "", "chemin du journal (défaut : partagé)")
	basePortail := fs.String("portail", "", "chemin de portail.db (défaut : partagé)")
	fichierConfig := fs.String("config", "", "chemin de conformite.json (défaut : partagé)")
	personne := fs.String("personne", "", "identifiant recherché (droit à l'oubli)")
	conservation := fs.Int("conservation", 0, "durée de conservation en jours (0 = configurée)")
	csv := fs.String("csv", "", "écrit le rapport au format CSV à ce chemin")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfgConformite := chargerConfigConformite(*fichierConfig)
	if *conservation <= 0 {
		*conservation = cfgConformite.ConservationJours
	}

	sources := compliance.SourcesRGPD{}

	// Comptes de l'annuaire : la collecte est facultative (l'annuaire peut
	// être injoignable) mais son échec est signalé.
	profils, err := multisite.ChargerSites(*fichierSites, *cleSites)
	if err == nil && len(profils) > 0 {
		profil := multisite.TrouverProfil(profils, *site)
		if profil == nil {
			profil = &profils[0]
		}
		if inventaire, errAd := collecterComptesRGPD(*profil, *ldaps); errAd != nil {
			fmt.Fprintf(stderr, "avertissement : annuaire non collecté (%v)\n", errAd)
		} else {
			sources.Comptes = inventaire
		}
	}

	// Journal d'actions.
	cheminJournal := strings.TrimSpace(*journal)
	if cheminJournal == "" {
		cheminJournal, err = audit.DefaultPath()
	}
	if err == nil && cheminJournal != "" {
		journalOuvert, errOuverture := audit.Open(cheminJournal)
		if errOuverture == nil {
			if entrees, errListe := journalOuvert.List(audit.Filter{Limit: 5000}); errListe == nil {
				sources.Entrees = entrees
			}
			_ = journalOuvert.Close()
		}
	}

	// Pré-inscriptions du portail.
	cheminPortail := strings.TrimSpace(*basePortail)
	if cheminPortail == "" {
		cheminPortail, err = portal.CheminBaseDefaut()
	}
	if err == nil && cheminPortail != "" {
		magasin, errOuverture := portal.OuvrirStore(cheminPortail)
		if errOuverture == nil {
			if demandes, errListe := magasin.ListerDemandes(""); errListe == nil {
				sources.Demandes = demandes
			}
			_ = magasin.Fermer()
		}
	}

	// Secrets du coffre local.
	if cheminCoffre, errCoffre := vault.CheminParDefaut(); errCoffre == nil {
		if _, errStat := os.Stat(cheminCoffre); errStat == nil {
			cheminCle, errCle := vault.CheminCleParDefaut()
			if errCle == nil {
				if coffre, errOuverture := vault.New(cheminCoffre, cheminCle); errOuverture == nil {
					if sams, errListe := coffre.Liste(); errListe == nil {
						sources.Secrets = sams
					}
					_ = coffre.Close()
				}
			}
		}
	}

	options := compliance.OptionsRGPD{
		Personne:          *personne,
		ConservationJours: *conservation,
	}
	rapport := compliance.GenererRapportRGPD(sources, options, time.Now())

	if *csv != "" {
		ecrit, err := compliance.ExporterRapportRGPDCSV(rapport, *csv)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		if !*jsonOut {
			fmt.Fprintf(stdout, "Rapport RGPD écrit : %s\n", ecrit)
		}
	}
	if *jsonOut {
		return ecrireJSON(stdout, rapport)
	}
	if *csv != "" {
		return nil
	}

	fmt.Fprintf(stdout, "Rapport RGPD — %s\n", rapport.GenerationDate)
	fmt.Fprintf(stdout, "  comptes : %d (%d actifs, %d désactivés, %d anciens)\n",
		rapport.Inventaire.TotalComptes, rapport.Inventaire.ComptesActifs,
		rapport.Inventaire.ComptesDesactives, rapport.Inventaire.ComptesAnciens)
	fmt.Fprintf(stdout, "  journal : %d entrées (%d simulées, %d anciennes)\n",
		rapport.Inventaire.EntreesJournal, rapport.Inventaire.EntreesSimulation,
		rapport.Inventaire.EntreesAnciennes)
	fmt.Fprintf(stdout, "  portail : %d demandes · coffre : %d secrets\n",
		rapport.Inventaire.DemandesPortail, rapport.Inventaire.SecretsCoffre)
	if *personne != "" {
		fmt.Fprintf(stdout, "  droit à l'oubli (%s) : %d référence(s)\n", *personne, len(rapport.DroitOubli))
	}
	for _, recommandation := range rapport.Recommandations {
		fmt.Fprintf(stdout, "  [%s] %s\n", recommandation.Niveau, recommandation.Message)
	}
	return nil
}

// collecterComptesRGPD collecte les comptes d'un site en projetant l'inventaire
// de l'assistant sur les champs attendus par le rapport RGPD.
func collecterComptesRGPD(profil multisite.ProfilDomaine, ldaps bool) ([]compliance.CompteRGPD, error) {
	pool, err := poolDepuisProfil(profil, ldaps)
	if err != nil {
		return nil, err
	}
	defer func() { _ = pool.Close() }()

	ctx, annuler := context.WithTimeout(context.Background(), 30*time.Second)
	defer annuler()

	inventaire, err := assistant.NouveauCollecteurAD(pool).Collecter(ctx, "")
	if err != nil {
		return nil, err
	}
	comptes := make([]compliance.CompteRGPD, 0, len(inventaire.Comptes))
	for _, compte := range inventaire.Comptes {
		comptes = append(comptes, compliance.CompteRGPD{
			Identifiant: compte.Identifiant,
			NomComplet:  compte.NomComplet,
			Mail:        compte.Mail,
			OU:          compte.OU,
			Actif:       compte.Actif,
		})
	}
	return comptes, nil
}

// cheminCle retourne le chemin effectif de secret.key.
func cheminCle(chemin string) (string, error) {
	if strings.TrimSpace(chemin) != "" {
		return chemin, nil
	}
	return compliance.CheminCleDefaut()
}

// lireJSONFichier lit un fichier JSON et retourne son contenu décodé.
func lireJSONFichier(chemin string) (any, error) {
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil, fmt.Errorf("lecture de %s: %w", chemin, err)
	}
	var valeur any
	if err := json.Unmarshal(donnees, &valeur); err != nil {
		return nil, fmt.Errorf("JSON illisible dans %s: %w", chemin, err)
	}
	return valeur, nil
}
