package db_test

// Ruling of 8 Nov 2026 on noema#329 (SR review of noema#168, notes 2 and 3): the database refuses a Decision Record without its bound fields (A13), and a
// Decision Grant that does not agree with the record it came from (REQ-TRU-008, INV-17). Execution still revalidates at spend (E4); these rules mean the
// Record can never show a grant its decision does not authorise. All values are SYNTHETIC.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// recordInsert builds a decision_record insert whose document is complete (testpg.BoundFieldsSQL) and then edited by extra, a jsonb expression applied with ||
// (or, for removal, a "- 'key'" expression applied by remove).
func recordInsert(id string, extra string, remove string) string {
	doc := `(jsonb_build_object('decision_id', '` + id + `', 'tenant_id', 'ten_a', 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k1', 'value', 'aa')) || ` + testpg.BoundFieldsSQL + ` || ` + extra + `)` + remove
	return `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ('` + id + `', 'ten_a', ` + doc + `, 'ALLOW', '` + testpg.FixtureIntentHash + `', '[]', 'k1', 'aa')`
}

// grantInsert builds a decision_grant insert for decision dec whose document is complete, then overridden by extra (a jsonb expression) and trimmed by
// remove (a "- 'key'" expression), and whose expires_at column is colExpires.
func grantInsert(grantID, dec, extra, remove, colExpires string) string {
	doc := `(jsonb_build_object('decision_id', '` + dec + `', 'tenant_id', 'ten_a') || ` + testpg.GrantBoundSQL + ` || ` + extra + `)` + remove
	return fmt.Sprintf(`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ('%[1]s', 'ten_a', '%[2]s', 'n_%[1]s', %[3]s, 'pol_1', '1', %[4]s, 'k1', 'aa')`, grantID, dec, doc, colExpires)
}

// ruleWorld returns try (a statement in a transaction that is rolled back, so it proves a refusal or an acceptance and leaves nothing) and put (a
// statement that stays, for the record a grant refers to). Both run as the tenant's Trust login.
func ruleWorld(t *testing.T) (try, put func(string) error) {
	t.Helper()
	pool, dsn := migratedDSN(t)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	login := testpg.TenantLogin(t, pool, dsn, "trust_rw", "ten_a")
	return func(stmt string) error { return tryAs(t, login, stmt) }, func(stmt string) error { _, err := login.Exec(ctx(t), stmt); return err }
}

// Rule (d): a record carries intent_id, correlation_id, policy_version, intent_hash, issued_at and expires_at, and expires_at is after issued_at.
func TestADecisionRecordMustCarryItsBoundFieldsAndAWindow(t *testing.T) {
	try, _ := ruleWorld(t)
	for _, key := range []string{"intent_id", "correlation_id", "policy_version", "intent_hash", "issued_at", "expires_at"} {
		wantCode(t, try(recordInsert("dec_miss_"+key, `'{}'::jsonb`, " - '"+key+"'")), violation, "a record with no "+key)
		wantCode(t, try(recordInsert("dec_empty_"+key, `jsonb_build_object('`+key+`', '')`, "")), violation, "a record with an empty "+key)
		wantCode(t, try(recordInsert("dec_null_"+key, `jsonb_build_object('`+key+`', 7)`, "")), violation, "a record whose "+key+" is not a string")
	}
	wantCode(t, try(recordInsert("dec_bad_time", `jsonb_build_object('issued_at', 'yesterday')`, "")), violation, "a record whose issued_at is not a timestamp")
	wantCode(t, try(recordInsert("dec_bad_exp", `jsonb_build_object('expires_at', '2026-13-45T25:61:61.000Z')`, "")), violation, "a record whose expires_at is not a real time")
	wantCode(t, try(recordInsert("dec_back", `jsonb_build_object('expires_at', '`+testpg.FixtureIssuedAt+`')`, "")), violation, "a record that expires when it is issued")
	wantCode(t, try(recordInsert("dec_before", `jsonb_build_object('expires_at', '2026-11-07T08:59:59.000Z')`, "")), violation, "a record that expires before it is issued")
	if err := try(recordInsert("dec_ok", `'{}'::jsonb`, "")); err != nil {
		t.Fatalf("a complete record: %v", err)
	}
}

// Rule (a): a grant does not outlive its record, by the document and by the column.
func TestAGrantCannotOutliveItsDecisionRecord(t *testing.T) {
	try, put := ruleWorld(t)
	// the record expires at 10:00:00.000Z; testpg.GrantBoundSQL already says 10:00:00.000Z for a grant
	if err := put(recordInsert("dec_short", `jsonb_build_object('expires_at', '2026-11-07T10:00:00.000Z')`, "")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, try(grantInsert("grt_doc_late", "dec_short", `jsonb_build_object('expires_at', '2026-11-07T10:00:00.001Z')`, "", `'2026-11-07T10:00:00Z'::timestamptz`)), violation, "the grant document expires after its record")
	wantCode(t, try(grantInsert("grt_col_late", "dec_short", `'{}'::jsonb`, "", `'2026-11-07T10:00:01Z'::timestamptz`)), violation, "the grant column expires after its record")
	wantCode(t, try(grantInsert("grt_doc_bad", "dec_short", `jsonb_build_object('expires_at', 'soon')`, "", `'2026-11-07T10:00:00Z'::timestamptz`)), violation, "a grant document with no real expiry")
	wantCode(t, try(grantInsert("grt_doc_none", "dec_short", `'{}'::jsonb`, " - 'expires_at'", `'2026-11-07T10:00:00Z'::timestamptz`)), violation, "a grant document with no expiry")
	if err := try(grantInsert("grt_equal", "dec_short", `'{}'::jsonb`, "", `'2026-11-07T10:00:00Z'::timestamptz`)); err != nil {
		t.Fatalf("a grant that expires with its record: %v", err)
	}
}

// Rule (b): the grant's intent hash is its record's.
func TestAGrantMustCarryTheIntentHashOfItsDecisionRecord(t *testing.T) {
	try, put := ruleWorld(t)
	if err := put(recordInsert("dec_h", `'{}'::jsonb`, "")); err != nil {
		t.Fatal(err)
	}
	other := "sha256:" + strings.Repeat("1", 64)
	wantCode(t, try(grantInsert("grt_h1", "dec_h", `jsonb_build_object('intent_hash', '`+other+`')`, "", `now() + interval '1 minute'`)), violation, "a grant for another intent")
	wantCode(t, try(grantInsert("grt_h2", "dec_h", `'{}'::jsonb`, " - 'intent_hash'", `now() + interval '1 minute'`)), violation, "a grant with no intent hash")
	if err := try(grantInsert("grt_h3", "dec_h", `'{}'::jsonb`, "", `now() + interval '1 minute'`)); err != nil {
		t.Fatalf("a grant for the record's own intent: %v", err)
	}
}

// Rule (c): the grant's policy_id@version is its record's.
func TestAGrantMustCarryThePolicyVersionOfItsDecisionRecord(t *testing.T) {
	try, put := ruleWorld(t)
	if err := put(recordInsert("dec_p", `'{}'::jsonb`, "")); err != nil {
		t.Fatal(err)
	}
	wantCode(t, try(grantInsert("grt_p1", "dec_p", `jsonb_build_object('policy_version', 'pol_1@2')`, "", `now() + interval '1 minute'`)), violation, "a grant under another policy version")
	wantCode(t, try(grantInsert("grt_p2", "dec_p", `jsonb_build_object('policy_version', 'pol_2@1')`, "", `now() + interval '1 minute'`)), violation, "a grant under another policy")
	wantCode(t, try(grantInsert("grt_p3", "dec_p", `'{}'::jsonb`, " - 'policy_version'", `now() + interval '1 minute'`)), violation, "a grant with no policy version")
	if err := try(grantInsert("grt_p4", "dec_p", `'{}'::jsonb`, "", `now() + interval '1 minute'`)); err != nil {
		t.Fatalf("a grant under the record's own policy version: %v", err)
	}
}
