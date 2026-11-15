package testpg_test

// DEC-2026-11-01 D10a / DEBT-01: the pgsodium bootstrap of a test database must be safe when several databases are made at once on one server. CREATE
// EXTENSION pgsodium creates cluster-wide roles, so two sessions that run it together on a server that has never had the extension collide on
// pg_authid (SQLSTATE 23505). CI hit this when test packages started together (noema#263, run 42382885859). The test makes eight databases at once on
// a fresh server; without the lock in bootstrapSigning it fails on a cluster that has the extension available. Every database is SYNTHETIC.

import (
	"fmt"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestDatabasesCanBeMadeAtOnceOnAFreshServer(t *testing.T) {
	for i := 0; i < 8; i++ {
		t.Run(fmt.Sprintf("db%d", i), func(t *testing.T) {
			t.Parallel()
			testpg.NewDatabase(t)
		})
	}
}
