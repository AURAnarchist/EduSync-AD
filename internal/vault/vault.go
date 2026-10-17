// Package vault implémente le coffre-fort local des mots de passe positionnés
// par l'application.
//
// Exception délibérée et strictement limitée à la règle énoncée dans
// internal/crypto (« le mot de passe n'est jamais écrit sur disque ») : celle-ci
// concerne les identifiants de connexion à l'AD. Ici, sur demande explicite de
// l'utilisateur, EduSync AD retient les mots de passe de comptes élèves ou
// personnels qu'il vient lui-même de positionner (création, réinitialisation),
// pour pouvoir les réafficher ensuite (fiche utilisateur, export CSV/étiquettes)
// sans repasser par une réinitialisation. Un mot de passe changé par un autre
// outil n'est jamais connu d'EduSync AD et reste introuvable — comportement
// attendu.
//
// Chiffrement AES-256-GCM avec la même clé que internal/crypto (fichier
// secret.key, permissions restrictives). Stocké dans une base SQLite locale,
// elle aussi à permissions restrictives — jamais sur le serveur AD. Le schéma
// et le fichier password_vault.sqlite3 sont identiques à la version Python
// (src/edusync_ad/core/password_vault.py).
package vault

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite" // pilote pur Go, sans CGO

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/crypto"
)

const (
	// NomFichier est le nom du fichier du coffre, partagé avec Python.
	NomFichier = "password_vault.sqlite3"
	// NomFichierCle est le nom du fichier de clé (secret.key).
	NomFichierCle = "secret.key"
)

// CheminParDefaut retourne le chemin par défaut du coffre, partagé avec Python
// (dossier de données utilisateur).
func CheminParDefaut() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichier), nil
}

// CheminCleParDefaut retourne le chemin de la clé locale (secret.key).
func CheminCleParDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichierCle), nil
}

// Vault est le coffre-fort local des mots de passe.
type Vault struct {
	chemin  string
	clePath string
	db      *sql.DB
}

// New ouvre (et crée) le coffre situé à « chemin ». Un chemin vide utilise le
// chemin par défaut. Un chemin de clé vide utilise la clé locale partagée.
func New(chemin, clePath string) (*Vault, error) {
	if chemin == "" {
		var err error
		if chemin, err = CheminParDefaut(); err != nil {
			return nil, err
		}
	}
	if clePath == "" {
		var err error
		if clePath, err = CheminCleParDefaut(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return nil, fmt.Errorf("vault: création du dossier %s: %w", filepath.Dir(chemin), err)
	}

	db, err := sql.Open("sqlite", dsn(chemin))
	if err != nil {
		return nil, fmt.Errorf("vault: ouverture %s: %w", chemin, err)
	}
	db.SetMaxOpenConns(1)

	v := &Vault{chemin: chemin, clePath: clePath, db: db}
	if err := v.initialiser(); err != nil {
		_ = db.Close()
		return nil, err
	}
	// Permissions restrictives (propriétaire uniquement), comme Python.
	_ = os.Chmod(chemin, 0o600)
	return v, nil
}

// dsn construit la chaîne de connexion du pilote modernc.org/sqlite.
func dsn(chemin string) string {
	safe := strings.ReplaceAll(chemin, "?", "%3F")
	safe = strings.ReplaceAll(safe, "#", "%23")
	return "file:" + safe + "?_pragma=busy_timeout(5000)"
}

// initialiser crée le schéma (identique à Python).
func (v *Vault) initialiser() error {
	_, err := v.db.Exec(`
		CREATE TABLE IF NOT EXISTS mots_de_passe (
			sam TEXT PRIMARY KEY,
			secret TEXT NOT NULL,
			horodatage TEXT NOT NULL
		)
	`)
	if err != nil {
		return fmt.Errorf("vault: création du schéma: %w", err)
	}
	return nil
}

// cle retourne la clé de chiffrement locale (créée si absente).
func (v *Vault) cle() ([]byte, error) {
	return crypto.LoadOrCreateKey(v.clePath)
}

// Store enregistre (ou remplace) le mot de passe d'un compte. Les identifiants
// vides sont ignorés.
func (v *Vault) Store(sam, motDePasse string) error {
	if strings.TrimSpace(sam) == "" || motDePasse == "" {
		return nil
	}
	cle, err := v.cle()
	if err != nil {
		return fmt.Errorf("vault: clé de chiffrement: %w", err)
	}
	jeton, err := crypto.Encrypt(cle, motDePasse)
	if err != nil {
		return fmt.Errorf("vault: chiffrement: %w", err)
	}
	_, err = v.db.Exec(`
		INSERT INTO mots_de_passe (sam, secret, horodatage) VALUES (?, ?, datetime('now'))
		ON CONFLICT(sam) DO UPDATE SET secret = excluded.secret, horodatage = excluded.horodatage
	`, strings.ToLower(sam), jeton)
	if err != nil {
		return fmt.Errorf("vault: enregistrement de %s: %w", sam, err)
	}
	return nil
}

// Get retourne le mot de passe d'un compte. Le second retour vaut false si le
// compte est inconnu ou si l'entrée n'est pas déchiffrable (clé changée) — dans
// ce cas l'appelant le traite comme « non enregistré ».
func (v *Vault) Get(sam string) (string, bool) {
	if strings.TrimSpace(sam) == "" {
		return "", false
	}
	var jeton string
	if err := v.db.QueryRow(
		"SELECT secret FROM mots_de_passe WHERE sam = ?", strings.ToLower(sam),
	).Scan(&jeton); err != nil {
		return "", false
	}
	cle, err := v.cle()
	if err != nil {
		return "", false
	}
	motDePasse, err := crypto.Decrypt(cle, jeton)
	if err != nil {
		return "", false
	}
	return motDePasse, true
}

// Has indique si un compte est présent dans le coffre.
func (v *Vault) Has(sam string) bool {
	if strings.TrimSpace(sam) == "" {
		return false
	}
	var present int
	err := v.db.QueryRow(
		"SELECT 1 FROM mots_de_passe WHERE sam = ?", strings.ToLower(sam),
	).Scan(&present)
	return err == nil
}

// Delete supprime l'entrée d'un compte.
func (v *Vault) Delete(sam string) error {
	if strings.TrimSpace(sam) == "" {
		return nil
	}
	if _, err := v.db.Exec("DELETE FROM mots_de_passe WHERE sam = ?", strings.ToLower(sam)); err != nil {
		return fmt.Errorf("vault: suppression de %s: %w", sam, err)
	}
	return nil
}

// ClearAll purge tout le coffre et retourne le nombre d'entrées supprimées.
func (v *Vault) ClearAll() (int, error) {
	nombre, err := v.Count()
	if err != nil {
		return 0, err
	}
	if _, err := v.db.Exec("DELETE FROM mots_de_passe"); err != nil {
		return 0, fmt.Errorf("vault: purge: %w", err)
	}
	return nombre, nil
}

// Count retourne le nombre d'entrées du coffre.
func (v *Vault) Count() (int, error) {
	var nombre int
	if err := v.db.QueryRow("SELECT COUNT(*) FROM mots_de_passe").Scan(&nombre); err != nil {
		return 0, fmt.Errorf("vault: comptage: %w", err)
	}
	return nombre, nil
}

// Liste retourne les identifiants (sam) présents, triés par ordre alphabétique.
func (v *Vault) Liste() ([]string, error) {
	lignes, err := v.db.Query("SELECT sam FROM mots_de_passe ORDER BY sam")
	if err != nil {
		return nil, fmt.Errorf("vault: listage: %w", err)
	}
	defer func() { _ = lignes.Close() }()
	var sams []string
	for lignes.Next() {
		var sam string
		if err := lignes.Scan(&sam); err != nil {
			return nil, fmt.Errorf("vault: lecture: %w", err)
		}
		sams = append(sams, sam)
	}
	return sams, lignes.Err()
}

// Close ferme le coffre.
func (v *Vault) Close() error {
	if v == nil || v.db == nil {
		return nil
	}
	return v.db.Close()
}
