package ad

import (
	"errors"
	"testing"
)

// TestHomeDirTypeValide vérifie les types de dossier personnel.
func TestHomeDirTypeValide(t *testing.T) {
	cas := []struct {
		typ    HomeDirType
		valide bool
	}{
		{HomeDirTypeStandard, true},
		{HomeDirTypePersonnalise, true},
		{HomeDirType("inconnu"), false},
	}
	for _, c := range cas {
		if c.typ.Valide() != c.valide {
			t.Errorf("HomeDirType %q.Valide() = %v, attendu %v", c.typ, !c.valide, c.valide)
		}
	}
}

// TestHomeDirParamsValidate vérifie la validation des paramètres.
func TestHomeDirParamsValidate(t *testing.T) {
	// DN manquant.
	p := &HomeDirParams{Type: HomeDirTypeStandard, Chemin: "\\\\srv\\homes\\%USERNAME%"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrUserDNManquant) {
		t.Errorf("Validate sans DN: err=%v, attendu ErrUserDNManquant", err)
	}

	// Type invalide.
	p = &HomeDirParams{UserDN: "CN=Test,OU=Users,DC=X", Type: "invalide", Chemin: "\\\\srv\\homes\\%USERNAME%"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrHomeDirTypeInvalide) {
		t.Errorf("Validate type invalide: err=%v, attendu ErrHomeDirTypeInvalide", err)
	}

	// Chemin vide pour standard/personnalise.
	p = &HomeDirParams{UserDN: "CN=Test,OU=Users,DC=X", Type: HomeDirTypeStandard, Chemin: ""}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrCheminHomeDirManquant) {
		t.Errorf("Validate chemin vide: err=%v, attendu ErrCheminHomeDirManquant", err)
	}

	// Lettre de lecteur invalide (plus d'un caractere).
	p = &HomeDirParams{UserDN: "CN=Test,OU=Users,DC=X", Type: HomeDirTypeStandard, Chemin: "\\\\srv\\homes\\%USERNAME%", LettreLecteur: "AB"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrLettreLecteurInvalide) {
		t.Errorf("Validate lettre invalide: err=%v, attendu ErrLettreLecteurInvalide", err)
	}

	// Valide complet.
	p = &HomeDirParams{UserDN: "CN=Test,OU=Users,DC=X", Type: HomeDirTypeStandard, Chemin: "\\\\srv\\homes\\%USERNAME%", LettreLecteur: "H"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate complet: err=%v, attendu nil", err)
	}
}

// TestResoudreVariablesHomeDir verifie la substitution des variables.
func TestResoudreVariablesHomeDir(t *testing.T) {
	cas := []struct {
		chemin  string
		user    *User
		attendu string
	}{
		{
			"\\\\srv\\homes\\%USERNAME%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand"},
			"\\\\srv\\homes\\j.durand",
		},
		{
			"\\\\srv\\homes\\%SAM%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand"},
			"\\\\srv\\homes\\j.durand",
		},
		{
			"\\\\srv\\homes\\%FULLNAME%",
			&User{SAMAccountName: "j.durand", DisplayName: "Jean Durand"},
			"\\\\srv\\homes\\Jean Durand",
		},
		{
			"pas de variable",
			&User{SAMAccountName: "j.durand"},
			"pas de variable",
		},
	}
	for _, c := range cas {
		got := resoudreVariablesHomeDir(c.chemin, c.user)
		if got != c.attendu {
			t.Errorf("resoudreVariablesHomeDir(%q) = %q, attendu %q", c.chemin, got, c.attendu)
		}
	}
}

// TestHomeDirBulkParamsValidate verifie la validation en masse.
func TestHomeDirBulkParamsValidate(t *testing.T) {
	// Aucun DN ni OU.
	p := &HomeDirBulkParams{}
	if err := p.Validate(); err == nil {
		t.Error("Validate sans DN ni OU devrait echouer")
	}

	// Avec OU seulement.
	p = &HomeDirBulkParams{OU: "OU=Users,DC=X", Type: HomeDirTypeStandard, Chemin: "\\\\srv\\homes\\%USERNAME%"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec OU: err=%v, attendu nil", err)
	}

	// Avec UserDNs seulement.
	p = &HomeDirBulkParams{UserDNs: []string{"CN=Test,OU=Users,DC=X"}, Type: HomeDirTypeStandard, Chemin: "\\\\srv\\homes\\%USERNAME%"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec UserDNs: err=%v, attendu nil", err)
	}
}
