package assistant

import (
	"testing"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// inventaireTest décrit un annuaire volontairement « abîmé » : chaque
// vérification doit y trouver son cas.
func inventaireTest() Inventaire {
	return Inventaire{
		BaseDN: "DC=lycee,DC=local",
		Comptes: []Compte{
			{DN: "CN=Léa Martin,OU=6e_A,OU=Eleves,DC=lycee,DC=local", Identifiant: "lea.martin",
				NomComplet: "Léa Martin", Actif: true, OU: "OU=6e_A,OU=Eleves,DC=lycee,DC=local"},
			{DN: "CN=Léo Martin,OU=6e_A,OU=Eleves,DC=lycee,DC=local", Identifiant: "LEA.MARTIN",
				NomComplet: "Léo Martin", Actif: true, OU: "OU=6e_A,OU=Eleves,DC=lycee,DC=local"},
			{DN: "CN=Ancien Élève,OU=6e_Z,OU=Eleves,DC=lycee,DC=local", Identifiant: "ancien.eleve",
				NomComplet: "Ancien Élève", Actif: false, OU: "OU=6e_Z,OU=Eleves,DC=lycee,DC=local"},
			{DN: "CN=Compte Technique,CN=Users,DC=lycee,DC=local", Identifiant: "technique",
				NomComplet: "Compte Technique", Actif: true, OU: "CN=Users,DC=lycee,DC=local"},
		},
		Groupes: []Groupe{
			{DN: "CN=6e_A,OU=Groupes,DC=lycee,DC=local", Nom: "6e_A",
				Membres: []string{"CN=Léa Martin,OU=6e_A,OU=Eleves,DC=lycee,DC=local"},
				OU:      "OU=Groupes,DC=lycee,DC=local"},
			{DN: "CN=Vide,OU=Groupes,DC=lycee,DC=local", Nom: "Vide",
				OU: "OU=Groupes,DC=lycee,DC=local"},
		},
		Unites: []Unite{
			{DN: "OU=Eleves,DC=lycee,DC=local", Nom: "Eleves", ParentDN: "DC=lycee,DC=local"},
			{DN: "OU=6e_A,OU=Eleves,DC=lycee,DC=local", Nom: "6e_A", ParentDN: "OU=Eleves,DC=lycee,DC=local"},
			{DN: "OU=Groupes,DC=lycee,DC=local", Nom: "Groupes", ParentDN: "DC=lycee,DC=local"},
			{DN: "OU=Archives,DC=lycee,DC=local", Nom: "Archives", ParentDN: "DC=lycee,DC=local"},
		},
	}
}

func TestDetecterAnomaliesIdentifiantsDoubles(t *testing.T) {
	anomalies := DetecterAnomalies(inventaireTest(), OptionsAnomalies{Doublons: true})
	if len(anomalies) != 1 {
		t.Fatalf("attendu 1 anomalie, obtenu %d (%+v)", len(anomalies), anomalies)
	}
	if anomalies[0].Type != AnomalieIdentifiantsDoubles || anomalies[0].Gravite != GraviteCritique {
		t.Fatalf("anomalie = %+v", anomalies[0])
	}
	if len(anomalies[0].Objets) != 2 {
		t.Fatalf("objets = %v", anomalies[0].Objets)
	}
}

func TestDetecterAnomaliesComptesOrphelins(t *testing.T) {
	anomalies := DetecterAnomalies(inventaireTest(), OptionsAnomalies{ComptesOrphelins: true})
	if len(anomalies) != 1 || anomalies[0].Type != AnomalieComptesOrphelins {
		t.Fatalf("anomalies = %+v", anomalies)
	}
	// Le compte de « 6e_Z » a une OU inexistante ; celui de « CN=Users » est
	// dans un conteneur par défaut et ne doit pas être signalé.
	if len(anomalies[0].Objets) != 1 ||
		anomalies[0].Objets[0] != "CN=Ancien Élève,OU=6e_Z,OU=Eleves,DC=lycee,DC=local" {
		t.Fatalf("objets = %v", anomalies[0].Objets)
	}
}

func TestDetecterAnomaliesSansOUConnue(t *testing.T) {
	// Inventaire partiel : aucune OU collectée → pas de faux orphelins.
	inventaire := Inventaire{
		BaseDN:  "DC=lycee,DC=local",
		Comptes: []Compte{{DN: "CN=X,OU=Inexistante,DC=lycee,DC=local", Identifiant: "x"}},
	}
	if anomalies := DetecterAnomalies(inventaire, OptionsAnomalies{ComptesOrphelins: true}); len(anomalies) != 0 {
		t.Fatalf("anomalies = %+v", anomalies)
	}
}

func TestDetecterAnomaliesOUsVides(t *testing.T) {
	anomalies := DetecterAnomalies(inventaireTest(), OptionsAnomalies{OUsVides: true})
	if len(anomalies) != 1 || anomalies[0].Type != AnomalieOUsVides {
		t.Fatalf("anomalies = %+v", anomalies)
	}
	if len(anomalies[0].Objets) != 1 || anomalies[0].Objets[0] != "OU=Archives,DC=lycee,DC=local" {
		t.Fatalf("objets = %v", anomalies[0].Objets)
	}
}

func TestDetecterAnomaliesGroupesSansMembres(t *testing.T) {
	anomalies := DetecterAnomalies(inventaireTest(), OptionsAnomalies{GroupesSansMembres: true})
	if len(anomalies) != 1 || anomalies[0].Type != AnomalieGroupesSansMembres {
		t.Fatalf("anomalies = %+v", anomalies)
	}
	if anomalies[0].Objets[0] != "CN=Vide,OU=Groupes,DC=lycee,DC=local" {
		t.Fatalf("objets = %v", anomalies[0].Objets)
	}
}

func TestDetecterAnomaliesToutes(t *testing.T) {
	anomalies := DetecterAnomalies(inventaireTest(), OptionsAnomaliesToutes())
	if len(anomalies) != 4 {
		t.Fatalf("attendu 4 anomalies, obtenu %d (%+v)", len(anomalies), anomalies)
	}
	// Sortie stable et ordonnée par type.
	types := []string{}
	for _, anomalie := range anomalies {
		types = append(types, anomalie.Type)
	}
	attendus := map[string]bool{
		AnomalieIdentifiantsDoubles: true, AnomalieComptesOrphelins: true,
		AnomalieOUsVides: true, AnomalieGroupesSansMembres: true,
	}
	for _, nom := range types {
		if !attendus[nom] {
			t.Fatalf("type inattendu %q", nom)
		}
	}
}

func TestOptionsAnomaliesVidesNeTrouventRien(t *testing.T) {
	if anomalies := DetecterAnomalies(inventaireTest(), OptionsAnomalies{}); len(anomalies) != 0 {
		t.Fatalf("anomalies = %+v", anomalies)
	}
}

func TestSuggererNettoyage(t *testing.T) {
	suggestions := SuggererNettoyage(inventaireTest(), OptionsNettoyageToutes())
	parType := map[string]Suggestion{}
	for _, suggestion := range suggestions {
		parType[suggestion.Type] = suggestion
	}
	if _, ok := parType[SuggestionComptesDesactives]; !ok {
		t.Errorf("suggestion d'archivage absente : %+v", suggestions)
	}
	if _, ok := parType[SuggestionGroupesVides]; !ok {
		t.Errorf("suggestion de suppression de groupe absente : %+v", suggestions)
	}
	if _, ok := parType[SuggestionOUsVides]; !ok {
		t.Errorf("suggestion de suppression d'OU absente : %+v", suggestions)
	}
	if _, ok := parType[SuggestionComptesOrphelins]; !ok {
		t.Errorf("suggestion de déplacement absente : %+v", suggestions)
	}
}

func TestSuggererNettoyageSelectionne(t *testing.T) {
	suggestions := SuggererNettoyage(inventaireTest(), OptionsNettoyage{GroupesVides: true})
	if len(suggestions) != 1 || suggestions[0].Type != SuggestionGroupesVides {
		t.Fatalf("suggestions = %+v", suggestions)
	}
}

func TestGenererIdentifiantIntelligent(t *testing.T) {
	cfg := config.DefaultAppConfig()
	identifiant, doublon, err := GenererIdentifiantIntelligent(cfg, "Thomas", "Martin", "", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.martin" || doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}

	// Format imposé (préréglage camelCase).
	identifiant, _, err = GenererIdentifiantIntelligent(cfg, "Thomas", "Martin", "prenomNom", map[string]bool{})
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomasMartin" {
		t.Fatalf("format camel : %q", identifiant)
	}

	// Doublon résolu.
	identifiant, doublon, err = GenererIdentifiantIntelligent(cfg, "Thomas", "Martin", "prenom.nom",
		map[string]bool{"thomas.martin": true})
	if err != nil {
		t.Fatal(err)
	}
	if identifiant != "thomas.martin2" || !doublon {
		t.Fatalf("obtenu %q (doublon=%v)", identifiant, doublon)
	}
}

func TestParentDN(t *testing.T) {
	if got := parentDN("CN=Léa Martin,OU=6e_A,OU=Eleves,DC=lycee,DC=local"); got != "OU=6e_A,OU=Eleves,DC=lycee,DC=local" {
		t.Errorf("parentDN = %q", got)
	}
	// Virgule échappée dans un RDN : elle ne sépare pas.
	if got := parentDN(`CN=Dupont\, Jean,OU=Eleves,DC=lycee,DC=local`); got != "OU=Eleves,DC=lycee,DC=local" {
		t.Errorf("parentDN = %q", got)
	}
	if got := parentDN("DC=lycee,DC=local"); got != "DC=local" {
		t.Errorf("parentDN = %q", got)
	}
	if got := parentDN("CN=Seul"); got != "" {
		t.Errorf("parentDN = %q", got)
	}
}

func TestValeurRDN(t *testing.T) {
	if got := valeurRDN("OU=6e_A,DC=lycee,DC=local"); got != "6e_A" {
		t.Errorf("valeurRDN = %q", got)
	}
	if got := valeurRDN("6e_A"); got != "6e_A" {
		t.Errorf("valeurRDN = %q", got)
	}
}

func TestOUsDuCompteEtDuGroupe(t *testing.T) {
	compte := Compte{DN: "CN=X,OU=A,DC=lycee,DC=local"}
	if got := ouDuCompte(compte); got != "OU=A,DC=lycee,DC=local" {
		t.Errorf("ouDuCompte = %q", got)
	}
	compte.OU = "OU=Explicite,DC=lycee,DC=local"
	if got := ouDuCompte(compte); got != "OU=Explicite,DC=lycee,DC=local" {
		t.Errorf("ouDuCompte explicite = %q", got)
	}

	groupe := Groupe{DN: "CN=G,OU=Groupes,DC=lycee,DC=local"}
	if got := ouDuGroupe(groupe); got != "OU=Groupes,DC=lycee,DC=local" {
		t.Errorf("ouDuGroupe = %q", got)
	}
}

// garantit que le type de compte par défaut utilisé par l'assistant est bien
// l'élève (comme la version Python, qui ne gère que ce cas).
func TestTypeCompteParDefaut(t *testing.T) {
	cfg := config.DefaultAppConfig()
	if got := cfg.MoteurIdentifiants(models.AccountEleve).Format; got != cfg.IdentifiantFormatEleve {
		t.Fatalf("format = %q", got)
	}
}
