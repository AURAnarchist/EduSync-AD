package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/ad"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/mailer"
	"github.com/estemobs/EduSync-AD/internal/multisite"
	"github.com/estemobs/EduSync-AD/internal/portal"
)

// cmdPortal gère le portail auto-service (M27) : réglages, file des demandes
// de création de compte et validation.
//
// Les commandes de consultation (config, requests, status, refuse) sont
// entièrement hors ligne : elles n'ouvrent que les fichiers du portail. Seule
// « approve » se connecte à l'annuaire, via un site enregistré (M25).
func cmdPortal(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New(
			"sous-commande manquante : « config », « requests », « status », « refuse » ou « approve »")}
	}
	switch args[0] {
	case "config":
		return cmdPortalConfig(args[1:], stdout)
	case "requests":
		return cmdPortalRequests(args[1:], stdout)
	case "status":
		return cmdPortalStatus(args[1:], stdout)
	case "refuse":
		return cmdPortalRefuse(args[1:], stdout)
	case "approve":
		return cmdPortalApprove(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// optionsPortail déclare les deux chemins du portail (réglages et base).
func optionsPortail(fs *flag.FlagSet) (*string, *string) {
	fichier := fs.String("file", "", "chemin de portail.json (défaut : partagé)")
	base := fs.String("db", "", "chemin de portail.db (défaut : partagé)")
	return fichier, base
}

// cheminConfigPortail retourne le chemin effectif du fichier de réglages.
func cheminConfigPortail(chemin string) string {
	if chemin != "" {
		return chemin
	}
	defaut, err := portal.CheminConfigDefaut()
	if err != nil {
		return ""
	}
	return defaut
}

// ouvrirStorePortail ouvre la base du portail (fermée par l'appelant).
func ouvrirStorePortail(chemin string) (*portal.Store, error) {
	store, err := portal.OuvrirStore(chemin)
	if err != nil {
		return nil, &codeSortie{code: 1, err: err}
	}
	return store, nil
}

// nouveauServicePortail assemble un service du portail (annuaire optionnel :
// les commandes hors ligne passent nil).
func nouveauServicePortail(cfg portal.Config, annuaire portal.Annuaire,
	store *portal.Store, domaine string) (*portal.Service, error) {

	appConfig, err := config.Load("")
	if err != nil {
		return nil, &codeSortie{code: 1, err: err}
	}
	journal, err := audit.DefaultPath()
	if err != nil {
		return nil, &codeSortie{code: 1, err: err}
	}
	service := portal.NouveauService(cfg, appConfig, annuaire, store, domaine, journal)
	// Le portail réutilise la configuration SMTP commune (comme core/mailer.py).
	service.SMTP = mailer.ChargerConfig("", "")
	return service, nil
}

// cmdPortalConfig affiche et/ou enregistre les réglages du portail.
func cmdPortalConfig(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("portal config", flag.ContinueOnError)
	fichier, _ := optionsPortail(fs)
	save := fs.Bool("save", false, "enregistrer les modifications")
	reset := fs.Bool("reset", false, "repartir des réglages par défaut")
	actif := fs.Bool("actif", false, "démarrage automatique du serveur")
	hote := fs.String("host", "", "interface d'écoute (ex. 0.0.0.0)")
	port := fs.Int("port", 0, "port d'écoute")
	codeMinutes := fs.Int("code-minutes", 0, "durée de validité des codes (minutes)")
	tentatives := fs.Int("tentatives-max", 0, "tentatives maximales par code")
	envois := fs.Int("envois-max", 0, "envois maximaux par identifiant")
	politique := fs.String("politique", "", "politique de mot de passe (eleve | personnel)")
	ouAccueil := fs.String("ou-accueil", "", "OU d'accueil des comptes créés (DN)")
	notification := fs.Bool("notification-mail", false, "envoyer les identifiants au demandeur")
	titre := fs.String("titre", "", "titre affiché aux usagers")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := portal.ChargerConfig(*fichier)
	if *reset {
		cfg = portal.ConfigDefaut()
	}
	visites := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { visites[f.Name] = true })
	if visites["actif"] {
		cfg.Actif = *actif
	}
	if visites["host"] {
		cfg.Hote = strings.TrimSpace(*hote)
	}
	if visites["port"] {
		cfg.Port = *port
	}
	if visites["code-minutes"] {
		cfg.CodeMinutes = *codeMinutes
	}
	if visites["tentatives-max"] {
		cfg.TentativesMax = *tentatives
	}
	if visites["envois-max"] {
		cfg.EnvoisMax = *envois
	}
	if visites["politique"] {
		cfg.Politique = strings.ToLower(strings.TrimSpace(*politique))
	}
	if visites["ou-accueil"] {
		cfg.OUAccueil = strings.TrimSpace(*ouAccueil)
	}
	if visites["notification-mail"] {
		cfg.NotificationMail = *notification
	}
	if visites["titre"] {
		cfg.Titre = strings.TrimSpace(*titre)
	}

	chemin := cheminConfigPortail(*fichier)
	if *save {
		ecrit, err := portal.SauvegarderConfig(cfg, *fichier)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		chemin = ecrit
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"config": cfg,
			"file":   chemin,
			"errors": cfg.Valider(),
		})
	}
	fmt.Fprintf(stdout, "Fichier    : %s\n", chemin)
	fmt.Fprintf(stdout, "Démarrage  : %s\n", actifTexte(cfg.Actif))
	fmt.Fprintf(stdout, "Écoute     : %s (%s)\n", cfg.BaseURL(), cfg.Hote)
	fmt.Fprintf(stdout, "Codes      : %d minutes, %d tentatives max, %d envois max\n",
		cfg.CodeMinutes, cfg.TentativesMax, cfg.EnvoisMax)
	fmt.Fprintf(stdout, "Politique  : %s\n", cfg.Politique)
	fmt.Fprintf(stdout, "OU accueil : %s\n", ouTexte(cfg.OUAccueil))
	fmt.Fprintf(stdout, "Notification : %s\n", actifTexte(cfg.NotificationMail))
	fmt.Fprintf(stdout, "Titre      : %s\n", cfg.Titre)
	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		fmt.Fprintf(stdout, "Erreurs    : %s\n", strings.Join(erreurs, " ; "))
	}
	return nil
}

// actifTexte rend un booléen en texte lisible.
func actifTexte(valeur bool) string {
	if valeur {
		return "oui"
	}
	return "non"
}

// ouTexte rend l'OU d'accueil ou un tiret si elle n'est pas renseignée.
func ouTexte(valeur string) string {
	if strings.TrimSpace(valeur) == "" {
		return "—"
	}
	return valeur
}

// cmdPortalRequests liste les demandes de création de compte.
func cmdPortalRequests(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("portal requests", flag.ContinueOnError)
	_, base := optionsPortail(fs)
	statut := fs.String("statut", portal.StatutEnAttente,
		"statut filtré (en_attente, approuvee, refusee)")
	tous := fs.Bool("all", false, "lister toutes les demandes")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	store, err := ouvrirStorePortail(*base)
	if err != nil {
		return err
	}
	defer store.Fermer()

	filtre := *statut
	if *tous {
		filtre = ""
	}
	demandes, err := store.ListerDemandes(filtre)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	if *jsonOut {
		if demandes == nil {
			demandes = []portal.Demande{}
		}
		return ecrireJSON(stdout, demandes)
	}
	if len(demandes) == 0 {
		fmt.Fprintln(stdout, "Aucune demande.")
		return nil
	}
	for _, demande := range demandes {
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\t%s\n", demande.ID, demande.Statut,
			demande.Profil, demande.NomComplet(), demande.CreatedAt)
	}
	return nil
}

// cmdPortalStatus publie l'état du portail (mêmes informations que /etat).
func cmdPortalStatus(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("portal status", flag.ContinueOnError)
	fichier, base := optionsPortail(fs)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := portal.ChargerConfig(*fichier)
	cheminBase := *base
	if cheminBase == "" {
		cheminBase, _ = portal.CheminBaseDefaut()
	}

	attentes := -1
	if store, err := portal.OuvrirStore(*base); err == nil {
		if demandes, err := store.ListerDemandes(portal.StatutEnAttente); err == nil {
			attentes = len(demandes)
		}
		_ = store.Fermer()
	}

	etat := map[string]any{
		"service":             "portail",
		"titre":               cfg.Titre,
		"actif":               cfg.Actif,
		"base_url":            cfg.BaseURL(),
		"code_minutes":        cfg.CodeMinutes,
		"tentatives_max":      cfg.TentativesMax,
		"demandes_en_attente": attentes,
		"configuration":       cheminConfigPortail(*fichier),
		"base":                cheminBase,
	}
	if *jsonOut {
		return ecrireJSON(stdout, etat)
	}
	fmt.Fprintf(stdout, "Service          : portail\n")
	fmt.Fprintf(stdout, "Titre            : %s\n", cfg.Titre)
	fmt.Fprintf(stdout, "Démarrage auto   : %s\n", actifTexte(cfg.Actif))
	fmt.Fprintf(stdout, "Adresse          : %s\n", cfg.BaseURL())
	fmt.Fprintf(stdout, "Demandes attente : %d\n", attentes)
	fmt.Fprintf(stdout, "Configuration    : %s\n", cheminConfigPortail(*fichier))
	fmt.Fprintf(stdout, "Base             : %s\n", cheminBase)
	return nil
}

// cmdPortalRefuse refuse une demande sans se connecter à l'annuaire.
func cmdPortalRefuse(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("portal refuse", flag.ContinueOnError)
	fichier, base := optionsPortail(fs)
	id := fs.String("id", "", "identifiant de la demande (obligatoire)")
	note := fs.String("note", "", "motif du refus")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --id est obligatoire")}
	}

	store, err := ouvrirStorePortail(*base)
	if err != nil {
		return err
	}
	defer store.Fermer()

	service, err := nouveauServicePortail(portal.ChargerConfig(*fichier), nil, store, "")
	if err != nil {
		return err
	}
	if err := service.RefuserDemande(*id, strings.TrimSpace(*note)); err != nil {
		return &codeSortie{code: 1, err: err}
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"id": *id, "statut": portal.StatutRefusee})
	}
	fmt.Fprintf(stdout, "Demande refusée : %s\n", *id)
	return nil
}

// poolDepuisProfil construit un pool LDAP à partir d'un site enregistré.
func poolDepuisProfil(profil multisite.ProfilDomaine, ldaps bool) (*ad.Pool, error) {
	motDePasse := profil.StoredPassword()
	if motDePasse == "" {
		return nil, &codeSortie{code: 2, err: errors.New(
			"le mot de passe du site n'est pas mémorisé : enregistrez-le avec « multisite add --remember »")}
	}
	port := 389
	if ldaps {
		port = 636
	}
	pool, err := ad.NewPool(ad.Config{
		Domain:        profil.Domain,
		Controller:    profil.Controller,
		Port:          port,
		UseLDAPS:      ldaps,
		AuthMethod:    ad.AuthSimple,
		BindDN:        profil.Username,
		BindPassword:  motDePasse,
		TLSCAPath:     profil.CACertPath,
		TLSSkipVerify: !profil.VerifyCertificate,
		Timeout:       10 * time.Second,
		MaxConns:      2,
	})
	if err != nil {
		return nil, &codeSortie{code: 1, err: err}
	}
	return pool, nil
}

// cmdPortalApprove valide une demande : le compte est créé dans l'annuaire du
// site enregistré indiqué, puis le demandeur est prévenu si l'option est active.
func cmdPortalApprove(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("portal approve", flag.ContinueOnError)
	fichier, base := optionsPortail(fs)
	id := fs.String("id", "", "identifiant de la demande (obligatoire)")
	site := fs.String("site", "", "identifiant du site enregistré (défaut : le premier)")
	fichierSites := fs.String("sites-file", "", "chemin de domaines.json (défaut : partagé)")
	cleSites := fs.String("sites-key", "", "chemin de la clé des sites (défaut : partagé)")
	ldaps := fs.Bool("ldaps", true, "connexion LDAPS (port 636)")
	voirMotDePasse := fs.Bool("show-password", false, "afficher le mot de passe généré")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --id est obligatoire")}
	}

	profils, err := multisite.ChargerSites(*fichierSites, *cleSites)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	var profil *multisite.ProfilDomaine
	if *site != "" {
		profil = multisite.TrouverProfil(profils, *site)
	} else if len(profils) > 0 {
		profil = &profils[0]
	}
	if profil == nil {
		return &codeSortie{code: 2, err: errors.New(
			"aucun site enregistré : utilisez --site ou « multisite add »")}
	}

	store, err := ouvrirStorePortail(*base)
	if err != nil {
		return err
	}
	defer store.Fermer()

	pool, err := poolDepuisProfil(*profil, *ldaps)
	if err != nil {
		return err
	}
	defer pool.Close()

	annuaire := &portal.PoolAnnuaire{Pool: pool}
	service, err := nouveauServicePortail(portal.ChargerConfig(*fichier), annuaire,
		store, profil.Domain)
	if err != nil {
		return err
	}

	infos, err := service.ApprouverDemande(*id)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}

	// Le mot de passe n'est pas écrit dans les journaux sauf demande explicite.
	resultat := map[string]any{
		"id":          *id,
		"statut":      portal.StatutApprouvee,
		"dn":          infos["dn"],
		"identifiant": infos["identifiant"],
		"mail":        infos["mail"],
		"nom":         infos["nom"],
	}
	if *voirMotDePasse {
		resultat["mot_de_passe"] = infos["mot_de_passe"]
	}
	if *jsonOut {
		return ecrireJSON(stdout, resultat)
	}
	fmt.Fprintf(stdout, "Compte créé : %s\n", infos["dn"])
	fmt.Fprintf(stdout, "Identifiant : %s\n", infos["identifiant"])
	fmt.Fprintf(stdout, "Messagerie  : %s\n", infos["mail"])
	if *voirMotDePasse {
		fmt.Fprintf(stdout, "Mot de passe : %s\n", infos["mot_de_passe"])
	} else {
		fmt.Fprintln(stdout, "Mot de passe : (masqué — utilisez --show-password)")
	}
	return nil
}
