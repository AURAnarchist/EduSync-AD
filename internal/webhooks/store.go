package webhooks

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	_ "modernc.org/sqlite" // pilote pur Go, sans CGO

	"github.com/estemobs/EduSync-AD/internal/config"
)

// formatISO reproduit `datetime.isoformat(timespec="seconds")` côté Python.
const formatISO = "2006-01-02T15:04:05-07:00"

// schemaLivraisons reproduit **exactement** le schéma de `_init_db` côté
// Python : une base écrite par Python se rouvre telle quelle en Go, et
// inversement.
const schemaLivraisons = `
CREATE TABLE IF NOT EXISTS deliveries (
    id TEXT PRIMARY KEY,
    webhook_id TEXT NOT NULL,
    event TEXT NOT NULL,
    payload TEXT NOT NULL,
    attempt INTEGER NOT NULL,
    status_code INTEGER,
    response_body TEXT,
    error TEXT,
    started_at TEXT NOT NULL,
    completed_at TEXT,
    next_retry_at TEXT
);
CREATE INDEX IF NOT EXISTS idx_deliveries_webhook ON deliveries (webhook_id);
CREATE INDEX IF NOT EXISTS idx_deliveries_pending ON deliveries (next_retry_at)
    WHERE next_retry_at IS NOT NULL;
`

// Livraison est une tentative de livraison journalisée, au format de la table
// `deliveries`.
type Livraison struct {
	ID            string         `json:"id"`
	WebhookID     string         `json:"webhook_id"`
	Evenement     string         `json:"event"`
	Donnees       map[string]any `json:"payload"`
	Tentative     int            `json:"attempt"`
	Statut        *int           `json:"status_code"`
	Corps         *string        `json:"response_body"`
	Erreur        *string        `json:"error"`
	CommenceLe    string         `json:"started_at"`
	TermineLe     *string        `json:"completed_at"`
	ProchaineTent *string        `json:"next_retry_at"`
}

// Store persiste les webhooks (`webhooks.json`) et les livraisons (SQLite).
type Store struct {
	cheminConfig     string
	cheminLivraisons string

	mu       sync.Mutex
	webhooks map[string]WebhookConfig
	base     *sql.DB
}

// CheminConfigDefaut retourne `webhooks.json` dans le dossier de configuration.
func CheminConfigDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichier), nil
}

// CheminLivraisonsDefaut retourne `webhook_deliveries.db`.
func CheminLivraisonsDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichierLivraisons), nil
}

// OuvrirStore ouvre (et crée au besoin) le magasin des webhooks. Un chemin
// vide utilise les emplacements par défaut.
func OuvrirStore(cheminConfig, cheminLivraisons string) (*Store, error) {
	var err error
	if cheminConfig == "" {
		if cheminConfig, err = CheminConfigDefaut(); err != nil {
			return nil, err
		}
	}
	if cheminLivraisons == "" {
		if cheminLivraisons, err = CheminLivraisonsDefaut(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(cheminConfig), 0o700); err != nil {
		return nil, fmt.Errorf("webhooks: création du dossier de configuration: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(cheminLivraisons), 0o700); err != nil {
		return nil, fmt.Errorf("webhooks: création du dossier des livraisons: %w", err)
	}

	base, err := sql.Open("sqlite", dsnSQLite(cheminLivraisons))
	if err != nil {
		return nil, fmt.Errorf("webhooks: ouverture de %s: %w", cheminLivraisons, err)
	}
	// Un seul écrivain à la fois, comme côté Python (sqlite3.connect).
	base.SetMaxOpenConns(1)
	if _, err := base.Exec(schemaLivraisons); err != nil {
		_ = base.Close()
		return nil, fmt.Errorf("webhooks: schéma des livraisons: %w", err)
	}

	store := &Store{
		cheminConfig:     cheminConfig,
		cheminLivraisons: cheminLivraisons,
		webhooks:         map[string]WebhookConfig{},
		base:             base,
	}
	store.charger()
	return store, nil
}

// dsnSQLite active les clés étrangères et un verrou d'écriture court.
func dsnSQLite(chemin string) string {
	return "file:" + chemin + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
}

// charger relit `webhooks.json` (jamais bloquant).
func (s *Store) charger() {
	donnees, err := os.ReadFile(s.cheminConfig)
	if err != nil {
		return
	}
	for _, webhook := range DepuisJSON(donnees) {
		s.webhooks[webhook.ID] = webhook
	}
}

// CheminConfig retourne le chemin du fichier de configuration.
func (s *Store) CheminConfig() string { return s.cheminConfig }

// CheminLivraisons retourne le chemin de la base des livraisons.
func (s *Store) CheminLivraisons() string { return s.cheminLivraisons }

// enregistrer réécrit `webhooks.json` (l'appelant doit détenir le verrou).
func (s *Store) enregistrer() error {
	liste := make([]WebhookConfig, 0, len(s.webhooks))
	for _, webhook := range s.webhooks {
		liste = append(liste, webhook)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].CreeLe < liste[j].CreeLe })
	donnees, err := ToJSON(liste)
	if err != nil {
		return err
	}
	if err := os.WriteFile(s.cheminConfig, donnees, 0o600); err != nil {
		return fmt.Errorf("webhooks: écriture de %s: %w", s.cheminConfig, err)
	}
	return nil
}

// Enregistrer force la réécriture du fichier de configuration.
func (s *Store) Enregistrer() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.enregistrer()
}

// Creer ajoute un webhook après validation.
func (s *Store) Creer(webhook WebhookConfig) (WebhookConfig, error) {
	if erreurs := webhook.Valider(); len(erreurs) > 0 {
		return WebhookConfig{}, nouvelleErreurWebhooks("%s", joindre(erreurs))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if webhook.CreeLe == "" {
		webhook.CreeLe = time.Now().UTC().Format(formatISO)
	}
	if webhook.Entetes == nil {
		webhook.Entetes = map[string]string{}
	}
	if webhook.Evenements == nil {
		webhook.Evenements = []string{}
	}
	if webhook.DelaiSecondes == 0 {
		webhook.DelaiSecondes = 10
	}
	if webhook.PolitiqueRetrai == "" {
		webhook.PolitiqueRetrai = RetraitExponentiel
	}
	if webhook.RetraitsMax == 0 {
		webhook.RetraitsMax = 5
	}
	s.webhooks[webhook.ID] = webhook
	if err := s.enregistrer(); err != nil {
		return WebhookConfig{}, err
	}
	return webhook, nil
}

// Obtenir retourne un webhook par identifiant.
func (s *Store) Obtenir(id string) (*WebhookConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	webhook, ok := s.webhooks[id]
	if !ok {
		return nil, nil
	}
	return &webhook, nil
}

// Lister retourne les webhooks triés par date de création.
func (s *Store) Lister(actifsSeulement bool) []WebhookConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	liste := make([]WebhookConfig, 0, len(s.webhooks))
	for _, webhook := range s.webhooks {
		if actifsSeulement && !webhook.Actif {
			continue
		}
		liste = append(liste, webhook)
	}
	sort.Slice(liste, func(i, j int) bool { return liste[i].CreeLe < liste[j].CreeLe })
	return liste
}

// MettreAJour modifie un webhook puis revalide l'ensemble.
func (s *Store) MettreAJour(id string, modifier func(*WebhookConfig)) (*WebhookConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	webhook, ok := s.webhooks[id]
	if !ok {
		return nil, nil
	}
	if modifier != nil {
		modifier(&webhook)
	}
	if erreurs := webhook.Valider(); len(erreurs) > 0 {
		return nil, nouvelleErreurWebhooks("%s", joindre(erreurs))
	}
	s.webhooks[id] = webhook
	if err := s.enregistrer(); err != nil {
		return nil, err
	}
	return &webhook, nil
}

// Supprimer retire un webhook.
func (s *Store) Supprimer(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.webhooks[id]; !ok {
		return false, nil
	}
	delete(s.webhooks, id)
	if err := s.enregistrer(); err != nil {
		return false, err
	}
	return true, nil
}

// Abonnes retourne les webhooks actifs abonnés à un événement.
func (s *Store) Abonnes(evenement string) []WebhookConfig {
	var abonnes []WebhookConfig
	for _, webhook := range s.Lister(true) {
		if webhook.Abonne(evenement) {
			abonnes = append(abonnes, webhook)
		}
	}
	return abonnes
}

// Journaliser enregistre une tentative de livraison.
func (s *Store) Journaliser(livraison Livraison) error {
	donnees, err := json.Marshal(livraison.Donnees)
	if err != nil {
		return fmt.Errorf("webhooks: sérialisation de la charge: %w", err)
	}
	_, err = s.base.Exec(`
		INSERT INTO deliveries (id, webhook_id, event, payload, attempt,
			status_code, response_body, error, started_at, completed_at, next_retry_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		livraison.ID, livraison.WebhookID, livraison.Evenement, string(donnees),
		livraison.Tentative, livraison.Statut, livraison.Corps, livraison.Erreur,
		livraison.CommenceLe, livraison.TermineLe, livraison.ProchaineTent)
	if err != nil {
		return fmt.Errorf("webhooks: journalisation de la livraison: %w", err)
	}
	return nil
}

// Remplacer enregistre une livraison en écrasant la ligne existante
// (mise à jour après une nouvelle tentative).
func (s *Store) Remplacer(livraison Livraison) error {
	if _, err := s.base.Exec(`DELETE FROM deliveries WHERE id = ?`, livraison.ID); err != nil {
		return fmt.Errorf("webhooks: remplacement de la livraison: %w", err)
	}
	return s.Journaliser(livraison)
}

// Livraisons retourne l'historique des livraisons d'un webhook (les plus
// récentes d'abord).
func (s *Store) Livraisons(webhookID string, limite int) ([]Livraison, error) {
	if limite <= 0 {
		limite = 50
	}
	requete := `SELECT id, webhook_id, event, payload, attempt, status_code,
		response_body, error, started_at, completed_at, next_retry_at
		FROM deliveries`
	var args []any
	if webhookID != "" {
		requete += ` WHERE webhook_id = ?`
		args = append(args, webhookID)
	}
	requete += ` ORDER BY started_at DESC, rowid DESC LIMIT ?`
	args = append(args, limite)

	lignes, err := s.base.Query(requete, args...)
	if err != nil {
		return nil, fmt.Errorf("webhooks: lecture des livraisons: %w", err)
	}
	defer lignes.Close()

	var livraisons []Livraison
	for lignes.Next() {
		livraison, err := scannerLivraison(lignes)
		if err != nil {
			return nil, err
		}
		livraisons = append(livraisons, *livraison)
	}
	return livraisons, lignes.Err()
}

// TentativesEnAttente retourne les livraisons à réessayer, dont l'échéance est
// passée, triées par échéance.
func (s *Store) TentativesEnAttente(maintenant time.Time, limite int) ([]Livraison, error) {
	if limite <= 0 {
		limite = 50
	}
	lignes, err := s.base.Query(`
		SELECT id, webhook_id, event, payload, attempt, status_code,
			response_body, error, started_at, completed_at, next_retry_at
		FROM deliveries
		WHERE next_retry_at IS NOT NULL AND next_retry_at <= ?
		ORDER BY next_retry_at ASC LIMIT ?`,
		maintenant.UTC().Format(formatISO), limite)
	if err != nil {
		return nil, fmt.Errorf("webhooks: lecture des tentatives: %w", err)
	}
	defer lignes.Close()

	var livraisons []Livraison
	for lignes.Next() {
		livraison, err := scannerLivraison(lignes)
		if err != nil {
			return nil, err
		}
		livraisons = append(livraisons, *livraison)
	}
	return livraisons, lignes.Err()
}

// CompterLivraisons compte les livraisons journalisées.
func (s *Store) CompterLivraisons() (int, error) {
	var nombre int
	if err := s.base.QueryRow(`SELECT COUNT(*) FROM deliveries`).Scan(&nombre); err != nil {
		return 0, fmt.Errorf("webhooks: comptage des livraisons: %w", err)
	}
	return nombre, nil
}

// Fermer libère la base et réécrit la configuration.
func (s *Store) Fermer() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.enregistrer(); err != nil {
		return err
	}
	if s.base == nil {
		return nil
	}
	base := s.base
	s.base = nil
	return base.Close()
}

// scannerLivraison lit une ligne de `deliveries`.
func scannerLivraison(scanner interface{ Scan(...any) error }) (*Livraison, error) {
	var (
		livraison     Livraison
		charge        string
		statut        sql.NullInt64
		corps         sql.NullString
		erreur        sql.NullString
		termine       sql.NullString
		prochaineTent sql.NullString
	)
	if err := scanner.Scan(&livraison.ID, &livraison.WebhookID, &livraison.Evenement,
		&charge, &livraison.Tentative, &statut, &corps, &erreur,
		&livraison.CommenceLe, &termine, &prochaineTent); err != nil {
		return nil, fmt.Errorf("webhooks: lecture d'une livraison: %w", err)
	}
	if err := json.Unmarshal([]byte(charge), &livraison.Donnees); err != nil {
		livraison.Donnees = map[string]any{}
	}
	if statut.Valid {
		valeur := int(statut.Int64)
		livraison.Statut = &valeur
	}
	if corps.Valid {
		livraison.Corps = &corps.String
	}
	if erreur.Valid {
		livraison.Erreur = &erreur.String
	}
	if termine.Valid {
		livraison.TermineLe = &termine.String
	}
	if prochaineTent.Valid {
		livraison.ProchaineTent = &prochaineTent.String
	}
	return &livraison, nil
}

// joindre assemble des messages d'erreur comme `"; ".join(errors)`.
func joindre(erreurs []string) string {
	resultat := ""
	for index, erreur := range erreurs {
		if index > 0 {
			resultat += "; "
		}
		resultat += erreur
	}
	return resultat
}
