package portal

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pilote pur Go, sans CGO

	"github.com/estemobs/EduSync-AD/internal/config"
)

// schemaStore reproduit **exactement** le schéma créé par `core/portal.py` :
// un fichier `portail.db` écrit par Python se rouvre tel quel en Go, et
// inversement, sans migration.
const schemaStore = `
CREATE TABLE IF NOT EXISTS codes (
    id TEXT PRIMARY KEY,
    scope TEXT NOT NULL,
    cible TEXT NOT NULL,
    dn TEXT NOT NULL,
    sam TEXT NOT NULL,
    cn TEXT NOT NULL DEFAULT '',
    mail TEXT NOT NULL DEFAULT '',
    disabled INTEGER NOT NULL DEFAULT 0,
    salt BLOB NOT NULL,
    digest BLOB NOT NULL,
    expires_at TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0,
    consumed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS codes_cible ON codes (cible);
CREATE TABLE IF NOT EXISTS demandes (
    id TEXT PRIMARY KEY,
    profil TEXT NOT NULL,
    prenom TEXT NOT NULL,
    nom TEXT NOT NULL,
    mail TEXT NOT NULL DEFAULT '',
    classe TEXT NOT NULL DEFAULT '',
    motif TEXT NOT NULL DEFAULT '',
    statut TEXT NOT NULL DEFAULT 'en_attente',
    created_at TEXT NOT NULL,
    traite_le TEXT NOT NULL DEFAULT '',
    traite_par TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT ''
);
`

// Raisons de refus d'un code (utilisées par le service et le journal).
const (
	raisonInconnu          = "inconnu"
	raisonConsomme         = "consomme"
	raisonExpire           = "expire"
	raisonTropDeTentatives = "trop_de_tentatives"
	raisonCodeIncorrect    = "code_incorrect"
	raisonOK               = "ok"
)

// ligneCode est une ligne de la table `codes`.
type ligneCode struct {
	id          string
	scope       string
	cible       string
	utilisateur Utilisateur
	sel         []byte
	empreinte   []byte
	expireLe    time.Time
	tentatives  int
	consomme    bool
}

// Store conserve les tickets de vérification et les demandes de compte.
// Une connexion unique protégée par un verrou : le serveur HTTP consulte ce
// magasin depuis plusieurs goroutines en même temps.
type Store struct {
	db     *sql.DB
	mu     sync.Mutex
	chemin string
}

// CheminBaseDefaut retourne le chemin de `portail.db`.
func CheminBaseDefaut() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, FichierBase), nil
}

// OuvrirStore ouvre (et crée si nécessaire) le magasin SQLite. Un chemin vide
// utilise l'emplacement par défaut.
func OuvrirStore(chemin string) (*Store, error) {
	if chemin == "" {
		defaut, err := CheminBaseDefaut()
		if err != nil {
			return nil, err
		}
		chemin = defaut
	}
	if err := os.MkdirAll(filepath.Dir(chemin), 0o700); err != nil {
		return nil, fmt.Errorf("portal: création du dossier de données: %w", err)
	}
	db, err := sql.Open("sqlite", dsnSQLite(chemin))
	if err != nil {
		return nil, fmt.Errorf("portal: ouverture de %s: %w", chemin, err)
	}
	// Un seul écrivain à la fois, comme côté Python (sqlite3.connect).
	db.SetMaxOpenConns(1)

	s := &Store{db: db, chemin: chemin}
	if err := s.initialiser(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// dsnSQLite construit la chaîne de connexion du pilote modernc.org/sqlite avec
// un délai d'attente, comme le journal d'audit : cela évite « database is
// locked » quand la version Python accède au même fichier.
func dsnSQLite(chemin string) string {
	safe := strings.ReplaceAll(chemin, "?", "%3F")
	safe = strings.ReplaceAll(safe, "#", "%23")
	return "file:" + safe + "?_pragma=busy_timeout(5000)"
}

// Chemin retourne le chemin du fichier ouvert.
func (s *Store) Chemin() string { return s.chemin }

// initialiser crée le schéma s'il n'existe pas.
func (s *Store) initialiser() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.db.Exec(schemaStore); err != nil {
		return fmt.Errorf("portal: création du schéma: %w", err)
	}
	return nil
}

// Fermer ferme la connexion (idempotent).
func (s *Store) Fermer() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil
	}
	err := s.db.Close()
	s.db = nil
	return err
}

// ─── Codes ────────────────────────────────────────────────────────────────

// CreerCode émet un ticket à usage unique et retourne son identifiant.
func (s *Store) CreerCode(scope, cible string, utilisateur Utilisateur, code string,
	ttlMinutes int, maintenant time.Time) (string, error) {

	if s.db == nil {
		return "", errors.New("portal: magasin fermé")
	}
	sel, err := octetsAleatoires(16)
	if err != nil {
		return "", fmt.Errorf("portal: génération du sel: %w", err)
	}
	ticket, err := identifiantAleatoireHex(12)
	if err != nil {
		return "", fmt.Errorf("portal: génération du ticket: %w", err)
	}
	empreinte := deriverCode(code, sel)
	expireLe := horodatageISO(maintenant.Add(time.Duration(ttlMinutes) * time.Minute))

	desactive := 0
	if utilisateur.Desactive {
		desactive = 1
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(
		"INSERT INTO codes (id, scope, cible, dn, sam, cn, mail, disabled, "+
			"salt, digest, expires_at, attempts, consumed) "+
			"VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, 0)",
		ticket, scope, cible, utilisateur.DN, utilisateur.SAM, utilisateur.CN,
		utilisateur.Mail, desactive, sel, empreinte, expireLe,
	)
	if err != nil {
		return "", fmt.Errorf("portal: enregistrement du code: %w", err)
	}
	// Purge des tickets expirés : la table ne grossit pas.
	if _, err := s.db.Exec("DELETE FROM codes WHERE expires_at < ?",
		horodatageISO(maintenant.Add(-time.Hour))); err != nil {
		return "", fmt.Errorf("portal: purge des codes: %w", err)
	}
	return ticket, nil
}

// CompterCodesRecents retourne le nombre de codes émis pour `cible` pendant la
// fenêtre de limitation.
func (s *Store) CompterCodesRecents(cible string, maintenant time.Time) (int, error) {
	if s.db == nil {
		return 0, errors.New("portal: magasin fermé")
	}
	depuis := horodatageISO(maintenant.Add(-FenetreLimitationMinutes * time.Minute))

	s.mu.Lock()
	defer s.mu.Unlock()
	var nombre int
	err := s.db.QueryRow(
		"SELECT COUNT(*) FROM codes WHERE cible = ? AND expires_at >= ?",
		cible, depuis).Scan(&nombre)
	if err != nil {
		return 0, fmt.Errorf("portal: comptage des codes: %w", err)
	}
	return nombre, nil
}

// chargerCode lit un ticket (appelé sous verrou).
func (s *Store) chargerCode(ticket string) (*ligneCode, error) {
	var (
		ligne     ligneCode
		dn, sam   string
		cn, mail  string
		desactive int
		expire    string
	)
	err := s.db.QueryRow("SELECT * FROM codes WHERE id = ?", ticket).Scan(
		&ligne.id, &ligne.scope, &ligne.cible, &dn, &sam, &cn, &mail, &desactive,
		&ligne.sel, &ligne.empreinte, &expire, &ligne.tentatives, &ligne.consomme,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("portal: lecture du code: %w", err)
	}
	moment, err := analyserISO(expire)
	if err != nil {
		// Une date illisible rend le ticket inexploitable (comme Python).
		return nil, nil
	}
	ligne.expireLe = moment
	ligne.utilisateur = Utilisateur{
		DN: dn, SAM: sam, CN: cn, Mail: mail, Desactive: desactive != 0,
	}
	return &ligne, nil
}

// VerifierCode vérifie un code **sans le consommer**. Chaque tentative est
// comptée, bonne ou mauvaise ; la raison détaillée ne sert qu'au journal.
func (s *Store) VerifierCode(ticket, code string, maxTentatives int,
	moment time.Time) (bool, string, *Utilisateur, error) {

	if s.db == nil {
		return false, "", nil, errors.New("portal: magasin fermé")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	ligne, err := s.chargerCode(ticket)
	if err != nil {
		return false, "", nil, err
	}
	if ligne == nil {
		return false, raisonInconnu, nil, nil
	}
	if ligne.consomme {
		return false, raisonConsomme, nil, nil
	}
	if !ligne.expireLe.After(moment) {
		return false, raisonExpire, nil, nil
	}
	if ligne.tentatives >= maxTentatives {
		return false, raisonTropDeTentatives, nil, nil
	}
	if _, err := s.db.Exec("UPDATE codes SET attempts = attempts + 1 WHERE id = ?",
		ticket); err != nil {
		return false, "", nil, fmt.Errorf("portal: comptage des tentatives: %w", err)
	}

	attendu := deriverCode(code, ligne.sel)
	if !empreinteEgale(attendu, ligne.empreinte) {
		return false, raisonCodeIncorrect, nil, nil
	}
	utilisateur := ligne.utilisateur
	return true, raisonOK, &utilisateur, nil
}

// ConsommerCode marque le ticket comme utilisé (appelé après le succès réel).
func (s *Store) ConsommerCode(ticket string) (bool, error) {
	if s.db == nil {
		return false, errors.New("portal: magasin fermé")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	resultat, err := s.db.Exec(
		"UPDATE codes SET consumed = 1 WHERE id = ? AND consumed = 0", ticket)
	if err != nil {
		return false, fmt.Errorf("portal: consommation du code: %w", err)
	}
	n, err := resultat.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("portal: consommation du code: %w", err)
	}
	return n == 1, nil
}

// ─── Demandes ─────────────────────────────────────────────────────────────

// CreerDemande enregistre une nouvelle demande (statut « en attente »).
func (s *Store) CreerDemande(profil, prenom, nom, mail, classe, motif string,
	moment time.Time) (*Demande, error) {

	if s.db == nil {
		return nil, errors.New("portal: magasin fermé")
	}
	id, err := identifiantAleatoireHex(8)
	if err != nil {
		return nil, fmt.Errorf("portal: génération de l'identifiant de demande: %w", err)
	}
	demande := Demande{
		ID:        id,
		Profil:    profil,
		Prenom:    strings.TrimSpace(prenom),
		Nom:       strings.TrimSpace(nom),
		Mail:      strings.TrimSpace(mail),
		Classe:    strings.TrimSpace(classe),
		Motif:     strings.TrimSpace(motif),
		Statut:    StatutEnAttente,
		CreatedAt: horodatageISO(moment),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	_, err = s.db.Exec(
		"INSERT INTO demandes (id, profil, prenom, nom, mail, classe, motif, "+
			"statut, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
		demande.ID, demande.Profil, demande.Prenom, demande.Nom, demande.Mail,
		demande.Classe, demande.Motif, demande.Statut, demande.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("portal: enregistrement de la demande: %w", err)
	}
	return &demande, nil
}

// scannerDemande lit une ligne de `demandes`.
func scannerDemande(scanner interface{ Scan(...any) error }) (*Demande, error) {
	var d Demande
	err := scanner.Scan(&d.ID, &d.Profil, &d.Prenom, &d.Nom, &d.Mail, &d.Classe,
		&d.Motif, &d.Statut, &d.CreatedAt, &d.TraiteLe, &d.TraitePar, &d.Note)
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DemandeParID retourne une demande (nil si elle n'existe pas).
func (s *Store) DemandeParID(id string) (*Demande, error) {
	if s.db == nil {
		return nil, errors.New("portal: magasin fermé")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	demande, err := scannerDemande(s.db.QueryRow(
		"SELECT * FROM demandes WHERE id = ?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("portal: lecture de la demande: %w", err)
	}
	return demande, nil
}

// ListerDemandes retourne les demandes d'un statut donné, de la plus récente à
// la plus ancienne. Un statut vide liste toutes les demandes.
func (s *Store) ListerDemandes(statut string) ([]Demande, error) {
	if s.db == nil {
		return nil, errors.New("portal: magasin fermé")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var (
		rows *sql.Rows
		err  error
	)
	if statut != "" {
		rows, err = s.db.Query(
			"SELECT * FROM demandes WHERE statut = ? ORDER BY created_at DESC", statut)
	} else {
		rows, err = s.db.Query("SELECT * FROM demandes ORDER BY created_at DESC")
	}
	if err != nil {
		return nil, fmt.Errorf("portal: lecture des demandes: %w", err)
	}
	defer rows.Close()

	var demandes []Demande
	for rows.Next() {
		demande, err := scannerDemande(rows)
		if err != nil {
			return nil, fmt.Errorf("portal: lecture d'une demande: %w", err)
		}
		demandes = append(demandes, *demande)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("portal: parcours des demandes: %w", err)
	}
	return demandes, nil
}

// DefinirStatutDemande met à jour le statut d'une demande et retourne la ligne
// relue (nil si la demande n'existe pas).
func (s *Store) DefinirStatutDemande(id, statut, traitePar, note string,
	moment time.Time) (*Demande, error) {

	if s.db == nil {
		return nil, errors.New("portal: magasin fermé")
	}
	s.mu.Lock()
	if _, err := s.db.Exec(
		"UPDATE demandes SET statut = ?, traite_le = ?, traite_par = ?, note = ? "+
			"WHERE id = ?",
		statut, horodatageISO(moment), traitePar, note, id); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("portal: mise à jour de la demande: %w", err)
	}
	s.mu.Unlock()
	return s.DemandeParID(id)
}
