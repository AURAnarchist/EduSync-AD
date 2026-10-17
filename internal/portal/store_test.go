package portal

import (
	"path/filepath"
	"testing"
	"time"
)

// nouveauStoreTest ouvre un magasin dans un répertoire temporaire.
func nouveauStoreTest(t *testing.T) *Store {
	t.Helper()
	store, err := OuvrirStore(filepath.Join(t.TempDir(), "portail.db"))
	if err != nil {
		t.Fatalf("ouverture du magasin: %v", err)
	}
	t.Cleanup(func() { _ = store.Fermer() })
	return store
}

// utilisateurTest est un compte de test.
func utilisateurTest() Utilisateur {
	return Utilisateur{
		DN:   "CN=Léa Martin,OU=Eleves,DC=college,DC=fr",
		SAM:  "l.martin",
		CN:   "Léa Martin",
		Mail: "lea.martin@college.fr",
	}
}

// TestStoreSchemaReprisTelQuel vérifie que le schéma est bien celui attendu par
// la version Python (deux tables, mêmes colonnes).
func TestStoreSchemaReprisTelQuel(t *testing.T) {
	store := nouveauStoreTest(t)
	for _, table := range []string{"codes", "demandes"} {
		var nom string
		err := store.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type = 'table' AND name = ?",
			table).Scan(&nom)
		if err != nil || nom != table {
			t.Fatalf("table %s absente : %v", table, err)
		}
	}

	colonnes := map[string][]string{}
	rows, err := store.db.Query("SELECT name FROM pragma_table_info('codes')")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var nom string
		if err := rows.Scan(&nom); err != nil {
			t.Fatal(err)
		}
		colonnes["codes"] = append(colonnes["codes"], nom)
	}
	attendues := []string{"id", "scope", "cible", "dn", "sam", "cn", "mail",
		"disabled", "salt", "digest", "expires_at", "attempts", "consumed"}
	if len(colonnes["codes"]) != len(attendues) {
		t.Fatalf("colonnes = %v", colonnes["codes"])
	}
	for i, attendu := range attendues {
		if colonnes["codes"][i] != attendu {
			t.Fatalf("colonne %d = %q, attendu %q", i, colonnes["codes"][i], attendu)
		}
	}
}

// TestStoreCycleDeVieCode couvre l'émission, la vérification, le plafond
// d'essais et la consommation unique d'un code.
func TestStoreCycleDeVieCode(t *testing.T) {
	store := nouveauStoreTest(t)
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	utilisateur := utilisateurTest()

	ticket, err := store.CreerCode(ScopeReinitialisation, "l.martin", utilisateur,
		"123456", 10, moment)
	if err != nil {
		t.Fatalf("CreerCode: %v", err)
	}
	if len(ticket) != 12 {
		t.Fatalf("ticket = %q", ticket)
	}

	// Le code n'est jamais stocké en clair.
	var empreinte, sel []byte
	if err := store.db.QueryRow("SELECT digest, salt FROM codes WHERE id = ?",
		ticket).Scan(&empreinte, &sel); err != nil {
		t.Fatal(err)
	}
	if len(sel) != 16 || len(empreinte) != 32 {
		t.Fatalf("sel = %d octets, empreinte = %d octets", len(sel), len(empreinte))
	}
	if string(empreinte) == "123456" {
		t.Fatal("le code ne doit jamais être stocké en clair")
	}

	// Mauvais code : refusé, mais la tentative est comptée.
	valide, raison, compte, err := store.VerifierCode(ticket, "000000", 5, moment)
	if err != nil || valide || raison != raisonCodeIncorrect || compte != nil {
		t.Fatalf("mauvais code : %v %q %v %v", valide, raison, compte, err)
	}
	var tentatives int
	if err := store.db.QueryRow("SELECT attempts FROM codes WHERE id = ?",
		ticket).Scan(&tentatives); err != nil || tentatives != 1 {
		t.Fatalf("tentatives = %d (%v)", tentatives, err)
	}

	// Bon code : accepté, sans consommation.
	valide, raison, compte, err = store.VerifierCode(ticket, "123456", 5, moment)
	if err != nil || !valide || raison != raisonOK {
		t.Fatalf("bon code : %v %q %v", valide, raison, err)
	}
	if compte == nil || compte.SAM != "l.martin" || compte.Mail != "lea.martin@college.fr" {
		t.Fatalf("compte = %+v", compte)
	}

	// Consommation unique.
	consomme, err := store.ConsommerCode(ticket)
	if err != nil || !consomme {
		t.Fatalf("première consommation : %v %v", consomme, err)
	}
	if consomme, err = store.ConsommerCode(ticket); err != nil || consomme {
		t.Fatalf("seconde consommation : %v %v", consomme, err)
	}
	if valide, raison, _, _ = store.VerifierCode(ticket, "123456", 5, moment); valide ||
		raison != raisonConsomme {
		t.Fatalf("code consommé : %v %q", valide, raison)
	}
}

// TestStoreExpirationEtPlafond vérifie l'expiration et le plafond de
// tentatives (les tickets à bout sont épuisés, pas rejouables).
func TestStoreExpirationEtPlafond(t *testing.T) {
	store := nouveauStoreTest(t)
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	ticket, err := store.CreerCode(ScopeConsultation, "l.martin", utilisateurTest(),
		"111111", 10, moment)
	if err != nil {
		t.Fatal(err)
	}
	if valide, raison, _, _ := store.VerifierCode(ticket, "111111", 5,
		moment.Add(11*time.Minute)); valide || raison != raisonExpire {
		t.Fatalf("code expiré : %v %q", valide, raison)
	}
	// Juste avant l'échéance, il est encore valable.
	if valide, raison, _, _ := store.VerifierCode(ticket, "111111", 5,
		moment.Add(9*time.Minute)); !valide || raison != raisonOK {
		t.Fatalf("code valide : %v %q", valide, raison)
	}

	ticket2, err := store.CreerCode(ScopeConsultation, "l.martin", utilisateurTest(),
		"222222", 10, moment)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if _, _, _, err := store.VerifierCode(ticket2, "000000", 2, moment); err != nil {
			t.Fatal(err)
		}
	}
	if valide, raison, _, _ := store.VerifierCode(ticket2, "222222", 2, moment); valide ||
		raison != raisonTropDeTentatives {
		t.Fatalf("plafond : %v %q", valide, raison)
	}

	// Un ticket inconnu n'est jamais valide.
	if valide, raison, _, _ := store.VerifierCode("inconnu", "222222", 5, moment); valide ||
		raison != raisonInconnu {
		t.Fatalf("ticket inconnu : %v %q", valide, raison)
	}
}

// TestStoreLimitationEnvois vérifie la fenêtre de limitation par identifiant.
func TestStoreLimitationEnvois(t *testing.T) {
	store := nouveauStoreTest(t)
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		if _, err := store.CreerCode(ScopeReinitialisation, "l.martin",
			utilisateurTest(), "123456", 10, moment.Add(time.Duration(i)*time.Second)); err != nil {
			t.Fatal(err)
		}
	}
	n, err := store.CompterCodesRecents("l.martin", moment.Add(time.Minute))
	if err != nil || n != 3 {
		t.Fatalf("codes récents = %d (%v)", n, err)
	}
	// Un autre identifiant n'est pas concerné.
	if n, _ := store.CompterCodesRecents("j.durand", moment.Add(time.Minute)); n != 0 {
		t.Fatalf("codes récents = %d", n)
	}
	// La limitation couvre en pratique la durée de vie du code (10 min) **et**
	// la fenêtre glissante (15 min), puisque le comptage porte sur les codes
	// non encore expirés à la date de référence — comme `count_recent_codes`.
	if n, _ := store.CompterCodesRecents("l.martin",
		moment.Add((FenetreLimitationMinutes+10+1)*time.Minute)); n != 0 {
		t.Fatalf("codes récents hors fenêtre = %d", n)
	}
}

// TestStoreDemandes couvre la création, la lecture, le filtrage et la mise à
// jour du statut.
func TestStoreDemandes(t *testing.T) {
	store := nouveauStoreTest(t)
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	demande, err := store.CreerDemande(ProfilEleve, " Léa ", " Martin ",
		"lea.martin@college.fr", " 6e A ", "  nouvelle élève  ", moment)
	if err != nil {
		t.Fatalf("CreerDemande: %v", err)
	}
	if len(demande.ID) != 8 {
		t.Fatalf("identifiant de demande = %q", demande.ID)
	}
	if demande.Prenom != "Léa" || demande.Nom != "Martin" || demande.Classe != "6e A" ||
		demande.Motif != "nouvelle élève" || demande.Statut != StatutEnAttente ||
		demande.CreatedAt != "2026-10-17T12:00:00+00:00" {
		t.Fatalf("demande = %+v", demande)
	}

	relue, err := store.DemandeParID(demande.ID)
	if err != nil || relue == nil || *relue != *demande {
		t.Fatalf("relecture = %+v (%v)", relue, err)
	}
	if absente, err := store.DemandeParID("inconnue"); err != nil || absente != nil {
		t.Fatalf("demande inconnue = %+v (%v)", absente, err)
	}

	if _, err := store.CreerDemande(ProfilPersonnel, "Jean", "Durand", "", "", "",
		moment.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	attentes, err := store.ListerDemandes(StatutEnAttente)
	if err != nil || len(attentes) != 2 {
		t.Fatalf("demandes en attente = %d (%v)", len(attentes), err)
	}
	// Les plus récentes d'abord.
	if attentes[0].Nom != "Durand" {
		t.Fatalf("ordre = %v", attentes)
	}
	if toutes, _ := store.ListerDemandes(""); len(toutes) != 2 {
		t.Fatalf("toutes les demandes = %d", len(toutes))
	}

	maj, err := store.DefinirStatutDemande(demande.ID, StatutApprouvee, "interface",
		"compte créé", moment.Add(2*time.Minute))
	if err != nil || maj == nil {
		t.Fatalf("DefinirStatutDemande: %v", err)
	}
	if maj.Statut != StatutApprouvee || maj.TraitePar != "interface" ||
		maj.Note != "compte créé" || maj.TraiteLe != "2026-10-17T12:02:00+00:00" {
		t.Fatalf("demande mise à jour = %+v", maj)
	}
	if attentes, _ := store.ListerDemandes(StatutEnAttente); len(attentes) != 1 {
		t.Fatalf("demandes en attente = %d", len(attentes))
	}
	if approuvees, _ := store.ListerDemandes(StatutApprouvee); len(approuvees) != 1 {
		t.Fatalf("demandes approuvées = %d", len(approuvees))
	}
}

// TestStoreFermetureIdempotente vérifie qu'une fermeture double ne panique
// pas et qu'un magasin fermé refuse les écritures.
func TestStoreFermetureIdempotente(t *testing.T) {
	store := nouveauStoreTest(t)
	if err := store.Fermer(); err != nil {
		t.Fatalf("Fermer: %v", err)
	}
	if err := store.Fermer(); err != nil {
		t.Fatalf("Fermer (2e): %v", err)
	}
	if _, err := store.CreerDemande(ProfilEleve, "Léa", "Martin", "", "", "",
		time.Now()); err == nil {
		t.Fatal("un magasin fermé doit refuser les écritures")
	}
	if _, err := store.ListerDemandes(""); err == nil {
		t.Fatal("un magasin fermé doit refuser les lectures")
	}
}
