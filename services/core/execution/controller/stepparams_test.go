package controller_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/signing/signingtest"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// A709 (SR's conditions on the Controller PR; A692; REQ-EXE-006, REQ-AUD-006, AGENT-13; INV-17): the receipt carries `step_params` for the bound params only, a personal identifier is
// masked or hashed, and the schema bounds the member. All values below are SYNTHETIC.

type policyStub struct {
	bound map[string]controller.ParamHandling
	err   error
	calls int
}

func (p *policyStub) Bound(string) (map[string]controller.ParamHandling, error) {
	p.calls++
	return p.bound, p.err
}

func hashed(t *testing.T, v any) string {
	t.Helper()
	h, err := crypto.HashOf(v)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func TestDiscloseParamsCarriesTheBoundParamsOnly(t *testing.T) {
	got, err := controller.DiscloseParams(
		map[string]any{"reason": "CREDIT_MISSING", "queue": "SYNTH_QUEUE", "amount": int64(5000), "flag": true},
		map[string]controller.ParamHandling{"reason": controller.HandlingVerbatim, "amount": controller.HandlingVerbatim, "flag": controller.HandlingVerbatim})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"reason": "CREDIT_MISSING", "amount": int64(5000), "flag": true}
	if len(got) != len(want) || got["reason"] != "CREDIT_MISSING" || got["amount"] != int64(5000) || got["flag"] != true {
		t.Fatalf("want %v, got %v", want, got)
	}
	if _, ok := got["queue"]; ok {
		t.Fatal("an unbound param must never be disclosed")
	}
}

func TestDiscloseParamsMasksAndHashesIdentifiers(t *testing.T) {
	got, err := controller.DiscloseParams(
		map[string]any{"account": "ACC-1234567890", "subject": "tx_SYNTH0001", "number": int64(1234567890)},
		map[string]controller.ParamHandling{"account": controller.HandlingMasked, "subject": controller.HandlingHashed, "number": controller.HandlingHashed})
	if err != nil {
		t.Fatal(err)
	}
	if got["account"] != "ACC-******7890" {
		t.Errorf("a masked identifier keeps its last four digits only, got %v", got["account"])
	}
	if got["subject"] != hashed(t, "tx_SYNTH0001") || got["number"] != hashed(t, int64(1234567890)) {
		t.Errorf("a hashed identifier is the canonical hash of its value, got %v", got)
	}
}

func TestDiscloseParamsRefusesWhatTheSchemaWouldRefuseAndWhatItCannotSay(t *testing.T) {
	v := controller.HandlingVerbatim
	cases := map[string]struct {
		params map[string]any
		bound  map[string]controller.ParamHandling
	}{
		"an unknown handling":      {map[string]any{"a": "x"}, map[string]controller.ParamHandling{"a": "AS_IS"}},
		"a nested object":          {map[string]any{"a": map[string]any{"b": "x"}}, map[string]controller.ParamHandling{"a": v}},
		"an array":                 {map[string]any{"a": []any{"x"}}, map[string]controller.ParamHandling{"a": v}},
		"a fractional number":      {map[string]any{"a": 1.5}, map[string]controller.ParamHandling{"a": v}},
		"a value past the length":  {map[string]any{"a": strings.Repeat("x", controller.MaxStepParamLength+1)}, map[string]controller.ParamHandling{"a": v}},
		"a masked non-string":      {map[string]any{"a": int64(12345678)}, map[string]controller.ParamHandling{"a": controller.HandlingMasked}},
		"a slot name out of shape": {map[string]any{"Bad-Name": "x"}, map[string]controller.ParamHandling{"Bad-Name": v}},
	}
	for name, c := range cases {
		if _, err := controller.DiscloseParams(c.params, c.bound); !errors.Is(err, controller.ErrStepParams) {
			t.Errorf("%s: want ErrStepParams, got %v", name, err)
		}
	}
	many, names := map[string]any{}, map[string]controller.ParamHandling{}
	for i := 0; i <= controller.MaxStepParams; i++ {
		k := "p" + strings.Repeat("a", i)
		many[k], names[k] = "x", v
	}
	if _, err := controller.DiscloseParams(many, names); !errors.Is(err, controller.ErrStepParams) {
		t.Errorf("more than MaxStepParams bound params: want ErrStepParams, got %v", err)
	}
}

func TestDiscloseParamsNeverReturnsAnEmptyMember(t *testing.T) {
	for name, bound := range map[string]map[string]controller.ParamHandling{"no binding": nil, "a binding the plan lacks": {"absent": controller.HandlingVerbatim}} {
		got, err := controller.DiscloseParams(map[string]any{"reason": "X"}, bound)
		if err != nil || got != nil {
			t.Errorf("%s: want nil and no error (the member is absent, never empty), got %v %v", name, got, err)
		}
	}
}

// The bounds in code are the bounds in the schema, so the signed receipt can never be one the contract refuses.
func TestTheBoundsAreTheSchemasBounds(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "execution-receipt.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties map[string]struct {
			MaxProperties        int `json:"maxProperties"`
			AdditionalProperties struct {
				MaxLength int `json:"maxLength"`
			} `json:"additionalProperties"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	sp := doc.Properties["step_params"]
	if sp.MaxProperties == 0 || sp.MaxProperties != controller.MaxStepParams || sp.AdditionalProperties.MaxLength == 0 || sp.AdditionalProperties.MaxLength != controller.MaxStepParamLength {
		t.Fatalf("schema maxProperties=%d maxLength=%d, code %d and %d", sp.MaxProperties, sp.AdditionalProperties.MaxLength, controller.MaxStepParams, controller.MaxStepParamLength)
	}
}

func TestTheReceiptCarriesTheBoundParamsOfTheExecutedStepUnderItsSignature(t *testing.T) {
	w, c, g := setup(t)
	pol := &policyStub{bound: map[string]controller.ParamHandling{"reason": controller.HandlingVerbatim, "subject": controller.HandlingHashed}}
	c.StepParams = pol
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Receipt == nil {
		t.Fatalf("%+v %v", out, err)
	}
	rc := out.Receipt
	if len(rc.StepParams) != 2 || rc.StepParams["reason"] != "CREDIT_MISSING" || rc.StepParams["subject"] != hashed(t, TargetRef) {
		t.Fatalf("step_params %v", rc.StepParams)
	}
	signed := string(rc.SignedJCS)
	if !strings.Contains(signed, `"step_params"`) || strings.Contains(signed, TargetRef) || strings.Contains(signed, "SYNTH_QUEUE") {
		t.Fatalf("the signed document must carry step_params, with no raw identifier and no unbound param: %s", signed)
	}
	if pol.calls != 1 {
		t.Fatalf("the policy is asked once, for the step's capability: %d", pol.calls)
	}
	if n := len(w.Snapshot().Dispatches); n != 1 {
		t.Fatalf("dispatches %d", n)
	}
}

func TestWithNoPolicyTheReceiptHasNoStepParams(t *testing.T) {
	_, c, g := setup(t)
	out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
	if err != nil || out.Receipt == nil {
		t.Fatalf("%+v %v", out, err)
	}
	if out.Receipt.StepParams != nil || strings.Contains(string(out.Receipt.SignedJCS), "step_params") {
		t.Fatalf("no policy, no member: %s", out.Receipt.SignedJCS)
	}
}

// A policy that cannot answer, or params that cannot be disclosed, stop the run BEFORE the spend: nothing is spent, recorded as dispatched or dispatched (EC-E16, INV-03).
func TestAPolicyThatFailsStopsTheRunBeforeTheSpend(t *testing.T) {
	for name, pol := range map[string]*policyStub{
		"the policy errors":      {err: errors.New("synthetic: policy unavailable")},
		"a param cannot be said": {bound: map[string]controller.ParamHandling{"reason": "AS_IS"}},
	} {
		w, c, g := setup(t)
		c.StepParams = pol
		c.Receipts = signingtest.Ring(t, 1)
		out, err := c.Execute(ctx(t), TenantID, g.Grant.GrantID)
		if err == nil || out.Receipt != nil {
			t.Fatalf("%s: want an error and no receipt, got %+v %v", name, out, err)
		}
		s := w.Snapshot()
		if len(s.Dispatches) != 0 || len(s.Record) != 1 {
			t.Fatalf("%s: nothing is dispatched and only the revalidation is recorded, got dispatches %d record %d", name, len(s.Dispatches), len(s.Record))
		}
	}
}
