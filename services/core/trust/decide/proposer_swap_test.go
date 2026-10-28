package decide_test

// The decision half of the proposer swap test (status/ENV-08.md).
//
// WHAT THIS VALIDATES. The decision path only: the real Admit, FailAtGate and Allow of this package, run on the Intent Object
// that each of two proposers handed over for the same canned decision. It does NOT validate the unbuilt intake, the compile
// endpoint, the schema gate or the complete architecture.
//
// WHAT IS A STAND-IN. `intake` below is a stand-in for the Trust intake, not the intake itself. The intake is not built:
// nothing in services/core parses an Intent Object or writes INTENT_* or DECISION_MADE to the Record. The stand-in is test
// code only; no core package imports it. It does the work PERD §23.3 gives Trust at T1 (an admitted Intent Object: status,
// signature, tenant) and T4 (a requested remedy that is registered and not PROHIBITED), then hands the Intent Object's slots
// to the real Admit (T7). The three authority layers are fixed to PASS because the authority and policy engines are not built;
// that is the only input the stand-in chooses, and it is the same for both proposers.
//
// WHERE THE PROPOSALS COME FROM. synthetic/proposer_swap/proposals/*.json, written by the model service's tests, which fail if
// the files are stale. Proposer A is a canned-proposal stub. Proposer B is the OpenAI Responses adapter replaying a canned
// response. Neither made a network or API call. The Intent Object follows SoT §43.1 and PERD §20.1
// (contracts/schemas/intent-object.v1.json). What differs between the proposers (provider, model version, tokens) sits in the
// envelope's `proposer` block, which `intake` is never given.
//
// THE LINK TO THE EXECUTION HALF. This test computes the decision for each scenario and each proposer, requires the two
// proposers' decisions to be equal, and requires them to equal the committed synthetic/proposer_swap/decisions.json
// (the drift guard: it fails if the file a run would write differs from the committed one; run it with
// NOEMA_WRITE_SWAP_DECISIONS=1 to write the file). The execution half, execution/e2e/proposer_swap_test.go, reads that
// committed file and runs the real EscalateCase on mock-case. tools/lint keeps trust and execution apart (import-boundary), so
// the two halves meet only at that file, as the two languages' halves meet only at the proposal files.

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// ---- the proposal files ----

type swapSlot struct {
	Type          string      `json:"type"`
	Status        string      `json:"status"`
	ResolvedRef   string      `json:"resolved_ref"`
	CandidatesRef *string     `json:"candidates_ref"`
	Value         any         `json:"value"`
	Unit          string      `json:"unit"`
	SurfaceForm   string      `json:"surface_form"`
	Confidence    json.Number `json:"confidence"`
	Window        [2]string   `json:"window"`
}

// swapIntent mirrors contracts/schemas/intent-object.v1.json and is decoded with DisallowUnknownFields, as the schema refuses
// unknown fields (SoT §43.1). Numbers stay as written (json.Number).
type swapIntent struct {
	SchemaVersion string  `json:"schema_version"`
	IntentID      string  `json:"intent_id"`
	Version       int     `json:"version"`
	Supersedes    *string `json:"supersedes"`
	TenantID      string  `json:"tenant_id"`
	CorrelationID string  `json:"correlation_id"`
	CreatedAt     string  `json:"created_at"`
	Principal     struct {
		PrincipalID   string `json:"principal_id"`
		PrincipalType string `json:"principal_type"`
		AuthnRef      string `json:"authn_ref"`
		AssertedBy    string `json:"asserted_by"`
	} `json:"principal"`
	Source struct {
		Mode          string   `json:"mode"`
		Channel       string   `json:"channel"`
		LocaleHeard   []string `json:"locale_detected"`
		ASRConfidence any      `json:"asr_confidence"`
		RawRef        string   `json:"raw_ref"`
	} `json:"source"`
	Pack struct {
		PackID      string `json:"pack_id"`
		PackVersion string `json:"pack_version"`
	} `json:"pack"`
	IntentNode        string              `json:"intent_node"`
	Slots             map[string]swapSlot `json:"slots"`
	AmbiguityFlags    []string            `json:"ambiguity_flags"`
	DesiredOutcomeRef []string            `json:"desired_outcome_refs"`
	Status            string              `json:"status"`
	Sig               struct {
		KeyID string `json:"key_id"`
		Value string `json:"value"`
	} `json:"sig"`

	raw []byte // the Intent Object exactly as received, for the signature
}

type swapEnvelope struct {
	Synthetic bool            `json:"synthetic"`
	Scenario  string          `json:"scenario"`
	Proposer  json.RawMessage `json:"proposer"` // provenance, kept as bytes so no code here can read a field of it
	Pipeline  struct {
		Outcome    string   `json:"outcome"`
		GateErrors []string `json:"gate_errors"`
	} `json:"pipeline"`
	IntentObject json.RawMessage `json:"intent_object"`
}

func repoRoot(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "packs", "fintech_v1.json")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("cannot find packs/fintech_v1.json above the test's directory")
		}
		dir = parent
	}
}

func swapDir(t testing.TB) string {
	t.Helper()
	return filepath.Join(repoRoot(t), "synthetic", "proposer_swap")
}

func loadProposal(t *testing.T, scenario, proposer string) (swapEnvelope, *swapIntent) {
	t.Helper()
	path := filepath.Join(swapDir(t), "proposals", scenario+"."+proposer+".json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var env swapEnvelope
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&env); err != nil || !env.Synthetic {
		t.Fatalf("%s: %v (synthetic=%v)", path, err, env.Synthetic)
	}
	if string(env.IntentObject) == "null" {
		return env, nil
	}
	var obj swapIntent
	idec := json.NewDecoder(bytes.NewReader(env.IntentObject))
	idec.DisallowUnknownFields()
	idec.UseNumber()
	if err := idec.Decode(&obj); err != nil {
		t.Fatalf("%s: the Intent Object does not decode against the contract's fields: %v", path, err)
	}
	obj.raw = env.IntentObject
	return env, &obj
}

// ---- the stand-in for the Trust intake ----

// swapKey is the synthetic model-service key of the proposal files (PERD §20.1: sig is an HMAC over the canonical JSON). It is
// not a secret.
var swapKey = []byte("synthetic-swap-test-key-not-a-secret")

const sessionTenant = "ten_aa" // the tenant of the authenticated session (INV-12)

type swapPack struct {
	Prohibited map[string]bool     // capability id -> prohibited
	Required   map[string][]string // intent node -> required slot names
	Material   map[string]bool     // material ambiguity flags
}

func loadSwapPack(t *testing.T) swapPack {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(repoRoot(t), "packs", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var p struct {
		Capabilities []struct {
			ID         string `json:"capability_id"`
			Prohibited bool   `json:"prohibited"`
		} `json:"capabilities"`
		Nodes []struct {
			ID    string `json:"node_id"`
			Slots []struct {
				Name     string `json:"name"`
				Required bool   `json:"required"`
			} `json:"slots"`
		} `json:"intent_nodes"`
		Understanding struct {
			MaterialFlags []string `json:"material_flags"`
		} `json:"understanding"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	out := swapPack{Prohibited: map[string]bool{}, Required: map[string][]string{}, Material: map[string]bool{}}
	for _, c := range p.Capabilities {
		out.Prohibited[c.ID] = c.Prohibited
	}
	for _, n := range p.Nodes {
		for _, s := range n.Slots {
			if s.Required {
				out.Required[n.ID] = append(out.Required[n.ID], s.Name)
			}
		}
	}
	for _, f := range p.Understanding.MaterialFlags {
		out.Material[f] = true
	}
	return out
}

// swapDecision is what the stand-in and the real constructors produced; it is also the row of decisions.json.
type swapDecision struct {
	GateStep      string    `json:"gate_step"`
	Outcome       string    `json:"outcome"`
	Reasons       []string  `json:"reasons"`
	Authority     [3]string `json:"authority"`
	IntentVersion int       `json:"intent_version"`
	Transaction   string    `json:"transaction"` // the target of an ALLOW, from the resolved subject_resource; empty otherwise
}

func describe(step string, d decide.Decision, transaction string) swapDecision {
	a := d.Authority()
	out := swapDecision{GateStep: step, Outcome: string(d.Outcome()), Reasons: []string{}, IntentVersion: d.IntentVersion(), Transaction: transaction,
		Authority: [3]string{string(a.Technical), string(a.Policy), string(a.Business)}}
	for _, r := range d.Reasons() {
		out.Reasons = append(out.Reasons, r.String())
	}
	return out
}

// intake is the stand-in for the Trust intake, not the intake itself. It is given the Intent Object and the session tenant and
// nothing else: not the envelope, so nothing about who proposed it. `extra` exists only for the control below.
func intake(t *testing.T, obj *swapIntent, pack swapPack, extra func(flags *[]string)) swapDecision {
	t.Helper()
	fail := func(step decide.GateStep, name string, version int) swapDecision {
		d, err := decide.FailAtGate(step, version)
		if err != nil {
			t.Fatal(err)
		}
		return describe(name, d, "")
	}
	// T1: an admitted Intent Object: status, signature, tenant (PERD §23.3). The schema is held by the model service's test
	// against the contract, and by the strict decode above.
	if obj == nil || obj.Status != "VALIDATED" || obj.TenantID != sessionTenant || !validSig(t, obj) {
		version := 0
		if obj != nil {
			version = obj.Version
		}
		return fail(decide.T1AdmittedIntent, "T1", version)
	}
	// T4: a requested remedy that is registered and not PROHIBITED (PERD §23.3).
	if remedy, ok := obj.Slots["requested_remedy"]; ok {
		if v, _ := remedy.Value.(string); pack.Prohibited[v] {
			return fail(decide.T4Capability, "T4", obj.Version)
		}
	}
	// T7: material ambiguity and unresolved required slots, by the real Admit.
	flags := append([]string(nil), obj.AmbiguityFlags...)
	if extra != nil {
		extra(&flags)
	}
	view := decide.IntentView{Version: obj.Version, RequiredSlots: map[string]decide.SlotStatus{}}
	for _, f := range flags {
		if pack.Material[f] {
			view.MaterialFlags = append(view.MaterialFlags, f)
		}
	}
	for _, name := range pack.Required[obj.IntentNode] {
		status := decide.SlotAbsent
		if s, ok := obj.Slots[name]; ok {
			status = decide.SlotStatus(s.Status)
		}
		view.RequiredSlots[name] = status
	}
	adm := decide.Admit(view)
	if resolved, ok := adm.Resolved(); ok {
		// The authority and policy engines are not built: the three layers are PASS for every proposer (stand-in).
		d, err := resolved.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
		if err != nil {
			t.Fatal(err)
		}
		ref := obj.Slots["subject_resource"].ResolvedRef
		return describe("T7", d, ref[strings.LastIndex(ref, ":")+1:])
	}
	unresolved, _ := adm.Unresolved()
	reason, err := decide.Core("AMBIGUOUS_COUNTERPARTY")
	if err != nil {
		t.Fatal(err)
	}
	d, err := unresolved.Escalate(reason)
	if err != nil {
		t.Fatal(err)
	}
	return describe("T7", d, "")
}

// validSig checks the model-service HMAC over the canonical JSON of the Intent Object without its sig (PERD §20.1). The bytes
// are the ones received, re-encoded with sorted keys and every number kept as written, so the check does not depend on how
// this side would format a float.
func validSig(t *testing.T, obj *swapIntent) bool {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(obj.raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatal(err)
	}
	delete(m, "sig")
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, swapKey)
	mac.Write(bytes.TrimRight(buf.Bytes(), "\n"))
	return hmac.Equal([]byte(obj.Sig.Value), []byte(base64.StdEncoding.EncodeToString(mac.Sum(nil))))
}

// ---- the tests ----

var swapScenarios = []string{"escalate", "prohibited", "invalid"}

type swapDecisions struct {
	Synthetic bool                    `json:"synthetic"`
	Note      string                  `json:"note"`
	Decisions map[string]swapDecision `json:"decisions"`
}

const decisionsNote = "SYNTHETIC. The decision of the real trust/decide constructors for each scenario, identical under both proposers. Written by TestSwapBothProposersGetTheSameDecisionAndTheCommittedFileIsCurrent (NOEMA_WRITE_SWAP_DECISIONS=1); read by the execution half."

func renderDecisions(d swapDecisions) []byte {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	_ = enc.Encode(d)
	return buf.Bytes()
}

// For the same canned decision the real decide constructors give the same decision under either proposer; an ALLOW for the
// escalation, REJECT at T4 for the prohibited remedy, REJECT at T1 for the invalid proposal. The file the execution half reads
// is exactly what this produced (the drift guard).
func TestSwapBothProposersGetTheSameDecisionAndTheCommittedFileIsCurrent(t *testing.T) {
	pack := loadSwapPack(t)
	got := swapDecisions{Synthetic: true, Note: decisionsNote, Decisions: map[string]swapDecision{}}
	for _, scenario := range swapScenarios {
		_, a := loadProposal(t, scenario, "A")
		_, b := loadProposal(t, scenario, "B")
		da, db := intake(t, a, pack, nil), intake(t, b, pack, nil)
		if !reflect.DeepEqual(da, db) {
			t.Fatalf("%s: the decision depends on the proposer:\n  A: %+v\n  B: %+v", scenario, da, db)
		}
		got.Decisions[scenario] = da
	}
	// Independent of the comparison and of the file, the decisions are the right ones.
	want := map[string]swapDecision{
		"escalate":   {GateStep: "T7", Outcome: "ALLOW", Reasons: []string{}, Authority: [3]string{"PASS", "PASS", "PASS"}, IntentVersion: 1, Transaction: "TX1"},
		"prohibited": {GateStep: "T4", Outcome: "REJECT", Reasons: []string{"PROHIBITED_CAPABILITY"}, Authority: [3]string{"NOT_EVALUATED", "NOT_EVALUATED", "NOT_EVALUATED"}, IntentVersion: 1},
		"invalid":    {GateStep: "T1", Outcome: "REJECT", Reasons: []string{"INTENT_INVALID"}, Authority: [3]string{"NOT_EVALUATED", "NOT_EVALUATED", "NOT_EVALUATED"}},
	}
	if !reflect.DeepEqual(got.Decisions, want) {
		t.Fatalf("decisions %+v, want %+v", got.Decisions, want)
	}
	path := filepath.Join(swapDir(t), "decisions.json")
	fresh := renderDecisions(got)
	if os.Getenv("NOEMA_WRITE_SWAP_DECISIONS") == "1" {
		if err := os.WriteFile(path, fresh, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	committed, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(committed, fresh) {
		t.Fatalf("synthetic/proposer_swap/decisions.json is not what the real decide constructors produce now; run this test with NOEMA_WRITE_SWAP_DECISIONS=1 and commit the file.\ncommitted:\n%s\nnow:\n%s", committed, fresh)
	}
}

// The comparison is not vacuous. This control lets the proposer's provenance reach the decision path (it adds a material
// ambiguity flag when the proposer is not the stub) and the test must see the decision change. If it does not, the comparison
// above could not have caught a real leak.
func TestSwapTheComparisonNoticesWhenProvenanceReachesTheDecision(t *testing.T) {
	pack := loadSwapPack(t)
	envA, a := loadProposal(t, "escalate", "A")
	envB, b := loadProposal(t, "escalate", "B")
	leak := func(provenance []byte) func(*[]string) {
		return func(flags *[]string) {
			if bytes.Contains(provenance, []byte("openai")) {
				*flags = append(*flags, "MULTIPLE_INTENTS")
			}
		}
	}
	clean, leaked := intake(t, a, pack, leak(envA.Proposer)), intake(t, b, pack, leak(envB.Proposer))
	if reflect.DeepEqual(clean, leaked) {
		t.Fatalf("provenance reached the decision path and the decision did not change: %+v", leaked)
	}
	t.Logf("noticed: A %s/%s, B %s/%s", clean.Outcome, clean.GateStep, leaked.Outcome, leaked.GateStep)
}

// The premise, seen from this side: the two proposers' Intent Objects are byte for byte the same, and what differs between
// them is provenance, which the stand-in is never given.
func TestSwapTheProposalsDifferOnlyInProvenance(t *testing.T) {
	for _, scenario := range swapScenarios {
		a, _ := loadProposal(t, scenario, "A")
		b, _ := loadProposal(t, scenario, "B")
		if !bytes.Equal(a.IntentObject, b.IntentObject) {
			t.Errorf("%s: the Intent Objects differ between the proposers", scenario)
		}
		if bytes.Equal(a.Proposer, b.Proposer) {
			t.Errorf("%s: the provenance is the same, so the swap swaps nothing", scenario)
		}
		if a.Pipeline.Outcome != b.Pipeline.Outcome || !reflect.DeepEqual(a.Pipeline.GateErrors, b.Pipeline.GateErrors) {
			t.Errorf("%s: the pipeline outcome differs: %+v vs %+v", scenario, a.Pipeline, b.Pipeline)
		}
	}
}
