// Package ownershipport is a compile-only stub (red head). The implementation follows in the next commit.
package ownershipport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

var (
	ErrConfig       = errors.New("ownershipport: stub")
	ErrNoDeadline   = errors.New("ownershipport: stub no deadline")
	ErrSession      = errors.New("ownershipport: stub session")
	ErrTenant       = errors.New("ownershipport: stub tenant")
	ErrIntent       = errors.New("ownershipport: stub intent")
	ErrRead         = errors.New("ownershipport: stub read")
	ErrBadAnswer    = errors.New("ownershipport: stub answer")
	ErrInconclusive = errors.New("ownershipport: stub inconclusive")
)

type ScopedReader interface {
	Read(ctx context.Context, req contracts.CandidatesRequestV1) (contracts.CandidatesResponseV1, error)
}

type Node struct{ ResourceKind string }

type Config struct {
	TenantID string
	Reader   ScopedReader
	Nodes    map[string]Node
}

type Ownership struct{}

func New(Config) (Ownership, error) { return Ownership{}, nil }

func (Ownership) OwnsTarget(context.Context, string, string, intake.AdmittedIntent) (bool, error) {
	return false, nil
}
