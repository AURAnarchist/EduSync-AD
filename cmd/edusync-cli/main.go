// Commande edusync-cli — interface en ligne de commande d'EduSync-AD.
//
// Destinée aux scripts, à l'intégration continue et à l'IA : toutes les
// commandes parlent JSON, sortent un code de retour explicite et ne dépendent
// d'aucune interface graphique. Le cœur partagé est celui de l'application
// desktop (internal/…), conformément à ROADMAP_GO_MIGRATION.md §3.
//
// État : la connexion AD réelle (bind LDAP/GSSAPI) arrive avec le pool de
// connexions de la Phase 0 (jours 5-6) ; la commande « connect » valide
// dès à présent la résolution, la TCP et la poignée de main TLS/LDAPS.
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
)

// version est renseignée au build :
//
//	go build -ldflags "-X main.version=1.17.1" ./cmd/edusync-cli
var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "erreur: %v\n", err)
		var exitCode *codeSortie
		if errors.As(err, &exitCode) {
			os.Exit(exitCode.code)
		}
		os.Exit(1)
	}
}

// codeSortie porte un code de retour d'erreur métier (2 = usage invalide).
type codeSortie struct {
	code int
	err  error
}

func (c *codeSortie) Error() string { return c.err.Error() }
func (c *codeSortie) Unwrap() error { return c.err }

func usage(sortie io.Writer) {
	fmt.Fprint(sortie, `EduSync AD — CLI

Utilisation :
  edusync-cli <commande> [options]

Commandes :
  version                 Affiche la version
  paths                   Affiche les chemins locaux (config, données, journal)
  config show             Affiche la configuration au format JSON
  audit list              Liste le journal d'actions
  users import-simulate   Simule l'import d'utilisateurs depuis un CSV (JSON)
  password simulate       Génère des mots de passe selon une politique (JSON)
  migration plan          Construit un plan de migration depuis un CSV (JSON)
  export fields           Liste les champs d'export disponibles (JSON)
  departs simulate        Simule un départ (sans écriture AD)
  departs pending list    Liste les suppressions planifiées
  departs pending purge   Purge les suppressions expirées
  profils simulate        Simule l'application d'un profil utilisateur (JSON)
  profils execute         Applique un profil utilisateur (JSON)
  connect                 Teste l'accessibilité d'un contrôleur (TCP + TLS)

Exemples :
  edusync-cli paths
  edusync-cli config show
  edusync-cli audit list --limit 20 --json
  edusync-cli connect --server dc1.edusync.local --port 636
  edusync-cli connect --server dc1.local --ca /etc/ssl/adcs-root.pem

Toutes les commandes --json produisent une sortie stable pour scripts/IA.
`)
}

func run(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		usage(stdout)
		return &codeSortie{code: 2, err: errors.New("commande manquante")}
	}

	switch args[0] {
	case "help", "-h", "--help":
		usage(stdout)
		return nil
	case "version":
		fmt.Fprintf(stdout, "edusync-cli %s\n", version)
		return nil
	case "paths":
		return cmdPaths(args[1:], stdout, stderr)
	case "config":
		return cmdConfig(args[1:], stdout, stderr)
	case "audit":
		return cmdAudit(args[1:], stdout, stderr)
	case "users":
		return cmdUsers(args[1:], stdout, stderr)
	case "password":
		return cmdPassword(args[1:], stdout, stderr)
	case "migration":
		return cmdMigration(args[1:], stdout, stderr)
	case "export":
		return cmdExport(args[1:], stdout, stderr)
	case "departs":
		return cmdDepart(args[1:], stdout, stderr)
	case "profils":
		return cmdProfils(args[1:], stdout, stderr)
	case "connect":
		return cmdConnect(args[1:], stdout, stderr)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("commande inconnue %q (voir « edusync-cli help »)", args[0])}
	}
}

// cmdPaths expose les emplacements utilisés par l'application — indispensable
// pour sauvegarder, migrer ou diagnostiquer un poste.
func cmdPaths(args []string, stdout, _ io.Writer) error {
	fs := flag.NewFlagSet("paths", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cheminConfig, err := config.DefaultPath()
	if err != nil {
		return err
	}
	cheminDonnees, err := config.DataDir()
	if err != nil {
		return err
	}
	cheminCache, err := config.CacheDir()
	if err != nil {
		return err
	}
	journal, err := audit.DefaultPath()
	if err != nil {
		return err
	}

	valeurs := struct {
		Configuration string `json:"configuration"`
		Donnees       string `json:"donnees"`
		Cache         string `json:"cache"`
		Journal       string `json:"journal"`
		Version       string `json:"version"`
	}{
		Configuration: cheminConfig,
		Donnees:       cheminDonnees,
		Cache:         cheminCache,
		Journal:       journal,
		Version:       version,
	}

	if *jsonOut {
		return ecrireJSON(stdout, valeurs)
	}
	fmt.Fprintf(stdout, "configuration : %s\n", valeurs.Configuration)
	fmt.Fprintf(stdout, "données       : %s\n", valeurs.Donnees)
	fmt.Fprintf(stdout, "cache         : %s\n", valeurs.Cache)
	fmt.Fprintf(stdout, "journal       : %s\n", valeurs.Journal)
	return nil
}

// cmdConfig affiche la configuration effective.
func cmdConfig(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « show »")}
	}
	if args[0] != "show" {
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}

	fs := flag.NewFlagSet("config show", flag.ContinueOnError)
	chemin := fs.String("f", "", "chemin d'un config.json alternatif")
	if err := fs.Parse(args[1:]); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg, err := config.Load(*chemin)
	if err != nil {
		fmt.Fprintf(stderr, "avertissement: %v (valeurs par défaut utilisées)\n", err)
	}
	return ecrireJSON(stdout, cfg)
}

// cmdAudit liste le journal d'actions avec filtres (mêmes critères que l'UI).
func cmdAudit(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « list »")}
	}
	if args[0] != "list" {
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}

	fs := flag.NewFlagSet("audit list", flag.ContinueOnError)
	limite := fs.Int("limit", 50, "nombre maximum d'entrées")
	typeAction := fs.String("type", "", "filtre par type d'action")
	resultat := fs.String("resultat", "", "filtre par résultat")
	domaine := fs.String("domaine", "", "filtre par domaine")
	operateur := fs.String("operateur", "", "filtre par opérateur")
	recherche := fs.String("search", "", "recherche dans le compte ou le détail")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	chemin := fs.String("f", "", "chemin d'un journal.db alternatif")
	if err := fs.Parse(args[1:]); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	l, err := audit.Open(*chemin)
	if err != nil {
		return err
	}
	defer l.Close() //nolint:errcheck

	entries, err := l.List(audit.Filter{
		Limit:      *limite,
		ActionType: *typeAction,
		Resultat:   *resultat,
		Domaine:    *domaine,
		Operateur:  *operateur,
		Recherche:  *recherche,
	})
	if err != nil {
		return err
	}

	if *jsonOut {
		if entries == nil {
			entries = []audit.Entry{}
		}
		return ecrireJSON(stdout, entries)
	}
	if len(entries) == 0 {
		fmt.Fprintln(stdout, "(journal vide)")
		return nil
	}
	for _, e := range entries {
		fmt.Fprintf(stdout, "%s  %-18s %-20s %-8s %-15s %s\n",
			e.Timestamp, e.ActionType, e.Compte, e.Resultat, e.Utilisateur, e.Detail)
	}
	return nil
}

// ecrireJSON sérialise avec une indentation stable (compatibilité outils).
func ecrireJSON(w io.Writer, valeur any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(valeur); err != nil {
		return fmt.Errorf("sérialisation JSON: %w", err)
	}
	return nil
}
