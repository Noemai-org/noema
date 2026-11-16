package recorder

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Confirmation is an accepted confirmation that resolved to a fresh evaluation (trust/confirm: PROCEED_TO_FRESH_EVALUATION) (stub).
type Confirmation struct {
	TenantID    string
	EventID     string
	RequestID   string
	BindingHash string
	AcceptedAt  time.Time
	ExpiresAt   time.Time
}

// RecordConfirmation records c in tx (stub).
func RecordConfirmation(ctx context.Context, tx pgx.Tx, c Confirmation) error { return nil }
