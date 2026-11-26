package db_test

// A581 and A590 (interim, relayed; migration 0040; REQ-TRU-001, REQ-TRU-007; INV-03, INV-17, INV-21): an early decision, one whose basis is null, may name the turn it answers and the
// verified principal. The two columns come as a pair, only on an outcome that is not ALLOW or CONFIRM and only with a null evidence snapshot, and a turn is a lower-case UUID. The unique key
// of the input tuple (decision_record_input_state_key) includes them, so a new turn is a new input, and a row that names neither is keyed exactly as before. All rows are SYNTHETIC.

import (
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
)

const earlyShape = "decision_record_early_key_shape"

const (
	dbTurnA = "0b8f3c1e-5d2a-4e7b-9c41-6a1d2f3e4b50"
	dbTurnB = "7c2d9a40-1e3b-4f6a-8d57-2b9c0e1f3a64"
)

// insertKeyed inserts a decision that names a turn and a principal (nil for none).
func insertKeyed(t *testing.T, pool *pgxpool.Pool, id, outcome, intentID string, basis map[string]string, turn, principal any) error {
	t.Helper()
	_, err := pool.Exec(ctx(t), `INSERT INTO decision_record (decision_id, tenant_id, json, outcome, intent_hash, policy_versions_json, sig_key_id, sig_value, turn_id, principal_id)
		VALUES ($1, 'ten_a', $2::jsonb, $3, $4, '[]', 'k1', 'aa', $5, $6)`, id, basisDoc(id, "ten_a", outcome, intentID, basis), outcome, testpg.FixtureIntentHash, turn, principal)
	return err
}

// A null-basis decision of a turn and a principal is stored.
func TestAnEarlyDecisionMayNameATurnAndAPrincipal(t *testing.T) {
	pool := basisWorld(t)
	for i, outcome := range earlyOutcomes {
		if err := insertKeyed(t, pool, "dec_t"+string(rune('a'+i)), outcome, "int_t"+string(rune('a'+i)), allKeys("null"), dbTurnA, "usr_88213"); err != nil {
			t.Errorf("%s: %v; want the row stored", outcome, err)
		}
	}
}

// The turn and the principal come together, only with a null evidence snapshot, never on an ALLOW or a CONFIRM, and in their contract forms.
func TestTheTurnAndThePrincipalHaveOneShape(t *testing.T) {
	pool := basisWorld(t)
	cases := []struct {
		name      string
		outcome   string
		basis     map[string]string
		turn, pri any
	}{
		{"a turn with no principal", "ESCALATE", allKeys("null"), dbTurnA, nil},
		{"a principal with no turn", "ESCALATE", allKeys("null"), nil, "usr_88213"},
		{"a turn on an ALLOW", "ALLOW", realBasis, dbTurnA, "usr_88213"},
		{"a turn on a CONFIRM", "CONFIRM", realBasis, dbTurnA, "usr_88213"},
		{"a turn on a decision with a snapshot", "ESCALATE", realBasis, dbTurnA, "usr_88213"},
		{"an upper-case turn", "ESCALATE", allKeys("null"), "0B8F3C1E-5D2A-4E7B-9C41-6A1D2F3E4B50", "usr_88213"},
		{"a turn with a trailing newline", "ESCALATE", allKeys("null"), dbTurnA + "\n", "usr_88213"},
		{"a malformed principal", "ESCALATE", allKeys("null"), dbTurnA, "88213"},
		{"an empty turn", "ESCALATE", allKeys("null"), "", "usr_88213"},
	}
	for i, c := range cases {
		err := insertKeyed(t, pool, "dec_s"+string(rune('a'+i)), c.outcome, "int_s"+string(rune('a'+i)), c.basis, c.turn, c.pri)
		if code, name := constraintOf(err); code != "23514" || name != earlyShape {
			t.Errorf("%s: got %v, want a check violation of %s", c.name, err, earlyShape)
		}
	}
}

// A new turn is another input; the same turn and principal for the same intent is one decision; a different principal is another input (A581, A590, INV-21).
func TestTheTurnAndThePrincipalAreInTheInputKey(t *testing.T) {
	pool := basisWorld(t)
	if err := insertKeyed(t, pool, "dec_k1", "ESCALATE", "int_k1", allKeys("null"), dbTurnA, "usr_88213"); err != nil {
		t.Fatal(err)
	}
	err := insertKeyed(t, pool, "dec_k2", "ESCALATE", "int_k1", allKeys("null"), dbTurnA, "usr_88213")
	if code, c := constraintOf(err); code != "23505" || c != "decision_record_input_state_key" {
		t.Errorf("the same turn and principal twice: got %v, want a unique violation of decision_record_input_state_key", err)
	}
	if err := insertKeyed(t, pool, "dec_k3", "ESCALATE", "int_k1", allKeys("null"), dbTurnB, "usr_88213"); err != nil {
		t.Errorf("a new turn is another input: %v", err)
	}
	if err := insertKeyed(t, pool, "dec_k4", "ESCALATE", "int_k1", allKeys("null"), dbTurnA, "usr_88214"); err != nil {
		t.Errorf("another principal is another input: %v", err)
	}
}

// A decision that names no turn is keyed exactly as before (A186, 0038): one per tuple, and not shadowed by a keyed one.
func TestADecisionWithNoTurnIsKeyedAsBefore(t *testing.T) {
	pool := basisWorld(t)
	if err := insertKeyed(t, pool, "dec_n1", "ESCALATE", "int_n1", allKeys("null"), nil, nil); err != nil {
		t.Fatal(err)
	}
	err := insertKeyed(t, pool, "dec_n2", "ESCALATE", "int_n1", allKeys("null"), nil, nil)
	if code, c := constraintOf(err); code != "23505" || c != "decision_record_input_state_key" {
		t.Errorf("an unkeyed null tuple twice: got %v, want a unique violation of decision_record_input_state_key", err)
	}
	if err := insertKeyed(t, pool, "dec_n3", "ESCALATE", "int_n1", allKeys("null"), dbTurnA, "usr_88213"); err != nil {
		t.Errorf("a keyed decision beside an unkeyed one is another input: %v", err)
	}
	if err := insertKeyed(t, pool, "dec_n4", "ALLOW", "int_n1", realBasis, nil, nil); err != nil {
		t.Errorf("an ALLOW with a real basis: %v", err)
	}
}
