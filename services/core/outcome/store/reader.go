package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/outcome/request"
)

// Reader is the read-only Record reader of LANE-5 slice 4 (A675, A692; interim, relayed; REQ-VER-001, REQ-SEC-004; INV-04, INV-12, AGENT-13). It reads, as the tenant's outcome login, what
// Execution wrote for one grant: the receipt with its signed document and step_params, the grant's capability, plan hash and intent hash, and the decision's intent. It writes nothing.
// The tenant is in the WHERE and row security is forced, so a row of another tenant is not found by the argument or by the login.
type Reader struct {
	Pool *pgxpool.Pool // the tenant's outcome_rw login
}

var _ request.Reader = Reader{}

// If a grant has more than one receipt (one for each attempt), the latest attempt is read: the grant was spent once, and a retry is under a new grant (AGENT-08).
const readFacts = `SELECT r.receipt_id, r.tenant_id, r.grant_id, r.attempt, r.idempotency_key, r.logical_action, r.execution_state,
		g.json->>'correlation_id', g.json->>'principal_id', r.dispatched_at, r.raw_status, r.external_ack, r.json, r.sig_key_id, r.sig_value,
		g.json->>'capability_id', g.json->>'intent_hash', g.json->>'plan_hash', d.intent_hash, d.json->>'intent_id', r.json->'step_params'
	FROM execution_receipt r
	JOIN decision_grant g ON g.grant_id = r.grant_id AND g.tenant_id = r.tenant_id
	JOIN decision_record d ON d.decision_id = g.decision_id AND d.tenant_id = g.tenant_id
	WHERE r.tenant_id = $1 AND r.grant_id = $2
	ORDER BY r.attempt DESC LIMIT 1`

// Load returns the facts of the tenant's grant, or request.ErrNotFound.
func (r Reader) Load(ctx context.Context, tenantID, grantID string) (request.Facts, error) {
	if err := needDeadline(ctx); err != nil {
		return request.Facts{}, err
	}
	if r.Pool == nil {
		return request.Facts{}, errors.New(errPrefix + "the reader has no pool")
	}
	var f request.Facts
	rc := &f.Receipt
	var corr, principal, capability, grantHash, plan, intent *string
	var params []byte
	err := r.Pool.QueryRow(ctx, readFacts, tenantID, grantID).Scan(&rc.ReceiptID, &rc.TenantID, &rc.GrantID, &rc.Attempt, &rc.IdempotencyKey, &rc.LogicalAction, &rc.State,
		&corr, &principal, &rc.DispatchedAt, &rc.RawStatus, &rc.ExternalAck, &rc.Document, &rc.SigKeyID, &rc.SigValue,
		&capability, &grantHash, &plan, &f.DecisionIntentHash, &intent, &params)
	if errors.Is(err, pgx.ErrNoRows) {
		return request.Facts{}, request.ErrNotFound
	}
	if err != nil {
		return request.Facts{}, fmt.Errorf("%sread the facts of a grant: %w", errPrefix, err)
	}
	for _, v := range []struct {
		src *string
		dst *string
	}{{corr, &rc.CorrelationID}, {principal, &rc.PrincipalID}, {capability, &f.CapabilityID}, {grantHash, &f.GrantIntentHash}, {plan, &f.PlanHash}, {intent, &f.IntentID}} {
		if v.src == nil || *v.src == "" {
			return request.Facts{}, fmt.Errorf("%sthe grant or decision of a receipt lacks a member the request needs", errPrefix)
		}
		*v.dst = *v.src
	}
	rc.DispatchedAt = rc.DispatchedAt.UTC()
	if f.StepParams, err = stepParams(params); err != nil {
		return request.Facts{}, err
	}
	return f, nil
}

// stepParams reads the receipt's step_params as canonical text: a string as it is, a number as its digits, a boolean as true or false, null as empty (the builder refuses an empty value for a
// bound variable). An object or an array is not what the contract allows and is an error that names no value.
func stepParams(raw []byte) (map[string]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		return nil, fmt.Errorf("%sthe receipt's step_params are not an object", errPrefix)
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		switch x := v.(type) {
		case string:
			out[k] = x
		case json.Number:
			out[k] = x.String()
		case bool:
			out[k] = strconv.FormatBool(x)
		case nil:
			out[k] = ""
		default:
			return nil, fmt.Errorf("%sa step_param of the receipt is not a scalar", errPrefix)
		}
	}
	return out, nil
}
