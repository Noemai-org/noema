package main

// TRU-02 (REQ-TRU-005, REQ-OPS-003, REQ-AUD-001; INV-09, INV-12, INV-21, AGENT-13): the policy publication command. Every test runs the command's own run function against a throwaway
// PostgreSQL database with the real migrations, the real Trust login of a tenant and the real Record signer, and reads the result back as Execution does (policystore.Active) and
// from the Record. The pack is the shipped packs/fintech_v1.json or a copy of it with the policy changed. All data is SYNTHETIC.
//
// The publication row keeps the hash of the policy document (ruling on noema#771, interim), so a second document under the same version is refused as document_changed. The documents
// themselves are still not stored (DEBT-23).

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/execution/signing"
)

// REQ-TRU-005, INV-09: the version in force for the tenant is the one the pack's document names, and Execution reads it back as policy_id@version.
func TestPublishesThePackPolicyVersionAndActiveReadsItBack(t *testing.T) {
	w := newWorld(t)
	o := w.publish(tenantA, w.trustA)
	if o.code != 0 || o.rep.Result != "published" || o.rep.Policy != "fin.pol@3.1" || o.rep.TenantID != tenantA {
		t.Fatalf("exit %d, report %+v, stderr %q; want 0, published fin.pol@3.1", o.code, o.rep, o.err)
	}
	if o.rep.Event != "policy_publish" || o.rep.Service != "policypublish" || o.rep.Power != "trust" || o.rep.Level != "info" || !strings.HasPrefix(o.rep.CorrelationID, "corr_") {
		t.Fatalf("the log line lacks its fields: %+v", o.rep)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.1" {
		t.Fatalf("active %q, want fin.pol@3.1", v)
	}
	var typ, by, actor, ref, payload, corr string
	if err := w.admin.QueryRow(ctx(t), `SELECT event_type, written_by, actor, object_ref, payload::text, correlation_id FROM record_event WHERE tenant_id = $1`, tenantA).Scan(&typ, &by, &actor, &ref, &payload, &corr); err != nil {
		t.Fatal(err)
	}
	if typ != "POLICY_PUBLISHED" || by != "core-trust" || actor != "admin:adm_synth01" || ref != "pol_fin_pol_3_1" || corr != o.rep.CorrelationID {
		t.Fatalf("event %s by %s actor %s ref %s correlation %s", typ, by, actor, ref, corr)
	}
	// ruling on noema#771, interim (relayed by the Founder in session, not yet committed): the event carries the hash of the document, the one the log line and the row hold
	if o.rep.DocumentHash == "" || w.storedDocumentHash("3.1") != o.rep.DocumentHash {
		t.Fatalf("reported hash %q, stored %q", o.rep.DocumentHash, w.storedDocumentHash("3.1"))
	}
	for _, want := range []string{`"policy_id": "fin.pol"`, `"version": "3.1"`, `"published_by": "adm_synth01"`, `"document_hash": "` + o.rep.DocumentHash + `"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload %s lacks %s", payload, want)
		}
	}
	if n := w.count(`SELECT count(*) FROM policy_publication WHERE tenant_id = $1`, tenantB); n != 0 {
		t.Fatalf("tenant B has %d publications after a publication for tenant A", n)
	}
}

// Idempotent: a second run with the same pack changes nothing, says so, and exits 0 (a seed script may run twice).
func TestASecondRunWithTheSameVersionIsANoOpThatSaysAlreadyPublished(t *testing.T) {
	w := newWorld(t)
	if o := w.publish(tenantA, w.trustA); o.code != 0 {
		t.Fatalf("first run: exit %d %q", o.code, o.err)
	}
	o := w.publish(tenantA, w.trustA)
	if o.code != 0 || o.rep.Result != "already_published" || o.rep.Policy != "fin.pol@3.1" {
		t.Fatalf("second run: exit %d, report %+v, stderr %q; want 0, already_published", o.code, o.rep, o.err)
	}
	if n := w.count(`SELECT count(*) FROM policy_publication`); n != 1 {
		t.Fatalf("%d publications, want 1", n)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 1 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 1: a no-op writes nothing", n)
	}
	// the one event is the first run's, and carries its document's hash (ruling on noema#771, interim)
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED' AND payload->>'document_hash' = $1`, o.rep.DocumentHash); n != 1 {
		t.Fatalf("%d POLICY_PUBLISHED events carry the hash %s, want 1", n, o.rep.DocumentHash)
	}
}

// The version comes from the document, never from a flag: a newer document is published and becomes active; the older one after it is refused as policystore requires.
func TestANewerPackVersionPublishesAndAnOlderOneAfterItIsRefused(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	newer := variantPack(t, withVersion("3.2"))
	if o := w.run(w.args(tenantA, login, packRoot)...); o.code != 0 || o.rep.Policy != "fin.pol@3.1" {
		t.Fatalf("3.1: exit %d %+v", o.code, o.rep)
	}
	if o := w.run(w.args(tenantA, login, newer)...); o.code != 0 || o.rep.Result != "published" || o.rep.Policy != "fin.pol@3.2" {
		t.Fatalf("3.2: exit %d %+v %q", o.code, o.rep, o.err)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.2" {
		t.Fatalf("active %q, want fin.pol@3.2", v)
	}
	o := w.run(w.args(tenantA, login, packRoot)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "not_newer" || o.rep.Level != "error" || o.err == "" {
		t.Fatalf("3.1 after 3.2: exit %d, report %+v, stderr %q; want 1, refused, not_newer", o.code, o.rep, o.err)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.2" {
		t.Fatalf("active %q after the refused downgrade, want fin.pol@3.2", v)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 2 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 2: a refused downgrade leaves none", n)
	}
}

// REQ-SEC-004, INV-12: a tenant that does not exist, or that the login is not the Trust login of, is refused with one answer, before anything is written.
func TestAnUnknownTenantIsRefusedAndNothingIsWritten(t *testing.T) {
	w := newWorld(t)
	o := w.run(w.args("ten_zz", w.login("trust-a", w.trustA), packRoot)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "login_not_for_tenant" {
		t.Fatalf("unknown tenant: exit %d, report %+v, stderr %q", o.code, o.rep, o.err)
	}
	w.nothingWritten()
}

func TestAWrongLoginIsRefusedAndNothingIsWritten(t *testing.T) {
	w := newWorld(t)
	for name, dsn := range map[string]string{
		"another tenant's trust login": w.login("trust-b", w.trustB),
		"the tenant's execution login": w.login("exec-a", w.exeA),
		"the administrator (unmapped)": w.dsnFile("admin", w.adminDSN),
	} {
		o := w.run(w.args(tenantA, dsn, packRoot)...)
		if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "login_not_for_tenant" {
			t.Errorf("%s: exit %d, report %+v, stderr %q; want 1, refused, login_not_for_tenant", name, o.code, o.rep, o.err)
		}
	}
	w.nothingWritten()
}

// A login that may only read must not learn "already published" either: the login is checked before the active version is read.
func TestALoginThatIsNotTheTrustLoginIsRefusedEvenWhenThePolicyIsAlreadyPublished(t *testing.T) {
	w := newWorld(t)
	if o := w.publish(tenantA, w.trustA); o.code != 0 {
		t.Fatalf("publish: exit %d %q", o.code, o.err)
	}
	o := w.run(w.args(tenantA, w.login("exec-a", w.exeA), packRoot)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "login_not_for_tenant" {
		t.Fatalf("execution login: exit %d, report %+v; want refused", o.code, o.rep)
	}
}

// AGENT-13: no DSN, password, login name or driver text in the output, on success, on a refusal and when the database cannot be reached.
func TestTheOutputHoldsNoCredentialAndNoDriverText(t *testing.T) {
	w := newWorld(t)
	good := w.login("trust-a", w.trustA)
	cfg := w.trustA.Config().ConnConfig
	unreachable := w.dsnFile("down", "postgres://svc_secretname:SECRETPW@127.0.0.1:1/secretdb?sslmode=disable&connect_timeout=2")
	unknownKeys := t.TempDir()
	writeKey(t, unknownKeys, "a key nobody registered")
	appendFails := w.args(tenantA, good, packRoot)
	appendFails[keyDirAt] = unknownKeys
	cases := map[string]outcome{
		"append fails": w.run(appendFails...),
		"success":      w.run(w.args(tenantA, good, packRoot)...),
		"refusal":      w.run(w.args(tenantA, w.login("exec-a", w.exeA), packRoot)...),
		"unreachable":  w.run(w.args(tenantA, unreachable, packRoot)...),
	}
	if c := cases["unreachable"].code; c != 2 || cases["append fails"].code != 2 {
		t.Fatalf("an unreachable database: exit %d, a failed append: exit %d; want 2 for both", c, cases["append fails"].code)
	}
	for name, o := range cases {
		text := o.out + o.err
		for _, banned := range []string{cfg.User, "SECRETPW", "svc_secretname", "secretdb", "postgres://", "password", "pgconn", "ERROR:", "policystore", "record_insert", w.trustA.Config().ConnString()} {
			if banned != "" && strings.Contains(text, banned) {
				t.Errorf("%s: the output holds %q:\n%s", name, banned, text)
			}
		}
	}
}

func TestTheDocumentNamesThePolicyAndNoFlagDoes(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	for _, extra := range [][]string{{"--policy-id", "fin.other"}, {"--version", "9.9"}, {"--dsn", "postgres://x"}} {
		if o := w.run(append(w.args(tenantA, login, packRoot), extra...)...); o.code != 2 {
			t.Errorf("%v: exit %d, want 2 (unknown flag)", extra, o.code)
		}
	}
	w.nothingWritten()
}

func TestAMissingOrMalformedArgumentIsAUsageError(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	full := w.args(tenantA, login, packRoot)
	for i := 0; i < len(full); i += 2 {
		args := append(append([]string{}, full[:i]...), full[i+2:]...)
		if o := w.run(args...); o.code != 2 || !strings.Contains(o.err, "usage:") {
			t.Errorf("without %s: exit %d, stderr %q; want 2 and the usage line", full[i], o.code, o.err)
		}
	}
	for name, args := range map[string][]string{
		"a tenant that is not an ID": w.args("tenant-a", login, packRoot),
		"a max-active of zero":       append(w.args(tenantA, login, packRoot), "--key-max-active", "0"),
		"a stray argument":           append(w.args(tenantA, login, packRoot), "extra"),
		"no arguments":               nil,
	} {
		if o := w.run(args...); o.code != 2 || !strings.Contains(o.err, "usage:") {
			t.Errorf("%s: exit %d, stderr %q; want 2 and the usage line", name, o.code, o.err)
		}
	}
	w.nothingWritten()
}

// REQ-TRU-004: a document the policy loader refuses is never published: nothing is written.
func TestAnInvalidPolicyDocumentIsRefusedBeforeAnyWrite(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	rules := func(pol map[string]any) []any { return pol["rules"].([]any) }
	cases := map[string]string{
		"a rule for a capability the pack does not register": variantPack(t, func(pol map[string]any) bool {
			rules(pol)[0].(map[string]any)["applies_to"] = []any{"fin.cap.never_registered"}
			return true
		}),
		"a rule with an unknown effect": variantPack(t, func(pol map[string]any) bool {
			rules(pol)[0].(map[string]any)["effect"] = "ALLOW"
			return true
		}),
		"no policy in the pack": variantPack(t, func(map[string]any) bool { return false }),
		"no policy ID":          variantPack(t, func(pol map[string]any) bool { delete(pol, "policy_id"); return true }),
	}
	for name, dir := range cases {
		o := w.run(w.args(tenantA, login, dir)...)
		if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "invalid_policy" {
			t.Errorf("%s: exit %d, report %+v, stderr %q; want 1, refused, invalid_policy", name, o.code, o.rep, o.err)
		}
	}
	w.nothingWritten()
}

// The publication store's patterns are not the command's to restate: a version or a publisher it refuses is refused with its own reason, and nothing is written.
func TestARequestOutsideThePublicationStoresPatternsIsRefused(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	if o := w.run(w.args(tenantA, login, variantPack(t, withVersion("3.1.0")))...); o.code != 1 || o.rep.Reason != "invalid_request" {
		t.Errorf("version 3.1.0: exit %d, report %+v; want 1, invalid_request", o.code, o.rep)
	}
	args := w.args(tenantA, login, packRoot)
	args[len(args)-1] = "ADMIN"
	if o := w.run(args...); o.code != 1 || o.rep.Reason != "invalid_request" {
		t.Errorf("publisher ADMIN: exit %d, report %+v; want 1, invalid_request", o.code, o.rep)
	}
	w.nothingWritten()
}

// The pack file is read inside the pack directory only: a name with a separator, and a link that leaves the directory, cannot be read.
func TestAPackFileThatLeavesThePackDirectoryCannotBeRead(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	real, err := filepath.Abs(filepath.Join(packRoot, packName))
	if err != nil {
		t.Fatal(err)
	}
	linked := t.TempDir()
	if err := os.Symlink(real, filepath.Join(linked, packName)); err != nil {
		t.Fatal(err)
	}
	for name, args := range map[string][]string{
		"a link out of the directory": w.args(tenantA, login, linked),
		"a parent path":               append(w.args(tenantA, login, packRoot+"/fintech"), "--pack-file", "../fintech_v1.json"),
		"an absolute path":            append(w.args(tenantA, login, packRoot), "--pack-file", real),
		"a missing file":              append(w.args(tenantA, login, packRoot), "--pack-file", "missing.json"),
		"a missing directory":         w.args(tenantA, login, filepath.Join(t.TempDir(), "nowhere")),
	} {
		if o := w.run(args...); o.code != 2 {
			t.Errorf("%s: exit %d, want 2", name, o.code)
		}
	}
	w.nothingWritten()
}

func TestADsnFileThatCannotBeUsedIsRefused(t *testing.T) {
	w := newWorld(t)
	// An empty DSN must not fall back to the ambient PG* settings: they name the tenant's own login here, so a fallback would publish.
	cfg := w.trustA.Config().ConnConfig
	t.Setenv("PGHOST", cfg.Host)
	t.Setenv("PGPORT", strconv.Itoa(int(cfg.Port)))
	t.Setenv("PGUSER", cfg.User)
	t.Setenv("PGDATABASE", cfg.Database)
	t.Setenv("PGSSLMODE", "disable")
	empty := w.dsnFile("empty", "")
	garbage := w.dsnFile("garbage", "this is not a DSN SECRETWORD")
	link := filepath.Join(w.files, "link")
	if err := os.Symlink(w.login("trust-a", w.trustA), link); err != nil {
		t.Fatal(err)
	}
	for name, path := range map[string]string{"missing": filepath.Join(w.files, "nowhere"), "empty": empty, "unparsable": garbage, "a link": link, "a directory": w.files} {
		o := w.run(w.args(tenantA, path, packRoot)...)
		if o.code != 2 {
			t.Errorf("%s: exit %d, want 2", name, o.code)
		}
		if strings.Contains(o.out+o.err, "SECRETWORD") {
			t.Errorf("%s: the output quotes the file", name)
		}
	}
	w.nothingWritten()
}

// PERD 32.4: the Record comes first. A signing key the database does not know makes the append fail; the publication, written in the same transaction, is not kept.
func TestWhenTheRecordAppendFailsNoPublicationIsKept(t *testing.T) {
	w := newWorld(t)
	unknown := t.TempDir()
	writeKey(t, unknown, "a key nobody registered")
	args := w.args(tenantA, w.login("trust-a", w.trustA), packRoot)
	args[keyDirAt] = unknown
	if o := w.run(args...); o.code != 2 || o.rep.Result != "error" {
		t.Fatalf("exit %d, report %+v, stderr %q; want 2, error", o.code, o.rep, o.err)
	}
	w.nothingWritten()
	if v := w.activeAsExecution(); v != "" {
		t.Fatalf("active %q, want none", v)
	}
}

func TestAKeyDirectoryWithNoKeyCannotSign(t *testing.T) {
	w := newWorld(t)
	args := w.args(tenantA, w.login("trust-a", w.trustA), packRoot)
	args[keyDirAt] = t.TempDir()
	if o := w.run(args...); o.code != 2 {
		t.Fatalf("exit %d, want 2", o.code)
	}
	args[keyDirAt] = filepath.Join(t.TempDir(), "nowhere")
	if o := w.run(args...); o.code != 2 {
		t.Fatalf("a missing key directory: exit %d, want 2", o.code)
	}
	w.nothingWritten()
}

// Two seed scripts at once: both succeed, one publishes, the other finds it already published (the database serialises the version check).
func TestTwoRunsAtOnceLeaveOnePublicationAndBothSucceed(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	const runs = 6
	results := make([]outcome, runs)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = w.run(w.args(tenantA, login, packRoot)...)
		}()
	}
	wg.Wait()
	published := 0
	for i, o := range results {
		if o.code != 0 {
			t.Errorf("run %d: exit %d, report %+v, stderr %q", i, o.code, o.rep, o.err)
		}
		if o.rep.Result == "published" {
			published++
		} else if o.rep.Result != "already_published" {
			t.Errorf("run %d: result %q", i, o.rep.Result)
		}
	}
	if published != 1 {
		t.Fatalf("%d runs published, want exactly 1", published)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 1 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 1", n)
	}
}

// REQ-SEC-005: the rotation overlap is the caller's number and is applied as given: two mounted keys are refused under 1 and accepted under 2, and the newest signs.
func TestTheKeyOverlapIsTheCallersNumber(t *testing.T) {
	w := newWorld(t)
	seed := make([]byte, 32)
	copy(seed, "policypublish second key")
	if err := os.WriteFile(filepath.Join(w.keyDir, "core-trust.ed25519.2.key"), []byte(hex.EncodeToString(seed)+"\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	ring, err := signing.LoadRing("core-trust", w.keyDir, signing.Options{MaxActive: 2})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.admin.Exec(ctx(t), `SELECT key_register('core-trust', 2, $1)`, []byte(ring.PublicKeys()["core-trust.ed25519.2"])); err != nil {
		t.Fatal(err)
	}
	args := w.args(tenantA, w.login("trust-a", w.trustA), packRoot)
	args[keyDirAt+2] = "1"
	if o := w.run(args...); o.code != 2 {
		t.Fatalf("two keys under a limit of 1: exit %d, want 2", o.code)
	}
	w.nothingWritten()
	args[keyDirAt+2] = "2"
	if o := w.run(args...); o.code != 0 || o.rep.Result != "published" {
		t.Fatalf("two keys under a limit of 2: exit %d, report %+v, stderr %q", o.code, o.rep, o.err)
	}
	var keyID string
	if err := w.admin.QueryRow(ctx(t), `SELECT sig_key_id FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`).Scan(&keyID); err != nil {
		t.Fatal(err)
	}
	if keyID != "core-trust.ed25519.2" {
		t.Fatalf("signed under %s, want the newest key core-trust.ed25519.2", keyID)
	}
}

// AGENT-13: the error value of a failed connection holds no part of the DSN, so no caller can print one by accident.
func TestAConnectionErrorValueCarriesNoCredential(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dsn")
	if err := os.WriteFile(path, []byte("postgres://svc_secretname:SECRETPW@127.0.0.1:1/secretdb?sslmode=disable&connect_timeout=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := connect(ctx(t), path)
	if err == nil {
		t.Fatal("connected to a closed port")
	}
	for _, banned := range []string{"svc_secretname", "SECRETPW", "secretdb", "127.0.0.1"} {
		if strings.Contains(err.Error(), banned) {
			t.Errorf("the error %q holds %q", err.Error(), banned)
		}
	}
}

// REQ-TRU-005, INV-09: the report carries the document's hash (and only the hash), and it is the hash the publication row keeps and the one a loader computes from the same file.
func TestThePublicationCarriesTheHashOfThePacksPolicyDocument(t *testing.T) {
	w := newWorld(t)
	want := packPolicyHash(t, packRoot)
	o := w.publish(tenantA, w.trustA)
	if o.code != 0 || o.rep.Result != "published" {
		t.Fatalf("exit %d, report %+v, stderr %q", o.code, o.rep, o.err)
	}
	if o.rep.DocumentHash != want {
		t.Fatalf("reported hash %q, want %q", o.rep.DocumentHash, want)
	}
	if got := w.storedDocumentHash("3.1"); got != want {
		t.Fatalf("stored hash %q, want %q", got, want)
	}
}

// AGENT-13: the output names the hash and never the document: no rule ID, reason code or condition of the policy appears on stdout or stderr.
func TestTheOutputHoldsTheHashAndNeverTheDocument(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	all := w.run(w.args(tenantA, login, packRoot)...)
	again := w.run(w.args(tenantA, login, packRoot)...)
	changed := w.run(w.args(tenantA, login, variantPack(t, changeReason))...)
	for name, o := range map[string]outcome{"published": all, "already published": again, "document changed": changed} {
		for _, leak := range []string{"fin.pol.prohibition.remedy", "PROHIBITED_CAPABILITY", "fin.reason.changed_text", "case_id != null", "PAYMENTS_OPERATIONS", `"rules"`, "applies_to"} {
			if strings.Contains(o.out, leak) || strings.Contains(o.err, leak) {
				t.Errorf("%s: the output holds %q", name, leak)
			}
		}
	}
	if !strings.Contains(all.out, `"document_hash":"sha256:`) {
		t.Errorf("the report has no document_hash: %s", all.out)
	}
}

// REQ-TRU-005, INV-21: the same document run again is one publication, whatever its spelling: the document is hashed in its canonical form, so key order and whitespace do not count.
func TestTheSameDocumentInAnotherSpellingIsAlreadyPublished(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	first := w.run(w.args(tenantA, login, packRoot)...)
	respelt := variantPack(t, sameDocument)
	if a, b := packPolicyHash(t, packRoot), packPolicyHash(t, respelt); a != b {
		t.Fatalf("fixture error: the two spellings hash differently: %s %s", a, b)
	}
	o := w.run(w.args(tenantA, login, respelt)...)
	if o.code != 0 || o.rep.Result != "already_published" || o.rep.DocumentHash != first.rep.DocumentHash {
		t.Fatalf("exit %d, report %+v, stderr %q; want 0, already_published, the first run's hash", o.code, o.rep, o.err)
	}
	if n := w.count(`SELECT count(*) FROM policy_publication`); n != 1 {
		t.Fatalf("%d publications, want 1", n)
	}
}

// REQ-TRU-005, INV-09, INV-21 (closes the STOP of the first session): a different document under the version that is already active is refused with its own reason, never reported as
// already published; nothing is written, the stored hash and the active version are unchanged.
func TestADifferentDocumentUnderTheActiveVersionIsRefusedAsDocumentChanged(t *testing.T) {
	w := newWorld(t)
	login := w.login("trust-a", w.trustA)
	first := w.run(w.args(tenantA, login, packRoot)...)
	if first.code != 0 {
		t.Fatalf("first run: exit %d %q", first.code, first.err)
	}
	dir := variantPack(t, changeReason)
	if packPolicyHash(t, dir) == first.rep.DocumentHash {
		t.Fatal("fixture error: the changed pack hashes like the first")
	}
	o := w.run(w.args(tenantA, login, dir)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "document_changed" || o.rep.Level != "error" || o.err == "" {
		t.Fatalf("exit %d, report %+v, stderr %q; want 1, refused, document_changed", o.code, o.rep, o.err)
	}
	if o.rep.Result == "already_published" || o.rep.Policy != "fin.pol@3.1" {
		t.Fatalf("report %+v", o.rep)
	}
	if o.rep.DocumentHash != packPolicyHash(t, dir) {
		t.Fatalf("the refusal reports hash %q, want the hash of the document that was offered", o.rep.DocumentHash)
	}
	if got := w.storedDocumentHash("3.1"); got != first.rep.DocumentHash {
		t.Fatalf("stored hash %q changed from %q", got, first.rep.DocumentHash)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.1" {
		t.Fatalf("active %q, want fin.pol@3.1", v)
	}
	if n := w.count(`SELECT count(*) FROM policy_publication`); n != 1 {
		t.Fatalf("%d publications, want 1", n)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 1 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 1", n)
	}
	// the changed document is published under a new version, as the refusal tells the operator to do
	next := variantPack(t, func(pol map[string]any) bool { changeReason(pol); pol["version"] = "3.2"; return true })
	if o := w.run(w.args(tenantA, login, next)...); o.code != 0 || o.rep.Result != "published" {
		t.Fatalf("the changed document under 3.2: exit %d, report %+v, stderr %q", o.code, o.rep, o.err)
	}
}

// REQ-SEC-004: one tenant's refusal does not touch another's publication of the same version, and each keeps its own hash.
func TestDocumentHashesAreKeptPerTenant(t *testing.T) {
	w := newWorld(t)
	a, b := w.login("trust-a", w.trustA), w.login("trust-b", w.trustB)
	changed := variantPack(t, changeReason)
	if o := w.run(w.args(tenantA, a, packRoot)...); o.code != 0 {
		t.Fatalf("A: exit %d %q", o.code, o.err)
	}
	if o := w.run(w.args(tenantB, b, changed)...); o.code != 0 || o.rep.Result != "published" {
		t.Fatalf("B publishes the other document under the same version: exit %d, report %+v, stderr %q", o.code, o.rep, o.err)
	}
	if o := w.run(w.args(tenantA, a, changed)...); o.code != 1 || o.rep.Reason != "document_changed" {
		t.Fatalf("A with B's document: exit %d, report %+v", o.code, o.rep)
	}
	if o := w.run(w.args(tenantB, b, changed)...); o.code != 0 || o.rep.Result != "already_published" {
		t.Fatalf("B again: exit %d, report %+v", o.code, o.rep)
	}
	var ha, hb string
	if err := w.admin.QueryRow(ctx(t), `SELECT (SELECT document_hash FROM policy_publication WHERE tenant_id = $1), (SELECT document_hash FROM policy_publication WHERE tenant_id = $2)`, tenantA, tenantB).Scan(&ha, &hb); err != nil {
		t.Fatal(err)
	}
	if ha != packPolicyHash(t, packRoot) || hb != packPolicyHash(t, changed) || ha == hb {
		t.Fatalf("hashes A %s, B %s", ha, hb)
	}
}

// REQ-TRU-005, INV-21: the active version was published before the hash existed, so there is no hash to compare. The command does not guess: it refuses with its own reason, writes
// nothing, and a new version publishes as usual.
func TestALegacyPublicationWithNoHashIsRefusedAsDocumentUnknownAndANewVersionPublishes(t *testing.T) {
	w := newLegacyWorld(t, "3.1")
	login := w.login("trust-a", w.trustA)
	o := w.run(w.args(tenantA, login, packRoot)...)
	if o.code != 1 || o.rep.Result != "refused" || o.rep.Reason != "document_unknown" || o.err == "" {
		t.Fatalf("exit %d, report %+v, stderr %q; want 1, refused, document_unknown", o.code, o.rep, o.err)
	}
	if n := w.count(`SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 0 {
		t.Fatalf("%d POLICY_PUBLISHED events, want 0", n)
	}
	if n := w.count(`SELECT count(*) FROM policy_publication`); n != 1 {
		t.Fatalf("%d publications, want 1", n)
	}
	if v := w.activeAsExecution(); v != "fin.pol@3.1" {
		t.Fatalf("active %q, want fin.pol@3.1", v)
	}
	newer := variantPack(t, withVersion("3.2"))
	if o := w.run(w.args(tenantA, login, newer)...); o.code != 0 || o.rep.Result != "published" || o.rep.DocumentHash != packPolicyHash(t, newer) {
		t.Fatalf("3.2 over a legacy 3.1: exit %d, report %+v, stderr %q", o.code, o.rep, o.err)
	}
	if got := w.storedDocumentHash("3.2"); got != packPolicyHash(t, newer) || w.storedDocumentHash("3.1") != "" {
		t.Fatalf("stored hashes: 3.2 %q, 3.1 %q", got, w.storedDocumentHash("3.1"))
	}
	// the older version after the legacy one is still just not newer
	if o := w.run(w.args(tenantA, login, packRoot)...); o.code != 1 || o.rep.Reason != "not_newer" {
		t.Fatalf("3.1 after 3.2: exit %d, report %+v", o.code, o.rep)
	}
}
