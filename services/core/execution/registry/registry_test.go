package registry_test

// Tests of the Capability Registry. REQ-CAP-001 (complete descriptors only), REQ-CAP-002 (prohibited: registered,
// rejecting, unreachable), INV-05, INV-18, BD-08. Everything here is SYNTHETIC; the pack is the repository's own.

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const fintechPack = "../../../../packs/fintech_v1.json"

func loadFintech(t testing.TB) *registry.Registry {
	t.Helper()
	b, err := os.ReadFile(fintechPack)
	if err != nil {
		t.Fatal(err)
	}
	r, err := registry.Load(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("the fintech pack did not load: %v", err)
	}
	return r
}

func descriptors(t testing.TB) []contracts.CapabilityDescriptorV1 {
	t.Helper()
	b, err := os.ReadFile(fintechPack)
	if err != nil {
		t.Fatal(err)
	}
	var p contracts.PackV1
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	return p.Capabilities
}

func code(err error) registry.Code {
	var e *registry.Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

// REQ-CAP-002: the eight prohibited capabilities are registered, and asking for a handle to any of them is refused with
// CAPABILITY_PROHIBITED.
func TestEveryProhibitedCapabilityIsRegisteredAndRefused(t *testing.T) {
	r := loadFintech(t)
	if got := len(r.Prohibited()); got != 8 {
		t.Fatalf("%d prohibited capabilities registered, want the 8 of SoT §34.3", got)
	}
	for _, id := range r.Prohibited() {
		if !r.IsProhibited(id) {
			t.Errorf("%s is listed prohibited but IsProhibited says no", id)
		}
		if _, err := r.Callable(id); code(err) != registry.CodeProhibited {
			t.Errorf("Callable(%s) = %v, want CAPABILITY_PROHIBITED", id, err)
		}
	}
}

// INV-18: an unregistered ID has no handle either.
func TestAnUnregisteredCapabilityIsRefused(t *testing.T) {
	r := loadFintech(t)
	for _, id := range []string{"", "fin.cap.unknown", "FIN.CAP.TRANSFER_FUNDS", "fin.cap.transfer_funds ", "../fin.cap.lookup_account"} {
		if _, err := r.Callable(id); code(err) != registry.CodeNotRegistered {
			t.Errorf("Callable(%q) = %v, want CAPABILITY_NOT_REGISTERED", id, err)
		}
	}
}

func TestAllowedCapabilitiesGiveAHandleThatNamesThem(t *testing.T) {
	r := loadFintech(t)
	n := 0
	for _, d := range descriptors(t) {
		if d.Prohibited {
			continue
		}
		c, err := r.Callable(string(d.CapabilityId))
		if err != nil {
			t.Fatalf("Callable(%s): %v", d.CapabilityId, err)
		}
		if c.ID() != string(d.CapabilityId) {
			t.Errorf("handle names %s, want %s", c.ID(), d.CapabilityId)
		}
		n++
	}
	if n != 12 {
		t.Fatalf("%d allowed capabilities, want 12", n)
	}
}

// REQ-CAP-001: a descriptor missing any field is refused; the whole pack fails to load.
func TestAnIncompleteDescriptorFailsTheLoad(t *testing.T) {
	good := descriptors(t)
	mutators := map[string]func(*contracts.CapabilityDescriptorV1){
		"no consequence profile": func(d *contracts.CapabilityDescriptorV1) { d.ConsequenceProfile = nil },
		"no verification":        func(d *contracts.CapabilityDescriptorV1) { d.Verification = nil },
		"no recovery list":       func(d *contracts.CapabilityDescriptorV1) { d.Recovery = nil },
		"no limits":              func(d *contracts.CapabilityDescriptorV1) { d.Limits = nil },
		"no idempotency":         func(d *contracts.CapabilityDescriptorV1) { d.Idempotency = nil },
		"no authority":           func(d *contracts.CapabilityDescriptorV1) { d.Authority = nil },
		"no compensation":        func(d *contracts.CapabilityDescriptorV1) { d.Compensation = nil },
		"no expected outcome":    func(d *contracts.CapabilityDescriptorV1) { d.ExpectedOutcome = nil },
		"no retry":               func(d *contracts.CapabilityDescriptorV1) { d.Retry = nil },
		"no audit":               func(d *contracts.CapabilityDescriptorV1) { d.Audit = nil },
		"no input schema":        func(d *contracts.CapabilityDescriptorV1) { d.InputSchema = nil },
	}
	for name, mutate := range mutators {
		t.Run(name, func(t *testing.T) {
			for _, i := range []int{0, len(good) - 1} { // an allowed one and a prohibited one
				ds := append([]contracts.CapabilityDescriptorV1(nil), good...)
				mutate(&ds[i])
				if _, err := registry.New(ds); code(err) != registry.CodeIncomplete {
					t.Errorf("descriptor %d (%s): New = %v, want an incomplete-descriptor refusal", i, ds[i].CapabilityId, err)
				}
			}
		})
	}
}

func TestADuplicateIDFailsTheLoad(t *testing.T) {
	ds := descriptors(t)
	ds = append(ds, ds[0])
	if _, err := registry.New(ds); err == nil {
		t.Fatal("two descriptors with one ID were accepted")
	}
}

// INV-05, BD-08: a monetary-write descriptor that is not marked prohibited cannot be registered, whatever else it says.
func TestAMonetaryWriteThatIsNotProhibitedFailsTheLoad(t *testing.T) {
	ds := descriptors(t)
	checked := 0
	defer func() {
		if checked == 0 {
			t.Error("no monetary-write descriptor in the pack to test")
		}
	}()
	for i := range ds {
		if ds[i].Prohibited && strings.HasPrefix(ds[i].RiskClass, "R4") {
			ds[i].Prohibited = false
			checked++
			if _, err := registry.New(ds); code(err) != registry.CodeSafetyBlock {
				t.Fatalf("%s with prohibited=false: New = %v, want INTERNAL_SAFETY_BLOCK", ds[i].CapabilityId, err)
			}
			ds[i].Prohibited = true
		}
	}
}

// An empty registry is not a registry: nothing may be planned against nothing.
func TestAnEmptyRegistryFails(t *testing.T) {
	if _, err := registry.New(nil); err == nil {
		t.Fatal("an empty registry was accepted")
	}
}

func TestAPackThatIsNotValidJSONFails(t *testing.T) {
	for _, in := range []string{"", "{", `{"capabilities": 3}`, strings.Repeat("[", 10)} {
		if _, err := registry.Load(strings.NewReader(in)); err == nil {
			t.Errorf("Load(%q) succeeded", in)
		}
	}
}

// REQ-CAP-001: "complete descriptors only" includes "no descriptor member the contract does not define". A misspelled safety field
// must fail the load, not be ignored: the generated pack types are closed, and these cases keep it that way at every level
// (Codex review 5757658172 asked for this; the behaviour already held).
func TestAnUnknownMemberAtAnyLevelFailsTheLoad(t *testing.T) {
	raw, err := os.ReadFile(fintechPack)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]func(m map[string]any){
		"the pack":                 func(m map[string]any) { m["zz_unknown"] = 1 },
		"a capability":             func(m map[string]any) { first(m)["zz_unknown"] = 1 },
		"a misspelled safety flag": func(m map[string]any) { first(m)["prohibitted"] = true },
		"limits":                   func(m map[string]any) { first(m)["limits"].(map[string]any)["zz_unknown"] = 1 },
		"consequence_profile":      func(m map[string]any) { first(m)["consequence_profile"].(map[string]any)["zz_unknown"] = "x" },
		"authority":                func(m map[string]any) { first(m)["authority"].(map[string]any)["zz_unknown"] = "x" },
		"verification":             func(m map[string]any) { first(m)["verification"].(map[string]any)["zz_unknown"] = "x" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			var m map[string]any
			if err := json.Unmarshal(raw, &m); err != nil {
				t.Fatal(err)
			}
			mutate(m)
			out, err := json.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := registry.Load(bytes.NewReader(out)); code(err) != registry.CodeIncomplete {
				t.Fatalf("an unknown member in %s: Load = %v, want a SCHEMA_INVALID refusal", name, err)
			}
		})
	}
	// A repeated member is refused too (the strict decoder checks it), and so is anything after the pack.
	dup := strings.Replace(string(raw), `"capabilities"`, `"capabilities": [], "capabilities"`, 1)
	if _, err := registry.Load(strings.NewReader(dup)); err == nil {
		t.Error("a pack with a repeated member loaded")
	}
	if _, err := registry.Load(strings.NewReader(string(raw) + ` {"x": 1}`)); err == nil {
		t.Error("a pack with data after its end loaded")
	}
}

func first(m map[string]any) map[string]any { return m["capabilities"].([]any)[0].(map[string]any) }
