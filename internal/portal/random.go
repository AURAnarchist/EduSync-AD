package portal

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"math/big"
)

// octetsAleatoires retourne n octets tirés au hasard de façon
// cryptographique (équivalent de `secrets.token_bytes`).
func octetsAleatoires(n int) ([]byte, error) {
	octets := make([]byte, n)
	if _, err := rand.Read(octets); err != nil {
		return nil, err
	}
	return octets, nil
}

// entierAleatoire retourne un entier uniforme dans [0, limite).
func entierAleatoire(limite int) (int, error) {
	valeur, err := rand.Int(rand.Reader, big.NewInt(int64(limite)))
	if err != nil {
		return 0, err
	}
	return int(valeur.Int64()), nil
}

// empreinteEgale compare deux empreintes en temps constant
// (équivalent de `hmac.compare_digest`).
func empreinteEgale(attendue, stockee []byte) bool {
	return hmac.Equal(attendue, stockee)
}

// deriverCode dérive un code de vérification avec PBKDF2-HMAC-SHA256, comme
// `hashlib.pbkdf2_hmac("sha256", code, sel, 60000)` côté Python. La dérivation
// est volontairement lente : un code de 6 chiffres ne représente que 10^6
// possibilités, le coût de PBKDF2 freine une attaque hors ligne sur la base.
func deriverCode(code string, sel []byte) []byte {
	return pbkdf2SHA256([]byte(code), sel, iterationsPBKDF2, sha256.Size)
}

// pbkdf2SHA256 est l'implémentation de référence de PBKDF2 (RFC 2898) sur
// HMAC-SHA256 : elle reproduit exactement la sortie de `hashlib.pbkdf2_hmac`.
// Un vecteur de test calculé avec la version Python verrouille cet accord
// (voir internal/portal/random_test.go).
func pbkdf2SHA256(motDePasse, sel []byte, iterations, longueurCle int) []byte {
	prf := hmac.New(sha256.New, motDePasse)
	tailleEmpreinte := prf.Size()
	nombreBlocs := (longueurCle + tailleEmpreinte - 1) / tailleEmpreinte

	var compteur [4]byte
	cle := make([]byte, 0, nombreBlocs*tailleEmpreinte)
	u := make([]byte, tailleEmpreinte)

	for bloc := 1; bloc <= nombreBlocs; bloc++ {
		prf.Reset()
		prf.Write(sel)
		compteur[0] = byte(bloc >> 24)
		compteur[1] = byte(bloc >> 16)
		compteur[2] = byte(bloc >> 8)
		compteur[3] = byte(bloc)
		prf.Write(compteur[:4])
		cle = prf.Sum(cle)
		t := cle[len(cle)-tailleEmpreinte:]
		copy(u, t)

		for n := 2; n <= iterations; n++ {
			prf.Reset()
			prf.Write(u)
			u = u[:0]
			u = prf.Sum(u)
			for i := range u {
				t[i] ^= u[i]
			}
		}
	}
	return cle[:longueurCle]
}
