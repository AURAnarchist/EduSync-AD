package assistant

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
	"github.com/estemobs/EduSync-AD/internal/rbac"
)

// Recommandation est un conseil de sécurité produit par l'analyse.
type Recommandation struct {
	// Type est la famille du conseil (`mdp`, `rbac`, `annuaire`, `journal`).
	Type string `json:"type"`
	// Niveau est l'une des constantes `Gravite…`.
	Niveau string `json:"niveau"`
	// Message est le conseil lisible.
	Message string `json:"message"`
}

// RapportSecurite est le résultat de l'analyse de sécurité.
type RapportSecurite struct {
	// Recommandations liste les conseils, du plus grave au moins grave.
	Recommandations []Recommandation `json:"recommandations"`
	// Score est une note de 0 à 100 (100 = aucun point de vigilance).
	Score int `json:"score"`
}

// ContexteSecurite rassemble les éléments de contexte de l'analyse de sécurité.
type ContexteSecurite struct {
	// Role est le rôle RBAC de l'opérateur connecté ("" si inconnu).
	Role string
	// PorteesVides indique qu'une délégation existe sans aucune portée.
	PorteesVides bool
	// ComptesTotal est le nombre de comptes de l'annuaire.
	ComptesTotal int
	// ComptesDesactives est le nombre de comptes désactivés.
	ComptesDesactives int
}

// penalites appliquées au score selon le niveau d'une recommandation.
const (
	penaliteCritique      = 15
	penaliteAvertissement = 8
	penaliteInfo          = 3
)

// AnalyserSecurite examine la politique de mots de passe, le rôle RBAC et
// l'état de l'annuaire, puis retourne des recommandations et un score.
func AnalyserSecurite(cfg config.AppConfig, contexte ContexteSecurite) RapportSecurite {
	recommandations := []Recommandation{}

	recommandations = append(recommandations,
		verifierPolitique("élèves", cfg.PolitiqueMDPEleve)...)
	recommandations = append(recommandations,
		verifierPolitique("personnels", cfg.PolitiqueMDPPersonnel)...)

	if contexte.Role == string(rbac.SuperAdmin) {
		recommandations = append(recommandations, Recommandation{
			Type:    "rbac",
			Niveau:  GraviteInfo,
			Message: "Opérateur en super-administrateur : revoir les portées de délégation.",
		})
	}
	if contexte.PorteesVides {
		recommandations = append(recommandations, Recommandation{
			Type:    "rbac",
			Niveau:  GraviteAvertissement,
			Message: "Une délégation ne définit aucune portée : elle est sans effet ou trop large.",
		})
	}

	if contexte.ComptesTotal > 0 {
		ratio := float64(contexte.ComptesDesactives) / float64(contexte.ComptesTotal)
		if ratio >= 0.2 {
			recommandations = append(recommandations, Recommandation{
				Type:   "annuaire",
				Niveau: GraviteInfo,
				Message: fmt.Sprintf("%d comptes désactivés sur %d (%.0f%%) : envisager un archivage.",
					contexte.ComptesDesactives, contexte.ComptesTotal, ratio*100),
			})
		}
	}

	trierRecommandations(recommandations)

	score := 100
	for _, recommandation := range recommandations {
		score -= penalitePour(recommandation.Niveau)
	}
	if score < 0 {
		score = 0
	}
	return RapportSecurite{Recommandations: recommandations, Score: score}
}

// verifierPolitique contrôle une politique de mot de passe et retourne les
// recommandations associées.
func verifierPolitique(cible string, politique models.PasswordPolicy) []Recommandation {
	recommandations := []Recommandation{}

	switch {
	case politique.Longueur < 8:
		recommandations = append(recommandations, Recommandation{
			Type:    "mdp",
			Niveau:  GraviteCritique,
			Message: fmt.Sprintf("La politique mot de passe des %s est trop courte (%d caractères).", cible, politique.Longueur),
		})
	case politique.Longueur < 12:
		recommandations = append(recommandations, Recommandation{
			Type:    "mdp",
			Niveau:  GraviteAvertissement,
			Message: fmt.Sprintf("La politique mot de passe des %s pourrait être allongée (%d caractères).", cible, politique.Longueur),
		})
	}

	if !politique.Majuscules {
		recommandations = append(recommandations, Recommandation{
			Type:    "mdp",
			Niveau:  GraviteAvertissement,
			Message: fmt.Sprintf("La politique mot de passe des %s n'exige pas de majuscules.", cible),
		})
	}
	if !politique.Chiffres {
		recommandations = append(recommandations, Recommandation{
			Type:    "mdp",
			Niveau:  GraviteAvertissement,
			Message: fmt.Sprintf("La politique mot de passe des %s n'exige pas de chiffres.", cible),
		})
	}
	if !politique.CaracteresSpeciaux {
		recommandations = append(recommandations, Recommandation{
			Type:    "mdp",
			Niveau:  GraviteInfo,
			Message: fmt.Sprintf("La politique mot de passe des %s n'exige pas de caractères spéciaux.", cible),
		})
	}
	return recommandations
}

// Compteur associe une valeur (type d'action, opérateur) à son nombre
// d'occurrences.
type Compteur struct {
	// Cle est la valeur comptée.
	Cle string `json:"cle"`
	// Nombre est le nombre d'occurrences.
	Nombre int `json:"nombre"`
}

// RapportJournal est le résultat de l'analyse du journal d'actions.
type RapportJournal struct {
	// Total est le nombre d'entrées analysées.
	Total int `json:"total"`
	// Simulations est le nombre d'entrées simulées.
	Simulations int `json:"simulations"`
	// Echecs est le nombre d'entrées dont le résultat n'est pas un succès.
	Echecs int `json:"echecs"`
	// ParType classe les actions par type (décroissant).
	ParType []Compteur `json:"par_type"`
	// ParOperateur classe les actions par opérateur (décroissant).
	ParOperateur []Compteur `json:"par_operateur"`
	// HorsHoraires compte les actions hors plage horaire (avant 7 h, après 20 h).
	HorsHoraires int `json:"hors_horaires"`
	// Recommandations liste les conseils issus du journal.
	Recommandations []Recommandation `json:"recommandations"`
}

// HeureDebutJournee et HeureFinJournee bornent la plage horaire normale.
const (
	HeureDebutJournee = 7
	HeureFinJournee   = 20
)

// AnalyserJournal résume le journal d'actions et signale les points de
// vigilance (échecs, suppressions, activité hors horaires).
func AnalyserJournal(entrees []audit.Entry) RapportJournal {
	rapport := RapportJournal{Total: len(entrees), Recommandations: []Recommandation{}}
	parType := map[string]int{}
	parOperateur := map[string]int{}
	suppressions := 0

	for _, entree := range entrees {
		if entree.Simulation {
			rapport.Simulations++
		}
		if !resultatPositif(entree.Resultat) {
			rapport.Echecs++
		}
		if entree.ActionType != "" {
			parType[entree.ActionType]++
		}
		if entree.Utilisateur != "" {
			parOperateur[entree.Utilisateur]++
		}
		if strings.Contains(strings.ToLower(entree.ActionType), "suppress") ||
			strings.Contains(strings.ToLower(entree.ActionType), "suppression") {
			suppressions++
		}
		if horsHoraires(entree.Timestamp) {
			rapport.HorsHoraires++
		}
	}

	rapport.ParType = compteursTries(parType)
	rapport.ParOperateur = compteursTries(parOperateur)

	if rapport.Total > 0 && rapport.Echecs*10 > rapport.Total {
		rapport.Recommandations = append(rapport.Recommandations, Recommandation{
			Type:   "journal",
			Niveau: GraviteAvertissement,
			Message: fmt.Sprintf("%d échecs sur %d actions récentes : vérifier les droits et l'annuaire.",
				rapport.Echecs, rapport.Total),
		})
	}
	if suppressions > 0 {
		rapport.Recommandations = append(rapport.Recommandations, Recommandation{
			Type:   "journal",
			Niveau: GraviteInfo,
			Message: fmt.Sprintf("%d suppression(s) enregistrée(s) : revoir les justificatifs.",
				suppressions),
		})
	}
	if rapport.HorsHoraires > 0 {
		rapport.Recommandations = append(rapport.Recommandations, Recommandation{
			Type:   "journal",
			Niveau: GraviteInfo,
			Message: fmt.Sprintf("%d action(s) hors horaires ouvrés (avant %dh, après %dh).",
				rapport.HorsHoraires, HeureDebutJournee, HeureFinJournee),
		})
	}

	trierRecommandations(rapport.Recommandations)
	return rapport
}

// resultatPositif indique si le résultat d'une action est un succès.
// Une entrée vide est considérée comme positive (anciennes entrées).
func resultatPositif(resultat string) bool {
	switch strings.ToLower(strings.TrimSpace(resultat)) {
	case "", "succes", "succès", "ok", "success":
		return true
	}
	return false
}

// horsHoraires indique si un horodatage ISO 8601 tombe hors plage ouvrée.
// Un horodatage illisible est ignoré (retour faux).
func horsHoraires(horodatage string) bool {
	for _, disposition := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02 15:04:05"} {
		moment, err := time.Parse(disposition, horodatage)
		if err == nil {
			return moment.Hour() < HeureDebutJournee || moment.Hour() >= HeureFinJournee
		}
	}
	return false
}

// compteursTries convertit une table en liste triée par nombre décroissant
// puis par clé croissante (sortie stable).
func compteursTries(table map[string]int) []Compteur {
	compteurs := make([]Compteur, 0, len(table))
	for cle, nombre := range table {
		compteurs = append(compteurs, Compteur{Cle: cle, Nombre: nombre})
	}
	sort.Slice(compteurs, func(i, j int) bool {
		if compteurs[i].Nombre != compteurs[j].Nombre {
			return compteurs[i].Nombre > compteurs[j].Nombre
		}
		return compteurs[i].Cle < compteurs[j].Cle
	})
	return compteurs
}

// trierRecommandations ordonne les recommandations par gravité décroissante.
func trierRecommandations(recommandations []Recommandation) {
	sort.SliceStable(recommandations, func(i, j int) bool {
		if rangGravite(recommandations[i].Niveau) != rangGravite(recommandations[j].Niveau) {
			return rangGravite(recommandations[i].Niveau) > rangGravite(recommandations[j].Niveau)
		}
		return recommandations[i].Message < recommandations[j].Message
	})
}

// penalitePour retourne la pénalité de score associée à un niveau.
func penalitePour(niveau string) int {
	switch niveau {
	case GraviteCritique:
		return penaliteCritique
	case GraviteAvertissement:
		return penaliteAvertissement
	case GraviteInfo:
		return penaliteInfo
	}
	return 0
}
