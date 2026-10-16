package ad

import (
	"errors"
	"testing"
)

func TestProfileTypeValide(t *testing.T) {
	cas := []struct {
		typ    ProfileType
		valide bool
	}{
		{ProfileTypeItinerant, true},
		{ProfileTypeLocal, true},
		{ProfileTypeObligatoire, true},
		{ProfileType("inconnu"), false},
	}
	for _, c := range cas {
		if c.typ.Valide() != c.valide {
			t.Errorf("ProfileType %q.Valide() = %v, attendu %v", c.typ, !c.valide, c.valide)
		}
	}
}

func TestProfileParamsValidate(t *testing.T) {
	// DN manquant
	p := &ProfileParams{Type: ProfileTypeItinerant, Chemin: "\\\\srv\\profil\\%USERNAME%"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrUserDNManquant) {
		t.Errorf("Validate sans DN: err=%v, attendu ErrUserDNManquant", err)
	}

	// Type invalide
	p = &ProfileParams{UserDN: "CN=Test,OU=Users,DC=X", Type: "invalide", Chemin: "\\\\srv\\profil\\%USERNAME%"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrProfileTypeInvalide) {
		t.Errorf("Validate type invalide: err=%v, attendu ErrProfileTypeInvalide", err)
	}

	// Chemin manquant pour itinérant/obligatoire
	p = &ProfileParams{UserDN: "CN=Test,OU=Users,DC=X", Type: ProfileTypeItinerant, Chemin: ""}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrCheminProfilManquant) {
		t.Errorf("Validate chemin vide: err=%v, attendu ErrCheminProfilManquant", err)
	}

	// Local sans chemin : valide
	p = &ProfileParams{UserDN: "CN=Test,OU=Users,DC=X", Type: ProfileTypeLocal, Chemin: ""}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate local sans chemin: err=%v, attendu nil", err)
	}

	// Valide complet
	p = &ProfileParams{UserDN: "CN=Test,OU=Users,DC=X", Type: ProfileTypeItinerant, Chemin: "\\\\srv\\profil\\%USERNAME%"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate complet: err=%v, attendu nil", err)
	}
}

func TestProfileResolveVariables(t *testing.T) {
	cas := []struct {
		chemin  string
		sam     string
		ouNom   string
		attendu string
	}{
		{"\\\\srv\\profil\\%USERNAME%", "j.durand", "Eleves", "\\\\srv\\profil\\j.durand"},
		{"\\\\srv\\profil\\%SAM%", "j.durand", "Eleves", "\\\\srv\\profil\\j.durand"},
		{"\\\\srv\\profil\\%OU%\\%USERNAME%", "j.durand", "Eleves", "\\\\srv\\profil\\Eleves\\j.durand"},
		{"\\\\srv\\profils\\%USERNAME%.man", "j.durand", "Eleves", "\\\\srv\\profils\\j.durand.man"},
		{"chemin sans variable", "j.durand", "Eleves", "chemin sans variable"},
		{"", "j.durand", "Eleves", ""},
	}
	for _, c := range cas {
		got := resoudreVariables(c.chemin, c.sam, c.ouNom)
		if got != c.attendu {
			t.Errorf("resoudreVariables(%q, %q, %q) = %q, attendu %q", c.chemin, c.sam, c.ouNom, got, c.attendu)
		}
	}
}

func TestProfileResolveVariablesVide(t *testing.T) {
	// SAM vide → variable remplacée par vide
	if got := resoudreVariables("%USERNAME%", "", "Eleves"); got != "" {
		t.Errorf("resoudreVariables %%USERNAME%% vide = %q, attendu %q", got, "")
	}
	// OU vide → variable remplacée par vide
	if got := resoudreVariables("%OU%", "j.durand", ""); got != "" {
		t.Errorf("resoudreVariables %%OU%% vide = %q, attendu %q", got, "")
	}
}
