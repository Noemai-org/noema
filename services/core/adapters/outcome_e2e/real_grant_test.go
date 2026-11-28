package outcomee2e_test

// TRU-05 follow-up (REQ-TRU-008, REQ-EXE-001; INV-03, INV-17): a grant that the REAL Decision Record writer issues and stores must be loadable and executable by the REAL
// ledger and controller. Until now every test of the controller stored a hand-made grant (the signed canonical form, without `sig`), so no test composed trust/recorder with
// execution/ledger; the writer stored the grant with its `sig` member and ledger.Load, which decodes the column strictly into controller.Grant, refused it
// (`json: unknown field "sig"`). The signature is kept in sig_key_id and sig_value; controller.Grant documents its canonical JSON, without the signature, as the signed part.
// Real PostgreSQL, real tenant logins, no stand-in for the writer, the loader or the controller. All data is SYNTHETIC.

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/bridge"
	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const realGrantKeyID = "k_realgrant"

// realGrantWorld is the stack with Trust's real writer in place of the stand-in's grant insert. The controller verifies under the key the real issuer signs with.
type realGrantWorld struct {
	*stack
	w     *recorder.Writer
	req   recorder.Request
	disp  recorder.Request // the ESCALATE disposition the action decision names (A503)
	plan  controller.PlanStep
	steps []grants.Step
}

func newRealGrantWorld(t *testing.T) *realGrantWorld {
	t.Helper()
	s := newStack(t, startMockCase(t))
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 3)
	}
	keys, err := grants.NewKeys([]grants.Key{{ID: realGrantKeyID, Secret: secret}})
	if err != nil {
		t.Fatal(err)
	}
	gen := ids.New(s.clk, nil)
	issuer, err := grants.NewIssuer(keys, gen, rand.New(rand.NewSource(5)), time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	s.ctl.Keys = crypto.NewKeyRing(map[string][]byte{realGrantKeyID: secret})
	w := &recorder.Writer{Keys: keys, Issuer: issuer, Appender: &chain.Appender{Clock: s.clk, IDs: gen, Signers: recordsig.Local("outcome-e2e", nil, "core-trust")}, Clock: s.clk, IDs: gen,
		RecordLifetime: time.Hour, Escalation: recorder.NewEscalationSet(bridge.CapCreateEscalationCase)}

	now := s.clk.Now().Unix()
	bb, err := decide.NewBasisBuilder("fintech@1.0", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := bb.RecordEvidence("evs_01ABC", []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: now - 30, Source: "fin.src.ledger", AgeSeconds: 30, EffectiveLimitSeconds: 300,
		LimitSetBy: decide.LimitByTrustProfile}}); err != nil {
		t.Fatal(err)
	}
	if err := bb.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "r_permit", PolicyID: policyScope, Version: "1.0"}}); err != nil {
		t.Fatal(err)
	}
	resolved, ok := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	if !ok {
		t.Fatal("the intent is not resolved")
	}
	d, err := resolved.Allow(decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass})
	if err != nil {
		t.Fatal(err)
	}
	if d, err = bb.Attach(d); err != nil {
		t.Fatal(err)
	}
	params := map[string]any{"transaction_ref": txRef, "reason_code": reasonCode, "created_by_intent": intentRef, "epoch": int64(1)}
	paramsHash, err := crypto.HashOf(params)
	if err != nil {
		t.Fatal(err)
	}
	const target = "fin.transaction:TX1"
	perm, err := reconcile.NewFreezeGate(nil).PermitTarget(false, bridge.CapCreateEscalationCase, target)
	if err != nil {
		t.Fatal(err)
	}
	step := grants.Step{PrincipalID: principal, CapabilityID: bridge.CapCreateEscalationCase, TargetRef: target, ParamsHash: contracts.CommonV1Hash(paramsHash),
		EvidenceSetHash: contracts.CommonV1Hash(hash(2)), PlanHash: contracts.CommonV1Hash(hash(3)), PlanStep: 1, Permission: perm}
	// the ESCALATE disposition of the action decision: its own Trust decision with no step and no grant, recorded first through the real writer (A503, steps if and only if a
	// disposition); the action decision names it
	reason, err := decide.Core("NOT_AUTHORIZED")
	if err != nil {
		t.Fatal(err)
	}
	unresolved, ok := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
	if !ok {
		t.Fatal("the intent is not unresolved")
	}
	escalate, err := unresolved.Escalate(reason)
	if err != nil {
		t.Fatal(err)
	}
	// its own sealed snapshot: the disposition and the action decision are two inputs of the same intent (A186, A542)
	dsp, err := decide.NewBasisBuilder("fintech@1.0", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := dsp.RecordEvidence("evs_01DSP", []decide.Freshness{{EvidenceID: "ev_1", EffectiveAt: now - 30, Source: "fin.src.ledger", AgeSeconds: 30, EffectiveLimitSeconds: 300,
		LimitSetBy: decide.LimitByTrustProfile}}); err != nil {
		t.Fatal(err)
	}
	if err := dsp.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "r_permit", PolicyID: policyScope, Version: "1.0"}}); err != nil {
		t.Fatal(err)
	}
	if escalate, err = dsp.Attach(escalate); err != nil {
		t.Fatal(err)
	}
	rw := &realGrantWorld{stack: s, w: w, steps: []grants.Step{step}}
	rw.disp = recorder.Request{TenantID: tenant, CorrelationID: correlation, IntentID: "int_realgrant", IntentHash: contracts.CommonV1Hash(hash(1)), PolicyID: policyScope, Decision: escalate, Evaluated: dsp}
	rw.req = recorder.Request{TenantID: tenant, CorrelationID: correlation, IntentID: "int_realgrant", IntentHash: contracts.CommonV1Hash(hash(1)), PolicyID: policyScope, Decision: d,
		Evaluated: bb, Steps: rw.steps}
	rw.plan = controller.PlanStep{PlanHash: hash(3), Step: 1, CapabilityID: bridge.CapCreateEscalationCase, Registered: true, AdapterExists: true, Params: params,
		KeyFields: controller.KeyFields{TransactionID: txRef, LogicalAction: "ESCALATE_CASE", ReasonCode: reasonCode, PolicyScope: policyScope, LogicalResolutionEpoch: "1"}, TimeoutMS: dispatchMS}
	return rw
}

// write runs the real writer as Trust's own login and commits; it registers the stored plan the grant binds (E5), which is another task's store.
func (rw *realGrantWorld) write(t *testing.T) recorder.Result {
	t.Helper()
	dtx, err := rw.pools["trust_rw"].Begin(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	disp, err := rw.w.Write(bg(t), dtx, rw.disp)
	if err != nil {
		_ = dtx.Rollback(bg(t))
		t.Fatalf("the disposition: %v", err)
	}
	if err := dtx.Commit(bg(t)); err != nil {
		t.Fatal(err)
	}
	rw.req.DispositionDecisionID = string(disp.Record.DecisionId)
	tx, err := rw.pools["trust_rw"].Begin(bg(t))
	if err != nil {
		t.Fatal(err)
	}
	res, err := rw.w.Write(bg(t), tx, rw.req)
	if err != nil {
		_ = tx.Rollback(bg(t))
		t.Fatalf("the writer: %v", err)
	}
	if err := tx.Commit(bg(t)); err != nil {
		t.Fatal(err)
	}
	if len(res.Grants) != 1 {
		t.Fatalf("%d grants issued, want 1", len(res.Grants))
	}
	rw.trust.mu.Lock()
	rw.trust.plans[string(res.Grants[0].GrantId)] = rw.plan
	rw.trust.mu.Unlock()
	return res
}

// REQ-TRU-008, REQ-EXE-001 (E1): the grant the real writer stored is loaded by the real ledger, verifies under its signature (rebuilt by the controller from the stored
// document and the stored key ID and value), and executes to a receipt.
func TestAGrantTheRealWriterIssuedIsLoadedVerifiedAndExecutedByExecution(t *testing.T) {
	rw := newRealGrantWorld(t)
	res := rw.write(t)
	id := string(res.Grants[0].GrantId)

	if sg, err := rw.trust.led.Load(bg(t), tenant, id); err != nil {
		t.Fatalf("the real ledger cannot load the grant the real writer stored: %v", err)
	} else if sg.Sig.KeyID != realGrantKeyID || sg.Sig.Value != res.Grants[0].Sig.Value || sg.Grant.GrantID != id {
		t.Fatalf("the loaded grant %+v and signature %+v are not the issued ones", sg.Grant, sg.Sig)
	}
	out, err := rw.ctl.Execute(bg(t), tenant, id)
	if err != nil || out.Void != "" || out.Receipt == nil {
		t.Fatalf("Execute: %+v, %v; want a receipt (E1 verifies the signature over the stored document)", out, err)
	}
	// the signature lives in its two columns and is not repeated inside the signed document
	var doc map[string]json.RawMessage
	var raw []byte
	if err := rw.admin.QueryRow(bg(t), `SELECT json FROM decision_grant WHERE grant_id = $1`, id).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if _, has := doc["sig"]; has {
		t.Fatal("the stored grant document still carries its sig member")
	}
}

// REQ-TRU-008, INV-21: a retry of the same input returns the stored decision and the stored grant, still signed: the signature is read from its columns, so the grant that comes
// back is the one that was issued and verifies.
func TestARetryOfTheSameInputReturnsTheStoredGrantStillSigned(t *testing.T) {
	rw := newRealGrantWorld(t)
	first := rw.write(t)
	second := rw.write(t)
	if len(second.Grants) != 1 || second.Grants[0].GrantId != first.Grants[0].GrantId || second.Grants[0].Sig != first.Grants[0].Sig || second.Grants[0].Sig.Value == "" {
		t.Fatalf("the retry returned %+v, want the stored grant %+v with its signature", second.Grants, first.Grants)
	}
	if n := rw.count(t, `SELECT count(*) FROM decision_grant`); n != 1 {
		t.Fatalf("%d grants stored after a retry, want 1", n)
	}
}
