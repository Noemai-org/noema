package controller_test

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// TST-01 gap 1 (INV-07, REQ-EXE-005, REQ-EXE-007): the property is stated over every TransportClass that types.go declares, not only the
// seven faults of the simulated network. The world's faults never produce DUPLICATE_CREATE, VERSION_CONFLICT_409_412, NO_RESPONSE,
// ADAPTER_NON_RETRYABLE_DEFECT, a 2xx without an acknowledgement or a class nobody listed; this test sends each of them through Execute (E9, E10, E11).
// Everything here is SYNTHETIC.

// classOverride makes the one write and then reports the class the test chose, whatever the simulated network did.
type classOverride struct {
	w *World
	r controller.TransportResult
}

func (o classOverride) Dispatch(ctx context.Context, req controller.DispatchRequest) controller.TransportResult {
	_ = o.w.Dispatch(ctx, req)
	return o.r
}

// declaredTransportClasses reads the TransportClass constants out of types.go, so a class added there without a pinned expectation below fails here.
func declaredTransportClasses(t testing.TB) []string {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "types.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, d := range f.Decls {
		gd, ok := d.(*ast.GenDecl)
		if !ok || gd.Tok != token.CONST {
			continue
		}
		for _, s := range gd.Specs {
			vs := s.(*ast.ValueSpec)
			if id, ok := vs.Type.(*ast.Ident); !ok || id.Name != "TransportClass" {
				continue
			}
			for _, v := range vs.Values {
				lit, ok := v.(*ast.BasicLit)
				if !ok {
					t.Fatalf("a TransportClass constant that is not a string literal: %T", v)
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, s)
			}
		}
	}
	if len(out) < 10 {
		t.Fatalf("found %d transport classes in types.go, want at least the ten of E10", len(out))
	}
	return out
}

// specState is the table of PERD 27.1 E10, written out again here and not read from Classify: only a clear answer leaves OUTCOME_UNKNOWN.
func specState(class string, ack string) (controller.ExecutionState, bool) {
	switch class {
	case "ACK_2XX", "DUPLICATE_409_SAME_KEY":
		if ack == "" {
			return controller.StateOutcomeUnknown, true
		}
		return controller.StateAcknowledged, true
	case "VERSION_CONFLICT_409_412":
		return controller.StateRejectedStaleVersion, true
	case "REJECTED_4XX", "DUPLICATE_CREATE":
		return controller.StateRejectedByTarget, true
	case "ADAPTER_NON_RETRYABLE_DEFECT":
		return controller.StateFailedTerminal, true
	case "TIMEOUT", "CONNECTION_RESET", "SERVER_5XX", "NO_RESPONSE":
		return controller.StateOutcomeUnknown, true
	}
	return "", false
}

var silenceClasses = map[string]bool{"TIMEOUT": true, "CONNECTION_RESET": true, "SERVER_5XX": true, "NO_RESPONSE": true}

func checkINV07Class(t failer, class, ack string) {
	w := NewWorld(secret)
	g := fresh(w, w.Clock.Now())
	w.Store(g)
	c := controllerFor(w, nil)
	c.Out = classOverride{w: w, r: controller.TransportResult{Class: controller.TransportClass(class), ExternalAck: ack, RawCode: "synthetic"}}
	ctx, cancel := deadline()
	defer cancel()
	out, err := c.Execute(ctx, TenantID, g.Grant.GrantID)
	if err != nil || out.Receipt == nil {
		t.Fatalf("class %q ack %q: Execute gave %+v, %v; want a receipt", class, ack, out, err)
	}
	want, known := specState(class, ack)
	if !known {
		want = controller.StateOutcomeUnknown // a class nobody listed is not a clear answer
	}
	got := out.Receipt.State
	if got != want {
		t.Fatalf("class %q ack %q gave %s, want %s", class, ack, got, want)
	}
	if silenceClasses[class] && got != controller.StateOutcomeUnknown {
		t.Fatalf("silence %q gave %s, want OUTCOME_UNKNOWN", class, got)
	}
	if got == "SUCCEEDED" || got == "FAILED" {
		t.Fatalf("class %q gave %s: Execution never writes success or failure", class, got)
	}
	if got == controller.StateAcknowledged && (out.Receipt.ExternalAck == nil || *out.Receipt.ExternalAck == "") {
		t.Fatalf("class %q: ACKNOWLEDGED without an acknowledgement", class)
	}
	if n := len(w.Snapshot().Dispatches); n != 1 {
		t.Fatalf("class %q: %d dispatches, want exactly 1", class, n)
	}
}

func allClassesForINV07(t testing.TB) []string {
	declared := declaredTransportClasses(t)
	for _, c := range declared {
		if _, known := specState(c, "CMP-9"); !known {
			t.Fatalf("transport class %q is declared in types.go and has no pinned INV-07 expectation: classify it, then add it to specState", c)
		}
	}
	return append(declared, "SOMETHING_NEW") // a class that nobody listed is OUTCOME_UNKNOWN too
}

func TestINV07EveryTransportClassIsClassifiedAndOnlyAClearAnswerLeavesOutcomeUnknown(t *testing.T) {
	classes := allClassesForINV07(t)
	rapid.Check(t, func(t *rapid.T) {
		checkINV07Class(t, rapid.SampledFrom(classes).Draw(t, "class"), rapid.SampledFrom([]string{"", "CMP-9"}).Draw(t, "ack"))
	})
	// The same over every class and both acknowledgement states, with no seed.
	for _, class := range classes {
		for _, ack := range []string{"", "CMP-9"} {
			checkINV07Class(t, class, ack)
		}
	}
}
