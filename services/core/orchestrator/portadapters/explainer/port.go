// Package explainerport is the orchestrator's Explainer port over the template Explainer (EXP-01 slice 4; ruling A155).
//
// It turns the session into the explainer's subject, passes the decision ID as it is, and returns text only for an answer the explainer did not block. The explainer renders,
// validates and records (EXPLANATION_RENDERED or EXPLANATION_BLOCKED) before it returns anything; this port adds no decision of its own.
//
// May import: the standard library, explanation, orchestrator. Nothing under evidence/ or trust/ imports this package.
//
// Must never: show text of a blocked answer, rewrite or hide an error, or read the Record or the Evidence Layer (the facts source behind the explainer does).
package explainerport

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// ErrNotUsable marks a call that cannot be honoured (no explainer, no tenant or principal, no decision); the explainer was not asked.
var ErrNotUsable = errors.New("explainerport: the explanation cannot be asked for")

// Explainer is the one method of explanation.Explainer this port uses.
type Explainer interface {
	Explain(ctx context.Context, s explanation.Subject, decisionID string) (explanation.Explanation, error)
}

// Port implements orchestrator.Explainer.
type Port struct{ Explainer Explainer }

var _ orchestrator.Explainer = Port{}

// Explain renders the answer for a recorded decision, as the session's subject.
func (p Port) Explain(ctx context.Context, s orchestrator.Session, decisionID string) (orchestrator.Explanation, error) {
	if p.Explainer == nil || s.TenantID == "" || s.PrincipalID == "" || decisionID == "" {
		return orchestrator.Explanation{}, ErrNotUsable
	}
	e, err := p.Explainer.Explain(ctx, explanation.Subject{TenantID: s.TenantID, PrincipalID: s.PrincipalID}, decisionID)
	if err != nil {
		return orchestrator.Explanation{}, err
	}
	if e.Blocked {
		return orchestrator.Explanation{Blocked: true}, nil
	}
	return orchestrator.Explanation{Text: e.Text}, nil
}
