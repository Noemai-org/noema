package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/plangraphs"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

// compose_planner.go is the Planner of the G1 final composition (STM-01; A684 step 2, A550, A551, A454, A440, A543, A330, interim, relayed, not yet committed; REQ-STM-001, REQ-TRU-009;
// INV-02, INV-14, INV-16; AGENT-09, AGENT-13). It builds trustport.PlannerAdapter from three parts that are all data of the verified pack and the stored intent: the pack's node plans
// (trustpack.NodePlans) with every slot kind copied from the NODE's slot declaration (A454: the node is the single authority), the stored intent's typed slots, and the plan contexts
// the request carries (planContextsFromRequest). It reads no file; its one setting, NOEMA_PLAN_TEXT_MAX_BYTES (A705), is read by wireTrustInputs. Without it the Planner stays
// a named gap that lacks it and nothing else (A330, A469).

const (
	// EnvPlanTextMaxBytes is the bound of every string a slot gives the Planner (A705): required, no code default.
	EnvPlanTextMaxBytes = "NOEMA_PLAN_TEXT_MAX_BYTES"

	needPlanTextBound   = "the Planner's slot-text bound (" + EnvPlanTextMaxBytes + ", required, no default)"
	needAdmittedIntents = "the admitted-intent store adapter (Intents)"
	needSlotDocuments   = "the stored intent document reader (the tenant's Trust login and its read bound)"
)

// slotDocuments reads the stored document of an admitted intent, in canonical form, as the tenant.
type slotDocuments interface {
	Document(ctx context.Context, tenantID, intentID string, version int) (intentstore.Admitted, error)
}

// plannerInputs are what the Planner is built from. Each one that is absent is named in the gaps; MaxTextBytes has no default (A330): zero is the gap and a negative value is fatal.
type plannerInputs struct {
	Pack         *trustpack.Pack
	Intents      intake.Intents
	Docs         slotDocuments
	MaxTextBytes int
}

// buildPlanner returns the Planner, or the names of what it lacks and no Planner. A negative bound, and a pack that cannot give its node plans or its registry, are errors that name
// no value (A469).
func buildPlanner(in plannerInputs) (intake.Planner, []string, error) {
	if in.MaxTextBytes < 0 {
		return nil, nil, errors.New("cmd/core: the Planner's slot-text bound is not a positive number")
	}
	var missing []string
	for _, need := range []struct {
		absent bool
		text   string
	}{{in.Pack == nil, needPack}, {absent(in.Intents), needAdmittedIntents}, {absent(in.Docs), needSlotDocuments}, {in.MaxTextBytes == 0, needPlanTextBound}} {
		if need.absent {
			missing = append(missing, need.text)
		}
	}
	if len(missing) > 0 {
		return nil, missing, nil
	}
	plans, err := in.Pack.NodePlans()
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: the Planner: the pack's node plans: %w", err)
	}
	reg, err := in.Pack.Registry()
	if err != nil {
		return nil, nil, fmt.Errorf("cmd/core: the Planner: the capability registry: %w", err)
	}
	graphs := plangraphs.Adapter{Pack: packPlanGraphs{plans: plans}, Intents: storedSlots{Intents: in.Intents, Docs: in.Docs}, Contexts: planContextsFromRequest{}, MaxTextBytes: in.MaxTextBytes}
	return trustport.PlannerAdapter{Registry: reg, Graphs: graphs}, nil, nil
}

// packPlanGraphs is plangraphs.Pack over the pack's node plans.
type packPlanGraphs struct{ plans map[string]trustpack.NodePlan }

var _ plangraphs.Pack = packPlanGraphs{}

// ActionGraph is the node's graph as the planner reads it, a copy. A node with no plan is plangraphs.ErrNoGraph. Every slot or map param takes its kind and resource kind from the
// node's own slot declaration (A454); a param that has no source, or two, keeps no kind, and a constant that is not a string, an integer or a boolean keeps no value, so the planner
// refuses each by name and nothing is completed here (INV-16).
func (g packPlanGraphs) ActionGraph(ctx context.Context, intentNode string) (plangraphs.PackGraph, error) {
	if err := ctx.Err(); err != nil {
		return plangraphs.PackGraph{}, err
	}
	np, ok := g.plans[intentNode]
	if !ok {
		return plangraphs.PackGraph{}, plangraphs.ErrNoGraph
	}
	out := plangraphs.PackGraph{IntentNode: intentNode, GraphVersion: np.GraphVersion, Slots: make(map[string]plangraphs.SlotDecl, len(np.Slots))}
	for _, s := range np.Slots {
		d := plangraphs.SlotDecl{Kind: plangraphs.SlotKind(s.Type), ResourceKind: s.ResourceKind}
		if _, dup := out.Slots[s.Name]; dup {
			d = plangraphs.SlotDecl{} // a slot declared twice has no declaration to trust: every param that names it is refused
		}
		out.Slots[s.Name] = d
	}
	for _, s := range np.Steps {
		step := plangraphs.PackStep{NodeID: s.NodeID, CapabilityID: s.CapabilityID, Preconditions: append([]string{}, s.Preconditions...), Postconditions: append([]string{}, s.Postconditions...)}
		for _, q := range s.Params {
			step.Params = append(step.Params, planParam(q, out.Slots))
		}
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

func planParam(q trustpack.PlanParam, decls map[string]plangraphs.SlotDecl) plangraphs.ParamSpec {
	spec := plangraphs.ParamSpec{Name: q.Name}
	sources := 0
	if q.Slot != "" {
		sources++
		spec.Kind, spec.Slot = plangraphs.KindSlot, q.Slot
	}
	if q.ConstJSON != "" {
		sources++
		spec.Kind, spec.Value = plangraphs.KindConst, constValue(q.ConstJSON)
	}
	if q.HasMap {
		sources++
		spec.Kind, spec.Slot, spec.Map = plangraphs.KindMap, q.MapFrom, make(map[string]string, len(q.MapValues))
		for k, v := range q.MapValues {
			spec.Map[k] = v
		}
	}
	if q.Fact != "" {
		sources++
		spec.Kind, spec.Ref = plangraphs.KindFact, q.Fact
	}
	if q.PolicyParam != "" {
		sources++
		spec.Kind, spec.Ref = plangraphs.KindPolicyParam, q.PolicyParam
	}
	if q.EvidencePacket {
		sources++
		spec.Kind = plangraphs.KindEvidencePacket
	}
	if sources != 1 {
		return plangraphs.ParamSpec{Name: q.Name}
	}
	if spec.Kind == plangraphs.KindSlot || spec.Kind == plangraphs.KindMap {
		if d, ok := decls[spec.Slot]; ok {
			spec.SlotKind, spec.ResourceKind = d.Kind, d.ResourceKind
		}
	}
	return spec
}

// constValue is a pack constant as plain data: a string, an integer (never a float: money is integer minor units) or a boolean. Anything else is nil, which the planner refuses.
func constValue(raw string) any {
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	var v any
	if dec.Decode(&v) != nil || dec.More() {
		return nil
	}
	switch x := v.(type) {
	case string, bool:
		return x
	case json.Number:
		if n, err := strconv.ParseInt(x.String(), 10, 64); err == nil {
			return n
		}
	}
	return nil
}

// storedSlots is plangraphs.Intents over the admitted intent (identity, hash and target as the Intents adapter resolved them) and its stored document (the typed slots).
type storedSlots struct {
	Intents intake.Intents
	Docs    slotDocuments
}

var _ plangraphs.Intents = storedSlots{}

var errSlotsNotConfigured = errors.New("cmd/core: the Planner's slot reader is not configured")

var planResourceRefRe = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$`)

// Slots reads the admitted intent, then its stored document, and holds the document to the intent: the same row identity, the same hash, and a hash that is the document's own
// (intentstore.Verify). A document that is not the admitted intent's gives no slots. Only a RESOLVED Resource (its `resolved_ref`, whose kind is the prefix of the reference) and
// a RESOLVED Reference (its `value`) are given as values. A Quantity, a Party and a TimeRef are never offered as resolved: no ruling states how they become a plan param, and a
// Quantity's value without its unit would lose the currency (safer reading).
func (s storedSlots) Slots(ctx context.Context, tenantID, intentID string, version int) (plangraphs.IntentSlots, error) {
	if absent(s.Intents) || absent(s.Docs) {
		return plangraphs.IntentSlots{}, errSlotsNotConfigured
	}
	a, err := s.Intents.Get(ctx, tenantID, intentID, version)
	if err != nil {
		return plangraphs.IntentSlots{}, fmt.Errorf("cmd/core: the Planner reads the admitted intent: %w", err)
	}
	doc, err := s.Docs.Document(ctx, tenantID, intentID, version)
	if err != nil {
		return plangraphs.IntentSlots{}, fmt.Errorf("cmd/core: the Planner reads the stored document: %w", err)
	}
	if doc.TenantID != a.TenantID || doc.IntentID != a.IntentID || doc.Version != a.Version || doc.IntentNode != a.IntentNode || doc.IntentHash != a.IntentHash {
		return plangraphs.IntentSlots{}, errors.New("cmd/core: the stored document is not the admitted intent's")
	}
	if err := intentstore.Verify(doc); err != nil {
		return plangraphs.IntentSlots{}, fmt.Errorf("cmd/core: the stored document does not verify: %w", err)
	}
	var head struct {
		Slots map[string]json.RawMessage `json:"slots"`
	}
	if err := json.Unmarshal(doc.JSON, &head); err != nil {
		return plangraphs.IntentSlots{}, errors.New("cmd/core: the stored document has no readable slots")
	}
	out := plangraphs.IntentSlots{TenantID: a.TenantID, IntentID: a.IntentID, Version: a.Version, IntentNode: a.IntentNode, IntentHash: a.IntentHash, TargetRef: a.TargetRef,
		Slots: make(map[string]plangraphs.SlotValue, len(head.Slots))}
	for name, raw := range head.Slots {
		out.Slots[name] = slotValue(raw)
	}
	return out, nil
}

// slotValue types one stored slot. Anything it cannot read as a resolved Resource or Reference is not resolved.
func slotValue(raw json.RawMessage) plangraphs.SlotValue {
	var h struct {
		Type        string          `json:"type"`
		Status      *string         `json:"status"`
		ResolvedRef string          `json:"resolved_ref"`
		Value       json.RawMessage `json:"value"`
	}
	if json.Unmarshal(raw, &h) != nil {
		return plangraphs.SlotValue{}
	}
	v := plangraphs.SlotValue{Kind: plangraphs.SlotKind(h.Type)}
	resolved := h.Status != nil && *h.Status == "RESOLVED" || h.Status == nil && h.Type == string(plangraphs.SlotKindReference)
	switch plangraphs.SlotKind(h.Type) {
	case plangraphs.SlotKindResource:
		if resolved && planResourceRefRe.MatchString(h.ResolvedRef) {
			v.Resolved, v.Value, v.ResourceKind = true, h.ResolvedRef, h.ResolvedRef[:strings.IndexByte(h.ResolvedRef, ':')]
		}
	case plangraphs.SlotKindReference:
		var text string
		if resolved && json.Unmarshal(h.Value, &text) == nil && text != "" {
			v.Resolved, v.Value = true, text
		}
	}
	return v
}

// poolDocuments is slotDocuments over the tenant's own Trust login.
type poolDocuments struct {
	pools   trustport.TenantPools
	timeout time.Duration
}

var _ slotDocuments = poolDocuments{}

// newPoolDocuments needs the tenant logins and a positive read bound; with either absent it is trustport.ErrConfig.
func newPoolDocuments(pools trustport.TenantPools, timeout time.Duration) (slotDocuments, error) {
	if absent(pools) || timeout <= 0 {
		return nil, trustport.ErrConfig
	}
	return poolDocuments{pools: pools, timeout: timeout}, nil
}

// Document reads one stored version of the tenant's intent. No row, and a row that is not the one asked for, is intake.ErrNotFound; a failure to read is trustport.ErrStore and
// names nothing of the row. The document is returned in canonical form (the database gives jsonb text), so that its hash can be checked.
func (d poolDocuments) Document(ctx context.Context, tenantID, intentID string, version int) (intentstore.Admitted, error) {
	switch {
	case absent(d.pools) || d.timeout <= 0:
		return intentstore.Admitted{}, trustport.ErrConfig
	case tenantID == "":
		return intentstore.Admitted{}, trustport.ErrSession
	case intentID == "" || version < 1:
		return intentstore.Admitted{}, fmt.Errorf("%w: the intent id or the version", trustport.ErrIntentRef)
	}
	q, err := d.pools.For(tenantID)
	if err != nil {
		return intentstore.Admitted{}, fmt.Errorf("cmd/core: the tenant's login: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, d.timeout)
	defer cancel()
	var a intentstore.Admitted
	var text string
	err = q.QueryRow(ctx, `SELECT tenant_id, intent_id, version, COALESCE(supersedes, ''), intent_node, intent_hash, correlation_id, json::text
		FROM admitted_intent WHERE tenant_id = $1 AND intent_id = $2 AND version = $3`, tenantID, intentID, version).
		Scan(&a.TenantID, &a.IntentID, &a.Version, &a.Supersedes, &a.IntentNode, &a.IntentHash, &a.CorrelationID, &text)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return intentstore.Admitted{}, intake.ErrNotFound
	case err != nil:
		return intentstore.Admitted{}, trustport.ErrStore
	case a.TenantID != tenantID || a.IntentID != intentID || a.Version != version:
		return intentstore.Admitted{}, intake.ErrNotFound
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(text)))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return intentstore.Admitted{}, fmt.Errorf("%w: the document does not decode", trustport.ErrStoredIntent)
	}
	if a.JSON, err = crypto.CanonicalJSON(v); err != nil {
		return intentstore.Admitted{}, fmt.Errorf("%w: the document has no canonical form", trustport.ErrStoredIntent)
	}
	return a, nil
}
