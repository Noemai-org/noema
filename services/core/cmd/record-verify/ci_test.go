package main

// REC-06 / REQ-AUD-001, REQ-AUD-005, INV-11, OD-06 (DEC-2026-10-17): the chain verifier job on the CI database. Each test builds a SYNTHETIC Record with the real appender and the
// real signers in a throwaway database, then runs the command as a separate process (this test binary started again with REC06_AS_COMMAND set, so main and its exit code
// are the real ones) with the DSN of the verification login, the way the CI step and the nightly workflow run it. The tampering is done by the database administrator with the
// immutability trigger off, the one way past the INSERT-only roles (REQ-AUD-001 tamper test). The tests whose names start TestRecordVerifyCI are the CI step and the nightly
// job: both run `go test -run '^TestRecordVerifyCI' ./cmd/record-verify`, so a tampered row and a broken prior hash each fail the job.
//
// Locally, without the pgsodium extension no event can be appended (the Record trigger fails closed): the tests that need events fail there and run in CI. The tests that
// need none (no tenants, a login that may write, an unreachable database, an empty chain) run anywhere.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const asCommand = "REC06_AS_COMMAND"

func TestMain(m *testing.M) {
	if os.Getenv(asCommand) == "1" {
		main()
		return
	}
	testpg.Main(m)
}

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

type ciWorld struct {
	admin *pgxpool.Pool
	dsn   string // the DSN of the verification login (record_verify)
	pools map[string]*pgxpool.Pool
	app   *chain.Appender
}

func newCIWorld(t *testing.T, tenants ...string) *ciWorld {
	t.Helper()
	admin, adminDSN := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	pools := map[string]*pgxpool.Pool{}
	for _, id := range tenants {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, $1)`, id); err != nil {
			t.Fatal(err)
		}
		pools[id] = testpg.TenantLogin(t, admin, adminDSN, "execution_rw", id)
	}
	clk := clock.NewManual(time.Date(2026, 11, 17, 2, 0, 0, 0, time.UTC))
	return &ciWorld{
		admin: admin, pools: pools,
		dsn: testpg.VerifierLogin(t, admin, adminDSN).Config().ConnString(),
		app: &chain.Appender{Clock: clk, IDs: ids.New(clk, nil), Signers: recordsig.All(t, admin, "rec06", nil)},
	}
}

func (w *ciWorld) appendN(t *testing.T, tenant string, n int, note string) {
	t.Helper()
	for i := 1; i <= n; i++ {
		tx, err := w.pools[tenant].Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.app.Append(ctx(t), tx, chain.Event{TenantID: tenant, CorrelationID: "corr_abc123", WrittenBy: "core-execution", Actor: "core-execution",
			EventType: "GRANT_SPENT", ObjectRef: fmt.Sprintf("grt_%04d", i), Payload: map[string]any{"n": i, "note": note}})
		if err != nil {
			_ = tx.Rollback(ctx(t))
			t.Fatalf("append: %v", err)
		}
		if err := tx.Commit(ctx(t)); err != nil {
			t.Fatal(err)
		}
	}
}

func (w *ciWorld) tamper(t *testing.T, sql string) {
	t.Helper()
	conn, err := w.admin.Acquire(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx(t), `SET session_replication_role = replica`); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec(ctx(t), sql); err != nil {
		t.Fatalf("tamper: %v", err)
	}
}

func (w *ciWorld) killed(t *testing.T, tenant string) bool {
	t.Helper()
	var k bool
	if err := w.admin.QueryRow(ctx(t), `SELECT kill_switch FROM tenant WHERE tenant_id = $1`, tenant).Scan(&k); err != nil {
		t.Fatal(err)
	}
	return k
}

// withUser is dsn with another login name (the cluster authenticates by trust, as it does for every test login).
func withUser(t *testing.T, dsn, user string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User(user)
	return u.String()
}

type outcome struct {
	code           int
	stdout, stderr string
}

// command runs the command as its own process against dsn, as CI and the nightly workflow do.
func command(t *testing.T, dsn string) outcome {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx(t), self)
	cmd.Env = []string{asCommand + "=1", envDSN + "=" + dsn}
	var out, errw bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errw
	code := 0
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if !errors.As(err, &ee) {
			t.Fatalf("run the command: %v", err)
		}
		code = ee.ExitCode()
	}
	return outcome{code: code, stdout: out.String(), stderr: errw.String()}
}

func (o outcome) result(t *testing.T) resultLine {
	t.Helper()
	lines := strings.Split(strings.TrimRight(o.stdout, "\n"), "\n")
	var r resultLine
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &r); err != nil || r.Msg != "RECORD_VERIFY_RESULT" {
		t.Fatalf("no RECORD_VERIFY_RESULT line in stdout %q (stderr %q)", o.stdout, o.stderr)
	}
	return r
}

func (o outcome) breakLine(t *testing.T, tenant string, seq int64, kind string) {
	t.Helper()
	for _, line := range strings.Split(o.stdout, "\n") {
		var m map[string]any
		if json.Unmarshal([]byte(line), &m) == nil && m["msg"] == "RECORD_CHAIN_BREAK" && m["tenant_id"] == tenant && m["seq"] == float64(seq) && m["kind"] == kind {
			return
		}
	}
	t.Fatalf("no RECORD_CHAIN_BREAK line for %s at seq %d of kind %s in stdout %q", tenant, seq, kind, o.stdout)
}

// REQ-AUD-001: the CI database's Record verifies, as the restricted verification login, and the job says how much it checked. This is the green half of the CI job.
func TestRecordVerifyCICleanChain(t *testing.T) {
	w := newCIWorld(t, "ten_aa", "ten_bb")
	w.appendN(t, "ten_aa", 4, "n")
	w.appendN(t, "ten_bb", 3, "n")
	o := command(t, w.dsn)
	if o.code != 0 {
		t.Fatalf("exit = %d on a clean Record; stdout %q stderr %q", o.code, o.stdout, o.stderr)
	}
	r := o.result(t)
	if r.Result != "clean" || r.Tenants != 2 || r.Events != 7 || r.Breaks != 0 {
		t.Fatalf("result = %+v, want clean over 2 tenants and 7 events", r)
	}
	if w.killed(t, "ten_aa") || w.killed(t, "ten_bb") {
		t.Fatal("a clean run stopped a tenant")
	}
	t.Logf("RECORD_VERIFY_CI_RESULT clean tenants=%d events=%d", r.Tenants, r.Events)
}

// REQ-AUD-001, REQ-AUD-005, INV-11 and the row's Done-when: a tampered row and a broken prior hash each fail the job, with the tenant, the sequence number and the kind, and
// stop that tenant only.
func TestRecordVerifyCITamperingFailsTheJob(t *testing.T) {
	hash0 := `'sha256:` + strings.Repeat("0", 64) + `'`
	cases := []struct {
		name string
		sql  string
		seq  int64
		kind string
	}{
		{"a tampered row: the hashed body edited", `UPDATE record_event SET body = jsonb_set(body, '{object_ref}', '"grt_9999"') WHERE tenant_id = 'ten_aa' AND seq = 2`, 2, "COLUMNS"},
		{"a tampered row: the stored hash edited", `UPDATE record_event SET hash = ` + hash0 + ` WHERE tenant_id = 'ten_aa' AND seq = 2`, 2, "HASH"},
		{"a tampered row: the payload edited after append", `UPDATE record_event SET payload = '{"n": 999}' WHERE tenant_id = 'ten_aa' AND seq = 2`, 2, "PAYLOAD_HASH"},
		{"a broken prior hash", `UPDATE record_event SET prev_hash = ` + hash0 + ` WHERE tenant_id = 'ten_aa' AND seq = 3`, 3, "PREV_HASH"},
		{"a missing sequence number", `DELETE FROM record_event WHERE tenant_id = 'ten_aa' AND seq = 2`, 3, "SEQ_GAP"}, // the verifier (record/chain.VerifyRows) reports the first row present after the gap (seq 3; its detail says "expected seq 2")
		{"a reordered pair", `UPDATE record_event SET seq = 1002 WHERE tenant_id = 'ten_aa' AND seq = 2; UPDATE record_event SET seq = 2 WHERE tenant_id = 'ten_aa' AND seq = 3; UPDATE record_event SET seq = 3 WHERE tenant_id = 'ten_aa' AND seq = 1002`, 2, "PREV_HASH"},
		{"the newest events dropped while the head is kept", `DELETE FROM record_event WHERE tenant_id = 'ten_aa' AND seq > 2`, 0, "HEAD_MISMATCH"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newCIWorld(t, "ten_aa", "ten_bb")
			w.appendN(t, "ten_aa", 4, "n")
			w.appendN(t, "ten_bb", 3, "n")
			w.tamper(t, c.sql)
			o := command(t, w.dsn)
			if o.code != 1 {
				t.Fatalf("exit = %d, want 1; stdout %q stderr %q", o.code, o.stdout, o.stderr)
			}
			r := o.result(t)
			if r.Result != "break" || r.Breaks != 1 || r.Tenants != 2 || r.ChainBreakTotal[c.kind] != 1 {
				t.Fatalf("result = %+v, want one %s break over 2 tenants", r, c.kind)
			}
			if c.seq != 0 {
				o.breakLine(t, "ten_aa", c.seq, c.kind)
			}
			if !w.killed(t, "ten_aa") || w.killed(t, "ten_bb") {
				t.Fatal("the kill switch must be on for ten_aa and off for ten_bb")
			}
		})
	}
}

// AGENT-13: the output of a failing job carries no stored content. The payload of the broken event holds a marker; the job names the event and the kind, never the content.
func TestRecordVerifyCIOutputCarriesNoPayload(t *testing.T) {
	w := newCIWorld(t, "ten_aa")
	w.appendN(t, "ten_aa", 3, payloadMarker)
	w.tamper(t, `UPDATE record_event SET payload = jsonb_set(payload, '{n}', '999') WHERE tenant_id = 'ten_aa' AND seq = 2`)
	o := command(t, w.dsn)
	if o.code != 1 {
		t.Fatalf("exit = %d, want 1", o.code)
	}
	if strings.Contains(o.stdout+o.stderr, payloadMarker) {
		t.Fatalf("the output of a failing job holds stored content:\nstdout %q\nstderr %q", o.stdout, o.stderr)
	}
}

// An empty chain verifies: the head is at the genesis and nothing was written. (The head row is made as the first append makes it; a provisioned tenant that has no head row yet
// is a separate question, open on the task.)
func TestRecordVerifyCIEmptyChainVerifies(t *testing.T) {
	w := newCIWorld(t, "ten_aa")
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO record_chain_head (tenant_id, seq, hash) VALUES ('ten_aa', 0, $1)`, chain.GenesisHash("ten_aa")); err != nil {
		t.Fatal(err)
	}
	o := command(t, w.dsn)
	if o.code != 0 {
		t.Fatalf("exit = %d on an empty chain; stdout %q stderr %q", o.code, o.stdout, o.stderr)
	}
	if r := o.result(t); r.Result != "clean" || r.Tenants != 1 || r.Events != 0 {
		t.Fatalf("result = %+v, want clean over 1 tenant and 0 events", r)
	}
}

// A database with no tenant at all verified nothing: that is not a pass.
func TestRecordVerifyCINoTenantsIsNotAPass(t *testing.T) {
	w := newCIWorld(t)
	o := command(t, w.dsn)
	if o.code != 2 {
		t.Fatalf("exit = %d, want 2; stdout %q stderr %q", o.code, o.stdout, o.stderr)
	}
}

// OD-06, "restricted write roles": the job runs as the verification login only. The administrator, a power's tenant login and a login that was given a write privilege on the
// Record are each refused before anything is read, and nothing is stopped.
func TestRecordVerifyCIRefusesALoginThatIsNotTheVerificationLogin(t *testing.T) {
	w := newCIWorld(t, "ten_aa")
	cases := map[string]string{
		"the administrator":      w.admin.Config().ConnString(),
		"a power's tenant login": w.pools["ten_aa"].Config().ConnString(),
	}
	// A member of record_verify that was given one more privilege on the Record: each of the four write privileges, on the Record and on its head.
	for i, grant := range []string{
		"INSERT ON record_event", "UPDATE ON record_event", "DELETE ON record_event", "TRUNCATE ON record_event",
		"INSERT ON record_chain_head", "UPDATE ON record_chain_head", "DELETE ON record_chain_head", "TRUNCATE ON record_chain_head",
	} {
		role := fmt.Sprintf("verify_writer_%d__rec06", i)
		if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+role+` LOGIN IN ROLE record_verify`); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = w.admin.Exec(context.Background(), `DROP OWNED BY `+role)
			_, _ = w.admin.Exec(context.Background(), `DROP ROLE IF EXISTS `+role)
		})
		if _, err := w.admin.Exec(ctx(t), `GRANT `+grant+` TO `+role); err != nil {
			t.Fatal(err)
		}
		cases["a verification login that may also "+grant] = withUser(t, w.dsn, role)
	}
	for name, dsn := range cases {
		t.Run(name, func(t *testing.T) {
			o := command(t, dsn)
			if o.code != 2 {
				t.Fatalf("exit = %d, want 2; stdout %q stderr %q", o.code, o.stdout, o.stderr)
			}
			if !strings.Contains(o.stderr, "verification login") {
				t.Errorf("stderr %q does not say that the verification login is required", o.stderr)
			}
			if w.killed(t, "ten_aa") {
				t.Fatal("a refused run stopped a tenant")
			}
		})
	}
}

// AGENT-13: an unreachable database is exit 2, and neither the connection string nor the password is printed.
func TestRecordVerifyCIAnUnreachableDatabaseIsNotAPassAndLeaksNothing(t *testing.T) {
	o := command(t, "postgres://record_verify:SYNTHETIC-PASSWORD-7c1@127.0.0.1:1/noema?sslmode=disable&connect_timeout=2")
	if o.code != 2 {
		t.Fatalf("exit = %d, want 2; stdout %q stderr %q", o.code, o.stdout, o.stderr)
	}
	for _, banned := range []string{"SYNTHETIC-PASSWORD-7c1", "127.0.0.1", "record_verify:"} {
		if strings.Contains(o.stdout+o.stderr, banned) {
			t.Errorf("the output holds %q: stdout %q stderr %q", banned, o.stdout, o.stderr)
		}
	}
}
