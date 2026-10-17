// Package audit journalise les actions EduSync-AD dans SQLite.
//
// Le schéma est **identique** à celui de la version Python
// (src/edusync_ad/core/audit.py) : un fichier journal.db créé par Python se
// rouvre en Go, et inversement, sans migration.
//
// Journal « append-only » : ce paquet n'expose volontairement aucune
// méthode de suppression ni de mise à jour. Toute effacement passe par un
// export puis une purge manuelle documentée (exigence de traçabilité,
// ROADMAP_GO_MIGRATION.md §12).
package audit

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // pilote pur Go, sans CGO

	"github.com/estemobs/EduSync-AD/internal/config"
	"github.com/estemobs/EduSync-AD/internal/models"
)

// Entry est une ligne du journal, identifiant SQLite compris.
// L'accès direct aux champs de ActionLogEntry est rendu possible par
// l'intégration (anonymous embedding) : le JSON reste donc à plat.
type Entry struct {
	ID int64 `json:"id"`
	models.ActionLogEntry
}

// Filter décrit les critères de lecture du journal.
// Les bornes From / To sont des chaînes ISO 8601 comparées
// lexicographiquement (valable pour des horodatages normalisés).
type Filter struct {
	From       string
	To         string
	ActionType string
	Resultat   string
	Domaine    string
	Operateur  string
	SessionID  string
	Simulation *bool
	Recherche  string
	Limit      int
	Offset     int
}

// Log est une connexion ouverte au journal d'actions.
type Log struct {
	db            *sql.DB
	sessionID     string
	currentUser   string
	currentDomain string
}

// NewSessionID génère un identifiant de session court (12 caractères
// hexadécimaux), équivalent de uuid4().hex[:12] côté Python.
func NewSessionID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "000000000000"
	}
	return hex.EncodeToString(b[:])
}

// DefaultPath retourne le chemin du journal d'actions par défaut
// (~/.local/share/EduSyncAD/journal.db sous Linux, équivalent macOS/Windows).
func DefaultPath() (string, error) {
	dir, err := config.DataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "journal.db"), nil
}

// Open ouvre (et crée si nécessaire) le journal situé à « path ».
// Si path est vide, le chemin par défaut est utilisé.
func Open(path string) (*Log, error) {
	if path == "" {
		var err error
		if path, err = DefaultPath(); err != nil {
			return nil, err
		}
	}

	db, err := sql.Open("sqlite", dsn(path))
	if err != nil {
		return nil, fmt.Errorf("audit: ouverture de %s: %w", path, err)
	}
	// SQLite n'autorise pas un pool d'écritures concurrentes : une seule
	// connexion, comme côté Python (sqlite3.connect).
	db.SetMaxOpenConns(1)

	l := &Log{db: db, sessionID: NewSessionID()}
	if err := l.ensureSchema(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return l, nil
}

// SessionID retourne l'identifiant de session courant.
func (l *Log) SessionID() string { return l.sessionID }

// SetContext renseigne l'opérateur et le domaine courants : ils sont
// renseignés automatiquement dans chaque entrée dont les champs sont vides
// (mêmesiliaisons que current_user / current_domain côté Python).
func (l *Log) SetContext(utilisateur, domaine string) {
	l.currentUser = utilisateur
	l.currentDomain = domaine
}

// Record ajoute une entrée au journal.
func (l *Log) Record(e models.ActionLogEntry) error {
	if e.SessionID == "" {
		e.SessionID = l.sessionID
	}
	if e.Utilisateur == "" {
		e.Utilisateur = l.currentUser
	}
	if e.Domaine == "" {
		e.Domaine = l.currentDomain
	}
	if e.Timestamp == "" {
		return fmt.Errorf("audit: horodatage manquant pour l'action %q", e.ActionType)
	}

	_, err := l.db.Exec(`
		INSERT INTO actions
			(timestamp, action_type, compte, ou_source, ou_destination,
			 resultat, session_id, simulation, detail, utilisateur, domaine)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		e.Timestamp, e.ActionType, e.Compte, e.OUSource, e.OUDestination,
		e.Resultat, e.SessionID, boolToInt(e.Simulation), e.Detail, e.Utilisateur, e.Domaine,
	)
	if err != nil {
		return fmt.Errorf("audit: écriture de l'action %q: %w", e.ActionType, err)
	}
	return nil
}

// List retourne les entrées correspondant au filtre, de la plus récente à
// la plus ancienne.
func (l *Log) List(f Filter) ([]Entry, error) {
	query := `
		SELECT id, timestamp, action_type, compte, ou_source, ou_destination,
		       resultat, session_id, simulation, detail, utilisateur, domaine
		FROM actions`
	var (
		conds []string
		args  []any
	)
	add := func(cond string, val any) {
		conds = append(conds, cond)
		args = append(args, val)
	}

	if f.From != "" {
		add("timestamp >= ?", f.From)
	}
	if f.To != "" {
		add("timestamp <= ?", f.To)
	}
	if f.ActionType != "" {
		add("action_type = ?", f.ActionType)
	}
	if f.Resultat != "" {
		add("resultat = ?", f.Resultat)
	}
	if f.Domaine != "" {
		add("domaine = ?", f.Domaine)
	}
	if f.Operateur != "" {
		add("utilisateur = ?", f.Operateur)
	}
	if f.SessionID != "" {
		add("session_id = ?", f.SessionID)
	}
	if f.Simulation != nil {
		add("simulation = ?", boolToInt(*f.Simulation))
	}
	if f.Recherche != "" {
		like := "%" + f.Recherche + "%"
		conds = append(conds, "(compte LIKE ? OR detail LIKE ?)")
		args = append(args, like, like)
	}

	if len(conds) > 0 {
		query += " WHERE " + strings.Join(conds, " AND ")
	}
	query += " ORDER BY id DESC"

	limit := f.Limit
	if limit <= 0 || limit > 10000 {
		limit = 1000
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	query += fmt.Sprintf(" LIMIT %d OFFSET %d", limit, offset)

	rows, err := l.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("audit: lecture du journal: %w", err)
	}
	defer rows.Close()

	var out []Entry
	for rows.Next() {
		var (
			e           Entry
			simulation  int
			detail      sql.NullString
			ouSource    sql.NullString
			ouDest      sql.NullString
			utilisateur sql.NullString
			domaine     sql.NullString
		)
		if err := rows.Scan(
			&e.ID, &e.Timestamp, &e.ActionType, &e.Compte, &ouSource, &ouDest,
			&e.Resultat, &e.SessionID, &simulation, &detail, &utilisateur, &domaine,
		); err != nil {
			return nil, fmt.Errorf("audit: lecture d'une entrée: %w", err)
		}
		e.OUSource = ouSource.String
		e.OUDestination = ouDest.String
		e.Detail = detail.String
		e.Utilisateur = utilisateur.String
		e.Domaine = domaine.String
		e.Simulation = simulation != 0
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("audit: itération du journal: %w", err)
	}
	return out, nil
}

// Count retourne le nombre total d'entrées.
func (l *Log) Count() (int, error) {
	var n int
	if err := l.db.QueryRow(`SELECT COUNT(*) FROM actions`).Scan(&n); err != nil {
		return 0, fmt.Errorf("audit: comptage: %w", err)
	}
	return n, nil
}

// Domains retourne la liste triée des domaines présents dans le journal
// (M25 : sélection multi-site). Les entrées sans domaine sont ignorées.
func (l *Log) Domains() ([]string, error) {
	return l.distinct("domaine")
}

// Operators retourne la liste triée des opérateurs présents dans le journal
// (M26 : journal par opérateur).
func (l *Log) Operators() ([]string, error) {
	return l.distinct("utilisateur")
}

// distinct retourne les valeurs distinctes non vides d'une colonne, triées.
func (l *Log) distinct(colonne string) ([]string, error) {
	// La colonne vient d'un littéral interne : aucune injection possible.
	lignes, err := l.db.Query(`SELECT DISTINCT ` + colonne + ` FROM actions
		WHERE ` + colonne + ` IS NOT NULL AND ` + colonne + ` != '' ORDER BY ` + colonne)
	if err != nil {
		return nil, fmt.Errorf("audit: lecture de %s: %w", colonne, err)
	}
	defer func() { _ = lignes.Close() }()
	var valeurs []string
	for lignes.Next() {
		var valeur string
		if err := lignes.Scan(&valeur); err != nil {
			return nil, fmt.Errorf("audit: lecture d'une valeur de %s: %w", colonne, err)
		}
		valeurs = append(valeurs, valeur)
	}
	return valeurs, lignes.Err()
}

// Close ferme la base.
func (l *Log) Close() error {
	if l.db == nil {
		return nil
	}
	return l.db.Close()
}

// PendingDeletion décrit une suppression planifiée (archivage de départ).
type PendingDeletion struct {
	ID             int64
	UserDN         string
	SAMAccountName string
	NomComplet     string
	MovedAt        string
	SessionID      string
	DelaiJours     int
}

// PurgeAvant retourne la date butoir (ISO 8601) au-delà de laquelle les
// suppressions planifiées sont purgables.
func PurgeAvant(delaiJours int) string {
	return time.Now().UTC().AddDate(0, 0, -delaiJours).Format(time.RFC3339)
}

// RecordPendingDeletion enregistre une suppression planifiée.
func (l *Log) RecordPendingDeletion(userDN, samAccountName, nomComplet, sessionID string, delaiJours int) error {
	_, err := l.db.Exec(`
		INSERT INTO pending_deletions
			(user_dn, sam_account_name, nom_complet, moved_at, session_id, delai_jours)
		VALUES (?, ?, ?, ?, ?, ?)`,
		userDN, samAccountName, nomComplet, time.Now().UTC().Format(time.RFC3339), sessionID, delaiJours,
	)
	if err != nil {
		return fmt.Errorf("audit: enregistrement suppression planifiée %s: %w", samAccountName, err)
	}
	return nil
}

// ListPendingDeletions retourne toutes les suppressions planifiées en attente.
func (l *Log) ListPendingDeletions() ([]PendingDeletion, error) {
	rows, err := l.db.Query(`
		SELECT id, user_dn, sam_account_name, nom_complet, moved_at, session_id, delai_jours
		FROM pending_deletions
		ORDER BY moved_at`)
	if err != nil {
		return nil, fmt.Errorf("audit: lecture suppressions planifiées: %w", err)
	}
	defer rows.Close()

	var out []PendingDeletion
	for rows.Next() {
		var p PendingDeletion
		if err := rows.Scan(&p.ID, &p.UserDN, &p.SAMAccountName, &p.NomComplet, &p.MovedAt, &p.SessionID, &p.DelaiJours); err != nil {
			return nil, fmt.Errorf("audit: lecture suppression planifiée: %w", err)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PurgeExpiredDeletions supprime les suppressions planifiées dont le délai est expiré.
// Retourne le nombre de lignes purgées.
func (l *Log) PurgeExpiredDeletions(delaiJours int) (int64, error) {
	butoir := PurgeAvant(delaiJours)
	res, err := l.db.Exec(`
		DELETE FROM pending_deletions
		WHERE moved_at <= ?`, butoir)
	if err != nil {
		return 0, fmt.Errorf("audit: purge suppressions expirées: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// ensureSchema crée les tables si besoin et applique les mêmes ALTER TABLE
// d'évolution que la version Python (colonnes utilisateur, domaine,
// delai_jours ajoutées après-coup dans le schéma d'origine).
func (l *Log) ensureSchema() error {
	if _, err := l.db.Exec(`
		CREATE TABLE IF NOT EXISTS actions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			timestamp TEXT NOT NULL,
			action_type TEXT NOT NULL,
			compte TEXT NOT NULL,
			ou_source TEXT,
			ou_destination TEXT,
			resultat TEXT NOT NULL,
			session_id TEXT NOT NULL,
			simulation INTEGER NOT NULL,
			detail TEXT
		)`); err != nil {
		return fmt.Errorf("audit: création de la table actions: %w", err)
	}

	existing, err := l.columns("actions")
	if err != nil {
		return err
	}
	for _, col := range []string{"utilisateur", "domaine"} {
		if !existing[col] {
			if _, err := l.db.Exec("ALTER TABLE actions ADD COLUMN " + col + " TEXT"); err != nil {
				return fmt.Errorf("audit: ajout de la colonne %s: %w", col, err)
			}
		}
	}

	if _, err := l.db.Exec(`
		CREATE TABLE IF NOT EXISTS pending_deletions (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			user_dn TEXT NOT NULL UNIQUE,
			sam_account_name TEXT NOT NULL,
			nom_complet TEXT,
			moved_at TEXT NOT NULL,
			session_id TEXT NOT NULL
		)`); err != nil {
		return fmt.Errorf("audit: création de la table pending_deletions: %w", err)
	}

	pending, err := l.columns("pending_deletions")
	if err != nil {
		return err
	}
	if !pending["delai_jours"] {
		if _, err := l.db.Exec(`ALTER TABLE pending_deletions ADD COLUMN delai_jours INTEGER`); err != nil {
			return fmt.Errorf("audit: ajout de la colonne delai_jours: %w", err)
		}
	}
	return nil
}

func (l *Log) columns(table string) (map[string]bool, error) {
	rows, err := l.db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return nil, fmt.Errorf("audit: lecture du schéma de %s: %w", table, err)
	}
	defer rows.Close()

	out := map[string]bool{}
	for rows.Next() {
		var (
			cid        int
			name       string
			typeName   string
			notNull    int
			defaultVal sql.NullString
			primaryKey int
		)
		// colonnes : cid, name, type, notnull, dflt_value, pk
		if err := rows.Scan(&cid, &name, &typeName, &notNull, &defaultVal, &primaryKey); err != nil {
			return nil, fmt.Errorf("audit: lecture du schéma: %w", err)
		}
		out[name] = true
	}
	return out, rows.Err()
}

// dsn construit la chaîne de connexion du pilote modernc.org/sqlite.
func dsn(path string) string {
	// Sépare les caractères qui perturberaient l'analyse de l'URI.
	safe := strings.ReplaceAll(path, "?", "%3F")
	safe = strings.ReplaceAll(safe, "#", "%23")
	// Le timeout d'attente évite « database is locked » lors d'accès croisés
	// avec l'application Python encore ouverte.
	return "file:" + safe + "?_pragma=busy_timeout(5000)"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
