package reconstruct_test

// A331 (relayed Architect ruling, interim; not a committed decision), REQ-AUD-003, REQ-SEC-003: the whole body of a reconstruction, as the reader gets it. See project_ref_test.go.
// All values are SYNTHETIC.

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// The reader's body, whole: a reference with sixteen digits and a reference with forty digits and a letter, stored in the Record on purpose (the append guard would refuse them,
// so the test writes them unguarded to prove that the reader masks them anyway), reach the reader with no run over four digits; a stored amount is served as it is in the claim
// text and masked in the `source` fact.
func TestTheServedBodyHoldsNoLongRunInAReferenceAndNoMoneyOutsideText(t *testing.T) {
	w := newWorld(t)
	events := us01()
	long, longer := "fin.account:0123456789012345", "fin.account:"+strings.Repeat("0123456789", 4)+"a"
	events[4].payload["resolved_target"] = long
	events[5].payload["source"] = "NGN 12,345.00"
	events[9].payload = map[string]any{"claims": []any{
		map[string]any{"fact_id": "f1", "text": "NGN 12,345.00", "bound_fact_status": "KNOWN", "disclosed_refs": []any{long, longer}},
	}}
	testpg.AllowUnguardedRecord(t, w.dsn)
	events[4].unguarded, events[5].unguarded, events[9].unguarded = true, true, true
	w.append(t, tenant, "corr_us01", events...)
	raw, err := build(t, w, tenant, "corr_us01")
	if err != nil {
		t.Fatal(err)
	}
	b := parse(t, raw)
	if got := b.section("RESOLUTION")[0].Facts["resolved_target"]; got != "fin.account:************2345" {
		t.Errorf("resolved_target %q, want it masked to the last four digits", got)
	}
	if got := b.section("EXPLANATION")[0].Facts["text"]; got != "NGN 12,345.00" {
		t.Errorf("claim text %q, want the amount as it is", got)
	}
	if got := b.section("OBSERVED_STATE")[0].Facts["source"]; got != "NGN 12,*45.00" {
		t.Errorf("source %q, want the amount masked outside customer-visible text", got)
	}
	for _, digits := range []string{"0123456789012345", "01234567890123456789"} {
		if bytes.Contains(raw, []byte(digits)) {
			t.Errorf("the body still contains %s", digits)
		}
	}
}
