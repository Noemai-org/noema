package snapshot_test

// Architect sync of 10 Nov 2026, ruling A119: a Decision Grant's evidence_set_hash must be the evidence_set_hash of the sealed snapshot the decision names, compared by a
// database trigger at grant ISSUE. Execution's revalidation at spend is unchanged. REQ-TRU-008, REQ-CTX-004, INV-17. All data is SYNTHETIC.

import (
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

const integrityViolation = "23000"

// decisionRecordSQL inserts an ALLOW record for tenant whose document names snapshot (empty = names none).
func decisionRecordSQL(tenant, decision, snapshotID string) string {
	extra := `'{}'::jsonb`
	if snapshotID != "" {
		extra = `jsonb_build_object('evidence_snapshot_id', '` + snapshotID + `')`
	}
	doc := `(jsonb_build_object('decision_id', '` + decision + `', 'tenant_id', '` + tenant + `', 'outcome', 'ALLOW', 'sig', jsonb_build_object('key_id', 'k1', 'value', 'aa')) || ` +
		testpg.BoundFieldsSQL + ` || ` + extra + `)`
	return `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ('` + decision + `', '` + tenant + `', ` + doc + `, 'ALLOW', '` + testpg.FixtureIntentHash + `', '[]', 'k1', 'aa')`
}

// grantSQL inserts a grant for decision whose document carries evidenceHash (empty = carries none).
func grantSQL(tenant, grant, decision, evidenceHash string) string {
	extra := `'{}'::jsonb`
	if evidenceHash != "" {
		extra = `jsonb_build_object('evidence_set_hash', '` + evidenceHash + `')`
	}
	doc := `(jsonb_build_object('decision_id', '` + decision + `', 'tenant_id', '` + tenant + `') || ` + testpg.GrantBoundSQL + ` || ` + extra + `)`
	return fmt.Sprintf(`INSERT INTO decision_grant (grant_id, tenant_id, decision_id, nonce, json, policy_id, policy_version, expires_at, sig_key_id, sig_value)
		VALUES ('%[1]s', '%[2]s', '%[3]s', 'n_%[1]s', %[4]s, 'pol_1', '1', now() + interval '1 minute', 'k1', 'aa')`, grant, tenant, decision, doc)
}

// try runs stmt in a transaction that is rolled back, so it proves a refusal or an acceptance and leaves nothing behind.
func try(t *testing.T, p *pgxpool.Pool, stmt string) error {
	t.Helper()
	tx, err := p.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	_, err = tx.Exec(ctx(t), stmt)
	return err
}

func TestAGrantMustCarryTheEvidenceSetHashOfTheSnapshotItsDecisionNames(t *testing.T) {
	w := newWorld(t)
	a, b := w.item(t, tenantA, 1), w.item(t, tenantA, 2)
	good := honest(t, 1, 2)
	if err := insertSnap(w.trust[tenantA], ctx(t), tenantA, "evs_aa1", []string{a, b}, good); err != nil {
		t.Fatal(err)
	}
	other := w.item(t, tenantA, 3)
	if err := insertSnap(w.trust[tenantA], ctx(t), tenantA, "evs_aa2", []string{other}, honest(t, 3)); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		decisionRecordSQL(tenantA, "dec_bound", "evs_aa1"),
		decisionRecordSQL(tenantA, "dec_nosnap", ""),
		decisionRecordSQL(tenantA, "dec_missing", "evs_aa9"),
	} {
		if _, err := w.trust[tenantA].Exec(ctx(t), stmt); err != nil {
			t.Fatalf("decision record: %v", err)
		}
	}
	other64 := "sha256:" + fmt.Sprintf("%064x", 7)
	cases := []struct {
		name  string
		stmt  string
		wants string // "" accepted
	}{
		{"the grant carries the snapshot's own hash", grantSQL(tenantA, "grt_ok", "dec_bound", good), ""},
		{"the grant carries another hash", grantSQL(tenantA, "grt_other", "dec_bound", other64), integrityViolation},
		{"the grant carries the hash of another sealed snapshot", grantSQL(tenantA, "grt_snap2", "dec_bound", honest(t, 3)), integrityViolation},
		{"the grant carries no evidence_set_hash", grantSQL(tenantA, "grt_none", "dec_bound", ""), integrityViolation},
		{"the decision names no snapshot", grantSQL(tenantA, "grt_nosnap", "dec_nosnap", good), integrityViolation},
		{"the decision names a snapshot that does not exist", grantSQL(tenantA, "grt_missing", "dec_missing", good), integrityViolation},
	}
	for _, c := range cases {
		if got := code(try(t, w.trust[tenantA], c.stmt)); got != c.wants {
			t.Errorf("%s: SQLSTATE %q, want %q", c.name, got, c.wants)
		}
	}
}

// A snapshot of another tenant is not a snapshot the decision may name: the lookup is tenant-bound, so it finds nothing and the grant is refused.
func TestADecisionCannotNameAnotherTenantsSnapshot(t *testing.T) {
	w := newWorld(t)
	id := w.item(t, tenantB, 1)
	good := honestB(t, 1)
	if err := insertSnap(w.trust[tenantB], ctx(t), tenantB, "evs_bb1", []string{id}, good); err != nil {
		t.Fatal(err)
	}
	if _, err := w.trust[tenantA].Exec(ctx(t), decisionRecordSQL(tenantA, "dec_cross", "evs_bb1")); err != nil {
		t.Fatalf("decision record: %v", err)
	}
	if got := code(try(t, w.trust[tenantA], grantSQL(tenantA, "grt_cross", "dec_cross", good))); got != integrityViolation {
		t.Errorf("a grant whose decision names another tenant's snapshot: SQLSTATE %q, want %q", got, integrityViolation)
	}
}

// honestB is the evidence_set_hash of tenant B's items numbered ns (the shared helper builds tenant A's IDs).
func honestB(t *testing.T, ns ...int) string {
	t.Helper()
	var pairs []snapshot.Pair
	for _, n := range ns {
		pairs = append(pairs, snapshot.Pair{EvidenceID: fmt.Sprintf("evr_bb%d", n), PayloadHash: hashN(n)})
	}
	h, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	return h
}
