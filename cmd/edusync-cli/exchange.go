package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/exchange"
)

// cmdExchange gère les commandes Microsoft Exchange (M20).
func cmdExchange(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « config », « plans », « onprem-script », « online-script », « policy-script » ou « report »")}
	}
	switch args[0] {
	case "config":
		return cmdExchangeConfig(args[1:], stdout)
	case "plans":
		return cmdExchangePlans(args[1:], stdout)
	case "onprem-script":
		return cmdExchangeScript(args[1:], stdout, "onprem")
	case "online-script":
		return cmdExchangeScript(args[1:], stdout, "online")
	case "policy-script":
		return cmdExchangePolicyScript(args[1:], stdout)
	case "report":
		return cmdExchangeReport(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// optionsExchange déclare les options de configuration Exchange.
func optionsExchange(fs *flag.FlagSet) func(exchange.Config) exchange.Config {
	mode := fs.String("mode", "", "onprem | online")
	smtp := fs.String("smtp-domain", "", "domaine SMTP (ex : lycee.fr)")
	motif := fs.String("alias-pattern", "", "motif d'alias (ex : {prenom}.{nom})")
	quota := fs.String("mailbox-quota", "", "quota de boîte (ex : 10GB)")
	archive := fs.Bool("archive", false, "activer l'archive en ligne")
	retention := fs.String("retention", "", "politique de rétention")
	serveur := fs.String("exchange-server", "", "serveur Exchange (on-prem)")
	bdd := fs.String("database", "", "base de données (Enable-Mailbox)")
	tenant := fs.String("tenant-id", "", "tenant ID (en ligne)")
	client := fs.String("client-id", "", "client ID (en ligne)")
	empreinte := fs.String("certificate-thumbprint", "", "empreinte de certificat (en ligne)")
	organisation := fs.String("organization", "", "organisation (en ligne)")

	return func(cfg exchange.Config) exchange.Config {
		visites := map[string]bool{}
		fs.Visit(func(f *flag.Flag) { visites[f.Name] = true })
		if visites["mode"] {
			cfg.Mode = *mode
		}
		if visites["smtp-domain"] {
			cfg.SMTPDomain = *smtp
		}
		if visites["alias-pattern"] {
			cfg.AliasPattern = *motif
		}
		if visites["mailbox-quota"] {
			cfg.MailboxQuota = *quota
		}
		if visites["archive"] {
			cfg.ArchiveEnabled = *archive
		}
		if visites["retention"] {
			cfg.RetentionPolicy = *retention
		}
		if visites["exchange-server"] {
			cfg.ExchangeServer = *serveur
		}
		if visites["database"] {
			cfg.Database = *bdd
		}
		if visites["tenant-id"] {
			cfg.TenantID = *tenant
		}
		if visites["client-id"] {
			cfg.ClientID = *client
		}
		if visites["certificate-thumbprint"] {
			cfg.CertificateThumbprint = *empreinte
		}
		if visites["organization"] {
			cfg.Organization = *organisation
		}
		return cfg
	}
}

func cmdExchangeConfig(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("exchange config", flag.ContinueOnError)
	appliquer := optionsExchange(fs)
	save := fs.Bool("save", false, "enregistrer la configuration")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	dir, err := exchange.DossierConfig()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	chemin := dir + "/" + exchange.FichierConfig
	cfg := appliquer(exchange.ChargerConfig(chemin))
	if *save {
		if err := exchange.SauvegarderConfig(chemin, cfg); err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	if *jsonOut {
		return ecrireJSON(stdout, cfg)
	}
	fmt.Fprintf(stdout, "Mode          : %s\n", cfg.Mode)
	fmt.Fprintf(stdout, "Domaine SMTP  : %s\n", cfg.SMTPDomain)
	fmt.Fprintf(stdout, "Motif d'alias : %s\n", cfg.AliasPattern)
	fmt.Fprintf(stdout, "Quota         : %s (alerte %s)\n", cfg.MailboxQuota, exchange.QuotaAlerte(cfg.MailboxQuota))
	fmt.Fprintf(stdout, "Erreurs       : %s\n", strings.Join(cfg.Validate(), " ; "))
	return nil
}

// lirePlansExchange lit les comptes (et groupes) depuis des fichiers JSON puis
// construit les plans.
func lirePlansExchange(src, groupsSrc, domain string, cfg exchange.Config) ([]exchange.PlanBal, error) {
	if src == "" {
		return nil, &codeSortie{code: 2, err: errors.New("l'option --src est obligatoire")}
	}
	donnees, err := os.ReadFile(src)
	if err != nil {
		return nil, &codeSortie{code: 1, err: fmt.Errorf("lecture des comptes %s: %w", src, err)}
	}
	var utilisateurs []exchange.UtilisateurBal
	if err := json.Unmarshal(donnees, &utilisateurs); err != nil {
		return nil, &codeSortie{code: 1, err: fmt.Errorf("décodage des comptes %s: %w", src, err)}
	}
	plans := exchange.ConstruirePlansBals(utilisateurs, cfg, domain)

	if groupsSrc != "" {
		gDonnees, err := os.ReadFile(groupsSrc)
		if err != nil {
			return nil, &codeSortie{code: 1, err: fmt.Errorf("lecture des groupes %s: %w", groupsSrc, err)}
		}
		var groupes []exchange.GroupeBal
		if err := json.Unmarshal(gDonnees, &groupes); err != nil {
			return nil, &codeSortie{code: 1, err: fmt.Errorf("décodage des groupes %s: %w", groupsSrc, err)}
		}
		plans = append(plans, exchange.ConstruirePlansGroupes(groupes, cfg)...)
	}
	return plans, nil
}

func cmdExchangePlans(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("exchange plans", flag.ContinueOnError)
	appliquer := optionsExchange(fs)
	src := fs.String("src", "", "fichier JSON des comptes (obligatoire)")
	groupsSrc := fs.String("groups-src", "", "fichier JSON des groupes (optionnel)")
	domain := fs.String("domain", "", "domaine pour l'UPN (ex : lycee.local)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	cfg := appliquer(exchange.ChargerConfig(""))
	plans, err := lirePlansExchange(*src, *groupsSrc, *domain, cfg)
	if err != nil {
		return err
	}
	if *jsonOut {
		return ecrireJSON(stdout, plans)
	}
	for _, plan := range plans {
		fmt.Fprintln(stdout, plan.Label())
	}
	return nil
}

func cmdExchangeScript(args []string, stdout io.Writer, mode string) error {
	fs := flag.NewFlagSet("exchange "+mode+"-script", flag.ContinueOnError)
	appliquer := optionsExchange(fs)
	src := fs.String("src", "", "fichier JSON des comptes (obligatoire)")
	groupsSrc := fs.String("groups-src", "", "fichier JSON des groupes (optionnel)")
	domain := fs.String("domain", "", "domaine pour l'UPN")
	dest := fs.String("dest", "", "écrire le script dans ce fichier (défaut : stdout)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	cfg := appliquer(exchange.ChargerConfig(""))
	cfg.Mode = mode
	plans, err := lirePlansExchange(*src, *groupsSrc, *domain, cfg)
	if err != nil {
		return err
	}
	script := exchange.ScriptSurSite(plans, cfg)
	if mode == "online" {
		script = exchange.ScriptEnLigne(plans, cfg)
	}
	return ecrireScriptExchange(*dest, script, stdout)
}

func cmdExchangePolicyScript(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("exchange policy-script", flag.ContinueOnError)
	appliquer := optionsExchange(fs)
	dest := fs.String("dest", "", "écrire le script dans ce fichier (défaut : stdout)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	cfg := appliquer(exchange.ChargerConfig(""))
	return ecrireScriptExchange(*dest, exchange.ScriptPolitiqueAdresses(cfg), stdout)
}

func cmdExchangeReport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("exchange report", flag.ContinueOnError)
	appliquer := optionsExchange(fs)
	src := fs.String("src", "", "fichier JSON des comptes (obligatoire)")
	groupsSrc := fs.String("groups-src", "", "fichier JSON des groupes (optionnel)")
	domain := fs.String("domain", "", "domaine pour l'UPN")
	dest := fs.String("dest", "", "fichier CSV de sortie (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *dest == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --dest est obligatoire")}
	}
	cfg := appliquer(exchange.ChargerConfig(""))
	plans, err := lirePlansExchange(*src, *groupsSrc, *domain, cfg)
	if err != nil {
		return err
	}
	if err := exchange.RapportCSV(plans, *dest); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"dest": *dest, "plans": len(plans)})
	}
	fmt.Fprintf(stdout, "Rapport de %d adresse(s) écrit vers %s\n", len(plans), *dest)
	return nil
}

// ecrireScriptExchange écrit le script sur disque (avec BOM) ou sur la sortie.
func ecrireScriptExchange(dest, script string, stdout io.Writer) error {
	if dest == "" {
		fmt.Fprint(stdout, script)
		return nil
	}
	if err := exchange.SauvegarderScript(dest, script); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	fmt.Fprintf(stdout, "Script écrit : %s\n", dest)
	return nil
}
