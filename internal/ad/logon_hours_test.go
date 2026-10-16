package ad

import (
	"errors"
	"testing"
)

// TestLogonHoursFromBytesEtRetour vérifie la conversion bidirectionnelle
// entre 21 octets LDAP et la grille 7×24.
func TestLogonHoursFromBytesEtRetour(t *testing.T) {
	// Tous les bits à 1 (toujours autorisé).
	tousUn := make([]byte, 21)
	for i := range tousUn {
		tousUn[i] = 0xFF
	}
	h := logonHoursFromBytes(tousUn)
	for j := 0; j < 7; j++ {
		for hh := 0; hh < 24; hh++ {
			if !h.EstAutorise(j, hh) {
				t.Errorf("attendu autorisé jour=%d heure=%d", j, hh)
			}
		}
	}
	out := h.ToBytes()
	for i, b := range out {
		if b != 0xFF {
			t.Errorf("octet %d = 0x%02x, attendu 0xFF", i, b)
		}
	}

	// Tous les bits à 0 (jamais autorisé).
	tousZeros := make([]byte, 21)
	h = logonHoursFromBytes(tousZeros)
	for j := 0; j < 7; j++ {
		for hh := 0; hh < 24; hh++ {
			if h.EstAutorise(j, hh) {
				t.Errorf("attendu interdit jour=%d heure=%d", j, hh)
			}
		}
	}
	out = h.ToBytes()
	for i, b := range out {
		if b != 0x00 {
			t.Errorf("octet %d = 0x%02x, attendu 0x00", i, b)
		}
	}
}

// TestLogonHoursBitUnique vérifie qu'un seul bit est correctement placé.
func TestLogonHoursBitUnique(t *testing.T) {
	// Bit 0 = dimanche 00h-01h (premier octet, bit de poids faible).
	h := LogonHours{}
	h.Definir(0, 0, true)
	out := h.ToBytes()
	if out[0] != 0x01 {
		t.Errorf("premier octet = 0x%02x, attendu 0x01 (dimanche 00h)", out[0])
	}
	if !h.EstAutorise(0, 0) {
		t.Error("dimanche 00h devrait être autorisé")
	}
	if h.EstAutorise(0, 1) {
		t.Error("dimanche 01h ne devrait pas être autorisé")
	}
	if h.EstAutorise(1, 0) {
		t.Error("lundi 00h ne devrait pas être autorisé")
	}

	// Bit 167 = samedi 23h-00h (dernier octet, bit de poids fort).
	h2 := LogonHours{}
	h2.Definir(6, 23, true)
	out2 := h2.ToBytes()
	if out2[20] != 0x80 {
		t.Errorf("dernier octet = 0x%02x, attendu 0x80 (samedi 23h)", out2[20])
	}
	if !h2.EstAutorise(6, 23) {
		t.Error("samedi 23h devrait être autorisé")
	}
}

// TestLogonHoursHeuresCours vérifie le préréglage "Heures cours".
func TestLogonHoursHeuresCours(t *testing.T) {
	h := HeuresCours()
	// Lundi-vendredi (jours 1-5), 8h-17h (heures 8-16).
	for jour := 1; jour <= 5; jour++ {
		for hh := 8; hh < 17; hh++ {
			if !h.EstAutorise(jour, hh) {
				t.Errorf("heures cours: attendu autorisé jour=%d heure=%d", jour, hh)
			}
		}
	}
	// En dehors (samedi-dimanche, nuit).
	if h.EstAutorise(0, 12) {
		t.Error("dimanche midi ne devrait pas être autorisé (heures cours)")
	}
	if h.EstAutorise(6, 12) {
		t.Error("samedi midi ne devrait pas être autorisé (heures cours)")
	}
	if h.EstAutorise(1, 7) {
		t.Error("lundi 7h ne devrait pas être autorisé (heures cours)")
	}
	if h.EstAutorise(1, 17) {
		t.Error("lundi 17h ne devrait pas être autorisé (heures cours)")
	}
}

// TestLogonHoursHeuresAdmin vérifie le préréglage "Heures admin" (toujours).
func TestLogonHoursHeuresAdmin(t *testing.T) {
	h := HeuresAdmin()
	for j := 0; j < 7; j++ {
		for hh := 0; hh < 24; hh++ {
			if !h.EstAutorise(j, hh) {
				t.Errorf("heures admin: attendu autorisé jour=%d heure=%d", j, hh)
			}
		}
	}
}

// TestLogonHoursParamsValidate vérifie la validation des paramètres.
func TestLogonHoursParamsValidate(t *testing.T) {
	// DN manquant.
	p := &LogonHoursParams{}
	if err := p.Validate(); err == nil || !errors.Is(err, ErrUserDNManquant) {
		t.Errorf("Validate sans DN: err=%v, attendu ErrUserDNManquant", err)
	}

	// Heures nil (pas de modification) = valide.
	p = &LogonHoursParams{UserDN: "CN=Test,OU=Users,DC=X"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate heures nil: err=%v, attendu nil", err)
	}

	// Heures définies = valide.
	p = &LogonHoursParams{UserDN: "CN=Test,OU=Users,DC=X", Heures: &LogonHours{}}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate heures définies: err=%v, attendu nil", err)
	}
}

// TestResoudreIndexBit vérifie le calcul de l'index de bit.
func TestResoudreIndexBit(t *testing.T) {
	cas := []struct {
		jour, heure int
		attendu     int
	}{
		{0, 0, 0},    // dimanche 00h
		{0, 23, 23},  // dimanche 23h
		{1, 0, 24},   // lundi 00h
		{6, 23, 167}, // samedi 23h
	}
	for _, c := range cas {
		if got := indexBit(c.jour, c.heure); got != c.attendu {
			t.Errorf("indexBit(%d, %d) = %d, attendu %d", c.jour, c.heure, got, c.attendu)
		}
	}
}

// TestLogonHoursBulkParamsValidate vérifie la validation en masse.
func TestLogonHoursBulkParamsValidate(t *testing.T) {
	// Aucun DN ni OU.
	p := &LogonHoursBulkParams{}
	if err := p.Validate(); err == nil {
		t.Error("Validate sans DN ni OU devrait échouer")
	}

	// Avec OU seulement.
	p = &LogonHoursBulkParams{OU: "OU=Users,DC=X"}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec OU: err=%v, attendu nil", err)
	}

	// Avec UserDNs seulement.
	p = &LogonHoursBulkParams{UserDNs: []string{"CN=Test,OU=Users,DC=X"}}
	if err := p.Validate(); err != nil {
		t.Errorf("Validate avec UserDNs: err=%v, attendu nil", err)
	}
}
