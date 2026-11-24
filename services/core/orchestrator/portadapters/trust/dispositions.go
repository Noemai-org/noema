package trustport

import (
	"context"
	"errors"
)

// ErrNoDisposition is returned by a Dispositions reader for a correlation that has no ESCALATE disposition decision in the tenant's Record: the same answer for a correlation that is
// absent and for one of another tenant. It is not "none needed": the caller that needs the disposition fails closed on it.
var ErrNoDisposition = errors.New("trustport: no ESCALATE disposition is recorded for this correlation")

// Dispositions finds, by correlation, the ESCALATE disposition decision the Record holds (A539, interim, relayed, not yet committed; REQ-TRU-010, INV-30). It is the recovery path of the
// escalation: when the workflow's input no longer carries the disposition, it is re-read from the Record and never from the workflow's history. The tenant is the verified session's,
// never a request's (INV-12). Implementations are read-only.
type Dispositions interface {
	DispositionFor(ctx context.Context, tenantID, correlationID string) (decisionID string, err error)
}
