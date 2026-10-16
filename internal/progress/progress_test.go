package progress

import (
	"context"
	"testing"
	"time"
)

func TestTrackerNouvelID(t *testing.T) {
	tr := NewTracker()

	id, ctx := tr.NouvelID("import")
	if id == "" {
		t.Fatal("l'ID ne doit pas être vide")
	}
	if ctx == nil {
		t.Fatal("le contexte ne doit pas être nil")
	}

	op := tr.Get(id)
	if op == nil {
		t.Fatal("l'opération doit exister après NouvelID")
	}
	if op.Type != "import" {
		t.Errorf("type = %q, attendu import", op.Type)
	}
	if op.Statut != StatutEnCours {
		t.Errorf("statut = %q, attendu en_cours", op.Statut)
	}
	if op.Total != 0 {
		t.Errorf("total = %d, attendu 0", op.Total)
	}
	if op.DemarreLe.IsZero() {
		t.Error("demarre_le doit être renseigné")
	}
}

func TestTrackerMiseAJour(t *testing.T) {
	tr := NewTracker()
	id, _ := tr.NouvelID("import")

	if !tr.MiseAJour(id, 5, "traitement de Jean Durand") {
		t.Error("MiseAJour doit retourner true")
	}

	op := tr.Get(id)
	if op.Actuel != 5 {
		t.Errorf("actuel = %d, attendu 5", op.Actuel)
	}
	if op.Message != "traitement de Jean Durand" {
		t.Errorf("message = %q", op.Message)
	}
	if op.Pourcent != 0 {
		t.Errorf("pourcent = %d, attendu 0 (total=0)", op.Pourcent)
	}

	// Avec un total défini, le pourcentage doit être calculé.
	op2 := tr.Get(id)
	op2.Total = 10
	tr.MiseAJour(id, 5, "")
	if op2.Pourcent != 50 {
		t.Errorf("pourcent = %d, attendu 50", op2.Pourcent)
	}
}

func TestTrackerMiseAJourIDInconnu(t *testing.T) {
	tr := NewTracker()
	if tr.MiseAJour("inexistant", 1, "x") {
		t.Error("MiseAJour doit retourner false pour un ID inconnu")
	}
}

func TestTrackerAnnuler(t *testing.T) {
	tr := NewTracker()
	id, ctx := tr.NouvelID("migration")

	if !tr.Annuler(id) {
		t.Error("Annuler doit retourner true")
	}

	op := tr.Get(id)
	if op.Statut != StatutAnnule {
		t.Errorf("statut = %q, attendu annule", op.Statut)
	}
	if op.TermineLe.IsZero() {
		t.Error("termine_le doit être renseigné après annulation")
	}

	// Le contexte doit être annulé.
	select {
	case <-ctx.Done():
		// OK
	case <-time.After(100 * time.Millisecond):
		t.Error("le contexte doit être annulé")
	}

	// Annuler deux fois retourne false.
	if tr.Annuler(id) {
		t.Error("Annuler une opération déjà annulée doit retourner false")
	}
}

func TestTrackerAnnulerIDInconnu(t *testing.T) {
	tr := NewTracker()
	if tr.Annuler("inexistant") {
		t.Error("Annuler doit retourner false pour un ID inconnu")
	}
}

func TestTrackerTerminer(t *testing.T) {
	tr := NewTracker()
	id, ctx := tr.NouvelID("password")

	tr.Terminer(id, true, "5 utilisateurs créés", "")

	op := tr.Get(id)
	if op.Statut != StatutTermine {
		t.Errorf("statut = %q, attendu termine", op.Statut)
	}
	if op.Message != "5 utilisateurs créés" {
		t.Errorf("message = %q", op.Message)
	}

	// Contexte annulé après terme.
	select {
	case <-ctx.Done():
	case <-time.After(100 * time.Millisecond):
		t.Error("le contexte doit être annulé après Terminer")
	}
}

func TestTrackerTerminerEchec(t *testing.T) {
	tr := NewTracker()
	id, _ := tr.NouvelID("departs")

	tr.Terminer(id, false, "échec réseau", "LDAP inaccessible")

	op := tr.Get(id)
	if op.Statut != StatutEchec {
		t.Errorf("statut = %q, attendu echec", op.Statut)
	}
	if op.Erreur != "LDAP inaccessible" {
		t.Errorf("erreur = %q", op.Erreur)
	}
}

func TestTrackerListe(t *testing.T) {
	tr := NewTracker()

	_, _ = tr.NouvelID("import")
	id2, _ := tr.NouvelID("migration")

	liste := tr.Liste()
	if len(liste) != 2 {
		t.Errorf("liste = %d opérations, attendu 2", len(liste))
	}
	if liste[0].ID != id2 {
		t.Errorf("premier élément = %q, attendu %q (plus récent d'abord)", liste[0].ID, id2)
	}
}

func TestTrackerAnnulable(t *testing.T) {
	// Contexte non annulé.
	ctx1, cancel1 := context.WithCancel(context.Background())
	defer cancel1()
	if Annulable(ctx1) {
		t.Error("Annulable doit retourner false sur un contexte actif")
	}

	// Contexte annulé.
	ctx2, cancel := context.WithCancel(context.Background())
	cancel()
	if !Annulable(ctx2) {
		t.Error("Annulable doit retourner true sur un contexte annulé")
	}
}

func TestTrackerGetInexistant(t *testing.T) {
	tr := NewTracker()
	if tr.Get("inexistant") != nil {
		t.Error("Get doit retourner nil pour un ID inconnu")
	}
}
