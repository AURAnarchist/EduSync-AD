package compliance

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"strings"
	"time"
)

// Paramètres TOTP (RFC 6238), identiques à ceux des applications
// d'authentification courantes (Google Authenticator, Authy, FreeOTP) : SHA-1,
// pas de 30 secondes, 6 chiffres.
const (
	// TOTPPeriode est le pas de temps d'un code.
	TOTPPeriode = 30 * time.Second
	// TOTPChiffres est le nombre de chiffres d'un code.
	TOTPChiffres = 6
	// TOTPSecretOctets est la taille du secret aléatoire (160 bits, la
	// recommandation RFC 4226 / RFC 6238).
	TOTPSecretOctets = 20
	// TOTPTolerance est le nombre de pas acceptés avant et après l'instant
	// courant (dérive d'horloge tolérée).
	TOTPTolerance = 1
	// NombreCodesSecours est le nombre de codes de secours générés.
	NombreCodesSecours = 10
)

// encodageBase32 est l'encodage sans remplissage utilisé par les applications
// d'authentification pour les secrets TOTP.
var encodageBase32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenererSecretTOTP génère un secret TOTP aléatoire encodé en base32 (sans
// remplissage), directement saisissable dans une application
// d'authentification.
func GenererSecretTOTP() (string, error) {
	brut := make([]byte, TOTPSecretOctets)
	if _, err := io.ReadFull(rand.Reader, brut); err != nil {
		return "", fmt.Errorf("compliance: génération du secret TOTP: %w", err)
	}
	return encodageBase32.EncodeToString(brut), nil
}

// DecoderSecretTOTP décode un secret base32 (tolérant à la casse, aux espaces
// et au remplissage `=`).
func DecoderSecretTOTP(secret string) ([]byte, error) {
	nettoye := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(secret), " ", ""))
	nettoye = strings.TrimRight(nettoye, "=")
	brut, err := encodageBase32.DecodeString(nettoye)
	if err != nil {
		return nil, fmt.Errorf("compliance: secret TOTP illisible: %w", err)
	}
	if len(brut) == 0 {
		return nil, fmt.Errorf("compliance: secret TOTP vide")
	}
	return brut, nil
}

// CodeTOTP retourne le code TOTP valable à l'instant donné.
func CodeTOTP(secret string, moment time.Time) (string, error) {
	brut, err := DecoderSecretTOTP(secret)
	if err != nil {
		return "", err
	}
	return codeTOTPPourCompteur(brut, compteurTOTP(moment), TOTPChiffres)
}

// VerifierTOTP compare un code à celui attendu, en tolérant `tolerance` pas de
// temps avant et après l'instant courant. La comparaison est à temps constant.
func VerifierTOTP(secret, code string, moment time.Time, tolerance int) (bool, error) {
	brut, err := DecoderSecretTOTP(secret)
	if err != nil {
		return false, err
	}
	code = strings.TrimSpace(code)
	if len(code) != TOTPChiffres {
		return false, nil
	}
	compteur := compteurTOTP(moment)
	for delta := -tolerance; delta <= tolerance; delta++ {
		// Un compteur négatif n'a pas de sens (avant l'époque Unix).
		if delta < 0 && compteur < uint64(-delta) {
			continue
		}
		attendu, err := codeTOTPPourCompteur(brut, compteur+uint64(delta), TOTPChiffres)
		if err != nil {
			return false, err
		}
		if subtle.ConstantTimeCompare([]byte(attendu), []byte(code)) == 1 {
			return true, nil
		}
	}
	return false, nil
}

// compteurTOTP retourne le numéro de pas de temps (T) d'un instant.
func compteurTOTP(moment time.Time) uint64 {
	return uint64(moment.Unix() / int64(TOTPPeriode/time.Second))
}

// codeTOTPPourCompteur implémente HOTP (RFC 4226), la brique de TOTP.
//
// Le compteur est encodé en 8 octets big-endian, signé avec HMAC-SHA1, puis
// tronqué dynamiquement (offset = 4 bits de poids faible du dernier octet).
func codeTOTPPourCompteur(secret []byte, compteur uint64, chiffres int) (string, error) {
	if len(secret) == 0 {
		return "", fmt.Errorf("compliance: secret TOTP vide")
	}
	var tampon [8]byte
	binary.BigEndian.PutUint64(tampon[:], compteur)

	mac := hmac.New(sha1.New, secret)
	if _, err := mac.Write(tampon[:]); err != nil {
		return "", fmt.Errorf("compliance: calcul HMAC: %w", err)
	}
	somme := mac.Sum(nil)

	offset := somme[len(somme)-1] & 0x0f
	binaire := (uint32(somme[offset])&0x7f)<<24 |
		uint32(somme[offset+1])<<16 |
		uint32(somme[offset+2])<<8 |
		uint32(somme[offset+3])

	modulo := uint32(1)
	for i := 0; i < chiffres; i++ {
		modulo *= 10
	}
	return fmt.Sprintf("%0*d", chiffres, binaire%modulo), nil
}

// GenererCodesSecours génère `NombreCodesSecours` codes de secours et leurs
// empreintes SHA-256. Les codes en clair ne sont retournés qu'une seule fois :
// seules les empreintes sont persistées.
func GenererCodesSecours() (codes []string, empreintes []string, err error) {
	codes = make([]string, 0, NombreCodesSecours)
	empreintes = make([]string, 0, NombreCodesSecours)
	for i := 0; i < NombreCodesSecours; i++ {
		code, err := codeSecoursAleatoire()
		if err != nil {
			return nil, nil, err
		}
		codes = append(codes, code)
		empreintes = append(empreintes, EmpreinteCodeSecours(code))
	}
	return codes, empreintes, nil
}

// codeSecoursAleatoire retourne un code de secours de la forme « xxxx-xxxx »
// (8 caractères de l'alphabet Crockford base32, sans caractères ambigus).
func codeSecoursAleatoire() (string, error) {
	const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	octets := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, octets); err != nil {
		return "", fmt.Errorf("compliance: génération d'un code de secours: %w", err)
	}
	var b strings.Builder
	for i, octet := range octets {
		if i == 4 {
			b.WriteByte('-')
		}
		b.WriteByte(alphabet[int(octet)%len(alphabet)])
	}
	return b.String(), nil
}

// EmpreinteCodeSecours retourne l'empreinte SHA-256 (hexadécimale) d'un code de
// secours, telle qu'elle est stockée dans la configuration.
func EmpreinteCodeSecours(code string) string {
	normalise := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), "-", ""))
	somme := sha256.Sum256([]byte(normalise))
	return hex.EncodeToString(somme[:])
}

// VerifierCodeSecours compare un code de secours aux empreintes enregistrées et
// retourne l'index de l'empreinte correspondante (-1 si aucune). La comparaison
// est à temps constant, et un code ne peut pas être rejoué : l'appelant doit
// retirer l'empreinte consommée.
func VerifierCodeSecours(code string, empreintes []string) int {
	empreinte := EmpreinteCodeSecours(code)
	for index, connue := range empreintes {
		if subtle.ConstantTimeCompare([]byte(empreinte), []byte(connue)) == 1 {
			return index
		}
	}
	return -1
}
