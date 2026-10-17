package portal

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// TestDeriverCodeParitePython verrouille l'accord entre PBKDF2-HMAC-SHA256 Go
// et `hashlib.pbkdf2_hmac("sha256", code, sel, 60000)` : le vecteur ci-dessous
// a été calculé avec la version Python du projet.
func TestDeriverCodeParitePython(t *testing.T) {
	sel := make([]byte, 16)
	for i := range sel {
		sel[i] = byte(i)
	}
	attendu := "407cf2510c0ab6009daf6b18f2edfccf545287211d428264da036fb73d5361a7"

	got := deriverCode("123456", sel)
	if hex.EncodeToString(got) != attendu {
		t.Fatalf("deriverCode = %s, attendu %s", hex.EncodeToString(got), attendu)
	}
	if len(got) != sha256.Size {
		t.Fatalf("longueur = %d, attendu %d", len(got), sha256.Size)
	}
}

// TestPbkdf2SHA256VecteurComplementaire vérifie une seconde dérivation (sel et
// code différents), elle aussi calculée avec la version Python.
func TestPbkdf2SHA256VecteurComplementaire(t *testing.T) {
	attendu := "95abefbc37c2569f135a34dc49f1c883f900c7663173cbf4c83eb8839c81e219"
	got := deriverCode("000000", []byte("edusync"))
	if hex.EncodeToString(got) != attendu {
		t.Fatalf("deriverCode = %s, attendu %s", hex.EncodeToString(got), attendu)
	}
}

func TestChiffresAleatoires(t *testing.T) {
	for i := 0; i < 20; i++ {
		code, err := chiffresAleatoires(LongueurCode)
		if err != nil {
			t.Fatalf("chiffresAleatoires: %v", err)
		}
		if len(code) != LongueurCode {
			t.Fatalf("code = %q (longueur %d)", code, len(code))
		}
		for _, r := range code {
			if r < '0' || r > '9' {
				t.Fatalf("code non numérique : %q", code)
			}
		}
	}
}

func TestIdentifiantAleatoireHex(t *testing.T) {
	for _, n := range []int{8, 12} {
		valeur, err := identifiantAleatoireHex(n)
		if err != nil {
			t.Fatalf("identifiantAleatoireHex(%d): %v", n, err)
		}
		if len(valeur) != n {
			t.Fatalf("longueur = %d, attendu %d (%q)", len(valeur), n, valeur)
		}
	}
}
