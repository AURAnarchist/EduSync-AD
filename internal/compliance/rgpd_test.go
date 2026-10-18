package compliance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/audit"
	"github.com/estemobs/EduSync-AD/internal/models"
	"github.com/estemobs/EduSync-AD/internal/portal"
)

// sourcesTest construit un jeu de données personnelles « riche » : chaque
// compteur du rapport doit y trouver de quoi s'incrémenter.
func sourcesTest(maintenant time.Time) SourcesRGPD {
	ancien := maintenant.AddDate(-3, 0, 0).Format(formatISO)
	recent := maintenant.AddDate(0, -1, 0).Format(formatISO)
	journalAncien := maintenant.AddDate(-2, 0, 0).Format(formatISO)
	journalRecent := maintenant.AddDate(0, 0, -1).Format(formatISO)

	return SourcesRGPD{
		Comptes: []CompteRGPD{
			{Identifiant: "lea.martin", NomComplet: "Léa Martin", Mail: "lea.martin@college.fr",
				OU: "OU=6e_A,DC=lycee,DC=local", Actif: true, CreeLe: recent},
			{Identifiant: "ancien.eleve", NomComplet: "Ancien Élève", Mail: "",
				OU: "OU=Archives,DC=lycee,DC=local", Actif: false, CreeLe: ancien},
			{Identifiant: "prof", NomComplet: "Professeur Test", Mail: "prof@college.fr",
				OU: "OU=Personnels,DC=lycee,DC=local", Actif: true, CreeLe: recent},
		},
		Entrees: []audit.Entry{
			{ID: 1, ActionLogEntry: models.ActionLogEntry{Timestamp: journalAncien,
				ActionType: "create_user", Compte: "ancien.eleve", Utilisateur: "admin",
				Resultat: "succes"}},
			{ID: 2, ActionLogEntry: models.ActionLogEntry{Timestamp: journalRecent,
				ActionType: "reset_password", Compte: "lea.martin", Utilisateur: "admin",
				Resultat: "succes", Simulation: true, Detail: "lea.martin@college.fr"}},
			{ID: 3, ActionLogEntry: models.ActionLogEntry{Timestamp: journalRecent,
				ActionType: "create_user", Compte: "prof", Utilisateur: "aide",
				Resultat: "succes"}},
		},
		Demandes: []portal.Demande{
			{ID: "d1", Profil: "eleve", Prenom: "Léa", Nom: "Martin",
				Mail: "lea.martin@college.fr", Classe: "6e A", Statut: "en_attente",
				CreatedAt: journalRecent},
		},
		Secrets: []string{"lea.martin", "prof"},
	}
}

func TestGenererRapportRGPDInventaire(t *testing.T) {
	maintenant := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	rapport := GenererRapportRGPD(sourcesTest(maintenant), OptionsRGPD{Fin: maintenant}, maintenant)

	inventaire := rapport.Inventaire
	if inventaire.TotalComptes != 3 || inventaire.ComptesActifs != 2 || inventaire.ComptesDesactives != 1 {
		t.Fatalf("inventaire comptes = %+v", inventaire)
	}
	if inventaire.ComptesSansMail != 1 {
		t.Errorf("comptes sans mail = %d", inventaire.ComptesSansMail)
	}
	if inventaire.ComptesAnciens != 1 {
		t.Errorf("comptes anciens = %d", inventaire.ComptesAnciens)
	}
	if inventaire.EntreesJournal != 3 || inventaire.EntreesSimulation != 1 {
		t.Errorf("journal = %+v", inventaire)
	}
	if inventaire.EntreesAnciennes != 1 {
		t.Errorf("entrées anciennes = %d", inventaire.EntreesAnciennes)
	}
	if inventaire.Operateurs != 2 {
		t.Errorf("opérateurs = %d", inventaire.Operateurs)
	}
	if inventaire.DemandesPortail != 1 || inventaire.SecretsCoffre != 2 {
		t.Errorf("portail/coffre = %+v", inventaire)
	}

	if rapport.Periode.Fin != "2026-10-17T12:00:00+00:00" {
		t.Errorf("fin de période = %q", rapport.Periode.Fin)
	}
	if rapport.Periode.Debut == "" || rapport.GenerationDate == "" {
		t.Errorf("période incomplète : %+v", rapport.Periode)
	}
	if len(rapport.Sections) < 4 {
		t.Errorf("sections = %d", len(rapport.Sections))
	}
	for _, section := range rapport.Sections {
		if section.Categorie == "" || section.Source == "" || len(section.Champs) == 0 ||
			section.Finalite == "" || section.ConservationJours == 0 {
			t.Errorf("section incomplète : %+v", section)
		}
	}
	if len(rapport.DroitOubli) != 0 {
		t.Errorf("aucune recherche nominative n'était demandée : %+v", rapport.DroitOubli)
	}
}

func TestGenererRapportRGPDRecommandations(t *testing.T) {
	maintenant := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	rapport := GenererRapportRGPD(sourcesTest(maintenant), OptionsRGPD{Fin: maintenant}, maintenant)

	types := map[string]bool{}
	parNiveau := map[string]int{}
	for _, recommandation := range rapport.Recommandations {
		types[recommandation.Type] = true
		parNiveau[recommandation.Niveau]++
	}
	for _, attendu := range []string{"conservation", "anonymisation", "securite"} {
		if !types[attendu] {
			t.Errorf("type de recommandation absent : %s (%+v)", attendu, rapport.Recommandations)
		}
	}
	if parNiveau[NiveauAvertissement] == 0 {
		t.Errorf("aucune recommandation de niveau avertissement : %+v", rapport.Recommandations)
	}
}

func TestGenererRapportRGPDVide(t *testing.T) {
	maintenant := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	rapport := GenererRapportRGPD(SourcesRGPD{}, OptionsRGPD{}, maintenant)
	if len(rapport.Recommandations) != 1 ||
		!strings.Contains(rapport.Recommandations[0].Message, "Aucun écart") {
		t.Fatalf("recommandations = %+v", rapport.Recommandations)
	}
	if rapport.DroitOubli == nil {
		t.Error("la liste du droit à l'oubli doit être sérialisée en [] (non nil)")
	}
}

func TestChercherDroitOubli(t *testing.T) {
	maintenant := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	sources := sourcesTest(maintenant)
	rapport := GenererRapportRGPD(sources, OptionsRGPD{Fin: maintenant, Personne: "lea.martin"}, maintenant)

	sourcesVues := map[string]int{}
	for _, reference := range rapport.DroitOubli {
		sourcesVues[reference.Source]++
		if reference.Reference == "" || reference.Champ == "" {
			t.Errorf("référence incomplète : %+v", reference)
		}
	}
	// La personne doit apparaître dans l'annuaire, le journal, le portail et le
	// coffre.
	for _, source := range []string{"annuaire", "journal", "portail", "coffre"} {
		if sourcesVues[source] == 0 {
			t.Errorf("source %q absente : %+v", source, rapport.DroitOubli)
		}
	}

	// Recherche par adresse : la casse et l'espacement ne doivent pas gêner.
	parMail := ChercherDroitOubli(sources, "  LEA.MARTIN@COLLEGE.FR ")
	if len(parMail) == 0 {
		t.Error("la recherche par adresse doit trouver la personne")
	}

	// Personne inconnue.
	if refs := ChercherDroitOubli(sources, "inconnu.personne"); len(refs) != 0 {
		t.Errorf("références inattendues : %+v", refs)
	}
	// Identifiant vide.
	if refs := ChercherDroitOubli(sources, "   "); len(refs) != 0 {
		t.Errorf("un identifiant vide ne doit rien retourner : %+v", refs)
	}
}

func TestExporterRapportRGPDCSV(t *testing.T) {
	maintenant := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	rapport := GenererRapportRGPD(sourcesTest(maintenant),
		OptionsRGPD{Fin: maintenant, Personne: "lea.martin"}, maintenant)

	chemin := filepath.Join(t.TempDir(), "rgpd", "rapport.csv")
	obtenu, err := ExporterRapportRGPDCSV(rapport, chemin)
	if err != nil {
		t.Fatal(err)
	}
	if obtenu != chemin {
		t.Errorf("chemin = %q", obtenu)
	}

	info, err := os.Stat(chemin)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("permissions = %v, attendu 0600", info.Mode().Perm())
	}

	donnees, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	contenu := string(donnees)
	for _, attendu := range []string{
		"categorie,cle,valeur",
		"inventaire,total_comptes,3",
		"inventaire,secrets_coffre,2",
		"section,comptes annuaire,Active Directory",
		"droit_oubli,annuaire/lea.martin",
		"recommandation,",
	} {
		if !strings.Contains(contenu, attendu) {
			t.Errorf("CSV sans %q\n%s", attendu, contenu)
		}
	}

	if _, err := ExporterRapportRGPDCSV(rapport, "   "); err == nil {
		t.Error("un chemin vide doit être refusé")
	}
	// Écrire dans un dossier existant doit échouer proprement.
	dossier := filepath.Join(t.TempDir(), "deja-la")
	if err := os.Mkdir(dossier, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ExporterRapportRGPDCSV(rapport, dossier); err == nil {
		t.Error("écrire dans un dossier existant doit échouer")
	}
}
