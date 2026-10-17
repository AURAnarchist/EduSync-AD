package main

import (
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/label"
	"github.com/estemobs/EduSync-AD/internal/mailer"
)

// cmdLabel gère les étiquettes avancées, les trombinoscopes et l'envoi SMTP (M24).
func cmdLabel(args []string, stdout, _ io.Writer) error {
	if len(args) == 0 {
		return &codeSortie{code: 2, err: errors.New("sous-commande manquante : « templates », « show », « render », « trombinoscope », « export », « import », « remove », « mail-config », « mail-set » ou « send »")}
	}
	switch args[0] {
	case "templates":
		return cmdLabelTemplates(args[1:], stdout)
	case "show":
		return cmdLabelShow(args[1:], stdout)
	case "render":
		return cmdLabelRender(args[1:], stdout)
	case "trombinoscope":
		return cmdLabelTrombinoscope(args[1:], stdout)
	case "export":
		return cmdLabelExport(args[1:], stdout)
	case "import":
		return cmdLabelImport(args[1:], stdout)
	case "remove":
		return cmdLabelRemove(args[1:], stdout)
	case "mail-config":
		return cmdLabelMailConfig(args[1:], stdout)
	case "mail-set":
		return cmdLabelMailSet(args[1:], stdout)
	case "send":
		return cmdLabelSend(args[1:], stdout)
	default:
		return &codeSortie{code: 2, err: fmt.Errorf("sous-commande inconnue %q", args[0])}
	}
}

// trouverModele retourne un modèle par identifiant.
func trouverModele(modeles []label.ModeleLabel, id string) (label.ModeleLabel, bool) {
	for _, m := range modeles {
		if m.ID == id {
			return m, true
		}
	}
	return label.ModeleLabel{}, false
}

// lireLignesCSV lit un export CSV (séparateur « ; », en-tête = clés ou libellés
// des champs d'export) et retourne des lignes prêtes pour le rendu.
func lireLignesCSV(chemin string) ([]map[string]any, error) {
	fichier, err := os.Open(chemin)
	if err != nil {
		return nil, fmt.Errorf("label: lecture du CSV: %w", err)
	}
	defer fichier.Close() //nolint:errcheck // lecture seule

	lecteur := csv.NewReader(fichier)
	lecteur.Comma = ';'
	lecteur.FieldsPerRecord = -1

	entetes, err := lecteur.Read()
	if err != nil {
		return nil, fmt.Errorf("label: en-tête CSV illisible: %w", err)
	}
	cles := make([]string, len(entetes))
	for i, entete := range entetes {
		entete = strings.TrimSpace(strings.TrimPrefix(entete, "\ufeff"))
		if _, ok := label.ChampsExport[entete]; ok {
			cles[i] = entete
			continue
		}
		for cle, libelle := range label.ChampsExport {
			if libelle == entete {
				cles[i] = cle
				break
			}
		}
	}

	var lignes []map[string]any
	for {
		enregistrement, err := lecteur.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("label: ligne CSV illisible: %w", err)
		}
		ligne := map[string]any{}
		for i, valeur := range enregistrement {
			if i < len(cles) && cles[i] != "" {
				ligne[cles[i]] = valeur
			}
		}
		lignes = append(lignes, ligne)
	}
	return lignes, nil
}

// chargerPhotos lit les photos « identifiant=chemin » fournies sur la ligne de
// commande.
func chargerPhotos(drapeaux []string) (map[string][]byte, error) {
	if len(drapeaux) == 0 {
		return nil, nil
	}
	photos := map[string][]byte{}
	for _, entree := range drapeaux {
		parties := strings.SplitN(entree, "=", 2)
		if len(parties) != 2 || strings.TrimSpace(parties[0]) == "" {
			return nil, fmt.Errorf("label: photo attendue au format identifiant=chemin (reçu %q)", entree)
		}
		donnees, err := os.ReadFile(parties[1])
		if err != nil {
			return nil, fmt.Errorf("label: lecture de la photo: %w", err)
		}
		photos[strings.ToLower(strings.TrimSpace(parties[0]))] = donnees
	}
	return photos, nil
}

func cmdLabelTemplates(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label templates", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de label_templates.json (défaut : partagé)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	modeles := label.ChargerModeles(*fichier)
	if *jsonOut {
		dicts := make([]map[string]any, 0, len(modeles))
		for _, m := range modeles {
			dicts = append(dicts, m.ToDict())
		}
		return ecrireJSON(stdout, dicts)
	}
	for _, m := range modeles {
		origine := ""
		if m.Integre {
			origine = " (intégré)"
		}
		fmt.Fprintf(stdout, "%-24s %-50s %s — %d élément(s)%s\n", m.ID, m.Nom, m.Planche, len(m.Elements), origine)
	}
	return nil
}

func cmdLabelShow(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label show", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de label_templates.json")
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" {
		return &codeSortie{code: 2, err: errors.New("--id est obligatoire")}
	}
	modele, ok := trouverModele(label.ChargerModeles(*fichier), *id)
	if !ok {
		return &codeSortie{code: 1, err: fmt.Errorf("modèle introuvable : %s", *id)}
	}
	if *jsonOut {
		return ecrireJSON(stdout, modele.ToDict())
	}
	fmt.Fprintf(stdout, "%s — %s\n", modele.ID, modele.Nom)
	fmt.Fprintf(stdout, "Planche : %s\n", modele.Planche)
	for _, el := range modele.Elements {
		fmt.Fprintf(stdout, "  %-8s %-14s x=%6.2f y=%6.2f w=%6.2f h=%6.2f %s %g pt %s %s\n",
			el.Kind, el.Name(), el.XMM, el.YMM, el.WMM, el.HMM, el.Font, el.SizePT, el.ColorHex, el.Align)
	}
	return nil
}

func cmdLabelRender(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label render", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de label_templates.json")
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	out := fs.String("out", "", "fichier PDF de sortie (obligatoire)")
	csvPath := fs.String("csv", "", "CSV des comptes (séparateur « ; »)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	var photosFlag listeDrapeau
	fs.Var(&photosFlag, "photo", "photo identifiant=chemin (répétable)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" || strings.TrimSpace(*out) == "" {
		return &codeSortie{code: 2, err: errors.New("--id et --out sont obligatoires")}
	}
	modele, ok := trouverModele(label.ChargerModeles(*fichier), *id)
	if !ok {
		return &codeSortie{code: 1, err: fmt.Errorf("modèle introuvable : %s", *id)}
	}
	var lignes []map[string]any
	if *csvPath != "" {
		var err error
		lignes, err = lireLignesCSV(*csvPath)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	photos, err := chargerPhotos(photosFlag)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	donnees, pages, err := label.RendreEtiquettesPDF(lignes, modele, photos)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if err := os.WriteFile(*out, donnees, 0o644); err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("label: écriture du PDF: %w", err)}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"fichier": *out, "pages": pages, "comptes": len(lignes)})
	}
	fmt.Fprintf(stdout, "Étiquettes générées : %s (%d page(s), %d compte(s)).\n", *out, pages, len(lignes))
	return nil
}

func cmdLabelTrombinoscope(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label trombinoscope", flag.ContinueOnError)
	out := fs.String("out", "", "fichier PDF de sortie (obligatoire)")
	csvPath := fs.String("csv", "", "CSV des comptes (séparateur « ; »)")
	page := fs.String("page", "A4", "format de page (A4 ou A3)")
	colonnes := fs.Int("colonnes", 4, "nombre de colonnes (1-8)")
	titre := fs.String("titre", "", "titre du document")
	sousTitre := fs.String("sous-titre", "", "sous-titre du document")
	accent := fs.String("accent", "", "couleur d'accent #RRGGBB")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	var photosFlag listeDrapeau
	fs.Var(&photosFlag, "photo", "photo identifiant=chemin (répétable)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*out) == "" {
		return &codeSortie{code: 2, err: errors.New("--out est obligatoire")}
	}
	var lignes []map[string]any
	if *csvPath != "" {
		var err error
		lignes, err = lireLignesCSV(*csvPath)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	photos, err := chargerPhotos(photosFlag)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	donnees, pages, err := label.GenererTrombinoscopePDF(lignes, label.OptionsTrombinoscope{
		Page: *page, Colonnes: *colonnes, Titre: *titre, SousTitre: *sousTitre,
		Photos: photos, AccentHex: *accent,
	})
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if err := os.WriteFile(*out, donnees, 0o644); err != nil {
		return &codeSortie{code: 1, err: fmt.Errorf("label: écriture du PDF: %w", err)}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"fichier": *out, "pages": pages, "comptes": len(lignes)})
	}
	fmt.Fprintf(stdout, "Trombinoscope généré : %s (%d page(s), %d compte(s)).\n", *out, pages, len(lignes))
	return nil
}

func cmdLabelExport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label export", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de label_templates.json")
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	out := fs.String("out", "", "fichier JSON de sortie (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" || strings.TrimSpace(*out) == "" {
		return &codeSortie{code: 2, err: errors.New("--id et --out sont obligatoires")}
	}
	modele, ok := trouverModele(label.ChargerModeles(*fichier), *id)
	if !ok {
		return &codeSortie{code: 1, err: fmt.Errorf("modèle introuvable : %s", *id)}
	}
	dest, err := label.ExporterModele(modele, *out)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"fichier": dest})
	}
	fmt.Fprintf(stdout, "Modèle exporté : %s\n", dest)
	return nil
}

func cmdLabelImport(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label import", flag.ContinueOnError)
	source := fs.String("file", "", "fichier JSON du modèle à importer (obligatoire)")
	store := fs.String("store", "", "chemin de label_templates.json")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*source) == "" {
		return &codeSortie{code: 2, err: errors.New("--file est obligatoire")}
	}
	modele, err := label.ImporterModele(*source)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	modeles := label.ChargerModeles(*store)
	remplace := false
	for i, m := range modeles {
		if m.ID == modele.ID {
			modeles[i] = modele
			remplace = true
			break
		}
	}
	if !remplace {
		modeles = append(modeles, modele)
	}
	if _, err := label.SauvegarderModeles(modeles, *store); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, modele.ToDict())
	}
	fmt.Fprintf(stdout, "Modèle importé : %s\n", modele.ID)
	return nil
}

func cmdLabelRemove(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label remove", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de label_templates.json")
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" {
		return &codeSortie{code: 2, err: errors.New("--id est obligatoire")}
	}
	modeles := label.ChargerModeles(*fichier)
	restants := make([]label.ModeleLabel, 0, len(modeles))
	supprime := false
	for _, m := range modeles {
		if m.ID == *id {
			supprime = true
			continue
		}
		restants = append(restants, m)
	}
	if !supprime {
		return &codeSortie{code: 1, err: fmt.Errorf("modèle introuvable : %s", *id)}
	}
	if _, err := label.SauvegarderModeles(restants, *fichier); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"restants": len(restants)})
	}
	fmt.Fprintf(stdout, "Modèle supprimé (%d restant(s)).\n", len(restants))
	return nil
}

func cmdLabelMailConfig(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label mail-config", flag.ContinueOnError)
	chemin := fs.String("mail-config", "", "chemin de label_mail.json")
	cle := fs.String("key", "", "chemin de la clé (défaut : secret.key partagé)")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	cfg := mailer.ChargerConfig(*chemin, *cle)
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{
			"host": cfg.Host, "port": cfg.Port, "username": cfg.Username,
			"use_tls": cfg.UseTLS, "use_ssl": cfg.UseSSL,
			"from_addr": cfg.FromAddr, "from_name": cfg.FromName, "timeout": cfg.Timeout,
			"mot_de_passe_defini": cfg.Password != "",
		})
	}
	etat := "non configurée"
	if cfg.Host != "" {
		etat = fmt.Sprintf("%s:%d → %s", cfg.Host, cfg.Port, cfg.FromAddr)
	}
	fmt.Fprintf(stdout, "Configuration SMTP : %s\n", etat)
	return nil
}

func cmdLabelMailSet(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label mail-set", flag.ContinueOnError)
	chemin := fs.String("mail-config", "", "chemin de label_mail.json")
	cle := fs.String("key", "", "chemin de la clé")
	host := fs.String("host", "", "serveur SMTP (obligatoire)")
	port := fs.Int("port", 587, "port SMTP")
	username := fs.String("username", "", "compte SMTP")
	password := fs.String("password", "", "mot de passe SMTP (chiffré au stockage)")
	fromAddr := fs.String("from", "", "adresse d'expéditrice (obligatoire)")
	fromName := fs.String("from-name", "EduSync AD", "nom d'expéditrice")
	timeout := fs.Float64("timeout", 20.0, "délai (s)")
	ssl := fs.Bool("ssl", false, "SSL implicite (port 465)")
	noTLS := fs.Bool("no-tls", false, "désactiver STARTTLS")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	cfg := mailer.ChargerConfig(*chemin, *cle)
	if *host != "" {
		cfg.Host = *host
	}
	cfg.Port = *port
	cfg.Username = *username
	if *password != "" {
		cfg.Password = *password
	}
	cfg.FromAddr = *fromAddr
	cfg.FromName = *fromName
	cfg.Timeout = *timeout
	cfg.UseSSL = *ssl
	cfg.UseTLS = !*noTLS && !*ssl
	if _, err := mailer.SauvegarderConfig(cfg, *chemin, *cle); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"host": cfg.Host, "port": cfg.Port, "from_addr": cfg.FromAddr})
	}
	fmt.Fprintf(stdout, "Configuration SMTP enregistrée (%s:%d).\n", cfg.Host, cfg.Port)
	return nil
}

func cmdLabelSend(args []string, stdout io.Writer) error {
	fs := flag.NewFlagSet("label send", flag.ContinueOnError)
	fichier := fs.String("file", "", "chemin de label_templates.json")
	mailFile := fs.String("mail-config", "", "chemin de label_mail.json")
	cle := fs.String("key", "", "chemin de la clé")
	id := fs.String("id", "", "identifiant du modèle (obligatoire)")
	vers := fs.String("to", "", "destinataire (obligatoire)")
	sujet := fs.String("subject", "", "sujet du message")
	corps := fs.String("body", "", "corps du message")
	csvPath := fs.String("csv", "", "CSV des comptes")
	jsonOut := fs.Bool("json", false, "sortie JSON")
	var photosFlag listeDrapeau
	fs.Var(&photosFlag, "photo", "photo identifiant=chemin (répétable)")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if strings.TrimSpace(*id) == "" || strings.TrimSpace(*vers) == "" {
		return &codeSortie{code: 2, err: errors.New("--id et --to sont obligatoires")}
	}
	modele, ok := trouverModele(label.ChargerModeles(*fichier), *id)
	if !ok {
		return &codeSortie{code: 1, err: fmt.Errorf("modèle introuvable : %s", *id)}
	}
	var lignes []map[string]any
	if *csvPath != "" {
		var err error
		lignes, err = lireLignesCSV(*csvPath)
		if err != nil {
			return &codeSortie{code: 1, err: err}
		}
	}
	photos, err := chargerPhotos(photosFlag)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	donnees, pages, err := label.RendreEtiquettesPDF(lignes, modele, photos)
	if err != nil {
		return &codeSortie{code: 1, err: err}
	}
	cfg := mailer.ChargerConfig(*mailFile, *cle)
	if err := mailer.EnvoyerEtiquette(cfg, *vers, *sujet, *corps, donnees, modele.ID+".pdf", nil); err != nil {
		return &codeSortie{code: 1, err: err}
	}
	if *jsonOut {
		return ecrireJSON(stdout, map[string]any{"destinataire": *vers, "modele": modele.ID, "pages": pages})
	}
	fmt.Fprintf(stdout, "Étiquette envoyée à %s (%d page(s)).\n", *vers, pages)
	return nil
}
