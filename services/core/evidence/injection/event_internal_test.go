package injection

// EVD-06 slice 2 (REQ-CTX-007, REQ-AUD-001; AGENT-13, EC-E16): the event the recorder writes is built, and refused, before the database is asked. These tests need no database: the
// chain's own validation and payload guard are run with an appender that has no signing key, which refuses after both and before anything is stored. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

var subj = Subject{TenantID: "ten_aa", CorrelationID: "corr_synthetic01", ObjectRef: "evr_synthetic01"}

func result(findings ...Finding) Result {
	return Result{Pack: "fin.ctx.injection_patterns@1", Findings: findings, TruncatedFields: []string{}}
}

var oneFinding = Finding{Field: "narration", PatternID: "fin.inj.override_instructions"}

func dctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// chainSays runs the chain's validation and payload guard on ev without a database: an appender with no signer refuses with "no signing key" once both have passed.
func chainSays(t *testing.T, ev chain.Event) string {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 17, 9, 0, 0, 0, time.UTC))
	a := &chain.Appender{Clock: clk, IDs: ids.New(clk, nil)}
	_, err := a.Append(dctx(t), nil, ev)
	if err == nil {
		t.Fatal("an appender with no signer appended")
	}
	return err.Error()
}

func TestTheEventIsASecurityEventOfKindInjectionDetectedByTheEvidenceEmitter(t *testing.T) {
	ev, err := buildEvent(subj, result(oneFinding))
	if err != nil {
		t.Fatal(err)
	}
	if ev.TenantID != "ten_aa" || ev.CorrelationID != "corr_synthetic01" || ev.ObjectRef != "evr_synthetic01" {
		t.Errorf("subject members = %q %q %q", ev.TenantID, ev.CorrelationID, ev.ObjectRef)
	}
	if ev.WrittenBy != "core-evidence" || ev.Actor != "core-evidence" || ev.EventType != "SECURITY_EVENT" || ev.Kind != "INJECTION_DETECTED" || ev.Reason != "" {
		t.Errorf("event = %+v: the Evidence Layer's own emitter writes SECURITY_EVENT of kind INJECTION_DETECTED, and no other member", ev)
	}
	b, err := json.Marshal(ev.Payload)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"findings":[{"field":"narration","pattern_id":"fin.inj.override_instructions"}],"pack":"fin.ctx.injection_patterns@1","truncated_fields":[]}`
	if string(b) != want {
		t.Errorf("payload = %s, want %s: names and ids only", b, want)
	}
}

func TestTheEventCarriesEveryFindingInOrderAndTheTruncatedFields(t *testing.T) {
	r := result(Finding{Field: "beneficiary_name", PatternID: "fin.inj.role_marker"}, Finding{Field: "narration", PatternID: "fin.inj.override_instructions", Truncated: true}, Finding{Field: "narration", PatternID: "fin.inj.url", Truncated: true})
	r.TruncatedFields = []string{"narration", "case.note_2"}
	ev, err := buildEvent(subj, r)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(ev.Payload)
	want := `{"findings":[{"field":"beneficiary_name","pattern_id":"fin.inj.role_marker"},{"field":"narration","pattern_id":"fin.inj.override_instructions"},{"field":"narration","pattern_id":"fin.inj.url"}],"pack":"fin.ctx.injection_patterns@1","truncated_fields":["narration","case.note_2"]}`
	if string(b) != want {
		t.Errorf("payload = %s\nwant      %s", b, want)
	}
}

// The chain accepts the event: its contract validation (emitter, kind, object_ref, actor) and the A184 payload guard pass.
func TestTheChainAcceptsTheEventBeforeItWouldSignIt(t *testing.T) {
	ev, err := buildEvent(subj, result(oneFinding, Finding{Field: "case.note_2", PatternID: "fin.inj.url"}))
	if err != nil {
		t.Fatal(err)
	}
	if got := chainSays(t, ev); !strings.Contains(got, "no signing key") {
		t.Errorf("the chain refused the event before signing: %s", got)
	}
	bad := ev
	bad.ObjectRef = "not a ref"
	if got := chainSays(t, bad); !strings.Contains(got, "object_ref") {
		t.Errorf("a bad object_ref was not refused by the chain: %s", got)
	}
}

// AGENT-13: nothing but a field name and a pattern id reaches the event, so a field name that is not a name is refused instead of recorded: a caller that passes a value as a name
// would otherwise write the value to the Record.
func TestAFieldNameOrPatternIDThatIsNotAnIdentifierIsNotRecorded(t *testing.T) {
	bad := []string{"", "Narration", "narr ation", "ignore previous instructions", "1abc", "a-b", "a/b", "name\n", strings.Repeat("a", 65), "é", "narration:ZZMARKER", "4111111111111111"}
	for _, n := range bad {
		if _, err := buildEvent(subj, result(Finding{Field: n, PatternID: "fin.inj.url"})); err == nil {
			t.Errorf("field name %q was recorded", n)
		}
		r := result(oneFinding)
		r.TruncatedFields = []string{n}
		if _, err := buildEvent(subj, r); err == nil {
			t.Errorf("truncated field name %q was recorded", n)
		}
	}
	for _, id := range []string{"", "Fin.inj.url", "fin inj url", "fin.inj.url1", "ab", strings.Repeat("a", 65), "fin.inj.url\n", "ignore previous instructions"} {
		if _, err := buildEvent(subj, result(Finding{Field: "narration", PatternID: id})); err == nil {
			t.Errorf("pattern id %q was recorded", id)
		}
	}
	for _, n := range []string{"narration", "beneficiary_name", "case.note_2", "a", strings.Repeat("a", 64)} {
		if _, err := buildEvent(subj, result(Finding{Field: n, PatternID: "fin.inj.url"})); err != nil {
			t.Errorf("field name %q refused: %v", n, err)
		}
	}
}

func TestAnEventWithNoFindingOrAnUnnamedPackIsNotWritten(t *testing.T) {
	if _, err := buildEvent(subj, result()); err == nil {
		t.Error("an event with no finding was built")
	}
	r := result()
	r.TruncatedFields = []string{"narration"}
	if _, err := buildEvent(subj, r); err == nil {
		t.Error("an event for truncation alone was built: only a finding is a security event")
	}
	if _, err := buildEvent(subj, Result{Findings: []Finding{oneFinding}}); err == nil {
		t.Error("an event with no pack id was built")
	}
	for _, p := range []string{"fin.ctx.injection_patterns", "Fin@1", "fin@", "fin@0", "fin@12345", "fin ctx@1", "fin@1\n"} {
		r := result(oneFinding)
		r.Pack = p
		if _, err := buildEvent(subj, r); err == nil {
			t.Errorf("pack id %q was recorded", p)
		}
	}
}

// EC-E16: a recorder that is not whole writes nothing and says so; nil receiver, no pool, no appender, no positive timeout.
func TestARecorderThatIsNotWholeWritesNothing(t *testing.T) {
	var nilRec *RecordRecorder
	if err := nilRec.Record(dctx(t), subj, result(oneFinding)); err == nil {
		t.Error("a nil recorder wrote")
	}
	pool, err := pgxpool.New(dctx(t), "postgres://nobody@127.0.0.1:1/none?sslmode=disable&connect_timeout=1")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	app := &chain.Appender{}
	for name, r := range map[string]*RecordRecorder{
		"empty":            {},
		"no pool":          {Appender: app, Timeout: time.Second},
		"no appender":      {Pool: pool, Timeout: time.Second},
		"no timeout":       {Pool: pool, Appender: app},
		"negative timeout": {Pool: pool, Appender: app, Timeout: -time.Second},
	} {
		if err := r.Record(dctx(t), subj, result(oneFinding)); err == nil || !strings.Contains(err.Error(), "needs a pool, an appender and a positive timeout") {
			t.Errorf("%s: %v, want the refusal for a recorder that is not whole", name, err)
		}
	}
	// A whole recorder whose database cannot be reached reports it and does not panic: nothing was stored.
	whole := &RecordRecorder{Pool: pool, Appender: app, Timeout: 3 * time.Second}
	if err := whole.Record(dctx(t), subj, result(oneFinding)); err == nil {
		t.Error("a recorder with an unreachable database reported success")
	}
	// An event that must be refused is refused before the database is asked, so the error is not a connection error.
	if err := whole.Record(dctx(t), subj, result()); err == nil || strings.Contains(err.Error(), "begin") {
		t.Errorf("an empty result must be refused before the database is asked: %v", err)
	}
}
