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
//
// It also reads the intent version's logical resolution epoch (migration 0033, ruling A345 S1: 1 at creation): the same row, so the slot's facts and the epoch are one view.
const intentSQL = `SELECT json::text, logical_resolution_epoch::bigint FROM admitted_intent WHERE tenant_id = $1 AND intent_id = $2 AND version = $3`

const (
	// statusSuffix marks a variable that is the status of a slot: `<slot>.status`.
	statusSuffix = ".status"
	// resourceSlot is the slot type that carries a resolved reference (intent-slot.v2 SlotType).
	resourceSlot = "Resource"
	// epochVariable is the variable that is the intent's logical resolution epoch (PERD 27.2): it is not a slot's.
	epochVariable = "logical_resolution_epoch"
)

// slotName is the form of a slot name (common.v1 SlotName).
var slotName = regexp.MustCompile(`\A[a-z][a-z0-9_]*\z`)

// slotStatuses are the statuses a slot can have (intent-slot.v2 SlotStatus), as Trust names them.
var slotStatuses = []decide.SlotStatus{decide.SlotResolved, decide.SlotUnresolved, decide.SlotAmbiguous, decide.SlotAbsent}

// Facts implements intakeadapters.FactSource. It gives only the facts it can read, each read and none assumed. From the stored intent of the query: a variable `<slot>` is the resolved
// reference of the intent's slot of that name, a variable `<slot>.status` is that slot's status, and the variable `logical_resolution_epoch` is the epoch stored with the intent
// version. From the sealed snapshot of the query (only with WithLists): the list entities the lists serve, each read from the snapshot's item that holds the list, as the tenant (INV-19:
// a list that cannot be known is an error and never an empty list). A variable no slot carries, and every entity the lists do not serve, has no source here and is refused by name.
// A variable is sourced from the slot of the same name, so the reader names no slot and no industry (INV-14).
type Facts struct {
	r     reader
	lists *EvidenceLists
}

var _ intakeadapters.FactSource = (*Facts)(nil)

// FactsOption changes how NewFacts builds the reader.
type FactsOption func(*Facts)

// WithLists gives the reader the sealed snapshot's lists, so that it serves the list entities they name. Without it every entity is refused by name. The lists are checked by NewFacts.
func WithLists(l EvidenceLists) FactsOption {
	return func(f *Facts) {
		own := l.own()
		f.lists = &own
	}
}

// NewFacts builds the reader. A missing login source or a timeout below one is ErrConfig, and so are lists that cannot read a list safely. The timeout comes from configuration
// (NOEMA_TRUST_DB_TIMEOUT_MS).
func NewFacts(logins Logins, timeout time.Duration, opts ...FactsOption) (*Facts, error) {
	r, err := newReader(logins, timeout)
	if err != nil {
		return nil, err
	}
	f := &Facts{r: r}
	for _, opt := range opts {
		opt(f)
	}
	if f.lists != nil {
		if err := f.lists.valid(); err != nil {
			return nil, err
		}
	}
	return f, nil
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
	epoch          bool // the variable is the intent's logical resolution epoch, not a slot's
}

// asksOf names the slot of each variable; a variable that cannot name a slot has no source.
func asksOf(variables []string) ([]ask, error) {
	out := make([]ask, 0, len(variables))
	for _, name := range variables {
		if name == epochVariable {
			out = append(out, ask{variable: name, epoch: true})
			continue
		}
		slot, status := strings.CutSuffix(name, statusSuffix)
		if !slotName.MatchString(slot) {
			return nil, fmt.Errorf("%w: the variable %s", ErrNoSource, name)
		}
		out = append(out, ask{variable: name, slot: slot, status: status})
	}
	return out, nil
}

// PolicyFacts gives the variables and entities of the need. A variable is read from the stored intent version of the query, as the tenant; an entity from the query's sealed snapshot
// (see Facts). An entity the lists do not serve is ErrNoSource, with nothing read. A variable no slot of the intent carries is ErrNoSource. A slot that cannot give it (no status, a
// status outside the contract, a reference on a slot that is not RESOLVED or none on one that is, an intent node other than the query's) is ErrIntent. The reference of a slot that is
// not RESOLVED is refused, not given as null: the rules that read it cannot be evaluated over a subject nobody resolved. An epoch below one is ErrBadAnswer. There is never a half
// answer: on any error the set is empty. A need of nothing reads nothing.
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
	if err := f.served(need.Entities); err != nil {
		return intakeadapters.FactSet{}, err
	}
	if len(need.Entities) > 0 {
		if err := checkSnapshot(q.TenantID, q.SnapshotID); err != nil {
			return intakeadapters.FactSet{}, err
		}
	}
	asks, err := asksOf(need.Variables)
	if err != nil {
		return intakeadapters.FactSet{}, err
	}
	out := intakeadapters.FactSet{Variables: make(map[string]predicate.Value, len(asks)), Entities: map[string][]predicate.Row{}}
	if len(asks) > 0 {
		if out.Variables, err = f.variables(ctx, q, asks); err != nil {
			return intakeadapters.FactSet{}, err
		}
	}
	if len(need.Entities) > 0 {
		if out.Entities, err = f.entities(ctx, q, need.Entities); err != nil {
			return intakeadapters.FactSet{}, err
		}
	}
	return out, nil
}

// served refuses, by name, an entity that the lists do not serve; with no lists every entity is refused. Nothing is read.
func (f *Facts) served(entities []string) error {
	for _, name := range entities {
		if f.lists == nil {
			return fmt.Errorf("%w: the entity %s", ErrNoSource, name)
		}
		if _, ok := f.lists.Entities[name]; !ok {
			return fmt.Errorf("%w: the entity %s", ErrNoSource, name)
		}
	}
	return nil
}

// entities reads the list of each entity from the sealed snapshot of the query, as the tenant. The snapshot's items are read once, so the lists are of one view.
func (f *Facts) entities(ctx context.Context, q intake.PolicyQuery, names []string) (map[string][]predicate.Row, error) {
	items, err := f.lists.Items.Items(ctx, q.TenantID, q.SnapshotID)
	if err != nil {
		return nil, err
	}
	out := make(map[string][]predicate.Row, len(names))
	for _, name := range names {
		if out[name], err = f.lists.rows(ctx, q.TenantID, items, name); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// variables reads the stored intent version of the query as the tenant and gives each variable asked.
func (f *Facts) variables(ctx context.Context, q intake.PolicyQuery, asks []ask) (map[string]predicate.Value, error) {
	var (
		doc   string
		epoch int64
	)
	if err := f.r.scan(ctx, q.TenantID, intentSQL, []any{q.TenantID, q.IntentID, q.IntentVersion}, &doc, &epoch); err != nil {
		return nil, err
	}
	var intent storedIntent
	if json.Unmarshal([]byte(doc), &intent) != nil || intent.IntentNode != q.IntentNode {
		return nil, fmt.Errorf("%w: the document is not the intent node of the query", ErrIntent)
	}
	out := make(map[string]predicate.Value, len(asks))
	for _, a := range asks {
		if a.epoch {
			if epoch < 1 {
				return nil, fmt.Errorf("%w: the logical resolution epoch", ErrBadAnswer)
			}
			out[a.variable] = predicate.Int(epoch)
			continue
		}
		raw, present := intent.Slots[a.slot]
		if !present {
			return nil, fmt.Errorf("%w: the variable %s, no slot of the intent carries it", ErrNoSource, a.variable)
		}
		v, err := slotFact(a, raw)
		if err != nil {
			return nil, err
		}
		out[a.variable] = v
	}
	return out, nil
}

// slotFact reads one fact from one slot. The slot must be as the contract says: a known status, a Resource slot RESOLVED exactly when it has a resolved reference, and a
// reference that is not empty. A status that is missing is refused when the status is asked, and a slot with no status is never RESOLVED.
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
	if s.Type == resourceSlot && resolved != (s.ResolvedRef != nil) {
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
