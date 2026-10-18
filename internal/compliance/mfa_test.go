package compliance

import (
	"strings"
	"testing"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/crypto"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// cleTest retourne une clé AES valide pour les tests.
func cleTest(t *testing.T) []byte {
	t.Helper()
	cle, err := crypto.NewKey()
	if err != nil {
		t.Fatalf("génération de clé: %v", err)
	}
	return cle
}

// TestVerifierMFAAdmin couvre les trois statuts.
func TestVerifierMFAAdmin(t *testing.T) {
	// Aucun MFA : statut « none », deux manques signalés.
	rapport := VerifierMFAAdmin(ConfigDefaut())
	if rapport.Statut != MFAAucun {
		t.Fatalf("statut = %q", rapport.Statut)
	}
	if len(rapport.Details) < 2 || len(rapport.Recommandations) < 2 {
		t.Fatalf("rapport = %+v", rapport)
	}

	// MFA activé mais sans secret ni codes de secours : « partial ».
	partiel := Config{MFAActif: true, MFAMethode: MethodeTOTP, DureeSessionMinutes: 480, ConservationJours: 365}
	rapport = VerifierMFAAdmin(partiel)
	if rapport.Statut != MFAPartiel {
		t.Fatalf("statut = %q (%+v)", rapport.Statut, rapport)
	}

	// Configuration complète.
	codes, empreintes, err := GenererCodesSecours()
	if err != nil {
		t.Fatal(err)
	}
	_ = codes // les codes en clair ne sont persistés qu'en empreinte
	complet := partiel
	complet.MFASecret = "JBSWY3DPEHPK3PXP"
	complet.MFACodesSecours = empreintes
	rapport = VerifierMFAAdmin(complet)
	if rapport.Statut != MFAComplet {
		t.Fatalf("statut = %q (%+v)", rapport.Statut, rapport)
	}
	if len(rapport.Details) != 0 {
		t.Errorf("détails = %v", rapport.Details)
	}

	// Méthode inconnue : signalée.
	inconnu := complet
	inconnu.MFAMethode = "sms"
	if rapport := VerifierMFAAdmin(inconnu); rapport.Statut != MFAPartiel {
		t.Errorf("statut = %q", rapport.Statut)
	}
}

func TestGenererCleSeche(t *testing.T) {
	cle := cleTest(t)
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	seche, err := GenererCleSeche("Administrator", cle, moment)
	if err != nil {
		t.Fatal(err)
	}
	if seche.AdminID != "Administrator" {
		t.Errorf("admin = %q", seche.AdminID)
	}
	if seche.DateCreation != "2026-10-17T12:00:00+00:00" {
		t.Errorf("création = %q", seche.DateCreation)
	}
	attendu := moment.AddDate(0, 0, ValiditeCleSecheJours).Format(formatISO)
	if seche.DateExpiration != attendu {
		t.Errorf("expiration = %q, attendu %q", seche.DateExpiration, attendu)
	}
	if seche.Description == "" {
		t.Error("description vide")
	}

	// La clé se relit avec la clé AES locale et fait 32 octets (64 caractères
	// hexadécimaux dans la version Python).
	clair, err := crypto.Decrypt(cle, seche.CleChiffree)
	if err != nil {
		t.Fatalf("déchiffrement: %v", err)
	}
	if len(clair) != 64 {
		t.Errorf("clé en clair = %d caractères", len(clair))
	}

	if _, err := GenererCleSeche("  ", cle, moment); err == nil {
		t.Error("un identifiant vide doit être refusé")
	}
	if _, err := GenererCleSeche("admin", []byte("court"), moment); err == nil {
		t.Error("une clé AES invalide doit être refusée")
	}
}

func TestVerifierSessionUnique(t *testing.T) {
	cfg := ConfigDefaut()
	cfg.SessionUnique = true

	// Aucune session : conforme.
	rapport := VerifierSessionUnique(cfg, nil)
	if rapport.Statut != "compliant" || len(rapport.Doublons) != 0 {
		t.Fatalf("rapport = %+v", rapport)
	}

	// Deux sessions pour le même opérateur : violation nommée.
	sessions := []Session{
		{Operateur: "admin", Domaine: "EDUSYNC.LOCAL", SessionID: "a"},
		{Operateur: "ADMIN", Domaine: "edusync.local", SessionID: "b"},
		{Operateur: "aide", Domaine: "EDUSYNC.LOCAL", SessionID: "c"},
	}
	rapport = VerifierSessionUnique(cfg, sessions)
	if rapport.Statut != "violation" {
		t.Fatalf("statut = %q", rapport.Statut)
	}
	if len(rapport.Doublons) != 1 || !strings.Contains(rapport.Doublons[0], "admin@edusync.local") {
		t.Fatalf("doublons = %v", rapport.Doublons)
	}
	if rapport.SessionsActives != 3 {
		t.Errorf("sessions = %d", rapport.SessionsActives)
	}

	// Politique désactivée.
	cfg.SessionUnique = false
	rapport = VerifierSessionUnique(cfg, sessions)
	if rapport.Statut != "desactive" {
		t.Fatalf("statut = %q", rapport.Statut)
	}
}

func TestAnalyserGranulariteMDP(t *testing.T) {
	// Configuration par défaut : politiques différentes et courtes → score
	// entamé et PSO proposées.
	analyse := AnalyserGranulariteMDP(config.DefaultAppConfig())
	if len(analyse.Politiques) != 2 {
		t.Fatalf("politiques = %+v", analyse.Politiques)
	}
	if analyse.Politiques[0].Longueur < 12 || analyse.Politiques[1].Longueur < 14 {
		t.Errorf("longueurs relevées insuffisantes : %+v", analyse.Politiques)
	}
	if len(analyse.Differences) == 0 {
		t.Error("les politiques par défaut diffèrent : un écart est attendu")
	}
	if analyse.Score >= 100 {
		t.Errorf("score = %d, attendu < 100", analyse.Score)
	}

	// Politiques durcies et identiques : plus d'écart de longueur.
	cfg := config.DefaultAppConfig()
	cfg.PolitiqueMDPEleve = models.PasswordPolicy{
		Longueur: 14, Majuscules: true, Chiffres: true, CaracteresSpeciaux: true,
	}
	cfg.PolitiqueMDPPersonnel = cfg.PolitiqueMDPEleve
	analyse = AnalyserGranulariteMDP(cfg)
	if analyse.Score < 90 {
		t.Errorf("score = %d, attendu ≥ 90 (%+v)", analyse.Score, analyse.Differences)
	}
	for _, difference := range analyse.Differences {
		if strings.Contains(difference, "Longueurs différentes") {
			t.Errorf("écart de longueur inattendu : %q", difference)
		}
	}
}

func TestScriptPSO(t *testing.T) {
	analyse := AnalyserGranulariteMDP(config.DefaultAppConfig())
	script := ScriptPSO(analyse, "DC=lycee,DC=local")
	for _, attendu := range []string{
		"New-ADFineGrainedPasswordPolicy",
		"PSO-Eleves",
		"PSO-Personnels",
		"Add-ADFineGrainedPasswordPolicySubject",
		"DC=lycee,DC=local",
	} {
		if !strings.Contains(script, attendu) {
			t.Errorf("script sans %q", attendu)
		}
	}
	if strings.Contains(script, "Nom inconnu") {
		t.Error("script incohérent")
	}
}

func TestSignerEtVerifierExport(t *testing.T) {
	cle := cleTest(t)
	moment := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	donnees := map[string]any{"eleves": 42, "etablissement": "Collège Jean Moulin", "actif": true}

	signature, err := SignerExport(donnees, cle, moment)
	if err != nil {
		t.Fatal(err)
	}
	if signature.Algorithm != AlgorithmeSignature {
		t.Errorf("algorithme = %q", signature.Algorithm)
	}
	if len(signature.Signature) != 64 || len(signature.DataHash) != 64 {
		t.Errorf("empreintes de taille inattendue : %+v", signature)
	}
	if signature.Horodatage != "2026-10-17T12:00:00+00:00" {
		t.Errorf("horodatage = %q", signature.Horodatage)
	}

	if err := VerifierExport(donnees, signature, cle); err != nil {
		t.Fatalf("vérification: %v", err)
	}

	// Données modifiées : refus.
	modifie := map[string]any{"eleves": 43, "etablissement": "Collège Jean Moulin", "actif": true}
	if err := VerifierExport(modifie, signature, cle); err == nil {
		t.Error("des données modifiées doivent être détectées")
	}

	// Mauvaise clé : refus (mêmes données, empreinte identique mais signature
	// différente).
	if err := VerifierExport(donnees, signature, cleTest(t)); err == nil {
		t.Error("une mauvaise clé doit invalider la signature")
	}

	// La signature est déterministe pour des données identiques.
	autre, err := SignerExport(donnees, cle, moment.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if autre.Signature != signature.Signature || autre.DataHash != signature.DataHash {
		t.Error("la signature ne doit dépendre ni de l'instant ni de l'ordre d'appel")
	}
	if autre.Horodatage == signature.Horodatage {
		t.Error("les horodatages doivent différer")
	}

	if _, err := SignerExport(donnees, []byte("court"), moment); err == nil {
		t.Error("une clé invalide doit être refusée")
	}
	if err := VerifierExport(donnees, signature, []byte("court")); err == nil {
		t.Error("une clé invalide doit être refusée à la vérification")
	}
}
