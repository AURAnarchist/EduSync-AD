// Package webhooks implémente les notifications événementielles (M28) :
// configuration `webhooks.json`, journal des livraisons
// `webhook_deliveries.db` et livraison HTTP signée HMAC-SHA256.
//
// Les formats de fichiers sont ceux de `core/webhooks.py` : un `webhooks.json`
// écrit par Python se relit tel quel en Go, et inversement.
package webhooks

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
)

// NomFichier est le fichier de configuration des webhooks (dossier de config).
const NomFichier = "webhooks.json"

// NomFichierLivraisons est la base SQLite du journal des livraisons (dossier de
// configuration, comme côté Python).
const NomFichierLivraisons = "webhook_deliveries.db"

// Joker abonne un webhook à tous les événements.
const Joker = "*"

// Evenements liste les événements standards, dans l'ordre de `WebhookEvent`.
var Evenements = []string{
	"user.created",
	"user.updated",
	"user.deleted",
	"user.moved",
	"user.disabled",
	"user.enabled",
	"user.password_reset",
	"group.created",
	"group.updated",
	"group.deleted",
	"group.member_added",
	"group.member_removed",
	"ou.created",
	"ou.renamed",
	"ou.deleted",
	"portal.code_sent",
	"portal.password_reset",
	"portal.lookup",
	"portal.request_submitted",
	"portal.request_approved",
	"portal.request_refused",
	"audit.entry",
	"system.startup",
	"system.shutdown",
}

// Politiques de nouvelle tentative.
const (
	RetraitExponentiel = "exponential"
	RetraitFixe        = "fixed"
	RetraitAucun       = "none"
)

// evenementsConnus indexe les événements standards pour la validation.
var evenementsConnus = func() map[string]bool {
	index := make(map[string]bool, len(Evenements))
	for _, evenement := range Evenements {
		index[evenement] = true
	}
	return index
}()

// WebhookConfig décrit un webhook abonné.
//
// Les noms JSON sont **exactement** ceux du dataclass Python (`asdict`) :
// `retry_policy`, `max_retries`, `timeout_seconds`… Un fichier écrit par une
// version se relit sans migration dans l'autre.
type WebhookConfig struct {
	ID              string            `json:"id"`
	Nom             string            `json:"name"`
	URL             string            `json:"url"`
	Evenements      []string          `json:"events"`
	Secret          string            `json:"secret"`
	Entetes         map[string]string `json:"headers"`
	Actif           bool              `json:"active"`
	CreeLe          string            `json:"created_at"`
	DernierDecl     *string           `json:"last_triggered_at"`
	DernierStatut   *int              `json:"last_status"`
	DerniereErreur  *string           `json:"last_error"`
	PolitiqueRetrai string            `json:"retry_policy"`
	RetraitsMax     int               `json:"max_retries"`
	DelaiSecondes   float64           `json:"timeout_seconds"`
}

// NouveauWebhook construit un webhook avec les valeurs par défaut de Python.
func NouveauWebhook(id, nom, adresse string, evenements []string) WebhookConfig {
	return WebhookConfig{
		ID:              id,
		Nom:             nom,
		URL:             adresse,
		Evenements:      evenements,
		Entetes:         map[string]string{},
		Actif:           true,
		PolitiqueRetrai: RetraitExponentiel,
		RetraitsMax:     5,
		DelaiSecondes:   10,
	}
}

// Valider retourne les erreurs de configuration, avec les messages de
// `WebhookConfig.validate` côté Python.
func (w WebhookConfig) Valider() []string {
	var erreurs []string
	if strings.TrimSpace(w.Nom) == "" {
		erreurs = append(erreurs, "Nom requis")
	}
	if strings.TrimSpace(w.URL) == "" {
		erreurs = append(erreurs, "URL requise")
	} else {
		analyse, err := url.Parse(w.URL)
		if err != nil || (analyse.Scheme != "http" && analyse.Scheme != "https") {
			erreurs = append(erreurs, "URL doit être http:// ou https://")
		}
		if err == nil && analyse.Host == "" {
			erreurs = append(erreurs, "URL invalide (hôte manquant)")
		}
	}
	if len(w.Evenements) == 0 {
		erreurs = append(erreurs, "Au moins un événement requis")
	}
	for _, evenement := range w.Evenements {
		if evenement != Joker && !evenementsConnus[evenement] {
			erreurs = append(erreurs, fmt.Sprintf("Événement inconnu : %s", evenement))
		}
	}
	if w.RetraitsMax < 0 || w.RetraitsMax > 20 {
		erreurs = append(erreurs, "Max retries invalide (0-20)")
	}
	if w.DelaiSecondes <= 0 || w.DelaiSecondes > 60 {
		erreurs = append(erreurs, "Timeout invalide (1-60s)")
	}
	return erreurs
}

// Abonne indique si le webhook écoute cet événement.
func (w WebhookConfig) Abonne(evenement string) bool {
	for _, abonnement := range w.Evenements {
		if abonnement == Joker || abonnement == evenement {
			return true
		}
	}
	return false
}

// Dict retourne la représentation JSON du webhook, avec les clés Python.
// L'ordre des clés suit l'ordre alphabétique de `json.Marshal` (l'ordre des
// champs du dataclass Python n'est pas significatif en JSON).
func (w WebhookConfig) Dict() map[string]any {
	entetes := map[string]string{}
	for cle, valeur := range w.Entetes {
		entetes[cle] = valeur
	}
	evenements := append([]string{}, w.Evenements...)
	if evenements == nil {
		evenements = []string{}
	}
	return map[string]any{
		"id":                w.ID,
		"name":              w.Nom,
		"url":               w.URL,
		"events":            evenements,
		"secret":            w.Secret,
		"headers":           entetes,
		"active":            w.Actif,
		"created_at":        w.CreeLe,
		"last_triggered_at": w.DernierDecl,
		"last_status":       w.DernierStatut,
		"last_error":        w.DerniereErreur,
		"retry_policy":      w.PolitiqueRetrai,
		"max_retries":       w.RetraitsMax,
		"timeout_seconds":   w.DelaiSecondes,
	}
}

// ToJSON sérialise les webhooks comme `json.dump(..., indent=2)`.
func ToJSON(webhooks []WebhookConfig) ([]byte, error) {
	liste := make([]map[string]any, 0, len(webhooks))
	for _, webhook := range webhooks {
		liste = append(liste, webhook.Dict())
	}
	donnees, err := json.MarshalIndent(liste, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("webhooks: sérialisation: %w", err)
	}
	return donnees, nil
}

// DepuisJSON relit un `webhooks.json`. Un fichier absent, illisible ou
// partiellement rempli ne bloque jamais (même tolérance que `_load`).
func DepuisJSON(donnees []byte) []WebhookConfig {
	var bruts []map[string]any
	if err := json.Unmarshal(donnees, &bruts); err != nil {
		return nil
	}
	webhooks := make([]WebhookConfig, 0, len(bruts))
	for _, brut := range bruts {
		webhook := WebhookConfig{
			ID:              chaine(brut["id"]),
			Nom:             chaine(brut["name"]),
			URL:             chaine(brut["url"]),
			Evenements:      chaines(brut["events"]),
			Secret:          chaine(brut["secret"]),
			Entetes:         entetes(brut["headers"]),
			Actif:           booleen(brut["active"], true),
			CreeLe:          chaine(brut["created_at"]),
			DernierDecl:     chainePointeur(brut["last_triggered_at"]),
			DernierStatut:   entierPointeur(brut["last_status"]),
			DerniereErreur:  chainePointeur(brut["last_error"]),
			PolitiqueRetrai: chaine(brut["retry_policy"]),
			RetraitsMax:     entier(brut["max_retries"], 5),
			DelaiSecondes:   decimal(brut["timeout_seconds"], 10),
		}
		if webhook.PolitiqueRetrai == "" {
			webhook.PolitiqueRetrai = RetraitExponentiel
		}
		webhooks = append(webhooks, webhook)
	}
	return webhooks
}

// EvenementsTries retourne la liste triée des événements connus.
func EvenementsTries() []string {
	copie := append([]string{}, Evenements...)
	sort.Strings(copie)
	return copie
}

func chaine(valeur any) string {
	texte, _ := valeur.(string)
	return texte
}

func chainePointeur(valeur any) *string {
	if texte, ok := valeur.(string); ok {
		return &texte
	}
	return nil
}

func entierPointeur(valeur any) *int {
	if nombre, ok := valeur.(float64); ok {
		entier := int(nombre)
		return &entier
	}
	return nil
}

func entier(valeur any, defaut int) int {
	if nombre, ok := valeur.(float64); ok {
		return int(nombre)
	}
	return defaut
}

func decimal(valeur any, defaut float64) float64 {
	if nombre, ok := valeur.(float64); ok {
		return nombre
	}
	return defaut
}

func booleen(valeur any, defaut bool) bool {
	if drapeau, ok := valeur.(bool); ok {
		return drapeau
	}
	return defaut
}

func chaines(valeur any) []string {
	liste, ok := valeur.([]any)
	if !ok {
		return []string{}
	}
	resultat := make([]string, 0, len(liste))
	for _, item := range liste {
		if texte, ok := item.(string); ok {
			resultat = append(resultat, texte)
		}
	}
	return resultat
}

func entetes(valeur any) map[string]string {
	brut, ok := valeur.(map[string]any)
	if !ok {
		return map[string]string{}
	}
	resultat := make(map[string]string, len(brut))
	for cle, item := range brut {
		if texte, ok := item.(string); ok {
			resultat[cle] = texte
		}
	}
	return resultat
}
