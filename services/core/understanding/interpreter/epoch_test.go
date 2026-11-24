package interpreter_test

// DEBT-54 part 2 (ruling A520, interim; A513, A482; PERD 28.2): at admission the Interpreter asks an OPTIONAL epoch port for the logical resolution epoch of the intent's subject
// (the Evidence reader's case list, read without recording) and passes the epoch and the subject to Save, which stores them in the same transaction as INTENT_ADMITTED. The
// subject is the resolved reference of the slot the pack names `subject_slot`. With no epoch port nothing changes (epoch 1, no subject: the G1 path). An intent whose subject is
// not resolved has no subject and asks nothing. A port that cannot answer stops the compile before anything is saved: a guessed epoch would reuse an earlier epoch's idempotency
// key. The tenant and the principal asked about are the session's (INV-12). SYNTHETIC data.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

type spyEpochs struct {
	c     *calls
	epoch int
	err   error
	asked [][3]string
}

func (s *spyEpochs) EpochOf(_ context.Context, tenantID, principalID, subjectRef string) (int, error) {
	s.c.order = append(s.c.order, "epoch")
	s.asked = append(s.asked, [3]string{tenantID, principalID, subjectRef})
	return s.epoch, s.err
}

func epochWorld(t *testing.T, out []byte, epoch int, err error) (*world, *spyEpochs) {
	t.Helper()
	raw, rerr := os.ReadFile("../../../../packs/fintech_v1.json")
	if rerr != nil {
		t.Fatal(rerr)
	}
	view, verr := understanding.NewPackView(raw)
	if verr != nil {
		t.Fatal(verr)
	}
	w := newWorld(t, out)
	sp := &spyEpochs{c: w.calls, epoch: epoch, err: err}
	in, nerr := interpreter.New(interpreter.Config{Compiler: w.comp, Gate: mustGate(t), Recorder: w.rec, Saver: w.save, Subject: understanding.SubjectOf(view), Epochs: sp})
	if nerr != nil {
		t.Fatal(nerr)
	}
	w.in = in
	return w, sp
}

// With the port set and a resolved subject: asked once, in the session's scope, after the compile and before the save; the save carries the epoch and the subject.
func TestAResolvedSubjectAsksTheEpochPortAndTheSaveCarriesTheAnswer(t *testing.T) {
	w, sp := epochWorld(t, proposal(nil), 3, nil)
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if len(sp.asked) != 1 || sp.asked[0] != [3]string{session.TenantID, session.PrincipalID, "fin.transaction:TX88213"} {
		t.Fatalf("the epoch port was asked %v, want once for the session's tenant and principal and the subject", sp.asked)
	}
	if got := strings.Join(w.calls.order, ","); got != "compile,epoch,save" {
		t.Errorf("order %s, want compile,epoch,save", got)
	}
	if len(w.save.saved) != 1 || w.save.saved[0].LogicalResolutionEpoch != 3 || w.save.saved[0].SubjectRef != "fin.transaction:TX88213" {
		t.Errorf("saved %+v, want epoch 3 and the subject", w.save.saved)
	}
}

// INV-12: the tenant and the principal asked about are the session's, whatever the model's proposal says.
func TestTheEpochIsAskedInTheSessionsScopeWhateverTheModelWrote(t *testing.T) {
	w, sp := epochWorld(t, proposal(func(m map[string]any) {
		m["tenant_id"] = "ten_other"
		m["principal"] = map[string]any{"principal_id": "usr_other", "principal_type": "unset", "authn_ref": session.AuthnRef, "asserted_by": "unset"}
	}), 2, nil)
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if len(sp.asked) != 1 || sp.asked[0][0] != session.TenantID || sp.asked[0][1] != session.PrincipalID {
		t.Errorf("asked %v, want the session's tenant %s and principal %s", sp.asked, session.TenantID, session.PrincipalID)
	}
}

// A subject that is not resolved asks nothing and is saved with no subject (the store gives it epoch 1).
func TestAnUnresolvedSubjectAsksNothingAndIsSavedWithNoSubject(t *testing.T) {
	w, sp := epochWorld(t, proposal(func(m map[string]any) {
		m["slots"].(map[string]any)["subject_resource"] = map[string]any{"type": "Resource", "status": "UNRESOLVED"}
	}), 9, nil)
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if len(sp.asked) != 0 {
		t.Errorf("the epoch port was asked %v for an unresolved subject", sp.asked)
	}
	if len(w.save.saved) != 1 || w.save.saved[0].LogicalResolutionEpoch != 0 || w.save.saved[0].SubjectRef != "" {
		t.Errorf("saved %+v, want no epoch stated and no subject", w.save.saved)
	}
}

// A port that cannot answer stops the compile before the save: ErrEpoch, nothing saved, nothing recorded, and the cause is reachable but never printed.
func TestAnEpochPortThatCannotAnswerStopsTheCompileBeforeTheSave(t *testing.T) {
	cause := errors.New("the case list is incomplete: leaky-detail-0001")
	w, _ := epochWorld(t, proposal(nil), 0, cause)
	_, err := w.in.Interpret(context.Background(), input())
	if !errors.Is(err, interpreter.ErrEpoch) || !errors.Is(err, cause) {
		t.Fatalf("err %v, want ErrEpoch carrying its cause", err)
	}
	if strings.Contains(err.Error(), "leaky-detail") {
		t.Errorf("the error prints its cause: %q", err.Error())
	}
	if len(w.save.saved) != 0 || len(w.rec.rejected) != 0 {
		t.Errorf("saved %d, rejected %d; want nothing saved and nothing recorded", len(w.save.saved), len(w.rec.rejected))
	}
}

// An answer that is not a positive epoch is refused as the port's failure, never stored.
func TestAnEpochThatIsNotPositiveIsRefusedBeforeTheSave(t *testing.T) {
	for _, bad := range []int{0, -1} {
		w, _ := epochWorld(t, proposal(nil), bad, nil)
		if _, err := w.in.Interpret(context.Background(), input()); !errors.Is(err, interpreter.ErrEpoch) {
			t.Errorf("epoch %d: err %v, want ErrEpoch", bad, err)
		}
		if len(w.save.saved) != 0 {
			t.Errorf("epoch %d: saved %d, want none", bad, len(w.save.saved))
		}
	}
}

// Without the port nothing changes: the saved intent states no epoch and no subject, and no port is asked.
func TestWithNoEpochPortTheSavedIntentIsAsItWas(t *testing.T) {
	w := newWorld(t, proposal(nil))
	if _, err := w.in.Interpret(context.Background(), input()); err != nil {
		t.Fatal(err)
	}
	if len(w.save.saved) != 1 || w.save.saved[0].LogicalResolutionEpoch != 0 || w.save.saved[0].SubjectRef != "" {
		t.Errorf("saved %+v, want no epoch stated and no subject", w.save.saved)
	}
}

// New refuses half of the pair: a subject reader with no epoch port, or an epoch port with no subject reader.
func TestNewRefusesAnEpochPortWithoutASubjectReaderAndTheOtherWayRound(t *testing.T) {
	w := newWorld(t, nil)
	raw, _ := os.ReadFile("../../../../packs/fintech_v1.json")
	view, _ := understanding.NewPackView(raw)
	base := interpreter.Config{Compiler: w.comp, Gate: mustGate(t), Recorder: w.rec, Saver: w.save}
	onlyPort := base
	onlyPort.Epochs = &spyEpochs{c: w.calls}
	onlyReader := base
	onlyReader.Subject = understanding.SubjectOf(view)
	for name, c := range map[string]interpreter.Config{"an epoch port only": onlyPort, "a subject reader only": onlyReader} {
		if _, err := interpreter.New(c); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
