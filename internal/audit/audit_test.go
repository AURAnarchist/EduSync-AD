package audit

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/models"
)

func ouvrirJournal(t *testing.T) (*Log, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "journal.db")
	l, err := Open(path)
	if err != nil {
		t.Fatalf("ouverture du journal: %v", err)
	}
	t.Cleanup(func() { _ = l.Close() })
	return l, path
}

func entree(action, compte string) models.ActionLogEntry {
	return models.ActionLogEntry{
		Timestamp:  time.Now().UTC().Format(time.RFC3339),
		ActionType: action,
		Compte:     compte,
		Resultat:   "succes",
	}
}

func TestEnregistrementEtLecture(t *testing.T) {
	l, _ := ouvrirJournal(t)
	l.SetContext("admin", "EDUSYNC.LOCAL")

	e := entree("creation_compte", "eleve01")
	e.OUSource = "OU=Eleves,DC=edusync,DC=local"
	e.Detail = "Creation depuis CSV"
	if err := l.Record(e); err != nil {
		t.Fatalf("enregistrement: %v", err)
	}

	entries, err := l.List(Filter{})
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("%d entrées, attendu 1", len(entries))
	}

	got := entries[0]
	if got.ID <= 0 {
		t.Error("l'identifiant SQLite doit être renseigné")
	}
	if got.ActionType != "creation_compte" || got.Compte != "eleve01" {
		t.Errorf("entrées incohérentes: %+v", got)
	}
	if got.Utilisateur != "admin" {
		t.Errorf("utilisateur = %q, attendu le contexte « admin »", got.Utilisateur)
	}
	if got.Domaine != "EDUSYNC.LOCAL" {
		t.Errorf("domaine = %q", got.Domaine)
	}
	if got.SessionID == "" {
		t.Error("session_id doit être auto-renseigné")
	}
	if got.Simulation {
		t.Error("simulation doit être false par défaut")
	}
	if got.OUSource != "OU=Eleves,DC=edusync,DC=local" {
		t.Errorf("ou_source = %q", got.OUSource)
	}
}

func TestSessionUnique(t *testing.T) {
	a, b := NewSessionID(), NewSessionID()
	if len(a) != 12 || len(b) != 12 {
		t.Errorf("longueur de session = %d/%d, attendu 12", len(a), len(b))
	}
	if a == b {
		t.Error("deux sessions doivent être distinctes")
	}
}

func TestHorodatageObligatoire(t *testing.T) {
	l, _ := ouvrirJournal(t)
	if err := l.Record(models.ActionLogEntry{ActionType: "x", Compte: "y", Resultat: "succes"}); err == nil {
		t.Fatal("une entrée sans horodatage doit être refusée")
	}
}

func TestFiltres(t *testing.T) {
	l, _ := ouvrirJournal(t)
	l.SetContext("admin", "EDUSYNC.LOCAL")

	base := time.Date(2026, 10, 1, 8, 0, 0, 0, time.UTC)

	ajouter := func(action, compte, resultat string, simulation bool, decalage time.Duration) {
		t.Helper()
		e := models.ActionLogEntry{
			Timestamp:  base.Add(decalage).Format(time.RFC3339),
			ActionType: action,
			Compte:     compte,
			Resultat:   resultat,
			Simulation: simulation,
		}
		if err := l.Record(e); err != nil {
			t.Fatalf("enregistrement: %v", err)
		}
	}

	ajouter("creation_compte", "eleve01", "succes", false, 0)
	ajouter("creation_compte", "eleve02", "echec", false, time.Hour)
	ajouter("reset_mdp", "perso01", "succes", false, 2*time.Hour)
	ajouter("migration_classe", "eleve03", "succes", true, 3*time.Hour)

	if n, _ := l.Count(); n != 4 {
		t.Fatalf("comptage = %d, attendu 4", n)
	}

	cas := []struct {
		nom     string
		filtre  Filter
		attendu int
		premier string
	}{
		{"tous", Filter{}, 4, "migration_classe"},
		{"par type", Filter{ActionType: "creation_compte"}, 2, "creation_compte"},
		{"par résultat", Filter{Resultat: "echec"}, 1, "creation_compte"},
		{"simulation", Filter{Simulation: boolPtr(true)}, 1, "migration_classe"},
		{"réel", Filter{Simulation: boolPtr(false)}, 3, "reset_mdp"},
		{"par domaine", Filter{Domaine: "EDUSYNC.LOCAL"}, 4, "migration_classe"},
		{"domaine inconnu", Filter{Domaine: "AUTRE.LOCAL"}, 0, ""},
		{"par opérateur", Filter{Operateur: "admin"}, 4, "migration_classe"},
		{"recherche", Filter{Recherche: "eleve0"}, 3, "migration_classe"},
		{"bornes", Filter{From: base.Format(time.RFC3339),
			To: base.Add(time.Hour).Format(time.RFC3339)}, 2, "creation_compte"},
		{"limite", Filter{Limit: 1}, 1, "migration_classe"},
	}
	for _, c := range cas {
		entries, err := l.List(c.filtre)
		if err != nil {
			t.Fatalf("%s: %v", c.nom, err)
		}
		if len(entries) != c.attendu {
			t.Errorf("%s: %d entrées, attendu %d", c.nom, len(entries), c.attendu)
			continue
		}
		if c.attendu > 0 && entries[0].ActionType != c.premier {
			t.Errorf("%s: première entrée = %q, attendu %q (les plus récentes d'abord)",
				c.nom, entries[0].ActionType, c.premier)
		}
	}
}

// TestPagination controle l'ordre anti-chronologique et les decalages.
func TestPagination(t *testing.T) {
	l, _ := ouvrirJournal(t)
	for i := 0; i < 5; i++ {
		if err := l.Record(entree("action", "compte")); err != nil {
			t.Fatal(err)
		}
	}

	page1, err := l.List(Filter{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	page2, err := l.List(Filter{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 || len(page2) != 2 {
		t.Fatalf("pagination: %d/%d entrées, attendu 2/2", len(page1), len(page2))
	}
	if page1[0].ID <= page1[1].ID || page1[1].ID <= page2[0].ID {
		t.Error("les entrées doivent être ordonnées par id décroissant")
	}
}

// TestPersistanceReouverture : un journal créé par un autre binaire (Python)
// doit se rouvrir avec le même schéma et les mêmes données.
func TestPersistanceReouverture(t *testing.T) {
	l, path := ouvrirJournal(t)
	l.SetContext("admin", "EDUSYNC.LOCAL")
	if err := l.Record(entree("suppression_compte", "eleve99")); err != nil {
		t.Fatal(err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("fermeture: %v", err)
	}

	l2, err := Open(path)
	if err != nil {
		t.Fatalf("réouverture: %v", err)
	}
	defer l2.Close() //nolint:errcheck

	if n, _ := l2.Count(); n != 1 {
		t.Fatalf("comptage après réouverture = %d, attendu 1", n)
	}
	entries, err := l2.List(Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if entries[0].Compte != "eleve99" {
		t.Errorf("compte = %q", entries[0].Compte)
	}
}

// TestSchemaCompatiblePython controle la présence des colonnes et tables du
// schéma Python (actions, pending_deletions, utilisateur, domaine,
// delai_jours).
func TestSchemaCompatiblePython(t *testing.T) {
	l, _ := ouvrirJournal(t)

	for _, table := range []string{"actions", "pending_deletions"} {
		cols, err := l.columns(table)
		if err != nil {
			t.Fatalf("schéma de %s: %v", table, err)
		}
		switch table {
		case "actions":
			for _, c := range []string{
				"id", "timestamp", "action_type", "compte", "ou_source",
				"ou_destination", "resultat", "session_id", "simulation",
				"detail", "utilisateur", "domaine",
			} {
				if !cols[c] {
					t.Errorf("colonne absente de actions: %s", c)
				}
			}
		case "pending_deletions":
			for _, c := range []string{
				"id", "user_dn", "sam_account_name", "nom_complet",
				"moved_at", "session_id", "delai_jours",
			} {
				if !cols[c] {
					t.Errorf("colonne absente de pending_deletions: %s", c)
				}
			}
		}
	}
}

// TestJournalAppendOnly : pas d'API de suppression. On vérifie ici que le
// compteur ne diminue jamais après nouvelle écriture.
func TestJournalAppendOnly(t *testing.T) {
	l, _ := ouvrirJournal(t)
	for i := 0; i < 3; i++ {
		if err := l.Record(entree("action", "compte")); err != nil {
			t.Fatal(err)
		}
	}
	if n, _ := l.Count(); n != 3 {
		t.Fatalf("comptage = %d, attendu 3", n)
	}
	if err := l.Record(entree("action", "compte")); err != nil {
		t.Fatal(err)
	}
	if n, _ := l.Count(); n != 4 {
		t.Errorf("comptage = %d, attendu 4", n)
	}
}

func boolPtr(b bool) *bool { return &b }
