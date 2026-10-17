package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/estemobs/EduSync-AD/internal/api"
	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/portal"
	"github.com/estemobs/EduSync-AD/internal/webhooks"
)

// cmdAPI gère l'API REST et les webhooks (M28) : réglages, clés d'accès,
// document OpenAPI, abonnements et journal des livraisons.
//
// Toutes les sous-commandes sont hors ligne, sauf « serve » (mise en écoute)
// et « webhooks test » (envoi réel vers l'URL déclarée).
func cmdAPI(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New(
			"sous-commande manquante : « config », « keys », « routes », « spec », « webhooks » ou « serve »")}
	}
	switch args[0] {
	case "config":
		return cmdAPIConfig(args[1:], stdout)
	case "keys":
		return cmdAPICles(args[1:], stdout)
	case "routes":
		return cmdAPIRoutes(args[1:], stdout)
	case "spec":
		return cmdAPISpec(args[1:], stdout)
	case "webhooks":
		return cmdAPIWebhooks(args[1:], stdout)
	case "serve":
		return cmdAPIServe(args[1:], stdout, stderr)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// ─── Réglages ──────────────────────────────────────────────────────────────

// cmdAPIConfig affiche et/ou enregistre les réglages de l'API.
func cmdAPIConfig(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("api config", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de api.json (défaut : partagé)")
	save := fs.Bool("save", false, "enregistrer les modifications")
	reset := fs.Bool("reset", false, "repartir des réglages par défaut")
	actif := fs.Bool("actif", false, "démarrage automatique du serveur")
	hote := fs.String("host", "", "interface d'écoute (ex. 0.0.0.0)")
	port := fs.Int("port", 0, "port d'écoute")
	cors := fs.String("cors", "", "origines autorisées, séparées par des virgules")
	limite := fs.Int("rate-limit", -1, "requêtes par minute et par clé (0 = illimité)")
	titre := fs.String("titre", "", "titre affiché")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := api.ChargerConfig(*fichier)
	if *reset {
		cfg = api.ConfigDefaut()
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
	if visites["cors"] {
		origines := []string{}
		for _, origine := range strings.Split(*cors, ",") {
			if origine = strings.TrimSpace(origine); origine != "" {
				origines = append(origines, origine)
			}
		}
		cfg.CORSOrigines = origines
	}
	if visites["rate-limit"] {
		cfg.LimiteRPM = *limite
	}
	if visites["titre"] {
		cfg.Titre = strings.TrimSpace(*titre)
	}

	chemin, err := api.CheminConfigDefaut()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *fichier != "" {
		chemin = *fichier
	}
	if *save {
		ecrit, err := api.SauvegarderConfig(cfg, *fichier)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		chemin = ecrit
	}

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"config": cfg,
			"file":   chemin,
			"url":    cfg.BaseURL(),
			"errors": cfg.Valider(),
		})
	}
	fmt.Fprintf(stdout, "Fichier    : %s\n", chemin)
	fmt.Fprintf(stdout, "Démarrage  : %s\n", actifTexte(cfg.Actif))
	fmt.Fprintf(stdout, "Écoute     : %s (%s)\n", cfg.BaseURL(), cfg.Hote)
	fmt.Fprintf(stdout, "CORS       : %s\n", strings.Join(cfg.CORSOrigines, ", "))
	fmt.Fprintf(stdout, "Limite     : %d requêtes/minute par clé\n", cfg.LimiteRPM)
	fmt.Fprintf(stdout, "Titre      : %s\n", cfg.Titre)
	if erreurs := cfg.Valider(); len(erreurs) > 0 {
		fmt.Fprintf(stdout, "Erreurs    : %s\n", strings.Join(erreurs, " ; "))
	}
	return nil
}

// ─── Clés d'accès ──────────────────────────────────────────────────────────

// cmdAPICles gère les clés d'API (liste, création, révocation, suppression).
func cmdAPICles(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New(
			"sous-commande manquante : « list », « create », « revoke », « remove » ou « permissions »")}
	}
	action := args[0]

	fs := flag.NewFlagSet("api keys "+action, flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de api_keys.json (défaut : partagé)")
	nom := fs.String("name", "", "nom lisible de la clé (create)")
	permissions := fs.String("permissions", "*", "permissions, séparées par des virgules (create)")
	jours := fs.Int("days", 0, "durée de validité en jours (0 = jamais)")
	identifiant := fs.String("id", "", "identifiant de la clé (revoke, remove)")
	toutes := fs.Bool("all", false, "inclure les clés inactives (list)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	magasin, err := api.OuvrirMagasinCles(*fichier)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	moment := time.Now().UTC()

	switch action {
	case "list":
		cles := magasin.Lister(*toutes, moment)
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{
				"keys":  cles,
				"total": len(cles),
				"file":  magasin.Chemin(),
			})
		}
		fmt.Fprintf(stdout, "Clés d'API (%s)\n", magasin.Chemin())
		for _, cle := range cles {
			fmt.Fprintf(stdout, "  %-18s %-24s %-12s %s %s\n", cle.ID, cle.Nom,
				cle.Prefixe, statutCle(cle, moment), strings.Join(cle.Permissions, ","))
		}
		if len(cles) == 0 {
			fmt.Fprintln(stdout, "  (aucune)")
		}
		return nil

	case "create":
		complet, info, err := magasin.Creer(*nom, separerListe(*permissions), *jours, moment)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{
				"key":  complet,
				"info": info,
				"file": magasin.Chemin(),
			})
		}
		fmt.Fprintf(stdout, "Clé créée : %s\n", info.ID)
		fmt.Fprintf(stdout, "Nom       : %s\n", info.Nom)
		fmt.Fprintf(stdout, "Permissions : %s\n", strings.Join(info.Permissions, ", "))
		if info.ExpireLe != nil {
			fmt.Fprintf(stdout, "Expire le : %s\n", *info.ExpireLe)
		}
		fmt.Fprintf(stdout, "Clé       : %s\n", complet)
		fmt.Fprintln(stdout, "Cette clé n'est affichée qu'une fois : conservez-la en lieu sûr.")
		return nil

	case "revoke":
		if *identifiant == "" {
			return &codeSortie{code: 2, err: errors.New("--id obligatoire")}
		}
		revoque, err := magasin.Revoquer(*identifiant)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		return sortieBooleen(stdout, *jsonOut, "revoked", revoque, *identifiant)

	case "remove":
		if *identifiant == "" {
			return &codeSortie{code: 2, err: errors.New("--id obligatoire")}
		}
		supprime, err := magasin.Supprimer(*identifiant)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		return sortieBooleen(stdout, *jsonOut, "deleted", supprime, *identifiant)

	case "permissions":
		permissions := make([]map[string]string, 0, len(api.PermissionsStandards))
		for _, permission := range api.PermissionsStandards {
			permissions = append(permissions, map[string]string{
				"code": permission.Code, "label": permission.Libelle,
			})
		}
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{"permissions": permissions})
		}
		for _, permission := range api.PermissionsStandards {
			fmt.Fprintf(stdout, "  %-18s %s\n", permission.Code, permission.Libelle)
		}
		return nil

	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", action)}
	}
}

// statutCle rend l'état d'une clé (« active », « expirée », « révoquée »).
func statutCle(cle api.CleAPIInfo, moment time.Time) string {
	switch {
	case !cle.Active:
		return "révoquée"
	case cle.Expiree(moment):
		return "expirée"
	default:
		return "active"
	}
}

// sortieBooleen affiche le résultat d'une opération booléenne.
//
// Une opération sans effet (identifiant inconnu) reste une erreur : le code de
// sortie doit permettre à un script de la détecter, même en sortie JSON.
func sortieBooleen(stdout io.Writer, jsonOut bool, champ string, valeur bool,
	identifiant string) error {
	if jsonOut {
		if err := ecrireJSON(stdout, map[string]any{
			"id": identifiant, champ: valeur,
		}); err != nil {
			return err
		}
	} else if valeur {
		fmt.Fprintf(stdout, "%s : %s\n", identifiant, champ)
	}
	if valeur {
		return nil
	}
	return &codeSortie{code: 1, err: fmt.Errorf("identifiant inconnu : %s", identifiant)}
}

// separerListe découpe une liste « a,b,c » en nettoyant les espaces.
func separerListe(liste string) []string {
	resultat := []string{}
	for _, morceau := range strings.Split(liste, ",") {
		if morceau = strings.TrimSpace(morceau); morceau != "" {
			resultat = append(resultat, morceau)
		}
	}
	return resultat
}

// ─── Routes et document ────────────────────────────────────────────────────

// routeurAPI construit le routeur complet (utilisé par routes et spec).
func routeurAPI() *api.Routeur {
	routeur := api.NouveauRouteur()
	api.EnregistrerRoutes(routeur, api.Dependances{})
	return routeur
}

// cmdAPIRoutes liste les routes de l'API.
func cmdAPIRoutes(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("api routes", flag.ContinueOnError)
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	routes := routeurAPI().Routes()
	type ligne struct {
		Methode  string `json:"method"`
		Motif    string `json:"path"`
		Publique bool   `json:"public"`
	}
	liste := make([]ligne, 0, len(routes))
	for _, route := range routes {
		liste = append(liste, ligne{Methode: route.Methode, Motif: route.Motif,
			Publique: route.Publique})
	}
	sort.SliceStable(liste, func(i, j int) bool { return liste[i].Motif < liste[j].Motif })

	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"version": api.VersionAPI,
			"prefix":  api.PrefixeAPI,
			"routes":  liste,
			"total":   len(liste),
		})
	}
	fmt.Fprintf(stdout, "Routes %s (%d, préfixe %s)\n", api.VersionAPI, len(liste), api.PrefixeAPI)
	for _, item := range liste {
		marque := ""
		if item.Publique {
			marque = " [publique]"
		}
		fmt.Fprintf(stdout, "  %-6s %s%s\n", item.Methode, item.Motif, marque)
	}
	return nil
}

// cmdAPISpec écrit le document OpenAPI.
func cmdAPISpec(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("api spec", flag.ContinueOnError)
	sortie := fs.String("out", "", "fichier de sortie (défaut : api-spec.json)")
	versStdout := fs.Bool("stdout", false, "écrire le document sur la sortie standard")
	jsonOut := fs.Bool("json", false, "sortie JSON (résumé)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	routeur := routeurAPI()
	if *versStdout {
		donnees, err := json.MarshalIndent(api.DocumentOpenAPI(version, routeur), "", "  ")
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		_, err = fmt.Fprintln(stdout, string(donnees))
		return err
	}

	chemin, err := api.EcrireDocumentOpenAPI(*sortie, version, routeur)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"file":    chemin,
			"version": version,
			"title":   api.TitreDocument,
		})
	}
	fmt.Fprintf(stdout, "Document OpenAPI écrit : %s\n", chemin)
	return nil
}

// ─── Webhooks ──────────────────────────────────────────────────────────────

// cmdAPIWebhooks gère les abonnements et le journal des livraisons.
func cmdAPIWebhooks(args []string, stdout io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New(
			"sous-commande manquante : « list », « add », « remove », « deliveries », « test » ou « events »")}
	}
	action := args[0]

	fs := flag.NewFlagSet("api webhooks "+action, flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de webhooks.json (défaut : partagé)")
	base := fs.String("db", "", "chemin de webhook_deliveries.db (défaut : partagé)")
	identifiant := fs.String("id", "", "identifiant du webhook")
	nom := fs.String("name", "", "nom lisible (add)")
	adresse := fs.String("url", "", "URL cible (add)")
	evenements := fs.String("events", "", "événements abonnés, séparés par des virgules (add)")
	secret := fs.String("secret", "", "secret HMAC (add) — stocké chiffré")
	desactive := fs.Bool("disable", false, "créer le webhook désactivé (add)")
	evenement := fs.String("event", "system.startup", "événement de test (test)")
	limite := fs.Int("limit", 20, "nombre de livraisons affichées (deliveries)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args[1:]); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	if action == "events" {
		evenements := webhooks.EvenementsTries()
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{"events": evenements, "total": len(evenements)})
		}
		for _, item := range evenements {
			fmt.Fprintln(stdout, "  "+item)
		}
		return nil
	}

	store, err := webhooks.OuvrirStore(*fichier, *base)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer func() { _ = store.Fermer() }()
	dispatcher := webhooks.NouveauDispatcher(store)

	switch action {
	case "list":
		liste := store.Lister(false)
		donnees := make([]map[string]any, 0, len(liste))
		for _, webhook := range liste {
			donnees = append(donnees, webhook.Dict())
		}
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{
				"webhooks": donnees, "total": len(donnees), "file": store.CheminConfig(),
			})
		}
		fmt.Fprintf(stdout, "Webhooks (%s)\n", store.CheminConfig())
		for _, webhook := range liste {
			fmt.Fprintf(stdout, "  %-14s %-24s %-40s %s\n", webhook.ID, webhook.Nom,
				webhook.URL, strings.Join(webhook.Evenements, ","))
		}
		if len(liste) == 0 {
			fmt.Fprintln(stdout, "  (aucun)")
		}
		return nil

	case "add":
		if strings.TrimSpace(*nom) == "" || strings.TrimSpace(*adresse) == "" {
			return &codeSortie{code: 2, err: errors.New("--name et --url obligatoires")}
		}
		abonnements := webhooks.SéparerEvenements(*evenements)
		webhook := webhooks.NouveauWebhook(webhooks.NouvelIdentifiantWebhook(), *nom, *adresse, abonnements)
		webhook.Actif = !*desactive
		if *secret != "" {
			chiffre, err := dispatcher.ChiffrerSecret(*secret)
			if err != nil {
				return &codeSortie{code: 1, err: err}
			}
			webhook.Secret = chiffre
		}
		cree, err := store.Creer(webhook)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{"webhook": cree.Dict()})
		}
		fmt.Fprintf(stdout, "Webhook créé : %s (%s)\n", cree.ID, cree.Nom)
		return nil

	case "remove":
		if *identifiant == "" {
			return &codeSortie{code: 2, err: errors.New("--id obligatoire")}
		}
		supprime, err := store.Supprimer(*identifiant)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		return sortieBooleen(stdout, *jsonOut, "deleted", supprime, *identifiant)

	case "deliveries":
		livraisons, err := store.Livraisons(*identifiant, *limite)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{
				"deliveries": livraisons, "total": len(livraisons),
				"file": store.CheminLivraisons(),
			})
		}
		fmt.Fprintf(stdout, "Livraisons (%s)\n", store.CheminLivraisons())
		for _, livraison := range livraisons {
			statut := "—"
			if livraison.Statut != nil {
				statut = fmt.Sprintf("%d", *livraison.Statut)
			}
			erreur := ""
			if livraison.Erreur != nil {
				erreur = " " + *livraison.Erreur
			}
			fmt.Fprintf(stdout, "  %s %-14s %-24s %-16s tentative %d%s\n",
				livraison.CommenceLe, livraison.WebhookID, livraison.Evenement,
				statut, livraison.Tentative, erreur)
		}
		if len(livraisons) == 0 {
			fmt.Fprintln(stdout, "  (aucune)")
		}
		return nil

	case "test":
		if *identifiant == "" {
			return &codeSortie{code: 2, err: errors.New("--id obligatoire")}
		}
		webhook, err := store.Obtenir(*identifiant)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		if webhook == nil {
			return &codeSortie{code: 1, err: fmt.Errorf("webhook inconnu : %s", *identifiant)}
		}
		livraison, err := dispatcher.Livrer(*webhook, *evenement,
			map[string]any{"test": true}, 0)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
		if *jsonOut {
			return ecrireJSON(stdout, map[string]any{"delivery": livraison})
		}
		fmt.Fprintf(stdout, "Livraison %s : %s\n", livraison.ID, statutLivraison(*livraison))
		return nil

	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", action)}
	}
}

// statutLivraison rend l'issue d'une livraison en texte lisible.
func statutLivraison(livraison webhooks.Livraison) string {
	switch {
	case livraison.Statut == nil:
		return "sans réponse"
	case *livraison.Statut >= 200 && *livraison.Statut < 300:
		return fmt.Sprintf("succès (HTTP %d)", *livraison.Statut)
	default:
		return fmt.Sprintf("échec (HTTP %d)", *livraison.Statut)
	}
}

// ─── Mise en écoute ────────────────────────────────────────────────────────

// cmdAPIServe démarre le serveur de l'API et attend l'interruption.
func cmdAPIServe(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("api serve", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de api.json (défaut : partagé)")
	cles := fs.String("keys", "", "chemin de api_keys.json (défaut : partagé)")
	port := fs.Int("port", -1, "port d'écoute (remplace la configuration)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}

	cfg := api.ChargerConfig(*fichier)
	if *port >= 0 {
		cfg.Port = *port
	}
	magasin, err := api.OuvrirMagasinCles(*cles)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if len(magasin.Lister(true, time.Now().UTC())) == 0 {
		return &codeSortie{code: 1, err: errors.New(
			"aucune clé d'API : créez-en une avec « edusync-cli api keys create --name … »")}
	}

	deps, fermer, err := dependancesAPI()
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	defer fermer()

	routeur := api.NouveauRouteur()
	api.EnregistrerRoutes(routeur, deps)
	serveur := api.NouveauServeur(cfg, routeur, magasin)
	if err := serveur.Demarrer(); err != nil {
		return &codeSortie{code: 1, err: err}
	}

	if *jsonOut {
		if err := ecrireJSON(stdout, map[string]any{
			"url": serveur.URL(), "prefix": api.PrefixeAPI, "listening": true,
		}); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(stdout, "API en écoute sur %s%s — interruption avec Ctrl-C\n",
			serveur.URL(), api.PrefixeAPI)
	}
	fmt.Fprintf(stderr, "Documentation OpenAPI : %s/openapi.json\n", serveur.URL())

	signaux := make(chan os.Signal, 1)
	signal.Notify(signaux, os.Interrupt, syscall.SIGTERM)
	<-signaux
	fmt.Fprintln(stdout, "Arrêt du serveur…")
	return serveur.Arreter()
}

// dependancesAPI assemble les dépendances du serveur : journal d'actions,
// webhooks et portail.
//
// L'annuaire n'est volontairement pas ouvert ici : la connexion AD est une
// action interactive (M9/M25). Les routes AD répondent donc « annuaire
// indisponible » tant que le serveur n'est pas embarqué dans l'application
// desktop qui, elle, dispose d'une session ; les routes de portail, webhooks,
// journal et métadonnées restent pleinement utilisables.
func dependancesAPI() (api.Dependances, func(), error) {
	deps := api.Dependances{Version: version}
	echec := func() {}

	journal, err := audit.Open("")
	if err != nil {
		return deps, echec, err
	}
	storeWebhooks, err := webhooks.OuvrirStore("", "")
	if err != nil {
		_ = journal.Close()
		return deps, echec, err
	}
	dispatcher := webhooks.NouveauDispatcher(storeWebhooks)
	dispatcher.Demarrer()

	appConfig, err := config.Load("")
	if err != nil {
		dispatcher.Arreter()
		_ = storeWebhooks.Fermer()
		_ = journal.Close()
		return deps, echec, err
	}

	deps.Journal = journal
	deps.Webhooks = storeWebhooks
	deps.Dispatcher = dispatcher
	deps.Config = appConfig

	// Le portail n'est branché que si sa base existe déjà : le serveur ne crée
	// aucun fichier au démarrage.
	var storePortail *portal.Store
	if chemin, err := portal.CheminBaseDefaut(); err == nil {
		if _, statErr := os.Stat(chemin); statErr == nil {
			storePortail, err = portal.OuvrirStore(chemin)
			if err != nil {
				dispatcher.Arreter()
				_ = storeWebhooks.Fermer()
				_ = journal.Close()
				return deps, echec, err
			}
			cheminJournal, _ := audit.DefaultPath()
			deps.Portail = portal.NouveauService(portal.ChargerConfig(""), appConfig,
				nil, storePortail, "", cheminJournal)
		}
	}

	fermer := func() {
		dispatcher.Arreter()
		_ = storeWebhooks.Fermer()
		if storePortail != nil {
			_ = storePortail.Fermer()
		}
		_ = journal.Close()
	}
	return deps, fermer, nil
}
