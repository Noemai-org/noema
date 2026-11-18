package intakeadapters

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// ClockBook is the clocks the pack declares and the intent nodes they apply to: a node's trust profile names its clocks, the pack's `clocks` define them.
type ClockBook struct {
	byNode map[string][]intake.ClockInstance // intent node to the clocks that apply to it (Anchor unset); a node with none is present with an empty list
}

// LoadClockBook loads the clocks of a pack file.
func LoadClockBook(pack []byte) (*ClockBook, error) { return nil, ErrNotBuilt }

// Anchors gives the time of a clock's anchor event for a sealed snapshot, in Unix seconds; happened is false when the event has not happened (NOT_STARTED, never assumed).
type Anchors interface {
	AnchorTime(ctx context.Context, tenantID, snapshotID, anchorEvent string) (at int64, happened bool, err error)
}

// ClocksConfig is what NewClocks needs.
type ClocksConfig struct {
	Book    *ClockBook
	Anchors Anchors
	// AtRisk is the at-risk fraction from the pack (ruling A205 Q1, provisional pack data); it is refused when unusable and never defaulted.
	AtRisk clocks.Ratio
	// Routes is the pack's routing of a clock phase to an escalation: phase to pack reason code. A phase not in it routes nowhere.
	Routes map[clocks.Phase]string
}

// Clocks implements intake.Clocks.
type Clocks struct{ c ClocksConfig }

var _ intake.Clocks = (*Clocks)(nil)

// NewClocks builds the adapter.
func NewClocks(c ClocksConfig) (*Clocks, error) { return nil, ErrNotBuilt }

// Applicable answers the clocks that apply to the intent node and the time of each anchor.
func (c *Clocks) Applicable(ctx context.Context, q intake.ClockQuery) (intake.ClockSet, error) {
	return intake.ClockSet{}, ErrNotBuilt
}

// Route answers whether the pack's routing escalates for the statuses.
func (c *Clocks) Route(ctx context.Context, q intake.ClockQuery, statuses []clocks.Status) (intake.ClockRouting, error) {
	return intake.ClockRouting{}, ErrNotBuilt
}
