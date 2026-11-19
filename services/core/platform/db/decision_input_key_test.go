package db_test

// TRU-05 follow-up (A172, A186; migration 0027) and TRU-07 (A366; migration 0034): a Decision Record is unique for its full input tuple, the tenant, the intent ID, the intent hash,
// the sealed context snapshot and the policy version, AND its confirmation state, the confirmation request it rests on (NULL for none). The same tuple and state can never hold two
// decisions, so a retried or resumed run reads the one that is stored; a new snapshot or another policy version is a new decision, and so is a decision that rests on a confirmation
// (the ALLOW that follows an ACCEPT) next to the CONFIRM of the same snapshot. All rows are SYNTHETIC.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

// inputKey is the tuple of A186, as it sits in the database: the tenant and the intent hash are columns, the other three are members of the signed document.
type inputKey struct{ tenant, intentID, intentHash, snapshot, policy string }

// keyedState is the confirmation state a row rests on: the confirmation request, or nil for none (the column is NULL).
func keyedState(request string) any {
	if request == "" {
		return nil
	}
	return request
}

func keyHash(n int) string { return fmt.Sprintf("sha256:%064x", n) }

const keyedRow = `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value, confirmation_request_id)
	VALUES ($1, $2, jsonb_build_object('decision_id', $1::text, 'tenant_id', $2::text, 'outcome', 'REJECT', 'sig', jsonb_build_object('key_id', 'k1', 'value', 'aa')) || ` + testpg.BoundFieldsSQL + `
	  || jsonb_build_object('intent_id', $3::text, 'intent_hash', $4::text, 'evidence_snapshot_id', $5::text, 'policy_version', $6::text),
	  'REJECT', $4, '[]', 'k1', 'aa', $7)`

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
		_, err := pool.Exec(ctx(t), keyedRow, fmt.Sprintf("dec_%d", n), k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy, nil)
		return err
	}
	base := inputKey{"ten_a", "int_01", keyHash(1), "evs_01", "fin.pol@3.1"}
	if err := insert(base); err != nil {
		t.Fatalf("the first decision of a tuple: %v", err)
	}
	// The same tuple again, under another decision ID, is refused by the named index and by nothing else.
	err := insert(base)
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || pe.Code != "23505" || pe.ConstraintName != "decision_record_input_state_key" {
		t.Fatalf("a second decision for the same input tuple and state: got %v, want a unique violation of decision_record_input_state_key", err)
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

// A366, INV-21 (migration 0034): the key is the tuple and the confirmation state. Two decisions of one tuple that rest on NO confirmation are one too many (two NULLs are not distinct: the
// key is over COALESCE of the column); one that rests on no confirmation and one that rests on a request are two decisions; a second of the same request is one too many; another request,
// or another tenant with the same request, is another key. (Which of two requests the Writer accepts is its own rule, trust/recorder.)
func TestADecisionRecordInputTupleIsUniquePerConfirmationState(t *testing.T) {
	pool := migrated(t)
	for _, q := range []string{`INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`, `INSERT INTO tenant (tenant_id, name) VALUES ('ten_b', 'B')`} {
		if _, err := pool.Exec(ctx(t), q); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	insert := func(k inputKey, state string) error {
		n++
		_, err := pool.Exec(ctx(t), keyedRow, fmt.Sprintf("dec_%d", n), k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy, keyedState(state))
		return err
	}
	conflict := func(name string, err error) {
		t.Helper()
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != "23505" || pe.ConstraintName != "decision_record_input_state_key" {
			t.Errorf("%s: got %v, want a unique violation of decision_record_input_state_key", name, err)
		}
	}
	base := inputKey{"ten_a", "int_01", keyHash(1), "evs_01", "fin.pol@3.1"}
	if err := insert(base, ""); err != nil {
		t.Fatalf("the first decision, resting on no confirmation: %v", err)
	}
	conflict("a second decision resting on no confirmation", insert(base, ""))
	if err := insert(base, "cfr_01ACCEPTED"); err != nil {
		t.Fatalf("a decision that rests on a confirmation, next to the one that rests on none: %v", err)
	}
	conflict("a second decision resting on the same confirmation request", insert(base, "cfr_01ACCEPTED"))
	if err := insert(base, "cfr_02OTHER"); err != nil {
		t.Errorf("a decision resting on another request is another key: %v", err)
	}
	other := base
	other.tenant = "ten_b"
	if err := insert(other, "cfr_01ACCEPTED"); err != nil {
		t.Errorf("another tenant's decision with the same tuple and request is its own: %v", err)
	}
	if err := insert(other, ""); err != nil {
		t.Errorf("another tenant's decision resting on none is its own: %v", err)
	}
	for name, state := range map[string]string{"an empty state": "cfr_", "a wrong prefix": "cfe_01X", "a space": "cfr_01 X", "upper-case prefix": "CFR_01X", "one character": "cfr_1"} {
		var pe *pgconn.PgError
		err := insert(inputKey{"ten_a", "int_77", keyHash(9), "evs_77", "fin.pol@3.1"}, state)
		if !errors.As(err, &pe) || pe.Code != "23514" {
			t.Errorf("%s as a state: got %v, want a check violation", name, err)
		}
	}
}
