// Package config provides helpers for generating identifiers, passwords, and
// resolving duplicates according to the configured policies.
package config

import (
	"crypto/rand"
	"math/big"
	"regexp"
	"strings"
	"unicode"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// GenerateIdentifiant crée un identifiant selon le format configuré.
// format peut contenir : {P} = prénom, {N} = nom, {p} = première lettre prénom, {n} = première lettre nom.
func (c *AppConfig) GenerateIdentifiant(prenom, nom string, typeCompte models.AccountType) string {
	format := c.IdentifiantFormatEleve
	if typeCompte == models.AccountPersonnel {
		format = c.IdentifiantFormatPersonnel
	}

	// Normaliser : minuscules, pas d'accents, pas d'espaces
	p := normalizeIdentifiantPart(prenom, c.ReglePrenomCompose)
	n := normalizeIdentifiantPart(nom, c.ReglePrenomCompose)

	// Première lettre
	p1 := ""
	if len(p) > 0 {
		p1 = string(p[0])
	}
	n1 := ""
	if len(n) > 0 {
		n1 = string(n[0])
	}

	result := strings.ReplaceAll(format, "{P}", p)
	result = strings.ReplaceAll(result, "{N}", n)
	result = strings.ReplaceAll(result, "{p}", p1)
	result = strings.ReplaceAll(result, "{n}", n1)

	return strings.ToLower(result)
}

// normalizeIdentifiantPart nettoie une partie de l'identifiant (prénom ou nom).
func normalizeIdentifiantPart(s string, rule models.PrenomComposeRule) string {
	// Règle prénom composé
	parts := strings.FieldsFunc(s, func(r rune) bool { return r == '-' || r == ' ' })
	var result string
	switch rule {
	case models.PrenomPremierPrenom:
		if len(parts) > 0 {
			result = parts[0]
		}
	case models.PrenomConcatenation:
		for _, part := range parts {
			result += part
		}
	case models.PrenomTroncature:
		for _, part := range parts {
			if len(part) > 0 {
				result += string(part[0])
			}
		}
	default:
		if len(parts) > 0 {
			result = parts[0]
		}
	}

	// Supprimer accents et caractères spéciaux, garder alphanumérique
	var b strings.Builder
	for _, r := range result {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			// Mapping basique des accents
			switch r {
			case 'à', 'â', 'ä', 'á', 'ã', 'å':
				b.WriteRune('a')
			case 'é', 'è', 'ê', 'ë':
				b.WriteRune('e')
			case 'î', 'ï', 'í', 'ì':
				b.WriteRune('i')
			case 'ô', 'ö', 'ò', 'ó', 'õ':
				b.WriteRune('o')
			case 'ù', 'û', 'ü', 'ú':
				b.WriteRune('u')
			case 'ç':
				b.WriteRune('c')
			case 'ñ':
				b.WriteRune('n')
			default:
				b.WriteRune(unicode.ToLower(r))
			}
		}
	}
	return b.String()
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

// GenerateEmail génère une adresse mail selon le format configuré.
func (c *AppConfig) GenerateEmail(prenom, nom, domaine string) string {
	if domaine == "" {
		domaine = "example.com" // fallback
	}
	format := c.FormatMail
	if format == "" {
		format = "{p}.{n}"
	}

	p := normalizeIdentifiantPart(prenom, c.ReglePrenomCompose)
	n := normalizeIdentifiantPart(nom, c.ReglePrenomCompose)
	p1 := ""
	if len(p) > 0 {
		p1 = string(p[0])
	}
	n1 := ""
	if len(n) > 0 {
		n1 = string(n[0])
	}

	result := strings.ReplaceAll(format, "{P}", p)
	result = strings.ReplaceAll(result, "{N}", n)
	result = strings.ReplaceAll(result, "{p}", p1)
	result = strings.ReplaceAll(result, "{n}", n1)

	return strings.ToLower(result) + "@" + domaine
}

// ResolveDuplicate résout un conflit d'identifiant selon la règle configurée.
// Retourne l'identifiant résolu et un booléen indiquant s'il y a eu résolution.
func (c *AppConfig) ResolveDuplicate(base string, existing []string, rule models.DoublonRule) (string, bool) {
	if !contains(existing, base) {
		return base, false
	}

	switch rule {
	case models.DoublonSuffixeNumerique:
		return resolveNumericSuffix(base, existing, false)
	case models.DoublonSuffixeNumeriqueSep:
		return resolveNumericSuffix(base, existing, true)
	case models.DoublonPrefixeNumerique:
		return resolveNumericPrefix(base, existing)
	case models.DoublonLettresPrenom:
		// Nécessite le prénom - pas implémenté ici sans contexte
		return resolveNumericSuffix(base, existing, false)
	case models.DoublonLettresNom:
		return resolveNumericSuffix(base, existing, false)
	case models.DoublonAnneeSuffixe:
		// Nécessite l'année - pas implémenté ici sans contexte
		return resolveNumericSuffix(base, existing, false)
	default:
		return resolveNumericSuffix(base, existing, false)
	}
}

func resolveNumericSuffix(base string, existing []string, withSeparator bool) (string, bool) {
	sep := ""
	if withSeparator {
		sep = "."
	}
	for i := 2; i < 1000; i++ {
		candidate := base + sep + string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10))
		if !contains(existing, candidate) {
			return candidate, true
		}
	}
	return base, false
}

func resolveNumericPrefix(base string, existing []string) (string, bool) {
	for i := 2; i < 1000; i++ {
		candidate := string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10)) + "." + base
		if !contains(existing, candidate) {
			return candidate, true
		}
	}
	return base, false
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
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
