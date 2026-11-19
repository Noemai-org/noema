// Package trustport is the orchestrator's Trust (STM-01): it asks the intake to decide, maps the one answer to an orchestrator.Decision, and proves a write's Binding against
// the decision and grant records Trust itself wrote (INV-03, INV-12). It decides nothing: an outcome is the intake's, and an answer outside the contract is an error, never a
// guess (INV-01). It also answers the intake's dispatch count, with a limit of at least 1 for every capability (A306).
package trustport

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Errors of the adapter. Callers compare with errors.Is.
var (
	// ErrSession is returned for a session without a tenant or a principal, and for a dispatch key without a tenant.
	ErrSession = errors.New("trustport: the session has no tenant or no principal")
	// ErrIntentRef is returned for an intent reference that is not `int_<id>@<version>` and for a stored intent that names no correlation.
	ErrIntentRef = errors.New("trustport: the intent reference or the stored intent is not usable")
	// ErrBadAnswer is returned for an answer outside what the intake or the ledger may give.
	ErrBadAnswer = errors.New("trustport: a port gave an answer outside its contract")
	// ErrNoRecord is what a Records implementation returns for a decision or grant that does not exist or that the tenant does not own: the same answer for both.
	ErrNoRecord = errors.New("trustport: no such record")
	// ErrConfig is returned for an adapter built without a port or with a limit below 1.
	ErrConfig = errors.New("trustport: the adapter is not configured")
)

// Evaluator is the intake (trust/intake.Service).
type Evaluator interface {
	Evaluate(ctx context.Context, r intake.Request) (intake.Result, error)
}

// Intents reads a stored, admitted intent as the tenant. The intent names the correlation it was admitted under: Decide takes it from there, because the orchestrator's
// port carries no correlation of its own and the text never names one.
type Intents interface {
	Get(ctx context.Context, tenantID, intentID string, version int) (intake.AdmittedIntent, error)
}

// DecisionRecord and GrantRecord are what Trust recorded, read as the tenant.
type DecisionRecord struct{ ID, Outcome, IntentRef string }

// GrantRecord is one Decision Grant as stored: its decision, the correlation and the principal it names.
type GrantRecord struct{ ID, DecisionID, CorrelationID, PrincipalID string }

// Records reads Trust's own decision and grant records. ErrNoRecord is the answer for a record that is absent or another tenant's.
type Records interface {
	Decision(ctx context.Context, tenantID, decisionID string) (DecisionRecord, error)
	Grant(ctx context.Context, tenantID, grantID string) (GrantRecord, error)
}

// Port implements orchestrator.Trust and orchestrator.Bindings.
type Port struct {
	Evaluator Evaluator
	Intents   Intents
	Records   Records
}

var (
	_ orchestrator.Trust    = Port{}
	_ orchestrator.Bindings = Port{}
)

// OutcomeOf maps a Trust outcome to the contract enum. Any other value is ErrBadAnswer.
func OutcomeOf(o decide.Outcome) (contracts.EnumsV1DecisionOutcome, error) {
	switch o {
	case decide.Allow:
		return contracts.EnumsV1DecisionOutcomeALLOW, nil
	case decide.Clarify:
		return contracts.EnumsV1DecisionOutcomeCLARIFY, nil
	case decide.Confirm:
		return contracts.EnumsV1DecisionOutcomeCONFIRM, nil
	case decide.Wait:
		return contracts.EnumsV1DecisionOutcomeWAIT, nil
	case decide.Escalate:
		return contracts.EnumsV1DecisionOutcomeESCALATE, nil
	case decide.Reject:
		return contracts.EnumsV1DecisionOutcomeREJECT, nil
	case decide.Unknown:
		return contracts.EnumsV1DecisionOutcomeUNKNOWN, nil
	}
	return "", fmt.Errorf("%w: outcome %q", ErrBadAnswer, o)
}

func (p Port) usable() error {
	if p.Evaluator == nil || p.Intents == nil || p.Records == nil {
		return ErrConfig
	}
	return nil
}

// Decide asks the intake for the one decision of the intent on the evidence. The identity is the session's, the correlation the stored intent's (INV-12). Only an ALLOW
// carries a decision ID: the intake records an ALLOW before it returns one (EC-E16), and an ALLOW without an ID is an error. An ALLOW with no grant is a read.
func (p Port) Decide(ctx context.Context, s orchestrator.Session, intentRef string, e orchestrator.EvidenceRef) (orchestrator.Decision, error) {
	if err := p.usable(); err != nil {
		return orchestrator.Decision{}, err
	}
	if s.TenantID == "" || s.PrincipalID == "" {
		return orchestrator.Decision{}, ErrSession
	}
	intentID, version, err := contracts.ParseIntentRef(intentRef)
	if err != nil {
		return orchestrator.Decision{}, fmt.Errorf("%w: the reference is not int_<id>@<version>", ErrIntentRef)
	}
	stored, err := p.Intents.Get(ctx, s.TenantID, intentID, version)
	if err != nil {
		return orchestrator.Decision{}, fmt.Errorf("trustport: read the intent: %w", err)
	}
	if stored.CorrelationID == "" {
		return orchestrator.Decision{}, fmt.Errorf("%w: the stored intent names no correlation", ErrIntentRef)
	}
	res, err := p.Evaluator.Evaluate(ctx, intake.Request{
		Session:             intake.Session{TenantID: s.TenantID, PrincipalID: s.PrincipalID, Role: s.Role},
		CorrelationID:       stored.CorrelationID,
		IntentRef:           intentRef,
		EvidenceSnapshotID:  e.SnapshotID,
		EvidenceUnavailable: e.Unavailable,
	})
	if err != nil {
		return orchestrator.Decision{}, fmt.Errorf("trustport: evaluate: %w", err)
	}
	outcome, err := OutcomeOf(res.Decision.Outcome())
	if err != nil {
		return orchestrator.Decision{}, err
	}
	if outcome != contracts.EnumsV1DecisionOutcomeALLOW {
		return orchestrator.Decision{Outcome: outcome}, nil
	}
	if res.DecisionID == "" {
		return orchestrator.Decision{}, fmt.Errorf("%w: an ALLOW with no recorded decision", ErrBadAnswer)
	}
	return orchestrator.Decision{
		ID: res.DecisionID, Outcome: outcome, Read: len(res.GrantIDs) == 0, GrantIDs: append([]string(nil), res.GrantIDs...), IntentRef: intentRef,
	}, nil
}

// Bound proves the Binding against what Trust recorded (Codex P1 on noema#576; INV-03). Everything that does not match, or is not found, is orchestrator.ErrNotBound; a
// store failure is returned as it is, so the caller can tell a refusal from "could not tell".
func (p Port) Bound(ctx context.Context, s orchestrator.Session, b orchestrator.Binding) error {
	if err := p.usable(); err != nil {
		return err
	}
	if s.TenantID == "" || s.PrincipalID == "" {
		return ErrSession
	}
	if b.CorrelationID == "" || b.DecisionID == "" || b.GrantID == "" || b.IntentRef == "" {
		return orchestrator.ErrNotBound
	}
	d, err := p.Records.Decision(ctx, s.TenantID, b.DecisionID)
	if errors.Is(err, ErrNoRecord) {
		return orchestrator.ErrNotBound
	}
	if err != nil {
		return fmt.Errorf("trustport: read the decision: %w", err)
	}
	g, err := p.Records.Grant(ctx, s.TenantID, b.GrantID)
	if errors.Is(err, ErrNoRecord) {
		return orchestrator.ErrNotBound
	}
	if err != nil {
		return fmt.Errorf("trustport: read the grant: %w", err)
	}
	if d.ID != b.DecisionID || d.Outcome != string(contracts.EnumsV1DecisionOutcomeALLOW) || d.IntentRef != b.IntentRef ||
		g.ID != b.GrantID || g.DecisionID != b.DecisionID || g.CorrelationID != b.CorrelationID || g.PrincipalID != s.PrincipalID {
		return orchestrator.ErrNotBound
	}
	return nil
}
