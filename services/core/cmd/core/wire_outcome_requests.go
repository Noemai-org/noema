package main

import (
	"context"
	"errors"

	verifierport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/verifier"
	"github.com/Noemai-org/noema/services/core/outcome/request"
	"github.com/Noemai-org/noema/services/core/outcome/store"
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

// newOutcomeRequests builds the router over one Reader for each tenant. A missing seam or an empty set gives nil.
func newOutcomeRequests(readers map[string]request.Reader, s outcomeRequestSeams) *outcomeRequests {
	if len(readers) == 0 || s.Signatures == nil || s.Outcomes == nil || s.IDs == nil {
		return nil
	}
	by := make(map[string]request.GrantRequests, len(readers))
	for tenant, rd := range readers {
		if tenant == "" || rd == nil {
			return nil
		}
		by[tenant] = request.GrantRequests{Reader: rd, Signatures: s.Signatures, Outcomes: s.Outcomes, IDs: s.IDs}
	}
	return &outcomeRequests{byTenant: by}
}

// buildOutcomeRequests builds the Requests over the outcome pools of slice 1's parts, or returns nil and the gap lines. Each gap is fixed text and carries no value.
func buildOutcomeRequests(parts *executionParts, s outcomeRequestSeams) (verifierport.Requests, []string) {
	if parts == nil {
		return nil, []string{"outcome requests: execution not configured"}
	}
	var gaps []string
	for _, c := range []struct {
		missing bool
		gap     string
	}{
		{s.Signatures == nil, "outcome requests: signature check missing"},
		{s.Outcomes == nil, "outcome requests: pack outcomes missing"},
		{s.IDs == nil, "outcome requests: verification ids missing"},
	} {
		if c.missing {
			gaps = append(gaps, c.gap)
		}
	}
	if len(gaps) > 0 {
		return nil, gaps
	}
	readers := make(map[string]request.Reader, len(parts.OutcomePools))
	for tenant, pool := range parts.OutcomePools {
		readers[tenant] = store.Reader{Pool: pool}
	}
	o := newOutcomeRequests(readers, s)
	if o == nil {
		return nil, []string{"outcome requests: no tenant has an outcome login"}
	}
	return o, nil
}

// For implements verifierport.Requests: the grant is read through the reader of the tenant named, and no other (INV-12). A nil router, an empty tenant or an unknown one is refused before any read.
func (o *outcomeRequests) For(ctx context.Context, tenantID, grantID string) (outcomeverifier.Request, error) {
	if o == nil || tenantID == "" {
		return outcomeverifier.Request{}, ErrNoOutcomeForTenant
	}
	src, ok := o.byTenant[tenantID]
	if !ok {
		return outcomeverifier.Request{}, ErrNoOutcomeForTenant
	}
	return src.For(ctx, tenantID, grantID)
}
