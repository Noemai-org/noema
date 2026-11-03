package main

// DEBT-04 (Tier 0), REQ-SEC-004, INV-12: until option A (a login per tenant and a database-checked mapping) has landed,
// the reconstruction handler must not be reachable from the core process. record_read_chain trusts the tenant it is
// given, so the only protection today is that nothing serves it. This test fails the moment cmd/core imports the
// handler or the verifier endpoint, directly or through any other package.

import (
	"os/exec"
	"strings"
	"testing"
)

var notYetWired = []string{
	"github.com/Noemai-org/noema/services/core/record/reconstruct",
}

func TestTheCoreProcessDoesNotImportTheReconstructionHandler(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list -deps: %v\n%s", err, out)
	}
	deps := strings.Fields(string(out))
	if len(deps) < 10 {
		t.Fatalf("go list returned %d packages; the scan is not looking at the build", len(deps))
	}
	for _, d := range deps {
		for _, banned := range notYetWired {
			if d == banned {
				t.Errorf("cmd/core depends on %s: the reconstruction endpoint is wired before DEBT-04 option A landed", banned)
			}
		}
	}
}
