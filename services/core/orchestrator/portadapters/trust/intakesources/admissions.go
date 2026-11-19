package intakesources

import (
	"context"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

// admissionSQL is the admission time of one intent version: the row's creation time in whole Unix seconds, dropped and never rounded up, as policystore.Published drops a
// publication time, so that the two are compared on one scale. The row and the Record event INTENT_ADMITTED are written in one transaction, the event first (intentstore.Save,
// EC-E16), so the row's time is the time of that admission. The Record itself is not readable by a Trust login (no power touches the Record tables, migration 0001).
const admissionSQL = `SELECT floor(extract(epoch FROM created_at))::bigint FROM admitted_intent WHERE tenant_id = $1 AND intent_id = $2 AND version = $3`

// Admissions implements intakeadapters.Admissions over the admitted-intent store (migration 0026), read as the tenant's own Trust login.
type Admissions struct{ r reader }

var _ intakeadapters.Admissions = (*Admissions)(nil)

// NewAdmissions builds the reader. A missing login source or a timeout below one is ErrConfig. The timeout comes from configuration (NOEMA_TRUST_DB_TIMEOUT_MS).
func NewAdmissions(logins Logins, timeout time.Duration) (*Admissions, error) {
	r, err := newReader(logins, timeout)
	if err != nil {
		return nil, err
	}
	return &Admissions{r: r}, nil
}

// AdmittedAt gives the admission time of the version, in Unix seconds, the one the policy version in force at admission is chosen by (PERD 23.4, INV-09). The version is the one
// asked for, not the latest. A version that does not exist, or that another tenant owns, is ErrNotFound; a time before one second is ErrBadAnswer and never a time.
func (a *Admissions) AdmittedAt(ctx context.Context, tenantID, intentID string, version int) (int64, error) {
	if a == nil {
		return 0, ErrConfig
	}
	if err := checkKey(tenantID, intentID, version); err != nil {
		return 0, err
	}
	var at int64
	if err := a.r.scan(ctx, tenantID, admissionSQL, []any{tenantID, intentID, version}, &at); err != nil {
		return 0, err
	}
	if at < 1 {
		return 0, ErrBadAnswer
	}
	return at, nil
}
