package intake

import (
	"context"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// The ports of T10 (reconciliation), T11 (policy) and T12 (clocks). Each is defined here, by its consumer, so Trust imports no execution, evidence or
// understanding package (INV-01, SoT 51); the composition root supplies the adapters. A port answers with data. It never builds a decision: Trust does.

// Reconciliation is what T10 needs of the reconciliation of the sealed snapshot's target (REQ-RES-001, REQ-RES-002).
type Reconciliation struct {
	// Classification is the derived classification (PERD 22.3). Empty is a defect and fails closed.
	Classification reconcile.Classification
	// Frozen reports that the target carries a contradiction freeze.
	Frozen bool
	// AllowedUnderFreeze is the capability IDs the pack allows on a frozen target (the descriptor's allowed_under_freeze): pack data.
	AllowedUnderFreeze []string
	// EscalateReason is the pack reason code the pack maps the classification to when it maps it to ESCALATE (a contradiction, an unknown outcome),
	// in the form `<vertical>.reason.<name>`; empty when the pack maps it to nothing.
	EscalateReason string
}

// Reconciler reconciles the sealed evidence snapshot of a tenant. The snapshot is the one T9 sealed; the adapter reads it and applies the pack's rule set.
type Reconciler interface {
	Reconcile(ctx context.Context, tenantID, snapshotID string) (Reconciliation, error)
}

// PolicyQuery is one policy evaluation: who, what, over which sealed evidence, at which clock reading (Unix seconds). The adapter finds the intent's
// admitted_at (AT_ADMISSION rules, INV-09) from the intent store itself.
type PolicyQuery struct {
	TenantID      string
	IntentID      string
	IntentVersion int
	IntentNode    string
	CapabilityID  string // empty when the intent names no capability
	SnapshotID    string
	Now           int64
}

// PolicyEvaluation is the result of the tenant's active policy set for one request (PERD 23.4).
type PolicyEvaluation struct {
	// Effect is the winning rule's effect, or REJECT when no rule matched (policy.Result.Effect).
	Effect policy.Effect
	// ReasonCode is the winning rule's reason code, core or pack; empty for none.
	ReasonCode string
	// Rules are the rules that decided, each with its policy and version (INV-09): what BasisBuilder.RecordPolicy takes.
	Rules []decide.PolicyRuleVersion
	// Versions are the policy versions used (REQ-TRU-007).
	Versions []decide.PolicyVersionRef
	// PermitMatched reports that some PERMIT rule matched: a CONFIRM stands only with one (INV-03).
	PermitMatched bool
	// ReevaluateAt is the time a WAIT is re-evaluated, Unix seconds, computed by the adapter from the rule's wait parameter; zero for any other effect.
	ReevaluateAt int64
}

// Policies evaluates the tenant's active policy set. A condition that cannot be evaluated is an error, never a result (policy.ErrCondition).
type Policies interface {
	Evaluate(ctx context.Context, q PolicyQuery) (PolicyEvaluation, error)
}

// ClockQuery names the request a clock question is about.
type ClockQuery struct {
	TenantID   string
	IntentNode string
	SnapshotID string
}

// ClockInstance is one clock that applies and the time of its anchor event; Anchor is nil when the event has not happened (NOT_STARTED, never assumed).
type ClockInstance struct {
	Clock  contracts.ClockV1
	Anchor *int64
}

// ClockSet is the clocks that apply and the at-risk fraction. AtRisk comes from the pack (ruling A205 Q1, provisional pack data): Trust holds no number.
type ClockSet struct {
	Clocks []ClockInstance
	AtRisk clocks.Ratio
}

// ClockRouting is what the pack's routing rules make of the clock statuses. Priority and routing parameters belong to the escalation case, not to the
// decision: Trust learns only whether the pack says to escalate, and with which pack reason code.
type ClockRouting struct {
	Escalate   bool
	ReasonCode string // the pack reason code, `<vertical>.reason.<name>`; needed when Escalate
}

// Clocks answers T12: the clocks that apply, and the pack's routing of their statuses.
type Clocks interface {
	Applicable(ctx context.Context, q ClockQuery) (ClockSet, error)
	Route(ctx context.Context, q ClockQuery, statuses []clocks.Status) (ClockRouting, error)
}
