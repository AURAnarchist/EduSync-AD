package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPolitiqueMotDePasseJSON(t *testing.T) {
	raw, err := json.Marshal(DefaultPasswordPolicy())
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}

	attendues := []string{
		"longueur", "majuscules", "chiffres", "caracteres_speciaux", "mot_de_passe_identique",
	}
	for _, clee := range attendues {
		if _, ok := data[clee]; !ok {
			t.Errorf("clé absente: %s", clee)
		}
	}
	// pattern_fixe vide = None côté Python : la clé ne doit pas être écrite.
	if _, ok := data["pattern_fixe"]; ok {
		t.Error("pattern_fixe vide ne doit pas figurer dans le JSON")
	}
	if len(data) != len(attendues) {
		t.Errorf("%d clés produites, %d attendues", len(data), len(attendues))
	}
}

func TestPolitiqueMotDePasseAvecPattern(t *testing.T) {
	p := DefaultPasswordPolicy()
	p.PatternFixe = "Edu$ync2026"

	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"pattern_fixe":"Edu$ync2026"`) {
		t.Errorf("pattern_fixe doit être sérialisé: %s", raw)
	}
}

func TestRegleDoublons(t *testing.T) {
	valides := []string{
		"suffixe_numerique", "suffixe_numerique_separateur", "prefixe_numerique",
		"lettres_prenom", "lettres_nom", "annee_suffixe",
	}
	for _, v := range valides {
		if !ValidDoublonRule(v) {
			t.Errorf("%q doit être valide", v)
		}
	}
	for _, v := range []string{"", "inconnu", "SUFFIXE_NUMERIQUE"} {
		if ValidDoublonRule(v) {
			t.Errorf("%q ne doit pas être valide", v)
		}
	}
}

func TestReglePrenomCompose(t *testing.T) {
	for _, v := range []string{"premier_prenom", "concatenation", "troncature"} {
		if !ValidPrenomComposeRule(v) {
			t.Errorf("%q doit être valide", v)
		}
	}
	for _, v := range []string{"", "autre", "PREMIER_PRENOM"} {
		if ValidPrenomComposeRule(v) {
			t.Errorf("%q ne doit pas être valide", v)
		}
	}
}

func TestNomComplet(t *testing.T) {
	u := GeneratedUser{Source: RawUserRow{Prenom: "Maëlys", Nom: "Durand"}}
	if got := u.NomComplet(); got != "Maëlys Durand" {
		t.Errorf("NomComplet() = %q", got)
	}
}

// TestActionLogEntryJSONPlat : les entrées du journal doivent rester
// plates (pas de champ imbriqué) pour rester lisibles par Python.
func TestActionLogEntryJSONPlat(t *testing.T) {
	raw, err := json.Marshal(ActionLogEntry{
		Timestamp: "2026-10-12T10:00:00Z", ActionType: "test", Compte: "eleve01",
		Resultat: "succes", SessionID: "abcdef123456",
	})
	if err != nil {
		t.Fatal(err)
	}

	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatal(err)
	}
	attendues := []string{
		"timestamp", "action_type", "compte", "ou_source", "ou_destination",
		"resultat", "session_id", "simulation", "detail", "utilisateur", "domaine",
	}
	for _, clee := range attendues {
		if _, ok := data[clee]; !ok {
			t.Errorf("clé absente: %s", clee)
		}
	}
	if len(data) != len(attendues) {
		t.Errorf("%d clés produites, %d attendues", len(data), len(attendues))
	}
}
