package webhooks

import "fmt"

// ErreurWebhooks est une erreur métier affichable (message déjà en français).
type ErreurWebhooks struct{ Message string }

// Error implémente error.
func (e *ErreurWebhooks) Error() string { return e.Message }

// nouvelleErreurWebhooks construit une erreur métier.
func nouvelleErreurWebhooks(format string, args ...any) *ErreurWebhooks {
	return &ErreurWebhooks{Message: fmt.Sprintf(format, args...)}
}
