package compliance

import (
	"encoding/base32"
	"strings"
	"testing"
	"time"
)

// secretRFC6238 est le secret des vecteurs de test du RFC 6238 : les 20 octets
// ASCII « 12345678901234567890 ».
func secretRFC6238() []byte { return []byte("12345678901234567890") }

// TestCodeTOTPVecteursRFC6238 vérifie l'implémentation HOTP/TOTP contre les
// valeurs publiées dans le RFC 6238 (codes à 8 chiffres, HMAC-SHA1).
func TestCodeTOTPVecteursRFC6238(t *testing.T) {
	cas := []struct {
		compteur uint64
		attendu  string
	}{
		{1, "94287082"},
		{0x00000000023523EC, "07081804"},
		{0x00000000023523ED, "14050471"},
		{0x000000000273EF07, "89005924"},
		{0x0000000003F940AA, "69279037"},
		{0x0000000027BC86AA, "65353130"},
	}
	for _, c := range cas {
		obtenu, err := codeTOTPPourCompteur(secretRFC6238(), c.compteur, 8)
		if err != nil {
			t.Fatalf("compteur %d : %v", c.compteur, err)
		}
		if obtenu != c.attendu {
			t.Errorf("compteur %d : obtenu %s, attendu %s", c.compteur, obtenu, c.attendu)
		}
	}
}

func TestCodeTOTPPourCompteurSecretVide(t *testing.T) {
	if _, err := codeTOTPPourCompteur(nil, 1, 6); err == nil {
		t.Fatal("un secret vide doit être refusé")
	}
}

func TestGenererSecretTOTP(t *testing.T) {
	secret, err := GenererSecretTOTP()
	if err != nil {
		t.Fatal(err)
	}
	brut, err := DecoderSecretTOTP(secret)
	if err != nil {
		t.Fatalf("secret non décodable : %v", err)
	}
	if len(brut) != TOTPSecretOctets {
		t.Fatalf("%d octets, attendu %d", len(brut), TOTPSecretOctets)
	}
	if strings.Contains(secret, "=") {
		t.Errorf("le secret ne doit pas contenir de remplissage : %q", secret)
	}

	autre, err := GenererSecretTOTP()
	if err != nil {
		t.Fatal(err)
	}
	if autre == secret {
		t.Error("deux secrets successifs doivent différer")
	}
}

func TestDecoderSecretTOTPTolerant(t *testing.T) {
	reference := base32.StdEncoding.WithPadding(base32.NoPadding).
		EncodeToString(secretRFC6238())

	for _, variante := range []string{
		reference,
		strings.ToLower(reference),
		" " + reference + " ",
		reference + "==",
	} {
		brut, err := DecoderSecretTOTP(variante)
		if err != nil {
			t.Fatalf("décodage de %q : %v", variante, err)
		}
		if string(brut) != string(secretRFC6238()) {
			t.Errorf("décodage de %q = %q", variante, brut)
		}
	}

	if _, err := DecoderSecretTOTP(""); err == nil {
		t.Error("un secret vide doit être refusé")
	}
	if _, err := DecoderSecretTOTP("!!!"); err == nil {
		t.Error("un secret invalide doit être refusé")
	}
	if _, err := DecoderSecretTOTP("ABC"); err == nil {
		t.Error("un secret trop court doit être refusé")
	}
}

func TestCodeTOTPEtVerification(t *testing.T) {
	secret, err := GenererSecretTOTP()
	if err != nil {
		t.Fatal(err)
	}
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	code, err := CodeTOTP(secret, moment)
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != TOTPChiffres {
		t.Fatalf("code = %q (%d chiffres)", code, len(code))
	}

	ok, err := VerifierTOTP(secret, code, moment, TOTPTolerance)
	if err != nil || !ok {
		t.Fatalf("code courant refusé (ok=%v, err=%v)", ok, err)
	}

	// Tolérance : un code généré un pas plus tôt reste valable sur le pas
	// courant, et inversement.
	precedent, _ := CodeTOTP(secret, moment.Add(-TOTPPeriode))
	if ok, _ := VerifierTOTP(secret, precedent, moment, TOTPTolerance); !ok {
		t.Error("le code du pas précédent doit être accepté dans la tolérance")
	}
	suivant, _ := CodeTOTP(secret, moment.Add(TOTPPeriode))
	if ok, _ := VerifierTOTP(secret, suivant, moment, TOTPTolerance); !ok {
		t.Error("le code du pas suivant doit être accepté dans la tolérance")
	}

	// Hors tolérance.
	lointain, _ := CodeTOTP(secret, moment.Add(5*TOTPPeriode))
	if ok, _ := VerifierTOTP(secret, lointain, moment, TOTPTolerance); ok {
		t.Error("un code hors tolérance ne doit pas être accepté")
	}

	// Mauvaise longueur et mauvais code.
	if ok, _ := VerifierTOTP(secret, "12345", moment, TOTPTolerance); ok {
		t.Error("un code trop court ne doit pas être accepté")
	}
	if ok, _ := VerifierTOTP(secret, "000000", moment, TOTPTolerance); ok {
		t.Error("un code erroné ne doit pas être accepté")
	}

	if _, err := VerifierTOTP("!!!", code, moment, TOTPTolerance); err == nil {
		t.Error("un secret illisible doit remonter une erreur")
	}
	if _, err := VerifierTOTP("ABC", code, moment, TOTPTolerance); err == nil {
		t.Error("un secret trop court doit remonter une erreur")
	}
}

func TestCompteurTOTP(t *testing.T) {
	// Le compteur doit valoir 0 sur l'époque Unix et 1 après un pas.
	epoque := time.Unix(0, 0).UTC()
	if got := compteurTOTP(epoque); got != 0 {
		t.Errorf("compteur à l'époque = %d", got)
	}
	if got := compteurTOTP(epoque.Add(TOTPPeriode)); got != 1 {
		t.Errorf("compteur après un pas = %d", got)
	}
	if got := compteurTOTP(time.Unix(59, 0).UTC()); got != 1 {
		t.Errorf("compteur à T=59 = %d", got)
	}
}

func TestGenererCodesSecours(t *testing.T) {
	codes, empreintes, err := GenererCodesSecours()
	if err != nil {
		t.Fatal(err)
	}
	if len(codes) != NombreCodesSecours || len(empreintes) != NombreCodesSecours {
		t.Fatalf("%d codes / %d empreintes", len(codes), len(empreintes))
	}
	vus := map[string]bool{}
	for index, code := range codes {
		if len(code) != 9 || code[4] != '-' {
			t.Errorf("format de code inattendu : %q", code)
		}
		if vus[code] {
			t.Errorf("code de secours dupliqué : %q", code)
		}
		vus[code] = true
		if empreintes[index] != EmpreinteCodeSecours(code) {
			t.Errorf("empreinte incohérente pour %q", code)
		}
	}
}

func TestVerifierCodeSecours(t *testing.T) {
	codes, empreintes, err := GenererCodesSecours()
	if err != nil {
		t.Fatal(err)
	}
	if index := VerifierCodeSecours(codes[2], empreintes); index != 2 {
		t.Errorf("index = %d, attendu 2", index)
	}
	// Tolérance à la casse et au séparateur, comme à la saisie par un humain.
	variante := strings.ToLower(strings.ReplaceAll(codes[0], "-", ""))
	if index := VerifierCodeSecours(variante, empreintes); index != 0 {
		t.Errorf("index = %d, attendu 0", index)
	}
	if index := VerifierCodeSecours("ZZZZ-ZZZZ", empreintes); index != -1 {
		t.Errorf("index = %d, attendu -1", index)
	}
	if index := VerifierCodeSecours("", empreintes); index != -1 {
		t.Errorf("index = %d, attendu -1", index)
	}
}
