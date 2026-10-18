package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/estemobs/EduSync-AD/internal/compliance"
)

func TestComplianceMFA(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))

	// Sans configuration, le MFA doit être "none"
	out, err := execution(t, "compliance", "mfa")
	if err != nil {
		t.Fatalf("compliance mfa: %v", err)
	}
	if !strings.Contains(out, "Statut MFA : none") {
		t.Errorf("statut attendu none, obtenu: %s", out)
	}
	if !strings.Contains(out, "MFA non activé") {
		t.Errorf("détail manquant: %s", out)
	}
}

func TestComplianceRGPD(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))

	out, err := execution(t, "compliance", "rgpd")
	if err != nil {
		t.Fatalf("compliance rgpd: %v", err)
	}
	if !strings.Contains(out, "Rapport RGPD") {
		t.Errorf("titre manquant: %s", out)
	}
	if !strings.Contains(out, "comptes : 0") {
		t.Errorf("inventaire comptes manquant: %s", out)
	}
}

func TestCompliancePSO(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))

	out, err := execution(t, "compliance", "pso")
	if err != nil {
		t.Fatalf("compliance pso: %v", err)
	}
	if !strings.Contains(out, "Score de conformité") {
		t.Errorf("score manquant: %s", out)
	}
	if !strings.Contains(out, "PSO-Eleves") || !strings.Contains(out, "PSO-Personnels") {
		t.Errorf("PSO proposées manquantes: %s", out)
	}
}

func TestComplianceSignVerify(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))

	// Créer un fichier JSON de test
	fichier := filepath.Join(dir, "test.json")
	donnees := map[string]string{"cle": "valeur", "autre": "test"}
	brut, _ := json.MarshalIndent(donnees, "", "  ")
	if err := os.WriteFile(fichier, brut, 0o600); err != nil {
		t.Fatal(err)
	}

	// Signer
	sigFile := filepath.Join(dir, "signature.json")
	out, err := execution(t, "compliance", "sign", "-fichier", fichier, "-sortie", sigFile)
	if err != nil {
		t.Fatalf("compliance sign: %v\n%s", err, out)
	}

	var sig compliance.SignatureExport
	if err := json.Unmarshal([]byte(out), &sig); err != nil {
		t.Fatalf("JSON signature invalide: %v\n%s", err, out)
	}
	if sig.Signature == "" || sig.DataHash == "" || sig.Algorithm != "HMAC-SHA256" {
		t.Errorf("signature incomplète: %+v", sig)
	}

	// Vérifier
	out, err = execution(t, "compliance", "verify", "-fichier", fichier, "-signature", sigFile)
	if err != nil {
		t.Fatalf("compliance verify: %v\n%s", err, out)
	}
	if !strings.Contains(out, "Signature valide") {
		t.Errorf("vérification échouée: %s", out)
	}

	// Modifier le fichier -> la vérification doit échouer
	donneesMod := map[string]string{"cle": "valeur_modifiee", "autre": "test"}
	brut2, _ := json.MarshalIndent(donneesMod, "", "  ")
	if err := os.WriteFile(fichier, brut2, 0o600); err != nil {
		t.Fatal(err)
	}

	_, err = execution(t, "compliance", "verify", "-fichier", fichier, "-signature", sigFile)
	if err == nil {
		t.Error("la vérification aurait dû échouer après modification")
	}
}

func TestComplianceSecret(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))

	out, err := execution(t, "compliance", "secret", "--json")
	if err != nil {
		t.Fatalf("compliance secret: %v", err)
	}
	var res map[string]any
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("JSON invalide: %v", err)
	}
	if res["mfa_secret"] == "" {
		t.Error("secret manquant")
	}
	if len(res["mfa_secret"].(string)) < 20 {
		t.Errorf("secret trop court: %s", res["mfa_secret"])
	}
	if res["methode"] != "totp" {
		t.Errorf("méthode inattendue: %v", res["methode"])
	}
	codes := res["codes_secours"].([]any)
	if len(codes) != 10 {
		t.Errorf("10 codes attendus, obtenu %d", len(codes))
	}
	empreintes := res["codes_empreintes"].([]any)
	if len(empreintes) != 10 {
		t.Errorf("10 empreintes attendues, obtenu %d", len(empreintes))
	}
}

func TestComplianceSession(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))

	// Sans journal ou sessions, le rapport doit être compliant (0 sessions)
	out, err := execution(t, "compliance", "session", "--json")
	if err != nil {
		t.Fatalf("compliance session: %v", err)
	}
	var res compliance.RapportSessionUnique
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("JSON invalide: %v", err)
	}
	if res.Statut != "compliant" {
		t.Errorf("statut attendu compliant, obtenu: %s", res.Statut)
	}
	if res.SessionsActives != 0 {
		t.Errorf("0 sessions attendues, obtenu %d", res.SessionsActives)
	}
}
