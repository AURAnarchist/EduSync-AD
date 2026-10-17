package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/vault"
)

// cmdVault gère le coffre-fort local des mots de passe.
func cmdVault(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « set », « get », « has », « delete », « list », « count » ou « clear »")}
	}
	switch args[0] {
	case "set":
		return cmdVaultSet(args[1:], stdout)
	case "get":
		return cmdVaultGet(args[1:], stdout)
	case "has":
		return cmdVaultHas(args[1:], stdout)
	case "delete":
		return cmdVaultDelete(args[1:], stdout)
	case "list":
		return cmdVaultList(args[1:], stdout)
	case "count":
		return cmdVaultCount(args[1:], stdout)
	case "clear":
		return cmdVaultClear(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// ouvrirCoffre ouvre le coffre partagé (ou un chemin explicite).
func ouvrirCoffre(chemin, cle string) (*vault.Vault, error) {
	v, err := vault.New(chemin, cle)
	if err != nil {
		return nil, &codeSortie{code: 1, err: err}
	}
	return v, nil
}

// cheminVault retourne le chemin du coffre : option explicite sinon défaut.
func cheminVault(explicite string) (string, error) {
	if explicite != "" {
		return explicite, nil
	}
	return vault.CheminParDefaut()
}

func cmdVaultSet(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vault set", flag.ContinueOnError)
	sam := fs.String("sam", "", "identifiant du compte (obligatoire)")
	motDePasse := fs.String("password", "", "mot de passe (préférer --password-stdin)")
	stdin := fs.Bool("password-stdin", false, "lire le mot de passe sur l'entrée standard")
	chemin := fs.String("vault", "", "chemin du coffre (défaut : partagé)")
	cle := fs.String("key", "", "chemin de la clé (défaut : secret.key partagé)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*sam) == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --sam est obligatoire")}
	}
	valeur := *motDePasse
	if *stdin {
		donnees, err := io.ReadAll(os.Stdin)
		if err != nil {
			return &codeSortie{code: 1, err: fmt.Errorf("lecture de l'entrée standard: %w", err)}
		}
		valeur = strings.TrimRight(string(donnees), "\r\n")
	}
	if valeur == "" {
		return &codeSortie{code: 2, err: errors.New("mot de passe vide")}
	}
	cheminCoffre, err := cheminVault(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	v, err := ouvrirCoffre(cheminCoffre, *cle)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := v.Store(*sam, valeur); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	fmt.Fprintf(stdout, "Mot de passe enregistré pour %s.\n", strings.ToLower(*sam))
	return nil
}

func cmdVaultGet(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vault get", flag.ContinueOnError)
	sam := fs.String("sam", "", "identifiant du compte (obligatoire)")
	chemin := fs.String("vault", "", "chemin du coffre")
	cle := fs.String("key", "", "chemin de la clé")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*sam) == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --sam est obligatoire")}
	}
	cheminCoffre, err := cheminVault(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	v, err := ouvrirCoffre(cheminCoffre, *cle)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	motDePasse, ok := v.Get(*sam)
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"sam": strings.ToLower(*sam), "trouve": ok, "mot_de_passe": motDePasse})
	}
	if !ok {
		return &codeSortie{code: 1, err: fmt.Errorf("aucun mot de passe enregistré pour %s", *sam)}
	}
	fmt.Fprintln(stdout, motDePasse)
	return nil
}

func cmdVaultHas(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vault has", flag.ContinueOnError)
	sam := fs.String("sam", "", "identifiant du compte (obligatoire)")
	chemin := fs.String("vault", "", "chemin du coffre")
	cle := fs.String("key", "", "chemin de la clé")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*sam) == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --sam est obligatoire")}
	}
	cheminCoffre, err := cheminVault(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	v, err := ouvrirCoffre(cheminCoffre, *cle)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	present := v.Has(*sam)
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"sam": strings.ToLower(*sam), "present": present})
	}
	fmt.Fprintf(stdout, "%v\n", present)
	if !present {
		return &codeSortie{code: 1, err: errors.New("absent du coffre")}
	}
	return nil
}

func cmdVaultDelete(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vault delete", flag.ContinueOnError)
	sam := fs.String("sam", "", "identifiant du compte (obligatoire)")
	chemin := fs.String("vault", "", "chemin du coffre")
	cle := fs.String("key", "", "chemin de la clé")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*sam) == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --sam est obligatoire")}
	}
	cheminCoffre, err := cheminVault(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	v, err := ouvrirCoffre(cheminCoffre, *cle)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	if err := v.Delete(*sam); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	fmt.Fprintf(stdout, "Entrée supprimée pour %s.\n", strings.ToLower(*sam))
	return nil
}

func cmdVaultList(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vault list", flag.ContinueOnError)
	chemin := fs.String("vault", "", "chemin du coffre")
	cle := fs.String("key", "", "chemin de la clé")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	cheminCoffre, err := cheminVault(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	v, err := ouvrirCoffre(cheminCoffre, *cle)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	sams, err := v.Liste()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, sams)
	}
	for _, sam := range sams {
		fmt.Fprintln(stdout, sam)
	}
	return nil
}

func cmdVaultCount(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vault count", flag.ContinueOnError)
	chemin := fs.String("vault", "", "chemin du coffre")
	cle := fs.String("key", "", "chemin de la clé")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	cheminCoffre, err := cheminVault(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	v, err := ouvrirCoffre(cheminCoffre, *cle)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	nombre, err := v.Count()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"nombre": nombre})
	}
	fmt.Fprintf(stdout, "%d\n", nombre)
	return nil
}

func cmdVaultClear(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("vault clear", flag.ContinueOnError)
	chemin := fs.String("vault", "", "chemin du coffre")
	cle := fs.String("key", "", "chemin de la clé")
	confirme := fs.Bool("yes", false, "confirmer la purge complète")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if !*confirme {
		return &codeSortie{code: 2, err: errors.New("purge refusée : ajoutez --yes pour confirmer")}
	}
	cheminCoffre, err := cheminVault(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	v, err := ouvrirCoffre(cheminCoffre, *cle)
	if err != nil {
		return err
	}
	defer func() { _ = v.Close() }()
	supprimes, err := v.ClearAll()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"supprimes": supprimes})
	}
	fmt.Fprintf(stdout, "Coffre purgé : %d entrée(s) supprimée(s).\n", supprimes)
	return nil
}
