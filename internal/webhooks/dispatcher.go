package webhooks

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/estemobs/EduSync-AD/internal/config"
	edcrypto "github.com/estemobs/EduSync-AD/internal/crypto"
)

// CorpsMaxLecture borne le corps de réponse conservé (4 Kio, comme Python).
const CorpsMaxLecture = 4096

// IntervalleRetrait est la période de la boucle de nouvelles tentatives.
const IntervalleRetrait = 30 * time.Second

// NomFichierCle est le fichier de clé locale partagé avec Python.
const NomFichierCle = "secret.key"

// Dispatcher livre les événements aux webhooks abonnés.
//
// Les secrets sont stockés chiffrés (AES-256-GCM, `secret.key` partagé avec
// Python). Un secret écrit en clair — ce que fait l'interface Python, malgré
// son intention — est utilisé tel quel : la relecture reste possible dans les
// deux sens.
type Dispatcher struct {
	Store *Store

	// Client est le client HTTP utilisé (injectable dans les tests).
	Client *http.Client
	// CheminCle est le chemin de `secret.key` (vide = dossier de config).
	CheminCle string
	// IntervalleRetrait est la période de la boucle de réessai.
	IntervalleRetrait time.Duration
	// Horloge retourne l'instant courant (injectable dans les tests).
	Horloge func() time.Time

	mu      sync.Mutex
	arret   chan struct{}
	enCours bool
}

// NouveauDispatcher construit un dispatcher sur un magasin.
func NouveauDispatcher(store *Store) *Dispatcher {
	return &Dispatcher{
		Store:             store,
		Client:            &http.Client{Timeout: 10 * time.Second},
		IntervalleRetrait: IntervalleRetrait,
		Horloge:           func() time.Time { return time.Now().UTC() },
	}
}

// maintenant retourne l'instant courant (UTC).
func (d *Dispatcher) maintenant() time.Time {
	if d.Horloge != nil {
		return d.Horloge().UTC()
	}
	return time.Now().UTC()
}

// cle charge (ou crée) la clé locale de chiffrement.
func (d *Dispatcher) cle() ([]byte, error) {
	chemin := d.CheminCle
	if chemin == "" {
		var err error
		if chemin, err = CheminCleDefaut(); err != nil {
			return nil, err
		}
	}
	return edcrypto.LoadOrCreateKey(chemin)
}

// CheminCleDefaut retourne `secret.key` dans le dossier de configuration.
func CheminCleDefaut() (string, error) {
	dir, err := config.ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, NomFichierCle), nil
}

// ChiffrerSecret chiffre un secret de webhook (chaîne vide conservée telle
// quelle). La clé est lue par le magasin de clés partagé.
func (d *Dispatcher) ChiffrerSecret(secret string) (string, error) {
	if secret == "" {
		return "", nil
	}
	cle, err := d.cle()
	if err != nil {
		return "", err
	}
	return edcrypto.Encrypt(cle, secret)
}

// dechiffrerSecret retourne le secret exploitable pour la signature.
func (d *Dispatcher) dechiffrerSecret(secret string) string {
	if secret == "" {
		return ""
	}
	cle, err := d.cle()
	if err != nil {
		return secret
	}
	clair, err := edcrypto.Decrypt(cle, secret)
	if err != nil {
		// Secret écrit en clair (interface Python) : utilisé tel quel.
		return secret
	}
	return clair
}

// Diffuser livre un événement à tous les webhooks abonnés et retourne le
// journal des livraisons effectuées (une entrée par tentative immédiate).
func (d *Dispatcher) Diffuser(evenement string, donnees map[string]any) ([]Livraison, error) {
	if d.Store == nil {
		return nil, nouvelleErreurWebhooks("webhooks: aucun magasin configuré")
	}
	if donnees == nil {
		donnees = map[string]any{}
	}
	var journal []Livraison
	for _, webhook := range d.Store.Abonnes(evenement) {
		livraison, err := d.Livrer(webhook, evenement, donnees, 0)
		if err != nil {
			return journal, err
		}
		journal = append(journal, *livraison)
	}
	return journal, nil
}

// Livrer effectue une tentative de livraison et la journalise.
//
// `tentative` est le numéro de la tentative précédente : 0 pour un envoi
// initial. Le corps envoyé est `{"event":…,"timestamp":…,"data":…}` et la
// signature HMAC-SHA256 du corps est transmise dans `X-Webhook-Signature`.
func (d *Dispatcher) Livrer(webhook WebhookConfig, evenement string,
	donnees map[string]any, tentative int) (*Livraison, error) {
	if d.Store == nil {
		return nil, nouvelleErreurWebhooks("webhooks: aucun magasin configuré")
	}
	moment := d.maintenant()
	commence := moment.Format(formatISO)

	corps, err := json.Marshal(map[string]any{
		"event":     evenement,
		"timestamp": commence,
		"data":      donnees,
	})
	if err != nil {
		return nil, fmt.Errorf("webhooks: sérialisation de l'événement: %w", err)
	}

	livraison := &Livraison{
		ID:         nouvelIdentifiant(),
		WebhookID:  webhook.ID,
		Evenement:  evenement,
		Donnees:    donnees,
		Tentative:  tentative + 1,
		CommenceLe: commence,
	}

	entetes := map[string]string{
		"Content-Type":        "application/json",
		"User-Agent":          "EduSync-Webhook/1.0",
		"X-Webhook-Event":     evenement,
		"X-Webhook-Delivery":  livraison.ID,
		"X-Webhook-Signature": "sha256=" + d.signer(webhook, corps),
	}
	// Les entêtes personnalisés priment, comme `headers.update(wh.headers)`.
	for nom, valeur := range webhook.Entetes {
		entetes[nom] = valeur
	}

	requete, err := http.NewRequest(http.MethodPost, webhook.URL, bytes.NewReader(corps))
	if err != nil {
		return nil, fmt.Errorf("webhooks: requête vers %s: %w", webhook.URL, err)
	}
	for nom, valeur := range entetes {
		requete.Header.Set(nom, valeur)
	}

	client := d.Client
	if client == nil {
		client = &http.Client{Timeout: time.Duration(webhook.DelaiSecondes * float64(time.Second))}
	}
	reponse, err := client.Do(requete)
	switch {
	case err != nil:
		statut := 0
		message := err.Error()
		livraison.Statut = &statut
		livraison.Erreur = &message
	default:
		statut := reponse.StatusCode
		lecture, _ := io.ReadAll(io.LimitReader(reponse.Body, CorpsMaxLecture))
		_ = reponse.Body.Close()
		corpsueur := tronquer(string(lecture), CorpsMaxLecture)
		livraison.Statut = &statut
		livraison.Corps = &corpsueur
		if statut < 200 || statut >= 300 {
			message := fmt.Sprintf("HTTP %d", statut)
			livraison.Erreur = &message
		}
	}

	termine := d.maintenant().Format(formatISO)
	livraison.TermineLe = &termine
	livraison.ProchaineTent = d.prochaineTentative(webhook, livraison)

	if err := d.Store.Journaliser(*livraison); err != nil {
		return livraison, err
	}
	d.majWebhook(webhook, livraison)
	return livraison, nil
}

// signer calcule la signature HMAC-SHA256 du corps (chaîne vide sans secret).
func (d *Dispatcher) signer(webhook WebhookConfig, corps []byte) string {
	secret := d.dechiffrerSecret(webhook.Secret)
	if secret == "" {
		return ""
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(corps)
	return hex.EncodeToString(mac.Sum(nil))
}

// prochaineTentative planifie une nouvelle tentative après un échec.
func (d *Dispatcher) prochaineTentative(webhook WebhookConfig, livraison *Livraison) *string {
	if livraison.Erreur == nil || livraison.Tentative >= webhook.RetraitsMax {
		return nil
	}
	var delai time.Duration
	switch webhook.PolitiqueRetrai {
	case RetraitExponentiel:
		secondes := 1 << uint(livraison.Tentative) * 60
		if secondes > 3600 {
			secondes = 3600
		}
		delai = time.Duration(secondes) * time.Second
	case RetraitFixe:
		delai = 5 * time.Minute
	default:
		return nil
	}
	prochaine := d.maintenant().Add(delai).Format(formatISO)
	return &prochaine
}

// majWebhook met à jour l'état de la dernière livraison.
func (d *Dispatcher) majWebhook(webhook WebhookConfig, livraison *Livraison) {
	if livraison.TermineLe == nil {
		return
	}
	_, _ = d.Store.MettreAJour(webhook.ID, func(cible *WebhookConfig) {
		termine := *livraison.TermineLe
		cible.DernierDecl = &termine
		cible.DernierStatut = copieEntier(livraison.Statut)
		cible.DerniereErreur = copieChaine(livraison.Erreur)
	})
}

// Demarrer lance la boucle de réessai (idempotent).
func (d *Dispatcher) Demarrer() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.enCours {
		return
	}
	d.enCours = true
	d.arret = make(chan struct{})
	arret := d.arret
	intervalle := d.IntervalleRetrait
	if intervalle <= 0 {
		intervalle = IntervalleRetrait
	}
	go func() {
		ticker := time.NewTicker(intervalle)
		defer ticker.Stop()
		for {
			select {
			case <-arret:
				return
			case <-ticker.C:
				_, _ = d.ExecuterRetraits()
			}
		}
	}()
}

// Arreter arrête la boucle de réessai (idempotent).
func (d *Dispatcher) Arreter() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.enCours {
		return
	}
	close(d.arret)
	d.enCours = false
	d.arret = nil
}

// ExecuterRetraits rejoue les livraisons échues et retourne les livraisons
// effectuées.
func (d *Dispatcher) ExecuterRetraits() ([]Livraison, error) {
	if d.Store == nil {
		return nil, nouvelleErreurWebhooks("webhooks: aucun magasin configuré")
	}
	enAttente, err := d.Store.TentativesEnAttente(d.maintenant(), 20)
	if err != nil {
		return nil, err
	}
	var effectuees []Livraison
	for _, ancienne := range enAttente {
		webhook, err := d.Store.Obtenir(ancienne.WebhookID)
		if err != nil {
			return effectuees, err
		}
		if webhook == nil || !webhook.Actif {
			// Webhook supprimé ou désactivé : la tentative n'a plus lieu
			// d'être (la ligne de journal est conservée).
			if err := d.annulerRetrait(ancienne); err != nil {
				return effectuees, err
			}
			continue
		}
		livraison, err := d.Livrer(*webhook, ancienne.Evenement, ancienne.Donnees, ancienne.Tentative)
		if err != nil {
			return effectuees, err
		}
		// La nouvelle tentative remplace la ligne d'origine (Python insérait
		// une ligne de plus : un correctif de forme, schéma inchangé).
		if ancienne.ID != livraison.ID {
			if _, err := d.Store.base.Exec(`DELETE FROM deliveries WHERE id = ?`, ancienne.ID); err != nil {
				return effectuees, fmt.Errorf("webhooks: suppression de la tentative: %w", err)
			}
		}
		effectuees = append(effectuees, *livraison)
	}
	return effectuees, nil
}

// annulerRetrait efface la planification d'une livraison sans webhook actif.
func (d *Dispatcher) annulerRetrait(livraison Livraison) error {
	_, err := d.Store.base.Exec(`UPDATE deliveries SET next_retry_at = NULL WHERE id = ?`,
		livraison.ID)
	if err != nil {
		return fmt.Errorf("webhooks: annulation de la tentative: %w", err)
	}
	return nil
}

// nouvelIdentifiant génère un identifiant de livraison de 16 caractères
// hexadécimaux (comme `uuid4().hex[:16]`).
func nouvelIdentifiant() string {
	octets := make([]byte, 8)
	if _, err := io.ReadFull(rand.Reader, octets); err != nil {
		return fmt.Sprintf("%016x", time.Now().UnixNano())
	}
	return hex.EncodeToString(octets)
}

// NouvelIdentifiantWebhook génère un identifiant de webhook (`w` suivi de
// 12 caractères hexadécimaux), au format `w` + `uuid4().hex[:12]` côté Python.
func NouvelIdentifiantWebhook() string {
	octets := make([]byte, 6)
	if _, err := rand.Read(octets); err != nil {
		return fmt.Sprintf("w%012x", time.Now().UnixNano()&0xffffffffffff)
	}
	return "w" + hex.EncodeToString(octets)
}

func tronquer(texte string, maximum int) string {
	if len(texte) <= maximum {
		return texte
	}
	return texte[:maximum]
}

func copieEntier(valeur *int) *int {
	if valeur == nil {
		return nil
	}
	copie := *valeur
	return &copie
}

func copieChaine(valeur *string) *string {
	if valeur == nil {
		return nil
	}
	copie := *valeur
	return &copie
}

// SéparerEvenements découpe une liste « a,b » (utilisée par la CLI).
func SéparerEvenements(liste string) []string {
	var resultat []string
	for _, morceau := range strings.Split(liste, ",") {
		morceau = strings.TrimSpace(morceau)
		if morceau != "" {
			resultat = append(resultat, morceau)
		}
	}
	return resultat
}
