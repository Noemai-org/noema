package recorder

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// inputKey is the full input tuple of a decision (ruling A186): the tenant, the intent ID, the intent hash, the sealed context snapshot and the policy version.
// The same tuple is the same input and has one decision; a new snapshot or another policy version is another input. In the database the tenant and the intent
// hash are columns and the other three are members of the signed document: the unique index decision_record_input_state_key (migration 0034, which replaced 0027's state-blind
// decision_record_input_key) is over exactly these and the confirmation state.
type inputKey struct{ tenant, intentID, intentHash, snapshot, policy string }

// confirmation state (A366, migration 0034): the unique key decision_record_input_state_key is the tuple above and the state, the confirmation request the decision rests on ("" for none).

// lockName is the text the advisory lock is taken on. Every member matches a contract pattern that has no "|", so two different tuples never give one name.
func (k inputKey) lockName() string {
	return strings.Join([]string{k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy}, "|")
}

// lookup reads the decision stored for the tuple, in the caller's transaction and as the caller's tenant login (forced row security; the tenant is also in the
// predicate). It returns the stored record and its grants in plan-step order, or found == false when there is none. It never writes.
//
// A stored decision that the new evaluation contradicts is not returned: ErrDecisionConflict (INV-21). What is compared is what the writer would have recorded
// from this request: the outcome, the disposition, the exact list of rule versions, and for each grant its plan step, capability, target, params hash and plan hash.
func (w *Writer) lookup(ctx context.Context, tx pgx.Tx, k inputKey, state string, r Request, basis decide.Basis) (Result, bool, error) {
	versions, err := json.Marshal(policyVersions(basis))
	if err != nil {
		return Result{}, false, fmt.Errorf("%smarshal the rule versions: %w", errPrefix, err)
	}
	var raw string
	var outcome string
	var sameVersions bool
	err = tx.QueryRow(ctx, `SELECT json::text, outcome, policy_versions_json = $6::jsonb FROM decision_record
		WHERE tenant_id = $1 AND (json ->> 'intent_id') = $2 AND intent_hash = $3 AND (json ->> 'evidence_snapshot_id') = $4 AND (json ->> 'policy_version') = $5
		AND COALESCE(confirmation_request_id, '') = $7`,
		k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy, string(versions), state).Scan(&raw, &outcome, &sameVersions)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, w.otherState(ctx, tx, k, r.Decision.Outcome(), state)
	}
	if err != nil {
		return Result{}, false, fmt.Errorf("%sread the stored decision: %w", errPrefix, err)
	}
	var rec contracts.DecisionRecordV2
	if err := rec.UnmarshalJSON([]byte(raw)); err != nil {
		return Result{}, false, fmt.Errorf("%sthe stored decision is not a decision-record.v2 document: %w", errPrefix, err)
	}
	if outcome != string(r.Decision.Outcome()) || !sameVersions || dispositionOf(rec) != r.DispositionDecisionID {
		return Result{}, false, ErrDecisionConflict
	}
	rows, err := tx.Query(ctx, `SELECT json::text, sig_key_id, sig_value FROM decision_grant WHERE tenant_id = $1 AND decision_id = $2 ORDER BY (json ->> 'plan_step')::bigint, grant_id`, k.tenant, string(rec.DecisionId))
	if err != nil {
		return Result{}, false, fmt.Errorf("%sread the stored grants: %w", errPrefix, err)
	}
	defer rows.Close()
	res := Result{Record: rec}
	for rows.Next() {
		var graw, keyID, sigValue string
		if err := rows.Scan(&graw, &keyID, &sigValue); err != nil {
			return Result{}, false, fmt.Errorf("%sread a stored grant: %w", errPrefix, err)
		}
		signed, err := withSig(graw, contracts.CommonV1Sig{KeyId: keyID, Value: sigValue})
		if err != nil {
			return Result{}, false, fmt.Errorf("%sa stored grant is not a document: %w", errPrefix, err)
		}
		var g contracts.DecisionGrantV1
		if err := g.UnmarshalJSON(signed); err != nil {
			return Result{}, false, fmt.Errorf("%sa stored grant is not a decision-grant.v1 document: %w", errPrefix, err)
		}
		res.Grants = append(res.Grants, g)
	}
	if err := rows.Err(); err != nil {
		return Result{}, false, fmt.Errorf("%sread the stored grants: %w", errPrefix, err)
	}
	if !sameGrants(res.Grants, r) {
		return Result{}, false, ErrDecisionConflict
	}
	if r.Decision.Outcome() == decide.Allow {
		// A233: the stored ALLOW followed the confirmation it spent (or none). A retry that names another is not the same input: a typed conflict, nothing spent or written.
		var spent string
		err := tx.QueryRow(ctx, `SELECT confirmation_event_id FROM confirmation_spend WHERE tenant_id = $1 AND decision_id = $2`, k.tenant, string(rec.DecisionId)).Scan(&spent)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Result{}, false, fmt.Errorf("%sread the stored confirmation spend: %w", errPrefix, err)
		}
		if spent != r.ConfirmationEventID {
			return Result{}, false, ErrDecisionConflict
		}
	}
	return res, true, nil
}

// withSig puts the signature the columns hold back into the stored signed part, so the grant that a retry returns is the one that was issued.
func withSig(stored string, sig contracts.CommonV1Sig) ([]byte, error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(stored), &doc); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(sig)
	if err != nil {
		return nil, err
	}
	doc["sig"] = raw
	return json.Marshal(doc)
}

// dispositionOf is the disposition decision a record names, or "".
func dispositionOf(rec contracts.DecisionRecordV2) string {
	if rec.DispositionDecisionId.Set && !rec.DispositionDecisionId.Null {
		return string(rec.DispositionDecisionId.Value)
	}
	return ""
}

// sameGrants reports whether the stored grants are the ones this request asks for: the same number, and for each plan step the same capability, target, params
// hash and plan hash. Nothing else of a grant (its nonce, window, signature) is compared: those are the stored grant's own.
func sameGrants(stored []contracts.DecisionGrantV1, r Request) bool {
	if len(stored) != len(r.Steps) {
		return false
	}
	want := make(map[int64]int, len(r.Steps))
	for i, s := range r.Steps {
		if _, dup := want[s.PlanStep]; dup {
			return false
		}
		want[s.PlanStep] = i
	}
	for _, g := range stored {
		i, ok := want[g.PlanStep]
		if !ok {
			return false
		}
		s := r.Steps[i]
		if string(g.CapabilityId) != s.CapabilityID || string(g.TargetRef) != s.TargetRef || g.ParamsHash != s.ParamsHash || g.PlanHash != s.PlanHash {
			return false
		}
		delete(want, g.PlanStep)
	}
	return len(want) == 0
}

// errUnresolved is resolveState's answer for an ALLOW that names a confirmation that is not recorded for the tenant. It is internal: the writer turns it into ErrConfirmationNotValid,
// or into ErrDecisionConflict when the input already has an ALLOW.
var errUnresolved = errors.New(errPrefix + "the confirmation is not recorded")

// noState is a state that no stored decision has (a stored state is NULL or a cfr_ ID): with it otherState reports every decision of the outcome on the tuple.
const noState = "?"

// resolveState is the confirmation state of the decision a request asks for (A366): the confirmation request it rests on, or "" for none. An ALLOW that follows a confirmation rests on the
// request that confirmation resolved, read from confirmation_resolution as the caller's tenant login (row security hides another tenant's; the tenant is also in the predicate); a
// confirmation that is not recorded for the tenant is errUnresolved. A REJECT DECLINED_BY_APPROVER rests on the declined request it names. Every other decision rests on none.
func resolveState(ctx context.Context, tx pgx.Tx, r Request) (string, error) {
	switch {
	case r.Decision.Outcome() == decide.Allow && r.ConfirmationEventID != "":
		var request string
		err := tx.QueryRow(ctx, `SELECT confirmation_request_id FROM confirmation_resolution WHERE tenant_id = $1 AND confirmation_event_id = $2`, r.TenantID, r.ConfirmationEventID).Scan(&request)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", errUnresolved
		}
		if err != nil {
			return "", fmt.Errorf("%sread the confirmation the decision follows: %w", errPrefix, err)
		}
		return request, nil
	case r.Decision.Outcome() == decide.Reject:
		return r.DeclinedRequestID, nil
	}
	return "", nil
}

// otherState is the answer when no decision of the tuple has this state: nil, unless another decision of the tuple, of the same outcome, rests on another state, which is
// ErrDecisionConflict (A366, INV-21): a tuple has one decision of an outcome, so two accepted confirmations of one binding, or a confirmation and none, cannot give two grants on one
// input, and a CONFIRM and the ALLOW that follows its ACCEPT (two outcomes) are the two decisions the state makes possible. The caller holds the tuple's advisory lock (the lock name has no
// state in it), so no writer of the tuple can add one between this read and its insert.
func (w *Writer) otherState(ctx context.Context, tx pgx.Tx, k inputKey, outcome decide.Outcome, state string) error {
	var n int
	err := tx.QueryRow(ctx, `SELECT count(*) FROM decision_record
		WHERE tenant_id = $1 AND (json ->> 'intent_id') = $2 AND intent_hash = $3 AND (json ->> 'evidence_snapshot_id') = $4 AND (json ->> 'policy_version') = $5
		AND outcome = $6 AND COALESCE(confirmation_request_id, '') <> $7`, k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy, string(outcome), state).Scan(&n)
	if err != nil {
		return fmt.Errorf("%sread the decisions of the input: %w", errPrefix, err)
	}
	if n > 0 {
		return ErrDecisionConflict
	}
	return nil
}
