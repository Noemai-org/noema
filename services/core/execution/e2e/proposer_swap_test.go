package e2e_test

// The proposer swap test, execution half. It runs the real core on the same canned decision from two different proposers.
//
// WHAT THIS VALIDATES. The decision path only: the real EscalateCase workflow, the real controller over PostgreSQL and the
// real mock-case, started from the decision the real trust/decide constructors made. It does NOT validate the unbuilt
// intake, the compile endpoint, the schema gate or the complete architecture.
//
// WHAT IS A STAND-IN. The decision is read from synthetic/proposer_swap/decisions.json, which the decision half
// (services/core/trust/decide/proposer_swap_test.go) writes by running the real decide.Admit and decide.FailAtGate on both
// proposers' Intent Objects, and which that test fails to match if it is stale. This package may not import trust (the
// import-boundary rule), so the two halves meet at that file. The grant is issued by the first end-to-end case's trustStandIn
// (stack_test.go), a stand-in for Trust, not Trust. Where the decision is not ALLOW, nothing is issued and nothing runs, as
// the core would do. No core package imports any of this.
//
// WHERE THE PROPOSALS COME FROM. synthetic/proposer_swap/proposals/*.json, written by the model service's tests
// (services/model/tests/test_proposer_swap.py, which fails if they are stale). Proposer A is a canned-proposal stub. Proposer B
// is the OpenAI Responses adapter replaying a canned response. Neither made a network or API call. The Intent Object follows
// SoT 43.1 and PERD 20.1 (contracts/schemas/intent-object.v1.json). What differs between the proposers (provider, model
// version, tokens) sits in the envelope's `proposer` block, which the run below is never given.
//
// WHAT IS ASSERTED. For the same canned decision the outcomes and the Record are identical under both proposers; a prohibited
// remedy and an invalid proposal are refused identically and write nothing; and the comparison is not vacuous: deliberately
// leaking the proposer into the decision path (the decision, a grant parameter) makes it fail.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/workflows"
)

// ---- the inputs ----

type swapDecision struct {
	GateStep      string    `json:"gate_step"`
	Outcome       string    `json:"outcome"`
	Reasons       []string  `json:"reasons"`
	Authority     [3]string `json:"authority"`
	IntentVersion int       `json:"intent_version"`
	Transaction   string    `json:"transaction"`
}

// proposal is what this half takes from an envelope: the Intent Object's id, and the provenance as bytes, so that no code here
// can read a field of it (only the leak controls look inside).
type proposal struct {
	IntentID   string
	Object     []byte
	Provenance []byte
}

func swapDir(t *testing.T) string { return filepath.Join(repoRoot(t), "synthetic", "proposer_swap") }

func loadDecision(t *testing.T, scenario string) swapDecision {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(swapDir(t), "decisions.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Synthetic bool                    `json:"synthetic"`
		Decisions map[string]swapDecision `json:"decisions"`
	}
	if err := json.Unmarshal(raw, &f); err != nil || !f.Synthetic {
		t.Fatalf("decisions.json: %v (synthetic=%v)", err, f.Synthetic)
	}
	d, ok := f.Decisions[scenario]
	if !ok {
		t.Fatalf("decisions.json has no decision for %q", scenario)
	}
	return d
}

func loadProposal(t *testing.T, scenario, proposer string) proposal {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(swapDir(t), "proposals", scenario+"."+proposer+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var env struct {
		Synthetic    bool            `json:"synthetic"`
		Proposer     json.RawMessage `json:"proposer"`
		IntentObject json.RawMessage `json:"intent_object"`
	}
	if err := json.Unmarshal(raw, &env); err != nil || !env.Synthetic {
		t.Fatalf("%s/%s: %v (synthetic=%v)", scenario, proposer, err, env.Synthetic)
	}
	p := proposal{Object: env.IntentObject, Provenance: env.Proposer}
	if string(env.IntentObject) != "null" {
		var obj struct {
			IntentID string `json:"intent_id"`
		}
		if err := json.Unmarshal(env.IntentObject, &obj); err != nil || obj.IntentID == "" {
			t.Fatalf("%s/%s: no intent_id in the Intent Object: %v", scenario, proposer, err)
		}
		p.IntentID = obj.IntentID
	}
	return p
}

// ---- running one proposal through the core, and what is observed ----

type observed struct {
	Decision swapDecision
	Workflow string // the EscalateResult as JSON with the mock-case-generated ids replaced
	Cases    string // mock-case's cases, the same way
	Record   []string
	Counts   map[string]int
}

// leak is a deliberate fault: a function that lets the proposer's provenance reach the decision path. The clean runs use
// none. The controls at the end of this file use them to show that the comparison notices.
type leak struct {
	name        string
	decision    func(provenance []byte, d *swapDecision)
	grantIntent func(provenance []byte) string
}

var (
	runsMu sync.Mutex
	runs   = map[string]observed{}
)

// runProposal runs one proposal through the real core. A clean run is computed once and shared by the tests.
func runProposal(t *testing.T, scenario, proposer string, l *leak) observed {
	t.Helper()
	key := scenario + "/" + proposer
	if l != nil {
		key += "/" + l.name
	}
	runsMu.Lock()
	defer runsMu.Unlock()
	if o, ok := runs[key]; ok {
		return o
	}
	o := runProposalOnce(t, scenario, proposer, l)
	runs[key] = o
	return o
}

func runProposalOnce(t *testing.T, scenario, proposer string, l *leak) observed {
	t.Helper()
	p := loadProposal(t, scenario, proposer)
	d := loadDecision(t, scenario)
	if l != nil && l.decision != nil {
		l.decision(p.Provenance, &d)
	}
	mc := startMockCase(t)
	st := newStack(t, mc)
	o := observed{Decision: d, Counts: map[string]int{}}

	var res *workflows.EscalateResult
	if d.Outcome == "ALLOW" {
		if d.Transaction != txRef {
			t.Fatalf("the proposal names transaction %q, the stack's fixture is %q", d.Transaction, txRef)
		}
		ref := p.IntentID
		if l != nil && l.grantIntent != nil {
			ref = l.grantIntent(p.Provenance)
		}
		params := map[string]any{"transaction_ref": txRef, "reason_code": reasonCode, "created_by_intent": ref, "epoch": int64(1)}
		createGrant, err := st.trust.issue(bg(t), bridge.CapCreateEscalationCase, txRef, params,
			controller.KeyFields{TransactionID: txRef, LogicalAction: "ESCALATE_CASE", ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"}, nil, false)
		if err != nil {
			t.Fatal(err)
		}
		r := runEscalate(t, st, createGrant)
		res = &r
	}
	n, err := chain.Verify(bg(t), st.admin, tenant)
	if err != nil {
		t.Fatalf("the Record chain does not verify: %v", err)
	}
	o.Counts["chain_verified_events"] = int(n)
	o.Record = recordRows(t, st, res)
	o.Workflow, o.Cases = resultAndCases(t, st, res)
	for name, q := range map[string]string{
		"grants": `SELECT count(*) FROM decision_grant`, "spends": `SELECT count(*) FROM grant_spend`,
		"receipts": `SELECT count(*) FROM execution_receipt`, "outbox": `SELECT count(*) FROM outbox_event`,
		"recovery": `SELECT count(*) FROM recovery_request`, "record": `SELECT count(*) FROM record_event`,
	} {
		o.Counts[name] = st.count(t, q)
	}
	o.Counts["mock_case_cases"] = st.mockCaseCount(t)
	return o
}

// recordRows reads every Record event of the tenant. Columns that hold values the mock-case generates (its case id and
// reference, which carry its own wall-clock time) are replaced by the same placeholder in both runs; the chain hashes
// that cover them cannot be compared, so the chain is verified in each run instead (chain.Verify) and not compared.
func recordRows(t *testing.T, st *stack, res *workflows.EscalateResult) []string {
	t.Helper()
	rows, err := st.admin.Query(bg(t), `SELECT seq, written_by, event_type, object_ref, correlation_id, at, schema_version, actor, body::text, payload::text
		FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var seq int64
		var by, typ, obj, corr, ver, actor, body string
		var payload *string
		var at any
		if err := rows.Scan(&seq, &by, &typ, &obj, &corr, &at, &ver, &actor, &body, &payload); err != nil {
			t.Fatal(err)
		}
		p := "<none>"
		if payload != nil {
			p = *payload
		}
		out = append(out, scrub(res, fmt.Sprintf("%d|%s|%s|%s|%s|%v|%s|%s|%s|%s", seq, by, typ, obj, corr, at, ver, actor, body, p)))
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func resultAndCases(t *testing.T, st *stack, res *workflows.EscalateResult) (string, string) {
	t.Helper()
	if res == nil {
		return "<no workflow>", "<no cases>"
	}
	wf, err := json.Marshal(res)
	if err != nil {
		t.Fatal(err)
	}
	cases := st.casesFor(t, res.Create.IdempotencyKey)
	cs, err := json.Marshal(cases)
	if err != nil {
		t.Fatal(err)
	}
	return scrub(res, string(wf)), scrub(res, string(cs))
}

// scrub replaces, with fixed placeholders, only what a run draws from entropy or from mock-case's own wall clock, and so
// differs between two runs of the very same decision:
//   - the random part of the ids the core mints (event ids "recev_<ULID>", receipt ids "rcp_<ULID>"; ids.New is given no
//     seeded reader by the stack, and the ULID entropy is random);
//   - the hashes and signatures that cover those ids (prev_hash and payload_hash in a Record event, the receipt signature),
//     which cannot be compared without them: the chain is verified in each run instead (chain.Verify) and the content the
//     hashes cover is compared;
//   - mock-case's case id and case reference, and the time it stamps on a case.
//
// Nothing a proposer or the core decides is replaced: event types, order, object refs, clock readings, grant ids, nonces,
// idempotency keys, params hashes, receipt states, queues, version tokens, statuses and counts all stay and are compared.
func scrub(res *workflows.EscalateResult, s string) string {
	if res != nil && res.ReadBack != nil {
		s = strings.ReplaceAll(s, res.ReadBack.CaseID, "<case_id>")
		s = strings.ReplaceAll(s, res.ReadBack.Reference, "<case_reference>")
	}
	s = caseCreatedAt.ReplaceAllString(s, `"CreatedAt":"<mock-case time>"`)
	s = mintedID.ReplaceAllString(s, "${1}_<random>")
	s = chainHash.ReplaceAllString(s, `"${1}": "<hash>"`)
	return receiptSig.ReplaceAllString(s, `"sig": {"value": "<hmac>"`)
}

var (
	caseCreatedAt = regexp.MustCompile(`"CreatedAt":"[^"]*"`)
	mintedID      = regexp.MustCompile(`\b(recev|rcp)_[0-9A-Z]{26}\b`)
	chainHash     = regexp.MustCompile(`"(prev_hash|payload_hash)": "sha256:[0-9a-f]{64}"`)
	receiptSig    = regexp.MustCompile(`"sig": \{"value": "[0-9a-f]{64}"`)
)

func (s *stack) mockCaseCount(t *testing.T) int {
	t.Helper()
	page, err := s.cases.FindCases(bg(t), adapters.CaseQuery{TenantID: tenant, PrincipalID: principal})
	if err != nil {
		t.Fatal(err)
	}
	return len(page.Items)
}

func diff(a, b observed) []string {
	var out []string
	add := func(name string, x, y any) {
		if !reflect.DeepEqual(x, y) {
			out = append(out, fmt.Sprintf("%s differs:\n    A: %v\n    B: %v", name, x, y))
		}
	}
	add("decision", a.Decision, b.Decision)
	add("workflow result", a.Workflow, b.Workflow)
	add("mock-case cases", a.Cases, b.Cases)
	add("counts", a.Counts, b.Counts)
	if len(a.Record) != len(b.Record) {
		out = append(out, fmt.Sprintf("record has %d events under A, %d under B", len(a.Record), len(b.Record)))
	}
	for i := 0; i < len(a.Record) && i < len(b.Record); i++ {
		add(fmt.Sprintf("record event %d", i+1), a.Record[i], b.Record[i])
	}
	sort.Strings(out)
	return out
}

// ---- the tests ----

// The same canned decision, from either proposer: the real core runs the escalation on the real mock-case and writes the
// same eight Record events.
func TestSwapSameDecisionGivesIdenticalOutcomesAndRecordUnderBothProposers(t *testing.T) {
	a, b := runProposal(t, "escalate", "A", nil), runProposal(t, "escalate", "B", nil)
	if d := diff(a, b); len(d) != 0 {
		t.Fatalf("the core behaved differently depending on the proposer:\n  %s", strings.Join(d, "\n  "))
	}
	// Independent of the comparison, the shared result is the right one (so two equally wrong runs would not pass).
	wantEvents := []string{"REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT", "REVALIDATION_PASSED", "GRANT_SPENT", "DISPATCH_ATTEMPTED", "RECEIPT"}
	var types []string
	for _, row := range a.Record {
		types = append(types, strings.Split(row, "|")[2])
	}
	if a.Decision.GateStep != "T7" || a.Decision.Outcome != "ALLOW" || !reflect.DeepEqual(types, wantEvents) ||
		a.Counts["chain_verified_events"] != len(wantEvents) || a.Counts["mock_case_cases"] != 1 || a.Counts["spends"] != 2 || !strings.Contains(a.Workflow, `"Status":"AWAITING_VERIFICATION"`) {
		t.Fatalf("the shared result is not the escalation of the first end-to-end case: %+v", a)
	}
}

// A prohibited remedy and an invalid proposal were refused by the real FailAtGate (the decision half); here the refusal is
// carried through the same way under both proposers, and nothing is written: no grant, no spend, no Record event, no case.
func TestSwapAProhibitedOrInvalidProposalIsRefusedIdenticallyAndWritesNothing(t *testing.T) {
	for _, tc := range []struct{ scenario, step, reason string }{
		{"prohibited", "T4", "PROHIBITED_CAPABILITY"},
		{"invalid", "T1", "INTENT_INVALID"},
	} {
		t.Run(tc.scenario, func(t *testing.T) {
			a, b := runProposal(t, tc.scenario, "A", nil), runProposal(t, tc.scenario, "B", nil)
			if d := diff(a, b); len(d) != 0 {
				t.Fatalf("the refusal depends on the proposer:\n  %s", strings.Join(d, "\n  "))
			}
			if a.Decision.Outcome != "REJECT" || a.Decision.GateStep != tc.step || !reflect.DeepEqual(a.Decision.Reasons, []string{tc.reason}) {
				t.Fatalf("decision %+v, want REJECT at %s with %s", a.Decision, tc.step, tc.reason)
			}
			for name, n := range a.Counts {
				if n != 0 {
					t.Errorf("%s = %d after a refusal, want 0", name, n)
				}
			}
			if len(a.Record) != 0 {
				t.Errorf("a refusal wrote %d Record events", len(a.Record))
			}
		})
	}
}

// The comparison is not vacuous. Each control lets the proposer's provenance reach the decision path in one way, and the test
// must see it. A control that is not seen fails the test, so a weakened comparison cannot pass quietly.
func TestSwapTheComparisonNoticesWhenProvenanceReachesTheDecisionPath(t *testing.T) {
	clean := runProposal(t, "escalate", "A", nil)
	controls := []leak{
		{name: "provenance-in-the-decision", decision: func(p []byte, d *swapDecision) {
			if bytes.Contains(p, []byte("openai")) {
				d.Outcome, d.GateStep, d.Reasons = "REJECT", "T7", []string{"AMBIGUOUS_COUNTERPARTY"}
			}
		}},
		{name: "provenance-in-the-grant-parameters", grantIntent: func(p []byte) string {
			if bytes.Contains(p, []byte("openai")) {
				return "int_openai"
			}
			return "int_swap01"
		}},
	}
	for _, l := range controls {
		l := l
		t.Run(l.name, func(t *testing.T) {
			d := diff(clean, runProposal(t, "escalate", "B", &l))
			if len(d) == 0 {
				t.Fatalf("provenance reached the decision path through %q and the comparison did not notice", l.name)
			}
			t.Logf("noticed: %s", strings.SplitN(d[0], "\n", 2)[0])
		})
	}
}

// The premise, seen from this side: the Intent Objects of the two proposers are byte for byte the same, and what differs
// between them is provenance, which the runs above are never given.
func TestSwapTheProposalsDifferOnlyInProvenance(t *testing.T) {
	for _, scenario := range []string{"escalate", "prohibited", "invalid"} {
		a, b := loadProposal(t, scenario, "A"), loadProposal(t, scenario, "B")
		if !bytes.Equal(a.Object, b.Object) {
			t.Errorf("%s: the Intent Objects differ between the proposers", scenario)
		}
		if bytes.Equal(a.Provenance, b.Provenance) {
			t.Errorf("%s: the provenance is the same, so the swap swaps nothing", scenario)
		}
	}
}
