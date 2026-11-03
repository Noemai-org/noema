package main

// DEBT-04 (Tier 0), REQ-SEC-004, INV-12: the database now binds the tenant to the login (migrations 0004 and 0005), but the
// reconstruction endpoint must not be reachable from the core process until tenant provisioning (the logins and their
// mapping rows) and EDG-01's ops and audit role check have landed. This test fails the moment cmd/core imports the handler,
// directly or through any other package. Remove it in the PR that wires the endpoint, with those two in place.

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
