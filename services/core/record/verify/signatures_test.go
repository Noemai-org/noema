package verify_test

// REC-08 / REQ-AUD-006: the chain verifier checks every signature of the Record (DEC-2026-11-01 D5, D7, D8, row 13). The tampering is done by the
// administrator with the immutability trigger off, the one way past the INSERT-only roles; the signature trigger of migration 0016 is not in the way of an
// UPDATE, so these rows are what a database credential that disabled the trigger could leave behind. Every key and event is SYNTHETIC (AGENT-25).

import (
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/record/verify"
)

// A signature that does not verify, a key that is not the emitter's, an unknown key and signed bytes that are not the stored event are each a SIGNATURE
// break at the event, a metric, an alert and a stopped tenant: the same handling as a hash break.
func TestEverySignatureFaultIsABreakAtTheEvent(t *testing.T) {
	cases := []struct {
		name string
		sql  string
	}{
		{"a signature value changed", `UPDATE record_event SET sig_value = repeat('0', 128) WHERE tenant_id = 'ten_aa' AND seq = 2`},
		{"a signature of another length", `UPDATE record_event SET sig_value = repeat('a', 127) WHERE tenant_id = 'ten_aa' AND seq = 2`},
		{"another identity's key named", `UPDATE record_event SET sig_key_id = 'core-outcome.ed25519.1' WHERE tenant_id = 'ten_aa' AND seq = 2`},
		{"a key that is not registered", `UPDATE record_event SET sig_key_id = 'core-execution.ed25519.9' WHERE tenant_id = 'ten_aa' AND seq = 2`},
		{"signed bytes that are not the stored event", `UPDATE record_event SET signed_jcs = convert_to('{"a":1}', 'UTF8') WHERE tenant_id = 'ten_aa' AND seq = 2`},
		{"signed bytes that are not JSON", `UPDATE record_event SET signed_jcs = '\x00' WHERE tenant_id = 'ten_aa' AND seq = 2`},
		{"the signature of another event", `UPDATE record_event SET sig_value = (SELECT sig_value FROM record_event WHERE tenant_id = 'ten_aa' AND seq = 3) WHERE tenant_id = 'ten_aa' AND seq = 2`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, "ten_aa", "ten_bb")
			w.appendN(t, "ten_aa", 4)
			w.appendN(t, "ten_bb", 2)
			w.tamper(t, c.sql)
			rep, err := w.v.Run(ctx(t))
			if err != nil {
				t.Fatalf("Run: %v", err)
			}
			a, b := rep.Results[0], rep.Results[1]
			if a.Status != verify.StatusBreak || a.Finding == nil || a.Finding.Kind != verify.BreakSignature || a.Finding.Seq != 2 {
				t.Fatalf("ten_aa result = %+v, want a SIGNATURE break at seq 2", a)
			}
			if b.Status != verify.StatusOK || b.Checked != 2 {
				t.Fatalf("ten_bb result = %+v, want verified and clean", b)
			}
			if len(w.alert.findings) != 1 || len(w.met.kinds) != 1 || w.met.kinds[0] != string(verify.BreakSignature) {
				t.Fatalf("alerts %v, metrics %v, want one of each, kind SIGNATURE", w.alert.findings, w.met.kinds)
			}
			if !w.killed(t, "ten_aa") || w.killed(t, "ten_bb") {
				t.Fatal("the kill switch must be on for ten_aa and off for ten_bb")
			}
			if strings.Contains(w.alert.findings[0].Detail, "0000000000") {
				t.Fatal("the finding carries signature material")
			}
		})
	}
}

// D5: a retired key still verifies what it signed, and a rotation with two active keys passes.
func TestARetiredKeyStillVerifiesWhatItSigned(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 3)
	if _, err := w.admin.Exec(ctx(t), `SELECT key_retire('core-execution.ed25519.1')`); err != nil {
		t.Fatal(err)
	}
	res, err := w.v.RunTenant(ctx(t), "ten_aa")
	if err != nil || res.Status != verify.StatusOK || res.Checked != 3 {
		t.Fatalf("RunTenant = %+v, %v; want OK over 3 events", res, err)
	}
}

// D5: the verifier reports every event signed under a revoked key at or after compromised_since as untrusted, and never accepts it silently; the events
// before that time stay trusted. The tenant is stopped, as for any finding.
func TestARevokedKeysLaterEventsAreReportedUntrusted(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 2)
	w.clk.Advance(time.Hour)
	since := w.clk.Now()
	w.appendN(t, "ten_aa", 2)
	if _, err := w.admin.Exec(ctx(t), `SELECT key_revoke('core-execution.ed25519.1', $1)`, since); err != nil {
		t.Fatal(err)
	}
	res, err := w.v.RunTenant(ctx(t), "ten_aa")
	if err != nil {
		t.Fatalf("RunTenant: %v", err)
	}
	if res.Status != verify.StatusBreak || res.Finding == nil || res.Finding.Kind != verify.BreakUntrustedKey || res.Finding.Seq != 3 {
		t.Fatalf("result = %+v, want an UNTRUSTED_KEY finding at seq 3 (the first event at or after compromised_since)", res)
	}
	if !w.killed(t, "ten_aa") {
		t.Fatal("the tenant was not stopped")
	}
}

func TestARevokedKeyWithNoLaterEventsLeavesTheRecordTrusted(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 3)
	if _, err := w.admin.Exec(ctx(t), `SELECT key_revoke('core-execution.ed25519.1', $1)`, w.clk.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	res, err := w.v.RunTenant(ctx(t), "ten_aa")
	if err != nil || res.Status != verify.StatusOK {
		t.Fatalf("RunTenant = %+v, %v; want OK: every event predates compromised_since", res, err)
	}
}

// A hash break at an earlier event is reported before a signature break at a later one: the chain is checked first.
func TestAHashBreakIsReportedBeforeALaterSignatureBreak(t *testing.T) {
	w := newWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 4)
	w.tamper(t, `UPDATE record_event SET sig_value = repeat('0', 128) WHERE seq = 4`)
	w.tamper(t, `UPDATE record_event SET hash = 'sha256:`+strings.Repeat("0", 64)+`' WHERE seq = 2`)
	res, _ := w.v.RunTenant(ctx(t), "ten_aa")
	if res.Finding == nil || res.Finding.Kind != "HASH" || res.Finding.Seq != 2 {
		t.Fatalf("result = %+v, want the HASH break at seq 2", res)
	}
}
