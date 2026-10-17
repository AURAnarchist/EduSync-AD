package main

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// configIsolee isole la configuration utilisateur dans un dossier temporaire :
// l'assistant lit `config.json` pour connaître la charte d'identifiants, les
// tests ne doivent donc pas dépendre du poste qui les exécute.
func configIsolee(t *testing.T) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
}

func TestAssistantSmartID(t *testing.T) {
	configIsolee(t)

	out, err := execution(t, "assistant", "smart-id", "--prenom", "Thomas", "--nom", "Martin")
	if err != nil {
		t.Fatalf("smart-id: %v", err)
	}
	if strings.TrimSpace(out) != "thomas.martin" {
		t.Fatalf("sortie = %q", out)
	}

	out, err = execution(t, "assistant", "smart-id", "--prenom", "Thomas", "--nom", "Martin",
		"--format", "prenomNom")
	if err != nil {
		t.Fatalf("smart-id: %v", err)
	}
	if strings.TrimSpace(out) != "thomasMartin" {
		t.Fatalf("sortie = %q", out)
	}
}

func TestAssistantSmartIDDoublon(t *testing.T) {
	configIsolee(t)

	out, err := execution(t, "assistant", "smart-id", "--prenom", "Thomas", "--nom", "Martin",
		"--existing", "thomas.martin, autre.identifiant", "--json")
	if err != nil {
		t.Fatalf("smart-id: %v", err)
	}
	var resultat struct {
		Identifiant string `json:"identifiant"`
		Doublon     bool   `json:"doublon"`
		Format      string `json:"format"`
	}
	if err := json.Unmarshal([]byte(out), &resultat); err != nil {
		t.Fatalf("JSON invalide (%s): %v", out, err)
	}
	if resultat.Identifiant != "thomas.martin2" || !resultat.Doublon {
		t.Fatalf("résultat = %+v", resultat)
	}
	if resultat.Format != "prenom.nom" {
		t.Fatalf("format = %q", resultat.Format)
	}
}

func TestAssistantSmartIDOptionsObligatoires(t *testing.T) {
	configIsolee(t)

	if _, err := execution(t, "assistant", "smart-id", "--nom", "Martin"); err == nil {
		t.Fatal("une erreur est attendue sans --prenom")
	}
	if _, err := execution(t, "assistant", "smart-id", "--prenom", "Thomas"); err == nil {
		t.Fatal("une erreur est attendue sans --nom")
	}
}

func TestAssistantSecurity(t *testing.T) {
	configIsolee(t)

	out, err := execution(t, "assistant", "security", "--json")
	if err != nil {
		t.Fatalf("security: %v", err)
	}
	var rapport struct {
		Score           int `json:"score"`
		Recommandations []struct {
			Type    string `json:"type"`
			Niveau  string `json:"niveau"`
			Message string `json:"message"`
		} `json:"recommandations"`
	}
	if err := json.Unmarshal([]byte(out), &rapport); err != nil {
		t.Fatalf("JSON invalide (%s): %v", out, err)
	}
	if rapport.Score <= 0 || rapport.Score > 100 {
		t.Fatalf("score = %d", rapport.Score)
	}
}

func TestAssistantSecuritySuperAdmin(t *testing.T) {
	configIsolee(t)

	out, err := execution(t, "assistant", "security", "--role", "super_admin", "--portees-vides", "--json")
	if err != nil {
		t.Fatalf("security: %v", err)
	}
	if !strings.Contains(out, "\"type\": \"rbac\"") {
		t.Fatalf("recommandation RBAC absente : %s", out)
	}
}

func TestAssistantJournal(t *testing.T) {
	configIsolee(t)

	chemin := filepath.Join(t.TempDir(), "journal.db")
	journal, err := audit.Open(chemin)
	if err != nil {
		t.Fatalf("ouverture du journal: %v", err)
	}
	entrees := []models.ActionLogEntry{
		{Timestamp: "2026-10-17T09:00:00+00:00", ActionType: "create_user",
			Resultat: "succes", Utilisateur: "admin"},
		{Timestamp: "2026-10-17T23:00:00+00:00", ActionType: "delete_user",
			Resultat: "echec", Utilisateur: "aide"},
	}
	for _, entree := range entrees {
		if err := journal.Record(entree); err != nil {
			t.Fatalf("écriture du journal: %v", err)
		}
	}
	if err := journal.Close(); err != nil {
		t.Fatalf("fermeture du journal: %v", err)
	}

	out, err := execution(t, "assistant", "journal", "--journal", chemin, "--json")
	if err != nil {
		t.Fatalf("journal: %v", err)
	}
	var rapport struct {
		Total        int `json:"total"`
		Echecs       int `json:"echecs"`
		HorsHoraires int `json:"hors_horaires"`
	}
	if err := json.Unmarshal([]byte(out), &rapport); err != nil {
		t.Fatalf("JSON invalide (%s): %v", out, err)
	}
	if rapport.Total != 2 || rapport.Echecs != 1 || rapport.HorsHoraires != 1 {
		t.Fatalf("rapport = %+v", rapport)
	}
}

func TestAssistantAnomaliesSansSite(t *testing.T) {
	configIsolee(t)

	// Aucun site enregistré : la commande doit expliquer quoi faire plutôt que
	// de tenter une connexion.
	_, err := execution(t, "assistant", "anomalies")
	if err == nil || !strings.Contains(err.Error(), "aucun site enregistré") {
		t.Fatalf("erreur = %v", err)
	}

	_, err = execution(t, "assistant", "cleanup")
	if err == nil || !strings.Contains(err.Error(), "aucun site enregistré") {
		t.Fatalf("erreur = %v", err)
	}
}

func TestAssistantSousCommandeInconnue(t *testing.T) {
	configIsolee(t)

	if _, err := execution(t, "assistant", "inconnue"); err == nil {
		t.Fatal("une erreur est attendue pour une sous-commande inconnue")
	}
	if _, err := execution(t, "assistant"); err == nil {
		t.Fatal("une erreur est attendue sans sous-commande")
	}
}
