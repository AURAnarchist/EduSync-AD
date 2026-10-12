package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func execution(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(args, &stdout, &stderr)
	return stdout.String(), err
}

func TestHelp(t *testing.T) {
	out, err := execution(t, "help")
	if err != nil {
		t.Fatalf("help: %v", err)
	}
	for _, attendu := range []string{"Commandes :", "connect", "audit list"} {
		if !strings.Contains(out, attendu) {
			t.Errorf("aide incomplète: %q absent", attendu)
		}
	}
}

func TestSansArgument(t *testing.T) {
	_, err := execution(t)
	if err == nil {
		t.Fatal("aucune commande = erreur d'usage")
	}
	var code *codeSortie
	if !errors.As(err, &code) || code.code != 2 {
		t.Errorf("code d'usage attendu 2, obtenu %v", err)
	}
}

func TestCommandeInconnue(t *testing.T) {
	_, err := execution(t, "nimporte")
	if err == nil {
		t.Fatal("une commande inconnue doit échouer")
	}
	var code *codeSortie
	if !errors.As(err, &code) || code.code != 2 {
		t.Errorf("code 2 attendu, obtenu %v", err)
	}
}

func TestVersion(t *testing.T) {
	out, err := execution(t, "version")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "edusync-cli") {
		t.Errorf("sortie = %q", out)
	}
}

// TestPathsIsole gere des chemins personnels : les variables XDG sont
// redirigées vers un dossier temporaire pour ne pas toucher au poste.
func TestPathsIsole(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(dir, "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))

	out, err := execution(t, "paths", "--json")
	if err != nil {
		t.Fatalf("paths: %v", err)
	}

	var valeurs map[string]string
	if err := json.Unmarshal([]byte(out), &valeurs); err != nil {
		t.Fatalf("sortie non JSON: %v\n%s", err, out)
	}
	for _, clee := range []string{"configuration", "donnees", "cache", "journal", "version"} {
		if valeurs[clee] == "" {
			t.Errorf("clé absente ou vide: %s", clee)
		}
	}
	if !strings.Contains(valeurs["configuration"], "config.json") {
		t.Errorf("configuration = %q", valeurs["configuration"])
	}
	if !strings.Contains(valeurs["journal"], "journal.db") {
		t.Errorf("journal = %q", valeurs["journal"])
	}
}

func TestConfigShow(t *testing.T) {
	out, err := execution(t, "config", "show")
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(out), &cfg); err != nil {
		t.Fatalf("JSON invalide: %v", err)
	}
	if cfg["theme"] != "clair" || cfg["langue"] != "fr" {
		t.Errorf("défauts inattendus: %v", cfg)
	}
	if _, ok := cfg["politique_mdp_eleve"]; !ok {
		t.Error("politique_mdp_eleve absente")
	}
}

func TestConfigShowFichierInvalide(t *testing.T) {
	dir := t.TempDir()
	chemin := filepath.Join(dir, "config.json")
	if err := os.WriteFile(chemin, []byte("{cassé"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Un fichier illisible ne doit pas faire échouer la commande : on
	// affiche les valeurs par défaut, avec un avertissement.
	out, err := execution(t, "config", "show", "-f", chemin)
	if err != nil {
		t.Fatalf("config show: %v", err)
	}
	if !strings.Contains(out, `"theme": "clair"`) {
		t.Errorf("défauts attendus: %s", out)
	}
}

func TestAuditListVide(t *testing.T) {
	chemin := filepath.Join(t.TempDir(), "journal.db")
	out, err := execution(t, "audit", "list", "-f", chemin, "--json")
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	if strings.TrimSpace(out) != "[]" {
		t.Errorf("sortie = %q, attendu []", out)
	}
}

func TestAuditListSansSousCommande(t *testing.T) {
	_, err := execution(t, "audit")
	var code *codeSortie
	if !errors.As(err, &code) || code.code != 2 {
		t.Errorf("code 2 attendu, obtenu %v", err)
	}
}

// ─── Commande « connect » ───────────────────────────────────────────────

// serveurTLS fabrique une autorité de certification et un serveur LDAPS
// de test signé par cette autorité (IP 127.0.0.1).
func serveurTLS(t *testing.T) (net.Listener, string) {
	t.Helper()

	// Autorité de certification.
	racinePub, racinePrive, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	racine := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "EduSync AD CA de test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	racineDER, err := x509.CreateCertificate(rand.Reader, racine, racine, racinePub, racinePrive)
	if err != nil {
		t.Fatal(err)
	}

	// Certificat de serveur signé par l'autorité.
	serveurPub, serveurPrive, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serveur := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "dc1.test.local"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{"dc1.test.local"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	serveurDER, err := x509.CreateCertificate(rand.Reader, serveur, racine, serveurPub, racinePrive)
	if err != nil {
		t.Fatal(err)
	}

	paire := tls.Certificate{
		Certificate: [][]byte{serveurDER, racineDER},
		PrivateKey:  serveurPrive,
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{paire}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			// Laisse le client entamer la poignée puis referme.
			_ = conn.(*tls.Conn).Handshake()
			_ = conn.Close()
		}
	}()

	// PEM de l'autorité pour l'option --ca.
	cheminCA := filepath.Join(t.TempDir(), "ca.pem")
	pemCA := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: racineDER})
	if err := os.WriteFile(cheminCA, pemCA, 0o600); err != nil {
		t.Fatal(err)
	}
	return ln, cheminCA
}

func TestConnectSansServeur(t *testing.T) {
	_, err := execution(t, "connect")
	var code *codeSortie
	if !errors.As(err, &code) || code.code != 2 {
		t.Errorf("code 2 attendu, obtenu %v", err)
	}
}

func TestConnectAvecCABon(t *testing.T) {
	ln, cheminCA := serveurTLS(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	out, err := execution(t, "connect",
		"--server", "127.0.0.1", "--port", port, "--ca", cheminCA, "--tls", "--timeout", "5s")
	if err != nil {
		t.Fatalf("connect: %v (sortie: %s)", err, out)
	}

	var res resultatConnect
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("JSON invalide: %v\n%s", err, out)
	}
	if !res.OK || res.TCP != "ok" || res.TLS != "ok" {
		t.Fatalf("résultat inattendu: %+v", res)
	}
	if !res.ChaineValide {
		t.Error("la chaîne doit être validée avec le CA fourni")
	}
	if res.Sujet == "" || res.Emetteur == "" {
		t.Errorf("certificat non détaillé: %+v", res)
	}
	if res.JoursRestants <= 0 {
		t.Errorf("jours_restants = %d", res.JoursRestants)
	}
	if len(res.EmpreinteSHA) != 64 {
		t.Errorf("empreinte SHA-256 invalide: %q", res.EmpreinteSHA)
	}
	if !strings.Contains(res.LDAPBind, "non_implante") {
		t.Errorf("ldap_bind doit signaler l'absence de bind: %q", res.LDAPBind)
	}
}

// TestConnectSansCAValide : sans ancre de confiance, la vérification doit
// échouer — et être signalée comme telle, jamais masquée.
func TestConnectSansCAValide(t *testing.T) {
	ln, _ := serveurTLS(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	out, err := execution(t, "connect", "--server", "127.0.0.1", "--port", port, "--tls", "--timeout", "5s")
	if err == nil {
		t.Fatalf("la connexion sans CA de confiance doit échouer: %s", out)
	}

	var res resultatConnect
	if e := json.Unmarshal([]byte(out), &res); e != nil {
		t.Fatalf("JSON invalide: %v\n%s", e, out)
	}
	if res.OK {
		t.Error("ok doit être false")
	}
	if res.TLS != "echec" {
		t.Errorf("tls = %q, attendu echec", res.TLS)
	}
	if res.Erreur == "" {
		t.Error("une erreur doit être expliquée")
	}
}

// TestConnectVerificationDesactivee : l'option explicite --verify=false doit
// fonctionner (certificat auto-signé d'un labo) tout en le signalant.
func TestConnectVerificationDesactivee(t *testing.T) {
	ln, _ := serveurTLS(t)
	_, port, _ := net.SplitHostPort(ln.Addr().String())

	out, err := execution(t, "connect",
		"--server", "127.0.0.1", "--port", port, "--verify=false", "--tls", "--timeout", "5s")
	if err != nil {
		t.Fatalf("connect: %v (sortie: %s)", err, out)
	}

	var res resultatConnect
	if err := json.Unmarshal([]byte(out), &res); err != nil {
		t.Fatalf("JSON invalide: %v", err)
	}
	if !res.OK || res.TLS != "ok" {
		t.Fatalf("résultat inattendu: %+v", res)
	}
	if res.ChaineValide {
		t.Error("chaine_valide doit être false quand la vérification est désactivée")
	}
}

func TestConnectPortFerme(t *testing.T) {
	// Un port à fermeture rapide : on prend un port ouvert puis fermé.
	ln, _ := serveurTLS(t)
	addr := ln.Addr().String()
	_, port, _ := net.SplitHostPort(addr)
	_ = ln.Close()

	out, err := execution(t, "connect", "--server", "127.0.0.1", "--port", port, "--timeout", "2s")
	if err == nil {
		t.Fatalf("un port fermé doit échouer: %s", out)
	}
	var res resultatConnect
	if e := json.Unmarshal([]byte(out), &res); e != nil {
		t.Fatalf("JSON invalide: %v\n%s", e, out)
	}
	if res.TCP != "echec" || res.OK {
		t.Errorf("tcp = %q ok = %v, attendu echec/false", res.TCP, res.OK)
	}
	if !strings.Contains(res.Erreur, "connexion TCP") {
		t.Errorf("erreur = %q", res.Erreur)
	}
}
