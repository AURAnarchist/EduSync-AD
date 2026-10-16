// Package progress fournit le suivi de progression et l'annulation pour les
// opérations de masse (import, migration, réinitialisation MDP, départs).
//
// Chaque opération longue expose un contexte annulable : l'UI peut demander
// l'arrêt à tout moment et le suivi de progression est consultable en temps
// réel via HTTP (polling HTMX).
package progress

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// Statut décrit l'état d'une opération.
type Statut string

const (
	StatutEnCours Statut = "en_cours"
	StatutTermine Statut = "termine"
	StatutAnnule  Statut = "annule"
	StatutEchec   Statut = "echec"
)

// Operation décrit une opération de masse en cours ou terminée.
type Operation struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"` // import, migration, password, departs
	Statut    Statut    `json:"statut"`
	Total     int       `json:"total"`
	Actuel    int       `json:"actuel"`
	Pourcent  int       `json:"pourcent"`
	Message   string    `json:"message"`
	DemarreLe time.Time `json:"demarre_le"`
	TermineLe time.Time `json:"termine_le,omitempty"`
	Erreur    string    `json:"erreur,omitempty"`
}

// Tracker suit l'ensemble des opérations de masse. Thread-safe.
type Tracker struct {
	mu          sync.Mutex
	operations  map[string]*Operation
	nextID      int
	annulations map[string]context.CancelFunc // fonctions d'annulation par ID
}

// NewTracker crée un tracker vide.
func NewTracker() *Tracker {
	return &Tracker{
		operations:  make(map[string]*Operation),
		annulations: make(map[string]context.CancelFunc),
	}
}

// NouvelID enregistre une nouvelle opération et retourne son identifiant
// unique ainsi qu'un contexte annulable lié à celle-ci.
func (t *Tracker) NouvelID(typeOp string) (string, context.Context) {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.nextID++
	id := fmt.Sprintf("op-%d", t.nextID)

	ctx, cancel := context.WithCancel(context.Background())
	t.annulations[id] = cancel

	t.operations[id] = &Operation{
		ID:        id,
		Type:      typeOp,
		Statut:    StatutEnCours,
		DemarreLe: time.Now(),
	}

	return id, ctx
}

// Annuler demande l'annulation d'une opération. Retourne faux si l'ID est
// inconnu ou si l'opération est déjà terminée.
func (t *Tracker) Annuler(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	op, ok := t.operations[id]
	if !ok || op.Statut != StatutEnCours {
		return false
	}

	if cancel, ok := t.annulations[id]; ok {
		cancel()
		delete(t.annulations, id)
	}
	op.Statut = StatutAnnule
	op.TermineLe = time.Now()
	return true
}

// MiseAJour fait progresser une opération d'un pas. Retourne faux si l'ID est
// inconnu ou si l'opération n'est plus en cours.
func (t *Tracker) MiseAJour(id string, actuel int, message string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	op, ok := t.operations[id]
	if !ok || op.Statut != StatutEnCours {
		return false
	}

	op.Actuel = actuel
	if op.Total > 0 {
		op.Pourcent = actuel * 100 / op.Total
	}
	if message != "" {
		op.Message = message
	}
	return true
}

// Terminer marque une opération comme terminée (succès ou échec).
func (t *Tracker) Terminer(id string, succes bool, message, erreur string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	op, ok := t.operations[id]
	if !ok {
		return
	}

	if succes {
		op.Statut = StatutTermine
	} else {
		op.Statut = StatutEchec
	}
	op.Message = message
	op.Erreur = erreur
	op.TermineLe = time.Now()
	if cancel, ok := t.annulations[id]; ok {
		cancel()
		delete(t.annulations, id)
	}
}

// Get retourne l'opération correspondant à l'ID, ou nil.
func (t *Tracker) Get(id string) *Operation {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.operations[id]
}

// Liste retourne toutes les opérations (récentes d'abord).
func (t *Tracker) Liste() []*Operation {
	t.mu.Lock()
	defer t.mu.Unlock()

	liste := make([]*Operation, 0, len(t.operations))
	for _, op := range t.operations {
		liste = append(liste, op)
	}

	// Tri : les plus récentes d'abord.
	for i := 0; i < len(liste); i++ {
		for j := i + 1; j < len(liste); j++ {
			if liste[j].DemarreLe.After(liste[i].DemarreLe) {
				liste[i], liste[j] = liste[j], liste[i]
			}
		}
	}
	return liste
}

// Annulable indique si le contexte de l'opération a été annulé.
func Annulable(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

var _ = fmt.Sprintf // utilisé pour l'identifiant d'opération
