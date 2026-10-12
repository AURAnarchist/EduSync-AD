package crypto

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// vecteurPython est une empreinte produite par la version Python
// (cryptography, AESGCM.encrypt avec nonce fixe) :
//
//	key   = bytes(range(32))
//	nonce = bytes(range(12))
//	plain = "EduSync AD 🔐 test"
//
// Si ce test casse, la compatibilité avec les données existantes est rompue.
const vecteurPython = "AAECAwQFBgcICQoLAmajSLyLoTvMBbd7Ln3oTfez9EBWKNaPCSd3yFtss/yp5Iv6"

const texteVecteur = "EduSync AD 🔐 test"

func cleDeTest() []byte {
	key := make([]byte, KeySize)
	for i := range key {
		key[i] = byte(i)
	}
	return key
}

func TestChiffrementDechiffrement(t *testing.T) {
	key := cleDeTest()

	token, err := Encrypt(key, "MotDePasse-Secrète-éà")
	if err != nil {
		t.Fatalf("chiffrement: %v", err)
	}
	if token == "MotDePasse-Secrète-éà" {
		t.Fatal("le texte clair ne doit jamais être renvoyé tel quel")
	}

	plain, err := Decrypt(key, token)
	if err != nil {
		t.Fatalf("déchiffrement: %v", err)
	}
	if plain != "MotDePasse-Secrète-éà" {
		t.Errorf("round-trip = %q", plain)
	}
}

// TestCompatibilitePython prouve qu'une empreinte fabriquée par Python se
// déchiffre en Go.
func TestCompatibilitePython(t *testing.T) {
	plain, err := Decrypt(cleDeTest(), vecteurPython)
	if err != nil {
		t.Fatalf("déchiffrement d'un vecteur Python: %v", err)
	}
	if plain != texteVecteur {
		t.Errorf("texte = %q, attendu %q", plain, texteVecteur)
	}
}

func TestNonceUnique(t *testing.T) {
	key := cleDeTest()
	a, err := Encrypt(key, "identique")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encrypt(key, "identique")
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("deux chiffrements du même texte doivent différer (nonce aléatoire)")
	}
	rawA, _ := base64.StdEncoding.DecodeString(a)
	rawB, _ := base64.StdEncoding.DecodeString(b)
	if string(rawA[:NonceSize]) == string(rawB[:NonceSize]) {
		t.Error("les nonces doivent être distincts")
	}
}

func TestRejetCleInvalide(t *testing.T) {
	if _, err := Encrypt([]byte("trop-court"), "x"); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("attendu ErrInvalidKey, obtenu %v", err)
	}
	if _, err := Decrypt(make([]byte, 16), vecteurPython); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("attendu ErrInvalidKey, obtenu %v", err)
	}
}

func TestRejetEmpreinteAbimee(t *testing.T) {
	key := cleDeTest()

	if _, err := Decrypt(key, "!!pas-du-base64!!"); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("attendu ErrInvalidToken, obtenu %v", err)
	}
	if _, err := Decrypt(key, base64.StdEncoding.EncodeToString([]byte("court"))); !errors.Is(err, ErrInvalidToken) {
		t.Errorf("attendu ErrInvalidToken pour une empreinte tronquée, obtenu %v", err)
	}
}

// TestChiffrementSansMauvaiseCle : une clé différente ne doit jamais
// déchiffrer (et surtout pas renvoyer une donnée corrompue).
func TestChiffrementSansMauvaiseCle(t *testing.T) {
	token, err := Encrypt(cleDeTest(), "secret")
	if err != nil {
		t.Fatal(err)
	}
	autreCle := make([]byte, KeySize)
	autreCle[0] = 1
	if _, err := Decrypt(autreCle, token); !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("attendu ErrDecryptFailed, obtenu %v", err)
	}
}

// TestDonneesAlterees : la modification d'un seul octet doit être détectée
// (authentification GCM).
func TestDonneesAlterees(t *testing.T) {
	key := cleDeTest()
	token, err := Encrypt(key, "données sensibles")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		t.Fatal(err)
	}
	raw[len(raw)-1] ^= 0xFF

	if _, err := Decrypt(key, base64.StdEncoding.EncodeToString(raw)); !errors.Is(err, ErrDecryptFailed) {
		t.Errorf("attendu ErrDecryptFailed après altération, obtenu %v", err)
	}
}

func TestCleCreeeUneSeuleFois(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")

	premiere, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	if len(premiere) != KeySize {
		t.Fatalf("taille de clé = %d, attendu %d", len(premiere), KeySize)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if perm := info.Mode().Perm(); perm&0o077 != 0 {
			t.Errorf("la clé doit être privée (0600), permissions = %o", perm)
		}
	}

	seconde, err := LoadOrCreateKey(path)
	if err != nil {
		t.Fatalf("relecture: %v", err)
	}
	if string(seconde) != string(premiere) {
		t.Error("une clé existante ne doit jamais être régénérée")
	}
}

func TestCleCorrompue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secret.key")
	if err := os.WriteFile(path, []byte("trop-court"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadOrCreateKey(path); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("attendu ErrInvalidKey, obtenu %v", err)
	}
}

func TestCheminVide(t *testing.T) {
	if _, err := LoadOrCreateKey(""); err == nil {
		t.Error("un chemin vide doit être refusé")
	}
}

// TestFormatEstBase64Standard controle l'absence de variante URL-safe
// (Python utilise base64.b64encode : alphabet standard avec padding).
func TestFormatEstBase64Standard(t *testing.T) {
	token, err := Encrypt(cleDeTest(), strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(token, "-_") {
		t.Error("l'empreinte doit utiliser l'alphabet base64 standard")
	}
	if !strings.HasSuffix(token, "=") && len(token)%4 != 0 {
		t.Error("l'empreinte doit être paddée comme base64.b64encode")
	}
}
