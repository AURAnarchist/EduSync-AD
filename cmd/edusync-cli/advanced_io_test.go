package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/ad"
)

func TestAdvIOValidateFilter(t *testing.T) {
	out, err := execution(t, "advanced-io", "validate-filter", "--filter", "(&(objectClass=user))", "--json")
	if err != nil {
		t.Fatalf("filtre valide: %v", err)
	}
	var rep struct {
		Valide  bool     `json:"valid"`
		Erreurs []string `json:"errors"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if !rep.Valide || len(rep.Erreurs) != 0 {
		t.Errorf("filtre valide rejeté : %s", out)
	}

	if _, err := execution(t, "advanced-io", "validate-filter", "--filter", "objectClass=user", "--json"); err == nil {
		t.Error("filtre invalide accepté")
	}
}

func TestAdvIOSource(t *testing.T) {
	out, err := execution(t, "advanced-io", "source", "--mode", "ou", "--ou", "OU=Eleves,DC=l", "--json")
	if err != nil {
		t.Fatalf("source OU: %v", err)
	}
	var rep struct {
		Base   string `json:"base_dn"`
		Filtre string `json:"ldap_filter"`
		Valide bool   `json:"valid"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if rep.Base != "OU=Eleves,DC=l" || rep.Filtre != ad.FiltreUtilisateurs || !rep.Valide {
		t.Errorf("source = %+v", rep)
	}

	if _, err := execution(t, "advanced-io", "source", "--mode", "ou", "--ou", "", "--json"); err == nil {
		t.Error("OU vide acceptée")
	}
}

func TestAdvIOImportAttenteAD(t *testing.T) {
	out, err := execution(t, "advanced-io", "import", "--mode", "filtre", "--base", "DC=l", "--json")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	var rep struct {
		Statut string `json:"statut"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if rep.Statut != "attente_ad" {
		t.Errorf("statut = %q", rep.Statut)
	}
}

func TestAdvIOExportCSV(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "entrees.json")
	donnees := `[{"dn":"CN=A","cn":"A","memberOf":["x","y"]},{"dn":"CN=B","cn":"B","mail":"b@x.fr"}]`
	if err := os.WriteFile(src, []byte(donnees), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out.csv")
	if _, err := execution(t, "advanced-io", "export", "--src", src, "--dest", dest, "--format", "csv"); err != nil {
		t.Fatalf("export csv: %v", err)
	}
	contenu, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("lecture: %v", err)
	}
	if !strings.Contains(string(contenu), "memberOf") || !strings.Contains(string(contenu), "x | y") {
		t.Errorf("contenu CSV = %q", contenu)
	}
}

func TestAdvIOExportLDIFEtVCard(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "entrees.json")
	if err := os.WriteFile(src, []byte(`[{"dn":"CN=A,DC=l","cn":"A","mail":"a@x.fr"}]`), 0o600); err != nil {
		t.Fatal(err)
	}

	ldif := filepath.Join(dir, "out.ldif")
	if _, err := execution(t, "advanced-io", "export", "--src", src, "--dest", ldif, "--format", "ldif", "--changetype", "add"); err != nil {
		t.Fatalf("export ldif: %v", err)
	}
	contenu, _ := os.ReadFile(ldif)
	if !strings.Contains(string(contenu), "changetype: add") || !strings.Contains(string(contenu), "mail: a@x.fr") {
		t.Errorf("LDIF = %q", contenu)
	}

	vcf := filepath.Join(dir, "out.vcf")
	if _, err := execution(t, "advanced-io", "export", "--src", src, "--dest", vcf, "--format", "vcard"); err != nil {
		t.Fatalf("export vcard: %v", err)
	}
	carte, _ := os.ReadFile(vcf)
	if !strings.Contains(string(carte), "BEGIN:VCARD") {
		t.Errorf("vCard = %q", carte)
	}
}

func TestAdvIOMergeHTML(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "entrees.json")
	if err := os.WriteFile(src, []byte(`[{"cn":"Jean Durand","sAMAccountName":"jdurand"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "docs")
	if _, err := execution(t, "advanced-io", "merge-html", "--src", src, "--dest", dest); err != nil {
		t.Fatalf("merge-html: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "jdurand_document.html")); err != nil {
		t.Errorf("document généré manquant : %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "index.html")); err != nil {
		t.Errorf("index.html manquant : %v", err)
	}
}

func TestAdvIOGEPMappingEtParse(t *testing.T) {
	out, err := execution(t, "advanced-io", "gep", "mapping", "--headers", "NOM,PRENOM,CLASSE", "--json")
	if err != nil {
		t.Fatalf("mapping: %v", err)
	}
	var mapping map[string]string
	if err := json.Unmarshal([]byte(out), &mapping); err != nil {
		t.Fatalf("JSON mapping: %v", err)
	}
	if mapping["nom"] != "NOM" || mapping["prenom"] != "PRENOM" {
		t.Errorf("mapping = %v", mapping)
	}

	dir := t.TempDir()
	src := filepath.Join(dir, "gep.csv")
	if err := os.WriteFile(src, []byte("NOM;PRENOM;CLASSE\nDurand;Jean;6A\n;SansNom;6A\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "edusync.csv")
	out, err = execution(t, "advanced-io", "gep", "parse", "--src", src, "--dest", dest, "--json")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var rep struct {
		Rows    int   `json:"rows"`
		Skipped []int `json:"skipped_row_numbers"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("JSON parse: %v", err)
	}
	if rep.Rows != 1 || len(rep.Skipped) != 1 || rep.Skipped[0] != 2 {
		t.Errorf("résultat GEP = %+v", rep)
	}
	converti, _ := os.ReadFile(dest)
	if !strings.Contains(string(converti), "prenom;nom;classe") {
		t.Errorf("CSV EduSync = %q", converti)
	}
}

func TestAdvIOConfigEtRunTool(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	out, err := execution(t, "advanced-io", "config", "--json")
	if err != nil {
		t.Fatalf("config: %v", err)
	}
	var cfg ad.ConfigAdvIO
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("JSON config: %v", err)
	}
	if cfg.CheminOutilGEP != "" {
		t.Errorf("config initiale non vide : %+v", cfg)
	}

	outil := filepath.Join(t.TempDir(), "outil")
	if err := os.WriteFile(outil, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := execution(t, "advanced-io", "config", "--tool", outil, "--json"); err != nil {
		t.Fatalf("écriture config: %v", err)
	}
	out, err = execution(t, "advanced-io", "config", "--json")
	if err != nil {
		t.Fatalf("relecture config: %v", err)
	}
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if cfg.CheminOutilGEP != outil {
		t.Errorf("chemin outil = %q, attendu %q", cfg.CheminOutilGEP, outil)
	}
}

func TestAdvIOSousCommandeInconnue(t *testing.T) {
	if _, err := execution(t, "advanced-io", "bogus"); err == nil {
		t.Error("sous-commande inconnue acceptée")
	}
}
