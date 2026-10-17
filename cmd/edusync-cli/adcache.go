package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

// cmdADCache gère le cache AD local SQLite (T2).
func cmdADCache(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « stats », « ous », « groups », « users », « search » ou « sync »")}
	}
	switch args[0] {
	case "stats":
		return cmdADCacheStats(args[1:], stdout)
	case "ous":
		return cmdADCacheOUs(args[1:], stdout)
	case "groups":
		return cmdADCacheGroups(args[1:], stdout)
	case "users":
		return cmdADCacheUsers(args[1:], stdout)
	case "search":
		return cmdADCacheSearch(args[1:], stdout)
	case "sync":
		return cmdADCacheSync(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// ouvrirCacheAD ouvre le cache (chemin vide = chemin par défaut).
func ouvrirCacheAD(chemin string) (*ad.ADCache, error) {
	if chemin == "" {
		defaut, err := ad.CheminCacheParDefaut()
		if err != nil {
			return nil, err
		}
		chemin = defaut
	}
	return ad.NewADCache(chemin, 0)
}

func cmdADCacheStats(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ad-cache stats", flag.ContinueOnError)
	chemin := fs.String("cache", "", "chemin du fichier de cache (défaut : profil utilisateur)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cache, err := ouvrirCacheAD(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = cache.Close() }()

	stats := cache.Stats()
	if *jsonOut {
		return ecrireJSON(stdout, stats)
	}
	fmt.Fprintf(stdout, "Chemin             : %s\n", cache.Chemin())
	fmt.Fprintf(stdout, "OUs                : %d\n", stats.OUsCount)
	fmt.Fprintf(stdout, "Groupes            : %d\n", stats.GroupsCount)
	fmt.Fprintf(stdout, "Utilisateurs       : %d\n", stats.UsersCount)
	fmt.Fprintf(stdout, "Dernière synchro   : %s\n", formaterHorodatage(stats.LastSync))
	fmt.Fprintf(stdout, "Dernier uSNChanged : %d\n", stats.LastUSNChanged)
	fmt.Fprintf(stdout, "Périmé             : %v\n", cache.IsStale())
	return nil
}

func cmdADCacheOUs(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ad-cache ous", flag.ContinueOnError)
	chemin := fs.String("cache", "", "chemin du fichier de cache")
	parent := fs.String("parent", "", "DN de l'OU parente (vide = toutes)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cache, err := ouvrirCacheAD(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = cache.Close() }()

	ous, err := cache.GetOUs(*parent)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, ous)
	}
	for _, o := range ous {
		fmt.Fprintf(stdout, "%s\n", o.DN)
	}
	return nil
}

func cmdADCacheGroups(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ad-cache groups", flag.ContinueOnError)
	chemin := fs.String("cache", "", "chemin du fichier de cache")
	ou := fs.String("ou", "", "DN de l'OU (vide = tous)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cache, err := ouvrirCacheAD(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = cache.Close() }()

	groupes, err := cache.GetGroups(*ou)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, groupes)
	}
	for _, g := range groupes {
		fmt.Fprintf(stdout, "%s (%d membre(s))\n", g.DN, g.MemberCount)
	}
	return nil
}

func cmdADCacheUsers(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ad-cache users", flag.ContinueOnError)
	chemin := fs.String("cache", "", "chemin du fichier de cache")
	ou := fs.String("ou", "", "DN de l'OU (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *ou == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --ou est obligatoire")}
	}

	cache, err := ouvrirCacheAD(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = cache.Close() }()

	users, err := cache.GetUsersInOU(*ou)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, users)
	}
	for _, u := range users {
		etat := "actif"
		if u.Desactive {
			etat = "désactivé"
		}
		fmt.Fprintf(stdout, "%s (%s, %s)\n", u.SAM, u.CN, etat)
	}
	return nil
}

func cmdADCacheSearch(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ad-cache search", flag.ContinueOnError)
	chemin := fs.String("cache", "", "chemin du fichier de cache")
	query := fs.String("query", "", "texte recherché (SAM ou CN)")
	limit := fs.Int("limit", 100, "nombre maximum de résultats")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *query == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --query est obligatoire")}
	}

	cache, err := ouvrirCacheAD(*chemin)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = cache.Close() }()

	users, err := cache.SearchUsers(*query, *limit)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, users)
	}
	for _, u := range users {
		fmt.Fprintf(stdout, "%s (%s)\n", u.SAM, u.CN)
	}
	return nil
}

func cmdADCacheSync(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("ad-cache sync", flag.ContinueOnError)
	chemin := fs.String("cache", "", "chemin du fichier de cache")
	full := fs.Bool("full", false, "synchronisation complète")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	mode := "incremental"
	if *full {
		mode = "complet"
	}
	type resultat struct {
		Chemin   string
		Mode     string
		Statut   string
		Detaille string
	}
	result := resultat{
		Chemin:   *chemin,
		Mode:     mode,
		Statut:   "attente_ad",
		Detaille: "connexion AD requise pour synchroniser le cache",
	}
	if *jsonOut {
		return ecrireJSON(stdout, result)
	}
	fmt.Fprintf(stdout, "Statut : %s\n", result.Statut)
	fmt.Fprintf(stdout, "Détail : %s\n", result.Detaille)
	return nil
}

// formaterHorodatage formate un horodatage Unix (secondes) pour l'affichage.
func formaterHorodatage(secondes float64) string {
	if secondes <= 0 {
		return "jamais"
	}
	return time.Unix(int64(secondes), 0).Format("2006-01-02 15:04:05")
}
