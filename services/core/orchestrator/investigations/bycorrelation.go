package investigations

import (
	"context"
	"errors"
)

// ErrAmbiguousInvestigation: more than one investigation of the tenant carries the correlation. The unique keys of the table (the investigation and the recorded input) do not
// make a correlation unique, so a by-correlation read that finds two refuses and names neither (A625, interim, relayed, not yet committed).
var ErrAmbiguousInvestigation = errors.New(errPrefix + "more than one investigation carries this correlation")

// SessionByCorrelation is not built yet.
func (s *Store) SessionByCorrelation(ctx context.Context, tenantID, correlationID string) (RecordedSession, error) {
	return RecordedSession{}, errors.New(errPrefix + "SessionByCorrelation is not built")
}
