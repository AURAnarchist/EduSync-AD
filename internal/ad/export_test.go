package ad

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// usersExportFixtures retourne un jeu d'utilisateurs pour les tests d'export.
func usersExportFixtures() []*User {
	return []*User{
		{
			DN: "CN=Léa Martin,OU=Eleves,DC=EDUSYNC,DC=LOCAL", SAMAccountName: "l.martin",
			UserPrincipalName: "l.martin@EDUSYNC.LOCAL", DisplayName: "Martin Léa",
			GivenName: "Léa", SN: "Martin", Email: "lea.martin@edusync.local",
			OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL", Enabled: true,
		},
		{
			DN: "CN=Jean Durand,OU=Eleves,DC=EDUSYNC,DC=LOCAL", SAMAccountName: "j.durand",
			UserPrincipalName: "j.durand@EDUSYNC.LOCAL", DisplayName: "Durand Jean",
			GivenName: "Jean", SN: "Durand", Email: "jean.durand@edusync.local",
			OU: "OU=Eleves,DC=EDUSYNC,DC=LOCAL", Enabled: true,
		},
	}
}

func TestExportCSVBasique(t *testing.T) {
	p := &Pool{}
	out, err := p.ExportCSV(context.Background(), ExportParams{
		Format:        FormatCSV,
		IncludeHeader: true,
		Separator:     ';',
		Fields:        []ExportField{FieldSAMAccountName, FieldDisplayName, FieldEmail},
		Users:         usersExportFixtures(),
	})
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}

	lignes := strings.Split(strings.TrimRight(string(out), "\r\n"), "\r\n")
	if len(lignes) != 3 {
		t.Fatalf("lignes: attendu 3 (en-tête + 2), obtenu %d : %q", len(lignes), out)
	}
	if lignes[0] != "sAMAccountName;displayName;mail" {
		t.Errorf("en-tête: %q", lignes[0])
	}
	if !strings.Contains(lignes[1], "l.martin") || !strings.Contains(lignes[1], "Martin Léa") {
		t.Errorf("ligne 1: %q", lignes[1])
	}
}

func TestExportCSVSeparateurPersonnalise(t *testing.T) {
	p := &Pool{}
	out, err := p.ExportCSV(context.Background(), ExportParams{
		Format:    FormatCSV,
		Separator: ',',
		Fields:    []ExportField{FieldSAMAccountName},
		Users:     usersExportFixtures(),
	})
	if err != nil {
		t.Fatalf("ExportCSV: %v", err)
	}
	lignes := strings.Split(strings.TrimRight(string(out), "\r\n"), "\r\n")
	// Sans en-tête : 2 lignes, 1 champ chacune (la virgule ne doit pas
	// apparaître dans les données).
	if len(lignes) != 2 {
		t.Fatalf("lignes: attendu 2, obtenu %d : %q", len(lignes), out)
	}
	if strings.Contains(lignes[0], ",") {
		t.Errorf("donnée contenant une virgule non échappée: %q", lignes[0])
	}
}

func TestExportCSVSansUtilisateurErreur(t *testing.T) {
	p := &Pool{}
	if _, err := p.ExportCSV(context.Background(), ExportParams{
		Format: FormatCSV,
		Fields: []ExportField{FieldSAMAccountName},
	}); err == nil {
		t.Error("devrait refuser un export sans utilisateur")
	}
}

func TestExportLabelsPDFValide(t *testing.T) {
	p := &Pool{}
	out, err := p.ExportLabelsPDF(context.Background(), ExportParams{
		Format: FormatPDF,
		Fields: []ExportField{FieldSAMAccountName, FieldDisplayName, FieldOU},
		Users:  usersExportFixtures(),
	})
	if err != nil {
		t.Fatalf("ExportLabelsPDF: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Errorf("le produit ne commence pas par %%PDF : %.40q", out)
	}
	if len(out) < 500 {
		t.Errorf("PDF anormalement petit : %d octets", len(out))
	}
}

func TestExportLabelsPDFDeuxModeles(t *testing.T) {
	p := &Pool{}
	for _, modele := range []LabelTemplate{LabelL7160, LabelL7163} {
		out, err := p.ExportLabelsPDF(context.Background(), ExportParams{
			Format:   FormatPDF,
			Template: modele,
			Fields:   []ExportField{FieldSAMAccountName, FieldDisplayName},
			Users:    usersExportFixtures(),
		})
		if err != nil {
			t.Errorf("modèle %s: %v", modele, err)
			continue
		}
		if !bytes.HasPrefix(out, []byte("%PDF")) {
			t.Errorf("modèle %s : pas de %%PDF", modele)
		}
	}
}

func TestExportLabelsPDFModeleInconnu(t *testing.T) {
	p := &Pool{}
	if _, err := p.ExportLabelsPDF(context.Background(), ExportParams{
		Format:   FormatPDF,
		Template: "L0000",
		Fields:   []ExportField{FieldSAMAccountName},
		Users:    usersExportFixtures(),
	}); err == nil {
		t.Error("devrait refuser un modèle d'étiquette inconnu")
	}
}

func TestExportTrombinoscopeValide(t *testing.T) {
	p := &Pool{}
	out, err := p.ExportTrombinoscope(context.Background(), ExportParams{
		Format: FormatPDF,
		Title:  "Trombinoscope 6e",
		Fields: []ExportField{FieldSAMAccountName, FieldDisplayName},
		Users:  usersExportFixtures(),
	})
	if err != nil {
		t.Fatalf("ExportTrombinoscope: %v", err)
	}
	if !bytes.HasPrefix(out, []byte("%PDF")) {
		t.Errorf("le produit ne commence pas par %%PDF : %.40q", out)
	}
}

func TestExportValidateDefauts(t *testing.T) {
	p := ExportParams{Format: FormatPDF, Users: usersExportFixtures()}
	if err := p.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if p.Template != LabelL7160 {
		t.Errorf("modèle par défaut: %q, attendu L7160", p.Template)
	}
	if p.Separator != ';' {
		t.Errorf("séparateur par défaut: %q, attendu ';'", p.Separator)
	}
}
