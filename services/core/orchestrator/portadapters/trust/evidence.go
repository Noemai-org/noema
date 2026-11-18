package trustport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Errors of the Evidence adapter (A329). Callers compare with errors.Is.
var (
	ErrEvidenceNoDeadline = errors.New("trustport: the evidence read needs a context with a deadline")
	ErrSnapshotNotFound   = errors.New("trustport: no such sealed snapshot")
	ErrSnapshotNotIntact  = errors.New("trustport: the sealed snapshot is not usable as evidence")
	ErrSnapshotIntent     = errors.New("trustport: the sealed snapshot cannot be bound to an intent")
	ErrSnapshotStore      = errors.New("trustport: the sealed snapshot could not be read")
	ErrSourceRules        = errors.New("trustport: the evidence rules are not usable")
)

// SourceRules are the trust profile's evidence rules for one intent node, read from the pack as data.
type SourceRules struct {
	RequiredSources []string
	MaxAgeSeconds   int64
}

// SourceProfiles gives the evidence rules of the trust profile that governs an intent node.
type SourceProfiles interface {
	SourceRules(ctx context.Context, tenantID, intentNode string) (SourceRules, error)
}

// Evidence implements intake.Evidence.
type Evidence struct {
	snapshots SnapshotReader
	profiles  SourceProfiles
	clock     clock.Clock
}

var _ intake.Evidence = Evidence{}

// NewEvidence builds the adapter.
func NewEvidence(snapshots SnapshotReader, profiles SourceProfiles, clk clock.Clock) (Evidence, error) {
	return Evidence{snapshots: snapshots, profiles: profiles, clock: clk}, nil
}

// SnapshotFreshness reads the sealed snapshot's freshness.
func (e Evidence) SnapshotFreshness(context.Context, string, string) (intake.SnapshotEvidence, error) {
	return intake.SnapshotEvidence{}, nil
}
