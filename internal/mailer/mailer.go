// Package mailer envoie des courriels SMTP — mutualisé par les étiquettes
// (M24) et, à terme, le portail auto-service (M27).
//
// Le mot de passe SMTP n'est chiffré qu'au stockage (AES-256-GCM,
// internal/crypto), jamais écrit en clair. Le fichier produit
// (`label_mail.json`) est **strictement compatible** avec la version Python
// (src/edusync_ad/core/mailer.py) : clé `password_token`, mêmes noms de champs.
package mailer

import (
	"bytes"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"net"
	"net/smtp"
	"net/textproto"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/crypto"
)

// NomFichier est le nom du fichier de configuration SMTP (conservé depuis M24,
// l'UI des étiquettes l'utilise).
const NomFichier = "label_mail.json"

// NomFichierCle est le nom du fichier de clé locale (secret.key).
const NomFichierCle = "secret.key"

// ErreurMail signale un échec de configuration ou d'envoi SMTP (message déjà
// en français).
type ErreurMail struct{ Message string }

// Error implémente error.
func (e *ErreurMail) Error() string { return e.Message }

func erreurMail(format string, args ...any) error {
	return &ErreurMail{Message: fmt.Sprintf(format, args...)}
}

// MailConfig est la configuration SMTP — le mot de passe n'est chiffré qu'au
// stockage.
type MailConfig struct {
	Host     string  `json:"host"`
	Port     int     `json:"port"`
	Username string  `json:"username"`
	Password string  `json:"password,omitempty"`
	UseTLS   bool    `json:"use_tls"`
	UseSSL   bool    `json:"use_ssl"`
	FromAddr string  `json:"from_addr"`
	FromName string  `json:"from_name"`
	Timeout  float64 `json:"timeout"`
}

// ConfigMailDefaut retourne une configuration vide (port 587, STARTTLS).
func ConfigMailDefaut() *MailConfig {
	return &MailConfig{Port: 587, UseTLS: true, FromName: "EduSync AD", Timeout: 20.0}
}

// Validate vérifie la configuration (messages français, comme Python).
func (c *MailConfig) Validate() []string {
	var erreurs []string
	if strings.TrimSpace(c.Host) == "" {
		erreurs = append(erreurs, "Serveur SMTP renseigné requis.")
	}
	if c.Port < 1 || c.Port > 65535 {
		erreurs = append(erreurs, "Port SMTP invalide (1-65535).")
	}
	if strings.TrimSpace(c.FromAddr) == "" {
		erreurs = append(erreurs, "Adresse d'expéditrice requise.")
	} else if !strings.Contains(c.FromAddr, "@") {
		erreurs = append(erreurs, fmt.Sprintf("Adresse d'expéditrice « %s » invalide.", c.FromAddr))
	}
	if c.UseSSL && c.UseTLS {
		erreurs = append(erreurs, "SSL implicite et STARTTLS sont exclusifs.")
	}
	return erreurs
}

// VersDictStocke sérialise la configuration pour le disque (mot de passe
// chiffré sous la clé « password_token »).
func (c *MailConfig) VersDictStocke(clePath string) (map[string]any, error) {
	data := map[string]any{
		"host":      c.Host,
		"port":      c.Port,
		"username":  c.Username,
		"use_tls":   c.UseTLS,
		"use_ssl":   c.UseSSL,
		"from_addr": c.FromAddr,
		"from_name": c.FromName,
		"timeout":   c.Timeout,
	}
	if c.Password != "" {
		cle, err := clePour(clePath)
		if err != nil {
			return nil, err
		}
		jeton, err := crypto.Encrypt(cle, c.Password)
		if err != nil {
			return nil, fmt.Errorf("mailer: chiffrement du mot de passe: %w", err)
		}
		data["password_token"] = jeton
	}
	return data, nil
}

// DepuisDictStocke reconstruit la configuration depuis le format Python.
func DepuisDictStocke(data map[string]any, clePath string) *MailConfig {
	cfg := ConfigMailDefaut()
	cfg.Host = chaine(data["host"], "")
	cfg.Port = entier(data["port"], 587)
	cfg.Username = chaine(data["username"], "")
	cfg.UseTLS = booleen(data["use_tls"], true)
	cfg.UseSSL = booleen(data["use_ssl"], false)
	cfg.FromAddr = chaine(data["from_addr"], "")
	cfg.FromName = chaine(data["from_name"], "EduSync AD")
	cfg.Timeout = nombre(data["timeout"], 20.0)
	if jeton := chaine(data["password_token"], ""); jeton != "" {
		cle, err := clePour(clePath)
		if err != nil {
			return cfg
		}
		if clair, err := crypto.Decrypt(cle, jeton); err == nil {
			cfg.Password = clair
		} else {
			cfg.Password = "" // clé de chiffrement perdue : à ressaisir
		}
	}
	return cfg
}

// CheminConfigParDefaut retourne le chemin par défaut de label_mail.json.
func CheminConfigParDefaut() string {
	dir, err := config.ConfigDir()
	if err != nil || dir == "" {
		return NomFichier
	}
	return filepath.Join(dir, NomFichier)
}

// CheminCleParDefaut retourne le chemin par défaut de la clé locale.
func CheminCleParDefaut() string {
	dir, err := config.ConfigDir()
	if err != nil || dir == "" {
		return NomFichierCle
	}
	return filepath.Join(dir, NomFichierCle)
}

func clePour(clePath string) ([]byte, error) {
	if clePath == "" {
		clePath = CheminCleParDefaut()
	}
	return crypto.LoadOrCreateKey(clePath)
}

// ChargerConfig lit la configuration SMTP (fichier absent ou illisible →
// configuration vide : jamais bloquant).
func ChargerConfig(chemin, clePath string) *MailConfig {
	if chemin == "" {
		chemin = CheminConfigParDefaut()
	}
	data, err := os.ReadFile(chemin)
	if err != nil {
		return ConfigMailDefaut()
	}
	var brut any
	if err := json.Unmarshal(data, &brut); err != nil {
		return ConfigMailDefaut()
	}
	carte, ok := brut.(map[string]any)
	if !ok {
		return ConfigMailDefaut()
	}
	return DepuisDictStocke(carte, clePath)
}

// SauvegarderConfig écrit la configuration SMTP après validation (mot de passe
// chiffré sur disque).
func SauvegarderConfig(cfg *MailConfig, chemin, clePath string) (string, error) {
	if errs := cfg.Validate(); len(errs) > 0 {
		return "", erreurMail("Configuration SMTP invalide : %s", strings.Join(errs, "; "))
	}
	if chemin == "" {
		chemin = CheminConfigParDefaut()
	}
	dict, err := cfg.VersDictStocke(clePath)
	if err != nil {
		return "", err
	}
	if dir := filepath.Dir(chemin); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mailer: création du dossier %s: %w", dir, err)
		}
	}
	data, err := json.MarshalIndent(dict, "", "  ")
	if err != nil {
		return "", fmt.Errorf("mailer: encodage JSON: %w", err)
	}
	if err := os.WriteFile(chemin, data, 0o600); err != nil {
		return "", fmt.Errorf("mailer: écriture de %s: %w", chemin, err)
	}
	return chemin, nil
}

// Message est un courriel prêt à être transmis.
type Message struct {
	Sujet          string
	De             string
	Vers           string
	Corps          string
	PieceJointeNom string
	PieceJointe    []byte
}

// ConstruireMessage construit le message (configuration et destinataire
// contrôlés). Lève une ErreurMail si la configuration est incomplète ou si le
// destinataire n'est pas une adresse valide.
func ConstruireMessage(cfg *MailConfig, vers, sujet, corps, sujetDefaut string) (*Message, error) {
	if errs := cfg.Validate(); len(errs) > 0 {
		return nil, erreurMail("Configuration SMTP invalide : %s", strings.Join(errs, "; "))
	}
	vers = strings.TrimSpace(vers)
	if !strings.Contains(vers, "@") {
		return nil, erreurMail("Adresse du destinataire « %s » invalide.", vers)
	}
	titre := strings.TrimSpace(sujet)
	if titre == "" {
		titre = sujetDefaut
	}
	if titre == "" {
		titre = "Message EduSync AD"
	}
	return &Message{
		Sujet: titre,
		De:    fmt.Sprintf("%s <%s>", cfg.FromName, cfg.FromAddr),
		Vers:  vers,
		Corps: corps,
	}, nil
}

// Octets sérialise le message en RFC 5322 (UTF-8, pièce jointe base64).
func (m *Message) Octets() ([]byte, error) {
	var corps bytes.Buffer
	entetes := textproto.MIMEHeader{}
	entetes.Set("From", m.De)
	entetes.Set("To", m.Vers)
	entetes.Set("Subject", encoderEntete(m.Sujet))

	if len(m.PieceJointe) == 0 {
		entetes.Set("MIME-Version", "1.0")
		entetes.Set("Content-Type", "text/plain; charset=\"utf-8\"")
		entetes.Set("Content-Transfer-Encoding", "base64")
		entetes.Set("Content-Disposition", "inline")
		if _, err := ecrireEntetes(&corps, entetes); err != nil {
			return nil, err
		}
		corps.WriteString("\r\n")
		corps.WriteString(encoderBase64Lignes([]byte(m.Corps)))
		return corps.Bytes(), nil
	}

	limite := fmt.Sprintf("edusync-%d", time.Now().UnixNano())
	entetes.Set("MIME-Version", "1.0")
	entetes.Set("Content-Type", "multipart/mixed; boundary=\""+limite+"\"")
	if _, err := ecrireEntetes(&corps, entetes); err != nil {
		return nil, err
	}
	corps.WriteString("\r\n")

	multi := multipart.NewWriter(&corps)
	if err := multi.SetBoundary(limite); err != nil {
		return nil, err
	}

	// Corps du message.
	teteCorps := textproto.MIMEHeader{}
	teteCorps.Set("Content-Type", "text/plain; charset=\"utf-8\"")
	teteCorps.Set("Content-Transfer-Encoding", "base64")
	teteCorps.Set("Content-Disposition", "inline")
	partieCorps, err := multi.CreatePart(teteCorps)
	if err != nil {
		return nil, err
	}
	if _, err := partieCorps.Write([]byte(encoderBase64Lignes([]byte(m.Corps)))); err != nil {
		return nil, err
	}

	// Pièce jointe.
	tetePiece := textproto.MIMEHeader{}
	tetePiece.Set("Content-Type", "application/pdf")
	tetePiece.Set("Content-Transfer-Encoding", "base64")
	tetePiece.Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", m.PieceJointeNom))
	partiePiece, err := multi.CreatePart(tetePiece)
	if err != nil {
		return nil, err
	}
	if _, err := partiePiece.Write([]byte(encoderBase64Lignes(m.PieceJointe))); err != nil {
		return nil, err
	}
	if err := multi.Close(); err != nil {
		return nil, err
	}
	return corps.Bytes(), nil
}

// Envoyeur transporte un message déjà construit jusqu'au serveur SMTP.
// L'implémentation par défaut utilise net/smtp ; les tests injectent un faux.
type Envoyeur interface {
	Envoyer(cfg *MailConfig, msg *Message) error
}

// Envoyer transmet un message déjà construit — lève une ErreurMail.
func Envoyer(cfg *MailConfig, msg *Message, envoyeur Envoyeur) error {
	if envoyeur == nil {
		envoyeur = envoyeurSMTP{}
	}
	if err := envoyeur.Envoyer(cfg, msg); err != nil {
		var em *ErreurMail
		if errors.As(err, &em) {
			return err
		}
		return erreurMail("Envoi impossible : %v", err)
	}
	return nil
}

// EnvoyerMail envoie un courriel sans pièce jointe — lève une ErreurMail.
func EnvoyerMail(cfg *MailConfig, vers, sujet, corps, sujetDefaut string, envoyeur Envoyeur) error {
	msg, err := ConstruireMessage(cfg, vers, sujet, corps, sujetDefaut)
	if err != nil {
		return err
	}
	return Envoyer(cfg, msg, envoyeur)
}

// EnvoyerEtiquette envoie l'étiquette PDF d'un destinataire — lève une
// ErreurMail. Les octets doivent être un PDF.
func EnvoyerEtiquette(cfg *MailConfig, vers, sujet, corps string, donneesPDF []byte, nomFichier string, envoyeur Envoyeur) error {
	msg, err := ConstruireMessage(cfg, vers, sujet, corps, "Votre étiquette")
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(donneesPDF, []byte("%PDF")) {
		return erreurMail("Le fichier à joindre n'est pas un PDF.")
	}
	if nomFichier == "" {
		nomFichier = "etiquette.pdf"
	}
	msg.PieceJointeNom = nomFichier
	msg.PieceJointe = donneesPDF
	return Envoyer(cfg, msg, envoyeur)
}

// EnvoyerEtiquetteFichier lit le PDF depuis le disque puis envoie.
func EnvoyerEtiquetteFichier(cfg *MailConfig, vers, sujet, corps, cheminPDF, nomFichier string, envoyeur Envoyeur) error {
	donnees, err := os.ReadFile(cheminPDF)
	if err != nil {
		return erreurMail("Lecture de l'étiquette PDF impossible : %v", err)
	}
	if nomFichier == "" {
		nomFichier = filepath.Base(cheminPDF)
	}
	return EnvoyerEtiquette(cfg, vers, sujet, corps, donnees, nomFichier, envoyeur)
}

// envoyeurSMTP est l'implémentation par défaut (net/smtp).
type envoyeurSMTP struct{}

func (envoyeurSMTP) Envoyer(cfg *MailConfig, msg *Message) error {
	donnees, err := msg.Octets()
	if err != nil {
		return erreurMail("Envoi impossible : %v", err)
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))
	timeout := time.Duration(cfg.Timeout * float64(time.Second))
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	dialer := &net.Dialer{Timeout: timeout}

	var conn net.Conn
	if cfg.UseSSL {
		conn, err = tlsDial(dialer, addr, cfg.Host)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return erreurMail("Envoi impossible : %v", err)
	}
	client, err := smtp.NewClient(conn, cfg.Host)
	if err != nil {
		_ = conn.Close()
		return erreurMail("Envoi impossible : %v", err)
	}
	defer client.Close() //nolint:errcheck // fermeture best-effort

	if !cfg.UseSSL && cfg.UseTLS {
		if err := client.StartTLS(tlsConfig(cfg.Host)); err != nil {
			return erreurMail("Envoi impossible : %v", err)
		}
	}
	if cfg.Username != "" {
		auth := smtp.PlainAuth("", cfg.Username, cfg.Password, cfg.Host)
		if err := client.Auth(auth); err != nil {
			return erreurMail("Authentification SMTP refusée (%v).", err)
		}
	}
	if err := client.Mail(cfg.FromAddr); err != nil {
		return erreurMail("Envoi impossible : %v", err)
	}
	for _, dest := range strings.Split(msg.Vers, ",") {
		dest = strings.TrimSpace(dest)
		if dest == "" {
			continue
		}
		if err := client.Rcpt(dest); err != nil {
			return erreurMail("Destinataire refusé par le serveur SMTP.")
		}
	}
	w, err := client.Data()
	if err != nil {
		return erreurMail("Envoi impossible : %v", err)
	}
	if _, err := w.Write(donnees); err != nil {
		_ = w.Close()
		return erreurMail("Envoi impossible : %v", err)
	}
	if err := w.Close(); err != nil {
		return erreurMail("Envoi impossible : %v", err)
	}
	return nil
}

// ─── Utilitaires ─────────────────────────────────────────────────────────

// tlsConfig retourne la configuration TLS utilisée pour SMTP.
func tlsConfig(serveur string) *tls.Config {
	return &tls.Config{ServerName: serveur, MinVersion: tls.VersionTLS12}
}

// tlsDial ouvre une connexion TLS implicite (SMTP sur SSL, port 465).
func tlsDial(dialer *net.Dialer, addr, serveur string) (net.Conn, error) {
	return tls.DialWithDialer(dialer, "tcp", addr, tlsConfig(serveur))
}

func ecrireEntetes(buf *bytes.Buffer, entetes textproto.MIMEHeader) (int, error) {
	ordre := []string{"From", "To", "Subject", "MIME-Version", "Content-Type", "Content-Transfer-Encoding", "Content-Disposition"}
	for _, cle := range ordre {
		for _, valeur := range entetes.Values(cle) {
			if _, err := fmt.Fprintf(buf, "%s: %s\r\n", cle, valeur); err != nil {
				return 0, err
			}
		}
	}
	return buf.Len(), nil
}

func encoderEntete(texte string) string {
	for _, r := range texte {
		if r > 127 {
			return mime.BEncoding.Encode("utf-8", texte)
		}
	}
	return texte
}

// encoderBase64Lignes encode en base64 et découpe en lignes de 76 caractères
// (limite RFC 2045).
func encoderBase64Lignes(donnees []byte) string {
	encode := base64.StdEncoding.EncodeToString(donnees)
	var b strings.Builder
	for len(encode) > 76 {
		b.WriteString(encode[:76])
		b.WriteString("\r\n")
		encode = encode[76:]
	}
	b.WriteString(encode)
	return b.String()
}

func chaine(v any, defaut string) string {
	if s, ok := v.(string); ok {
		return s
	}
	return defaut
}

func entier(v any, defaut int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	}
	return defaut
}

func nombre(v any, defaut float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	}
	return defaut
}

func booleen(v any, defaut bool) bool {
	if b, ok := v.(bool); ok {
		return b
	}
	return defaut
}
