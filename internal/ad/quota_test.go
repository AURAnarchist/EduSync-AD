package ad

import (
	"errors"
	"testing"
)

// TestQuotaTypeValide vérifie les types de quota supportés.
func TestQuotaTypeValide(t *testing.T) {
	cas := []struct {
		typ    QuotaType
		valide bool
	}{
		{QuotaTypeDisque, true},
		{QuotaTypeFichiers, true},
		{QuotaType("inconnu"), false},
	}
	for _, c := range cas {
		if c.typ.Valide() != c.valide {
			t.Errorf("QuotaType %q.Valide() = %v, attendu %v", c.typ, !c.valide, c.valide)
		}
	}
}

// TestQuotaSeuilValide vérifie les types de seuil.
func TestQuotaSeuilValide(t *testing.T) {
	cas := []struct {
		seuil  QuotaSeuil
		valide bool
	}{
		{QuotaSeuilAlerte, true},
		{QuotaSeuilBlocage, true},
		{QuotaSeuil("inconnu"), false},
	}
	for _, c := range cas {
		if c.seuil.Valide() != c.valide {
			t.Errorf("QuotaSeuil %q.Valide() = %v, attendu %v", c.seuil, !c.valide, c.valide)
		}
	}
}

// TestQuotaParamsValidate vérifie la validation des paramètres.
func TestQuotaParamsValidate(t *testing.T) {
	// DN manquant.
	p := &QuotaParams{Type: QuotaTypeDisque, Limite: 1024}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrUserDNManquant) {
		t.Errorf("Validate sans DN: err=%v, attendu ErrUserDNManquant", err)
	}

	// Type invalide.
	p = &QuotaParams{UserDN: "CN=Test,OU=Users,DC=X", Type: "invalide", Limite: 1024}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrQuotaTypeInvalide) {
		t.Errorf("Validate type invalide: err=%v, attendu ErrQuotaTypeInvalide", err)
	}

	// Limite invalide (négative).
	p = &QuotaParams{UserDN: "CN=Test,OU=Users,DC=X", Type: QuotaTypeDisque, Limite: -1}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrQuotaLimiteInvalide) {
		t.Errorf("Validate limite négative: err=%v, attendu ErrQuotaLimiteInvalide", err)
	}

	// Seuil invalide.
	p = &QuotaParams{UserDN: "CN=Test,OU=Users,DC=X", Type: QuotaTypeDisque, Limite: 1024, Seuil: "invalide"}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrQuotaSeuilInvalide) {
		t.Errorf("Validate seuil invalide: err=%v, attendu ErrQuotaSeuilInvalide", err)
	}

	// Valide complet.
	p = &QuotaParams{UserDN: "CN=Test,OU=Users,DC=X", Type: QuotaTypeDisque, Limite: 1024, Seuil: QuotaSeuilAlerte}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate complet: err=%v, attendu nil", err)
	}

	// Pas de seuil = valide.
	p = &QuotaParams{UserDN: "CN=Test,OU=Users,DC=X", Type: QuotaTypeDisque, Limite: 1024}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate sans seuil: err=%v, attendu nil", err)
	}
}

// TestFormateTaille vérifie le formatage de taille.
func TestFormateTaille(t *testing.T) {
	cas := []struct {
		octets  int64
		attendu string
	}{
		{0, "0 o"},
		{500, "500 o"},
		{1024, "1,00 Ko"},
		{1536, "1,50 Ko"},
		{1048576, "1,00 Mo"},
		{1073741824, "1,00 Go"},
		{1099511627776, "1,00 To"},
	}
	for _, c := range cas {
		got := FormateTaille(c.octets)
		if got != c.attendu {
			t.Errorf("FormateTaille(%d) = %q, attendu %q", c.octets, got, c.attendu)
		}
	}
}

// TestQuotaBulkParamsValidate vérifie la validation en masse.
func TestQuotaBulkParamsValidate(t *testing.T) {
	// Aucun DN ni OU.
	p := &QuotaBulkParams{}
	if err := p.Validate(); err == nil {
		t.Error("Validate sans DN ni OU devrait échouer")
	}

	// Avec OU seulement.
	p = &QuotaBulkParams{OU: "OU=Users,DC=X", Type: QuotaTypeDisque, Limite: 1024}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec OU: err=%v, attendu nil", err)
	}

	// Avec UserDNs seulement.
	p = &QuotaBulkParams{UserDNs: []string{"CN=Test,OU=Users,DC=X"}, Type: QuotaTypeDisque, Limite: 1024}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec UserDNs: err=%v, attendu nil", err)
	}
}
