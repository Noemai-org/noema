package db_test

// A574 and A577 (interim; migration 0038; decision-record.v2; REQ-TRU-001, REQ-TRU-007, REQ-TRU-008; INV-03, INV-17, INV-21, INV-25): evidence_snapshot_id, policy_version and
// evidence_freshness are required KEYS of every stored Decision Record. Their value is null for an outcome reached before the evidence and the policy were evaluated (REJECT, CLARIFY, WAIT,
// ESCALATE, UNKNOWN) and non-null for ALLOW and CONFIRM, so neither can carry a missing basis and no grant can rest on one. No outcome may omit a key. A null basis is still one decision per
// input tuple (A186), and the immutability triggers of the table are untouched. All rows are SYNTHETIC.

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

const basisConstraint = "decision_record_basis_by_outcome"

var basisKeys = []string{"evidence_snapshot_id", "policy_version", "evidence_freshness"}
var earlyOutcomes = []string{"REJECT", "CLARIFY", "WAIT", "ESCALATE", "UNKNOWN"}
var actionOutcomes = []string{"ALLOW", "CONFIRM"}

// realBasis is the JSON value of each basis key for a decision that evaluated them.
var realBasis = map[string]string{"evidence_snapshot_id": `"evs_synth01"`, "policy_version": `"fin.pol@3.2"`, "evidence_freshness": `[]`}

// basisDoc is a bound-complete Decision Record document as JSON text. basis maps each basis key to its JSON value; a key that is not in the map is omitted from the document.
func basisDoc(id, tenant, outcome, intentID string, basis map[string]string) string {
	doc := fmt.Sprintf(`{"decision_id":%q,"tenant_id":%q,"outcome":%q,"sig":{"key_id":"k1","value":"aa"},"intent_id":%q,"correlation_id":"corr_synth01","intent_hash":%q,`+
		`"issued_at":%q,"expires_at":%q,"authority":{"technical":"PASS","policy":"PASS","business":"PASS"}`,
		id, tenant, outcome, intentID, testpg.FixtureIntentHash, testpg.FixtureIssuedAt, testpg.FixtureRecordExpires)
	for _, k := range basisKeys {
		if v, ok := basis[k]; ok {
			doc += fmt.Sprintf(`,%q:%s`, k, v)
		}
	}
	return doc + `}`
}

// basisWith is realBasis with the named keys replaced by the given JSON value, or removed when the value is empty.
func basisWith(over map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range realBasis {
		out[k] = v
	}
	for k, v := range over {
		if v == "" {
			delete(out, k)
		} else {
			out[k] = v
		}
	}
	return out
}

func allKeys(value string) map[string]string {
	over := map[string]string{}
	for _, k := range basisKeys {
		over[k] = value
	}
	return basisWith(over)
}

func basisWorld(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := migrated(t)
	if _, err := pool.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_a', 'A')`); err != nil {
		t.Fatal(err)
	}
	return pool
}

func insertBasis(t *testing.T, pool *pgxpool.Pool, id, outcome, intentID string, basis map[string]string) error {
	t.Helper()
	_, err := pool.Exec(ctx(t), `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value)
		VALUES ($1, 'ten_a', $2::jsonb, $3, $4, '[]', 'k1', 'aa')`, id, basisDoc(id, "ten_a", outcome, intentID, basis), outcome, testpg.FixtureIntentHash)
	return err
}

func constraintOf(err error) (code, name string) {
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return pe.Code, pe.ConstraintName
	}
	return "", ""
}

// An outcome reached before the evaluation stores a null for each basis member, all three or any of them.
func TestAnEarlyOutcomeMayStoreANullBasis(t *testing.T) {
	pool := basisWorld(t)
	n := 0
	for _, outcome := range earlyOutcomes {
		cases := map[string]map[string]string{"all three null": allKeys("null")}
		for _, k := range basisKeys {
			cases["only "+k+" null"] = basisWith(map[string]string{k: "null"})
		}
		for name, basis := range cases {
			n++
			if err := insertBasis(t, pool, fmt.Sprintf("dec_e%d", n), outcome, fmt.Sprintf("int_e%d", n), basis); err != nil {
				t.Errorf("%s with %s: %v; want the row stored", outcome, name, err)
			}
		}
	}
}

// ALLOW and CONFIRM never store a null in a basis member: a grant, or a request to confirm, cannot rest on a missing basis.
func TestAnAllowAndAConfirmRefuseANullBasis(t *testing.T) {
	pool := basisWorld(t)
	n := 0
	for _, outcome := range actionOutcomes {
		cases := map[string]map[string]string{"all three null": allKeys("null")}
		for _, k := range basisKeys {
			cases["only "+k+" null"] = basisWith(map[string]string{k: "null"})
		}
		for name, basis := range cases {
			n++
			err := insertBasis(t, pool, fmt.Sprintf("dec_a%d", n), outcome, fmt.Sprintf("int_a%d", n), basis)
			if code, c := constraintOf(err); code != "23514" || c != basisConstraint {
				t.Errorf("%s with %s: got %v, want a check violation of %s", outcome, name, err, basisConstraint)
			}
		}
	}
	if err := insertBasis(t, pool, "dec_a_ok", "ALLOW", "int_a_ok", realBasis); err != nil {
		t.Errorf("an ALLOW with a real basis: %v", err)
	}
	if err := insertBasis(t, pool, "dec_c_ok", "CONFIRM", "int_c_ok", realBasis); err != nil {
		t.Errorf("a CONFIRM with a real basis: %v", err)
	}
}

// No outcome may omit a basis key: the key is required and its value is null, never missing (A577).
func TestNoOutcomeMayOmitABasisKey(t *testing.T) {
	pool := basisWorld(t)
	n := 0
	for _, outcome := range append(append([]string{}, earlyOutcomes...), actionOutcomes...) {
		for _, k := range basisKeys {
			n++
			err := insertBasis(t, pool, fmt.Sprintf("dec_o%d", n), outcome, fmt.Sprintf("int_o%d", n), basisWith(map[string]string{k: ""}))
			if code, c := constraintOf(err); code != "23514" || c != basisConstraint {
				t.Errorf("%s without %s: got %v, want a check violation of %s", outcome, k, err, basisConstraint)
			}
		}
	}
}

// A null basis is still one decision per input tuple (A186, migration 0034): two NULLs are never equal in a plain unique index, so the key must not leave a null tuple uncovered.
func TestANullBasisIsStillOneDecisionPerInputTuple(t *testing.T) {
	pool := basisWorld(t)
	if err := insertBasis(t, pool, "dec_k1", "ESCALATE", "int_k1", allKeys("null")); err != nil {
		t.Fatalf("the first decision of a null tuple: %v", err)
	}
	err := insertBasis(t, pool, "dec_k2", "ESCALATE", "int_k1", allKeys("null"))
	if code, c := constraintOf(err); code != "23505" || c != "decision_record_input_state_key" {
		t.Errorf("a second decision for the same null tuple: got %v, want a unique violation of decision_record_input_state_key", err)
	}
	if err := insertBasis(t, pool, "dec_k3", "ESCALATE", "int_k2", allKeys("null")); err != nil {
		t.Errorf("another intent with a null basis is another input: %v", err)
	}
	if err := insertBasis(t, pool, "dec_k4", "ALLOW", "int_k1", realBasis); err != nil {
		t.Errorf("the same intent with a real basis is another input: %v", err)
	}
}

// The immutability triggers of decision_record are untouched: a stored decision with a null basis is never edited or deleted.
func TestAStoredNullBasisDecisionIsStillInsertOnly(t *testing.T) {
	pool := basisWorld(t)
	if err := insertBasis(t, pool, "dec_i1", "UNKNOWN", "int_i1", allKeys("null")); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx(t), `UPDATE decision_record SET json = json || '{"outcome":"UNKNOWN"}'::jsonb WHERE decision_id = 'dec_i1'`); err == nil {
		t.Error("an UPDATE of a stored decision is accepted")
	}
	if _, err := pool.Exec(ctx(t), `DELETE FROM decision_record WHERE decision_id = 'dec_i1'`); err == nil {
		t.Error("a DELETE of a stored decision is accepted")
	}
	if _, err := pool.Exec(ctx(t), `TRUNCATE decision_record`); err == nil {
		t.Error("a TRUNCATE of decision_record is accepted")
	}
}
