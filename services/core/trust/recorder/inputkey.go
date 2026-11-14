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
// hash are columns and the other three are members of the signed document: the unique index decision_record_input_key (migration 0027) is over exactly these.
type inputKey struct{ tenant, intentID, intentHash, snapshot, policy string }

// lockName is the text the advisory lock is taken on. Every member matches a contract pattern that has no "|", so two different tuples never give one name.
func (k inputKey) lockName() string {
	return strings.Join([]string{k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy}, "|")
}

// lookup reads the decision stored for the tuple, in the caller's transaction and as the caller's tenant login (forced row security; the tenant is also in the
// predicate). It returns the stored record and its grants in plan-step order, or found == false when there is none. It never writes.
//
// A stored decision that the new evaluation contradicts is not returned: ErrDecisionConflict (INV-21). What is compared is what the writer would have recorded
// from this request: the outcome, the disposition, the exact list of rule versions, and for each grant its plan step, capability, target, params hash and plan hash.
func (w *Writer) lookup(ctx context.Context, tx pgx.Tx, k inputKey, r Request, basis decide.Basis) (Result, bool, error) {
	versions, err := json.Marshal(policyVersions(basis))
	if err != nil {
		return Result{}, false, fmt.Errorf("%smarshal the rule versions: %w", errPrefix, err)
	}
	var raw string
	var outcome string
	var sameVersions bool
	err = tx.QueryRow(ctx, `SELECT json::text, outcome, policy_versions_json = $6::jsonb FROM decision_record
		WHERE tenant_id = $1 AND (json ->> 'intent_id') = $2 AND intent_hash = $3 AND (json ->> 'evidence_snapshot_id') = $4 AND (json ->> 'policy_version') = $5`,
		k.tenant, k.intentID, k.intentHash, k.snapshot, k.policy, string(versions)).Scan(&raw, &outcome, &sameVersions)
	if errors.Is(err, pgx.ErrNoRows) {
		return Result{}, false, nil
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
	rows, err := tx.Query(ctx, `SELECT json::text FROM decision_grant WHERE tenant_id = $1 AND decision_id = $2 ORDER BY (json ->> 'plan_step')::bigint, grant_id`, k.tenant, string(rec.DecisionId))
	if err != nil {
		return Result{}, false, fmt.Errorf("%sread the stored grants: %w", errPrefix, err)
	}
	defer rows.Close()
	res := Result{Record: rec}
	for rows.Next() {
		var graw string
		if err := rows.Scan(&graw); err != nil {
			return Result{}, false, fmt.Errorf("%sread a stored grant: %w", errPrefix, err)
		}
		var g contracts.DecisionGrantV1
		if err := g.UnmarshalJSON([]byte(graw)); err != nil {
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
	return res, true, nil
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
