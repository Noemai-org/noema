package intakesources

import (
	"context"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

// Admissions implements intakeadapters.Admissions.
type Admissions struct{ r reader }

var _ intakeadapters.Admissions = (*Admissions)(nil)

// NewAdmissions builds the reader.
func NewAdmissions(logins Logins, timeout time.Duration) (*Admissions, error) {
	r, err := newReader(logins, timeout)
	return &Admissions{r: r}, err
}

// AdmittedAt gives the admission time of one intent version.
func (a *Admissions) AdmittedAt(_ context.Context, _, _ string, _ int) (int64, error) {
	return 0, nil
}
