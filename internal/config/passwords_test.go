package config

import (
	"strings"
	"testing"
	"unicode"

	"github.com/estemobs/EduSync-AD/internal/models"
)

// TestCleanToken vérifie le nettoyage des prénoms/noms, aligné sur
// `clean_token` de la version Python (accents, apostrophes, espaces, casse).
func TestCleanToken(t *testing.T) {
	cas := map[string]string{
		"Léa":         "lea",
		"Jean-Pierre": "jean-pierre",
		"d'Artagnan":  "dartagnan",
		"  Åsa  ":     "asa",
		"ÉLODIE":      "elodie",
		"François":    "francois",
		"Jean Paul":   "jeanpaul",
		"Renée-Anne":  "renee-anne",
		"Ørsted":      "rsted",
		"nom_avec._-": "nom_avec._-",
		"a!b@c#d":     "abcd",
	}
	for entree, attendu := range cas {
		if got := CleanToken(entree); got != attendu {
			t.Errorf("CleanToken(%q) = %q, attendu %q", entree, got, attendu)
		}
	}
}

// TestRendreMotifMotDePasse vérifie la substitution des variables dans un
// motif fixe (casse et caractères littéraux conservés).
func TestRendreMotifMotDePasse(t *testing.T) {
	cas := []struct {
		motif, prenom, nom string
		annee              int
		attendu            string
	}{
		{"Ecole{AN}!", "Léa", "Martin", 2026, "Ecole26!"},
		{"{P}.{N}{ANNEE}", "Léa", "Martin", 2026, "lea.martin2026"},
		{"{P3}{N2}", "Christophe", "Durand", 2026, "{P3}{N2}"},
		{"{p2}-{n3}", "Léa", "Martin", 2026, "le-mar"},
		{"MotDePasse", "Léa", "Martin", 2026, "MotDePasse"},
		{"Edu$ync2026", "Léa", "Martin", 2026, "Edu$ync2026"},
	}
	for _, c := range cas {
		if got := RendreMotifMotDePasse(c.motif, c.prenom, c.nom, c.annee); got != c.attendu {
			t.Errorf("RendreMotifMotDePasse(%q, %q, %q, %d) = %q, attendu %q",
				c.motif, c.prenom, c.nom, c.annee, got, c.attendu)
		}
	}
}

// TestGenererMotDePasseMotifFixe vérifie que le motif fixe court-circuite la
// génération aléatoire.
func TestGenererMotDePasseMotifFixe(t *testing.T) {
	cfg := DefaultAppConfig()
	policy := models.PasswordPolicy{Longueur: 8, PatternFixe: "Ecole{N}!"}
	got, err := cfg.GenererMotDePasse(policy, "Léa", "Martin")
	if err != nil {
		t.Fatalf("GenererMotDePasse: %v", err)
	}
	if got != "Ecolemartin!" {
		t.Errorf("mot de passe = %q, attendu \"Ecolemartin!\"", got)
	}
}

// TestGenererMotDePasseAleatoire vérifie que la génération aléatoire respecte
// la longueur de la politique.
func TestGenererMotDePasseAleatoire(t *testing.T) {
	cfg := DefaultAppConfig()
	policy := models.PasswordPolicy{Longueur: 12, Majuscules: true, Chiffres: true}
	got, err := cfg.GenererMotDePasse(policy, "Léa", "Martin")
	if err != nil {
		t.Fatalf("GenererMotDePasse: %v", err)
	}
	if len([]rune(got)) != 12 {
		t.Errorf("longueur = %d, attendu 12 (%q)", len([]rune(got)), got)
	}
}

// TestValiderMotDePasse vérifie les exigences signalées, dans l'ordre exact de
// la version Python.
func TestValiderMotDePasse(t *testing.T) {
	policy := models.PasswordPolicy{Longueur: 8, Majuscules: true, Chiffres: true}

	if got := ValiderMotDePasse(policy, ""); len(got) != 1 ||
		got[0] != "Le mot de passe est obligatoire." {
		t.Errorf("mot de passe vide = %v", got)
	}

	got := ValiderMotDePasse(policy, "abc")
	attendus := []string{
		"Le mot de passe doit contenir au moins 8 caractères.",
		"Le mot de passe doit contenir au moins une majuscule.",
		"Le mot de passe doit contenir au moins un chiffre.",
	}
	if strings.Join(got, "|") != strings.Join(attendus, "|") {
		t.Errorf("erreurs = %v, attendues %v", got, attendus)
	}

	if got := ValiderMotDePasse(policy, "Abcdefg1"); len(got) != 0 {
		t.Errorf("mot de passe valide rejeté : %v", got)
	}

	// Caractère spécial exigé
	speciaux := models.PasswordPolicy{Longueur: 8, CaracteresSpeciaux: true}
	got = ValiderMotDePasse(speciaux, "abcdefgh")
	if len(got) != 1 || got[0] != "Le mot de passe doit contenir au moins un caractère spécial." {
		t.Errorf("erreurs = %v", got)
	}

	// Interdits (comparaison sans casse)
	got = ValiderMotDePasse(models.PasswordPolicy{Longueur: 8}, "Léa.Martin", "léa.martin")
	if len(got) != 1 || got[0] != "Le mot de passe ne doit pas être identique à votre identifiant." {
		t.Errorf("interdit = %v", got)
	}

	// Longueur maximale
	var long strings.Builder
	for i := 0; i < MdpLongueurMaxSaisie+1; i++ {
		long.WriteRune('a')
	}
	long.WriteString("1")
	got = ValiderMotDePasse(models.PasswordPolicy{}, long.String())
	trouve := false
	for _, e := range got {
		if strings.Contains(e, "ne doit pas dépasser") {
			trouve = true
		}
	}
	if !trouve {
		t.Errorf("dépassement de longueur non signalé : %v", got)
	}

	// Le prédicat « caractère spécial » ne doit pas compter les lettres
	// accentuées comme spéciales.
	if !contientRune("é", unicode.IsLetter) {
		t.Error("é doit être une lettre")
	}
}
