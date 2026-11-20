package main

// TRU-02 (ruling A484): the world the tests of the tenantinit command share: a throwaway PostgreSQL database with the real migrations, the operator's superuser login (the only
// role that can create a tenant row or call key_register; migration 0030 and 0013), a key directory with one SYNTHETIC core-trust key, and the command's own run function.
// All data is SYNTHETIC (AGENT-25). The password in the DSN file is a SYNTHETIC marker: the test cluster authenticates by trust, so any value connects, and the tests look for it in the output.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/db"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA = "ten_aa"
	tenantB = "ten_bb"

	// markerPassword is the SYNTHETIC password written into the DSN file. No output may carry it.
	markerPassword = "synthetic-marker-synthetic-marker"

	usageLine = "tenantinit: usage: tenantinit --dsn-file FILE --key-dir DIR --key-max-active N --tenant ten_ID=NAME [--tenant ten_ID=NAME ...]"

	// The fixed public texts. A cause is kept in the error value for errors.Is and is never printed.
	textNameDiffers  = "tenantinit: a tenant with this ID already exists under another name; nothing was changed"
	textKeyDiffers   = "tenantinit: a different public key is already registered under this key version; nothing was changed"
	textKeyNotActive = "tenantinit: a key of this version is registered but is no longer active; nothing was changed"
	textKeyRefused   = "tenantinit: the database refused to register a key (a version that is not newer than the registered ones, or too many active keys); nothing was changed"
	textLoginDenied  = "tenantinit: the login may not create tenants or register keys (only the operator's superuser does); nothing was changed"
	textKeysUnusable = "tenantinit: the signing keys cannot be loaded; nothing was changed"
	textDSNUnusable  = "tenantinit: the DSN file cannot be used"
	textFailed       = "tenantinit: the initialisation could not be completed; nothing was changed"
)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

// seedFor is a SYNTHETIC 32-byte seed: the label, zero-padded. Never mounted outside a test.
func seedFor(label string) []byte {
	seed := make([]byte, ed25519.SeedSize)
	copy(seed, label)
	return seed
}

type world struct {
	t       *testing.T
	admin   *pgxpool.Pool
	dsn     string // the superuser's URL, without the marker password
	dsnPath string // the DSN file of the operator's superuser, with the marker password
	keyDir  string
	files   string
	dials   int
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	w := &world{t: t, admin: admin, dsn: dsn, keyDir: t.TempDir(), files: t.TempDir()}
	w.dsnPath = w.dsnFile("operator", withPassword(t, dsn))
	w.key(1, "tenantinit key one")
	return w
}

// withPassword adds the marker password to a DSN URL.
func withPassword(t *testing.T, dsn string) string {
	t.Helper()
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	user := "postgres"
	if u.User != nil {
		user = u.User.Username()
	}
	u.User = url.UserPassword(user, markerPassword)
	return u.String()
}

// dsnFile writes a DSN into a file only its owner can read, as the deploy step mounts one, and returns its path.
func (w *world) dsnFile(name, dsn string) string {
	w.t.Helper()
	p := filepath.Join(w.files, name)
	if err := os.WriteFile(p, []byte(dsn+"\n"), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return p
}

// key writes the SYNTHETIC core-trust key of a version into the key directory as the deploy step does (0400, the seed in hex) and returns its public half, derived with the standard library.
func (w *world) key(version int, label string) ed25519.PublicKey {
	w.t.Helper()
	return writeKeyFile(w.t, w.keyDir, fmt.Sprintf("core-trust.ed25519.%d.key", version), label, 0o400)
}

func writeKeyFile(t *testing.T, dir, name, label string, mode os.FileMode) ed25519.PublicKey {
	t.Helper()
	seed := seedFor(label)
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(hex.EncodeToString(seed)+"\n"), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return ed25519.NewKeyFromSeed(seed).Public().(ed25519.PublicKey)
}

type result struct {
	code     int
	out, err string
}

func (r result) all() string { return r.out + r.err }

// run calls the command's run function with a dialer that counts the connections it is asked to open.
func (w *world) run(args ...string) result {
	w.t.Helper()
	var out, errw bytes.Buffer
	code := runWith(args, &out, &errw, func(c context.Context, path string) (*pgx.Conn, error) {
		w.dials++
		return connect(c, path)
	})
	return result{code: code, out: out.String(), err: errw.String()}
}

// args is a valid command line: the operator's DSN file, the key directory, the overlap and the tenants given as ID=NAME.
func (w *world) args(maxActive string, tenants ...string) []string {
	a := []string{"--dsn-file", w.dsnPath, "--key-dir", w.keyDir, "--key-max-active", maxActive}
	for _, tn := range tenants {
		a = append(a, "--tenant", tn)
	}
	return a
}

// outLine is the one line of output a successful run prints.
func outLine(tenantsCreated, tenantsExisting, keysRegistered, keysExisting int) string {
	return fmt.Sprintf(`{"tenants_created":%d,"tenants_existing":%d,"keys_registered":%d,"keys_existing":%d}`+"\n", tenantsCreated, tenantsExisting, keysRegistered, keysExisting)
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(w.t), query, args...).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

type tenantRow struct {
	name, timezone string
	killSwitch     bool
}

func (w *world) tenants() map[string]tenantRow {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT tenant_id, name, timezone, kill_switch FROM tenant`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]tenantRow{}
	for rows.Next() {
		var id string
		var r tenantRow
		if err := rows.Scan(&id, &r.name, &r.timezone, &r.killSwitch); err != nil {
			w.t.Fatal(err)
		}
		out[id] = r
	}
	if err := rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

type keyRow struct {
	keyID, status string
	version       int
	public        []byte
}

func (w *world) keys() map[string]keyRow {
	w.t.Helper()
	rows, err := w.admin.Query(ctx(w.t), `SELECT key_id, identity, version, status, public_key FROM signing_key`)
	if err != nil {
		w.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]keyRow{}
	for rows.Next() {
		var r keyRow
		var identity string
		if err := rows.Scan(&r.keyID, &identity, &r.version, &r.status, &r.public); err != nil {
			w.t.Fatal(err)
		}
		if identity != "core-trust" {
			w.t.Fatalf("a key of %s was registered", identity)
		}
		out[r.keyID] = r
	}
	if err := rows.Err(); err != nil {
		w.t.Fatal(err)
	}
	return out
}

// nothingWritten asserts that the run left no tenant row and no signing key.
func (w *world) nothingWritten() {
	w.t.Helper()
	if n := len(w.tenants()); n != 0 {
		w.t.Fatalf("%d tenant rows, want none", n)
	}
	if n := len(w.keys()); n != 0 {
		w.t.Fatalf("%d signing keys, want none", n)
	}
}

// registerKey registers a public key as the operator would by hand (key_register is the only way, and no runtime role can call it).
func (w *world) registerKey(version int, pub []byte) {
	w.t.Helper()
	if _, err := w.admin.Exec(ctx(w.t), `SELECT key_register('core-trust', $1, $2)`, version, pub); err != nil {
		w.t.Fatal(err)
	}
}

// mustContainNone fails if s carries any of the forbidden strings.
func mustContainNone(t *testing.T, what, s string, forbidden ...string) {
	t.Helper()
	for _, f := range forbidden {
		if f != "" && strings.Contains(s, f) {
			t.Errorf("%s carries %q", what, f)
		}
	}
}
