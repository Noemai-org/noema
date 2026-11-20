package keyannounce_test

// SEC-KEY-ROTATED (REQ-SEC-005, REQ-AUD-001, REQ-AUD-006; INV-11; DEC-2026-11-01-receipt-and-record-signing D2a, D3a, D4 invariant K1, test 20): at start-up the core appends
// KEY_ROTATED, written and signed by admin, for every signing_key version that has none on the tenant's Record, exactly once, naming the key by key_id only. Every case runs on a
// real PostgreSQL with the real migrations, the real chain appender and the database's own signature check. All keys and data are SYNTHETIC.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/record/keyannounce"
	"github.com/Noemai-org/noema/services/core/record/verify"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const tenantA, tenantB = "ten_aa", "ten_bb"

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

type world struct {
	admin    *pgxpool.Pool // the migration role: registers keys, reads anything
	dsn      string
	signers  map[string]chain.Signer // every identity's SYNTHETIC signer
	clk      *clock.Manual
	idg      *ids.Generator
	verifier *pgxpool.Pool
	ann      *keyannounce.Announcer
}

// newWorld migrates a database, creates two tenants and registers version 1 of a SYNTHETIC key for every signing identity, as the deploy step does (no Record event).
func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
	}
	w := &world{admin: admin, dsn: dsn, clk: clock.NewManual(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))}
	w.idg = ids.New(w.clk, nil)
	w.signers = recordsig.All(t, admin, "keyannounce", nil)
	w.verifier = testpg.VerifierLogin(t, admin, dsn)
	w.ann = w.announcer(map[string]chain.Signer{keyannounce.Emitter: w.signers[keyannounce.Emitter]})
	return w
}

func (w *world) announcer(signers map[string]chain.Signer) *keyannounce.Announcer {
	return &keyannounce.Announcer{Appender: &chain.Appender{Clock: w.clk, IDs: w.idg, Signers: signers}, Keys: keyannounce.PoolKeys{Q: w.verifier}}
}

// tenant returns the connections of one tenant's announcement: its admin emitter login and its record_read login. The logins are made once per tenant and test.
func (w *world) tenant(t *testing.T, id string) keyannounce.Tenant {
	t.Helper()
	return keyannounce.Tenant{ID: id, Admin: testpg.EmitterLogin(t, w.admin, w.dsn, id, keyannounce.Emitter), Reader: testpg.TenantLogin(t, w.admin, w.dsn, "record_read", id)}
}

// registered returns every key ID in the registry, in (identity, version) order.
func (w *world) registered(t *testing.T) []string {
	t.Helper()
	rows, err := w.admin.Query(ctx(t), `SELECT key_id FROM signing_key ORDER BY identity, version`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		if err := rows.Scan(&k); err != nil {
			t.Fatal(err)
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// registerNext registers the next version of an identity with a throwaway SYNTHETIC public key, as the deploy step would on a rotation.
func (w *world) registerNext(t *testing.T, identity string, version int) string {
	t.Helper()
	pub := bytes.Repeat([]byte{byte(version)}, 32)
	var id string
	if err := w.admin.QueryRow(ctx(t), `SELECT key_register($1, $2, $3)`, identity, version, pub).Scan(&id); err != nil {
		t.Fatalf("key_register(%s, %d): %v", identity, version, err)
	}
	return id
}

// announced returns the KEY_ROTATED events of the tenant's chain as read through the tenant's own record_read login, in chain order.
func announced(t *testing.T, ten keyannounce.Tenant) []chain.Row {
	t.Helper()
	rows, err := chain.ReadChain(ctx(t), ten.Reader)
	if err != nil {
		t.Fatal(err)
	}
	var out []chain.Row
	for _, r := range rows {
		if r.EventType == keyannounce.EventType {
			out = append(out, r)
		}
	}
	return out
}

func keyIDOf(t *testing.T, r chain.Row) string {
	t.Helper()
	if r.Payload == nil {
		t.Fatalf("event %s has no stored payload", r.EventID)
	}
	var p map[string]any
	if err := json.Unmarshal([]byte(*r.Payload), &p); err != nil {
		t.Fatal(err)
	}
	k, _ := p["key_id"].(string)
	return k
}

func keyIDs(t *testing.T, rows []chain.Row) []string {
	t.Helper()
	var out []string
	for _, r := range rows {
		out = append(out, keyIDOf(t, r))
	}
	return out
}

func chainLen(t *testing.T, ten keyannounce.Tenant) int {
	t.Helper()
	rows, err := chain.ReadChain(ctx(t), ten.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return len(rows)
}

// A registered key with no event gets one KEY_ROTATED, written by admin, with the principal admin, naming the key by key_id only (D2a, D4 invariant K1, test 20).
func TestEveryRegisteredKeyWithNoEventGetsOneKeyRotatedByAdminNamingTheKeyIDOnly(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	want := w.registered(t)
	if len(want) != len(recordsig.Identities) {
		t.Fatalf("registered %d keys, want %d", len(want), len(recordsig.Identities))
	}

	got, err := w.ann.Announce(ctx(t), ten)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Announce returned %v, want every registered key in (identity, version) order %v", got, want)
	}
	rows := announced(t, ten)
	if !reflect.DeepEqual(keyIDs(t, rows), want) {
		t.Fatalf("KEY_ROTATED on the chain for %v, want exactly %v", keyIDs(t, rows), want)
	}
	for _, r := range rows {
		if r.WrittenBy != "admin" || r.Actor != "admin" {
			t.Fatalf("event %s written_by %q actor %q, want admin and admin (no administrator session caused a start-up announcement)", r.EventID, r.WrittenBy, r.Actor)
		}
		var p map[string]any
		if err := json.Unmarshal([]byte(*r.Payload), &p); err != nil {
			t.Fatal(err)
		}
		if len(p) != 1 || p["key_id"] == nil {
			t.Fatalf("payload %s, want the key_id and nothing else (D4: the audit trail names keys by key_id only)", *r.Payload)
		}
		var sigKey string
		if err := w.admin.QueryRow(ctx(t), `SELECT sig_key_id FROM record_event WHERE tenant_id = $1 AND event_id = $2`, tenantA, r.EventID).Scan(&sigKey); err != nil {
			t.Fatal(err)
		}
		if sigKey != "admin.ed25519.1" {
			t.Fatalf("event %s signed under %q, want the admin key", r.EventID, sigKey)
		}
	}
	// The chain verifies: hashes from genesis, and every signature in Go under the registered keys.
	if n, err := chain.Verify(ctx(t), w.admin, tenantA); err != nil || n != int64(len(want)) {
		t.Fatalf("chain.Verify = %d, %v; want %d events and no break", n, err, len(want))
	}
	res, err := (&verify.Verifier{DB: w.verifier, Alerts: noAlerts{}, Metrics: noMetrics{}, Clock: w.clk}).RunTenant(ctx(t), tenantA)
	if err != nil || res.Status != verify.StatusOK || res.Checked != int64(len(want)) {
		t.Fatalf("the verification job: %+v, %v; want OK over %d events", res, err, len(want))
	}
}

type noAlerts struct{}

func (noAlerts) ChainBreak(context.Context, verify.Finding) error { return nil }

type noMetrics struct{}

func (noMetrics) ChainBreak(string) {}

// No key material reaches the Record: neither the public key (hex or base64 of the 32 bytes) nor anything but the key ID is in the stored body or payload (D3, D3a, D4).
func TestNoKeyMaterialIsInTheEvent(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	if _, err := w.ann.Announce(ctx(t), ten); err != nil {
		t.Fatal(err)
	}
	rows, err := w.admin.Query(ctx(t), `SELECT k.public_key, e.body::text, e.payload::text, encode(e.signed_jcs, 'escape') FROM signing_key k
		JOIN record_event e ON e.tenant_id = $1 AND e.event_type = 'KEY_ROTATED' AND e.payload ->> 'key_id' = k.key_id`, tenantA)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var pub []byte
		var body, payload, signed string
		if err := rows.Scan(&pub, &body, &payload, &signed); err != nil {
			t.Fatal(err)
		}
		n++
		for _, text := range []string{body, payload, signed} {
			if strings.Contains(text, hex.EncodeToString(pub)) || strings.Contains(text, "public_key") {
				t.Fatalf("an announcement holds key material: %s", text)
			}
		}
	}
	if n != len(recordsig.Identities) {
		t.Fatalf("matched %d announcements to keys, want %d", n, len(recordsig.Identities))
	}
}

// A key that already has a KEY_ROTATED written by admin gets none: the check reads the tenant's chain, not a memory of this process. A restarted process (a new
// Announcer) finds every key announced and appends nothing.
func TestAKeyThatAlreadyHasAnEventGetsNone(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	// One key was announced earlier, by hand through the same appender and the admin login (as an earlier start-up would have).
	const done = "core-outcome.ed25519.1"
	tx, err := ten.Admin.BeginTx(ctx(t), pgx.TxOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.ann.Appender.Append(ctx(t), tx, chain.Event{TenantID: tenantA, CorrelationID: "corr_earlier01", WrittenBy: "admin", Actor: "admin", EventType: keyannounce.EventType,
		ObjectRef: "key_earlier0001", Payload: map[string]any{"key_id": done}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	got, err := w.ann.Announce(ctx(t), ten)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(recordsig.Identities)-1 {
		t.Fatalf("Announce returned %v, want every key but %s", got, done)
	}
	for _, k := range got {
		if k == done {
			t.Fatalf("%s was announced a second time", done)
		}
	}
	rows := announced(t, ten)
	count := 0
	for _, k := range keyIDs(t, rows) {
		if k == done {
			count++
		}
	}
	if count != 1 || len(rows) != len(recordsig.Identities) {
		t.Fatalf("KEY_ROTATED events %v, want one per key (%s once)", keyIDs(t, rows), done)
	}
	before := chainLen(t, ten)
	again, err := w.announcer(map[string]chain.Signer{"admin": w.signers["admin"]}).Announce(ctx(t), ten)
	if err != nil || len(again) != 0 {
		t.Fatalf("a restarted process appended %v (%v), want nothing", again, err)
	}
	if n := chainLen(t, ten); n != before {
		t.Fatalf("chain length %d after the restart, want %d", n, before)
	}
}

// A second start-up appends nothing: no event, no new chain head.
func TestASecondRunAppendsNothing(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	if _, err := w.ann.Announce(ctx(t), ten); err != nil {
		t.Fatal(err)
	}
	var seq int64
	var hash string
	head := func() {
		if err := w.admin.QueryRow(ctx(t), `SELECT seq, hash FROM record_chain_head WHERE tenant_id = $1`, tenantA).Scan(&seq, &hash); err != nil {
			t.Fatal(err)
		}
	}
	head()
	seq1, hash1 := seq, hash
	got, err := w.ann.Announce(ctx(t), ten)
	if err != nil || len(got) != 0 {
		t.Fatalf("the second run appended %v (%v), want nothing", got, err)
	}
	head()
	if seq != seq1 || hash != hash1 {
		t.Fatalf("the chain head moved from %d to %d on a second run", seq1, seq)
	}
	if n := len(announced(t, ten)); n != len(recordsig.Identities) {
		t.Fatalf("%d KEY_ROTATED events after two runs, want %d", n, len(recordsig.Identities))
	}
}

// A new version registered after the first start-up gets one more event at the next one, and only that one (rotation, REQ-SEC-005).
func TestARegistrationOfANewVersionAppendsOneMore(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	if _, err := w.ann.Announce(ctx(t), ten); err != nil {
		t.Fatal(err)
	}
	next := w.registerNext(t, "core-trust", 2)
	got, err := w.ann.Announce(ctx(t), ten)
	if err != nil || !reflect.DeepEqual(got, []string{next}) {
		t.Fatalf("Announce after a rotation returned %v (%v), want only %s", got, err, next)
	}
	rows := announced(t, ten)
	if len(rows) != len(recordsig.Identities)+1 || keyIDOf(t, rows[len(rows)-1]) != next {
		t.Fatalf("KEY_ROTATED events %v, want the original %d and %s last", keyIDs(t, rows), len(recordsig.Identities), next)
	}
	if got, err := w.ann.Announce(ctx(t), ten); err != nil || len(got) != 0 {
		t.Fatalf("the next run appended %v (%v), want nothing", got, err)
	}
}

// Two start-ups at once append one event per key between them (the per-tenant lock and the re-read under it).
func TestConcurrentRunsAppendOnePerKey(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	const runs = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var total atomic.Int64
	errs := make(chan error, runs)
	for i := 0; i < runs; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			got, err := w.ann.Announce(ctx(t), ten)
			total.Add(int64(len(got)))
			errs <- err
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("a concurrent run failed: %v", err)
		}
	}
	want := w.registered(t)
	if total.Load() != int64(len(want)) {
		t.Fatalf("the runs reported %d appended events between them, want %d", total.Load(), len(want))
	}
	if got := keyIDs(t, announced(t, ten)); !reflect.DeepEqual(got, want) {
		t.Fatalf("KEY_ROTATED on the chain %v, want exactly one per key %v", got, want)
	}
}

// Each tenant's chain is announced on its own: the keys are the deployment's, the Record is the tenant's. A tenant not yet announced is untouched.
func TestEachTenantsChainIsAnnouncedOnItsOwn(t *testing.T) {
	w := newWorld(t)
	a, b := w.tenant(t, tenantA), w.tenant(t, tenantB)
	if _, err := w.ann.Announce(ctx(t), a); err != nil {
		t.Fatal(err)
	}
	if n := chainLen(t, b); n != 0 {
		t.Fatalf("tenant B's chain holds %d events after only A was announced", n)
	}
	got, err := w.ann.Announce(ctx(t), b)
	if err != nil || len(got) != len(recordsig.Identities) {
		t.Fatalf("tenant B announced %v (%v), want every key", got, err)
	}
	if n := len(announced(t, a)); n != len(recordsig.Identities) {
		t.Fatalf("tenant A holds %d KEY_ROTATED, want %d", n, len(recordsig.Identities))
	}
}

// With no key registered there is nothing to announce: no error, and the tenant's chain is not touched (no head is made).
func TestNoRegisteredKeyIsNothingToAnnounce(t *testing.T) {
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, tenantA); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(time.Date(2026, 11, 18, 9, 0, 0, 0, time.UTC))
	ann := &keyannounce.Announcer{Appender: &chain.Appender{Clock: clk, IDs: ids.New(clk, nil), Signers: map[string]chain.Signer{}}, Keys: keyannounce.PoolKeys{Q: testpg.VerifierLogin(t, admin, dsn)}}
	ten := keyannounce.Tenant{ID: tenantA, Admin: testpg.EmitterLogin(t, admin, dsn, tenantA, "admin"), Reader: testpg.TenantLogin(t, admin, dsn, "record_read", tenantA)}
	got, err := ann.Announce(ctx(t), ten)
	if err != nil || len(got) != 0 {
		t.Fatalf("Announce with no key returned %v (%v), want nothing", got, err)
	}
	var heads int
	if err := admin.QueryRow(ctx(t), `SELECT count(*) FROM record_chain_head`).Scan(&heads); err != nil || heads != 0 {
		t.Fatalf("%d chain heads (%v), want none: nothing to announce touches no chain", heads, err)
	}
}

// Only an event written by admin counts as an announcement: another emitter's KEY_ROTATED for the same key does not stand in for it.
func TestAnotherEmittersKeyRotatedDoesNotCountAsTheAnnouncement(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	trust := testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", tenantA)
	app := &chain.Appender{Clock: w.clk, IDs: w.idg, Signers: map[string]chain.Signer{"core-trust": w.signers["core-trust"]}}
	tx, err := trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := app.Append(ctx(t), tx, chain.Event{TenantID: tenantA, CorrelationID: "corr_other001", WrittenBy: "core-trust", Actor: "core-trust", EventType: keyannounce.EventType,
		ObjectRef: "key_other0001", Payload: map[string]any{"key_id": "admin.ed25519.1"}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	got, err := w.ann.Announce(ctx(t), ten)
	if err != nil || len(got) != len(recordsig.Identities) {
		t.Fatalf("Announce returned %v (%v), want every key: core-trust's event is not the admin announcement", got, err)
	}
}

// Fail closed, Record first: a failure to append is an error, and nothing is kept from the run. Each case below is a real refusal by the database or an error of the signer,
// and after each the chain holds no KEY_ROTATED; a later run with the fault removed announces every key.
func TestAFailureToAppendIsAnErrorAndLeavesNothingPartial(t *testing.T) {
	cases := []struct {
		name string
		run  func(w *world, ten keyannounce.Tenant) (*keyannounce.Announcer, keyannounce.Tenant)
	}{
		{"the second append fails to sign", func(w *world, ten keyannounce.Tenant) (*keyannounce.Announcer, keyannounce.Tenant) {
			return w.announcer(map[string]chain.Signer{"admin": &failingSigner{inner: w.signers["admin"], failAt: 2}}), ten
		}},
		{"the admin key is not the registered one (the database refuses the signature)", func(w *world, ten keyannounce.Tenant) (*keyannounce.Announcer, keyannounce.Tenant) {
			other := recordsig.Local("another world", nil, "admin")
			return w.announcer(other), ten
		}},
		{"no signer for admin", func(w *world, ten keyannounce.Tenant) (*keyannounce.Announcer, keyannounce.Tenant) {
			return w.announcer(map[string]chain.Signer{}), ten
		}},
		{"the login is not the admin emitter's (the database refuses the emitter)", func(w *world, ten keyannounce.Tenant) (*keyannounce.Announcer, keyannounce.Tenant) {
			ten.Admin = testpg.TenantLogin(t, w.admin, w.dsn, "trust_rw", tenantA)
			return w.ann, ten
		}},
		{"the key registry cannot be read (the admin login has no SELECT on signing_key)", func(w *world, ten keyannounce.Tenant) (*keyannounce.Announcer, keyannounce.Tenant) {
			a := w.announcer(map[string]chain.Signer{"admin": w.signers["admin"]})
			a.Keys = keyannounce.PoolKeys{Q: ten.Admin.(*pgxpool.Pool)}
			return a, ten
		}},
		{"the chain cannot be read (the reader is not a record_read login)", func(w *world, ten keyannounce.Tenant) (*keyannounce.Announcer, keyannounce.Tenant) {
			ten.Reader = testpg.TenantLogin(t, w.admin, w.dsn, "execution_rw", tenantA)
			return w.ann, ten
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			ten := w.tenant(t, tenantA)
			ann, bad := c.run(w, ten)
			got, err := ann.Announce(ctx(t), bad)
			if err == nil {
				t.Fatalf("Announce returned %v and no error, want a refusal (startup refuses: the Record comes first)", got)
			}
			if n := len(announced(t, ten)); n != 0 {
				t.Fatalf("%d KEY_ROTATED events kept after a failed run, want none (nothing partial)", n)
			}
			var events int
			if err := w.admin.QueryRow(ctx(t), `SELECT count(*) FROM record_event WHERE tenant_id = $1`, tenantA).Scan(&events); err != nil || events != 0 {
				t.Fatalf("%d events on the chain after a failed run (%v), want none", events, err)
			}
			// The fault removed, the next start-up announces every key.
			again, err := w.ann.Announce(ctx(t), ten)
			if err != nil || len(again) != len(recordsig.Identities) {
				t.Fatalf("the next run returned %v (%v), want every key", again, err)
			}
		})
	}
}

// A context with no deadline is refused before anything is read or written (AGENTS.md section 7: a deadline on every call).
func TestAContextWithNoDeadlineIsRefused(t *testing.T) {
	w := newWorld(t)
	ten := w.tenant(t, tenantA)
	if _, err := w.ann.Announce(context.Background(), ten); err == nil {
		t.Fatal("Announce accepted a context with no deadline")
	}
	if n := chainLen(t, ten); n != 0 {
		t.Fatalf("%d events on the chain, want none", n)
	}
}

// failingSigner signs like its inner signer until call failAt, which fails: a failure of the key custody half way through a run.
type failingSigner struct {
	inner  chain.Signer
	failAt int
	calls  atomic.Int32
}

func (f *failingSigner) SignRecordEvent(body map[string]any) (string, string, []byte, error) {
	if int(f.calls.Add(1)) == f.failAt {
		return "", "", nil, errors.New("the signing key is unavailable")
	}
	return f.inner.SignRecordEvent(body)
}
