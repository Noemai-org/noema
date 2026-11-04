package db

// PLT-06, Codex review 5763745105 (Major): an advisory unlock that fails or times out must not leave the session lock on a
// connection that goes back to the pool. The failure is forced on a real connection by making the unlock statement outlive the
// cleanup limit; the lock is then looked at from an independent session, because the session that holds an advisory lock can
// always take it again, so the same pool could not tell.

import (
	"context"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

func TestAMigrationLockIsNeverLeftOnAPooledConnectionWhenTheUnlockFails(t *testing.T) {
	// Two ways for the release to fail. A server error leaves the connection alive and the lock held, which is exactly what must not
	// go back to the pool (pgx itself closes a connection whose query was cancelled by a timeout, so that case cannot tell a
	// correct discard from a release; it is kept to show the timeout path is reported and does not hang).
	for name, failing := range map[string]string{"a server error (the connection stays alive)": `SELECT 1/0`, "a timeout (the unlock outlives the cleanup limit)": `SELECT pg_sleep(60)`} {
		t.Run(name, func(t *testing.T) {
			pool, dsn := testpg.NewDatabase(t)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			one := fstest.MapFS{"0001_probe.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE probe (id int);`)}}

			saved := unlockStatement
			t.Cleanup(func() { unlockStatement = saved })
			unlockStatement = failing

			got, err := Migrate(ctx, pool, one)
			if err == nil || !strings.Contains(err.Error(), "release the migration lock") {
				t.Fatalf("Migrate = %v, %v; want the failed unlock reported", got, err)
			}
			if len(got) != 1 || got[0] != "0001_probe.sql" {
				t.Fatalf("the migration that was applied is not reported: %v", got)
			}

			// An independent session (its own pool, so its own connection) must be able to take the lock at once: the
			// connection that could not unlock was closed, not returned.
			other, err := pgxpool.New(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			defer other.Close()
			var free bool
			if err := other.QueryRow(ctx, `SELECT pg_try_advisory_lock(hashtext('noema:migrate'))`).Scan(&free); err != nil {
				t.Fatal(err)
			}
			if !free {
				t.Fatal("the migration lock is still held after the failed unlock: a connection holding it went back to the pool")
			}
			_, _ = other.Exec(ctx, `SELECT pg_advisory_unlock(hashtext('noema:migrate'))`)

			// And the next migration does not hang.
			unlockStatement = saved
			two := fstest.MapFS{"0001_probe.sql": one["0001_probe.sql"], "0002_next.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE next (id int);`)}}
			next, cancel2 := context.WithTimeout(ctx, 20*time.Second)
			defer cancel2()
			if applied, err := Migrate(next, pool, two); err != nil || len(applied) != 1 || applied[0] != "0002_next.sql" {
				t.Fatalf("the next Migrate = %v, %v; want only 0002 applied, without hanging", applied, err)
			}
		})
	}
}
