package config

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// Bornes de longueur acceptées pour un mot de passe **choisi** par un
// utilisateur (elles ne s'appliquent pas aux mots de passe générés).
// Valeurs identiques à `core/passwords.py` (MIN_LENGTH / MAX_INPUT_LENGTH).
const (
	// MdpLongueurMin est la longueur minimale imposée quelle que soit la
	// politique configurée.
	MdpLongueurMin = 6
	// MdpLongueurMaxSaisie est la longueur maximale acceptée.
	MdpLongueurMaxSaisie = 128
)

// motifVarRe reconnaît les variables substituables d'un motif de mot de passe
// fixe — mêmes variables que le moteur d'identifiants (`core/identifiers.py`).
var motifVarRe = regexp.MustCompile(`\{(P|N|AN|ANNEE|p\d+|n\d+)\}`)

// caracteresTokenInterdits correspond à `_INVALID_CHARS_RE` côté Python : tout
// ce qui n'est ni minuscule ASCII, ni chiffre, ni « . », « _ » ou « - ».
var caracteresTokenInterdits = regexp.MustCompile(`[^a-z0-9._-]`)

// sansAccents remplace les lettres latines accentuées décomposables par leur
// base (équivalent de `strip_accents`, NFKD + suppression des diacritiques).
// Les lettres non décomposables (ø, đ, ł…) sont laissées telles quelles : comme
// côté Python, elles sont ensuite éliminées par le filtre de caractères.
func sansAccents(texte string) string {
	var b strings.Builder
	for _, r := range texte {
		if base, ok := tableAccents(r); ok {
			b.WriteString(base)
		} else {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tableAccents associe une lettre latine décomposable (minuscule) à sa base.
func tableAccents(r rune) (string, bool) {
	base, ok := accentsLatins[r]
	return base, ok
}

// accentsLatins ne contient que les lettres que NFKD décompose réellement en
// une lettre de base suivie de diacritiques — c'est ce que `strip_accents`
// supprime. Les lettres sans décomposition (ø, đ, ł, ħ, ı, ŧ) en sont donc
// volontairement absentes : `clean_token` les retire ensuite.
var accentsLatins = map[rune]string{
	'à': "a", 'á': "a", 'â': "a", 'ã': "a", 'ä': "a", 'å': "a", 'ā': "a", 'ă': "a", 'ą': "a",
	'ç': "c", 'ć': "c", 'ĉ': "c", 'ċ': "c", 'č': "c",
	'ď': "d",
	'è': "e", 'é': "e", 'ê': "e", 'ë': "e", 'ē': "e", 'ĕ': "e", 'ė': "e", 'ę': "e", 'ě': "e",
	'ĝ': "g", 'ğ': "g", 'ġ': "g", 'ģ': "g",
	'ĥ': "h",
	'ì': "i", 'í': "i", 'î': "i", 'ï': "i", 'ĩ': "i", 'ī': "i", 'ĭ': "i", 'į': "i",
	'ĵ': "j",
	'ķ': "k",
	'ĺ': "l", 'ļ': "l", 'ľ': "l", 'ŀ': "l",
	'ñ': "n", 'ń': "n", 'ņ': "n", 'ň': "n",
	'ò': "o", 'ó': "o", 'ô': "o", 'õ': "o", 'ö': "o", 'ō': "o", 'ŏ': "o", 'ő': "o",
	'ŕ': "r", 'ŗ': "r", 'ř': "r",
	'ś': "s", 'ŝ': "s", 'ş': "s", 'š': "s",
	'ţ': "t", 'ť': "t",
	'ù': "u", 'ú': "u", 'û': "u", 'ü': "u", 'ũ': "u", 'ū': "u", 'ŭ': "u", 'ů': "u", 'ű': "u", 'ų': "u",
	'ŵ': "w",
	'ý': "y", 'ÿ': "y", 'ŷ': "y",
	'ź': "z", 'ż': "z", 'ž': "z",
}

// CleanToken nettoie un prénom ou un nom comme `core/identifiers.py`:
// minuscules, sans accents, sans apostrophe ni espace, et privé de tout
// caractère hors « a-z0-9._- ».
func CleanToken(texte string) string {
	s := sansAccents(strings.ToLower(texte))
	s = strings.ReplaceAll(s, "'", "")
	s = strings.ReplaceAll(s, " ", "")
	return caracteresTokenInterdits.ReplaceAllString(s, "")
}

// RendreMotifMotDePasse substitue les variables d'identifiant dans un motif de
// mot de passe fixe (`pattern_fixe`). Contrairement au moteur d'identifiants,
// le motif conserve sa casse et ses caractères littéraux (ex. « Ecole{AN}! »
// donne « Ecole26! ») : seules les variables sont remplacées, après nettoyage.
func RendreMotifMotDePasse(motif, prenom, nom string, annee int) string {
	prenomNet := CleanToken(prenom)
	nomNet := CleanToken(nom)
	anneeTexte := strconv.Itoa(annee)
	if len(anneeTexte) > 2 {
		anneeTexte = anneeTexte[len(anneeTexte)-2:]
	}
	return motifVarRe.ReplaceAllStringFunc(motif, func(m string) string {
		token := motifVarRe.FindStringSubmatch(m)[1]
		switch {
		case token == "P":
			return prenomNet
		case token == "N":
			return nomNet
		case token == "ANNEE":
			return strconv.Itoa(annee)
		case token == "AN":
			return anneeTexte
		case strings.HasPrefix(token, "p"):
			return tronquerRunes(prenomNet, token[1:])
		case strings.HasPrefix(token, "n"):
			return tronquerRunes(nomNet, token[1:])
		}
		return m
	})
}

// RenderTemplate rend un gabarit d'identifiant ou d'adresse de messagerie :
// les variables {P} {N} {pN} {nN} {ANNEE} {AN} sont remplacées, puis le
// résultat est nettoyé (minuscules, sans accents ni caractères étrangers),
// comme `identifiers.render_template` côté Python.
func RenderTemplate(gabarit, prenom, nom string, annee int) string {
	return CleanToken(RendreMotifMotDePasse(gabarit, prenom, nom, annee))
}

// tronquerRunes garde les n premiers caractères (équivalent de `s[:n]` Python).
func tronquerRunes(s, nTexte string) string {
	n, err := strconv.Atoi(nTexte)
	if err != nil || n < 0 {
		return s
	}
	runes := []rune(s)
	if n >= len(runes) {
		return s
	}
	return string(runes[:n])
}

// GenererMotDePasse applique la politique de mot de passe : un motif fixe est
// rendu avec les variables d'identifiant s'il est renseigné, sinon un mot de
// passe aléatoire est généré. Équivalent de `passwords.generate_password`.
func (c *AppConfig) GenererMotDePasse(policy models.PasswordPolicy, prenom, nom string) (string, error) {
	if policy.PatternFixe != "" {
		return RendreMotifMotDePasse(policy.PatternFixe, prenom, nom, time.Now().Year()), nil
	}
	return c.GeneratePassword(policy)
}

// ValiderMotDePasse contrôle un mot de passe choisi par un utilisateur et
// retourne la liste des exigences non satisfaites (vide = valide), en
// français. Le motif fixe n'entre pas en compte : il ne sert qu'à *générer*
// des mots de passe. Les valeurs `interdits` (identifiant, nom…) sont
// comparées sans tenir compte de la casse.
// Équivalent de `passwords.validate_password`.
func ValiderMotDePasse(policy models.PasswordPolicy, motDePasse string, interdits ...string) []string {
	if motDePasse == "" {
		return []string{"Le mot de passe est obligatoire."}
	}

	var erreurs []string
	minimum := MdpLongueurMin
	if policy.Longueur > minimum {
		minimum = policy.Longueur
	}
	longueur := utf8.RuneCountInString(motDePasse)
	if longueur < minimum {
		erreurs = append(erreurs, fmt.Sprintf(
			"Le mot de passe doit contenir au moins %d caractères.", minimum))
	}
	if longueur > MdpLongueurMaxSaisie {
		erreurs = append(erreurs, fmt.Sprintf(
			"Le mot de passe ne doit pas dépasser %d caractères.", MdpLongueurMaxSaisie))
	}
	if policy.Majuscules && !contientRune(motDePasse, unicode.IsUpper) {
		erreurs = append(erreurs, "Le mot de passe doit contenir au moins une majuscule.")
	}
	if policy.Chiffres && !contientRune(motDePasse, unicode.IsDigit) {
		erreurs = append(erreurs, "Le mot de passe doit contenir au moins un chiffre.")
	}
	if policy.CaracteresSpeciaux && !contientRune(motDePasse, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		erreurs = append(erreurs, "Le mot de passe doit contenir au moins un caractère spécial.")
	}

	minuscule := strings.ToLower(motDePasse)
	for _, candidat := range interdits {
		if strings.TrimSpace(candidat) != "" &&
			minuscule == strings.ToLower(strings.TrimSpace(candidat)) {
			erreurs = append(erreurs,
				"Le mot de passe ne doit pas être identique à votre identifiant.")
			break
		}
	}
	return erreurs
}

// contientRune indique si la chaîne contient au moins un caractère satisfaisant
// le prédicat.
func contientRune(texte string, predicat func(rune) bool) bool {
	for _, r := range texte {
		if predicat(r) {
			return true
		}
	}
	return false
}
