// Package plangraphs implements trustport.PlanGraphs: it reads the pack's action graph for an intent node as data and fills each step's params from the admitted
// intent's resolved slots and the pack's own declared values, and from nothing else (INV-02, AGENT-09). Packs are data, never imported as code (SoT §51).
package plangraphs

import (
	"context"
	"errors"
	"fmt"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// ParamKind says where a step param comes from. It is a closed set of six; a pack that declares any other kind is refused (the contract of noema#765, the Founder's text of
// 19 Nov 2026, interim, relayed, not yet committed).
type ParamKind string

const (
	// KindSlot takes the value of a RESOLVED slot of the admitted intent.
	KindSlot ParamKind = "slot"
	// KindConst is a constant the pack declares.
	KindConst ParamKind = "const"
	// KindMap looks up the RESOLVED value of slot Slot in Map; a value not in the map is refused, never passed through.
	KindMap ParamKind = "map"
	// KindFact is the run-time fact named Ref, supplied by the PlanContext; absent, the step is not plannable now.
	KindFact ParamKind = "fact"
	// KindPolicyParam is the winning PERMIT rule's param named Ref for the step's capability, supplied by the PlanContext; absent, the step is not plannable now.
	KindPolicyParam ParamKind = "policy_param"
	// KindEvidencePacket is the sealed evidence packet reference of the PlanContext, an opaque value; absent, the step is not plannable now.
	KindEvidencePacket ParamKind = "evidence_packet"
)

// SlotKind is the type of a slot (A440, interim, relayed, not yet committed): a slot param declares the kind it expects, and the planner refuses a resolved value of another kind.
type SlotKind string

// The six slot kinds. For SlotKindResource the declaration and the value also carry a resource kind.
const (
	SlotKindResource  SlotKind = "Resource"
	SlotKindQuantity  SlotKind = "Quantity"
	SlotKindParty     SlotKind = "Party"
	SlotKindTimeRef   SlotKind = "TimeRef"
	SlotKindReference SlotKind = "Reference"
	SlotKindEnum      SlotKind = "Enum"
)

// ParamSpec is the pack's declaration of one step param. Which fields apply depends on Kind: slot uses Slot; const uses Value; map uses Slot and Map; fact and policy_param
// use Ref; evidence_packet uses none.
type ParamSpec struct {
	Name  string
	Kind  ParamKind
	Slot  string
	Value any
	Map   map[string]string
	Ref   string
	// SlotKind is the kind the slot param (kind slot or map) expects of the slot's value (A440). It is required there, and is named SlotKind because Kind is the param's source.
	SlotKind SlotKind
	// ResourceKind is the expected resource kind; required when SlotKind is SlotKindResource.
	ResourceKind string
}

// PlanContext is what the plan needs besides the admitted intent: the capabilities to plan now, and the run-time values the params draw on. The intake's PlanRequest carries the
// same fields (a parallel lane, TRU-14 continuation); the mapping is Runnable <- the runnable capability ids (noema#782 runnable.Runnable), PolicyParams <- the winning PERMIT
// rule's params per capability, Facts <- the verified run-time facts (case_id), EvidencePacket <- the sealed evidence packet reference. Nothing in it is free text.
type PlanContext struct {
	Runnable       []string
	PolicyParams   map[string]map[string]string
	Facts          map[string]string
	EvidencePacket string
}

// PlanContexts supplies the PlanContext of a request. The composition implements it over the PlanRequest fields.
type PlanContexts interface {
	PlanContext(ctx context.Context, r intake.PlanRequest) (PlanContext, error)
}

// PackStep is one step of the pack's action graph for an intent node, as data.
type PackStep struct {
	NodeID         string
	CapabilityID   string
	TargetRef      string // optional: a target the pack pins; it must equal the admitted intent's, and is never the source of the step's target
	Params         []ParamSpec
	Preconditions  []string
	Postconditions []string
}

// SlotDecl is the node's declaration of one slot: the single authority of the slot's kind and resource kind (A454, interim, relayed, not yet committed). ResourceKind is set
// for SlotKindResource only.
type SlotDecl struct {
	Kind         SlotKind
	ResourceKind string
}

// PackGraph is the action graph the pack declares for one intent node.
type PackGraph struct {
	IntentNode   string
	GraphVersion string
	// Slots is the node's slot declarations by slot name; a slot param's slot must be declared here.
	Slots map[string]SlotDecl
	Steps []PackStep
}

// ErrNoGraph is what a Pack returns for an intent node it has no graph for.
var ErrNoGraph = errors.New("plangraphs: the pack has no action graph for the intent node")

// Pack reads the loaded pack's action graph. The composition fills it from the pack on disk; nothing here imports a pack.
type Pack interface {
	ActionGraph(ctx context.Context, intentNode string) (PackGraph, error)
}

// SlotValue is one slot of a stored, admitted intent.
type SlotValue struct {
	Resolved bool
	Value    any
	// Kind is the value's own kind (A440); a value with no kind is refused by a slot param. ResourceKind is set for SlotKindResource.
	Kind         SlotKind
	ResourceKind string
}

// IntentSlots is what the plan needs of a stored, admitted intent: its identity, its target and its slots. The adapter reading the intent store resolves TargetRef from the
// stored intent, never from a request or a param.
type IntentSlots struct {
	TenantID   string
	IntentID   string
	Version    int
	IntentNode string
	IntentHash string
	TargetRef  string
	Slots      map[string]SlotValue
}

// Intents reads the slots of a stored, admitted intent as the tenant.
type Intents interface {
	Slots(ctx context.Context, tenantID, intentID string, version int) (IntentSlots, error)
}

// The refusals. Each wraps trustport.ErrPlanGraphInvalid, which the Planner maps to intake.ErrPlanInvalid.
var (
	ErrParamSource    = fmt.Errorf("%w: a param has a source that is not one of the six kinds", trustport.ErrPlanGraphInvalid)
	ErrUnresolvedSlot = fmt.Errorf("%w: a param names a slot that is absent or not resolved", trustport.ErrPlanGraphInvalid)
	ErrTargetMismatch = fmt.Errorf("%w: a step target is not the admitted intent's target", trustport.ErrPlanGraphInvalid)
	ErrWrongIntent    = fmt.Errorf("%w: the graph or the stored intent is not the requested intent's", trustport.ErrPlanGraphInvalid)
	ErrMapValue       = fmt.Errorf("%w: a mapped slot's value is not in the pack's map", trustport.ErrPlanGraphInvalid)
	// ErrNotPlannable is a step whose run-time fact, policy param or evidence packet is absent now. It is a typed refusal, never a guess.
	ErrNotPlannable = fmt.Errorf("%w: a run-time fact, policy param or evidence packet the step needs is absent", trustport.ErrPlanGraphInvalid)
	// ErrSlotKind is a slot param with no valid kind, or a resolved value whose own kind (or resource kind) is not the declared one (A440). It is refused at planning, by name.
	ErrSlotKind = fmt.Errorf("%w: a slot param's declared kind is missing or is not the resolved value's kind", trustport.ErrPlanGraphInvalid)
	// ErrFreeText is a string from a slot that carries a digit run over four (AGENT-13) or exceeds the length bound.
	ErrFreeText   = fmt.Errorf("%w: a slot text has a digit run over four or is longer than the bound", trustport.ErrPlanGraphInvalid)
	ErrNoRunnable = fmt.Errorf("%w: no runnable step, or a runnable capability the graph does not have", trustport.ErrPlanGraphInvalid)
)

// Adapter implements trustport.PlanGraphs.
type Adapter struct {
	Pack     Pack
	Intents  Intents
	Contexts PlanContexts
	// MaxTextBytes bounds every string that comes from a slot (the Security Reviewer's condition on noema#762). It has NO default: zero or negative is trustport.ErrConfig.
	MaxTextBytes int
}

var _ trustport.PlanGraphs = Adapter{}
