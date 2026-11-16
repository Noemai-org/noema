package testpg

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// The scan of the Record that every test database gets when the test ends (A184, A154): no stored Record event holds a payload string with a digit run over four (typed IDs,
// RFC 3339 timestamps, content hashes and the named reference shapes excepted, see record/payloadguard), and no object_ref is derived from a payload string. The append guard
// refuses such an event at the door; this scan is the second lock, it also reads rows that reached the table by raw SQL in a test (a fixture) and so keeps a future fixture
// from writing a long number into a Record that is then served. A test that writes such a row on purpose calls AllowUnguardedRecord.

var (
	allowMu   sync.Mutex
	allowedDB = map[string]bool{}
)

// AllowUnguardedRecord exempts the database of dsn from the end-of-test scan: for a test that puts a long digit run into the Record on purpose, to prove that a reader masks
// it (recordsig.AppendUnguarded) or that this scan finds it. The test names what it does in its own comment; no other test may call this.
func AllowUnguardedRecord(t testing.TB, dsn string) {
	t.Helper()
	allowMu.Lock()
	defer allowMu.Unlock()
	allowedDB[dsn] = true
}

func allowed(dsn string) bool {
	allowMu.Lock()
	defer allowMu.Unlock()
	return allowedDB[dsn]
}

// ScanRecord returns one finding per stored Record event of the database at dsn that the append guard would refuse or whose object_ref is derived from its payload. A database with no
// record_event table (a test that migrates only part of the schema) has nothing to scan. The findings name the event type, the event ID and the JSON path, never a value.
func ScanRecord(ctx context.Context, dsn string) ([]string, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	defer conn.Close(ctx)
	var have bool
	if err := conn.QueryRow(ctx, `SELECT to_regclass('public.record_event') IS NOT NULL`).Scan(&have); err != nil || !have {
		return nil, err
	}
	rows, err := conn.Query(ctx, `SELECT event_id, event_type, object_ref, payload::text FROM record_event WHERE payload IS NOT NULL ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id, typ, ref, payload string
		if err := rows.Scan(&id, &typ, &ref, &payload); err != nil {
			return nil, err
		}
		for _, f := range payloadguard.ScanEvent(typ, ref, []byte(payload)) {
			out = append(out, id+": "+f)
		}
	}
	return out, rows.Err()
}

// scanAtEnd registers the scan as a cleanup. It is registered after the cleanup that drops the database, so it runs first.
func scanAtEnd(t testing.TB, dsn string) {
	t.Cleanup(func() {
		if allowed(dsn) {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		found, err := ScanRecord(ctx, dsn)
		if err != nil {
			t.Errorf("testpg: scan of the Record (A184): %v", err)
			return
		}
		for _, f := range found {
			t.Errorf("testpg: the Record holds a payload the append guard refuses (A184, A154): %s", f)
		}
	})
}
