package main

// STM-01 final composition, the Planner (A684 step 2; A550, A551, A454, A440, A543, A330, interim, relayed, not yet committed; REQ-STM-001, REQ-TRU-009; INV-02, INV-14, INV-16;
// AGENT-09, AGENT-13). The Planner's graph reader is the verified pack's node plans with every slot kind copied from the NODE's slot declaration (A454), its slot reader is the stored
// intent's typed slots, and its text bound has no value anywhere, so it stays a named gap. Every value is SYNTHETIC and every name starts with cpl.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/plangraphs"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

const (
	cplTenant = "ten_synthetic1"
	cplIntent = "int_synthetic1"
	cplNode   = "synthetic.node"
	cplRef    = "synthetic.kind:ref_synthetic1"
)

func cplPlans() map[string]trustpack.NodePlan {
	return map[string]trustpack.NodePlan{cplNode: {
		GraphID: "synthetic.graph", GraphVersion: "1.2.3",
		Slots: []trustpack.PlanSlot{{Name: "target", Type: "Resource", ResourceKind: "synthetic.kind"}, {Name: "reason", Type: "Reference"}},
		Steps: []trustpack.PlanStep{{NodeID: "step_a", CapabilityID: "synthetic.cap.a", Preconditions: []string{"pre_a"}, Postconditions: []string{"post_a"}, Params: []trustpack.PlanParam{
			{Name: "p_slot", Slot: "target"},
			{Name: "p_map", HasMap: true, MapFrom: "reason", MapValues: map[string]string{"X": "CODE_X"}},
			{Name: "p_str", ConstJSON: `"lit"`}, {Name: "p_int", ConstJSON: `7`}, {Name: "p_bool", ConstJSON: `true`},
			{Name: "p_fact", Fact: "case_id"}, {Name: "p_policy", PolicyParam: "queue"}, {Name: "p_evidence", EvidencePacket: true},
		}}},
	}}
}

// A454: every slot or map param takes its kind and resource kind from the node's slot declaration; every other param takes its source from the one member the pack wrote.
func TestThePackGraphIsTheNodePlanWithSlotKindsFromTheNodesSlots(t *testing.T) {
	got, err := packPlanGraphs{plans: cplPlans()}.ActionGraph(t.Context(), cplNode)
	if err != nil {
		t.Fatal(err)
	}
	want := plangraphs.PackGraph{IntentNode: cplNode, GraphVersion: "1.2.3",
		Slots: map[string]plangraphs.SlotDecl{"target": {Kind: plangraphs.SlotKindResource, ResourceKind: "synthetic.kind"}, "reason": {Kind: plangraphs.SlotKindReference}},
		Steps: []plangraphs.PackStep{{NodeID: "step_a", CapabilityID: "synthetic.cap.a", Preconditions: []string{"pre_a"}, Postconditions: []string{"post_a"}, Params: []plangraphs.ParamSpec{
			{Name: "p_slot", Kind: plangraphs.KindSlot, Slot: "target", SlotKind: plangraphs.SlotKindResource, ResourceKind: "synthetic.kind"},
			{Name: "p_map", Kind: plangraphs.KindMap, Slot: "reason", Map: map[string]string{"X": "CODE_X"}, SlotKind: plangraphs.SlotKindReference},
			{Name: "p_str", Kind: plangraphs.KindConst, Value: "lit"}, {Name: "p_int", Kind: plangraphs.KindConst, Value: int64(7)}, {Name: "p_bool", Kind: plangraphs.KindConst, Value: true},
			{Name: "p_fact", Kind: plangraphs.KindFact, Ref: "case_id"}, {Name: "p_policy", Kind: plangraphs.KindPolicyParam, Ref: "queue"}, {Name: "p_evidence", Kind: plangraphs.KindEvidencePacket},
		}}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

// Fail closed (INV-16): a param with no source, with two, a slot the node does not declare, and a constant that is not plain data are each left for the planner to refuse, never
// completed. None of them gets a kind the node's slots do not give.
func TestAParamTheNodeCannotBackIsLeftForThePlannerToRefuse(t *testing.T) {
	plans := cplPlans()
	np := plans[cplNode]
	np.Steps[0].Params = []trustpack.PlanParam{
		{Name: "none"},
		{Name: "two", Slot: "target", Fact: "case_id"},
		{Name: "undeclared", Slot: "synthetic_absent"},
		{Name: "float", ConstJSON: `1.5`},
		{Name: "null", ConstJSON: `null`},
	}
	plans[cplNode] = np
	g, err := packPlanGraphs{plans: plans}.ActionGraph(t.Context(), cplNode)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]plangraphs.ParamSpec{}
	for _, p := range g.Steps[0].Params {
		by[p.Name] = p
	}
	if by["none"].Kind != "" || by["two"].Kind != "" {
		t.Errorf("a param with no source or two has kind %q / %q; want none", by["none"].Kind, by["two"].Kind)
	}
	if u := by["undeclared"]; u.SlotKind != "" || u.ResourceKind != "" {
		t.Errorf("a slot the node does not declare got the kind %q/%q", u.SlotKind, u.ResourceKind)
	}
	if by["float"].Value != nil || by["null"].Value != nil {
		t.Errorf("a constant that is not an integer, a string or a boolean has a value: %v, %v", by["float"].Value, by["null"].Value)
	}
}

// A slot the node declares twice has no declaration to trust: a param that names it keeps no kind and the planner refuses it.
func TestASlotTheNodeDeclaresTwiceBacksNoParam(t *testing.T) {
	plans := cplPlans()
	np := plans[cplNode]
	np.Slots = append(np.Slots, trustpack.PlanSlot{Name: "target", Type: "Reference"})
	plans[cplNode] = np
	g, err := packPlanGraphs{plans: plans}.ActionGraph(t.Context(), cplNode)
	if err != nil {
		t.Fatal(err)
	}
	if d := g.Slots["target"]; d.Kind != "" || d.ResourceKind != "" {
		t.Errorf("a slot declared twice has the declaration %+v", d)
	}
	if p := g.Steps[0].Params[0]; p.SlotKind != "" {
		t.Errorf("the param of a slot declared twice has the kind %q", p.SlotKind)
	}
}

func TestThePackGraphOfAnUnknownNodeIsErrNoGraphAndADoneContextIsAnError(t *testing.T) {
	if _, err := (packPlanGraphs{plans: cplPlans()}).ActionGraph(t.Context(), "synthetic.other"); !errors.Is(err, plangraphs.ErrNoGraph) {
		t.Errorf("an unknown node: %v; want ErrNoGraph", err)
	}
	if _, err := (packPlanGraphs{}).ActionGraph(t.Context(), cplNode); !errors.Is(err, plangraphs.ErrNoGraph) {
		t.Errorf("no plans: %v; want ErrNoGraph", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := (packPlanGraphs{plans: cplPlans()}).ActionGraph(ctx, cplNode); err == nil {
		t.Error("a done context gave a graph")
	}
}

// INV-02: what the planner reads is a copy; a change to one answer reaches neither the pack's plans nor the next answer.
func TestThePackGraphIsACopy(t *testing.T) {
	g := packPlanGraphs{plans: cplPlans()}
	first, err := g.ActionGraph(t.Context(), cplNode)
	if err != nil {
		t.Fatal(err)
	}
	first.Slots["target"] = plangraphs.SlotDecl{Kind: "MUTATED"}
	first.Steps[0].Preconditions[0] = "MUTATED"
	first.Steps[0].Params[1].Map["X"] = "MUTATED"
	second, err := g.ActionGraph(t.Context(), cplNode)
	if err != nil {
		t.Fatal(err)
	}
	fresh, _ := packPlanGraphs{plans: cplPlans()}.ActionGraph(t.Context(), cplNode)
	if !reflect.DeepEqual(second, fresh) {
		t.Errorf("a change to an answer reached the next: %+v", second)
	}
}

// ---- the stored intent's typed slots ----

type cplIntentsFake struct {
	a   intake.AdmittedIntent
	err error
}

func (f cplIntentsFake) Get(context.Context, string, string, int) (intake.AdmittedIntent, error) {
	return f.a, f.err
}
func (cplIntentsFake) LatestVersion(context.Context, string, string) (int, error) { return 1, nil }
func (cplIntentsFake) Verify(context.Context, intake.AdmittedIntent) error        { return nil }

type cplDocsFake struct {
	a   intentstore.Admitted
	err error
}

func (f cplDocsFake) Document(context.Context, string, string, int) (intentstore.Admitted, error) {
	return f.a, f.err
}

// cplAdmitted is a stored intent in canonical form with its own hash, as the gate admits it.
func cplAdmitted(t testing.TB, slots map[string]any) intentstore.Admitted {
	t.Helper()
	doc := map[string]any{"tenant_id": cplTenant, "intent_id": cplIntent, "correlation_id": "cor_synthetic1", "intent_node": cplNode, "status": "VALIDATED", "version": 1, "slots": slots}
	canon, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	return intentstore.Admitted{TenantID: cplTenant, IntentID: cplIntent, Version: 1, IntentNode: cplNode, CorrelationID: "cor_synthetic1", IntentHash: "sha256:" + hex.EncodeToString(sum[:]), JSON: canon}
}

func cplSlotsOf(a intentstore.Admitted) intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: a.TenantID, IntentID: a.IntentID, Version: a.Version, IntentNode: a.IntentNode, IntentHash: a.IntentHash, CorrelationID: a.CorrelationID, TargetRef: cplRef}
}

func TestTheStoredIntentsSlotsAreTypedFromTheStoredDocument(t *testing.T) {
	a := cplAdmitted(t, map[string]any{
		"target":  map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": cplRef},
		"reason":  map[string]any{"type": "Reference", "value": "X"},
		"pending": map[string]any{"type": "Resource", "status": "AMBIGUOUS", "resolved_ref": cplRef},
		"unsure":  map[string]any{"type": "Reference", "status": "AMBIGUOUS", "value": "X"},
		"amount":  map[string]any{"type": "Quantity", "status": "RESOLVED", "value": 500, "unit": "SYN"},
		"who":     map[string]any{"type": "Party", "status": "RESOLVED", "surface_form": "synthetic"},
	})
	got, err := storedSlots{Intents: cplIntentsFake{a: cplSlotsOf(a)}, Docs: cplDocsFake{a: a}}.Slots(t.Context(), cplTenant, cplIntent, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.TenantID != cplTenant || got.IntentID != cplIntent || got.Version != 1 || got.IntentNode != cplNode || got.IntentHash != a.IntentHash || got.TargetRef != cplRef {
		t.Errorf("identity, hash and target are the admitted intent's: %+v", got)
	}
	want := map[string]plangraphs.SlotValue{
		"target":  {Resolved: true, Value: cplRef, Kind: plangraphs.SlotKindResource, ResourceKind: "synthetic.kind"},
		"reason":  {Resolved: true, Value: "X", Kind: plangraphs.SlotKindReference},
		"pending": {Kind: plangraphs.SlotKindResource},
		"unsure":  {Kind: plangraphs.SlotKindReference},
		// The shape of a Quantity, a Party or a TimeRef as a plan param is unstated (a Quantity's value without its unit would lose the currency): they are never offered as resolved.
		"amount": {Kind: plangraphs.SlotKindQuantity},
		"who":    {Kind: plangraphs.SlotKindParty},
	}
	if !reflect.DeepEqual(got.Slots, want) {
		t.Errorf("slots %+v; want %+v", got.Slots, want)
	}
}

// A document that is not the admitted intent's own (another hash, a changed byte, another id) is no source of a param (INV-16, INV-17).
func TestAStoredDocumentThatIsNotTheAdmittedIntentsGivesNoSlots(t *testing.T) {
	a := cplAdmitted(t, map[string]any{"reason": map[string]any{"type": "Reference", "value": "X"}})
	tamper := a
	tamper.JSON = []byte(strings.Replace(string(a.JSON), `"value":"X"`, `"value":"Y"`, 1))
	other := a
	other.IntentHash = "sha256:" + strings.Repeat("0", 64)
	for name, doc := range map[string]intentstore.Admitted{"a changed byte": tamper, "another hash": other} {
		if got, err := (storedSlots{Intents: cplIntentsFake{a: cplSlotsOf(a)}, Docs: cplDocsFake{a: doc}}).Slots(t.Context(), cplTenant, cplIntent, 1); err == nil {
			t.Errorf("%s: slots %+v and no error", name, got)
		}
	}
	admitted := cplSlotsOf(a)
	admitted.IntentHash = "sha256:" + strings.Repeat("1", 64)
	if got, err := (storedSlots{Intents: cplIntentsFake{a: admitted}, Docs: cplDocsFake{a: a}}).Slots(t.Context(), cplTenant, cplIntent, 1); err == nil {
		t.Errorf("a document of another hash than the admitted intent's gave slots %+v", got)
	}
}

func TestAReadThatFailsIsReturnedAndNeverAnEmptySlotSet(t *testing.T) {
	a := cplAdmitted(t, map[string]any{})
	boom := errors.New("synthetic read failure")
	for name, s := range map[string]storedSlots{
		"the intent":   {Intents: cplIntentsFake{err: intake.ErrNotFound}, Docs: cplDocsFake{a: a}},
		"the document": {Intents: cplIntentsFake{a: cplSlotsOf(a)}, Docs: cplDocsFake{err: boom}},
		"no intents":   {Docs: cplDocsFake{a: a}},
		"no documents": {Intents: cplIntentsFake{a: cplSlotsOf(a)}},
	} {
		if got, err := s.Slots(t.Context(), cplTenant, cplIntent, 1); err == nil {
			t.Errorf("%s: slots %+v and no error", name, got)
		}
	}
}

// ---- the document reader over the tenant's own login ----

type cplRow struct {
	vals []any
	err  error
}

func (r cplRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = r.vals[i].(string)
		case *int:
			*p = r.vals[i].(int)
		default:
			return errors.New("cplRow: a destination it does not know")
		}
	}
	return nil
}

type cplQuerier struct {
	row  cplRow
	seen []any
}

func (q *cplQuerier) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	q.seen = args
	return q.row
}

type cplPools struct {
	q   *cplQuerier
	err error
	got string
}

func (p *cplPools) For(tenantID string) (trustport.Querier, error) {
	p.got = tenantID
	return p.q, p.err
}

// The reader asks the tenant's own login for exactly one row, and gives the document in canonical form (the database returns jsonb text) so that its hash can be checked.
func TestThePoolDocumentsReadsTheTenantsRowAndMakesTheDocumentCanonical(t *testing.T) {
	a := cplAdmitted(t, map[string]any{"reason": map[string]any{"type": "Reference", "value": "X"}})
	spaced := strings.NewReplacer(`":`, `": `, `,"`, `, "`).Replace(string(a.JSON))
	q := &cplQuerier{row: cplRow{vals: []any{cplTenant, cplIntent, 1, "", cplNode, a.IntentHash, "cor_synthetic1", spaced}}}
	pools := &cplPools{q: q}
	d, err := newPoolDocuments(pools, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	got, err := d.Document(t.Context(), cplTenant, cplIntent, 1)
	if err != nil {
		t.Fatal(err)
	}
	if pools.got != cplTenant || !reflect.DeepEqual(q.seen, []any{cplTenant, cplIntent, 1}) {
		t.Errorf("the login of %q was asked with %v", pools.got, q.seen)
	}
	if !reflect.DeepEqual(got, a) {
		t.Errorf("got %+v; want %+v", got, a)
	}
}

func TestThePoolDocumentsRefusesWhatItCannotAnswer(t *testing.T) {
	if _, err := newPoolDocuments(nil, time.Second); err == nil {
		t.Error("no pools was accepted")
	}
	if _, err := newPoolDocuments(&cplPools{q: &cplQuerier{}}, 0); err == nil {
		t.Error("no read bound was accepted")
	}
	mk := func(p *cplPools) slotDocuments {
		t.Helper()
		d, err := newPoolDocuments(p, time.Second)
		if err != nil || d == nil {
			t.Fatalf("newPoolDocuments: %v, %v", d, err)
		}
		return d
	}
	d := mk(&cplPools{q: &cplQuerier{row: cplRow{err: pgx.ErrNoRows}}})
	if _, err := d.Document(t.Context(), cplTenant, cplIntent, 1); !errors.Is(err, intake.ErrNotFound) {
		t.Errorf("no row: %v; want ErrNotFound", err)
	}
	d = mk(&cplPools{err: trustport.ErrNoPool})
	if _, err := d.Document(t.Context(), cplTenant, cplIntent, 1); !errors.Is(err, trustport.ErrNoPool) {
		t.Errorf("no login for the tenant: %v; want ErrNoPool", err)
	}
	// A row that is another tenant's, intent's or version than the one asked for is not an answer.
	a := cplAdmitted(t, map[string]any{})
	d = mk(&cplPools{q: &cplQuerier{row: cplRow{vals: []any{"ten_synthetic2", cplIntent, 1, "", cplNode, a.IntentHash, "cor_synthetic1", string(a.JSON)}}}})
	if _, err := d.Document(t.Context(), cplTenant, cplIntent, 1); !errors.Is(err, intake.ErrNotFound) {
		t.Errorf("another tenant's row: %v; want ErrNotFound", err)
	}
}

// ---- the Planner ----

func cplEscalationPlan(t testing.TB, p *trustpack.Pack) (node string, np trustpack.NodePlan) {
	t.Helper()
	plans, err := p.NodePlans()
	if err != nil || len(plans) == 0 {
		t.Fatalf("the real pack gives no node plan: %v", err)
	}
	for id, v := range plans {
		return id, v
	}
	return "", trustpack.NodePlan{}
}

// No ruling or setting gives the text bound: with every other input the Planner is a named gap that lacks that and nothing else, and no Planner is built (A330, A469).
func TestWithNoTextBoundThePlannerIsAGapThatNamesOnlyIt(t *testing.T) {
	a := cplAdmitted(t, map[string]any{})
	got, missing, err := buildPlanner(plannerInputs{Pack: decPack(t), Intents: cplIntentsFake{a: cplSlotsOf(a)}, Docs: cplDocsFake{a: a}})
	if err != nil || got != nil || !reflect.DeepEqual(missing, []string{needPlanTextBound}) {
		t.Errorf("got %v, %v, %v; want no Planner, the text bound named and no error", got, missing, err)
	}
}

func TestEveryInputThePlannerLacksIsNamedAndNoneIsInvented(t *testing.T) {
	got, missing, err := buildPlanner(plannerInputs{})
	want := []string{needPack, needAdmittedIntents, needSlotDocuments, needPlanTextBound}
	if err != nil || got != nil || !reflect.DeepEqual(missing, want) {
		t.Errorf("got %v, %v, %v; want the gaps %q", got, missing, err, want)
	}
}

// A present value that cannot be a bound is fatal and never a gap (A469): a negative one.
func TestANegativeTextBoundIsFatalAndNamesNoValue(t *testing.T) {
	a := cplAdmitted(t, map[string]any{})
	_, _, err := buildPlanner(plannerInputs{Pack: decPack(t), Intents: cplIntentsFake{a: cplSlotsOf(a)}, Docs: cplDocsFake{a: a}, MaxTextBytes: -7})
	if err == nil || strings.Contains(err.Error(), "-7") {
		t.Errorf("got %v; want an error that does not carry the value", err)
	}
}

// The whole Planner over the real pack: the plan of the pack's own graph, from the stored intent's resolved slots, the node's slot kinds and the request's capabilities, and nothing else
// (INV-02). SYNTHETIC ids.
func TestThePlannerPlansThePacksGraphFromTheStoredIntent(t *testing.T) {
	p := decPack(t)
	node, np := cplEscalationPlan(t, p)
	first := np.Steps[0]
	var mapParam, slotParam trustpack.PlanParam
	for _, q := range first.Params {
		switch {
		case q.HasMap:
			mapParam = q
		case q.Slot != "":
			slotParam = q
		}
	}
	var target, enum trustpack.PlanSlot
	for _, s := range np.Slots {
		switch s.Name {
		case slotParam.Slot:
			target = s
		case mapParam.MapFrom:
			enum = s
		}
	}
	var key, code string
	for k, v := range mapParam.MapValues {
		key, code = k, v
		break
	}
	ref := target.ResourceKind + ":txn_synth1"
	slots := map[string]any{
		target.Name: map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": ref},
		enum.Name:   map[string]any{"type": "Reference", "value": key},
	}
	a := cplAdmitted(t, slots)
	a.IntentNode = node
	doc := map[string]any{"tenant_id": cplTenant, "intent_id": cplIntent, "correlation_id": "cor_synthetic1", "intent_node": node, "status": "VALIDATED", "version": 1, "slots": slots}
	canon, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	a.JSON, a.IntentHash = canon, "sha256:"+hex.EncodeToString(sum[:])
	admitted := cplSlotsOf(a)
	admitted.TargetRef = ref
	pl, missing, err := buildPlanner(plannerInputs{Pack: p, Intents: cplIntentsFake{a: admitted}, Docs: cplDocsFake{a: a}, MaxTextBytes: 256})
	if err != nil || len(missing) != 0 || pl == nil {
		t.Fatalf("buildPlanner = %v, %v, %v", pl, missing, err)
	}
	plan, err := pl.Plan(t.Context(), intake.PlanRequest{TenantID: cplTenant, IntentID: cplIntent, IntentVersion: 1, IntentHash: a.IntentHash, IntentNode: node, WriteRequired: true,
		Capabilities: []string{first.CapabilityID}})
	if err != nil {
		t.Fatal(err)
	}
	wantHash, err := crypto.HashOf(map[string]any{mapParamsFirst(first, target.Name): ref, mapParam.Name: code})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Steps) != 1 || plan.Steps[0].CapabilityID != first.CapabilityID || plan.Steps[0].TargetRef != ref || plan.Steps[0].ParamsHash != wantHash {
		t.Errorf("plan %+v; want one step of %q on %q with the params hash %s", plan, first.CapabilityID, ref, wantHash)
	}
}

// mapParamsFirst is the name of the first step's param that takes the target slot.
func mapParamsFirst(s trustpack.PlanStep, slot string) string {
	for _, q := range s.Params {
		if q.Slot == slot {
			return q.Name
		}
	}
	return ""
}

// In the decisions lane the Planner is built from the pack, the Intents adapter and the tenant's login when the text bound is given, and is the one named gap when it is not; fill sets
// it only when built and never overwrites another lane's field.
func TestTheDecisionsLaneBuildsThePlannerOnlyWithATextBound(t *testing.T) {
	in := decInputs(t, nil)
	without, err := buildDecisions(in)
	if err != nil {
		t.Fatal(err)
	}
	if g, ok := decGap(without, "Planner"); !ok || !reflect.DeepEqual(g.Missing, []string{needPlanTextBound}) || without.Planner != nil {
		t.Errorf("without a bound: gap %+v (named %v), Planner %T; want only the text bound", g, ok, without.Planner)
	}
	in.PlanTextMaxBytes = 256 // a SYNTHETIC test number, not a product value
	with, err := buildDecisions(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := decGap(with, "Planner"); ok || with.Planner == nil {
		t.Fatalf("with a bound the Planner is built: %T, gap named %v", with.Planner, ok)
	}
	if got := with.fill(TrustAdapters{}); got.Planner == nil {
		t.Error("fill did not set the Planner")
	}
	if got := without.fill(TrustAdapters{}); got.Planner != nil {
		t.Errorf("fill set a Planner that was not built: %T", got.Planner)
	}
	if _, err := buildDecisions(func() decisionInputs { x := in; x.PlanTextMaxBytes = -1; return x }()); err == nil {
		t.Error("a negative bound was accepted")
	}
}

// The bound is the one given: a resolved text longer than it is refused at planning (AGENT-13), and the same plan with a bound that holds it is made.
func TestTheTextBoundGivenIsTheOneThePlannerEnforces(t *testing.T) {
	p := decPack(t)
	node, np := cplEscalationPlan(t, p)
	first := np.Steps[0]
	var mapParam, slotParam trustpack.PlanParam
	for _, q := range first.Params {
		switch {
		case q.HasMap:
			mapParam = q
		case q.Slot != "":
			slotParam = q
		}
	}
	var target trustpack.PlanSlot
	for _, s := range np.Slots {
		if s.Name == slotParam.Slot {
			target = s
		}
	}
	var key string
	for k := range mapParam.MapValues {
		key = k
		break
	}
	ref := target.ResourceKind + ":txn_synth1"
	slots := map[string]any{slotParam.Slot: map[string]any{"type": "Resource", "status": "RESOLVED", "resolved_ref": ref}, mapParam.MapFrom: map[string]any{"type": "Reference", "value": key}}
	doc := map[string]any{"tenant_id": cplTenant, "intent_id": cplIntent, "correlation_id": "cor_synthetic1", "intent_node": node, "status": "VALIDATED", "version": 1, "slots": slots}
	canon, err := crypto.CanonicalJSON(doc)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	a := intentstore.Admitted{TenantID: cplTenant, IntentID: cplIntent, Version: 1, IntentNode: node, CorrelationID: "cor_synthetic1", JSON: canon, IntentHash: "sha256:" + hex.EncodeToString(sum[:])}
	admitted := cplSlotsOf(a)
	admitted.TargetRef = ref
	req := intake.PlanRequest{TenantID: cplTenant, IntentID: cplIntent, IntentVersion: 1, IntentHash: a.IntentHash, IntentNode: node, WriteRequired: true, Capabilities: []string{first.CapabilityID}}
	bound := func(n int) intake.Planner {
		pl, missing, err := buildPlanner(plannerInputs{Pack: p, Intents: cplIntentsFake{a: admitted}, Docs: cplDocsFake{a: a}, MaxTextBytes: n})
		if err != nil || len(missing) != 0 || pl == nil {
			t.Fatalf("buildPlanner(%d) = %v, %v, %v", n, pl, missing, err)
		}
		return pl
	}
	if _, err := bound(len(ref)-1).Plan(t.Context(), req); !errors.Is(err, intake.ErrPlanInvalid) {
		t.Errorf("a text one byte over the bound: %v; want ErrPlanInvalid", err)
	}
	if _, err := bound(len(ref)).Plan(t.Context(), req); err != nil {
		t.Errorf("a text at the bound: %v", err)
	}
}
