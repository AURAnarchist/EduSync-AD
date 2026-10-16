package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/audit"
)

// resultatDepart est le rapport structuré d'une opération de départ.
type resultatDepart struct {
	UserDN         string `json:"user_dn"`
	SAMAccountName string `json:"sam_account_name,omitempty"`
	DisplayName    string `json:"display_name,omitempty"`
	Mode           string `json:"mode"`
	DelaiJours     int    `json:"delai_jours"`
	Statut         string `json:"statut"`
	Detail         string `json:"detail"`
}

// cmdDepart simule ou exécute un départ (désactivation / réactivation).
// Sans connexion AD réelle, seule la validation des paramètres est possible.
func cmdDepart(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « simulate » ou « pending »")}
	}

	switch args[0] {
	case "simulate":
		return cmdDepartSimulate(args[1:], stdout, stderr)
	case "pending":
		return cmdDepartPending(args[1:], stdout, stderr)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : simulate | pending)", args[0])}
	}
}

// cmdDepartSimulate valide les paramètres d'un départ sans toucher à l'AD.
func cmdDepartSimulate(args []string, stdout, _ io.Writer) error {
	fs := flag.NewFlagSet("departs simulate", flag.ContinueOnError)
	userDN := fs.String("user-dn", "", "DN de l'utilisateur (obligatoire)")
	mode := fs.String("mode", "immediat", "mode : immediat, differe ou restaurer")
	delai := fs.Int("delai-jours", 0, "délai avant purge en jours (défaut 30 si différé)")
	motif := fs.String("motif", "", "motif du départ")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *userDN == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --user-dn est obligatoire")}
	}

	params := ad.DepartParams{
		UserDN:     *userDN,
		Mode:       ad.DepartMode(*mode),
		DelaiJours: *delai,
		Motif:      *motif,
	}
	if err := params.Validate(); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	// Sans pool AD, on ne peut pas vérifier l'existence du compte.
	// On signale que la simulation nécessite une connexion AD.
	res := resultatDepart{
		UserDN:     params.UserDN,
		Mode:       string(params.Mode),
		DelaiJours: params.DelaiJours,
		Statut:     "attente_ad",
		Detail:     "connexion AD requise pour vérifier l'existence du compte (utiliser l'UI ou le pool)",
	}

	if *jsonOut {
		var out []resultatDepart
		out = append(out, res)
		return ecrireJSON(stdout, out)
	}
	fmt.Fprintf(stdout, "Utilisateur : %s\n", res.UserDN)
	fmt.Fprintf(stdout, "Mode        : %s\n", res.Mode)
	fmt.Fprintf(stdout, "Délai       : %d jours\n", res.DelaiJours)
	fmt.Fprintf(stdout, "Statut      : %s\n", res.Statut)
	fmt.Fprintf(stdout, "Détail      : %s\n", res.Detail)
	return nil
}

// cmdDepartPending gère les suppressions planifiées du journal audit.
func cmdDepartPending(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « list » ou « purge »")}
	}

	switch args[0] {
	case "list":
		return cmdDepartPendingList(args[1:], stdout)
	case "purge":
		return cmdDepartPendingPurge(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q (attendu : list | purge)", args[0])}
	}
}

// cmdDepartPendingList liste les suppressions planifiées en attente.
func cmdDepartPendingList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("departs pending list", flag.ContinueOnError)
	chemin := fs.String("f", "", "chemin d'un journal.db alternatif")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	l, err := audit.Open(*chemin)
	if err != nil {
		return err
	}
	defer l.Close() //nolint:errcheck

	pending, err := l.ListPendingDeletions()
	if err != nil {
		return err
	}

	if *jsonOut {
		if pending == nil {
			pending = []audit.PendingDeletion{}
		}
		return ecrireJSON(stdout, pending)
	}
	if len(pending) == 0 {
		fmt.Fprintln(stdout, "(aucune suppression planifiée)")
		return nil
	}
	for _, p := range pending {
		fmt.Fprintf(stdout, "%s  %-25s %-20s %-12s delai=%dj\n",
			p.MovedAt, p.UserDN, p.SAMAccountName, p.SessionID, p.DelaiJours)
	}
	return nil
}

// cmdDepartPendingPurge purge les suppressions planifiées expirées.
func cmdDepartPendingPurge(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("departs pending purge", flag.ContinueOnError)
	chemin := fs.String("f", "", "chemin d'un journal.db alternatif")
	delai := fs.Int("delai-jours", 30, "délai au-delà duquel les suppressions sont purgées")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	l, err := audit.Open(*chemin)
	if err != nil {
		return err
	}
	defer l.Close() //nolint:errcheck

	n, err := l.PurgeExpiredDeletions(*delai)
	if err != nil {
		return err
	}

	if *jsonOut {
		type resultatPurge struct {
			Purgées    int64 `json:"purgees"`
			DelaiJours int   `json:"delai_jours"`
		}
		return ecrireJSON(stdout, resultatPurge{Purgées: n, DelaiJours: *delai})
	}
	fmt.Fprintf(stdout, "%d suppression(s) planifiée(s) purgée(s) (délai > %d jours)\n", n, *delai)
	return nil
}
