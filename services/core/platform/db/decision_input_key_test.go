package db_test

// TRU-05 follow-up (A172, A186; migration 0027): a Decision Record is unique for its full input tuple: the tenant, the intent ID, the intent hash, the sealed
// context snapshot and the policy version. The same tuple can never hold two decisions, so a retried or resumed run reads the one that is stored; a new
// snapshot or another policy version is a new decision. All rows are SYNTHETIC.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// inputKey is the tuple of A186, as it sits in the database: the tenant and the intent hash are columns, the other three are members of the signed document.
type inputKey struct{ tenant, intentID, intentHash, snapshot, policy string }

func keyHash(n int) string { return fmt.Sprintf("sha256:%064x", n) }

const keyedRow = `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
	VALUES ($1, $2, jsonb_build_object('decision_id', $1::text, 'tenant_id', $2::text, 'outcome', 'REJECT', 'sig', jsonb_build_object('key_id', 'k1', 'value', 'aa')) || ` + testpg.BoundFieldsSQL + `
	  || jsonb_build_object('intent_id', $3::text, 'intent_hash', $4::text, 'evidence_snapshot_id', $5::text, 'policy_version', $6::text),
	  'REJECT', $4, '[]', 'k1', 'aa')`

func TestADecisionRecordInputTupleIsUnique(t *testing.T) {
	pool := migrated(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	insert := func(k inputKey) error {
		n++
		_, err := pool.Exec(ctx(t), keyedRow, fmt.Sprintf("dec_%d", n), k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy)
		return err
	}
	base := inputKey{"ten_a", "int_01", keyHash(1), "evs_01", "fin.pol@3.1"}
	if err := insert(base); err != nil {
		t.Fatalf("the first decision of a tuple: %v", err)
	}
	// The same tuple again, under another decision ID, is refused by the named index and by nothing else.
	err := insert(base)
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "23505" || pe.ConstraintName != "decision_record_input_key" {
		t.Fatalf("a second decision for the same input tuple: got %v, want a unique violation of decision_record_input_key", err)
	}
	// Each member of the tuple matters: changing exactly one of them is another input and another decision.
	for name, k := range map[string]inputKey{
		"another tenant":         {"ten_b", base.intentID, base.intentHash, base.snapshot, base.policy},
		"another intent":         {base.tenant, "int_02", base.intentHash, base.snapshot, base.policy},
		"another intent hash":    {base.tenant, base.intentID, keyHash(2), base.snapshot, base.policy},
		"a new context snapshot": {base.tenant, base.intentID, base.intentHash, "evs_02", base.policy},
		"another policy version": {base.tenant, base.intentID, base.intentHash, base.snapshot, "fin.pol@3.2"},
	} {
		if err := insert(k); err != nil {
			t.Errorf("%s is a different input and must be a different decision: %v", name, err)
		}
	}
}

// A document with no context snapshot is not covered by the index. decision-record.v2 requires the snapshot and the writer refuses a request without one
// (ErrNoEvaluatedBasis), so only the direct inserts of the schema tests lack it; they write many records for one tenant and must keep working.
func TestARecordWithNoSnapshotIsNotKeyed(t *testing.T) {
	pool := migrated(t)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"dec_a", "dec_b"} {
		if _, err := pool.Exec(ctx(t), decisionRow(id, "ten_a", "REJECT")); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
}
