package intakesources

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

// intentSQL reads the document of one admitted intent version, as the admission gate stored it.
const intentSQL = `SELECT json::text FROM admitted_intent WHERE tenant_id = $1 AND intent_id = $2 AND version = $3`

const (
	// statusSuffix marks a variable that is the status of a slot: `<slot>.status`.
	statusSuffix = ".status"
	// resourceSlot is the slot type that carries a resolved reference (intent-slot.v2 SlotType).
	resourceSlot = "Resource"
)

// slotName is the form of a slot name (common.v1 SlotName).
var slotName = regexp.MustCompile(`\A[a-z][a-z0-9_]*\z`)

// slotStatuses are the statuses a slot can have (intent-slot.v2 SlotStatus), as Trust names them.
var slotStatuses = []decide.SlotStatus{decide.SlotResolved, decide.SlotUnresolved, decide.SlotAmbiguous, decide.SlotAbsent}

// Facts implements intakeadapters.FactSource. It gives only the facts that the stored intent of the query carries, each read and none assumed: a variable `<slot>` is the resolved
// reference of the intent's slot of that name, and a variable `<slot>.status` is that slot's status. A variable is sourced from the slot of the same name, so the reader names no
// slot and no industry (INV-14); a variable that no slot carries, and every evidence entity, has no source here and is refused by name (INV-19). It reads nothing of the evidence
// snapshot: the facts it gives do not depend on it.
type Facts struct{ r reader }

var _ intakeadapters.FactSource = (*Facts)(nil)

// NewFacts builds the reader. A missing login source or a timeout below one is ErrConfig. The timeout comes from configuration (NOEMA_TRUST_DB_TIMEOUT_MS).
func NewFacts(logins Logins, timeout time.Duration) (*Facts, error) {
	r, err := newReader(logins, timeout)
	if err != nil {
		return nil, err
	}
	return &Facts{r: r}, nil
}

// storedIntent is the two members of the admitted document this reader needs, so that it does not depend on the object's version.
type storedIntent struct {
	IntentNode string                     `json:"intent_node"`
	Slots      map[string]json.RawMessage `json:"slots"`
}

type storedSlot struct {
	Type        string  `json:"type"`
	Status      *string `json:"status"`
	ResolvedRef *string `json:"resolved_ref"`
}

// ask is one variable the rules read and the slot it is sourced from.
type ask struct {
	variable, slot string
	status         bool // the variable is the slot's status and not its reference
}

// asksOf names the slot of each variable; a variable that cannot name a slot has no source.
func asksOf(variables []string) ([]ask, error) {
	out := make([]ask, 0, len(variables))
	for _, name := range variables {
		slot, status := strings.CutSuffix(name, statusSuffix)
		if !slotName.MatchString(slot) {
			return nil, fmt.Errorf("%w: the variable %s", ErrNoSource, name)
		}
		out = append(out, ask{variable: name, slot: slot, status: status})
	}
	return out, nil
}

// PolicyFacts gives the variables of the need from the stored intent version of the query, read as the tenant. An entity in the need has no source and is ErrNoSource, with
// nothing read. A variable no slot of the intent carries is ErrNoSource. A slot that cannot give it (no status, a status outside the contract, a reference on a slot that is not
// RESOLVED or none on one that is, an intent node other than the query's) is ErrIntent. The reference of a slot that is not RESOLVED is refused, not given as null: the rules that
// read it cannot be evaluated over a subject nobody resolved. There is never a half answer: on any error the set is empty. A need of nothing reads nothing.
func (f *Facts) PolicyFacts(ctx context.Context, q intake.PolicyQuery, need intakeadapters.FactNeed) (intakeadapters.FactSet, error) {
	if f == nil {
		return intakeadapters.FactSet{}, ErrConfig
	}
	if err := checkKey(q.TenantID, q.IntentID, q.IntentVersion); err != nil {
		return intakeadapters.FactSet{}, err
	}
	if q.IntentNode == "" {
		return intakeadapters.FactSet{}, fmt.Errorf("%w: an intent node is required", ErrQuery)
	}
	if len(need.Entities) > 0 {
		return intakeadapters.FactSet{}, fmt.Errorf("%w: the entity %s", ErrNoSource, need.Entities[0])
	}
	asks, err := asksOf(need.Variables)
	if err != nil {
		return intakeadapters.FactSet{}, err
	}
	out := intakeadapters.FactSet{Variables: make(map[string]predicate.Value, len(asks)), Entities: map[string][]predicate.Row{}}
	if len(asks) == 0 {
		return out, nil
	}
	var doc string
	if err := f.r.scan(ctx, q.TenantID, intentSQL, []any{q.TenantID, q.IntentID, q.IntentVersion}, &doc); err != nil {
		return intakeadapters.FactSet{}, err
	}
	var intent storedIntent
	if json.Unmarshal([]byte(doc), &intent) != nil || intent.IntentNode != q.IntentNode {
		return intakeadapters.FactSet{}, fmt.Errorf("%w: the document is not the intent node of the query", ErrIntent)
	}
	for _, a := range asks {
		raw, present := intent.Slots[a.slot]
		if !present {
			return intakeadapters.FactSet{}, fmt.Errorf("%w: the variable %s, no slot of the intent carries it", ErrNoSource, a.variable)
		}
		v, err := slotFact(a, raw)
		if err != nil {
			return intakeadapters.FactSet{}, err
		}
		out.Variables[a.variable] = v
	}
	return out, nil
}

// slotFact reads one fact from one slot. The slot must be as the contract says: a known status, a Resource slot RESOLVED exactly when it has a resolved reference, and a
// reference that is not empty.
func slotFact(a ask, raw json.RawMessage) (predicate.Value, error) {
	refuse := func(why string) error { return fmt.Errorf("%w: the variable %s, %s", ErrIntent, a.variable, why) }
	var s storedSlot
	if json.Unmarshal(raw, &s) != nil {
		return predicate.Value{}, refuse("the slot is not a slot")
	}
	if s.Status != nil && !slices.Contains(slotStatuses, decide.SlotStatus(*s.Status)) {
		return predicate.Value{}, refuse("the slot's status is not one of the contract's")
	}
	if s.ResolvedRef != nil && *s.ResolvedRef == "" {
		return predicate.Value{}, refuse("the slot's reference is empty")
	}
	resolved := s.Status != nil && decide.SlotStatus(*s.Status) == decide.SlotResolved
	if s.Type == resourceSlot && (s.Status == nil || resolved != (s.ResolvedRef != nil)) {
		return predicate.Value{}, refuse("the slot is RESOLVED only with a resolved reference")
	}
	if a.status {
		if s.Status == nil {
			return predicate.Value{}, refuse("the slot has no status")
		}
		return predicate.Enum(*s.Status), nil
	}
	if s.Type != resourceSlot || !resolved {
		return predicate.Value{}, refuse("the slot has no resolved reference")
	}
	return predicate.Ref(*s.ResolvedRef), nil
}
