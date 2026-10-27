package bridge

import (
	"context"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// Capability IDs of the fintech pack that act on mock-case (packs/fintech_v1.json). They are data in the pack; they
// are named here only because this bridge is the adapter side of exactly these two descriptors.
const (
	CapCreateEscalationCase = "fin.cap.create_escalation_case"
	CapRouteToInternalOwner = "fin.cap.route_to_internal_owner"
)

// Parameter names of the two capabilities (the `input_schema` of their descriptors).
const (
	paramTransactionRef  = "transaction_ref"
	paramReasonCode      = "reason_code"
	paramCreatedByIntent = "created_by_intent"
	paramEpoch           = "epoch"
	paramOwnerQueue      = "owner_queue"
	paramPriority        = "priority"
)

// CaseDispatcher is the Dispatcher of E9 for mock-case. It makes exactly one adapter call per Dispatch and never
// retries (REQ-EXE-006): the answer is reported as a transport class and Execution classifies it (REQ-EXE-007).
// A capability it has no mapping for, a parameter it does not know, and a conditional write sent without an
// expected version are adapter defects, never calls: there is no callable path for them (REQ-CAP-002, REQ-EXE-004).
type CaseDispatcher struct {
	Case adapters.Case
}

var _ controller.Dispatcher = CaseDispatcher{}

func defect(raw string) controller.TransportResult {
	return controller.TransportResult{Class: controller.TransportDefect, RawCode: raw}
}

// Dispatch implements controller.Dispatcher.
func (d CaseDispatcher) Dispatch(ctx context.Context, req controller.DispatchRequest) controller.TransportResult {
	timeout := time.Duration(req.TimeoutMS) * time.Millisecond
	switch req.CapabilityID {
	case CapCreateEscalationCase:
		in, raw := createInput(req, timeout)
		if raw != "" {
			return defect(raw)
		}
		return fromWrite(d.Case.CreateCase(ctx, in))
	case CapRouteToInternalOwner:
		in, raw := routeInput(req, timeout)
		if raw != "" {
			return defect(raw)
		}
		return fromWrite(d.Case.RouteCase(ctx, in))
	}
	return defect("NO_ADAPTER_FOR_CAPABILITY")
}

func fromWrite(w adapters.WriteResult) controller.TransportResult {
	return controller.TransportResult{Class: controller.TransportClass(w.Class), ExternalAck: w.ExternalAck, RawCode: w.RawCode, KeyCollision: w.KeyCollision}
}

func strParam(p map[string]any, name string) (string, bool) {
	v, ok := p[name].(string)
	return v, ok && v != ""
}

// unknownParam returns the first parameter name that is not in allowed ("" when there is none).
func unknownParam(p map[string]any, allowed ...string) string {
	for k := range p {
		known := false
		for _, a := range allowed {
			if k == a {
				known = true
			}
		}
		if !known {
			return k
		}
	}
	return ""
}

func createInput(req controller.DispatchRequest, timeout time.Duration) (adapters.CreateCaseInput, string) {
	p := req.Params
	if k := unknownParam(p, paramTransactionRef, paramReasonCode, paramCreatedByIntent, paramEpoch, paramOwnerQueue, paramPriority); k != "" {
		return adapters.CreateCaseInput{}, "UNKNOWN_PARAMETER"
	}
	txRef, ok1 := strParam(p, paramTransactionRef)
	reason, ok2 := strParam(p, paramReasonCode)
	intent, ok3 := strParam(p, paramCreatedByIntent)
	epoch, ok4 := p[paramEpoch].(int64)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return adapters.CreateCaseInput{}, "MISSING_PARAMETER"
	}
	queue, _ := p[paramOwnerQueue].(string)
	priority, _ := p[paramPriority].(string)
	return adapters.CreateCaseInput{IdempotencyKey: req.IdempotencyKey, TenantID: req.TenantID, PrincipalID: req.PrincipalID, TransactionRef: txRef,
		ReasonCode: reason, CreatedByIntent: intent, Epoch: epoch, OwnerQueue: queue, Priority: priority, Timeout: timeout}, ""
}

func routeInput(req controller.DispatchRequest, timeout time.Duration) (adapters.RouteCaseInput, string) {
	p := req.Params
	if k := unknownParam(p, paramOwnerQueue); k != "" {
		return adapters.RouteCaseInput{}, "UNKNOWN_PARAMETER"
	}
	queue, ok := strParam(p, paramOwnerQueue)
	if !ok || req.TargetRef == "" {
		return adapters.RouteCaseInput{}, "MISSING_PARAMETER"
	}
	if len(req.ExpectedFrom) != 1 || req.ExpectedFrom[0].VersionToken == "" {
		return adapters.RouteCaseInput{}, "CONDITIONAL_WRITE_WITHOUT_EXPECTED_VERSION"
	}
	return adapters.RouteCaseInput{TenantID: req.TenantID, CaseID: req.TargetRef, OwnerQueue: queue, ExpectedVersionToken: req.ExpectedFrom[0].VersionToken, Timeout: timeout}, ""
}
