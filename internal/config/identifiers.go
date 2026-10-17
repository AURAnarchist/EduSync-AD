package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// ─── Moteur de génération d'identifiants (§4 du cahier des charges) ─────────
//
// Un seul moteur de modèles gère les formats prédéfinis (table p.5-6) et le
// format personnalisé libre. Les formats prédéfinis sont eux-mêmes des modèles
// (ex. « prenom.nom » → « {P}.{N} »), sauf « prenomNom » et « NomPrenom » qui
// utilisent de la casse (camelCase), non exprimable avec les variables
// documentées, et sont donc traités à part.
//
// Portage fidèle de `core/identifiers.py` : mêmes formats, mêmes règles de
// doublons, même limite de longueur du sAMAccountName.

// LongueurMaxIdentifiant est la limite du sAMAccountName (identifiant de
// connexion pré-Windows 2000). Elle vient de la base SAM d'Active Directory
// elle-même — contrainte historique non configurable : un identifiant plus
// long est refusé par le contrôleur de domaine avec une erreur peu explicite.
// On tronque donc ici plutôt que de laisser passer un format/nom trop long.
const LongueurMaxIdentifiant = 20

// PresetsIdentifiants liste les formats prédéfinis, dans l'ordre du cahier des
// charges (table « Génération des identifiants — Formats prédéfinis »).
var PresetsIdentifiants = []string{
	"prenom",
	"nom",
	"prenomnom",
	"nomprenom",
	"prenom.nom",
	"nom.prenom",
	"p.nom",
	"nom.p",
	"pnom",
	"nomp",
	"pp.nom",
	"ppp.nom",
	"nom.pp",
	"nom.ppp",
	"prenom.nnn",
	"prenom_nom",
	"nom_prenom",
	"prenomNom",
	"NomPrenom",
}

// PresetsIdentifiantsCamel liste les formats prédéfinis à casse camelCase.
var PresetsIdentifiantsCamel = []string{"prenomNom", "NomPrenom"}

// modelesIdentifiants associe chaque format prédéfini à son modèle.
var modelesIdentifiants = map[string]string{
	"prenom":     "{P}",
	"nom":        "{N}",
	"prenomnom":  "{P}{N}",
	"nomprenom":  "{N}{P}",
	"prenom.nom": "{P}.{N}",
	"nom.prenom": "{N}.{P}",
	"p.nom":      "{p1}.{N}",
	"nom.p":      "{N}.{p1}",
	"pnom":       "{p1}{N}",
	"nomp":       "{N}{p1}",
	"pp.nom":     "{p2}.{N}",
	"ppp.nom":    "{p3}.{N}",
	"nom.pp":     "{N}.{p2}",
	"nom.ppp":    "{N}.{p3}",
	"prenom.nnn": "{P}.{n3}",
	"prenom_nom": "{P}_{N}",
	"nom_prenom": "{N}_{P}",
}

// presetsCamelIndex indexe les formats camelCase pour un test en O(1).
var presetsCamelIndex = func() map[string]bool {
	index := make(map[string]bool, len(PresetsIdentifiantsCamel))
	for _, preset := range PresetsIdentifiantsCamel {
		index[preset] = true
	}
	return index
}()

// separatorPrenomCompose découpe un prénom composé sur les tirets et les
// espaces (plusieurs séparateurs consécutifs comptent pour un seul).
var separatorPrenomCompose = regexp.MustCompile(`[-\s]+`)

// accentsLatinsMaj dérive les majuscules accentuées de `accentsLatins` : comme
// côté Python (`strip_accents` passe par NFKD), la correspondance est la même
// quelle que soit la casse.
var accentsLatinsMaj = func() map[rune]string {
	table := make(map[rune]string, len(accentsLatins))
	for lettre, base := range accentsLatins {
		majuscule := unicode.ToUpper(lettre)
		if majuscule != lettre {
			table[majuscule] = strings.ToUpper(base)
		}
	}
	return table
}()

// StripAccents remplace les lettres latines accentuées décomposables par leur
// base, en conservant la casse (équivalent de `strip_accents`).
func StripAccents(texte string) string {
	var b strings.Builder
	for _, r := range texte {
		switch {
		case accentsLatins[r] != "":
			b.WriteString(accentsLatins[r])
		case accentsLatinsMaj[r] != "":
			b.WriteString(accentsLatinsMaj[r])
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// AppliquerReglePrenomCompose applique la règle de prénom composé sur le prénom
// brut (avant nettoyage).
func AppliquerReglePrenomCompose(prenomBrut string, regle models.PrenomComposeRule) string {
	parties := []string{}
	for _, partie := range separatorPrenomCompose.Split(strings.TrimSpace(prenomBrut), -1) {
		if partie != "" {
			parties = append(parties, partie)
		}
	}
	if len(parties) == 0 {
		return prenomBrut
	}
	if regle == models.PrenomConcatenation {
		return strings.Join(parties, "")
	}
	// Premier prénom et troncature aboutissent tous deux à ne garder que ce qui
	// précède le premier séparateur (tiret ou espace).
	return parties[0]
}

// rendreTemplate rend un modèle en forçant éventuellement la longueur des
// préfixes {pN} (override == "p") ou {nN} (override == "n").
func rendreTemplate(motif, prenom, nom string, annee int, override string, longueur int) string {
	rendu := motifVarRe.ReplaceAllStringFunc(motif, func(motifTrouve string) string {
		token := motifVarRe.FindStringSubmatch(motifTrouve)[1]
		switch {
		case token == "P":
			return prenom
		case token == "N":
			return nom
		case token == "ANNEE":
			return strconv.Itoa(annee)
		case token == "AN":
			return deuxDerniersChiffres(strconv.Itoa(annee))
		case strings.HasPrefix(token, "p"):
			return premiersCaracteres(prenom, longueurPrefixe(token, "p", override, longueur))
		case strings.HasPrefix(token, "n"):
			return premiersCaracteres(nom, longueurPrefixe(token, "n", override, longueur))
		}
		return motifTrouve
	})
	return CleanToken(rendu)
}

// longueurPrefixe calcule la longueur effective d'un préfixe {pN}/{nN}.
func longueurPrefixe(token, famille, override string, longueur int) int {
	if override == famille {
		return longueur
	}
	valeur, err := strconv.Atoi(token[1:])
	if err != nil || valeur < 0 {
		return 0
	}
	return valeur
}

// deuxDerniersChiffres retourne les deux derniers caractères d'une année.
func deuxDerniersChiffres(annee string) string {
	if len(annee) > 2 {
		return annee[len(annee)-2:]
	}
	return annee
}

// premiersCaracteres retourne au plus `longueur` premiers caractères.
func premiersCaracteres(texte string, longueur int) string {
	if longueur <= 0 {
		return ""
	}
	runes := []rune(texte)
	if len(runes) <= longueur {
		return texte
	}
	return string(runes[:longueur])
}

// tronquerIdentifiant ramène un identifiant à la limite SAM.
func tronquerIdentifiant(identifiant string) string {
	if len(identifiant) <= LongueurMaxIdentifiant {
		return identifiant
	}
	return identifiant[:LongueurMaxIdentifiant]
}

// tronquerAvecSuffixe tronque `base` (pas le suffixe) pour que `base+suffixe`
// tienne dans la limite : un suffixe de désambiguïsation ne doit jamais être
// coupé par une troncature naïve en fin de chaîne, ce qui annulerait la
// résolution du doublon qu'il est censé apporter.
func tronquerAvecSuffixe(base, suffixe string) string {
	place := LongueurMaxIdentifiant - len(suffixe)
	if place < 1 {
		return tronquerIdentifiant(suffixe)
	}
	return base[:min(place, len(base))] + suffixe
}

// majusculePremiere met en majuscule le premier caractère (chaîne vide sûre).
func majusculePremiere(texte string) string {
	if texte == "" {
		return ""
	}
	return strings.ToUpper(texte[:1]) + texte[1:]
}

// identifiantCamel applique les formats camelCase (« prenomNom », « NomPrenom »).
func identifiantCamel(format, prenom, nom string) string {
	switch format {
	case "prenomNom":
		return prenom + majusculePremiere(nom)
	case "NomPrenom":
		return majusculePremiere(nom) + majusculePremiere(prenom)
	}
	return prenom + nom
}

// MoteurIdentifiants génère les identifiants selon un format et une règle de
// doublons. Les champs laissés à zéro prennent les valeurs de Python :
// règle de doublons « suffixe_numerique », règle prénom composé
// « premier_prenom », séparateur « - », année courante.
type MoteurIdentifiants struct {
	Format             string
	RegleDoublon       models.DoublonRule
	ReglePrenomCompose models.PrenomComposeRule
	SeparateurDoublon  string
	Annee              int
}

// ReglePrenomComposeEffective retourne la règle de prénom composé appliquée.
func (m MoteurIdentifiants) ReglePrenomComposeEffective() models.PrenomComposeRule {
	if m.ReglePrenomCompose == "" {
		return models.PrenomPremierPrenom
	}
	return m.ReglePrenomCompose
}

// regleDoublon retourne la règle de doublons appliquée.
func (m MoteurIdentifiants) regleDoublon() models.DoublonRule {
	if m.RegleDoublon == "" {
		return models.DoublonSuffixeNumerique
	}
	return m.RegleDoublon
}

// separateur retourne le séparateur de doublon appliqué.
func (m MoteurIdentifiants) separateur() string {
	if m.SeparateurDoublon == "" {
		return "-"
	}
	return m.SeparateurDoublon
}

// anneeEffective retourne l'année utilisée par les variables {AN}/{ANNEE}.
func (m MoteurIdentifiants) anneeEffective() int {
	if m.Annee != 0 {
		return m.Annee
	}
	return time.Now().Year()
}

// estCamel indique si le format est un préréglage camelCase.
func (m MoteurIdentifiants) estCamel() bool {
	return presetsCamelIndex[m.Format]
}

// modele retourne le modèle effectif du format (le format lui-même s'il n'est
// pas un préréglage connu : format personnalisé libre).
func (m MoteurIdentifiants) modele() string {
	if modele, ok := modelesIdentifiants[m.Format]; ok {
		return modele
	}
	return m.Format
}

// parties nettoie le prénom (après règle de prénom composé) et le nom.
func (m MoteurIdentifiants) parties(prenomBrut, nomBrut string) (string, string) {
	prenom := CleanToken(AppliquerReglePrenomCompose(prenomBrut, m.ReglePrenomComposeEffective()))
	nom := CleanToken(nomBrut)
	return prenom, nom
}

// IdentifiantBase retourne l'identifiant sans résolution de doublon.
func (m MoteurIdentifiants) IdentifiantBase(prenomBrut, nomBrut string) string {
	prenom, nom := m.parties(prenomBrut, nomBrut)
	if m.estCamel() {
		return tronquerIdentifiant(identifiantCamel(m.Format, prenom, nom))
	}
	return tronquerIdentifiant(rendreTemplate(m.modele(), prenom, nom, m.anneeEffective(), "", 0))
}

// GenererUnique retourne (identifiant, doublonResolu) pour un usager, en
// évitant les identifiants déjà utilisés (existants, comparés sans casse).
func (m MoteurIdentifiants) GenererUnique(prenomBrut, nomBrut string,
	existants map[string]bool) (string, bool, error) {
	index := make(map[string]bool, len(existants))
	for identifiant := range existants {
		index[strings.ToLower(identifiant)] = true
	}

	base := m.IdentifiantBase(prenomBrut, nomBrut)
	if !index[strings.ToLower(base)] {
		return base, false, nil
	}

	prenom, nom := m.parties(prenomBrut, nomBrut)
	for _, candidat := range m.candidatsDoublon(base, prenom, nom) {
		if !index[strings.ToLower(candidat)] {
			return candidat, true, nil
		}
	}
	return "", false, fmt.Errorf(
		"config: impossible de générer un identifiant unique pour %s %s", prenomBrut, nomBrut)
}

// candidatsDoublon énumère les identifiants de remplacement selon la règle.
func (m MoteurIdentifiants) candidatsDoublon(base, prenom, nom string) []string {
	switch m.regleDoublon() {
	case models.DoublonSuffixeNumerique:
		return suffixesNumeriques(base)
	case models.DoublonSuffixeNumeriqueSep:
		candidats := make([]string, 0, 998)
		for n := 2; n < 1000; n++ {
			candidats = append(candidats, tronquerAvecSuffixe(base, m.separateur()+strconv.Itoa(n)))
		}
		return candidats
	case models.DoublonPrefixeNumerique:
		candidats := make([]string, 0, 998)
		for n := 2; n < 1000; n++ {
			// Préfixe : la troncature en fin de chaîne préserve le « {n}. » en
			// tête, pas besoin de tronquerAvecSuffixe ici.
			candidats = append(candidats, tronquerIdentifiant(strconv.Itoa(n)+"."+base))
		}
		return candidats
	case models.DoublonLettresPrenom:
		return append(m.candidatsLettres("p", base, prenom, nom), suffixesNumeriques(base)...)
	case models.DoublonLettresNom:
		return append(m.candidatsLettres("n", base, prenom, nom), suffixesNumeriques(base)...)
	case models.DoublonAnneeSuffixe:
		premier := tronquerAvecSuffixe(base, strconv.Itoa(m.anneeEffective()))
		return append([]string{premier}, suffixesNumeriques(base)...)
	}
	return suffixesNumeriques(base)
}

// suffixesNumeriques énumère base+« 2 », base+« 3 », …
func suffixesNumeriques(base string) []string {
	candidats := make([]string, 0, 998)
	for n := 2; n < 1000; n++ {
		candidats = append(candidats, tronquerAvecSuffixe(base, strconv.Itoa(n)))
	}
	return candidats
}

// candidatsLettres allonge le préfixe du prénom (« p ») ou du nom (« n ») du
// modèle, puis retombe sur les suffixes numériques si le modèle n'a pas de
// variable de préfixe correspondante.
func (m MoteurIdentifiants) candidatsLettres(famille, base, prenom, nom string) []string {
	motif := m.modele()
	if m.estCamel() {
		return nil
	}
	recherche := regexp.MustCompile(`\{` + famille + `(\d+)\}`)
	trouve := recherche.FindStringSubmatch(motif)
	if trouve == nil {
		return nil
	}
	debut, err := strconv.Atoi(trouve[1])
	if err != nil {
		return nil
	}
	maximum := len([]rune(nom))
	if famille == "p" {
		maximum = len([]rune(prenom))
	}

	candidats := []string{}
	for longueur := debut + 1; longueur <= maximum; longueur++ {
		candidat := tronquerIdentifiant(
			rendreTemplate(motif, prenom, nom, m.anneeEffective(), famille, longueur))
		if candidat != base {
			candidats = append(candidats, candidat)
		}
	}
	return candidats
}
