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
	// The release fails with a server error: the connection stays alive and the session still holds the lock, which is exactly what
	// must not go back to the pool.
	//
	// A timeout is deliberately not tested here. Closing the connection is the client's complete action, but how soon the server
	// ends a session that is inside a long statement is the server's business: a backend does not notice a closed socket until the
	// statement ends (client_connection_check_interval is off by default), so a test built on a stalled unlock passes on one
	// PostgreSQL version and fails on another. It passed on 17.11 and failed on the 16 that CI uses for this branch, which is how
	// that was found. A cancelled query also makes pgx close the connection itself, so such a test could not tell a correct discard
	// from a plain release in any case.
	pool, dsn := testpg.NewDatabase(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	one := fstest.MapFS{"0001_probe.sql": &fstest.MapFile{Data: []byte(`CREATE TABLE probe (id int);`)}}

	saved := unlockStatement
	t.Cleanup(func() { unlockStatement = saved })
	unlockStatement = `SELECT 1/0`

	got, err := Migrate(ctx, pool, one)
	if err == nil || !strings.Contains(err.Error(), "release the migration lock") {
		t.Fatalf("Migrate = %v, %v; want the failed unlock reported", got, err)
	}
	if len(got) != 1 || got[0] != "0001_probe.sql" {
		t.Fatalf("the migration that was applied is not reported: %v", got)
	}

	// An independent session (its own pool, so its own connection) must be able to take the lock at once: the connection that
	// could not unlock was closed, not returned.
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
}
