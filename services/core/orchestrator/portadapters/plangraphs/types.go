// Package plangraphs implements trustport.PlanGraphs: it reads the pack's action graph for an intent node as data and fills each step's params from the admitted
// intent's resolved slots and the pack's own declared values, and from nothing else (INV-02, AGENT-09). Packs are data, never imported as code (SoT §51).
package plangraphs

import (
	"context"
	"errors"
	"fmt"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
)

// ParamSource says where a step param comes from. It is a closed set: a pack that declares any other source is refused.
type ParamSource string

const (
	// SourceSlot takes the value of a RESOLVED slot of the admitted intent.
	SourceSlot ParamSource = "SLOT"
	// SourceConst is a constant the pack declares.
	SourceConst ParamSource = "CONST"
	// SourceDefault takes the slot's resolved value, or the pack's declared default when the intent has no such slot. A slot that is present and not resolved is refused.
	SourceDefault ParamSource = "DEFAULT"
)

// ParamSpec is the pack's declaration of one step param.
type ParamSpec struct {
	Name   string
	Source ParamSource
	Slot   string // SourceSlot and SourceDefault
	Value  any    // SourceConst: the constant; SourceDefault: the default
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

// PackGraph is the action graph the pack declares for one intent node.
type PackGraph struct {
	IntentNode   string
	GraphVersion string
	Steps        []PackStep
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
	ErrParamSource    = fmt.Errorf("%w: a param has a source that is neither a resolved slot nor a pack value", trustport.ErrPlanGraphInvalid)
	ErrUnresolvedSlot = fmt.Errorf("%w: a param names a slot that is absent or not resolved", trustport.ErrPlanGraphInvalid)
	ErrTargetMismatch = fmt.Errorf("%w: a step target is not the admitted intent's target", trustport.ErrPlanGraphInvalid)
	ErrWrongIntent    = fmt.Errorf("%w: the graph or the stored intent is not the requested intent's", trustport.ErrPlanGraphInvalid)
)

// Adapter implements trustport.PlanGraphs.
type Adapter struct {
	Pack    Pack
	Intents Intents
}

var _ trustport.PlanGraphs = Adapter{}
