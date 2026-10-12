// Package ad implémente le pattern universel Validate → Simulate → Execute → Audit
// pour toute opération d'écriture AD.
//
// Le mode simulation est OBLIGATOIRE : aucune écriture AD ne peut contourner
// la prévisualisation. L'UI appelle Simulate, affiche le résultat, puis
// appelle Execute uniquement sur validation utilisateur.
package ad

import (
	"context"
	"errors"
	"fmt"
)

// OperationType identifie le type d'opération pour l'audit.
type OperationType string

const (
	OpCreateUser      OperationType = "create_user"
	OpEnableUser      OperationType = "enable_user"
	OpDisableUser     OperationType = "disable_user"
	OpMoveUser        OperationType = "move_user"
	OpSetPassword     OperationType = "set_password"
	OpResetPassword   OperationType = "reset_password"
	OpAddToGroup      OperationType = "add_to_group"
	OpRemoveFromGroup OperationType = "remove_from_group"
	OpDeleteUser      OperationType = "delete_user"
	OpCreateGroup     OperationType = "create_group"
	OpDeleteGroup     OperationType = "delete_group"
	OpCreateOU        OperationType = "create_ou"
	OpDeleteOU        OperationType = "delete_ou"
	OpMigration       OperationType = "migration"
)

// Simulator définit l'interface de simulation pour toute opération.
type Simulator interface {
	Validate() error
	Simulate(ctx context.Context) (*SimulateResult, error)
	Execute(ctx context.Context) error
	OperationType() OperationType
	TargetIdentifier() string // pour l'audit (DN, sAMAccountName, etc.)
}

// SimulationError est une erreur de simulation (ex: conflit, prérequis manquant).
type SimulationError struct {
	Message string
}

func (e *SimulationError) Error() string {
	return e.Message
}

// NewSimulationError crée une erreur de simulation.
func NewSimulationError(msg string) *SimulationError {
	return &SimulationError{Message: msg}
}

// IsSimulationError teste si une erreur est une erreur de simulation.
func IsSimulationError(err error) bool {
	var se *SimulationError
	return errors.As(err, &se)
}

// Executor encapsule le cycle complet Validate → Simulate → Execute → Audit.
type Executor struct {
	Pool   *Pool
	Audit  AuditLogger
	User   string // opérateur
	Domain string
}

// AuditLogger est l'interface minimale pour journaliser.
type AuditLogger interface {
	LogEntry(entry AuditEntry) error
}

// AuditEntry représente une entrée de journal (subset de internal/audit).
type AuditEntry struct {
	Timestamp     string
	ActionType    string
	Compte        string
	OUSource      string
	OUDestination string
	Resultat      string
	SessionID     string
	Simulation    bool
	Detail        string
	Utilisateur   string
	Domaine       string
}

// Run exécute le cycle complet.
func (e *Executor) Run(ctx context.Context, sim Simulator) error {
	// 1. Validate
	if err := sim.Validate(); err != nil {
		return err
	}

	// 2. Simulate
	result, err := sim.Simulate(ctx)
	if err != nil {
		return err
	}
	if !result.WouldSucceed {
		return NewSimulationError(result.Error)
	}

	// 3. Execute
	if err := sim.Execute(ctx); err != nil {
		// Log l'échec d'exécution
		if e.Audit != nil {
			_ = e.Audit.LogEntry(AuditEntry{
				ActionType:  string(sim.OperationType()),
				Compte:      sim.TargetIdentifier(),
				Resultat:    "echec",
				Detail:      fmt.Sprintf("Execute échoué: %v", err),
				Utilisateur: e.User,
				Domaine:     e.Domain,
			})
		}
		return err
	}

	// 4. Audit (succès)
	if e.Audit != nil {
		_ = e.Audit.LogEntry(AuditEntry{
			ActionType:  string(sim.OperationType()),
			Compte:      sim.TargetIdentifier(),
			Resultat:    "succes",
			Detail:      result.Changes[len(result.Changes)-1], // dernière action
			Utilisateur: e.User,
			Domaine:     e.Domain,
		})
	}

	return nil
}

// RunWithSimulatedResult permet à l'UI d'afficher la simulation, puis
// d'appeler Execute séparément (pattern utilisé par HTMX).
//
// Usage UI :
//  1. POST /api/simulate → retourne SimulateResult
//  2. User confirme
//  3. POST /api/execute → appelle Execute seul
func (e *Executor) RunWithSimulatedResult(ctx context.Context, sim Simulator, alreadySimulated bool, result *SimulateResult) error {
	if !alreadySimulated {
		// 1. Validate
		if err := sim.Validate(); err != nil {
			return err
		}
		// 2. Simulate
		r, err := sim.Simulate(ctx)
		if err != nil {
			return err
		}
		if !r.WouldSucceed {
			return NewSimulationError(r.Error)
		}
		*result = *r
		return nil // UI affiche result, attend confirmation
	}

	// 3. Execute (déjà simulé)
	if err := sim.Execute(ctx); err != nil {
		if e.Audit != nil {
			_ = e.Audit.LogEntry(AuditEntry{
				ActionType:  string(sim.OperationType()),
				Compte:      sim.TargetIdentifier(),
				Resultat:    "echec",
				Detail:      fmt.Sprintf("Execute échoué: %v", err),
				Utilisateur: e.User,
				Domaine:     e.Domain,
			})
		}
		return err
	}

	// 4. Audit
	if e.Audit != nil {
		_ = e.Audit.LogEntry(AuditEntry{
			ActionType:  string(sim.OperationType()),
			Compte:      sim.TargetIdentifier(),
			Resultat:    "succes",
			Detail:      result.Changes[len(result.Changes)-1],
			Utilisateur: e.User,
			Domaine:     e.Domain,
		})
	}

	return nil
}
