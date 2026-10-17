package mailer

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fauxEnvoyeur struct {
	cfg *MailConfig
	msg *Message
	err error
}

func (f *fauxEnvoyeur) Envoyer(cfg *MailConfig, msg *Message) error {
	f.cfg, f.msg = cfg, msg
	return f.err
}

func configValide() *MailConfig {
	return &MailConfig{
		Host: "smtp.lycee.fr", Port: 587, Username: "edusync",
		Password: "Secret-SMTP-123", UseTLS: true,
		FromAddr: "edusync@lycee.fr", FromName: "EduSync AD", Timeout: 20,
	}
}

func pdfValide() []byte { return []byte("%PDF-1.4\n%fake\n") }

// ─── Configuration ───────────────────────────────────────────────────────

func TestConfigValideSansErreur(t *testing.T) {
	if errs := configValide().Validate(); len(errs) != 0 {
		t.Errorf("erreurs: %v", errs)
	}
}

func TestConfigServeurManquant(t *testing.T) {
	if !contientMot((&MailConfig{FromAddr: "a@b.fr"}).Validate(), "Serveur SMTP") {
		t.Error("serveur manquant non détecté")
	}
}

func TestConfigExpeditriceManquanteOuInvalide(t *testing.T) {
	if !contientMot((&MailConfig{Host: "h"}).Validate(), "expéditrice requise") {
		t.Error("expéditrice requise non détectée")
	}
	if !contientMot((&MailConfig{Host: "h", FromAddr: "pas-un-mail"}).Validate(), "invalide") {
		t.Error("expéditrice invalide non détectée")
	}
}

func TestConfigPortInvalide(t *testing.T) {
	if !contientMot((&MailConfig{Host: "h", FromAddr: "a@b.fr", Port: 0}).Validate(), "Port") {
		t.Error("port invalide non détecté")
	}
}

func TestConfigSSLEtTLSExclusifs(t *testing.T) {
	cfg := &MailConfig{Host: "h", FromAddr: "a@b.fr", UseSSL: true, UseTLS: true}
	if !contientMot(cfg.Validate(), "exclusifs") {
		t.Error("exclusivité SSL/TLS non détectée")
	}
}

func TestMotDePasseChiffreSurDisque(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, NomFichier)
	clePath := filepath.Join(dir, "secret.key")
	if _, err := SauvegarderConfig(configValide(), chemin, clePath); err != nil {
		t.Fatalf("SauvegarderConfig: %v", err)
	}

	brut, err := os.ReadFile(chemin)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(brut), "Secret-SMTP-123") {
		t.Error("mot de passe en clair sur disque")
	}
	var dict map[string]any
	if err := json.Unmarshal(brut, &dict); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	if _, ok := dict["password_token"]; !ok {
		t.Error("password_token absent")
	}
	for _, cle := range []string{"host", "port", "username", "use_tls", "use_ssl", "from_addr", "from_name", "timeout"} {
		if _, ok := dict[cle]; !ok {
			t.Errorf("clé %s absente", cle)
		}
	}

	charge := ChargerConfig(chemin, clePath)
	if charge.Password != "Secret-SMTP-123" || charge.Host != "smtp.lycee.fr" || charge.Port != 587 {
		t.Errorf("config rechargée = %+v", charge)
	}
}

func TestClePerdueLaisseMotDePasseVide(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, NomFichier)
	if _, err := SauvegarderConfig(configValide(), chemin, filepath.Join(dir, "key1")); err != nil {
		t.Fatal(err)
	}
	charge := ChargerConfig(chemin, filepath.Join(dir, "other-key"))
	if charge.Password != "" {
		t.Errorf("mot de passe = %q, attendu vide", charge.Password)
	}
	if charge.Host != "smtp.lycee.fr" {
		t.Errorf("hôte = %q", charge.Host)
	}
}

func TestSauvegardeRefuseConfigInvalide(t *testing.T) {
	dir := t.TempDir()
	if _, err := SauvegarderConfig(&MailConfig{}, filepath.Join(dir, NomFichier), filepath.Join(dir, "k")); err == nil {
		t.Error("config invalide acceptée")
	}
}

func TestFichierAbsentConfigVide(t *testing.T) {
	cfg := ChargerConfig(filepath.Join(t.TempDir(), "absent.json"), "")
	if cfg.Host != "" || cfg.Password != "" {
		t.Errorf("config = %+v", cfg)
	}
}

// ─── Envoi ───────────────────────────────────────────────────────────────

func TestEnvoiEtiquetteAvecPieceJointe(t *testing.T) {
	fe := &fauxEnvoyeur{}
	if err := EnvoyerEtiquette(configValide(), "eleve@lycee.fr", "Votre étiquette", "Bonjour", pdfValide(), "", fe); err != nil {
		t.Fatalf("EnvoyerEtiquette: %v", err)
	}
	if fe.cfg.Host != "smtp.lycee.fr" || fe.cfg.Port != 587 {
		t.Errorf("cfg = %+v", fe.cfg)
	}
	if fe.msg.Vers != "eleve@lycee.fr" || fe.msg.Sujet != "Votre étiquette" {
		t.Errorf("message = %+v", fe.msg)
	}
	if len(fe.msg.PieceJointe) == 0 || fe.msg.PieceJointeNom != "etiquette.pdf" {
		t.Errorf("pièce jointe = %q (%d octets)", fe.msg.PieceJointeNom, len(fe.msg.PieceJointe))
	}
	octets, err := fe.msg.Octets()
	if err != nil {
		t.Fatalf("Octets: %v", err)
	}
	if !strings.Contains(string(octets), "application/pdf") {
		t.Error("content-type du PDF absent du message sérialisé")
	}
}

func TestDestinataireInvalideRefuse(t *testing.T) {
	if err := EnvoyerEtiquette(configValide(), "pas-de-mail", "s", "b", pdfValide(), "", &fauxEnvoyeur{}); err == nil {
		t.Error("destinataire invalide accepté")
	}
}

func TestConfigInvalideRefuseeAlEnvoi(t *testing.T) {
	if err := EnvoyerEtiquette(&MailConfig{}, "a@b.fr", "s", "b", pdfValide(), "", &fauxEnvoyeur{}); err == nil {
		t.Error("config invalide acceptée")
	}
}

func TestPieceJointeNonPDFRefusee(t *testing.T) {
	if err := EnvoyerEtiquette(configValide(), "a@b.fr", "s", "b", []byte("ce n'est pas un pdf"), "", &fauxEnvoyeur{}); err == nil {
		t.Error("pièce jointe non PDF acceptée")
	}
}

func TestFichierManquantRapporteEnFrancais(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "absent.pdf")
	err := EnvoyerEtiquetteFichier(configValide(), "a@b.fr", "s", "b", chemin, "", &fauxEnvoyeur{})
	if err == nil || !strings.Contains(err.Error(), "Lecture") {
		t.Errorf("erreur = %v", err)
	}
}

func TestEchecAuthentificationEnveloppe(t *testing.T) {
	fe := &fauxEnvoyeur{err: &ErreurMail{Message: "Authentification SMTP refusée (535)."}}
	err := EnvoyerEtiquette(configValide(), "a@b.fr", "s", "b", pdfValide(), "", fe)
	if err == nil || !strings.Contains(err.Error(), "Authentification") {
		t.Errorf("erreur = %v", err)
	}
}

func TestEchecConnexionEnveloppe(t *testing.T) {
	fe := &fauxEnvoyeur{err: errors.New("Connection refused")}
	err := EnvoyerEtiquette(configValide(), "a@b.fr", "s", "b", pdfValide(), "", fe)
	if err == nil || !strings.Contains(err.Error(), "Envoi impossible") {
		t.Errorf("erreur = %v", err)
	}
}

func TestEnvoiMailSansPieceJointe(t *testing.T) {
	fe := &fauxEnvoyeur{}
	if err := EnvoyerMail(configValide(), "a@b.fr", "Sujet", "Corps", "défaut", fe); err != nil {
		t.Fatalf("EnvoyerMail: %v", err)
	}
	if fe.msg.Sujet != "Sujet" || len(fe.msg.PieceJointe) != 0 {
		t.Errorf("message = %+v", fe.msg)
	}
	octets, err := fe.msg.Octets()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(octets), "text/plain") {
		t.Error("content-type texte absent")
	}
}

// contientMot indique si au moins un message contient le fragment donné.
func contientMot(messages []string, fragment string) bool {
	for _, m := range messages {
		if strings.Contains(m, fragment) {
			return true
		}
	}
	return false
}
