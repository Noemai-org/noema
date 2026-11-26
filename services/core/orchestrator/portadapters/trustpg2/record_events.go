package trustpg2

import (
	"context"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

// RecordEvents is the store's RequestEvents over the Record (A710, A724; REQ-TRU-009; PERD 32.4: the Record first). Red stub: it appends nothing yet.
type RecordEvents struct {
	Appender *chain.Appender
}

// Received appends nothing yet.
func (RecordEvents) Received(context.Context, pgx.Tx, ReceivedEvent) error { return nil }

// Expired appends nothing yet.
func (RecordEvents) Expired(context.Context, pgx.Tx, ExpiredEvent) error { return nil }
