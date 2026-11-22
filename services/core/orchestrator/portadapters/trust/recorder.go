package trustport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

// Errors of the Recorder adapter (TRU-12). Callers compare with errors.Is.
var (
	// ErrRecordRequest is returned for a request the adapter cannot map: a missing tenant, principal, builder or plan hash, or facts that do not fit the steps. Nothing is written.
	ErrRecordRequest = errors.New("trustport: the record request cannot be honoured")
	// ErrRecordConflict is returned when the same input (intent, intent hash, evidence snapshot, policy) is already stored with other content (A186, INV-21). Nothing is written.
	ErrRecordConflict = errors.New("trustport: the stored decision for this input does not agree with the new request")
	// ErrBasisDigest is returned when the digest the intake names is not the digest of the builder it passes (TRU-13). Nothing is written.
	ErrBasisDigest = errors.New("trustport: the basis digest is not the builder's")
	// ErrPlanHash is returned when the confirmation was given for another plan than the one being recorded, or a plan hash and a confirmation event do not come together (A230,
	// INV-17). Nothing is written.
	ErrPlanHash = errors.New("trustport: the confirmation was given for another plan")
	// ErrNoDeadline is returned when the context of a call has no deadline (AGENTS section 7: a context deadline on every call). Nothing is written and no transaction is opened.
	ErrNoDeadline = errors.New("trustport: the context has no deadline")
	// ErrCommit wraps a failure of the commit itself. The commit may or may not have reached the database, so this is the one error that does not say "nothing was written":
	// a retry of the same request returns the stored ALLOW with its grants (A186), and writes nothing twice.
	ErrCommit = errors.New("trustport: the commit failed; a retry returns the stored decision")
)

// rollbackBound is how long a rollback may take once the request's own context has ended. The rollback runs on a context that keeps the request's values and not its
// cancellation, so that a cancelled request still releases its transaction. commitBound is the same for the commit: once the adapter has decided to commit, the commit is
// finished, not abandoned halfway by a cancellation that arrives in the window (that would leave the outcome unknown for no reason); it is still bounded.
const (
	rollbackBound = 5 * time.Second
	commitBound   = 5 * time.Second
)

var (
	recDecisionRe = regexp.MustCompile(`\Adec_[0-9A-Za-z]{2,32}\z`)
	recHashRe     = regexp.MustCompile(`\Asha256:[0-9a-f]{64}\z`)
)

// TxBeginner opens a transaction as the tenant's own database login (the composition holds one pool per tenant, as noema#535 does). The adapter commits or rolls it back.
type TxBeginner interface {
	Begin(ctx context.Context, tenantID string) (pgx.Tx, error)
}

// RecordWriter is trust/recorder.Writer: it never commits or rolls back.
type RecordWriter interface {
	Write(ctx context.Context, tx pgx.Tx, r recorder.Request) (recorder.Result, error)
}

// RecordStepFacts is what the composition knows about one write step beyond the plan: the target versions the evidence was read at and the freeze permission for the capability on
// that target (REQ-RES-002). The Permission can only come from reconcile.FreezeGate.
type RecordStepFacts struct {
	TargetVersions []contracts.DecisionGrantV1TargetVersionsItem
	Permission     reconcile.Permission
}

// RecordFacts is what the composition adds to a RecordRequest: the policy the record and the grants bind, the hash of the evidence set, the content digest a confirmation binds (one per
// request, A459; set only for a request that follows a confirmation, and never the request's own value, A435), and one RecordStepFacts per write step, in
// step order. The stores behind it (T9 to T11) belong to other lanes.
type RecordFacts struct {
	PolicyID                        string
	EvidenceSetHash                 string
	ConfirmationBindingEvidenceHash string
	Steps                           []RecordStepFacts
}

// RecordFactSource gives the facts of one request.
type RecordFactSource interface {
	Facts(ctx context.Context, r intake.RecordRequest) (RecordFacts, error)
}

// RecorderConfig builds a RecorderAdapter. Keys come from the key files (SEC-09, NOEMA_GRANT_KEY_DIR); this package holds no key, no lifetime and no default.
type RecorderConfig struct {
	Begin    TxBeginner
	Facts    RecordFactSource
	Keys     *grants.Keys
	Appender *chain.Appender
	Clock    clock.Clock
	IDs      interface {
		ID(prefix string) (string, error)
	}
	Entropy        io.Reader
	GrantLifetime  time.Duration
	RecordLifetime time.Duration
	Escalation     recorder.EscalationSet
	// TxTimeout bounds the whole of one Record (facts, transaction, write) by a time of the adapter's own, so that a caller with a distant deadline cannot hold a transaction
	// open (AGENT-16). The earlier of this and the caller's deadline wins. It has no default: zero or negative is ErrConfig. Its value is the Architect's.
	TxTimeout time.Duration
}

// RecorderAdapter implements intake.Recorder (T15): it writes the Decision Record, the grants and their events with recorder.Writer inside one transaction it opens and commits,
// so they appear together or not at all (PERD 32.4, EC-E16). It decides nothing.
type RecorderAdapter struct {
	begin  TxBeginner
	facts  RecordFactSource
	writer RecordWriter

	txTimeout time.Duration
}

var _ intake.Recorder = (*RecorderAdapter)(nil)

// NewRecorderAdapter builds the adapter and its recorder.Writer. A missing port, key, clock, ID source, entropy or lifetime is ErrConfig.
func NewRecorderAdapter(c RecorderConfig) (*RecorderAdapter, error) {
	if c.Begin == nil || c.Facts == nil || c.Keys == nil || c.Appender == nil || c.Clock == nil || c.IDs == nil || c.Entropy == nil || c.GrantLifetime <= 0 || c.RecordLifetime <= 0 || c.TxTimeout <= 0 {
		return nil, ErrConfig
	}
	issuer, err := grants.NewIssuer(c.Keys, c.IDs, c.Entropy, c.GrantLifetime)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrConfig, err)
	}
	w := &recorder.Writer{Keys: c.Keys, Issuer: issuer, Appender: c.Appender, Clock: c.Clock, IDs: c.IDs, Escalation: c.Escalation, RecordLifetime: c.RecordLifetime}
	return &RecorderAdapter{begin: c.Begin, facts: c.Facts, writer: w, txTimeout: c.TxTimeout}, nil
}

// Record writes the decision and its grants in one transaction. The checks that need no database run first, so a refused request opens no transaction. If the context ends
// while the transaction runs, the transaction is rolled back and the context's error is returned: nothing is written (the other choice, returning the stored ALLOW on a retry,
// is what a failed commit leaves, see ErrCommit). A returned error other than ErrCommit means nothing was written. A context with no deadline is refused first (ErrNoDeadline); the facts, Begin and the write run under the earlier of the caller's deadline and TxTimeout.
func (a *RecorderAdapter) Record(ctx context.Context, r intake.RecordRequest) (intake.RecordResult, error) {
	if _, ok := ctx.Deadline(); !ok {
		return intake.RecordResult{}, ErrNoDeadline
	}
	// The earlier of the caller's deadline and the adapter's own TxTimeout bounds the facts, Begin and the write.
	ctx, cancel := context.WithTimeout(ctx, a.txTimeout)
	defer cancel()
	req, err := a.request(ctx, r)
	if err != nil {
		return intake.RecordResult{}, err
	}
	tx, err := a.begin.Begin(ctx, r.TenantID)
	if err != nil {
		return intake.RecordResult{}, fmt.Errorf("trustport: begin: %w", err)
	}
	res, err := a.writer.Write(ctx, tx, req)
	if errors.Is(err, recorder.ErrDecisionConflict) {
		err = fmt.Errorf("%w: %w", ErrRecordConflict, err)
	}
	if err == nil {
		err = ctx.Err() // a request that ended while it ran is not committed
	}
	if err == nil {
		err = answerShape(res, len(r.Steps))
	}
	if err != nil {
		rollback(ctx, tx)
		return intake.RecordResult{}, err
	}
	cc, ccancel := context.WithTimeout(context.WithoutCancel(ctx), commitBound)
	defer ccancel()
	if err := tx.Commit(cc); err != nil {
		rollback(ctx, tx)
		return intake.RecordResult{}, fmt.Errorf("%w: %w", ErrCommit, err)
	}
	out := intake.RecordResult{DecisionID: string(res.Record.DecisionId), ConfirmationRequestID: res.ConfirmationRequestID}
	for _, g := range res.Grants {
		out.GrantIDs = append(out.GrantIDs, string(g.GrantId))
	}
	return out, nil
}

// answerShape: a decision ID, and one grant for each write step and none for a read (the intake checks the same; a wrong shape is never committed).
func answerShape(res recorder.Result, steps int) error {
	if !recDecisionRe.MatchString(string(res.Record.DecisionId)) || len(res.Grants) != steps {
		return ErrBadAnswer
	}
	return nil
}

func rollback(ctx context.Context, tx pgx.Tx) {
	c, cancel := context.WithTimeout(context.WithoutCancel(ctx), rollbackBound)
	defer cancel()
	_ = tx.Rollback(c) // an error here leaves the transaction to the pool, which discards the connection; the error that caused the rollback is the one reported
}

// request maps the intake's request and the facts to recorder.Request, with every check that needs no database.
func (a *RecorderAdapter) request(ctx context.Context, r intake.RecordRequest) (recorder.Request, error) {
	if r.TenantID == "" || r.PrincipalID == "" || r.Evaluated == nil {
		return recorder.Request{}, ErrRecordRequest
	}
	if err := outcomeShape(r); err != nil {
		return recorder.Request{}, err
	}
	digest, err := r.Evaluated.Digest()
	if err != nil {
		return recorder.Request{}, fmt.Errorf("%w: %v", ErrBasisDigest, err)
	}
	if digest != r.BasisDigest {
		return recorder.Request{}, ErrBasisDigest
	}
	if (r.ConfirmationEventID == "") != (r.ConfirmationPlanHash == "") || (r.ConfirmationEventID != "" && r.ConfirmationPlanHash != r.PlanHash) {
		return recorder.Request{}, ErrPlanHash
	}
	for i, s := range r.Steps {
		// Plan steps are zero-based and a step's number is its index in the plan (intake T15; Execution E5 compares the grant's plan_step with the stored step's number). A number
		// outside the plan, a negative, a one-based or a repeated one would name a step E5 cannot find: refused before anything is written.
		if s.PlanStep != int64(i) {
			return recorder.Request{}, fmt.Errorf("%w: step %d carries plan step %d", ErrRecordRequest, i, s.PlanStep)
		}
	}
	f, err := a.facts.Facts(ctx, r)
	if err != nil {
		return recorder.Request{}, err
	}
	if f.PolicyID == "" || !recHashRe.MatchString(f.EvidenceSetHash) || len(f.Steps) != len(r.Steps) {
		return recorder.Request{}, ErrRecordRequest
	}
	// A435, A459, INV-25: a Record that follows a confirmation carries the fresh content digest of the evidence (the facts', never the request's); the Writer recomputes the binding from it
	// and the database refuses the spend unless the confirmation was given for exactly that. Any other Record carries none.
	var binding contracts.CommonV1Hash
	if r.ConfirmationEventID != "" {
		if !recHashRe.MatchString(f.ConfirmationBindingEvidenceHash) {
			return recorder.Request{}, fmt.Errorf("%w: a confirmed record has no well-formed fresh evidence value of its binding", ErrRecordRequest)
		}
		binding = contracts.CommonV1Hash(f.ConfirmationBindingEvidenceHash)
	}
	steps := make([]grants.Step, 0, len(r.Steps))
	for i, s := range r.Steps {
		steps = append(steps, grants.Step{PrincipalID: r.PrincipalID, CapabilityID: s.CapabilityID, TargetRef: s.TargetRef, ParamsHash: contracts.CommonV1Hash(s.ParamsHash),
			EvidenceSetHash: contracts.CommonV1Hash(f.EvidenceSetHash), PlanHash: contracts.CommonV1Hash(r.PlanHash), PlanStep: s.PlanStep,
			TargetVersions: f.Steps[i].TargetVersions, Permission: f.Steps[i].Permission})
	}
	return recorder.Request{TenantID: r.TenantID, CorrelationID: r.CorrelationID, IntentID: r.IntentID, IntentHash: contracts.CommonV1Hash(r.IntentHash), PolicyID: f.PolicyID,
		Decision: r.Decision, Evaluated: r.Evaluated, ConfirmationPlanHash: contracts.CommonV1Hash(r.ConfirmationPlanHash), ConfirmationEventID: r.ConfirmationEventID, ConfirmationBindingEvidenceHash: binding, Steps: steps,
		ConfirmationRequest: newRequest(r.ConfirmationRequest)}, nil
}

// outcomeShape is A456 (interim, relayed, not yet committed): the port serves every outcome. An ALLOW names its plan hash (a read has no steps but has its plan and its hash, A205
// Q3; a write's steps are the intake's to count). Every other outcome has no plan hash and no step, so no grant (INV-03), and only a CONFIRM carries a confirmation request.
func outcomeShape(r intake.RecordRequest) error {
	if r.Decision.Outcome() == decide.Allow {
		if !recHashRe.MatchString(r.PlanHash) {
			return ErrRecordRequest
		}
		return nil
	}
	if r.PlanHash != "" || len(r.Steps) > 0 || r.ConfirmationRequest != nil && r.Decision.Outcome() != decide.Confirm {
		return fmt.Errorf("%w: a %s decision carries no plan hash, no step and no grant", ErrRecordRequest, r.Decision.Outcome())
	}
	return nil
}

// newRequest maps the confirmation request of a CONFIRM value for value; the Writer mints the request's ID and computes the binding hash (SoT 43.2a).
func newRequest(c *intake.NewConfirmationRequest) *recorder.NewConfirmationRequest {
	if c == nil {
		return nil
	}
	return &recorder.NewConfirmationRequest{PrincipalID: c.PrincipalID, IntentHash: c.IntentHash, CapabilityID: c.CapabilityID, TargetRef: c.TargetRef, ParamsHash: c.ParamsHash,
		ConfirmationBindingEvidenceHash: c.ConfirmationBindingEvidenceHash, PlanHash: c.PlanHash, PolicyVersion: c.PolicyVersion, Requirement: c.Requirement, RequesterID: c.RequesterID, ExpiresAt: c.ExpiresAt}
}
