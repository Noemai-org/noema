// Package admissionsport is the orchestrator's Admissions port over the Record-backed reader (STM-01; A161, A169).
//
// It asks the reader (record/admission) for the session's tenant and the turn's reference, and hands back what the Record holds; it decides nothing (the runner compares
// every field itself) and it never passes on an answer about another tenant (INV-12).
//
// May import: the standard library, orchestrator, record/admission. Nothing under evidence/ or trust/ imports this package (the three powers stay apart).
//
// Must never: write, take a tenant from anything but the session, rewrite or hide a reader's error, or carry content of an answer in an error message (AGENT-13).
package admissionsport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/record/admission"
)

var (
	// ErrNotUsable marks a call that cannot be honoured (no reader, no tenant, no reference); the reader was not asked.
	ErrNotUsable = errors.New("admissionsport: the lookup cannot be made")
	// ErrWrongTenant marks an answer about a tenant other than the session's; nothing of it is returned.
	ErrWrongTenant = errors.New("admissionsport: the Record answered for another tenant")
)

// Looker is the one method of record/admission.Reader this port uses.
type Looker interface {
	Lookup(ctx context.Context, tenantID, eventID string) (admission.Found, error)
}

// Port implements orchestrator.Admissions.
type Port struct{ Reader Looker }

var _ orchestrator.Admissions = Port{}

// Lookup finds the recorded event a turn's reference names, as the session's tenant.
func (p Port) Lookup(ctx context.Context, s orchestrator.Session, ref string) (orchestrator.Admission, error) {
	if p.Reader == nil || s.TenantID == "" || ref == "" {
		return orchestrator.Admission{}, ErrNotUsable
	}
	f, err := p.Reader.Lookup(ctx, s.TenantID, ref)
	if err != nil {
		return orchestrator.Admission{}, err
	}
	if f.TenantID != s.TenantID {
		return orchestrator.Admission{}, ErrWrongTenant
	}
	return orchestrator.Admission{EventType: f.EventType, TenantID: f.TenantID, PrincipalID: f.PrincipalID, CorrelationID: f.CorrelationID, RequestID: f.RequestID}, nil
}
