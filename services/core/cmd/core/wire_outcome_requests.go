package main

import (
	"context"
	"errors"

	verifierport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/verifier"
	"github.com/Noemai-org/noema/services/core/outcome/request"
	outcomeverifier "github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// wire_outcome_requests.go: the write path's Requests, composed per tenant (LANE-5 slice 4; A675, A692, interim, relayed; REQ-VER-001, REQ-SEC-004; INV-04, INV-12, AGENT-13).
// buildOutcomeRequests gives each tenant a request.GrantRequests over store.Reader on that tenant's own outcome login, and a router that sends a grant to its session's tenant only.
// The signature check, the pack's outcome definitions and the id source are seams other lanes own: a missing one is a named gap and builds nothing.

// ErrNoOutcomeForTenant is what the router returns for a tenant it holds no reader for, or a call without a tenant. Nothing was read.
var ErrNoOutcomeForTenant = errors.New("cmd/core: the Outcome request source has no reader for this tenant")

// outcomeRequestSeams are the parts other lanes own. A nil seam is a named gap, never a stand-in.
type outcomeRequestSeams struct {
	Signatures request.Signatures
	Outcomes   request.Outcomes
	IDs        request.IDs
}

// outcomeRequests routes by tenant.
type outcomeRequests struct {
	byTenant map[string]request.GrantRequests
}

var _ verifierport.Requests = (*outcomeRequests)(nil)

// newOutcomeRequests builds the router over one Reader for each tenant.
func newOutcomeRequests(readers map[string]request.Reader, s outcomeRequestSeams) *outcomeRequests {
	return nil
}

// buildOutcomeRequests builds the Requests over the outcome pools of slice 1's parts, or returns nil and the gap lines.
func buildOutcomeRequests(parts *executionParts, s outcomeRequestSeams) (verifierport.Requests, []string) {
	return nil, nil
}

// For implements verifierport.Requests.
func (o *outcomeRequests) For(ctx context.Context, tenantID, grantID string) (outcomeverifier.Request, error) {
	return outcomeverifier.Request{}, errors.New("cmd/core: not built")
}
