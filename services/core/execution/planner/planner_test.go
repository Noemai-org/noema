package planner_test

// PLN-01 slice 1: plan construction from registry handles and the canonical plan hash. REQ-PLN-001, REQ-PLN-002, INV-18.
// Everything is SYNTHETIC; the registry is the repository's own fintech pack.

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/planner"
	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// tb is what the helpers need of a test; both *testing.T and *rapid.T have it.
type tb interface {
	Helper()
	Fatal(args ...any)
}

var (
	regOnce sync.Once
	regVal  *registry.Registry
	regErr  error
)

// reg loads the pack once; the registry is read-only after that and safe to share.
func reg(t tb) *registry.Registry {
	t.Helper()
	regOnce.Do(func() {
		b, err := os.ReadFile("../../../../packs/fintech_v1.json")
		if err != nil {
			regErr = err
			return
		}
		regVal, regErr = registry.Load(bytes.NewReader(b))
	})
	if regErr != nil {
		t.Fatal(regErr)
	}
	return regVal
}

func allowed(t tb, r *registry.Registry) []string {
	t.Helper()
	var out []string
	for _, id := range r.IDs() {
		if r.Registered(id) {
			out = append(out, id)
		}
	}
	if len(out) == 0 {
		t.Fatal("no allowed capabilities")
	}
	return out
}

func code(err error) registry.Code {
	var e *registry.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func twoSteps() []planner.StepSpec {
	return []planner.StepSpec{
		{NodeID: "lookup_transaction", CapabilityID: "fin.cap.lookup_transaction", Params: map[string]any{"transaction_id": "txn_01"},
			Postconditions: []string{"exists(transaction where transaction.id == subject_resource)"}},
		{NodeID: "create_escalation_case", CapabilityID: "fin.cap.create_escalation_case",
			Params:        map[string]any{"owner_queue": "DISPUTES", "priority": int64(2), "urgent": false, "note": nil},
			Preconditions: []string{"true"}},
	}
}

// REQ-PLN-002: a plan of registered capabilities hashes, and the hash is SHA-256 of the RFC 8785 form of the plan document
// (computed here by a second route: decode the document and canonicalise the generic value).
func TestAPlanHashesToTheHashOfItsCanonicalDocument(t *testing.T) {
	r := reg(t)
	p, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", twoSteps())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := p.Hash()
	if err != nil {
		t.Fatalf("Hash: %v", err)
	}
	raw, err := json.Marshal(p.Document())
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var generic any
	if err := dec.Decode(&generic); err != nil {
		t.Fatal(err)
	}
	canon, err := crypto.CanonicalJSON(generic)
	if err != nil {
		t.Fatal(err)
	}
	if want := crypto.Hash(canon); got != want {
		t.Fatalf("Hash = %s, want %s", got, want)
	}
	if !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 {
		t.Fatalf("hash %q is not sha256:<64 hex>", got)
	}
	steps := p.Document().Steps
	if len(steps) != 2 || steps[0].Step != 0 || steps[1].Step != 1 {
		t.Fatalf("steps are not numbered 0, 1 in order: %+v", steps)
	}
}

// INV-18, REQ-CAP-002: none of the eight prohibited capabilities, and no unregistered ID, can be put in a plan.
func TestAPlanWithAProhibitedOrUnregisteredCapabilityCannotBeBuilt(t *testing.T) {
	r := reg(t)
	bad := append([]string{"fin.cap.unknown", "", "FIN.CAP.LOOKUP_ACCOUNT"}, r.Prohibited()...)
	for _, id := range bad {
		specs := twoSteps()
		specs[1].CapabilityID = id
		p, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", specs)
		want := registry.CodeNotRegistered
		if r.IsProhibited(id) {
			want = registry.CodeProhibited
		}
		if code(err) != want {
			t.Errorf("New with %q = %v, want %s", id, err, want)
		}
		if h, err := p.Hash(); err == nil || h != "" {
			t.Errorf("the zero plan after a refused build hashed to %q", h)
		}
	}
}

// REQ-PLN-002: a plan that arrives as a document (a stored row) is checked against the registry before it is hashed.
func TestADocumentWithAProhibitedOrUnregisteredIDCannotBeHashed(t *testing.T) {
	r := reg(t)
	good, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", twoSteps())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := planner.HashDocument(r, good.Document()); err != nil {
		t.Fatalf("a good document did not hash: %v", err)
	}
	for _, id := range append([]string{"fin.cap.unknown"}, r.Prohibited()...) {
		doc := good.Document()
		doc.Steps = append([]contracts.PlanV1StepsItem(nil), doc.Steps...)
		doc.Steps[1].CapabilityId = contracts.CommonV1PackId(id)
		if h, err := planner.HashDocument(r, doc); err == nil || h != "" {
			t.Errorf("a document naming %q hashed to %q", id, h)
		}
	}
	for name, mut := range map[string]func(*contracts.PlanV1){
		"no steps":         func(d *contracts.PlanV1) { d.Steps = nil },
		"a gap in numbers": func(d *contracts.PlanV1) { d.Steps[1].Step = 5 },
		"repeated number":  func(d *contracts.PlanV1) { d.Steps[1].Step = 0 },
		"no graph version": func(d *contracts.PlanV1) { d.GraphVersion = "" },
		"no intent node":   func(d *contracts.PlanV1) { d.IntentNode = "" },
		// Codex review of noema#142: only plan.v1 at schema version 1.0 has a hash (REQ-PLN-002, INV-18).
		"no schema version":      func(d *contracts.PlanV1) { d.SchemaVersion = "" },
		"a future schema":        func(d *contracts.PlanV1) { d.SchemaVersion = "2.0" },
		"a near miss of 1.0":     func(d *contracts.PlanV1) { d.SchemaVersion = "1.0.0" },
		"a padded schema string": func(d *contracts.PlanV1) { d.SchemaVersion = " 1.0" },
	} {
		doc := good.Document()
		doc.Steps = append([]contracts.PlanV1StepsItem(nil), doc.Steps...)
		mut(&doc)
		if h, err := planner.HashDocument(r, doc); err == nil || h != "" {
			t.Errorf("%s: hashed to %q", name, h)
		}
	}
}

// A float, or any other type outside string, integer, boolean and null, never reaches a plan (plan.v1 params).
func TestParamsAreStringsIntegersBooleansAndNullOnly(t *testing.T) {
	r := reg(t)
	for name, v := range map[string]any{"float": math.Pi, "slice": []string{"a"}, "map": map[string]any{"a": 1}, "struct": struct{}{}} {
		specs := twoSteps()
		specs[1].Params = map[string]any{"p": v}
		if _, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", specs); err == nil {
			t.Errorf("%s: accepted as a param", name)
		}
	}
	specs := twoSteps()
	specs[1].Params = map[string]any{"a": 1, "b": int32(2), "c": int64(3), "d": true, "e": "x", "f": nil}
	if _, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", specs); err != nil {
		t.Errorf("integers, a boolean, a string and null were refused: %v", err)
	}
}

func TestAPlanNeedsStepsAndAVersion(t *testing.T) {
	r := reg(t)
	if _, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", nil); err == nil {
		t.Error("a plan with no steps was built")
	}
	if _, err := planner.New(r, "", "fin.payments.exception.investigate", twoSteps()); err == nil {
		t.Error("a plan with no graph version was built")
	}
	if _, err := planner.New(r, "1.0.0", "", twoSteps()); err == nil {
		t.Error("a plan with no intent node was built")
	}
	if _, err := planner.New(nil, "1.0.0", "x", twoSteps()); err == nil {
		t.Error("a plan was built without a registry")
	}
}

// A Plan is immutable once built: the document it returns is a deep copy, so no caller can change the plan, or what it hashes to,
// by writing into the result (Codex review 5757658878). Every nested collection is mutated below.
func TestMutatingTheReturnedDocumentCannotChangeThePlan(t *testing.T) {
	r := reg(t)
	p, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", twoSteps())
	if err != nil {
		t.Fatal(err)
	}
	before, err := p.Hash()
	if err != nil {
		t.Fatal(err)
	}
	raw0, _ := json.Marshal(p.Document())

	d := p.Document()
	d.GraphVersion = "9.9.9"
	d.IntentNode = "fin.other"
	d.Steps[0].NodeId = "changed"
	d.Steps[0].CapabilityId = "fin.cap.unknown"
	for k, v := range d.Steps[0].Params { // the bytes of a param value are shared unless they are copied
		if len(v) > 1 {
			v[1] = 'X'
		}
		d.Steps[0].Params[k] = v
	}
	d.Steps[0].Params["added"] = []byte(`"new"`)
	delete(d.Steps[1].Params, "owner_queue")
	for i := range d.Steps[0].ExpectedPostconditions {
		d.Steps[0].ExpectedPostconditions[i] = "changed"
	}
	for i := range d.Steps[1].Preconditions {
		d.Steps[1].Preconditions[i] = "changed"
	}
	d.Steps = d.Steps[:1]

	after, err := p.Hash()
	if err != nil || after != before {
		t.Fatalf("the hash changed after the returned document was mutated: %s -> %s (%v)", before, after, err)
	}
	if raw1, _ := json.Marshal(p.Document()); !bytes.Equal(raw0, raw1) {
		t.Fatalf("the plan's document changed:\nbefore %s\nafter  %s", raw0, raw1)
	}
}

// The input is copied too: changing the specs a plan was built from, after New, cannot change the plan.
func TestMutatingTheInputSpecsAfterBuildingCannotChangeThePlan(t *testing.T) {
	r := reg(t)
	specs := twoSteps()
	p, err := planner.New(r, "1.0.0", "fin.payments.exception.investigate", specs)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := p.Hash()
	specs[0].Params["transaction_id"] = "txn_CHANGED"
	specs[0].Params["added"] = "x"
	specs[0].Postconditions[0] = "changed"
	specs[1].Preconditions[0] = "changed"
	specs[1].NodeID = "changed"
	if after, _ := p.Hash(); after != before {
		t.Fatalf("the hash followed the caller's later edits: %s -> %s", before, after)
	}
}
