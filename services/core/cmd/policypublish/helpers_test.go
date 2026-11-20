package main

// TRU-02: the world the tests of the policy publication command share: a throwaway PostgreSQL database with the real migrations, two tenants, their Trust logins, an Execution login,
// a registered SYNTHETIC core-trust signing key, and the command's own run function. All data is SYNTHETIC.

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/trust/policy"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

func TestMain(m *testing.M) { testpg.Main(m) }

const (
	tenantA  = "ten_aa"
	tenantB  = "ten_bb"
	policyID = "fin.pol"
	packName = "fintech_v1.json"
	packRoot = "../../../../packs"
)

// keyDirAt is the index of the key directory in the arguments w.args builds.
const keyDirAt = 9

var epoch = time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC)

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

type world struct {
	t                    *testing.T
	admin                *pgxpool.Pool
	adminDSN             string
	trustA, trustB, exeA *pgxpool.Pool
	keyDir               string
	files                string
}

// writeKey writes a SYNTHETIC core-trust signing key as the deploy step does (0400, the seed in hex) and returns the loaded ring.
func writeKey(t *testing.T, dir, label string) *signing.Ring {
	t.Helper()
	seed := make([]byte, 32)
	copy(seed, label)
	path := filepath.Join(dir, "core-trust.ed25519.1.key")
	if err := os.WriteFile(path, []byte(hex.EncodeToString(seed)+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	ring, err := signing.LoadRing("core-trust", dir, signing.Options{MaxActive: 1})
	if err != nil {
		t.Fatal(err)
	}
	return ring
}

// documentHashMigration is the number of the migration that adds document_hash to the publication row.
const documentHashMigration = 35

var migrationNumber = regexp.MustCompile(`^([0-9]{4})_`)

// migrationsBefore is the migrations with a number below n.
func migrationsBefore(t *testing.T, n int) fs.FS {
	t.Helper()
	out := fstest.MapFS{}
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		m := migrationNumber.FindStringSubmatch(e.Name())
		if m == nil || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if k, _ := strconv.Atoi(m[1]); k < n {
			b, err := fs.ReadFile(migrations.FS, e.Name())
			if err != nil {
				t.Fatal(err)
			}
			out[e.Name()] = &fstest.MapFile{Data: b}
		}
	}
	return out
}

func newWorld(t *testing.T) *world { t.Helper(); return buildWorld(t, "") }

// newLegacyWorld is a world in which tenant A already holds a publication of fin.pol at legacyVersion that was made before the hash existed: the database is migrated up to the
// number before the hash migration, the row is inserted by the tenant's own Trust login, and the rest of the migrations are applied.
func newLegacyWorld(t *testing.T, legacyVersion string) *world {
	t.Helper()
	return buildWorld(t, legacyVersion)
}

func buildWorld(t *testing.T, legacyVersion string) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	first := fs.FS(migrations.FS)
	if legacyVersion != "" {
		first = migrationsBefore(t, documentHashMigration)
	}
	if _, err := db.Migrate(ctx(t), admin, first); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{tenantA, tenantB} {
		if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
	}
	w := &world{t: t, admin: admin, adminDSN: dsn, keyDir: t.TempDir(), files: t.TempDir(),
		trustA: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantA), trustB: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenantB),
		exeA: testpg.TenantLogin(t, admin, dsn, "execution_rw", tenantA)}
	if legacyVersion != "" {
		if _, err := w.trustA.Exec(ctx(t), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by)
			VALUES ($1, $2, $3, policy_version_key($3), 'adm_synth01')`, tenantA, policyID, legacyVersion); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
			t.Fatal(err)
		}
	}
	ring := writeKey(t, w.keyDir, "policypublish registered")
	for id, pub := range ring.PublicKeys() {
		if id != "core-trust.ed25519.1" {
			t.Fatalf("unexpected key %s", id)
		}
		if _, err := admin.Exec(ctx(t), `SELECT key_register('core-trust', 1, $1)`, []byte(pub)); err != nil {
			t.Fatal(err)
		}
	}
	return w
}

// dsnFile writes the DSN of a pool's login into a file only its owner can read, as the deploy step mounts one.
func (w *world) dsnFile(name, dsn string) string {
	w.t.Helper()
	p := filepath.Join(w.files, name)
	if err := os.WriteFile(p, []byte(dsn+"\n"), 0o600); err != nil {
		w.t.Fatal(err)
	}
	return p
}

func (w *world) login(name string, p *pgxpool.Pool) string {
	return w.dsnFile(name, p.Config().ConnString())
}

type report struct {
	Result        string `json:"result"`
	Reason        string `json:"reason"`
	Policy        string `json:"policy"`
	TenantID      string `json:"tenant_id"`
	CorrelationID string `json:"correlation_id"`
	Event         string `json:"event"`
	Service       string `json:"service"`
	Power         string `json:"power"`
	Level         string `json:"level"`
	Msg           string `json:"msg"`
	DocumentHash  string `json:"document_hash"`
}

type outcome struct {
	code     int
	out, err string
	rep      report
}

func (w *world) args(tenant, dsnFile, packDir string) []string {
	return []string{"--tenant", tenant, "--pack-dir", packDir, "--pack-file", packName, "--dsn-file", dsnFile, "--key-dir", w.keyDir, "--key-max-active", "1", "--published-by", "adm_synth01"}
}

func (w *world) run(args ...string) outcome {
	w.t.Helper()
	var out, errw bytes.Buffer
	code := run(args, &out, &errw, clock.NewManual(epoch))
	o := outcome{code: code, out: out.String(), err: errw.String()}
	lines := strings.Split(strings.TrimSpace(o.out), "\n")
	if last := lines[len(lines)-1]; last != "" {
		_ = json.Unmarshal([]byte(last), &o.rep)
	}
	return o
}

func (w *world) publish(tenant string, login *pgxpool.Pool) outcome {
	w.t.Helper()
	return w.run(w.args(tenant, w.login("trust-"+tenant, login), packRoot)...)
}

func (w *world) count(query string, args ...any) int {
	w.t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(w.t), query, args...).Scan(&n); err != nil {
		w.t.Fatal(err)
	}
	return n
}

func (w *world) nothingWritten() {
	w.t.Helper()
	if n := w.count(`SELECT count(*) FROM policy_publication`); n != 0 {
		w.t.Fatalf("%d publications written, want none", n)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 0 {
		w.t.Fatalf("%d POLICY_PUBLISHED events written, want none", n)
	}
}

func (w *world) activeAsExecution() string {
	w.t.Helper()
	v, err := policystore.Active(ctx(w.t), w.exeA, tenantA, policyID)
	if err != nil {
		w.t.Fatal(err)
	}
	return v
}

// variantPack writes a copy of the shipped pack into a new directory with the policy changed (or removed, when edit returns false), and returns the directory.
func variantPack(t *testing.T, edit func(pol map[string]any) bool) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(packRoot, packName))
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	var pol map[string]any
	if err := json.Unmarshal(top["policy"], &pol); err != nil {
		t.Fatal(err)
	}
	if edit(pol) {
		if top["policy"], err = json.Marshal(pol); err != nil {
			t.Fatal(err)
		}
	} else {
		delete(top, "policy")
	}
	b, err := json.Marshal(top)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, packName), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func withVersion(v string) func(map[string]any) bool {
	return func(pol map[string]any) bool { pol["version"] = v; return true }
}

// packPolicyHash is the document hash of the policy document of the pack in dir, as the loader of a later session computes it from the same file.
func packPolicyHash(t *testing.T, dir string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, packName))
	if err != nil {
		t.Fatal(err)
	}
	var top struct {
		Policy json.RawMessage `json:"policy"`
	}
	if err := json.Unmarshal(raw, &top); err != nil {
		t.Fatal(err)
	}
	h, err := policy.DocumentHash(top.Policy)
	if err != nil {
		t.Fatalf("hash of the pack's policy: %v", err)
	}
	return h
}

// storedDocumentHash is the hash the publication row of tenant A holds for fin.pol at version, or "" when it is NULL.
func (w *world) storedDocumentHash(version string) string {
	w.t.Helper()
	var h *string
	if err := w.admin.QueryRow(ctx(w.t), `SELECT document_hash FROM policy_publication WHERE tenant_id = $1 AND policy_id = $2 AND version = $3`, tenantA, policyID, version).Scan(&h); err != nil {
		w.t.Fatal(err)
	}
	if h == nil {
		return ""
	}
	return *h
}

// changeReason is an edit that keeps the policy valid and the version unchanged: one rule's reason code is another text.
func changeReason(pol map[string]any) bool {
	pol["rules"].([]any)[1].(map[string]any)["reason_code"] = "fin.reason.changed_text"
	return true
}

// sameDocument is an edit that changes nothing in the document; the variant pack is written with sorted keys and no whitespace, which is not the shipped file's spelling.
func sameDocument(map[string]any) bool { return true }
