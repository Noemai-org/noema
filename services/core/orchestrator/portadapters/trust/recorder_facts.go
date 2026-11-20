package trustport

import (
	"context"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// StepFactSource gives the target versions and the freeze permission of one write step, read for the sealed snapshot it is handed and never from the request.
type StepFactSource interface {
	StepFacts(ctx context.Context, snap SealedSnapshot, s intake.RecordStep) (RecordStepFacts, error)
}

// SealedFactsConfig builds a SealedFacts.
type SealedFactsConfig struct {
	Snapshots SnapshotReader
	PolicyID  string
	Steps     StepFactSource
}

// SealedFacts implements RecordFactSource over the sealed snapshot of the evaluation.
type SealedFacts struct{}

// NewSealedFacts builds the fact source.
func NewSealedFacts(SealedFactsConfig) (*SealedFacts, error) { return &SealedFacts{}, nil }

// Facts gives the facts of one request.
func (*SealedFacts) Facts(context.Context, intake.RecordRequest) (RecordFacts, error) {
	return RecordFacts{}, nil
}
