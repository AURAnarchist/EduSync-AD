// Ce fichier fournit les helpers de génération d'identifiants, de mots de
// passe et d'adresses mail, ainsi que la résolution des doublons. Le moteur
// d'identifiants lui-même (formats prédéfinis, modèles libres, règles de
// doublons) est dans `identifiers.go`.
package config

import (
	"crypto/rand"
	"math/big"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// MoteurIdentifiants retourne le moteur d'identifiants configuré pour un type
// de compte : format élève ou personnel, règle de doublons, règle de prénom
// composé et année courante.
func (c *AppConfig) MoteurIdentifiants(typeCompte models.AccountType) MoteurIdentifiants {
	format := c.IdentifiantFormatEleve
	if typeCompte == models.AccountPersonnel {
		format = c.IdentifiantFormatPersonnel
	}
	return MoteurIdentifiants{
		Format:             format,
		RegleDoublon:       c.RegleDoublons,
		ReglePrenomCompose: c.ReglePrenomCompose,
		Annee:              time.Now().Year(),
	}
}

// GenerateIdentifiant crée un identifiant selon le format configuré, sans
// résolution de doublon. Pour tenir compte des identifiants déjà pris, utiliser
// `MoteurIdentifiants(...).GenererUnique(...)`.
func (c *AppConfig) GenerateIdentifiant(prenom, nom string, typeCompte models.AccountType) string {
	return c.MoteurIdentifiants(typeCompte).IdentifiantBase(prenom, nom)
}

// GenerateEmail génère une adresse mail selon `format_mail` : la partie locale
// passe par le même moteur de modèles que les identifiants ({P}, {N}, {p1},
// {n1}, {AN}, {ANNEE}), le prénom composé étant d'abord réduit par la règle
// configurée.
func (c *AppConfig) GenerateEmail(prenom, nom, domaine string) string {
	modele := c.FormatMail
	if modele == "" {
		modele = "{P}.{N}"
	}
	prenomNet := CleanToken(AppliquerReglePrenomCompose(prenom, c.ReglePrenomCompose))
	nomNet := CleanToken(nom)
	locale := RenderTemplate(modele, prenomNet, nomNet, time.Now().Year())
	return locale + "@" + domaine
}

// ResolveDuplicate résout un conflit sur une valeur déjà calculée (identifiant
// ou partie locale d'une adresse mail) selon la règle configurée.
//
// La limite de 20 caractères du sAMAccountName ne s'applique pas ici : elle est
// appliquée en amont par le moteur d'identifiants, et les adresses mail n'y
// sont pas soumises. Les règles « lettres » retombent sur un suffixe numérique,
// faute de prénom/nom pour allonger un préfixe (même repli que Python).
func (c *AppConfig) ResolveDuplicate(base string, existants []string, regle models.DoublonRule) (string, bool) {
	index := make(map[string]bool, len(existants))
	for _, valeur := range existants {
		index[strings.ToLower(valeur)] = true
	}
	if !index[strings.ToLower(base)] {
		return base, false
	}

	candidats := make([]string, 0, 1000)
	if regle == models.DoublonAnneeSuffixe {
		candidats = append(candidats, base+strconv.Itoa(time.Now().Year()))
	}
	for n := 2; n < 1000; n++ {
		switch regle {
		case models.DoublonPrefixeNumerique:
			candidats = append(candidats, strconv.Itoa(n)+"."+base)
		case models.DoublonSuffixeNumeriqueSep:
			candidats = append(candidats, base+"-"+strconv.Itoa(n))
		default:
			candidats = append(candidats, base+strconv.Itoa(n))
		}
	}
	for _, candidat := range candidats {
		if !index[strings.ToLower(candidat)] {
			return candidat, true
		}
	}
	return base, false
}

// GeneratePassword génère un mot de passe selon la politique.
func (c *AppConfig) GeneratePassword(policy models.PasswordPolicy) (string, error) {
	const (
		lower     = "abcdefghijklmnopqrstuvwxyz"
		upper     = "ABCDEFGHIJKLMNOPQRSTUVWXYZ"
		digits    = "0123456789"
		special   = "!@#$%^&*"
		ambiguous = "Il1O0"
	)

	var charset strings.Builder
	var required []string

	charset.WriteString(lower)
	if policy.Majuscules {
		charset.WriteString(upper)
		required = append(required, upper)
	}
	if policy.Chiffres {
		charset.WriteString(digits)
		required = append(required, digits)
	}
	if policy.CaracteresSpeciaux {
		charset.WriteString(special)
		required = append(required, special)
	}

	chars := charset.String()
	if len(chars) == 0 {
		chars = lower
	}

	// Longueur minimale
	length := policy.Longueur
	if length < 8 {
		length = 8
	}
	if length > 64 {
		length = 64
	}

	// Générer en s'assurant d'avoir au moins un de chaque requis
	var password strings.Builder
	for _, req := range required {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(req))))
		password.WriteByte(req[idx.Int64()])
	}

	// Compléter
	for password.Len() < length {
		idx, _ := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		c := chars[idx.Int64()]
		// Éviter caractères ambigus
		if strings.ContainsRune(ambiguous, rune(c)) {
			continue
		}
		password.WriteByte(c)
	}

	// Mélanger (Fisher-Yates)
	pw := []rune(password.String())
	for i := len(pw) - 1; i > 0; i-- {
		j, _ := rand.Int(rand.Reader, big.NewInt(int64(i+1)))
		pw[i], pw[j.Int64()] = pw[j.Int64()], pw[i]
	}

	return string(pw), nil
}

// SanitizeDNPart nettoie une chaîne pour être utilisée dans un DN (CN, OU, etc.).
var dnInvalidChars = regexp.MustCompile(`[<>,;=+"]`)

func SanitizeDNPart(s string) string {
	s = strings.TrimSpace(s)
	s = dnInvalidChars.ReplaceAllString(s, "")
	return s
}

// BuildUserDN construit le DN complet d'un utilisateur.
func BuildUserDN(cn, ou string) string {
	return "CN=" + SanitizeDNPart(cn) + "," + ou
}

// DomaineToBaseDN convertit "EDUSYNC.LOCAL" → "DC=EDUSYNC,DC=LOCAL".
func DomaineToBaseDN(domaine string) string {
	parts := strings.Split(domaine, ".")
	var dns []string
	for _, p := range parts {
		dns = append(dns, "DC="+p)
	}
	return strings.Join(dns, ",")
}
