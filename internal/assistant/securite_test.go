package assistant

import (
	"testing"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
	"github.com/estemobs/EduSync-AD/internal/rbac"
)

func politiqueSecurisee() models.PasswordPolicy {
	return models.PasswordPolicy{Longueur: 12, Majuscules: true, Chiffres: true, CaracteresSpeciaux: true}
}

func TestAnalyserSecuriteSansAlerte(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.PolitiqueMDPEleve = politiqueSecurisee()
	cfg.PolitiqueMDPPersonnel = politiqueSecurisee()

	rapport := AnalyserSecurite(cfg, ContexteSecurite{})
	if rapport.Score != 100 {
		t.Fatalf("score = %d (%+v)", rapport.Score, rapport.Recommandations)
	}
}

func TestAnalyserSecuritePolitiqueCourte(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.PolitiqueMDPEleve = models.PasswordPolicy{Longueur: 6, Majuscules: true, Chiffres: true}
	cfg.PolitiqueMDPPersonnel = politiqueSecurisee()

	rapport := AnalyserSecurite(cfg, ContexteSecurite{})
	if rapport.Score >= 100 {
		t.Fatalf("score = %d, attendu < 100", rapport.Score)
	}
	trouve := false
	for _, recommandation := range rapport.Recommandations {
		if recommandation.Type == "mdp" && recommandation.Niveau == GraviteCritique {
			trouve = true
		}
	}
	if !trouve {
		t.Fatalf("recommandation critique absente : %+v", rapport.Recommandations)
	}
}

func TestAnalyserSecuriteSuperAdminEtPorteesVides(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.PolitiqueMDPEleve = politiqueSecurisee()
	cfg.PolitiqueMDPPersonnel = politiqueSecurisee()

	rapport := AnalyserSecurite(cfg, ContexteSecurite{
		Role:         string(rbac.SuperAdmin),
		PorteesVides: true,
	})
	types := map[string]bool{}
	for _, recommandation := range rapport.Recommandations {
		types[recommandation.Type] = true
	}
	if !types["rbac"] {
		t.Fatalf("recommandations RBAC absentes : %+v", rapport.Recommandations)
	}
	if rapport.Score != 100-penaliteInfo-penaliteAvertissement {
		t.Fatalf("score = %d", rapport.Score)
	}
}

func TestAnalyserSecuriteComptesDesactives(t *testing.T) {
	cfg := config.DefaultAppConfig()
	cfg.PolitiqueMDPEleve = politiqueSecurisee()
	cfg.PolitiqueMDPPersonnel = politiqueSecurisee()

	rapport := AnalyserSecurite(cfg, ContexteSecurite{ComptesTotal: 10, ComptesDesactives: 4})
	if len(rapport.Recommandations) != 1 || rapport.Recommandations[0].Type != "annuaire" {
		t.Fatalf("recommandations = %+v", rapport.Recommandations)
	}
	// Sous le seuil de 20 %, aucune recommandation.
	rapport = AnalyserSecurite(cfg, ContexteSecurite{ComptesTotal: 10, ComptesDesactives: 1})
	if len(rapport.Recommandations) != 0 {
		t.Fatalf("recommandations = %+v", rapport.Recommandations)
	}
}

func TestAnalyserJournal(t *testing.T) {
	entrees := []audit.Entry{
		{ActionLogEntry: models.ActionLogEntry{Timestamp: "2026-10-17T09:00:00+00:00",
			ActionType: "create_user", Resultat: "succes", Utilisateur: "admin"}},
		{ActionLogEntry: models.ActionLogEntry{Timestamp: "2026-10-17T09:05:00+00:00",
			ActionType: "create_user", Resultat: "succes", Utilisateur: "admin"}},
		{ActionLogEntry: models.ActionLogEntry{Timestamp: "2026-10-17T22:30:00+00:00",
			ActionType: "delete_user", Resultat: "echec", Utilisateur: "aide"}},
		{ActionLogEntry: models.ActionLogEntry{Timestamp: "2026-10-17T10:00:00+00:00",
			ActionType: "reset_password", Resultat: "succes", Utilisateur: "aide", Simulation: true}},
	}
	rapport := AnalyserJournal(entrees)

	if rapport.Total != 4 || rapport.Simulations != 1 || rapport.Echecs != 1 {
		t.Fatalf("rapport = %+v", rapport)
	}
	if rapport.HorsHoraires != 1 {
		t.Fatalf("hors horaires = %d", rapport.HorsHoraires)
	}
	if len(rapport.ParType) == 0 || rapport.ParType[0].Cle != "create_user" || rapport.ParType[0].Nombre != 2 {
		t.Fatalf("par type = %+v", rapport.ParType)
	}
	if len(rapport.ParOperateur) == 0 || rapport.ParOperateur[0].Cle != "admin" {
		t.Fatalf("par opérateur = %+v", rapport.ParOperateur)
	}
	if len(rapport.Recommandations) == 0 {
		t.Fatalf("aucune recommandation sur un journal dégradé")
	}
}

func TestAnalyserJournalSain(t *testing.T) {
	entrees := make([]audit.Entry, 0, 20)
	for index := 0; index < 20; index++ {
		entrees = append(entrees, audit.Entry{ActionLogEntry: models.ActionLogEntry{
			Timestamp: "2026-10-17T10:00:00+00:00", ActionType: "create_user",
			Resultat: "succes", Utilisateur: "admin",
		}})
	}
	rapport := AnalyserJournal(entrees)
	if rapport.Echecs != 0 || rapport.HorsHoraires != 0 || len(rapport.Recommandations) != 0 {
		t.Fatalf("rapport = %+v", rapport)
	}
	// Les listes doivent être sérialisées en `[]` (et non `null`) pour rester
	// consommables directement par un client JSON.
	if rapport.Recommandations == nil || rapport.ParType == nil {
		t.Fatalf("listes nil : %+v", rapport)
	}
}

func TestHorsHoraires(t *testing.T) {
	cas := []struct {
		horodatage string
		attendu    bool
	}{
		{"2026-10-17T06:59:00+00:00", true},
		{"2026-10-17T07:00:00+00:00", false},
		{"2026-10-17T19:59:00+00:00", false},
		{"2026-10-17T20:00:00+00:00", true},
		{"2026-10-17T12:00:00", false},
		{"pas un horodatage", false},
	}
	for _, c := range cas {
		if got := horsHoraires(c.horodatage); got != c.attendu {
			t.Errorf("horsHoraires(%q) = %v", c.horodatage, got)
		}
	}
}

func TestResultatPositif(t *testing.T) {
	for _, valeur := range []string{"", "succes", "Succès", "OK", "SUCCESS"} {
		if !resultatPositif(valeur) {
			t.Errorf("resultatPositif(%q) = false", valeur)
		}
	}
	for _, valeur := range []string{"echec", "erreur", "refusé"} {
		if resultatPositif(valeur) {
			t.Errorf("resultatPositif(%q) = true", valeur)
		}
	}
}

func TestCompteursTries(t *testing.T) {
	compteurs := compteursTries(map[string]int{"b": 2, "a": 2, "c": 5})
	if len(compteurs) != 3 || compteurs[0].Cle != "c" {
		t.Fatalf("compteurs = %+v", compteurs)
	}
	if compteurs[1].Cle != "a" || compteurs[2].Cle != "b" {
		t.Fatalf("ordre instable : %+v", compteurs)
	}
}
