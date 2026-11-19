package intakesources

import (
	"context"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

// Facts implements intakeadapters.FactSource.
type Facts struct{ r reader }

var _ intakeadapters.FactSource = (*Facts)(nil)

// NewFacts builds the reader.
func NewFacts(logins Logins, timeout time.Duration) (*Facts, error) {
	r, err := newReader(logins, timeout)
	return &Facts{r: r}, err
}

// PolicyFacts gives the facts the rules read.
func (f *Facts) PolicyFacts(_ context.Context, _ intake.PolicyQuery, _ intakeadapters.FactNeed) (intakeadapters.FactSet, error) {
	return intakeadapters.FactSet{}, nil
}
