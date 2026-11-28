package main

import (
	"testing"
)

// STM-01 bundle 1 (REQ-STM-001, REQ-RES-002, REQ-TRU-010; INV-16, AGENT-16): the Reconciler adapter is built over the sealed snapshots, the Reconciler's own rule set and mapping, and the
// escalation reasons and the freeze allowance of the pack's limits file, with a read bound that is a required setting and has no default.
func TestTheReconcilerAdapterIsBuiltFromTheLimitsAndTheSettingAndIsAGapWithoutThem(t *testing.T) {
	in := twIn(t)
	twEnv(&in, map[string]string{EnvTrustReconcileTimeout: "750"})
	w, err := wireTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	if w.fill(TrustAdapters{}).Reconciler == nil {
		t.Errorf("Reconciler is not built; reasons %v", w.Reasons["Reconciler"])
	}
	in = twIn(t) // no setting: a named gap, never a default
	if w, err = wireTrustInputs(in); err != nil {
		t.Fatal(err)
	}
	if w.fill(TrustAdapters{}).Reconciler != nil {
		t.Error("Reconciler is built with no read bound: a default was invented")
	}
	in = twIn(t)
	twEnv(&in, map[string]string{EnvTrustReconcileTimeout: "0"})
	if _, err = wireTrustInputs(in); err == nil {
		t.Error("a zero read bound was accepted")
	}
}
