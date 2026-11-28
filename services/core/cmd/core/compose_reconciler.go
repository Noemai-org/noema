package main

import (
	evidencereconciler "github.com/Noemai-org/noema/services/core/evidence/reconciler"
	reconcileradapter "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/reconciler"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

// limitsPack is the pack's routing of a classification, read from the trust limits file the manifest pinned (A544): the escalation reason of each classification the file maps and the
// capabilities allowed on a frozen target. It holds no reason code and no capability of its own (INV-14); an unmapped classification has no reason.
type limitsPack struct {
	reasons map[reconcile.Classification]string
	allowed []string
}

func (p limitsPack) EscalateReason(c reconcile.Classification) string { return p.reasons[c] }
func (p limitsPack) AllowedUnderFreeze() []string                     { return append([]string(nil), p.allowed...) }

// buildReconcilerInput builds T10 over the tenants' sealed snapshots, the Reconciler's own rule set and mapping (no second mapping), the pack's escalation reasons and freeze allowance,
// and the required read bound. Without any of them, or with a limits file that maps none, it is a gap that fails closed: nothing is routed that the pack did not say.
func buildReconcilerInput(in trustInputsIn) (intake.Reconciler, error) {
	if len(in.Verified.Pack) == 0 || len(in.Verified.Limits) == 0 || in.RuleSet == nil || in.Mapping == nil || in.ReconcileTimeout <= 0 || !in.screened() {
		return nil, nil
	}
	_, doc, err := trustLimitsOf(in.Verified)
	if err != nil {
		return nil, err
	}
	if doc.EscalationReasons == nil || doc.AllowedUnderFreeze == nil {
		return nil, nil
	}
	pack := limitsPack{reasons: map[reconcile.Classification]string{}, allowed: *doc.AllowedUnderFreeze}
	for _, e := range *doc.EscalationReasons {
		pack.reasons[reconcile.Classification(e.Classification)] = e.ReasonCode
	}
	store, err := reconcileradapter.NewStore(reconcileradapter.PoolsOf(in.Pools))
	if err != nil {
		return nil, inputsFail("the sealed snapshot store cannot be built", err)
	}
	payloads, err := buildScreenedPayloads(in.Payloads, in.Screen)
	if err != nil {
		return nil, inputsFail("the payload reader cannot be built", err)
	}
	a, err := reconcileradapter.New(reconcileradapter.Config{
		Snapshots: store, Classifiers: reconcileradapter.ReconcilerAt(evidencereconciler.Config{RuleSet: in.RuleSet, Mapping: in.Mapping, Payloads: payloads}), Pack: pack, Timeout: in.ReconcileTimeout,
	})
	if err != nil {
		return nil, inputsFail("the Reconciler adapter cannot be built", err)
	}
	return a, nil
}
