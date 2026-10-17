package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/portal"
)

// nouveauPortailTest isole les dossiers de configuration et de données, puis
// retourne un chemin de base SQLite dédié.
func nouveauPortailTest(t *testing.T) string {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	return filepath.Join(t.TempDir(), "portail.db")
}

// TestPortalConfigAfficheEtEnregistre couvre la lecture, la modification ciblée
// et l'écriture des réglages.
func TestPortalConfigAfficheEtEnregistre(t *testing.T) {
	nouveauPortailTest(t)

	out, err := execution(t, "portal", "config", "--json")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var charge struct {
		Config map[string]any `json:"config"`
		File   string         `json:"file"`
		Errors []string       `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &charge); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if charge.Config["port"].(float64) != 8787 || charge.Config["actif"].(bool) {
		t.Fatalf("config par défaut = %+v", charge.Config)
	}
	if !strings.HasSuffix(charge.File, "portail.json") {
		t.Errorf("fichier = %q", charge.File)
	}

	// Les modifications ne sont pas écrites sans --save.
	if _, err := execution(t, "portal", "config", "--port", "9123"); err != nil {
		t.Fatalf("config --port: %v", err)
	}
	if _, err := os.Stat(charge.File); !os.IsNotExist(err) {
		t.Fatalf("le fichier ne doit pas exister sans --save : %v", err)
	}

	if _, err := execution(t, "portal", "config", "--port", "9123", "--host", "0.0.0.0",
		"--code-minutes", "7", "--tentatives-max", "4", "--envois-max", "2",
		"--politique", "personnel", "--ou-accueil", "OU=Eleves,DC=college,DC=fr",
		"--titre", "Portail du collège", "--actif", "--save"); err != nil {
		t.Fatalf("config --save: %v", err)
	}

	relu := portal.ChargerConfig(charge.File)
	if relu.Port != 9123 || relu.Hote != "0.0.0.0" || relu.CodeMinutes != 7 ||
		relu.TentativesMax != 4 || relu.EnvoisMax != 2 || relu.Politique != "personnel" ||
		relu.OUAccueil != "OU=Eleves,DC=college,DC=fr" || relu.Titre != "Portail du collège" ||
		!relu.Actif {
		t.Fatalf("réglages enregistrés = %+v", relu)
	}
	if erreurs := relu.Valider(); len(erreurs) != 0 {
		t.Fatalf("les réglages enregistrés doivent être valides : %v", erreurs)
	}

	// --reset repart des défauts.
	out, err = execution(t, "portal", "config", "--reset", "--json")
	if err != nil {
		t.Fatalf("config --reset: %v", err)
	}
	if !strings.Contains(out, `"port": 8787`) {
		t.Fatalf("réinitialisation: %s", out)
	}

	// Un port invalide est refusé à l'enregistrement.
	if _, err := execution(t, "portal", "config", "--port", "70000", "--save"); err == nil {
		t.Fatal("un port invalide doit être refusé")
	}
}

// TestPortalRequestsEtRefuse couvre la file des demandes : listage puis refus.
func TestPortalRequestsEtRefuse(t *testing.T) {
	base := nouveauPortailTest(t)

	store, err := portal.OuvrirStore(base)
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	demande, err := store.CreerDemande(portal.ProfilEleve, "Léa", "Martin",
		"lea.martin@college.fr", "6e A", "", time.Now())
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	if err := store.Fermer(); err != nil {
		t.Fatalf("fermeture: %v", err)
	}

	out, err := execution(t, "portal", "requests", "--db", base, "--json")
	if err != nil {
		t.Fatalf("requests: %v", err)
	}
	var demandes []map[string]any
	if err := json.Unmarshal([]byte(out), &demandes); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if len(demandes) != 1 || demandes[0]["id"] != demande.ID {
		t.Fatalf("demandes = %+v", demandes)
	}

	// Le listage texte et le filtre par statut fonctionnent.
	out, err = execution(t, "portal", "requests", "--db", base, "--statut", "approuvee")
	if err != nil {
		t.Fatalf("requests --statut: %v", err)
	}
	if !strings.Contains(out, "Aucune demande") {
		t.Fatalf("liste = %q", out)
	}

	// Sans --id, le refus est refusé.
	if _, err := execution(t, "portal", "refuse", "--db", base); err == nil {
		t.Fatal("--id est obligatoire")
	}

	if _, err := execution(t, "portal", "refuse", "--db", base, "--id", demande.ID,
		"--note", "doublon", "--json"); err != nil {
		t.Fatalf("refuse: %v", err)
	}
	store, err = portal.OuvrirStore(base)
	if err != nil {
		t.Fatalf("réouverture: %v", err)
	}
	defer store.Fermer()
	relue, err := store.DemandeParID(demande.ID)
	if err != nil || relue == nil || relue.Statut != portal.StatutRefusee ||
		relue.Note != "doublon" {
		t.Fatalf("demande = %+v (%v)", relue, err)
	}

	// Un second refus de la même demande échoue.
	if _, err := execution(t, "portal", "refuse", "--db", base, "--id", demande.ID); err == nil {
		t.Fatal("une demande déjà traitée ne peut pas être refusée deux fois")
	}
}

// TestPortalStatus couvre l'état hors ligne du portail.
func TestPortalStatus(t *testing.T) {
	base := nouveauPortailTest(t)

	store, err := portal.OuvrirStore(base)
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	if _, err := store.CreerCode(portal.ScopeConsultation, "l.martin",
		portal.Utilisateur{SAM: "l.martin"}, "123456", 10, time.Now()); err != nil {
		t.Fatalf("code: %v", err)
	}
	if _, err := store.CreerDemande(portal.ProfilPersonnel, "Jean", "Durand", "", "", "",
		time.Now()); err != nil {
		t.Fatalf("demande: %v", err)
	}
	_ = store.Fermer()

	out, err := execution(t, "portal", "status", "--db", base, "--json")
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	var etat map[string]any
	if err := json.Unmarshal([]byte(out), &etat); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if etat["service"] != "portail" || etat["base_url"] != "http://127.0.0.1:8787" ||
		etat["demandes_en_attente"].(float64) != 1 ||
		etat["code_minutes"].(float64) != 10 {
		t.Fatalf("état = %+v", etat)
	}
	if etat["base"] != base {
		t.Errorf("base = %v", etat["base"])
	}

	out, err = execution(t, "portal", "status", "--db", base)
	if err != nil {
		t.Fatalf("status texte: %v", err)
	}
	for _, attendu := range []string{"Service", "portail", "Demandes attente : 1", base} {
		if !strings.Contains(out, attendu) {
			t.Errorf("sortie sans %q :\n%s", attendu, out)
		}
	}
}

// TestPortalApproveSansSite vérifie qu'aucun site enregistré n'ouvre une
// connexion par accident.
func TestPortalApproveSansSite(t *testing.T) {
	base := nouveauPortailTest(t)

	store, err := portal.OuvrirStore(base)
	if err != nil {
		t.Fatalf("ouverture: %v", err)
	}
	demande, err := store.CreerDemande(portal.ProfilEleve, "Léa", "Martin", "", "", "",
		time.Now())
	if err != nil {
		t.Fatalf("création: %v", err)
	}
	_ = store.Fermer()

	if _, err := execution(t, "portal", "approve", "--db", base); err == nil {
		t.Fatal("--id est obligatoire")
	}
	_, err = execution(t, "portal", "approve", "--db", base, "--id", demande.ID)
	if err == nil || !strings.Contains(err.Error(), "aucun site enregistré") {
		t.Fatalf("erreur = %v", err)
	}

	// Le site demandé doit exister dans domaines.json.
	_, err = execution(t, "portal", "approve", "--db", base, "--id", demande.ID,
		"--site", "inconnu")
	if err == nil || !strings.Contains(err.Error(), "aucun site enregistré") {
		t.Fatalf("site inconnu : %v", err)
	}
}

// TestPortalSousCommandeInconnue vérifie les messages d'erreur d'usage.
func TestPortalSousCommandeInconnue(t *testing.T) {
	nouveauPortailTest(t)

	if _, err := execution(t, "portal"); err == nil {
		t.Fatal("sans sous-commande, une erreur est attendue")
	}
	_, err := execution(t, "portal", "bidon")
	if err == nil || !strings.Contains(err.Error(), "sous-commande inconnue") {
		t.Fatalf("erreur = %v", err)
	}
}
