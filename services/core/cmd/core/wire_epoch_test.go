package main

// UND-04, the epoch wiring (rulings A520, A513, A482, interim, relayed, not yet committed; REQ-INT-001, REQ-SEC-004; INV-12, INV-14, INV-19): an adapter turns the Interpreter's
// Epochs port into portread.Reader.NextEpoch, and Subject and Epochs are set together or not at all. The case source is a real implementation of adapters.Case that serves the
// cases it was given; the faults are real answers (an incomplete page, a failing read). Identifiers are SYNTHETIC.

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/epochport"
	"github.com/Noemai-org/noema/services/core/evidence/portread"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/understanding"
)

const epKind = "fin.transaction"

// epSetup is a real Reader with no case port: enough to tell a whole setup from a half one; what it answers is epochport's own test.
func epSetup() epochport.Setup {
	return epochport.Setup{Cases: &portread.Reader{ReadTimeout: time.Second}, Kind: epKind, Timeout: time.Second}
}

// packView is a real PackView that counts how often the subject function reads it: the wiring must use the view it was given (the gate's).
type packView struct{ nodes int }

func (*packView) Ref() (string, string) { return "fintech", "1" }
func (p *packView) Node(string) (understanding.Node, bool) {
	p.nodes++
	return understanding.Node{}, false
}

// Neither is the G1 behaviour: epoch 1 and no subject.
func TestEpochPartsWithNoSetupLeaveBothUnset(t *testing.T) {
	subject, epochs, err := epochParts(&packView{}, nil)
	if err != nil || subject != nil || epochs != nil {
		t.Fatalf("got %v, %v, %v; want nil, nil, nil", subject != nil, epochs != nil, err)
	}
}

func TestEpochPartsSetBothFromTheSamePackView(t *testing.T) {
	setup := epSetup()
	v := &packView{}
	subject, epochs, err := epochParts(v, &setup)
	if err != nil || subject == nil || epochs == nil {
		t.Fatalf("got %v, %v, %v; want both set", subject != nil, epochs != nil, err)
	}
	if _, ok := subject(understanding.AdmittedIntent{}); ok || v.nodes != 1 {
		t.Errorf("the subject function did not read the view it was given (reads %d, ok %v)", v.nodes, ok)
	}
}

// A half setup is a startup error with fixed text; it names no value and sets neither.
func TestEpochPartsRefuseAHalfSetupWithFixedText(t *testing.T) {
	for name, s := range map[string]epochport.Setup{
		"no cases":   {Kind: "fin.secretkind", Timeout: time.Second},
		"no kind":    {Cases: epSetup().Cases, Timeout: time.Second},
		"no timeout": {Cases: epSetup().Cases, Kind: "fin.secretkind"},
	} {
		subject, epochs, err := epochParts(&packView{}, &s)
		if !errors.Is(err, epochport.ErrRefused) || subject != nil || epochs != nil {
			t.Errorf("%s: got %v, %v, %v; want epochport.ErrRefused and neither set", name, subject != nil, epochs != nil, err)
		} else if strings.Contains(err.Error(), "secretkind") {
			t.Errorf("%s: the error repeats a value: %v", name, err)
		}
	}
	if _, _, err := epochParts(nil, &epochport.Setup{Cases: epSetup().Cases, Kind: epKind, Timeout: time.Second}); !errors.Is(err, epochport.ErrRefused) {
		t.Errorf("no pack view: got %v; want epochport.ErrRefused", err)
	}
}

// The setup reaches the build: a half setup stops it before any Interpreter is made, with the fixed text; with no setup the build is the one it was (it still needs the admissions).
func TestBuildUnderstandingTakesTheEpochSetupAndRefusesAHalfOne(t *testing.T) {
	m := newModelStub(t)
	env := Env{Getenv: undSettings(copyPack(t), m).get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	p, err := buildUnderstandingWith(env, clock.NewManual(apiEpoch), undFakeRecorder(t, "ten_aa"), undAdmissions{}, &epochport.Setup{Kind: epKind, Timeout: time.Second})
	if !errors.Is(err, epochport.ErrRefused) {
		t.Errorf("half setup: got %v; want epochport.ErrRefused", err)
	}
	wantNone(t, "half epoch setup", p)
	if _, err := buildUnderstandingWith(env, clock.NewManual(apiEpoch), undFakeRecorder(t, "ten_aa"), nil, nil); err == nil || !strings.Contains(err.Error(), "admissions") {
		t.Errorf("no setup, no admissions: got %v; want the admissions refusal", err)
	}
}
