package main

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"os"
	"time"
)

// resultatConnect est le rapport structuré de la commande « connect ».
// Les clés JSON sont en français, comme le reste des fichiers du projet.
type resultatConnect struct {
	OK            bool   `json:"ok"`
	Serveur       string `json:"serveur"`
	Port          int    `json:"port"`
	Protocole     string `json:"protocole"` // ldaps | ldap
	TCP           string `json:"tcp"`       // ok | echec
	TLS           string `json:"tls"`       // ok | echec | non_demande
	Sujet         string `json:"sujet,omitempty"`
	Emetteur      string `json:"emetteur,omitempty"`
	ExpireLe      string `json:"expire_le,omitempty"`
	JoursRestants int    `json:"jours_restants,omitempty"`
	EmpreinteSHA  string `json:"empreinte_sha256,omitempty"`
	ChaineValide  bool   `json:"chaine_valide"`
	LDAPBind      string `json:"ldap_bind"` // statut honnête : pas encore branché
	Adresse       string `json:"adresse_resolue,omitempty"`
	DureeMS       int64  `json:"duree_ms"`
	Erreur        string `json:"erreur,omitempty"`
}

// cmdConnect vérifie qu'un contrôleur de domaine est joignable et son
// certificat LDAPS exploitable.
//
// Portée actuelle (Phase 0) : résolution DNS + TCP + poignée de main TLS,
// chaîne de certificats et date d'expiration. Le bind LDAP/GSSAPI est ajouté
// avec le pool de connexions (jours 5-6 de la roadmap) ; il est signalé
// explicitement dans la sortie JSON pour ne jamais faire croire à tort
// qu'une authentification a eu lieu.
func cmdConnect(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	serveur := fs.String("server", "", "contrôleur de domaine (FQDN ou IP) — obligatoire")
	port := fs.Int("port", 636, "port (636 = LDAPS, 389 = LDAP)")
	cheminCA := fs.String("ca", "", "certificat racine de confiance (PEM) — CA interne type AD CS")
	verifier := fs.Bool("verify", true, "vérifier la chaîne du certificat serveur")
	tlsForce := fs.Bool("tls", false, "forcer TLS/LDAPS quel que soit le port (ports personnalisés)")
	timeout := fs.Duration("timeout", 5*time.Second, "délai maximum d'attente")
	if err := fs.Parse(args); err != nil {
		return &codeSortie{code: 2, err: err}
	}
	if *serveur == "" {
		return &codeSortie{code: 2, err: errors.New("l'option --server est obligatoire")}
	}

	debut := time.Now()
	res := resultatConnect{
		Serveur:   *serveur,
		Port:      *port,
		Protocole: protocole(*port, *tlsForce),
		TCP:       "echec",
		TLS:       "non_demande",
		LDAPBind:  "non_implante (Phase 0 — pool de connexions, jours 5-6)",
	}
	terminer := func(err error) error {
		res.DureeMS = time.Since(debut).Milliseconds()
		if err != nil {
			res.Erreur = err.Error()
			res.OK = false
		}
		if jerr := ecrireJSON(stdout, res); jerr != nil {
			return jerr
		}
		if res.OK {
			return nil
		}
		return &codeSortie{code: 1, err: errors.New(res.Erreur)}
	}

	addr := net.JoinHostPort(*serveur, fmt.Sprint(*port))
	dialer := &net.Dialer{Timeout: *timeout}

	// 1. Résolution + TCP : on distingue « introuvable » de « filtré ».
	adresse, err := net.DefaultResolver.LookupIPAddr(context.Background(), *serveur)
	if err != nil {
		return terminer(fmt.Errorf("résolution DNS de %s: %w", *serveur, err))
	}
	if len(adresse) == 0 {
		return terminer(fmt.Errorf("résolution DNS de %s: aucune adresse", *serveur))
	}
	res.Adresse = adresse[0].IP.String()
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return terminer(fmt.Errorf("connexion TCP %s: %w", addr, err))
	}
	res.TCP = "ok"

	if res.Protocole != "ldaps" {
		_ = conn.Close()
		res.OK = true
		res.Erreur = ""
		return terminer(nil)
	}

	// 2. Poignée de main TLS (LDAPS).
	pool, err := chargerCA(*cheminCA)
	if err != nil {
		_ = conn.Close()
		return terminer(err)
	}
	cfg := &tls.Config{
		ServerName:         *serveur,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: !*verifier, //nolint:gosec // explicite : option --verify=false
	}
	if pool != nil {
		cfg.RootCAs = pool
	}

	tlsConn := tls.Client(conn, cfg)
	if err := tlsConn.SetDeadline(time.Now().Add(*timeout)); err != nil {
		_ = tlsConn.Close()
		return terminer(err)
	}
	if err := tlsConn.Handshake(); err != nil {
		_ = tlsConn.Close()
		res.TLS = "echec"
		return terminer(fmt.Errorf("poignée de main TLS sur %s: %w", addr, err))
	}
	defer tlsConn.Close() //nolint:errcheck

	etat := tlsConn.ConnectionState()
	if len(etat.PeerCertificates) == 0 {
		res.TLS = "echec"
		return terminer(errors.New("aucun certificat présenté par le serveur"))
	}
	cert := etat.PeerCertificates[0]
	restant := time.Until(cert.NotAfter)

	res.TLS = "ok"
	res.OK = true
	res.Sujet = cert.Subject.String()
	res.Emetteur = cert.Issuer.String()
	res.ExpireLe = cert.NotAfter.Format(time.RFC3339)
	// Arrondi au supérieur : un certificat valable encore 23 h affiche
	// « 1 jour », pas « 0 jour » (et surtout pas « expiré »).
	res.JoursRestants = int(math.Ceil(restant.Hours() / 24))
	res.EmpreinteSHA = empreinte(cert.Raw)
	res.ChaineValide = *verifier || *cheminCA != ""

	if restant <= 0 {
		res.OK = false
		res.Erreur = fmt.Sprintf("certificat expiré le %s", res.ExpireLe)
	} else if res.JoursRestants <= 15 {
		fmt.Fprintf(stderr, "avertissement: certificat arrivant à échéance dans %d jours\n", res.JoursRestants)
	}
	return terminer(nil)
}

// protocole nomme le mode d'attente attendu sur le port donné.
func protocole(port int, tlsForce bool) string {
	if port == 636 || tlsForce {
		return "ldaps"
	}
	return "ldap"
}

// chargerCA charge un certificat racine additionnel (CA interne d'un AD),
// souvent absent du magasin système du poste administrateur.
func chargerCA(chemin string) (*x509.CertPool, error) {
	if chemin == "" {
		return nil, nil
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil, fmt.Errorf("lecture du certificat CA %s: %w", chemin, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(donnees) {
		return nil, fmt.Errorf("le fichier %s ne contient aucun certificat PEM valide", chemin)
	}
	return pool, nil
}

// empreinte calcule l'empreinte SHA-256 (hexa) d'un certificat.
func empreinte(der []byte) string {
	somme := sha256.Sum256(der)
	return hex.EncodeToString(somme[:])
}
