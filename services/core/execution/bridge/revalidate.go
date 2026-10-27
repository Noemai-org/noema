package bridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// Facts a failed revalidation names (the descriptors' revalidation_reads, PERD REQ-EXE-003).
const (
	FactCaseStatus  = "case_status"
	FactCaseVersion = "case_version"
	FactOpenCases   = "open_cases"
)

// CaseRevalidator is E7 for mock-case: it re-reads exactly what the descriptors of the two capabilities name,
// fresh, and compares it with what the grant was issued against. Any read that cannot be made is an error, which
// the controller turns into a void grant (REVALIDATION_UNAVAILABLE); an unreadable answer is never a pass.
//
// Not read here: `subject_resource.status` of create_escalation_case. It is a read of the bank ledger, which is
// Tushe-dependent work the Founder paused on 25 Oct 2026; the revalidation of that fact is added with the ledger
// adapter.
type CaseRevalidator struct {
	Case adapters.Case
}

var _ controller.Revalidator = CaseRevalidator{}

// Revalidate implements controller.Revalidator.
func (r CaseRevalidator) Revalidate(ctx context.Context, g controller.Grant, step controller.PlanStep) (controller.Revalidation, error) {
	switch step.CapabilityID {
	case CapRouteToInternalOwner:
		return r.route(ctx, g)
	case CapCreateEscalationCase:
		return r.create(ctx, g, step)
	}
	return controller.Revalidation{}, fmt.Errorf("bridge: no revalidation for capability %q", step.CapabilityID)
}

// route: the case still exists, is open or routed, and holds the version token the grant was issued against.
func (r CaseRevalidator) route(ctx context.Context, g controller.Grant) (controller.Revalidation, error) {
	if len(g.TargetVersions) != 1 {
		return controller.Revalidation{}, errors.New("bridge: a conditional write needs exactly one expected version")
	}
	c, err := r.Case.GetCase(ctx, g.TenantID, g.TargetRef)
	if err != nil {
		return controller.Revalidation{}, err
	}
	var changed []string
	if c.Status != "OPEN" && c.Status != "ROUTED" {
		changed = append(changed, FactCaseStatus)
	}
	if c.VersionToken != g.TargetVersions[0].VersionToken {
		changed = append(changed, FactCaseVersion)
	}
	return controller.Revalidation{Passed: len(changed) == 0, ChangedFacts: changed}, nil
}

// create: no open or routed case exists for this principal and transaction in this resolution epoch (the
// case-dedupe rule of the graph's precondition). A case an earlier attempt of the same action may have created
// therefore fails the revalidation: the answer to an unknown outcome is to verify by key, not to write again.
// An incomplete page proves nothing, so it is an error and not a pass.
func (r CaseRevalidator) create(ctx context.Context, g controller.Grant, step controller.PlanStep) (controller.Revalidation, error) {
	txRef, ok := strParam(step.Params, paramTransactionRef)
	if !ok {
		return controller.Revalidation{}, errors.New("bridge: the plan step has no transaction_ref")
	}
	page, err := r.Case.FindCases(ctx, adapters.CaseQuery{TenantID: g.TenantID, PrincipalID: g.PrincipalID, TransactionRef: txRef})
	if err != nil {
		return controller.Revalidation{}, err
	}
	if !page.Complete {
		return controller.Revalidation{}, errors.New("bridge: the case list is incomplete, so absence cannot be concluded")
	}
	for _, c := range page.Items {
		if (c.Status == "OPEN" || c.Status == "ROUTED") && c.Epoch == epochOf(step.Params) {
			return controller.Revalidation{Passed: false, ChangedFacts: []string{FactOpenCases}}, nil
		}
	}
	return controller.Revalidation{Passed: true}, nil
}

func epochOf(p map[string]any) int64 {
	e, _ := p[paramEpoch].(int64)
	return e
}
