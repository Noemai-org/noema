package e2e_test

import (
	"context"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

var roleRounds atomic.Int64

// Every test binary that starts a mock-case creates the same two cluster-wide roles on the one PostgreSQL server. A check followed
// by a create races across processes, and the loser fails with a duplicate pg_authid key (seen in CI on noema#136). This test makes
// several callers create the same fresh roles at once: all of them must succeed.
func TestConcurrentCallersCreateTheMockCaseRolesWithoutRacing(t *testing.T) {
	server := testpg.ServerURL(t)
	for round := 0; round < 12; round++ {
		admin := fmt.Sprintf("mc_race_admin_%d_%d", os.Getpid(), roleRounds.Add(1))
		app := fmt.Sprintf("mc_race_app_%d_%d", os.Getpid(), roleRounds.Load())
		t.Cleanup(func() {
			c, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if cc, err := pgx.Connect(c, server); err == nil {
				_, _ = cc.Exec(c, `DROP ROLE IF EXISTS `+admin)
				_, _ = cc.Exec(c, `DROP ROLE IF EXISTS `+app)
				_ = cc.Close(c)
			}
		})
		const callers = 8
		errs := make([]error, callers)
		var wg sync.WaitGroup
		start := make(chan struct{})
		for i := 0; i < callers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				conn, err := pgx.Connect(ctx, server)
				if err != nil {
					errs[i] = err
					return
				}
				defer conn.Close(ctx)
				<-start
				errs[i] = ensureMockCaseRoles(ctx, conn, admin, app)
			}()
		}
		close(start)
		wg.Wait()
		for i, err := range errs {
			if err != nil {
				t.Fatalf("round %d, caller %d: %v", round, i, err)
			}
		}
	}
}
