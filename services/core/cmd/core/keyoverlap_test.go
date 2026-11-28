package main

// A739 (REQ-SEC-005; AGENT-16): the overlap of every key ring is a whole number from 1 to 999, refused with an error that names the setting and never carries the value. Every ring reads
// its setting through parseKeyOverlap: a source guard pins that, so a ring cannot keep a bound of its own. All values are SYNTHETIC.

import (
	"os"
	"strings"
	"testing"
)

func TestTheOverlapOfAKeyRingIsOneToNineHundredNinetyNine(t *testing.T) {
	for in, want := range map[string]int{"1": 1, "2": 2, "999": 999} {
		if got, err := parseKeyOverlap("NOEMA_X_KEY_MAX_ACTIVE", in); err != nil || got != want {
			t.Errorf("%q: got %d, %v; want %d", in, got, err, want)
		}
	}
	for _, bad := range []string{"", " ", "0", "-1", "1000", "999999999", "abc", "1.5", " 7", "007", "1e3", "99999999999999999999"} {
		_, err := parseKeyOverlap("NOEMA_X_KEY_MAX_ACTIVE", bad)
		if err == nil {
			t.Errorf("%q was accepted", bad)
			continue
		}
		if !strings.Contains(err.Error(), "NOEMA_X_KEY_MAX_ACTIVE") || (strings.TrimSpace(bad) != "" && strings.Contains(err.Error(), bad)) {
			t.Errorf("%q: error %q must name the setting and never the value", bad, err)
		}
	}
}

func TestEveryKeyRingReadsItsOverlapThroughTheOneParser(t *testing.T) {
	rings := map[string]string{
		"EnvTrustKeyMaxActive": "wire_decisions.go", "EnvOrchestratorKeyMaxActive": "recorder.go", "EnvExecutionKeyMaxActive": "execution.go",
		"EnvEdgeKeyMaxActive": "wire_gateway.go", "EnvGrantKeyMaxActive": "wire_trust.go", "EnvEvidenceKeyMaxActive": "compose_injection.go",
	}
	for name, file := range rings {
		src, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(src), "parseKeyOverlap(") {
			t.Errorf("%s reads %s without parseKeyOverlap: a ring keeps a bound of its own (A739)", file, name)
		}
		if strings.Contains(string(src), "keyOverlapRe") || strings.Contains(string(src), "overlapRe.") {
			t.Errorf("%s still carries its own overlap pattern (A739)", file)
		}
	}
}
