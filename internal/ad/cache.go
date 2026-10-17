// Package ad : cache AD local SQLite (T2).
//
// Le cache reprend le schéma **identique** à celui de la version Python
// (src/edusync_ad/core/ad/cache.py) : un fichier `ad_cache.sqlite` créé par
// Python se rouvre en Go, et inversement, sans migration. Il permet un
// explorateur instantané sur les gros domaines (>5k objets).
//
// Synchronisation complète (OUs → groupes → utilisateurs) ou incrémentale via
// `uSNChanged` depuis le dernier USN enregistré. Le TTL (par défaut 5 min)
// sert de repli quand `uSNChanged` n'est pas exploitable. Les suppressions ne
// sont pas détectées par la synchronisation incrémentale : elle repose sur la
// synchronisation complète (limite documentée, comme côté Python).
package ad

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-ldap/ldap/v3"
	_ "modernc.org/sqlite" // pilote pur Go, sans CGO

	"github.com/estemobs/EduSync-AD/internal/config"
)

const (
	// NomFichierCache est le nom du fichier SQLite du cache AD.
	NomFichierCache = "ad_cache.sqlite"
	// CacheTTLDefaut est la durée de vie par défaut du cache (5 min).
	CacheTTLDefaut = 5 * time.Minute
	// CacheIntervallePollDefaut est la cadence de vérification uSNChanged.
	CacheIntervallePollDefaut = 30 * time.Second
)

// CacheStats résume l'état du cache. Les noms JSON correspondent aux champs du
// dataclass Python CacheStats (parité des sorties).
type CacheStats struct {
	OUsCount       int     `json:"ous_count"`
	GroupsCount    int     `json:"groups_count"`
	UsersCount     int     `json:"users_count"`
	LastSync       float64 `json:"last_sync"`
	LastUSNChanged int64   `json:"last_usn_changed"`
	SyncDurationMs int     `json:"sync_duration_ms"`
}

// CacheOU représente une unité d'organisation mise en cache.
type CacheOU struct {
	DN         string `json:"dn"`
	Name       string `json:"name"`
	ParentDN   string `json:"parent_dn,omitempty"`
	Depth      int    `json:"depth"`
	USNChanged int64  `json:"usn_changed,omitempty"`
}

// CacheGroup représente un groupe mis en cache.
type CacheGroup struct {
	DN          string `json:"dn"`
	CN          string `json:"cn"`
	OUDN        string `json:"ou_dn,omitempty"`
	GroupType   int    `json:"group_type,omitempty"`
	MemberCount int    `json:"member_count"`
	USNChanged  int64  `json:"usn_changed,omitempty"`
}

// CacheUser représente un utilisateur mis en cache.
type CacheUser struct {
	DN          string `json:"dn"`
	SAM         string `json:"sam"`
	CN          string `json:"cn"`
	GivenName   string `json:"given_name,omitempty"`
	SN          string `json:"sn,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Mail        string `json:"mail,omitempty"`
	OUDN        string `json:"ou_dn,omitempty"`
	UAC         int    `json:"user_account_control"`
	USNChanged  int64  `json:"usn_changed,omitempty"`
	Desactive   bool   `json:"disabled"`
}

// EnfantOU décrit un enfant direct d'une OU (sous-OU, groupe ou utilisateur).
type EnfantOU struct {
	DN        string `json:"dn"`
	CN        string `json:"cn"`
	Kind      string `json:"kind"` // "ou" | "group" | "user"
	SAM       string `json:"sam,omitempty"`
	Desactive bool   `json:"disabled,omitempty"`
}

// ADCache est un cache local SQLite des objets AD (OUs, groupes, utilisateurs).
type ADCache struct {
	mu     sync.Mutex
	db     *sql.DB
	chemin string
	ttl    time.Duration
	stats  CacheStats
}

// CheminCacheParDefaut retourne le chemin du cache par défaut, partagé avec
// Python (~/.config/EduSyncAD/ad_cache.sqlite sous Linux).
func CheminCacheParDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichierCache), nil
}

// NewADCache ouvre (et crée) le cache situé à « chemin ». Un chemin vide
// utilise le chemin par défaut. Un TTL non positif utilise CacheTTLDefaut.
func NewADCache(chemin string, ttl time.Duration) (*ADCache, error) {
	if chemin == "" {
		var err error
		if chemin, err = CheminCacheParDefaut(); err != nil {
			return nil, err
		}
	}
	if ttl <= 0 {
		ttl = CacheTTLDefaut
	}

	db, err := sql.Open("sqlite", dsnCache(chemin))
	if err != nil {
		return nil, fmt.Errorf("ad: ouverture du cache %s: %w", chemin, err)
	}
	// SQLite n'autorise pas un pool d'écritures concurrentes : une connexion.
	db.SetMaxOpenConns(1)

	c := &ADCache{db: db, chemin: chemin, ttl: ttl}
	if err := c.initialiser(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return c, nil
}

// initialiser applique les pragmas, crée le schéma et recharge l'état.
func (c *ADCache) initialiser() error {
	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA cache_size=-32768", // ~32 Mo
	} {
		if _, err := c.db.Exec(pragma); err != nil {
			return fmt.Errorf("ad: pragma %q: %w", pragma, err)
		}
	}
	if err := c.creerSchema(); err != nil {
		return err
	}
	return c.chargerEtat()
}

// creerSchema crée les tables et index (schéma identique à Python).
func (c *ADCache) creerSchema() error {
	instructions := []string{
		`CREATE TABLE IF NOT EXISTS ous (
			dn TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			parent_dn TEXT,
			depth INTEGER DEFAULT 0,
			usn_changed INTEGER DEFAULT 0,
			updated_at REAL DEFAULT (strftime('%s', 'now'))
		)`,
		`CREATE TABLE IF NOT EXISTS groups (
			dn TEXT PRIMARY KEY,
			cn TEXT NOT NULL,
			ou_dn TEXT,
			group_type INTEGER,
			member_count INTEGER DEFAULT 0,
			usn_changed INTEGER DEFAULT 0,
			updated_at REAL DEFAULT (strftime('%s', 'now')),
			FOREIGN KEY (ou_dn) REFERENCES ous(dn)
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			dn TEXT PRIMARY KEY,
			sam TEXT NOT NULL,
			cn TEXT NOT NULL,
			given_name TEXT,
			sn TEXT,
			display_name TEXT,
			mail TEXT,
			ou_dn TEXT,
			user_account_control INTEGER DEFAULT 512,
			enabled INTEGER GENERATED ALWAYS AS ((user_account_control & 2) = 0) STORED,
			usn_changed INTEGER DEFAULT 0,
			updated_at REAL DEFAULT (strftime('%s', 'now')),
			FOREIGN KEY (ou_dn) REFERENCES ous(dn)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ous_parent ON ous(parent_dn)`,
		`CREATE INDEX IF NOT EXISTS idx_groups_ou ON groups(ou_dn)`,
		`CREATE INDEX IF NOT EXISTS idx_users_ou ON users(ou_dn)`,
		`CREATE INDEX IF NOT EXISTS idx_users_sam ON users(sam)`,
		`CREATE INDEX IF NOT EXISTS idx_users_cn ON users(cn)`,
		`CREATE TABLE IF NOT EXISTS meta (
			key TEXT PRIMARY KEY,
			value TEXT
		)`,
	}
	for _, instruction := range instructions {
		if _, err := c.db.Exec(instruction); err != nil {
			return fmt.Errorf("ad: création du schéma cache: %w", err)
		}
	}
	return nil
}

// chargerEtat relit les compteurs et les métadonnées depuis la base.
func (c *ADCache) chargerEtat() error {
	c.stats.OUsCount = c.compter("ous")
	c.stats.GroupsCount = c.compter("groups")
	c.stats.UsersCount = c.compter("users")

	// La base fait autorité : réinitialisation avant relecture.
	c.stats.LastSync = 0
	c.stats.LastUSNChanged = 0

	if v, ok := c.lireMeta("last_sync"); ok {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			c.stats.LastSync = f
		}
	}
	if v, ok := c.lireMeta("last_usn_changed"); ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			c.stats.LastUSNChanged = n
		}
	}
	return nil
}

// compter retourne le nombre de lignes d'une table connue.
func (c *ADCache) compter(table string) int {
	var n int
	// Table contrôlée par l'appelant (constantes internes uniquement).
	if err := c.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		return 0
	}
	return n
}

// lireMeta lit une métadonnée.
func (c *ADCache) lireMeta(cle string) (string, bool) {
	var valeur string
	err := c.db.QueryRow("SELECT value FROM meta WHERE key = ?", cle).Scan(&valeur)
	if err != nil {
		return "", false
	}
	return valeur, true
}

// sauverMeta écrit (ou remplace) une métadonnée.
func (c *ADCache) sauverMeta(cle, valeur string) error {
	_, err := c.db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", cle, valeur)
	return err
}

// Chemin retourne le chemin du fichier de cache.
func (c *ADCache) Chemin() string { return c.chemin }

// TTL retourne la durée de vie configurée.
func (c *ADCache) TTL() time.Duration { return c.ttl }

// Stats retourne une copie des statistiques courantes.
func (c *ADCache) Stats() CacheStats {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stats
}

// IsStale indique si le cache est périmé (jamais synchronisé ou TTL dépassé).
func (c *ADCache) IsStale() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stats.LastSync <= 0 {
		return true
	}
	return time.Since(time.Unix(0, int64(c.stats.LastSync*float64(time.Second)))) > c.ttl
}

// Close ferme la connexion SQLite.
func (c *ADCache) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.db == nil {
		return nil
	}
	err := c.db.Close()
	c.db = nil
	return err
}

// Sync synchronise le cache depuis l'annuaire. « full » force une
// synchronisation complète ; sinon une synchronisation incrémentale par
// uSNChanged est tentée (repli automatique sur le complet au premier appel).
func (c *ADCache) Sync(ctx context.Context, p *Pool, full bool) (CacheStats, error) {
	debut := time.Now()
	base := p.BaseDN()

	depuis := c.stats.LastUSNChanged
	if full || depuis <= 0 {
		if err := c.syncComplet(ctx, p, base); err != nil {
			return c.stats, err
		}
	} else {
		if err := c.syncIncremental(ctx, p, base, depuis); err != nil {
			return c.stats, err
		}
	}

	c.stats.LastSync = float64(time.Now().UnixNano()) / float64(time.Second)
	c.stats.SyncDurationMs = int(time.Since(debut).Milliseconds())
	if err := c.sauverMeta("last_sync", strconv.FormatFloat(c.stats.LastSync, 'f', -1, 64)); err != nil {
		return c.stats, fmt.Errorf("ad: écriture last_sync: %w", err)
	}
	if err := c.sauverMeta("last_usn_changed", strconv.FormatInt(c.stats.LastUSNChanged, 10)); err != nil {
		return c.stats, fmt.Errorf("ad: écriture last_usn_changed: %w", err)
	}
	return c.stats, nil
}

// syncComplet remplace intégralement le contenu du cache.
func (c *ADCache) syncComplet(ctx context.Context, p *Pool, base string) error {
	ous, err := p.listerOUsAD(ctx, base, 0)
	if err != nil {
		return err
	}
	groupes, err := p.listerGroupesAD(ctx, base, 0)
	if err != nil {
		return err
	}
	utilisateurs, err := p.listerUtilisateursAD(ctx, base, 0)
	if err != nil {
		return err
	}

	if err := c.remplacerOUs(ous); err != nil {
		return err
	}
	if err := c.remplacerGroupes(groupes); err != nil {
		return err
	}
	if err := c.remplacerUtilisateurs(utilisateurs); err != nil {
		return err
	}

	c.stats.OUsCount = len(ous)
	c.stats.GroupsCount = len(groupes)
	c.stats.UsersCount = len(utilisateurs)
	c.stats.LastUSNChanged = maxUSNCache(ous, groupes, utilisateurs)
	return nil
}

// syncIncremental applique les objets modifiés depuis le dernier USN connu.
func (c *ADCache) syncIncremental(ctx context.Context, p *Pool, base string, depuis int64) error {
	ous, err := p.listerOUsAD(ctx, base, depuis)
	if err != nil {
		return err
	}
	groupes, err := p.listerGroupesAD(ctx, base, depuis)
	if err != nil {
		return err
	}
	utilisateurs, err := p.listerUtilisateursAD(ctx, base, depuis)
	if err != nil {
		return err
	}

	if err := c.upsertOUs(ous); err != nil {
		return err
	}
	if err := c.upsertGroupes(groupes); err != nil {
		return err
	}
	if err := c.upsertUtilisateurs(utilisateurs); err != nil {
		return err
	}

	if max := maxUSNCache(ous, groupes, utilisateurs); max > c.stats.LastUSNChanged {
		c.stats.LastUSNChanged = max
	}
	c.stats.OUsCount = c.compter("ous")
	c.stats.GroupsCount = c.compter("groups")
	c.stats.UsersCount = c.compter("users")
	return nil
}

// maxUSNCache retourne le plus grand uSNChanged parmi tous les objets fournis.
func maxUSNCache(ous []CacheOU, groupes []CacheGroup, utilisateurs []CacheUser) int64 {
	var max int64
	for _, o := range ous {
		if o.USNChanged > max {
			max = o.USNChanged
		}
	}
	for _, g := range groupes {
		if g.USNChanged > max {
			max = g.USNChanged
		}
	}
	for _, u := range utilisateurs {
		if u.USNChanged > max {
			max = u.USNChanged
		}
	}
	return max
}

// dsnCache construit la chaîne de connexion du pilote modernc.org/sqlite.
// Les clés étrangères ne sont pas activées : comme Python, le schéma les
// déclare mais l'insertion reste tolérante (ordre de synchronisation libre).
func dsnCache(chemin string) string {
	safe := strings.ReplaceAll(chemin, "?", "%3F")
	safe = strings.ReplaceAll(safe, "#", "%23")
	return "file:" + safe + "?_pragma=busy_timeout(5000)"
}

// ─── Écriture ─────────────────────────────────────────────────────────────

// remplacerOUs vide la table puis insère les OUs fournies.
func (c *ADCache) remplacerOUs(ous []CacheOU) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("ad: transaction OUs: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM ous"); err != nil {
		return fmt.Errorf("ad: purge OUs: %w", err)
	}
	for _, o := range ous {
		if _, err := tx.Exec(`INSERT INTO ous (dn, name, parent_dn, depth, usn_changed) VALUES (?, ?, ?, ?, ?)`,
			o.DN, o.Name, o.ParentDN, o.Depth, o.USNChanged); err != nil {
			return fmt.Errorf("ad: insertion OU %s: %w", o.DN, err)
		}
	}
	return tx.Commit()
}

// remplacerGroupes vide la table puis insère les groupes fournis.
func (c *ADCache) remplacerGroupes(groupes []CacheGroup) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("ad: transaction groupes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM groups"); err != nil {
		return fmt.Errorf("ad: purge groupes: %w", err)
	}
	for _, g := range groupes {
		if _, err := tx.Exec(`INSERT INTO groups (dn, cn, ou_dn, group_type, member_count, usn_changed) VALUES (?, ?, ?, ?, ?, ?)`,
			g.DN, g.CN, g.OUDN, g.GroupType, g.MemberCount, g.USNChanged); err != nil {
			return fmt.Errorf("ad: insertion groupe %s: %w", g.DN, err)
		}
	}
	return tx.Commit()
}

// remplacerUtilisateurs vide la table puis insère les utilisateurs fournis.
func (c *ADCache) remplacerUtilisateurs(utilisateurs []CacheUser) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("ad: transaction utilisateurs: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec("DELETE FROM users"); err != nil {
		return fmt.Errorf("ad: purge utilisateurs: %w", err)
	}
	for _, u := range utilisateurs {
		if _, err := tx.Exec(`INSERT INTO users (dn, sam, cn, given_name, sn, display_name, mail, ou_dn, user_account_control, usn_changed)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			u.DN, u.SAM, u.CN, u.GivenName, u.SN, u.DisplayName, u.Mail, u.OUDN, u.UAC, u.USNChanged); err != nil {
			return fmt.Errorf("ad: insertion utilisateur %s: %w", u.DN, err)
		}
	}
	return tx.Commit()
}

// upsertOUs insère ou met à jour les OUs fournies.
func (c *ADCache) upsertOUs(ous []CacheOU) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("ad: transaction OUs: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, o := range ous {
		if _, err := tx.Exec(`INSERT INTO ous (dn, name, parent_dn, depth, usn_changed) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT(dn) DO UPDATE SET name=excluded.name, parent_dn=excluded.parent_dn, depth=excluded.depth, usn_changed=excluded.usn_changed`,
			o.DN, o.Name, o.ParentDN, o.Depth, o.USNChanged); err != nil {
			return fmt.Errorf("ad: mise à jour OU %s: %w", o.DN, err)
		}
	}
	return tx.Commit()
}

// upsertGroupes insère ou met à jour les groupes fournis.
func (c *ADCache) upsertGroupes(groupes []CacheGroup) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("ad: transaction groupes: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, g := range groupes {
		if _, err := tx.Exec(`INSERT INTO groups (dn, cn, ou_dn, group_type, member_count, usn_changed) VALUES (?, ?, ?, ?, ?, ?)
			ON CONFLICT(dn) DO UPDATE SET cn=excluded.cn, ou_dn=excluded.ou_dn, group_type=excluded.group_type, member_count=excluded.member_count, usn_changed=excluded.usn_changed`,
			g.DN, g.CN, g.OUDN, g.GroupType, g.MemberCount, g.USNChanged); err != nil {
			return fmt.Errorf("ad: mise à jour groupe %s: %w", g.DN, err)
		}
	}
	return tx.Commit()
}

// upsertUtilisateurs insère ou met à jour les utilisateurs fournis.
func (c *ADCache) upsertUtilisateurs(utilisateurs []CacheUser) error {
	tx, err := c.db.Begin()
	if err != nil {
		return fmt.Errorf("ad: transaction utilisateurs: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	for _, u := range utilisateurs {
		if _, err := tx.Exec(`INSERT INTO users (dn, sam, cn, given_name, sn, display_name, mail, ou_dn, user_account_control, usn_changed)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(dn) DO UPDATE SET sam=excluded.sam, cn=excluded.cn, given_name=excluded.given_name, sn=excluded.sn,
				display_name=excluded.display_name, mail=excluded.mail, ou_dn=excluded.ou_dn,
				user_account_control=excluded.user_account_control, usn_changed=excluded.usn_changed`,
			u.DN, u.SAM, u.CN, u.GivenName, u.SN, u.DisplayName, u.Mail, u.OUDN, u.UAC, u.USNChanged); err != nil {
			return fmt.Errorf("ad: mise à jour utilisateur %s: %w", u.DN, err)
		}
	}
	return tx.Commit()
}

// ─── Lecture ──────────────────────────────────────────────────────────────

// colonnesUtilisateur est la liste ordonnée des colonnes lues pour un
// utilisateur (les colonnes nullables sont ramenées à la chaîne vide, comme le
// None de la version Python).
const colonnesUtilisateur = `dn, sam, cn, COALESCE(given_name,''), COALESCE(sn,''), COALESCE(display_name,''), COALESCE(mail,''), COALESCE(ou_dn,''), user_account_control, usn_changed`

// GetOUs retourne les OUs (filtrées par parent si fourni).
func (c *ADCache) GetOUs(parentDN string) ([]CacheOU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var rows *sql.Rows
	var err error
	if parentDN != "" {
		rows, err = c.db.Query("SELECT dn, name, COALESCE(parent_dn,''), depth, usn_changed FROM ous WHERE parent_dn = ? ORDER BY name", parentDN)
	} else {
		rows, err = c.db.Query("SELECT dn, name, COALESCE(parent_dn,''), depth, usn_changed FROM ous ORDER BY depth, name")
	}
	if err != nil {
		return nil, fmt.Errorf("ad: lecture OUs: %w", err)
	}
	defer rows.Close()

	var resultat []CacheOU
	for rows.Next() {
		var o CacheOU
		if err := rows.Scan(&o.DN, &o.Name, &o.ParentDN, &o.Depth, &o.USNChanged); err != nil {
			return nil, fmt.Errorf("ad: lecture OUs: %w", err)
		}
		resultat = append(resultat, o)
	}
	return resultat, rows.Err()
}

// GetGroups retourne les groupes (filtrés par OU si fourni).
func (c *ADCache) GetGroups(ouDN string) ([]CacheGroup, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var rows *sql.Rows
	var err error
	if ouDN != "" {
		rows, err = c.db.Query("SELECT dn, cn, COALESCE(ou_dn,''), COALESCE(group_type,0), member_count, usn_changed FROM groups WHERE ou_dn = ? ORDER BY cn", ouDN)
	} else {
		rows, err = c.db.Query("SELECT dn, cn, COALESCE(ou_dn,''), COALESCE(group_type,0), member_count, usn_changed FROM groups ORDER BY cn")
	}
	if err != nil {
		return nil, fmt.Errorf("ad: lecture groupes: %w", err)
	}
	defer rows.Close()

	var resultat []CacheGroup
	for rows.Next() {
		var g CacheGroup
		if err := rows.Scan(&g.DN, &g.CN, &g.OUDN, &g.GroupType, &g.MemberCount, &g.USNChanged); err != nil {
			return nil, fmt.Errorf("ad: lecture groupes: %w", err)
		}
		resultat = append(resultat, g)
	}
	return resultat, rows.Err()
}

// GetUsersInOU retourne les utilisateurs d'une OU.
func (c *ADCache) GetUsersInOU(ouDN string) ([]CacheUser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	rows, err := c.db.Query(
		"SELECT "+colonnesUtilisateur+" FROM users WHERE ou_dn = ? ORDER BY cn",
		ouDN)
	if err != nil {
		return nil, fmt.Errorf("ad: lecture utilisateurs: %w", err)
	}
	defer rows.Close()
	return scannerUtilisateurs(rows)
}

// SearchUsers recherche des utilisateurs par SAM ou CN (LIKE).
func (c *ADCache) SearchUsers(query string, limit int) ([]CacheUser, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit <= 0 {
		limit = 100
	}

	motif := "%" + query + "%"
	rows, err := c.db.Query(
		`SELECT `+colonnesUtilisateur+`
		 FROM users WHERE sam LIKE ? OR cn LIKE ? ORDER BY cn LIMIT ?`,
		motif, motif, limit)
	if err != nil {
		return nil, fmt.Errorf("ad: recherche utilisateurs: %w", err)
	}
	defer rows.Close()
	return scannerUtilisateurs(rows)
}

// scannerUtilisateurs lit un jeu de lignes utilisateurs.
func scannerUtilisateurs(rows *sql.Rows) ([]CacheUser, error) {
	var resultat []CacheUser
	for rows.Next() {
		var u CacheUser
		if err := rows.Scan(&u.DN, &u.SAM, &u.CN, &u.GivenName, &u.SN, &u.DisplayName, &u.Mail, &u.OUDN, &u.UAC, &u.USNChanged); err != nil {
			return nil, fmt.Errorf("ad: lecture utilisateur: %w", err)
		}
		u.Desactive = u.UAC&2 != 0
		resultat = append(resultat, u)
	}
	return resultat, rows.Err()
}

// SearchOUs recherche des OUs par nom.
func (c *ADCache) SearchOUs(query string, limit int) ([]CacheOU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if limit <= 0 {
		limit = 50
	}

	rows, err := c.db.Query(
		"SELECT dn, name, COALESCE(parent_dn,''), depth, usn_changed FROM ous WHERE name LIKE ? ORDER BY depth, name LIMIT ?",
		"%"+query+"%", limit)
	if err != nil {
		return nil, fmt.Errorf("ad: recherche OUs: %w", err)
	}
	defer rows.Close()

	var resultat []CacheOU
	for rows.Next() {
		var o CacheOU
		if err := rows.Scan(&o.DN, &o.Name, &o.ParentDN, &o.Depth, &o.USNChanged); err != nil {
			return nil, fmt.Errorf("ad: recherche OUs: %w", err)
		}
		resultat = append(resultat, o)
	}
	return resultat, rows.Err()
}

// GetOUChildren retourne les enfants directs d'une OU (OUs, groupes, users).
func (c *ADCache) GetOUChildren(ouDN string) ([]EnfantOU, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	var resultat []EnfantOU

	ous, err := c.db.Query("SELECT dn, name FROM ous WHERE parent_dn = ? ORDER BY name", ouDN)
	if err != nil {
		return nil, fmt.Errorf("ad: lecture enfants OUs: %w", err)
	}
	for ous.Next() {
		var dn, nom string
		if err := ous.Scan(&dn, &nom); err != nil {
			ous.Close()
			return nil, err
		}
		resultat = append(resultat, EnfantOU{DN: dn, CN: nom, Kind: "ou"})
	}
	ous.Close()

	groupes, err := c.db.Query("SELECT dn, cn FROM groups WHERE ou_dn = ? ORDER BY cn", ouDN)
	if err != nil {
		return nil, fmt.Errorf("ad: lecture enfants groupes: %w", err)
	}
	for groupes.Next() {
		var dn, cn string
		if err := groupes.Scan(&dn, &cn); err != nil {
			groupes.Close()
			return nil, err
		}
		resultat = append(resultat, EnfantOU{DN: dn, CN: cn, Kind: "group"})
	}
	groupes.Close()

	utilisateurs, err := c.db.Query("SELECT dn, sam, cn, user_account_control FROM users WHERE ou_dn = ? ORDER BY cn", ouDN)
	if err != nil {
		return nil, fmt.Errorf("ad: lecture enfants utilisateurs: %w", err)
	}
	for utilisateurs.Next() {
		var dn, sam, cn string
		var uac int
		if err := utilisateurs.Scan(&dn, &sam, &cn, &uac); err != nil {
			utilisateurs.Close()
			return nil, err
		}
		resultat = append(resultat, EnfantOU{DN: dn, CN: cn, Kind: "user", SAM: sam, Desactive: uac&2 != 0})
	}
	utilisateurs.Close()

	return resultat, nil
}

// EnfantsExplorateur retourne les enfants d'une OU sous forme de nœuds
// d'explorateur (intégration avec l'arborescence de l'UI).
func (c *ADCache) EnfantsExplorateur(parentDN string, includeGroups bool) ([]*ExplorerNode, error) {
	enfants, err := c.GetOUChildren(parentDN)
	if err != nil {
		return nil, err
	}
	var noeuds []*ExplorerNode
	for _, e := range enfants {
		switch e.Kind {
		case "ou":
			noeuds = append(noeuds, &ExplorerNode{
				DN: e.DN, Name: e.CN, Type: "ou", ParentDN: parentDN,
				HasChildren: true, Children: []*ExplorerNode{},
			})
		case "group":
			if includeGroups {
				noeuds = append(noeuds, &ExplorerNode{
					DN: e.DN, Name: e.CN, Type: "group", ParentDN: parentDN,
				})
			}
		}
	}
	return noeuds, nil
}

// ─── Enumération AD ───────────────────────────────────────────────────────

// listerEntrees effectue une recherche LDAP complète avec pagination.
func (p *Pool) listerEntrees(ctx context.Context, base, filter string, attrs []string) ([]*ldap.Entry, error) {
	conn, err := p.Get(ctx)
	if err != nil {
		return nil, err
	}
	defer p.Put(conn)

	var entrees []*ldap.Entry
	ctrl := ldap.NewControlPaging(500)
	for {
		req := ldap.NewSearchRequest(base, ldap.ScopeWholeSubtree, ldap.NeverDerefAliases, 0, 0, false, filter, attrs, []ldap.Control{ctrl})
		res, err := conn.Search(req)
		if err != nil {
			return nil, fmt.Errorf("ad: recherche cache sous %s: %w", base, err)
		}
		entrees = append(entrees, res.Entries...)
		for _, c := range res.Controls {
			if pc, ok := c.(*ldap.ControlPaging); ok {
				ctrl.SetCookie(pc.Cookie)
			}
		}
		if len(ctrl.Cookie) == 0 {
			break
		}
	}
	return entrees, nil
}

// listerOUsAD liste les OUs (delta depuis « depuis » si > 0).
func (p *Pool) listerOUsAD(ctx context.Context, base string, depuis int64) ([]CacheOU, error) {
	filter := "(objectClass=organizationalUnit)"
	if depuis > 0 {
		filter = fmt.Sprintf("(&(objectClass=organizationalUnit)(uSNChanged>=%d))", depuis)
	}
	entrees, err := p.listerEntrees(ctx, base, filter, []string{"dn", "ou", "cn", "uSNChanged"})
	if err != nil {
		return nil, err
	}

	// Parent map pour le calcul de profondeur (comme Python).
	parents := map[string]string{}
	for _, e := range entrees {
		if i := strings.Index(e.DN, ","); i >= 0 {
			parents[e.DN] = e.DN[i+1:]
		}
	}

	var ous []CacheOU
	for _, e := range entrees {
		nom := e.GetAttributeValue("ou")
		if nom == "" {
			nom = e.GetAttributeValue("cn")
		}
		parent := parents[e.DN]
		depth := 0
		for courant := parent; courant != ""; {
			if _, ok := parents[courant]; !ok {
				break
			}
			depth++
			courant = parents[courant]
		}
		ous = append(ous, CacheOU{
			DN:         e.DN,
			Name:       nom,
			ParentDN:   parent,
			Depth:      depth,
			USNChanged: attrsEntier(e, "uSNChanged"),
		})
	}
	return ous, nil
}

// listerGroupesAD liste les groupes (delta depuis « depuis » si > 0).
func (p *Pool) listerGroupesAD(ctx context.Context, base string, depuis int64) ([]CacheGroup, error) {
	filter := "(objectClass=group)"
	if depuis > 0 {
		filter = fmt.Sprintf("(&(objectClass=group)(uSNChanged>=%d))", depuis)
	}
	entrees, err := p.listerEntrees(ctx, base, filter, []string{"dn", "cn", "groupType", "member", "uSNChanged"})
	if err != nil {
		return nil, err
	}

	var groupes []CacheGroup
	for _, e := range entrees {
		cn := e.GetAttributeValue("cn")
		groupes = append(groupes, CacheGroup{
			DN:          e.DN,
			CN:          cn,
			OUDN:        parentDN(e.DN),
			GroupType:   int(attrsEntier(e, "groupType")),
			MemberCount: len(e.GetAttributeValues("member")),
			USNChanged:  attrsEntier(e, "uSNChanged"),
		})
	}
	return groupes, nil
}

// listerUtilisateursAD liste les utilisateurs (delta depuis « depuis » si > 0).
func (p *Pool) listerUtilisateursAD(ctx context.Context, base string, depuis int64) ([]CacheUser, error) {
	filter := "(&(objectClass=user)(objectCategory=person))"
	if depuis > 0 {
		filter = fmt.Sprintf("(&(objectClass=user)(objectCategory=person)(uSNChanged>=%d))", depuis)
	}
	entrees, err := p.listerEntrees(ctx, base, filter,
		[]string{"dn", "sAMAccountName", "cn", "givenName", "sn", "displayName", "mail", "userAccountControl", "uSNChanged"})
	if err != nil {
		return nil, err
	}

	var utilisateurs []CacheUser
	for _, e := range entrees {
		uac := attrsEntier(e, "userAccountControl")
		if e.GetAttributeValue("userAccountControl") == "" {
			uac = 512
		}
		cn := e.GetAttributeValue("cn")
		if cn == "" {
			cn = e.GetAttributeValue("sAMAccountName")
		}
		utilisateurs = append(utilisateurs, CacheUser{
			DN:          e.DN,
			SAM:         e.GetAttributeValue("sAMAccountName"),
			CN:          cn,
			GivenName:   e.GetAttributeValue("givenName"),
			SN:          e.GetAttributeValue("sn"),
			DisplayName: e.GetAttributeValue("displayName"),
			Mail:        e.GetAttributeValue("mail"),
			OUDN:        parentDN(e.DN),
			UAC:         int(uac),
			USNChanged:  attrsEntier(e, "uSNChanged"),
		})
	}
	return utilisateurs, nil
}

// attrsEntier lit un attribut LDAP numérique (0 si absent ou invalide).
func attrsEntier(e *ldap.Entry, attr string) int64 {
	v := e.GetAttributeValue(attr)
	if v == "" {
		return 0
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0
	}
	return n
}

// ─── Intégration Pool ─────────────────────────────────────────────────────

// SetADCache attache un cache SQLite au pool (utilisé par l'explorateur).
func (p *Pool) SetADCache(c *ADCache) { p.cacheAD = c }

// ADCache retourne le cache SQLite attaché (ou nil).
func (p *Pool) ADCache() *ADCache { return p.cacheAD }
