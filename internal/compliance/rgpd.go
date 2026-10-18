package compliance

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/portal"
)

// CompteRGPD est la projection d'un compte pour le rapport RGPD.
type CompteRGPD struct {
	// Identifiant est le sAMAccountName.
	Identifiant string
	// NomComplet est le nom affiché du titulaire.
	NomComplet string
	// Mail est l'adresse de messagerie (peut être vide).
	Mail string
	// OU est l'unité d'organisation d'accueil.
	OU string
	// Actif indique si le compte est activé.
	Actif bool
	// CreeLe est l'horodatage de création (ISO 8601, vide si inconnu).
	CreeLe string
}

// SourcesRGPD rassemble les données brutes nécessaires au rapport.
type SourcesRGPD struct {
	// Comptes sont les comptes de l'annuaire analysé.
	Comptes []CompteRGPD
	// Entrees est le journal d'actions.
	Entrees []audit.Entry
	// Demandes sont les pré-inscriptions du portail.
	Demandes []portal.Demande
	// Secrets liste les identifiants présents dans le coffre local.
	Secrets []string
}

// OptionsRGPD paramètre la génération du rapport.
type OptionsRGPD struct {
	// Debut et Fin bornent la période analysée (zéro = période par défaut :
	// un an avant la fin).
	Debut time.Time
	Fin   time.Time
	// Personne est l'identifiant dont on recherche les données (droit à
	// l'oubli). Vide = aucune recherche nominative.
	Personne string
	// ConservationJours est la durée de conservation retenue (0 = 365).
	ConservationJours int
}

// InventaireDonnees quantifie les données personnelles stockées.
type InventaireDonnees struct {
	// TotalComptes est le nombre de comptes de l'annuaire.
	TotalComptes int `json:"total_comptes"`
	// ComptesActifs est le nombre de comptes activés.
	ComptesActifs int `json:"comptes_actifs"`
	// ComptesDesactives est le nombre de comptes désactivés.
	ComptesDesactives int `json:"comptes_desactives"`
	// ComptesSansMail est le nombre de comptes sans adresse de messagerie.
	ComptesSansMail int `json:"comptes_sans_mail"`
	// ComptesAnciens est le nombre de comptes créés avant la durée de
	// conservation.
	ComptesAnciens int `json:"comptes_anciens"`
	// EntreesJournal est le nombre d'entrées du journal analysées.
	EntreesJournal int `json:"entrees_journal"`
	// EntreesSimulation est le nombre d'entrées simulées.
	EntreesSimulation int `json:"entrees_simulation"`
	// EntreesAnciennes est le nombre d'entrées plus vieilles que la durée de
	// conservation.
	EntreesAnciennes int `json:"entrees_anciennes"`
	// DemandesPortail est le nombre de pré-inscriptions enregistrées.
	DemandesPortail int `json:"demandes_portail"`
	// SecretsCoffre est le nombre de secrets conservés dans le coffre local.
	SecretsCoffre int `json:"secrets_coffre"`
	// Operateurs est le nombre d'opérateurs distincts présents au journal.
	Operateurs int `json:"operateurs"`
}

// SectionDonnees décrit une catégorie de données personnelles traitées.
type SectionDonnees struct {
	// Categorie nomme le jeu de données.
	Categorie string `json:"categorie"`
	// Source indique le fichier ou l'annuaire d'origine.
	Source string `json:"source"`
	// Champs liste les données personnelles concernées.
	Champs []string `json:"champs"`
	// Finalite rappelle la raison du traitement.
	Finalite string `json:"finalite"`
	// ConservationJours est la durée de conservation appliquée.
	ConservationJours int `json:"conservation_jours"`
}

// ReferencePersonne localise une donnée personnelle dans les sources locales.
type ReferencePersonne struct {
	// Source est le jeu de données (« journal », « portail », « coffre »).
	Source string `json:"source"`
	// Reference identifie l'enregistrement (id d'entrée, id de demande…).
	Reference string `json:"reference"`
	// Champ est le champ concerné.
	Champ string `json:"champ"`
	// Valeur est la valeur trouvée (donnée personnelle).
	Valeur string `json:"valeur"`
}

// RecommandationRGPD est un conseil de conformité.
type RecommandationRGPD struct {
	// Type classe le conseil (« conservation », « anonymisation », « securite »).
	Type string `json:"type"`
	// Niveau est NiveauInfo, NiveauAvertissement ou NiveauCritique.
	Niveau string `json:"niveau"`
	// Message est le conseil lisible.
	Message string `json:"message"`
}

// PeriodeRGPD borne la période analysée.
type PeriodeRGPD struct {
	// Debut est la borne basse (ISO 8601, secondes).
	Debut string `json:"debut"`
	// Fin est la borne haute (ISO 8601, secondes).
	Fin string `json:"fin"`
}

// RapportRGPD est le résultat de `GenererRapportRGPD`.
type RapportRGPD struct {
	// GenerationDate est l'instant de génération.
	GenerationDate string `json:"generation_date"`
	// Periode borne l'analyse.
	Periode PeriodeRGPD `json:"periode"`
	// Inventaire quantifie les données personnelles.
	Inventaire InventaireDonnees `json:"inventaire"`
	// Sections décrit les jeux de données traités.
	Sections []SectionDonnees `json:"sections"`
	// DroitOubli localise les données d'une personne (recherche nominative).
	DroitOubli []ReferencePersonne `json:"droit_oubli"`
	// Recommandations liste les conseils de conformité.
	Recommandations []RecommandationRGPD `json:"recommandations"`
}

// ConservationDefautJours est la durée de conservation retenue par défaut.
const ConservationDefautJours = 365

// GenererRapportRGPD produit un rapport de conformité sur les données
// personnelles stockées localement : comptes de l'annuaire, journal d'actions,
// pré-inscriptions du portail et coffre de mots de passe.
//
// Le rapport est **en lecture seule** : il ne supprime ni ne modifie aucune
// donnée. Le journal d'actions est « append-only » (exigence de traçabilité),
// l'exercice du droit à l'oubli y passe donc par une pseudonymisation manuelle
// documentée, pas par une suppression.
func GenererRapportRGPD(sources SourcesRGPD, options OptionsRGPD, maintenant time.Time) RapportRGPD {
	fin := options.Fin
	if fin.IsZero() {
		fin = maintenant
	}
	debut := options.Debut
	if debut.IsZero() {
		debut = fin.AddDate(-1, 0, 0)
	}
	conservation := options.ConservationJours
	if conservation <= 0 {
		conservation = ConservationDefautJours
	}
	limite := fin.AddDate(0, 0, -conservation)

	rapport := RapportRGPD{
		GenerationDate:  maintenant.Format(formatISO),
		Periode:         PeriodeRGPD{Debut: debut.Format(formatISO), Fin: fin.Format(formatISO)},
		Inventaire:      InventaireDonnees{},
		Sections:        sectionsRGPD(conservation),
		DroitOubli:      []ReferencePersonne{},
		Recommandations: []RecommandationRGPD{},
	}

	// Comptes de l'annuaire.
	inventaire := rapport.Inventaire
	inventaire.TotalComptes = len(sources.Comptes)
	for _, compte := range sources.Comptes {
		if compte.Actif {
			inventaire.ComptesActifs++
		} else {
			inventaire.ComptesDesactives++
		}
		if strings.TrimSpace(compte.Mail) == "" {
			inventaire.ComptesSansMail++
		}
		if cree, err := time.Parse(time.RFC3339, compte.CreeLe); err == nil && cree.Before(limite) {
			inventaire.ComptesAnciens++
		}
	}

	// Journal d'actions.
	operateurs := map[string]bool{}
	inventaire.EntreesJournal = len(sources.Entrees)
	for _, entree := range sources.Entrees {
		if entree.Simulation {
			inventaire.EntreesSimulation++
		}
		if entree.Utilisateur != "" {
			operateurs[strings.ToLower(entree.Utilisateur)] = true
		}
		if moment, err := time.Parse(time.RFC3339, entree.Timestamp); err == nil && moment.Before(limite) {
			inventaire.EntreesAnciennes++
		}
	}
	inventaire.Operateurs = len(operateurs)

	inventaire.DemandesPortail = len(sources.Demandes)
	inventaire.SecretsCoffre = len(sources.Secrets)
	rapport.Inventaire = inventaire

	// Recherche nominative (droit à l'oubli).
	if strings.TrimSpace(options.Personne) != "" {
		rapport.DroitOubli = ChercherDroitOubli(sources, options.Personne)
	}

	rapport.Recommandations = recommandationsRGPD(rapport, conservation)
	return rapport
}

// ChercherDroitOubli localise toutes les occurrences d'une personne dans les
// sources locales (journal, pré-inscriptions, coffre). Aucune donnée n'est
// modifiée : le résultat sert de base à une pseudonymisation manuelle.
func ChercherDroitOubli(sources SourcesRGPD, personne string) []ReferencePersonne {
	cible := strings.ToLower(strings.TrimSpace(personne))
	if cible == "" {
		return []ReferencePersonne{}
	}

	references := []ReferencePersonne{}
	for _, compte := range sources.Comptes {
		if correspond(cible, compte.Identifiant, compte.Mail, compte.NomComplet) {
			references = append(references, ReferencePersonne{
				Source: "annuaire", Reference: compte.Identifiant, Champ: "compte",
				Valeur: strings.TrimSpace(compte.NomComplet + " " + compte.Mail),
			})
		}
	}
	for _, entree := range sources.Entrees {
		if correspond(cible, entree.Compte, entree.Utilisateur, entree.Detail) {
			references = append(references, ReferencePersonne{
				Source: "journal", Reference: fmt.Sprintf("%d", entree.ID),
				Champ: entree.ActionType, Valeur: entree.Compte,
			})
		}
	}
	for _, demande := range sources.Demandes {
		if correspond(cible, demande.Mail, demande.NomComplet(), demande.Prenom, demande.Nom) {
			references = append(references, ReferencePersonne{
				Source: "portail", Reference: demande.ID, Champ: "demande",
				Valeur: strings.TrimSpace(demande.NomComplet() + " " + demande.Mail),
			})
		}
	}
	for _, secret := range sources.Secrets {
		if strings.ToLower(strings.TrimSpace(secret)) == cible {
			references = append(references, ReferencePersonne{
				Source: "coffre", Reference: secret, Champ: "mot_de_passe",
				Valeur: "(secret chiffré)",
			})
		}
	}

	sort.SliceStable(references, func(i, j int) bool {
		if references[i].Source != references[j].Source {
			return references[i].Source < references[j].Source
		}
		return references[i].Reference < references[j].Reference
	})
	return references
}

// correspond teste si l'un des champs vaut la cible (insensible à la casse) ou
// la contient comme mot entier délimité (pour `detail` du journal).
func correspond(cible string, champs ...string) bool {
	for _, champ := range champs {
		valeur := strings.ToLower(strings.TrimSpace(champ))
		if valeur == "" {
			continue
		}
		if valeur == cible || strings.Contains(valeur, cible) {
			return true
		}
	}
	return false
}

// sectionsRGPD décrit les jeux de données personnelles traités par
// l'application. Ces fiches sont statiques : elles documentent le traitement.
func sectionsRGPD(conservation int) []SectionDonnees {
	return []SectionDonnees{
		{
			Categorie: "comptes annuaire", Source: "Active Directory",
			Champs:            []string{"nom", "prénom", "identifiant", "adresse mail", "OU", "appartenances aux groupes"},
			Finalite:          "gestion du cycle de vie des comptes scolaires",
			ConservationJours: conservation,
		},
		{
			Categorie: "journal d'actions", Source: "journal.db",
			Champs:            []string{"opérateur", "compte visé", "horodatage", "détail"},
			Finalite:          "traçabilité des opérations et sécurité (append-only)",
			ConservationJours: conservation,
		},
		{
			Categorie: "pré-inscriptions portail", Source: "portail.db",
			Champs:            []string{"nom", "prénom", "adresse mail", "classe", "motif"},
			Finalite:          "demande de création de compte par l'usager",
			ConservationJours: conservation,
		},
		{
			Categorie: "coffre de mots de passe", Source: "password_vault.sqlite3",
			Champs:            []string{"identifiant", "mot de passe (chiffré AES-256)"},
			Finalite:          "restitution des mots de passe positionnés par l'application",
			ConservationJours: conservation,
		},
		{
			Categorie: "sites et secrets locaux", Source: "domaines.json, secret.key, api_keys.json",
			Champs:            []string{"contrôleurs", "identifiants techniques", "empreintes de clés"},
			Finalite:          "connexions multi-domaine et accès à l'API",
			ConservationJours: conservation,
		},
	}
}

// recommandationsRGPD produit les conseils de conformité à partir de
// l'inventaire.
func recommandationsRGPD(rapport RapportRGPD, conservation int) []RecommandationRGPD {
	recommandations := []RecommandationRGPD{}
	inventaire := rapport.Inventaire

	if inventaire.ComptesAnciens > 0 {
		recommandations = append(recommandations, RecommandationRGPD{
			Type: "conservation", Niveau: NiveauAvertissement,
			Message: fmt.Sprintf(
				"%d compte(s) créés il y a plus de %d jours : vérifier leur utilité et archiver les comptes inactifs.",
				inventaire.ComptesAnciens, conservation),
		})
	}
	if inventaire.ComptesDesactives > 0 {
		recommandations = append(recommandations, RecommandationRGPD{
			Type: "anonymisation", Niveau: NiveauInfo,
			Message: fmt.Sprintf(
				"%d compte(s) désactivé(s) : planifier leur anonymisation ou leur suppression après le délai de conservation.",
				inventaire.ComptesDesactives),
		})
	}
	if inventaire.EntreesAnciennes > 0 {
		recommandations = append(recommandations, RecommandationRGPD{
			Type: "conservation", Niveau: NiveauAvertissement,
			Message: fmt.Sprintf(
				"%d entrée(s) de journal dépassent la durée de conservation : exporter puis purger (`audit purge`).",
				inventaire.EntreesAnciennes),
		})
	}
	if inventaire.ComptesSansMail > 0 {
		recommandations = append(recommandations, RecommandationRGPD{
			Type: "securite", Niveau: NiveauInfo,
			Message: fmt.Sprintf(
				"%d compte(s) sans adresse de messagerie : la notification aux usagers ne peut pas être assurée.",
				inventaire.ComptesSansMail),
		})
	}
	if inventaire.SecretsCoffre > 0 {
		recommandations = append(recommandations, RecommandationRGPD{
			Type: "securite", Niveau: NiveauInfo,
			Message: fmt.Sprintf(
				"%d secret(s) conservés localement : le coffre ne doit pas quitter la machine d'administration (chiffrement AES-256, permissions 0600).",
				inventaire.SecretsCoffre),
		})
	}
	if len(recommandations) == 0 {
		recommandations = append(recommandations, RecommandationRGPD{
			Type: "conservation", Niveau: NiveauInfo,
			Message: "Aucun écart de conservation détecté sur la période analysée.",
		})
	}
	return recommandations
}

// ExporterRapportRGPDCSV écrit le rapport au format CSV (colonnes
// `categorie;cle;valeur`). Le fichier est créé avec des permissions 0600 car il
// contient des données personnelles.
func ExporterRapportRGPDCSV(rapport RapportRGPD, chemin string) (string, error) {
	if strings.TrimSpace(chemin) == "" {
		return "", fmt.Errorf("compliance: chemin d'export vide")
	}
	if dir := filepath.Dir(chemin); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("compliance: création du dossier d'export: %w", err)
		}
	}

	fichier, err := os.OpenFile(chemin, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("compliance: création de %s: %w", chemin, err)
	}
	defer fichier.Close() //nolint:errcheck // l'erreur de fermeture n'apporte rien ici

	writer := csv.NewWriter(fichier)
	lignes := [][]string{{"categorie", "cle", "valeur"}}
	lignes = append(lignes,
		[]string{"rapport", "generation_date", rapport.GenerationDate},
		[]string{"periode", "debut", rapport.Periode.Debut},
		[]string{"periode", "fin", rapport.Periode.Fin},
	)
	inventaire := rapport.Inventaire
	lignes = append(lignes,
		[]string{"inventaire", "total_comptes", fmt.Sprintf("%d", inventaire.TotalComptes)},
		[]string{"inventaire", "comptes_actifs", fmt.Sprintf("%d", inventaire.ComptesActifs)},
		[]string{"inventaire", "comptes_desactives", fmt.Sprintf("%d", inventaire.ComptesDesactives)},
		[]string{"inventaire", "comptes_sans_mail", fmt.Sprintf("%d", inventaire.ComptesSansMail)},
		[]string{"inventaire", "comptes_anciens", fmt.Sprintf("%d", inventaire.ComptesAnciens)},
		[]string{"inventaire", "entrees_journal", fmt.Sprintf("%d", inventaire.EntreesJournal)},
		[]string{"inventaire", "entrees_simulation", fmt.Sprintf("%d", inventaire.EntreesSimulation)},
		[]string{"inventaire", "entrees_anciennes", fmt.Sprintf("%d", inventaire.EntreesAnciennes)},
		[]string{"inventaire", "demandes_portail", fmt.Sprintf("%d", inventaire.DemandesPortail)},
		[]string{"inventaire", "secrets_coffre", fmt.Sprintf("%d", inventaire.SecretsCoffre)},
		[]string{"inventaire", "operateurs", fmt.Sprintf("%d", inventaire.Operateurs)},
	)
	for _, section := range rapport.Sections {
		lignes = append(lignes, []string{"section", section.Categorie, section.Source})
	}
	for _, reference := range rapport.DroitOubli {
		lignes = append(lignes, []string{"droit_oubli", reference.Source + "/" + reference.Reference, reference.Valeur})
	}
	for _, recommandation := range rapport.Recommandations {
		lignes = append(lignes, []string{"recommandation", recommandation.Type + "/" + recommandation.Niveau, recommandation.Message})
	}

	if err := writer.WriteAll(lignes); err != nil {
		return "", fmt.Errorf("compliance: écriture de %s: %w", chemin, err)
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", fmt.Errorf("compliance: écriture de %s: %w", chemin, err)
	}
	return chemin, nil
}
