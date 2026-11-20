package trustport

import (
	"context"
	"fmt"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// StepFactSource gives the target versions and the freeze permission of one write step. It is handed the sealed snapshot of the evaluation and the plan step, and nothing else of
// the request: a version token is never read from a request (INV-17). Its production implementation reads the versions the evidence was read at and the reconciliation result
// for the target (T9 to T11, other lanes).
type StepFactSource interface {
	StepFacts(ctx context.Context, snap SealedSnapshot, s intake.RecordStep) (RecordStepFacts, error)
}

// SealedFactsConfig builds a SealedFacts. PolicyID is the policy of the manifest-verified pack the composition loaded; it has no default.
type SealedFactsConfig struct {
	Snapshots SnapshotReader
	PolicyID  string
	Steps     StepFactSource
	Facts     ItemFacts
}

// SealedFacts implements RecordFactSource over the sealed snapshot of the evaluation (TRU-12; A377 items 6 and 7). The evidence set hash is the sealed snapshot's, which the
// database sealed and this code re-checks; the policy is the verified pack's and must be one the evaluation used; the step facts come from the StepFactSource for that snapshot.
// A RecordRequest has no field that could set any of them.
type SealedFacts struct {
	snaps SnapshotReader
	pol   string
	steps StepFactSource
}

var _ RecordFactSource = (*SealedFacts)(nil)

// NewSealedFacts builds the fact source. A missing reader, step source or policy is ErrConfig.
func NewSealedFacts(c SealedFactsConfig) (*SealedFacts, error) {
	if c.Snapshots == nil || c.Steps == nil || c.PolicyID == "" {
		return nil, ErrConfig
	}
	return &SealedFacts{snaps: c.Snapshots, pol: c.PolicyID, steps: c.Steps}, nil
}

// Facts reads the sealed snapshot the evaluation recorded in its basis, as the request's tenant, and refuses it unless it is that snapshot, intact, and of the evaluation's own
// correlation. It returns empty facts with an error, never partial ones.
func (f *SealedFacts) Facts(ctx context.Context, r intake.RecordRequest) (RecordFacts, error) {
	if _, ok := ctx.Deadline(); !ok {
		return RecordFacts{}, ErrNoDeadline
	}
	if r.TenantID == "" || r.Evaluated == nil {
		return RecordFacts{}, ErrRecordRequest
	}
	basis, err := r.Evaluated.Basis()
	if err != nil || basis.EvidenceSnapshotID == "" {
		return RecordFacts{}, fmt.Errorf("%w: the evaluation has no complete basis", ErrRecordRequest)
	}
	used := false
	for _, rule := range basis.PolicyRules {
		used = used || rule.PolicyID == f.pol
	}
	if !used {
		return RecordFacts{}, fmt.Errorf("%w: the evaluation did not use the pack's policy", ErrRecordRequest)
	}
	snap, err := f.snaps.Sealed(ctx, r.TenantID, basis.EvidenceSnapshotID)
	if err != nil {
		return RecordFacts{}, err
	}
	if _, err := intact(snap, r.TenantID, basis.EvidenceSnapshotID); err != nil {
		return RecordFacts{}, err
	}
	if snap.CorrelationID != r.CorrelationID {
		return RecordFacts{}, notIntact()
	}
	out := RecordFacts{PolicyID: f.pol, EvidenceSetHash: snap.EvidenceSetHash, Steps: make([]RecordStepFacts, 0, len(r.Steps))}
	for _, s := range r.Steps {
		sf, err := f.steps.StepFacts(ctx, snap, s)
		if err != nil {
			return RecordFacts{}, err
		}
		out.Steps = append(out.Steps, sf)
	}
	return out, nil
}
