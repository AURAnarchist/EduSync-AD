package config

import (
	"strconv"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// exemplesPredefinis reprend la table « Génération des identifiants — Formats
// prédéfinis » (Thomas Martin) du cahier des charges, identique au test Python
// `tests/core/test_identifiers.py`.
var exemplesPredefinis = []struct {
	format  string
	attendu string
}{
	{"prenom", "thomas"},
	{"nom", "martin"},
	{"prenomnom", "thomasmartin"},
	{"nomprenom", "martinthomas"},
	{"prenom.nom", "thomas.martin"},
	{"nom.prenom", "martin.thomas"},
	{"p.nom", "t.martin"},
	{"nom.p", "martin.t"},
	{"pnom", "tmartin"},
	{"nomp", "martint"},
	{"pp.nom", "th.martin"},
	{"ppp.nom", "tho.martin"},
	{"nom.pp", "martin.th"},
	{"nom.ppp", "martin.tho"},
	{"prenom.nnn", "thomas.mar"},
	{"prenom_nom", "thomas_martin"},
	{"nom_prenom", "martin_thomas"},
	{"prenomNom", "thomasMartin"},
	{"NomPrenom", "MartinThomas"},
}

func TestFormatsPredefinis(t *testing.T) {
	for _, cas := range exemplesPredefinis {
		moteur := MoteurIdentifiants{Format: cas.format}
		if obtenu := moteur.IdentifiantBase("Thomas", "Martin"); obtenu != cas.attendu {
			t.Errorf("format %q : obtenu %q, attendu %q", cas.format, obtenu, cas.attendu)
		}
	}
}

func TestPresetsIdentifiantsCouvrentLesExemples(t *testing.T) {
	index := map[string]bool{}
	for _, format := range PresetsIdentifiants {
		index[format] = true
	}
	for _, cas := range exemplesPredefinis {
		if !index[cas.format] {
			t.Errorf("format %q absent de PresetsIdentifiants", cas.format)
		}
	}
	for _, format := range PresetsIdentifiantsCamel {
		if !index[format] {
			t.Errorf("format camel %q absent de PresetsIdentifiants", format)
		}
	}
}

func TestStripAccents(t *testing.T) {
	if got := StripAccents("Éléonore"); got != "Eleonore" {
		t.Errorf("StripAccents(Éléonore) = %q", got)
	}
	if got := StripAccents("François"); got != "Francois" {
		t.Errorf("StripAccents(François) = %q", got)
	}
}

func TestCleanTokenIdentifiants(t *testing.T) {
	cas := []struct{ entree, attendu string }{
		{"O'Brien", "obrien"},
		{"Jean Paul", "jeanpaul"},
		{"Renée", "renee"},
		{"Müller!", "muller"},
		{"Jean-Pierre", "jean-pierre"}, // le tiret est conservé
	}
	for _, c := range cas {
		if got := CleanToken(c.entree); got != c.attendu {
			t.Errorf("CleanToken(%q) = %q, attendu %q", c.entree, got, c.attendu)
		}
	}
}

func TestModelesPersonnalises(t *testing.T) {
	cas := []struct {
		format  string
		annee   int
		attendu string
	}{
		{"eleve.{p1}.{N}", 0, "eleve.t.martin"},
		{"{p1}{N}{AN}", 2025, "tmartin25"},
		{"{P}.{n3}", 0, "thomas.mar"},
	}
	for _, c := range cas {
		moteur := MoteurIdentifiants{Format: c.format, Annee: c.annee}
		if got := moteur.IdentifiantBase("Thomas", "Martin"); got != c.attendu {
			t.Errorf("format %q : obtenu %q, attendu %q", c.format, got, c.attendu)
		}
	}
}

func TestRenderTemplatePartageAvecLeMoteur(t *testing.T) {
	// RenderTemplate (motifs de mot de passe et adresses mail) doit donner le
	// même résultat que le moteur d'identifiants pour le même motif.
	if got := RenderTemplate("{P}.{N}", "Thomas", "Martin", 2025); got != "thomas.martin" {
		t.Errorf("RenderTemplate = %q", got)
	}
	if got := RenderTemplate("{p1}{N}{AN}", "Thomas", "Martin", 2025); got != "tmartin25" {
		t.Errorf("RenderTemplate = %q", got)
	}
}

func TestPrenomCompose(t *testing.T) {
	premier := MoteurIdentifiants{Format: "prenom.nom", ReglePrenomCompose: models.PrenomPremierPrenom}
	if got := premier.IdentifiantBase("Jean-Pierre", "Dupont"); got != "jean.dupont" {
		t.Errorf("premier prénom : %q", got)
	}
	concat := MoteurIdentifiants{Format: "prenom.nom", ReglePrenomCompose: models.PrenomConcatenation}
	if got := concat.IdentifiantBase("Jean-Pierre", "Dupont"); got != "jeanpierre.dupont" {
		t.Errorf("concaténation : %q", got)
	}
	// Troncature aboutit au même résultat que « premier prénom » (comme Python).
	tronc := MoteurIdentifiants{Format: "prenom.nom", ReglePrenomCompose: models.PrenomTroncature}
	if got := tronc.IdentifiantBase("Jean-Pierre", "Dupont"); got != "jean.dupont" {
		t.Errorf("troncature : %q", got)
	}
}

// ensemble construit l'index des identifiants déjà pris.
func ensemble(valeurs ...string) map[string]bool {
	index := make(map[string]bool, len(valeurs))
	for _, valeur := range valeurs {
		index[valeur] = true
	}
	return index
}

func TestDoublonSuffixeNumerique(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "prenom.nom"}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin",
		ensemble("thomas.martin", "thomas.martin2"))
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.martin3" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestDoublonSuffixeNumeriqueSeparateur(t *testing.T) {
	moteur := MoteurIdentifiants{
		Format:            "prenom.nom",
		RegleDoublon:      models.DoublonSuffixeNumeriqueSep,
		SeparateurDoublon: "-",
	}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin", ensemble("thomas.martin"))
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.martin-2" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestDoublonPrefixeNumerique(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "prenom.nom", RegleDoublon: models.DoublonPrefixeNumerique}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin", ensemble("thomas.martin"))
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "2.thomas.martin" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestDoublonLettresPrenom(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "p.nom", RegleDoublon: models.DoublonLettresPrenom}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin",
		ensemble("t.martin", "th.martin"))
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "tho.martin" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestDoublonLettresNom(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "prenom.nnn", RegleDoublon: models.DoublonLettresNom}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin", ensemble("thomas.mar"))
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.mart" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestDoublonLettresSansVariableDePrefixe(t *testing.T) {
	// Le motif « {P}.{N} » ne contient pas de {pN} : la règle « lettres » doit
	// retomber sur un suffixe numérique, comme côté Python.
	moteur := MoteurIdentifiants{Format: "prenom.nom", RegleDoublon: models.DoublonLettresPrenom}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin", ensemble("thomas.martin"))
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.martin2" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestDoublonAnneeSuffixe(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "prenom.nom", RegleDoublon: models.DoublonAnneeSuffixe, Annee: 2025}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin", ensemble("thomas.martin"))
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.martin2025" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestSansDoublon(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "prenom.nom"}
	identifiant, doublon, err := moteur.GenererUnique("Thomas", "Martin", ensemble())
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.martin" || doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

// Le sAMAccountName est limité à 20 caractères par Active Directory lui-même
// (contrainte SAM pré-Windows 2000, non configurable) — découvert en testant
// contre un vrai AD (« Thérèse Müller-Özdemir » → 21 caractères).
func TestTroncatureLimiteSAM(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "prenom.nom"}
	identifiant := moteur.IdentifiantBase("Thérèse", "Müller-Özdemir")
	if identifiant != "therese.muller-ozdem" {
		t.Fatalf("identifiant = %q", identifiant)
	}
	if len(identifiant) > LongueurMaxIdentifiant {
		t.Fatalf("longueur = %d", len(identifiant))
	}
}

func TestDoublonNumeriqueResteDansLaLimiteSAM(t *testing.T) {
	moteur := MoteurIdentifiants{Format: "prenom.nom"}
	base := moteur.IdentifiantBase("Thérèse", "Müller-Özdemir")
	identifiant, doublon, err := moteur.GenererUnique("Thérèse", "Müller-Özdemir", ensemble(base))
	if err != nil {
		t.Fatal(err)
	}
	if !doublon {
		t.Fatal("doublon attendu")
	}
	if len(identifiant) > LongueurMaxIdentifiant {
		t.Fatalf("longueur = %d (%q)", len(identifiant), identifiant)
	}
	// Le suffixe ne doit jamais être coupé par la troncature (sinon
	// l'identifiant retomberait sur `base`, déjà pris).
	if identifiant[len(identifiant)-1] != '2' {
		t.Fatalf("identifiant = %q, doit se terminer par « 2 »", identifiant)
	}
	if identifiant == base {
		t.Fatal("identifiant confondu avec la base")
	}
}

func TestGenererUniqueRemonteUneErreurQuandToutEstPris(t *testing.T) {
	// Tous les candidats sont pris : le moteur doit remonter une erreur plutôt
	// que de renvoyer un identifiant déjà utilisé.
	moteur := MoteurIdentifiants{Format: "prenom.nom"}
	existants := ensemble("thomas.martin")
	for n := 2; n < 1000; n++ {
		existants["thomas.martin"+strconv.Itoa(n)] = true
	}
	if _, _, err := moteur.GenererUnique("Thomas", "Martin", existants); err == nil {
		t.Fatal("une erreur est attendue quand tous les candidats sont pris")
	}
}

func TestGenerateEmailEtIdentifiantDepuisConfig(t *testing.T) {
	cfg := DefaultAppConfig()
	if got := cfg.GenerateIdentifiant("Thomas", "Martin", models.AccountEleve); got != "thomas.martin" {
		t.Errorf("élève : %q", got)
	}
	// Le format personnel par défaut est « p.nom » (préréglage → « t.martin »).
	if got := cfg.GenerateIdentifiant("Thomas", "Martin", models.AccountPersonnel); got != "t.martin" {
		t.Errorf("personnel : %q", got)
	}
	if got := cfg.GenerateEmail("Thomas", "Martin", "lycee.fr"); got != "thomas.martin@lycee.fr" {
		t.Errorf("mail : %q", got)
	}
	// La règle de prénom composé s'applique à l'adresse mail.
	cfg.ReglePrenomCompose = models.PrenomConcatenation
	if got := cfg.GenerateEmail("Jean-Pierre", "Dupont", "lycee.fr"); got != "jeanpierre.dupont@lycee.fr" {
		t.Errorf("mail composé : %q", got)
	}
}
