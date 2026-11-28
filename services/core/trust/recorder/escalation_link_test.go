package recorder_test

// TRU-10 (REQ-TRU-010; INV-25, INV-03, INV-14; PERD 23.1, ruling A297): an action decision is the per-write-step decision. An ESCALATE disposition authorises only the
// escalation plan's write steps, and a decision that names a disposition names an existing ESCALATE decision of its own tenant. The escalation plan's capabilities come
// from the pack (packs/fintech/graph/g2_escalate.json, read here as data) and are injected into the Writer; the recorder holds none. Real PostgreSQL. A refused
// request writes nothing, so these tests COMMIT after a refusal: a leftover row or event would survive and be counted. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sync/atomic"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const (
	capEvidence  = "fin.cap.record_investigation_evidence"
	capRoute     = "fin.cap.route_to_internal_owner"
	capComplaint = "fin.cap.log_complaint_reference" // a WRITE of the pack that is not a node of the escalation graph (g3_complaint.json)
	capOther     = "fin.cap.some_other_write"
)

// packEscalationCapabilities reads the capability IDs of the pack's escalation graph from the pack data, as the production adapter will (not built here).
func packEscalationCapabilities(t testing.TB) []string {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech/graph/g2_escalate.json")
	if err != nil {
		t.Fatal(err)
	}
	var g struct {
		Nodes []struct {
			Kind         string `json:"kind"`
			CapabilityID string `json:"capability_id"`
		} `json:"nodes"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, n := range g.Nodes {
		if n.Kind == "WRITE" {
			out = append(out, n.CapabilityID)
		}
	}
	if len(out) != 3 {
		t.Fatalf("the escalation graph has %d write nodes, want 3 (create case, record evidence, route)", len(out))
	}
	return out
}

// escalationWorld is newWorld, which already holds the escalation set of the pack.
func escalationWorld(t *testing.T) *world {
	t.Helper()
	return newWorld(t)
}

func stepFor(t *testing.T, capabilityID string, planStep int) grants.Step {
	t.Helper()
	s := step(t)
	p, err := recordPermit(capabilityID)
	if err != nil {
		t.Fatal(err)
	}
	s.CapabilityID, s.Permission, s.PlanStep = capabilityID, p, int64(planStep)
	return s
}

// seq makes each action request a distinct input (A186: one decision per full input tuple).
var seq atomic.Int64

// h2 is a distinct intent hash for each n, so that two requests are two inputs (A186).
func h2(n int) contracts.CommonV1Hash { return contracts.CommonV1Hash(fmt.Sprintf("sha256:%064x", n)) }

func recordPermit(capabilityID string) (reconcile.Permission, error) {
	return reconcile.NewFreezeGate(nil).PermitTarget(false, capabilityID, target)
}

func contractsDecisionID(s string) contracts.CommonV1DecisionId {
	return contracts.CommonV1DecisionId(s)
}

type counts struct{ records, grants, events, spends int }

func (w *world) counts(t testing.TB) counts {
	t.Helper()
	q := func(s string) int {
		var n int
		if err := w.admin.QueryRow(ctx(t), s).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	return counts{q(`SELECT count(*) FROM decision_record`), q(`SELECT count(*) FROM decision_grant`), q(`SELECT count(*) FROM record_event`), q(`SELECT count(*) FROM confirmation_spend`)}
}

// writeAndCommit commits whatever the Write left, also after a refusal: a refusal must have written nothing, so there is nothing to lose by committing it.
func (w *world) writeAndCommit(t testing.TB, r recorder.Request) (recorder.Result, error) {
	t.Helper()
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	res, werr := w.w.Write(ctx(t), tx, r)
	if cerr := tx.Commit(ctx(t)); cerr != nil && werr == nil {
		t.Fatal(cerr)
	}
	return res, werr
}

// disposition writes an ESCALATE decision (no steps) for the intent hash n and returns its decision ID.
func (w *world) disposition(t *testing.T, n int) string {
	t.Helper()
	b := evaluated(t)
	r := request(t, decision(t, decide.Escalate, b), b)
	r.IntentHash = h2(n)
	res, err := w.writeAndCommit(t, r)
	if err != nil || res.Record.Outcome != "ESCALATE" {
		t.Fatalf("the disposition: %+v %v", res, err)
	}
	return string(res.Record.DecisionId)
}

// intentOf is the intent hash and the intent ID of a stored decision, for an action decision that follows it as its disposition (A542): the same intent, evaluated again, as a
// continuation is. The administrator reads the row, whatever tenant it is in; a decision that does not exist gives the defaults of request().
func (w *world) intentOf(t *testing.T, decisionID string) (contracts.CommonV1Hash, string, bool) {
	t.Helper()
	var hash, intent string
	err := w.admin.QueryRow(ctx(t), `SELECT intent_hash, json ->> 'intent_id' FROM decision_record WHERE decision_id = $1`, decisionID).Scan(&hash, &intent)
	if err != nil {
		return "", "", false
	}
	return contracts.CommonV1Hash(hash), intent, true
}

// action is an action decision that follows disp: the intent and the intent hash of the disposition (A542), the same intent evaluated again, so against another sealed snapshot, which makes
// it another input (A186). With no disposition, or one that does not exist, it is an input of its own.
func (w *world) action(t *testing.T, o decide.Outcome, disp string, capabilities ...string) recorder.Request {
	t.Helper()
	n := int(seq.Add(1))
	b := evaluatedWith(t, fmt.Sprintf("evs_%d", 1000+n), "3.1")
	var steps []grants.Step
	for i, c := range capabilities {
		steps = append(steps, stepFor(t, c, i+1))
	}
	r := request(t, decision(t, o, b), b, steps...)
	r.IntentHash, r.DispositionDecisionID = h2(n+1000), disp
	if hash, intent, ok := w.intentOf(t, disp); ok && disp != "" {
		r.IntentHash, r.IntentID = hash, intent
	}
	return r
}

func (w *world) storedDisposition(t *testing.T, decisionID string) (string, bool) {
	t.Helper()
	var d *string
	if err := w.admin.QueryRow(ctx(t), `SELECT disposition_decision_id FROM decision_record WHERE decision_id = $1`, decisionID).Scan(&d); err != nil {
		t.Fatal(err)
	}
	if d == nil {
		return "", false
	}
	return *d, true
}

// REQ-TRU-010, INV-25 (ruling A317 to A328, rescoped by A534, interim): an ALLOW with a step of the escalation plan and no disposition is refused with the typed error, and nothing is
// written. A write outside the escalation plan (the complaint write) needs none: escalation_rescope_test.go.
func TestADecisionWithStepsAndNoDispositionIsRefusedAndNothingIsWritten(t *testing.T) {
	w := escalationWorld(t)
	for _, c := range []string{capability, capEvidence, capRoute} {
		before := w.counts(t)
		r := w.action(t, decide.Allow, "", c)
		res, err := w.writeAndCommit(t, r)
		if !errors.Is(err, recorder.ErrDispositionRequired) {
			t.Errorf("%s with no disposition: got %v (%+v), want ErrDispositionRequired", c, err, res)
		}
		if after := w.counts(t); after != before {
			t.Errorf("%s: a refusal changed the rows from %+v to %+v", c, before, after)
		}
	}
}

// A decision that issues no grant needs no disposition: an ALLOW of a disclosure (a read, no steps) and a refusal are written as before.
func TestADecisionWithNoStepsNeedsNoDisposition(t *testing.T) {
	w := escalationWorld(t)
	for _, o := range []decide.Outcome{decide.Allow, decide.Reject, decide.Wait} {
		res, err := w.writeAndCommit(t, w.action(t, o, ""))
		if err != nil || len(res.Grants) != 0 {
			t.Fatalf("%s with no steps and no disposition: %+v %v", o, res, err)
		}
		if _, linked := w.storedDisposition(t, string(res.Record.DecisionId)); linked {
			t.Errorf("%s: a disposition appears on a decision that named none", o)
		}
	}
}

// REQ-TRU-010, US-03: the steps of the escalation plan (create the case, record the evidence, route) each follow the ESCALATE disposition as their own action decision, and
// the Decision Record, the column and the Record all show the link.
func TestEachStepOfTheEscalationPlanFollowsItsEscalateDisposition(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	for _, c := range []string{capability, capEvidence, capRoute} {
		res, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, c))
		if err != nil || len(res.Grants) != 1 {
			t.Fatalf("%s: %+v %v", c, res, err)
		}
		if res.Record.DispositionDecisionId.Value != contractsDecisionID(disp) || !res.Record.DispositionDecisionId.Set {
			t.Errorf("%s: the record does not name the disposition: %+v", c, res.Record.DispositionDecisionId)
		}
		if got, linked := w.storedDisposition(t, string(res.Record.DecisionId)); !linked || got != disp {
			t.Errorf("%s: stored disposition %q, want %q", c, got, disp)
		}
	}
	// a REJECT carries no step, so it names no disposition (A503: steps if and only if a disposition): the writer refuses it and nothing is written
	before := w.counts(t)
	if _, err := w.writeAndCommit(t, w.action(t, decide.Reject, disp)); !errors.Is(err, recorder.ErrDispositionWithoutSteps) {
		t.Fatalf("a REJECT that names a disposition: got %v, want ErrDispositionWithoutSteps", err)
	}
	if after := w.counts(t); after != before {
		t.Errorf("the refusal changed the rows from %+v to %+v", before, after)
	}
}

// INV-25, REQ-TRU-010: an ESCALATE disposition authorises only the escalation plan's write steps. One step outside the set refuses the whole decision, and nothing is written.
func TestAStepOutsideTheEscalationPlanUnderAnEscalateDispositionIsRefusedAndNothingIsWritten(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)
	cases := map[string][]string{
		"the complaint write, which is in another graph": {capComplaint},
		"a capability the pack does not know":            {capOther},
		"an escalation step and one outside":             {capability, capComplaint},
		"one outside, then escalation steps":             {capOther, capEvidence, capRoute},
	}
	for name, caps := range cases {
		before := w.counts(t)
		res, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, caps...))
		if !errors.Is(err, recorder.ErrStepOutsideEscalation) {
			t.Errorf("%s: got %v (%+v), want ErrStepOutsideEscalation", name, err, res)
		}
		if after := w.counts(t); after != before {
			t.Errorf("%s: a refusal changed the rows from %+v to %+v", name, before, after)
		}
	}
}

// The writer follows the set it is given, never a list of its own (INV-14: no industry term in the core): a set of one other capability allows that one and refuses the
// escalation steps of the pack; the empty set (the zero value) refuses every step after a disposition.
func TestTheWriterUsesTheEscalationSetItIsGivenAndFailsClosedWithNone(t *testing.T) {
	w := escalationWorld(t)
	disp := w.disposition(t, 1)

	w.w.Escalation = recorder.NewEscalationSet(capComplaint)
	if res, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, capComplaint)); err != nil || len(res.Grants) != 1 {
		t.Fatalf("a capability of the injected set was refused: %+v %v", res, err)
	}
	before := w.counts(t)
	if _, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, capability)); !errors.Is(err, recorder.ErrStepOutsideEscalation) {
		t.Fatalf("a pack escalation step outside the injected set: %v", err)
	}
	w.w.Escalation = recorder.EscalationSet{}
	if _, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, capability)); !errors.Is(err, recorder.ErrStepOutsideEscalation) {
		t.Fatalf("the empty set allowed a step: %v", err)
	}
	if after := w.counts(t); after != before {
		t.Errorf("refusals changed the rows from %+v to %+v", before, after)
	}
	// steps if and only if a disposition (A503): a decision with no step names none, whatever the set
	if _, err := w.writeAndCommit(t, w.action(t, decide.Reject, disp)); !errors.Is(err, recorder.ErrDispositionWithoutSteps) {
		t.Fatalf("a decision without steps that names a disposition: got %v, want ErrDispositionWithoutSteps", err)
	}
	if after := w.counts(t); after != before {
		t.Errorf("the refusal of a disposition without steps changed the rows from %+v to %+v", before, after)
	}
}

// otherTenantDisposition writes an ESCALATE decision as tenant ten_bb, through that tenant's own login.
func (w *world) otherTenantDisposition(t *testing.T) string {
	t.Helper()
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	b := evaluated(t)
	r := request(t, decision(t, decide.Escalate, b), b)
	r.TenantID, r.IntentHash = "ten_bb", h2(7)
	tx, err := testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", "ten_bb").Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := w.w.Write(ctx(t), tx, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return string(res.Record.DecisionId)
}

// notDispositions are the names a decision must not be allowed to give as its disposition (REQ-TRU-010, INV-25).
func (w *world) notDispositions(t *testing.T) map[string]string {
	t.Helper()
	allow, err := w.writeAndCommit(t, w.action(t, decide.Allow, ""))
	if err != nil {
		t.Fatal(err)
	}
	reject, err := w.writeAndCommit(t, w.action(t, decide.Reject, ""))
	if err != nil {
		t.Fatal(err)
	}
	return map[string]string{
		"a decision that does not exist":     "dec_NOSUCHDECISION",
		"another tenant's ESCALATE decision": w.otherTenantDisposition(t),
		"the tenant's own ALLOW decision":    string(allow.Record.DecisionId),
		"the tenant's own REJECT decision":   string(reject.Record.DecisionId),
	}
}

func (w *world) requireDispositionRefused(t *testing.T) {
	t.Helper()
	for name, disp := range w.notDispositions(t) {
		before := w.counts(t)
		res, err := w.writeAndCommit(t, w.action(t, decide.Allow, disp, capability))
		if !errors.Is(err, recorder.ErrDispositionNotValid) {
			t.Errorf("%s: got %v (%+v), want ErrDispositionNotValid", name, err, res)
		}
		if after := w.counts(t); after != before {
			t.Errorf("%s: a refusal changed the rows from %+v to %+v", name, before, after)
		}
	}
}

// REQ-TRU-010: a decision that names a disposition names an existing ESCALATE decision of its own tenant. Unknown, another tenant's, and one with another outcome are each
// refused with the typed error, for an ALLOW with a step (a decision with none names no disposition at all, A503), and nothing is written.
func TestADecisionNamingADispositionThatIsNotAnExistingEscalateDecisionOfItsTenantIsRefused(t *testing.T) {
	escalationWorld(t).requireDispositionRefused(t)
}

// The same, with the database's foreign key (migration 0012) switched off: the recorder's own check refuses before anything is written, not only the database after the
// Record event is appended. Without this the typed refusal could hide behind the foreign key and a mutant that drops the check would survive.
func TestTheRecorderRefusesAnInvalidDispositionOnItsOwnWithTheForeignKeyOff(t *testing.T) {
	w := escalationWorld(t)
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE decision_record DISABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	w.requireDispositionRefused(t)
}

// ---- the property of the task row ----

// TestPropertyNoActionDecisionWithoutALinkedDisposition (TRU-10 done-when; REQ-TRU-010, INV-25): over generated requests (a disposition that is absent, valid, unknown,
// another tenant's, or not an ESCALATE decision; an ALLOW with up to three steps drawn from the escalation plan and from outside it, or a REJECT), (1) a decision that names
// a disposition is written only when it is an existing ESCALATE decision of the tenant; (2) under a named disposition no grant is ever issued for a capability outside the
// escalation plan; (3) no grant of the escalation plan is ever issued by a decision that names no disposition (A317 to A328, rescoped by A534); (4) a refusal writes no row and no event; (5) a decision with no steps that names none is
// written. The set is the pack's, read as data.
func TestPropertyNoActionDecisionWithoutALinkedDisposition(t *testing.T) {
	w := escalationWorld(t)
	valid := w.disposition(t, 1)
	bad := w.notDispositions(t)
	pool := append(packEscalationCapabilities(t), capComplaint, capOther)
	kinds := []string{"none", "valid", "unknown", "otherTenant", "ownAllow", "ownReject"}
	pick := map[string]string{"none": "", "valid": valid, "unknown": bad["a decision that does not exist"], "otherTenant": bad["another tenant's ESCALATE decision"],
		"ownAllow": bad["the tenant's own ALLOW decision"], "ownReject": bad["the tenant's own REJECT decision"]}
	inSet := recorder.NewEscalationSet(packEscalationCapabilities(t)...)
	rapid.Check(t, func(rt *rapid.T) {
		kind := rapid.SampledFrom(kinds).Draw(rt, "disposition")
		o := rapid.SampledFrom([]decide.Outcome{decide.Allow, decide.Reject}).Draw(rt, "outcome")
		var caps []string
		if o == decide.Allow {
			caps = rapid.SliceOfNDistinct(rapid.SampledFrom(pool), 0, 3, func(s string) string { return s }).Draw(rt, "steps")
		}
		disp := pick[kind]
		before := w.counts(t)
		res, err := w.writeAndCommit(t, w.action(t, o, disp, caps...))
		after := w.counts(t)

		outside := slices.ContainsFunc(caps, func(c string) bool { return !inSet.Contains(c) })
		switch {
		case disp == "" && len(caps) > 0 && slices.ContainsFunc(caps, inSet.Contains):
			if !errors.Is(err, recorder.ErrDispositionRequired) {
				rt.Fatalf("none with %v: got %v, want ErrDispositionRequired", caps, err)
			}
		case disp != "" && len(caps) == 0:
			if !errors.Is(err, recorder.ErrDispositionWithoutSteps) {
				rt.Fatalf("%s with no step: got %v, want ErrDispositionWithoutSteps", kind, err)
			}
		case disp != "" && outside:
			if !errors.Is(err, recorder.ErrStepOutsideEscalation) {
				rt.Fatalf("%s with %v: got %v, want ErrStepOutsideEscalation", kind, caps, err)
			}
		case disp != "" && kind != "valid":
			if !errors.Is(err, recorder.ErrDispositionNotValid) {
				rt.Fatalf("%s: got %v, want ErrDispositionNotValid", kind, err)
			}
		default:
			if err != nil {
				rt.Fatalf("%s with %v: a valid request was refused: %v", kind, caps, err)
			}
		}
		if err != nil {
			if after != before {
				rt.Fatalf("%s with %v: a refusal changed the rows from %+v to %+v", kind, caps, before, after)
			}
			return
		}
		if len(res.Grants) != len(caps) || after.records != before.records+1 {
			rt.Fatalf("%s with %v: %d grants and %d new records", kind, caps, len(res.Grants), after.records-before.records)
		}
		got, linked := w.storedDisposition(t, string(res.Record.DecisionId))
		if linked != (disp != "") || got != disp {
			rt.Fatalf("%s: stored disposition %q (%v), want %q", kind, got, linked, disp)
		}
	})
	// over everything ever written: no grant of a capability of the escalation plan exists whose decision names no disposition (A317 to A328, rescoped by A534)
	var unlinked int
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM decision_grant g JOIN decision_record d ON d.decision_id = g.decision_id AND d.tenant_id = g.tenant_id
		WHERE d.disposition_decision_id IS NULL AND (g.json ->> 'capability_id') = ANY($1)`, packEscalationCapabilities(t)).Scan(&unlinked); err != nil || unlinked != 0 {
		t.Errorf("%d grants of the escalation plan belong to a decision that names no disposition (%v)", unlinked, err)
	}
	// and a decision that names a disposition names an ESCALATE decision of its tenant, and holds grants only for the escalation plan
	rows, err := w.admin.Query(ctx(t), `SELECT g.json ->> 'capability_id', p.outcome, d.tenant_id = p.tenant_id
		FROM decision_grant g JOIN decision_record d ON d.decision_id = g.decision_id AND d.tenant_id = g.tenant_id
		JOIN decision_record p ON p.decision_id = d.disposition_decision_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var c, outcome string
		var same bool
		if err := rows.Scan(&c, &outcome, &same); err != nil {
			t.Fatal(err)
		}
		if !inSet.Contains(c) || outcome != "ESCALATE" || !same {
			t.Errorf("a grant for %s under a %s disposition (same tenant %v) was written", c, outcome, same)
		}
	}
}

// The tenant of the disposition is checked by the recorder itself, not only by the row security of the login (REQ-SEC-004, defence in depth): written through a connection that
// sees every tenant's rows, with the foreign key off, another tenant's ESCALATE decision is still refused and nothing is written.
func TestTheRecorderChecksTheDispositionTenantEvenWhereRowSecurityShowsEveryTenant(t *testing.T) {
	w := escalationWorld(t)
	other := w.otherTenantDisposition(t)
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE decision_record DISABLE TRIGGER ALL`); err != nil {
		t.Fatal(err)
	}
	w.trust = w.admin
	before := w.counts(t)
	res, err := w.writeAndCommit(t, w.action(t, decide.Allow, other, capability))
	if !errors.Is(err, recorder.ErrDispositionNotValid) {
		t.Fatalf("another tenant's disposition: got %v (%+v), want ErrDispositionNotValid", err, res)
	}
	if after := w.counts(t); after != before {
		t.Errorf("a refusal changed the rows from %+v to %+v", before, after)
	}
}
