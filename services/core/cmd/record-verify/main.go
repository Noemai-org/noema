// Command record-verify is the chain verifier job of REC-06 (REQ-AUD-001, REQ-AUD-005, INV-11, OD-06 of DEC-2026-10-17). CI runs it on every merge against the CI database and the nightly
// workflow runs it again; both run these tests and this code, the verifier of record/verify. It reads the Record as the restricted verification login (record_verify, noema_verify in
// Compose) and never repairs a chain. See README.md.
package main

import (
	"os"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

func main() {
	os.Exit(run(os.Getenv, os.Stdout, os.Stderr, openDatabase, clock.System{}))
}
