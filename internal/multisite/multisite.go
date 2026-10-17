// Package multisite gère le multi-domaine / multi-site (M25).
//
// Un même EduSync AD peut piloter plusieurs annuaires (plusieurs
// établissements, domaines ou forêts). Les profils de domaine — libellé, nom de
// domaine, contrôleur, identifiant de connexion et, sur demande explicite, le
// mot de passe associé — sont regroupés dans domaines.json.
//
// Ce fichier est chiffré avec la clé AES-256 locale (secret.key) et écrit avec
// des permissions 0600 : il peut contenir des identifiants. Il reste local à la
// machine de l'administrateur et n'est jamais transmis. Le format (enveloppe
// {version, payload} + chiffrement par champ) est identique à la version Python
// (src/edusync_ad/core/multisite.py).
package multisite

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/crypto"
)

// VersionFormat est la version du fichier domaines.json.
const VersionFormat = 1

// NomFichier est le nom du fichier des sites, partagé avec Python.
const NomFichier = "domaines.json"

// NomFichierCle est le nom du fichier de clé locale (secret.key).
const NomFichierCle = "secret.key"

// NomFichierHerite est le fichier de connexion mémorisée (M9) migré par M25.
const NomFichierHerite = "connection.local.json"

// domaineRe valide un nom de domaine DNS (RFC 1123 simplifié).
var domaineRe = regexp.MustCompile(
	`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?(\.[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?)*$`,
)

// MultisiteError signale un fichier domaines.json illisible (corrompu, clé
// perdue, format inconnu).
type MultisiteError struct{ Message string }

// Error implémente error.
func (e *MultisiteError) Error() string { return e.Message }

// ProfilDomaine décrit un domaine/forêt géré par l'application.
type ProfilDomaine struct {
	ID                string `json:"id"`
	Label             string `json:"label"`
	Domain            string `json:"domain"`
	Controller        string `json:"controller"`
	Username          string `json:"username"`
	Password          string `json:"password"`
	RememberPassword  bool   `json:"remember_password"`
	VerifyCertificate bool   `json:"verify_certificate"`
	CACertPath        string `json:"ca_cert_path"`
}

// DisplayName retourne le libellé lisible du site.
func (p ProfilDomaine) DisplayName() string {
	if strings.TrimSpace(p.Label) != "" {
		return p.Label + " — " + p.Domain
	}
	return p.Domain
}

// StoredPassword retourne le mot de passe conservé, ou vide si l'utilisateur ne
// l'a pas mémorisé.
func (p ProfilDomaine) StoredPassword() string {
	if p.RememberPassword {
		return p.Password
	}
	return ""
}

// Validate retourne les messages d'erreur (vide = profil valide).
func (p ProfilDomaine) Validate() []string {
	var erreurs []string
	if strings.TrimSpace(p.Label) == "" {
		erreurs = append(erreurs, "Le libellé du site est obligatoire.")
	}
	domaine := strings.TrimSpace(p.Domain)
	if domaine == "" {
		erreurs = append(erreurs, "Le nom de domaine est obligatoire.")
	} else if !domaineRe.MatchString(domaine) {
		erreurs = append(erreurs, fmt.Sprintf("Nom de domaine invalide : « %s ».", p.Domain))
	}
	if p.RememberPassword && p.Password == "" {
		erreurs = append(erreurs, "Le mot de passe à mémoriser est vide.")
	}
	return erreurs
}

// Sanitized retourne une copie prête à être écrite sur disque (mot de passe
// abandonné si non mémorisé, domaine normalisé).
func (p ProfilDomaine) Sanitized() ProfilDomaine {
	return ProfilDomaine{
		ID:                p.ID,
		Label:             strings.TrimSpace(p.Label),
		Domain:            strings.ToLower(strings.TrimSpace(p.Domain)),
		Controller:        strings.TrimSpace(p.Controller),
		Username:          strings.TrimSpace(p.Username),
		Password:          p.StoredPassword(),
		RememberPassword:  p.RememberPassword,
		VerifyCertificate: p.VerifyCertificate,
		CACertPath:        strings.TrimSpace(p.CACertPath),
	}
}

// SitesPath retourne le chemin par défaut du fichier des sites.
func SitesPath() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichier), nil
}

// clePathParDefaut retourne le chemin de la clé locale.
func clePathParDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichierCle), nil
}

// nouvelID genere un identifiant de site unique (8 caractères hexadécimaux).
func nouvelID(existants []ProfilDomaine) string {
	pris := map[string]bool{}
	for _, p := range existants {
		pris[p.ID] = true
	}
	for {
		buf := make([]byte, 4)
		if _, err := rand.Read(buf); err != nil {
			continue
		}
		candidat := hex.EncodeToString(buf)
		if !pris[candidat] {
			return candidat
		}
	}
}

// NewSiteID retourne un identifiant de site qui n'entre pas en collision avec
// les identifiants existants.
func NewSiteID(existants []ProfilDomaine) string {
	return nouvelID(existants)
}

// NouveauProfil construit un profil de domaine avec un identifiant généré.
func NouveauProfil(label, domain string, options ...OptionProfil) ProfilDomaine {
	profil := ProfilDomaine{
		ID:                nouvelID(nil),
		Label:             label,
		Domain:            domain,
		VerifyCertificate: true,
	}
	for _, option := range options {
		option(&profil)
	}
	return profil
}

// OptionProfil modifie un profil à la construction.
type OptionProfil func(*ProfilDomaine)

// AvecControleur définit le contrôleur du profil.
func AvecControleur(controleur string) OptionProfil {
	return func(p *ProfilDomaine) { p.Controller = controleur }
}

// AvecIdentifiant définit l'identifiant de connexion.
func AvecIdentifiant(utilisateur string) OptionProfil {
	return func(p *ProfilDomaine) { p.Username = utilisateur }
}

// AvecMotDePasse définit le mot de passe et l'option de mémorisation.
func AvecMotDePasse(motDePasse string, memoriser bool) OptionProfil {
	return func(p *ProfilDomaine) {
		p.Password = motDePasse
		p.RememberPassword = memoriser
	}
}

// AvecVerificationCertificat active/désactive la vérification du certificat.
func AvecVerificationCertificat(verifier bool) OptionProfil {
	return func(p *ProfilDomaine) { p.VerifyCertificate = verifier }
}

// AvecCACert définit le chemin du certificat d'autorité.
func AvecCACert(chemin string) OptionProfil {
	return func(p *ProfilDomaine) { p.CACertPath = chemin }
}

// AvecID force l'identifiant du profil.
func AvecID(id string) OptionProfil {
	return func(p *ProfilDomaine) { p.ID = id }
}

// AvecExistants fournit les profils existants pour générer un identifiant unique.
func AvecExistants(existants []ProfilDomaine) OptionProfil {
	return func(p *ProfilDomaine) { p.ID = nouvelID(existants) }
}

// chargerCle retourne la clé de chiffrement locale (créée si absente).
func chargerCle(clePath string) ([]byte, error) {
	if clePath == "" {
		defaut, err := clePathParDefaut()
		if err != nil {
			return nil, err
		}
		clePath = defaut
	}
	return crypto.LoadOrCreateKey(clePath)
}

// ChargerSites charge domaines.json. Retourne une liste vide si le fichier est
// absent ; une *MultisiteError si le fichier est illisible.
func ChargerSites(chemin, clePath string) ([]ProfilDomaine, error) {
	if chemin == "" {
		defaut, err := SitesPath()
		if err != nil {
			return nil, err
		}
		chemin = defaut
	}
	if _, err := os.Stat(chemin); errors.Is(err, os.ErrNotExist) {
		return []ProfilDomaine{}, nil
	}
	cle, err := chargerCle(clePath)
	if err != nil {
		return nil, &MultisiteError{Message: fmt.Sprintf("domaines.json illisible : %v", err)}
	}

	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil, &MultisiteError{Message: fmt.Sprintf("domaines.json illisible : %v", err)}
	}
	var enveloppe struct {
		Version *int   `json:"version"`
		Payload string `json:"payload"`
	}
	if err := json.Unmarshal(donnees, &enveloppe); err != nil {
		return nil, &MultisiteError{Message: fmt.Sprintf("domaines.json illisible : %v", err)}
	}
	version := VersionFormat
	if enveloppe.Version != nil {
		version = *enveloppe.Version
	}
	if version != VersionFormat {
		return nil, &MultisiteError{Message: fmt.Sprintf("Version de fichier de domaines non gérée : %d", version)}
	}
	clair, err := crypto.Decrypt(cle, enveloppe.Payload)
	if err != nil {
		return nil, &MultisiteError{Message: fmt.Sprintf("domaines.json illisible : %v", err)}
	}
	return profilsDepuisPayload([]byte(clair))
}

// profilsDepuisPayload décode la liste de profils (identifiants complétés).
func profilsDepuisPayload(donnees []byte) ([]ProfilDomaine, error) {
	var bruts []map[string]any
	if err := json.Unmarshal(donnees, &bruts); err != nil {
		return nil, &MultisiteError{Message: fmt.Sprintf("domaines.json illisible : %v", err)}
	}
	profils := make([]ProfilDomaine, 0, len(bruts))
	for _, brut := range bruts {
		profil := ProfilDomaine{
			ID:                chaine(brut["id"]),
			Label:             chaine(brut["label"]),
			Domain:            chaine(brut["domain"]),
			Controller:        chaine(brut["controller"]),
			Username:          chaine(brut["username"]),
			Password:          chaine(brut["password"]),
			RememberPassword:  booleen(brut["remember_password"]),
			VerifyCertificate: booleenDefaut(brut["verify_certificate"], true),
			CACertPath:        chaine(brut["ca_cert_path"]),
		}
		if profil.ID == "" {
			profil.ID = nouvelID(profils)
		}
		profils = append(profils, profil)
	}
	return profils, nil
}

// chaine retourne une valeur comme chaîne (vide si absente).
func chaine(valeur any) string {
	if valeur == nil {
		return ""
	}
	if s, ok := valeur.(string); ok {
		return s
	}
	return fmt.Sprint(valeur)
}

// booleen retourne une valeur comme booléen (faux si absente).
func booleen(valeur any) bool {
	b, _ := valeur.(bool)
	return b
}

// booleenDefaut retourne une valeur comme booléen, avec valeur par défaut si
// absente (nil).
func booleenDefaut(valeur any, defaut bool) bool {
	if valeur == nil {
		return defaut
	}
	b, ok := valeur.(bool)
	if !ok {
		return defaut
	}
	return b
}

// SauvegarderSites écrit domaines.json chiffré, permissions 0600.
func SauvegarderSites(profils []ProfilDomaine, chemin, clePath string) error {
	if chemin == "" {
		defaut, err := SitesPath()
		if err != nil {
			return err
		}
		chemin = defaut
	}
	cle, err := chargerCle(clePath)
	if err != nil {
		return err
	}

	nettoyes := make([]ProfilDomaine, 0, len(profils))
	for _, profil := range profils {
		nettoyes = append(nettoyes, profil.Sanitized())
	}
	blob, err := json.Marshal(nettoyes)
	if err != nil {
		return fmt.Errorf("multisite: encodage des profils: %w", err)
	}
	jeton, err := crypto.Encrypt(cle, string(blob))
	if err != nil {
		return fmt.Errorf("multisite: chiffrement: %w", err)
	}
	enveloppe, err := json.Marshal(map[string]any{"version": VersionFormat, "payload": jeton})
	if err != nil {
		return fmt.Errorf("multisite: encodage de l'enveloppe: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return fmt.Errorf("multisite: création du dossier: %w", err)
	}
	if err := os.WriteFile(chemin, enveloppe, 0o600); err != nil {
		return fmt.Errorf("multisite: écriture %s: %w", chemin, err)
	}
	// os.WriteFile ne force pas les permissions d'un fichier existant.
	_ = os.Chmod(chemin, 0o600)
	return nil
}

// TrouverProfil retourne le profil d'identifiant « siteID » (nil si absent).
func TrouverProfil(profils []ProfilDomaine, siteID string) *ProfilDomaine {
	if siteID == "" {
		return nil
	}
	for i := range profils {
		if profils[i].ID == siteID {
			return &profils[i]
		}
	}
	return nil
}

// ProfilPourDomaine retourne le profil correspondant à un nom de domaine (nil si
// absent), comparaison insensible à la casse.
func ProfilPourDomaine(profils []ProfilDomaine, domain string) *ProfilDomaine {
	if strings.TrimSpace(domain) == "" {
		return nil
	}
	voulu := strings.ToLower(strings.TrimSpace(domain))
	for i := range profils {
		if strings.ToLower(strings.TrimSpace(profils[i].Domain)) == voulu {
			return &profils[i]
		}
	}
	return nil
}

// connexionHeritee décrit connection.local.json (M9), champs chiffrés un par un.
type connexionHeritee struct {
	Domaine     string
	Controleur  string
	Utilisateur string
	MotDePasse  string
	aMotDePasse bool
}

// chargerConnexionHeritee lit connection.local.json (chaque champ chiffré).
func chargerConnexionHeritee(chemin, clePath string) *connexionHeritee {
	if chemin == "" {
		defaut, err := config.ConfigDir()
		if err != nil {
			return nil
		}
		chemin = filepath.Join(defaut, NomFichierHerite)
	}
	if _, err := os.Stat(chemin); err != nil {
		return nil
	}
	cle, err := chargerCle(clePath)
	if err != nil {
		return nil
	}
	donnees, err := os.ReadFile(chemin)
	if err != nil {
		return nil
	}
	var payload map[string]string
	if err := json.Unmarshal(donnees, &payload); err != nil {
		return nil
	}
	connexion := &connexionHeritee{}
	for champ, jeton := range payload {
		valeur, err := crypto.Decrypt(cle, jeton)
		if err != nil {
			return nil
		}
		switch champ {
		case "domaine":
			connexion.Domaine = valeur
		case "controleur":
			connexion.Controleur = valeur
		case "utilisateur":
			connexion.Utilisateur = valeur
		case "mot_de_passe":
			connexion.MotDePasse = valeur
			connexion.aMotDePasse = true
		}
	}
	return connexion
}

// ImporterConnexionHeritee migre connection.local.json (M9). Retourne nil s'il
// n'y a rien à migrer (fichier absent, domaine déjà présent ou vide).
func ImporterConnexionHeritee(chemin, clePath, cheminHerite string) (*ProfilDomaine, error) {
	herite := chargerConnexionHeritee(cheminHerite, clePath)
	if herite == nil || strings.TrimSpace(herite.Domaine) == "" {
		return nil, nil
	}
	existants, err := ChargerSites(chemin, clePath)
	if err != nil {
		return nil, err
	}
	if ProfilPourDomaine(existants, herite.Domaine) != nil {
		return nil, nil
	}
	profil := NouveauProfil(herite.Domaine, herite.Domaine,
		AvecControleur(herite.Controleur),
		AvecIdentifiant(herite.Utilisateur),
		AvecMotDePasse(herite.MotDePasse, herite.aMotDePasse),
		AvecExistants(existants),
	)
	return &profil, nil
}

// AssurerSites charge les sites, avec migration de la connexion mémorisée si le
// fichier est vide. Ne retourne jamais d'erreur bloquante : un fichier illisible
// est simplement ignoré.
func AssurerSites(chemin, clePath, cheminHerite string, journal func(string)) []ProfilDomaine {
	profils, err := ChargerSites(chemin, clePath)
	if err != nil {
		if journal != nil {
			journal("Fichier de domaines ignoré : " + err.Error())
		}
		return []ProfilDomaine{}
	}
	if len(profils) > 0 {
		return profils
	}
	herite, err := ImporterConnexionHeritee(chemin, clePath, cheminHerite)
	if err != nil {
		if journal != nil {
			journal("Fichier de domaines ignoré : " + err.Error())
		}
		return []ProfilDomaine{}
	}
	if herite == nil {
		return []ProfilDomaine{}
	}
	if err := SauvegarderSites([]ProfilDomaine{*herite}, chemin, clePath); err != nil {
		if journal != nil {
			journal("Impossible d'enregistrer le site migré : " + err.Error())
		}
	}
	return []ProfilDomaine{*herite}
}
