// Package assistant fournit les fonctions d'assistance de M29 : détection
// d'anomalies dans l'annuaire, suggestions de nettoyage de fin d'année,
// génération « intelligente » d'identifiants et analyse de sécurité/journal.
//
// Les analyses travaillent sur un `Inventaire` — un instantané de l'annuaire —
// et non directement sur une connexion LDAP : elles sont donc testables sans
// Active Directory, et la collecte réelle est isolée dans `adapter.go`.
package assistant

import (
	"fmt"
	"sort"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// Niveaux de gravité des anomalies, suggestions et recommandations.
const (
	// GraviteInfo signale une observation sans urgence.
	GraviteInfo = "info"
	// GraviteAvertissement signale un point à corriger.
	GraviteAvertissement = "avertissement"
	// GraviteCritique signale un problème à traiter sans délai.
	GraviteCritique = "critique"
)

// Types d'anomalies (mêmes clés que la version Python).
const (
	AnomalieIdentifiantsDoubles = "identifiants_en_double"
	AnomalieComptesOrphelins    = "comptes_orphelins"
	AnomalieOUsVides            = "ou_vides"
	AnomalieGroupesSansMembres  = "groupes_sans_membres"
)

// Compte est un compte utilisateur vu par l'assistant.
type Compte struct {
	// DN est le DN complet du compte.
	DN string
	// Identifiant est le sAMAccountName.
	Identifiant string
	// NomComplet est le displayName (repli : CN).
	NomComplet string
	// Mail est l'adresse de messagerie (peut être vide).
	Mail string
	// Actif indique si le compte est activé (userAccountControl).
	Actif bool
	// OU est le DN de l'OU parente (déduit du DN si absent).
	OU string
	// Groupes est la liste des DN des groupes dont le compte est membre.
	Groupes []string
}

// Groupe est un groupe de sécurité ou de distribution vu par l'assistant.
type Groupe struct {
	// DN est le DN complet du groupe.
	DN string
	// Nom est le nom commun (CN) du groupe.
	Nom string
	// Identifiant est le sAMAccountName.
	Identifiant string
	// Description est la description du groupe (peut être vide).
	Description string
	// Membres est la liste des DN des membres directs.
	Membres []string
	// OU est le DN de l'OU parente.
	OU string
}

// Unite est une unité d'organisation (OU) vue par l'assistant.
type Unite struct {
	// DN est le DN complet de l'OU.
	DN string
	// Nom est le nom de l'OU (attribut `ou`, repli sur le RDN).
	Nom string
	// ParentDN est le DN de l'OU parente.
	ParentDN string
}

// Inventaire est l'instantané de l'annuaire sur lequel travaillent les
// analyses.
type Inventaire struct {
	// BaseDN est la racine du domaine interrogé.
	BaseDN string
	// Comptes liste les comptes utilisateurs.
	Comptes []Compte
	// Groupes liste les groupes.
	Groupes []Groupe
	// Unites liste les unités d'organisation.
	Unites []Unite
}

// Anomalie décrit un problème détecté dans l'annuaire.
type Anomalie struct {
	// Type est l'une des constantes `Anomalie…`.
	Type string `json:"type"`
	// Gravite est l'une des constantes `Gravite…`.
	Gravite string `json:"gravite"`
	// Message est un texte lisible décrivant le problème.
	Message string `json:"message"`
	// Objets liste les DN concernés.
	Objets []string `json:"objets"`
}

// OptionsAnomalies sélectionne les vérifications à exécuter.
type OptionsAnomalies struct {
	// Doublons vérifie les identifiants attribués plusieurs fois.
	Doublons bool
	// ComptesOrphelins vérifie les comptes dont l'OU parente a disparu.
	ComptesOrphelins bool
	// OUsVides vérifie les OU sans utilisateur, groupe ni sous-OU.
	OUsVides bool
	// GroupesSansMembres vérifie les groupes sans membre.
	GroupesSansMembres bool
}

// OptionsAnomaliesToutes active les quatre vérifications.
func OptionsAnomaliesToutes() OptionsAnomalies {
	return OptionsAnomalies{
		Doublons:           true,
		ComptesOrphelins:   true,
		OUsVides:           true,
		GroupesSansMembres: true,
	}
}

// DetecterAnomalies analyse l'inventaire et retourne les anomalies trouvées.
// Le résultat est trié (type, gravité, premier objet) pour rester stable d'un
// appel à l'autre — les tests et la sortie JSON s'appuient dessus.
func DetecterAnomalies(inventaire Inventaire, options OptionsAnomalies) []Anomalie {
	var anomalies []Anomalie

	if options.Doublons {
		anomalies = append(anomalies, detecterIdentifiantsDoubles(inventaire)...)
	}
	if options.ComptesOrphelins {
		anomalies = append(anomalies, detecterComptesOrphelins(inventaire)...)
	}
	if options.OUsVides {
		anomalies = append(anomalies, detecterOUsVides(inventaire)...)
	}
	if options.GroupesSansMembres {
		anomalies = append(anomalies, detecterGroupesSansMembres(inventaire)...)
	}

	trierAnomalies(anomalies)
	return anomalies
}

// detecterIdentifiantsDoubles cherche les sAMAccountName attribués plusieurs
// fois (comparaison insensible à la casse, comme Active Directory qui refuse
// un doublon exact mais pas une simple différence de casse selon les versions).
func detecterIdentifiantsDoubles(inventaire Inventaire) []Anomalie {
	parIdentifiant := map[string][]string{}
	for _, compte := range inventaire.Comptes {
		identifiant := strings.TrimSpace(compte.Identifiant)
		if identifiant == "" {
			continue
		}
		cle := strings.ToLower(identifiant)
		parIdentifiant[cle] = append(parIdentifiant[cle], compte.DN)
	}

	anomalies := []Anomalie{}
	for identifiant, dns := range parIdentifiant {
		if len(dns) < 2 {
			continue
		}
		sort.Strings(dns)
		anomalies = append(anomalies, Anomalie{
			Type:    AnomalieIdentifiantsDoubles,
			Gravite: GraviteCritique,
			Message: fmt.Sprintf("L'identifiant « %s » est utilisé par %d comptes.", identifiant, len(dns)),
			Objets:  dns,
		})
	}
	return anomalies
}

// detecterComptesOrphelins cherche les comptes dont l'OU parente n'existe plus.
//
// Seules les parentes qui ressemblent à une OU (`OU=…`) sont examinées : un
// compte placé dans un conteneur par défaut (`CN=Users`, `CN=Computers`) est
// parfaitement valide. La vérification est ignorée si l'inventaire ne contient
// aucune OU, pour ne pas signaler tout l'annuaire lorsqu'une collecte partielle
// a échoué.
func detecterComptesOrphelins(inventaire Inventaire) []Anomalie {
	if len(inventaire.Unites) == 0 {
		return nil
	}
	connues := map[string]bool{}
	for _, unite := range inventaire.Unites {
		connues[strings.ToLower(unite.DN)] = true
	}
	if inventaire.BaseDN != "" {
		connues[strings.ToLower(inventaire.BaseDN)] = true
	}

	dns := []string{}
	for _, compte := range inventaire.Comptes {
		parent := parentDN(compte.DN)
		if parent == "" || !strings.HasPrefix(strings.ToUpper(parent), "OU=") {
			continue
		}
		if !connues[strings.ToLower(parent)] {
			dns = append(dns, compte.DN)
		}
	}
	if len(dns) == 0 {
		return nil
	}
	sort.Strings(dns)
	return []Anomalie{{
		Type:    AnomalieComptesOrphelins,
		Gravite: GraviteAvertissement,
		Message: fmt.Sprintf("%d compte(s) dont l'OU parente n'existe plus.", len(dns)),
		Objets:  dns,
	}}
}

// detecterOUsVides cherche les OU qui ne contiennent ni compte, ni groupe, ni
// sous-OU.
func detecterOUsVides(inventaire Inventaire) []Anomalie {
	occupees := map[string]bool{}
	for _, compte := range inventaire.Comptes {
		occupees[strings.ToLower(ouDuCompte(compte))] = true
	}
	for _, groupe := range inventaire.Groupes {
		occupees[strings.ToLower(ouDuGroupe(groupe))] = true
	}
	parentes := map[string]bool{}
	for _, unite := range inventaire.Unites {
		if unite.ParentDN != "" {
			parentes[strings.ToLower(unite.ParentDN)] = true
		}
	}

	dns := []string{}
	for _, unite := range inventaire.Unites {
		cle := strings.ToLower(unite.DN)
		if occupees[cle] || parentes[cle] {
			continue
		}
		dns = append(dns, unite.DN)
	}
	if len(dns) == 0 {
		return nil
	}
	sort.Strings(dns)
	return []Anomalie{{
		Type:    AnomalieOUsVides,
		Gravite: GraviteInfo,
		Message: fmt.Sprintf("%d unité(s) d'organisation sans objet.", len(dns)),
		Objets:  dns,
	}}
}

// detecterGroupesSansMembres cherche les groupes sans membre direct.
func detecterGroupesSansMembres(inventaire Inventaire) []Anomalie {
	dns := []string{}
	for _, groupe := range inventaire.Groupes {
		if len(groupe.Membres) == 0 {
			dns = append(dns, groupe.DN)
		}
	}
	if len(dns) == 0 {
		return nil
	}
	sort.Strings(dns)
	return []Anomalie{{
		Type:    AnomalieGroupesSansMembres,
		Gravite: GraviteAvertissement,
		Message: fmt.Sprintf("%d groupe(s) sans membre.", len(dns)),
		Objets:  dns,
	}}
}

// Suggestion décrit une action de nettoyage de fin d'année.
type Suggestion struct {
	// Type est la famille de la suggestion.
	Type string `json:"type"`
	// Action est l'action proposée (`archiver`, `supprimer`, `deplacer`, `verifier`).
	Action string `json:"action"`
	// Gravite est l'une des constantes `Gravite…`.
	Gravite string `json:"gravite"`
	// Message est un texte lisible.
	Message string `json:"message"`
	// Objets liste les DN concernés.
	Objets []string `json:"objets"`
}

// Types de suggestions de nettoyage.
const (
	SuggestionComptesDesactives = "comptes_desactives"
	SuggestionGroupesVides      = "groupes_vides"
	SuggestionOUsVides          = "ou_vides"
	SuggestionComptesOrphelins  = "comptes_orphelins"
)

// OptionsNettoyage sélectionne les familles de suggestions à produire.
type OptionsNettoyage struct {
	// ComptesDesactives propose l'archivage des comptes désactivés.
	ComptesDesactives bool
	// GroupesVides propose la suppression des groupes sans membre.
	GroupesVides bool
	// OUsVides propose la suppression des OU vides.
	OUsVides bool
	// ComptesOrphelins propose de replacer les comptes orphelins.
	ComptesOrphelins bool
}

// OptionsNettoyageToutes active les quatre familles de suggestions.
func OptionsNettoyageToutes() OptionsNettoyage {
	return OptionsNettoyage{
		ComptesDesactives: true,
		GroupesVides:      true,
		OUsVides:          true,
		ComptesOrphelins:  true,
	}
}

// SuggererNettoyage produit les suggestions de nettoyage de fin d'année.
//
// Une suggestion n'est jamais une suppression automatique : elle est proposée à
// l'opérateur, qui la valide dans l'interface (même principe que la simulation
// avant exécution du reste de l'application).
func SuggererNettoyage(inventaire Inventaire, options OptionsNettoyage) []Suggestion {
	suggestions := []Suggestion{}

	if options.ComptesDesactives {
		dns := []string{}
		for _, compte := range inventaire.Comptes {
			if !compte.Actif {
				dns = append(dns, compte.DN)
			}
		}
		if len(dns) > 0 {
			sort.Strings(dns)
			suggestions = append(suggestions, Suggestion{
				Type:    SuggestionComptesDesactives,
				Action:  "archiver",
				Gravite: GraviteInfo,
				Message: fmt.Sprintf("%d compte(s) désactivé(s) à archiver.", len(dns)),
				Objets:  dns,
			})
		}
	}

	if options.GroupesVides {
		dns := []string{}
		for _, groupe := range inventaire.Groupes {
			if len(groupe.Membres) == 0 {
				dns = append(dns, groupe.DN)
			}
		}
		if len(dns) > 0 {
			sort.Strings(dns)
			suggestions = append(suggestions, Suggestion{
				Type:    SuggestionGroupesVides,
				Action:  "supprimer",
				Gravite: GraviteAvertissement,
				Message: fmt.Sprintf("%d groupe(s) sans membre à supprimer.", len(dns)),
				Objets:  dns,
			})
		}
	}

	if options.OUsVides {
		for _, anomalie := range detecterOUsVides(inventaire) {
			suggestions = append(suggestions, Suggestion{
				Type:    SuggestionOUsVides,
				Action:  "supprimer",
				Gravite: GraviteInfo,
				Message: anomalie.Message,
				Objets:  anomalie.Objets,
			})
		}
	}

	if options.ComptesOrphelins {
		for _, anomalie := range detecterComptesOrphelins(inventaire) {
			suggestions = append(suggestions, Suggestion{
				Type:    SuggestionComptesOrphelins,
				Action:  "deplacer",
				Gravite: GraviteAvertissement,
				Message: anomalie.Message,
				Objets:  anomalie.Objets,
			})
		}
	}

	return suggestions
}

// GenererIdentifiantIntelligent génère un identifiant unique selon la charte de
// l'établissement. Si `format` est vide, le format configuré pour un élève est
// utilisé ; sinon il remplace ce format (préréglage ou modèle libre).
//
// Le second retour indique qu'un doublon a été résolu (l'identifiant de base
// était déjà pris).
func GenererIdentifiantIntelligent(cfg config.AppConfig, prenom, nom, format string,
	existants map[string]bool) (string, bool, error) {
	moteur := cfg.MoteurIdentifiants(models.AccountEleve)
	if strings.TrimSpace(format) != "" {
		moteur.Format = strings.TrimSpace(format)
	}
	return moteur.GenererUnique(prenom, nom, existants)
}

// parentDN retourne le DN parent (le DN privé de son premier RDN), en
// respectant les virgules échappées (`\,`) d'un RDN.
func parentDN(dn string) string {
	for i := 0; i < len(dn); i++ {
		if dn[i] != ',' {
			continue
		}
		if i > 0 && dn[i-1] == '\\' {
			continue
		}
		return strings.TrimSpace(dn[i+1:])
	}
	return ""
}

// ouDuCompte retourne l'OU d'un compte : le champ explicite s'il est renseigné,
// sinon la parente déduite du DN.
func ouDuCompte(compte Compte) string {
	if compte.OU != "" {
		return compte.OU
	}
	return parentDN(compte.DN)
}

// ouDuGroupe retourne l'OU d'un groupe.
func ouDuGroupe(groupe Groupe) string {
	if groupe.OU != "" {
		return groupe.OU
	}
	return parentDN(groupe.DN)
}

// trierAnomalies ordonne les anomalies pour une sortie stable.
func trierAnomalies(anomalies []Anomalie) {
	sort.SliceStable(anomalies, func(i, j int) bool {
		if anomalies[i].Type != anomalies[j].Type {
			return anomalies[i].Type < anomalies[j].Type
		}
		if rangGravite(anomalies[i].Gravite) != rangGravite(anomalies[j].Gravite) {
			return rangGravite(anomalies[i].Gravite) > rangGravite(anomalies[j].Gravite)
		}
		premierI, premierJ := "", ""
		if len(anomalies[i].Objets) > 0 {
			premierI = anomalies[i].Objets[0]
		}
		if len(anomalies[j].Objets) > 0 {
			premierJ = anomalies[j].Objets[0]
		}
		return premierI < premierJ
	})
}

// rangGravite retourne un rang numérique (plus grand = plus grave).
func rangGravite(gravite string) int {
	switch gravite {
	case GraviteCritique:
		return 3
	case GraviteAvertissement:
		return 2
	case GraviteInfo:
		return 1
	}
	return 0
}
