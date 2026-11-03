package verifier

import (
	"context"
	"errors"
	"fmt"
	"time"

	ip "github.com/Noemai-org/noema/services/core/internal/predicate"
	opredicate "github.com/Noemai-org/noema/services/core/outcome/predicate"
)

const (
	schemaVersion = "1.0"

	idVerdict  = "ver"
	idEvidence = "evr"
	idRecovery = "rrq"
	idWatch    = "wat"

	queryByKey         = "BY_KEY"
	queryByTransaction = "BY_TRANSACTION"

	readOK          = "OK"
	readUnavailable = "UNAVAILABLE"
	readMalformed   = "MALFORMED"
	readRefused     = "REFUSED"

	caseEntity   = "case"
	receiptAcked = "ACKNOWLEDGED"
)

// knownStates are the execution states a receipt can hold (execution-receipt.v1). Any other value is corrupt data.
var knownStates = map[string]bool{"DISPATCHED": true, "ACKNOWLEDGED": true, "OUTCOME_UNKNOWN": true, "REJECTED_BY_TARGET": true,
	"REJECTED_STALE_VERSION": true, "FAILED_TERMINAL": true}

var errNoDeadline = errors.New("verifier: the context needs a deadline")

// Verifier decides, from its own reads, whether an action achieved what it claimed (REQ-VER-001). It reads the target
// through CaseReader and never uses the receipt, an HTTP status, a workflow completion or Trust's snapshot as proof
// (INV-04, AGENT-21).
//
// Scope of OUT-01: the `case` evidence entity and a capability that is QUERYABLE_BY_KEY. Other entities have no reader
// yet; a predicate that names one does not compile here (OUT-02 and the Evidence Layer widen it).
type Verifier struct {
	cfg   Config
	scope ip.Scope
}

// New checks the wiring. A missing port is an error here, never a silent default (fail closed).
func New(cfg Config) (*Verifier, error) {
	switch {
	case cfg.Clock == nil, cfg.Waiter == nil, cfg.IDs == nil, cfg.Cases == nil, cfg.Store == nil, cfg.Signer == nil, cfg.Authenticity == nil:
		return nil, errors.New("verifier: every port (clock, waiter, ids, cases, store, signer, authenticity) is required")
	case cfg.Budget.MaxPages < 1:
		return nil, errors.New("verifier: the read budget needs at least one page")
	}
	fields, ok := cfg.Scope.Entities[caseEntity]
	if !ok {
		return nil, fmt.Errorf("verifier: the scope has no %q entity", caseEntity)
	}
	// Only the entity the verifier can read is in the scope a definition compiles against.
	scope := ip.Scope{Entities: map[string]map[string]ip.Type{caseEntity: fields}, Variables: cfg.Scope.Variables}
	return &Verifier{cfg: cfg, scope: scope}, nil
}

// run is one verification in progress.
type run struct {
	v        *Verifier
	ctx      context.Context
	req      Request
	rc       Receipt
	reads    []ReadRecord
	evidence []string
	unbound  bool  // a predicate variable the definition names has no binding
	err      error // the first fault of the run itself (an ID source, the waiter): no verdict is stored
}

type decision struct {
	verdict   Verdict
	reason    Reason
	predicate *bool
	watch     *Pending
	auth      Authenticity
	badSig    bool
}

// Verify runs one verification and stores its verdict, its RecoveryRequest (every verdict except ACHIEVED), its watch
// (PENDING_EXTERNAL) and their Record events in one transaction. While the settle window is open it reads and writes
// nothing and returns *ErrSettleWindowOpen.
func (v *Verifier) Verify(ctx context.Context, req Request) (Result, error) {
	if _, ok := ctx.Deadline(); !ok {
		return Result{}, errNoDeadline
	}
	if req.VerificationID == "" {
		return Result{}, errors.New("verifier: a verification needs an ID, so that a redelivery finds its verdict")
	}
	if res, found, err := v.replay(ctx, req); err != nil || found {
		return res, err
	}
	success, pending, unbound, err := v.compile(req)
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrDefinition, err)
	}
	rc, err := v.cfg.Store.LoadReceipt(ctx, req.TenantID, req.ReceiptID)
	if err != nil {
		return Result{}, fmt.Errorf("verifier: load receipt %s: %w", req.ReceiptID, err)
	}
	if rc.TenantID != req.TenantID || rc.ReceiptID != req.ReceiptID {
		return Result{}, fmt.Errorf("verifier: receipt %s is not tenant %s's", req.ReceiptID, req.TenantID)
	}
	if rc.PrincipalID == "" {
		return Result{}, fmt.Errorf("verifier: receipt %s names no principal: the transaction read cannot be scoped", req.ReceiptID)
	}
	if !knownStates[rc.State] {
		return Result{}, fmt.Errorf("verifier: receipt %s is in a state no execution produces", req.ReceiptID)
	}
	if ready := rc.DispatchedAt.Add(time.Duration(req.Definition.SettleWindowSeconds) * time.Second); v.cfg.Clock.Now().Before(ready) {
		return Result{}, &ErrSettleWindowOpen{ReadyAt: ready}
	}
	r := &run{v: v, ctx: ctx, req: req, rc: rc, unbound: unbound}
	d := r.decide(success, pending)
	if r.err != nil {
		return Result{}, r.err
	}
	res, err := r.store(d)
	if errors.Is(err, ErrAlreadyVerified) {
		// Another delivery of the same verification stored its verdict first: that verdict stands.
		if res, found, rerr := v.replay(ctx, req); rerr != nil || found {
			return res, rerr
		}
	}
	return res, err
}

// replay returns the stored verdict of a verification that has been decided, without reading or writing anything.
func (v *Verifier) replay(ctx context.Context, req Request) (Result, bool, error) {
	found, ok, err := v.cfg.Store.FindVerdict(ctx, req.TenantID, req.VerificationID)
	if err != nil {
		return Result{}, false, fmt.Errorf("verifier: look up verification %s: %w", req.VerificationID, err)
	}
	if !ok {
		return Result{}, false, nil
	}
	if found.ReceiptID != req.ReceiptID {
		return Result{}, false, fmt.Errorf("verifier: verification %s was made for another receipt", req.VerificationID)
	}
	res := found.Result
	res.Replayed = true
	return res, true, nil
}

// compile type-checks the definition's predicates against the scope the verifier can serve. unbound is true when a predicate
// names a variable that the request does not bind: the same predicate fails to compile against a scope that holds only the
// bound variables. The compiler decides this, not a scan of the source text.
func (v *Verifier) compile(req Request) (success, pending *ip.Program, unbound bool, err error) {
	if s := req.Definition.SettleWindowSeconds; s < 0 || s > opredicate.MaxSettleSeconds {
		return nil, nil, false, errors.New("a settle window outside 0 to opredicate.MaxSettleSeconds seconds")
	}
	boundVars := make(map[string]ip.Type, len(req.Bindings))
	for name := range req.Bindings {
		t, ok := v.scope.Variables[name]
		if !ok {
			return nil, nil, false, fmt.Errorf("%q is not a predicate variable", name)
		}
		boundVars[name] = t
	}
	bound := ip.Scope{Entities: v.scope.Entities, Variables: boundVars}
	if success, err = opredicate.Compile(req.Definition.SuccessPredicate, v.scope); err != nil {
		return nil, nil, false, fmt.Errorf("success predicate: %w", err)
	}
	if _, berr := opredicate.Compile(req.Definition.SuccessPredicate, bound); berr != nil {
		unbound = true
	}
	if p := req.Definition.Pending; p != nil {
		if pending, err = opredicate.Compile(p.Predicate, v.scope); err != nil {
			return nil, nil, false, fmt.Errorf("pending condition: %w", err)
		}
		if _, berr := opredicate.Compile(p.Predicate, bound); berr != nil {
			unbound = true
		}
	}
	return success, pending, unbound, nil
}

func (r *run) newID(prefix string) string {
	id, err := r.v.cfg.IDs.ID(prefix)
	if err != nil && r.err == nil {
		r.err = fmt.Errorf("verifier: %s id: %w", prefix, err)
	}
	return id
}

func unknown(reason Reason) decision {
	return decision{verdict: Unknown, reason: reason, auth: AuthNotChecked}
}

func (r *run) decide(success, pending *ip.Program) decision {
	auth, err := r.v.cfg.Authenticity.Check(r.ctx, r.rc)
	if err != nil {
		// A check that cannot run is not a pass, and not a finding either: fail closed without calling the signature invalid.
		return unknown(ReasonReceiptUnverifiable)
	}
	switch auth {
	case AuthValid, AuthNotChecked:
	case AuthInvalid:
		d := unknown(ReasonReceiptUnverifiable)
		d.auth, d.badSig = auth, true
		return d
	default:
		// A status this verifier does not know is neither a pass nor a finding: fail closed, raise nothing.
		return unknown(ReasonReceiptUnverifiable)
	}
	if r.unbound {
		// The language reads an unbound variable as null and a comparison with null as false; a verdict must not follow
		// from a variable nobody bound (REQ-VER-002: a missing field is UNKNOWN, not false).
		d := unknown(ReasonPredicateUnknown)
		d.auth = auth
		return d
	}
	d := r.verdict(success, pending)
	d.auth = auth
	return d
}

func (r *run) verdict(success, pending *ip.Program) decision {
	if !r.req.QueryableByKey {
		return unknown(ReasonNotQueryableByKey)
	}
	byKey, reason := r.readAll(CaseQuery{TenantID: r.req.TenantID, IdempotencyKey: r.rc.IdempotencyKey}, queryByKey)
	if reason != "" {
		return unknown(reason)
	}
	byTx, reason := r.readAll(CaseQuery{TenantID: r.req.TenantID, PrincipalID: r.rc.PrincipalID, TransactionRef: r.req.TransactionRef}, queryByTransaction)
	if reason != "" {
		return unknown(reason)
	}
	if !r.consistent(byKey, byTx) {
		return unknown(ReasonInconsistent)
	}
	env, ok := r.env(byTx)
	if !ok {
		return unknown(ReasonInconsistent)
	}
	ok1, known := evalBool(success, env)
	if !known {
		return unknown(ReasonPredicateUnknown)
	}
	if ok1 && len(byKey) == 1 {
		// The predicate was read over every case of the transaction, but only the case under our key is our effect. It must
		// satisfy the predicate on its own: another case cannot make this one a success (INV-04, AGENT-21).
		ownEnv, ok := r.env(byKey)
		if !ok {
			return unknown(ReasonInconsistent)
		}
		own, ownKnown := evalBool(success, ownEnv)
		if !ownKnown {
			return unknown(ReasonPredicateUnknown)
		}
		if !own {
			return unknown(ReasonInconsistent)
		}
	}
	switch {
	case ok1 && len(byKey) == 1:
		return decision{verdict: Achieved, reason: ReasonPredicateTrue, predicate: &ok1}
	case ok1: // the world looks right, but not through our key: not our effect, so not a success
		return unknown(ReasonInconsistent)
	}
	notAchieved := ReasonPredicateFalse
	if len(byKey) == 0 {
		notAchieved = ReasonVerifiedAbsent
	}
	if r.rc.State == receiptAcked {
		notAchieved = ReasonPostconditionUnchecked
	}
	if pending != nil && len(byKey) == 1 {
		// Like the success predicate, the pending condition is about the case under our key, not any case of the transaction.
		ownEnv, ok := r.env(byKey)
		if !ok {
			return unknown(ReasonInconsistent)
		}
		pend, known := evalBool(pending, ownEnv)
		if !known {
			return unknown(ReasonPredicateUnknown)
		}
		if pend {
			return r.pendingExternal(&ok1)
		}
	}
	return decision{verdict: NotAchieved, reason: notAchieved, predicate: &ok1}
}

// pendingExternal accepts the watch only if it can be kept: a first check and a deadline, both ahead of now and in order
// (INV-28). A PENDING_EXTERNAL verdict without a usable watch is never stored.
func (r *run) pendingExternal(predicate *bool) decision {
	p, now := r.req.Definition.Pending, r.v.cfg.Clock.Now()
	if p.DeadlineAt.IsZero() || !p.DeadlineAt.After(now) || !p.NextCheckAt.After(now) || p.NextCheckAt.After(p.DeadlineAt) {
		return unknown(ReasonWatchUnusable)
	}
	return decision{verdict: PendingExternal, reason: ReasonPendingCondition, predicate: predicate, watch: p}
}

// evalBool evaluates a Boolean program. known is false when the result is null or the evaluation fails: UNKNOWN
// propagates and is never read as false (REQ-VER-002).
func evalBool(p *ip.Program, env ip.Env) (value, known bool) {
	v, err := p.Eval(env)
	if err != nil || v.IsNull() || v.Kind != ip.KindBoolean {
		return false, false
	}
	return v.B, true
}

// consistent checks that every case read belongs to this tenant and transaction, that the key's case is the case under
// that key, that at most one case holds the key, and that the key's case is among the transaction's cases.
func (r *run) consistent(byKey, byTx []Case) bool {
	if len(byKey) > 1 {
		return false
	}
	for _, c := range byKey {
		if c.TenantID != r.req.TenantID || c.PrincipalID != r.rc.PrincipalID || c.IdempotencyKey != r.rc.IdempotencyKey || c.TransactionRef != r.req.TransactionRef {
			return false
		}
	}
	for _, c := range byTx {
		if c.TenantID != r.req.TenantID || c.PrincipalID != r.rc.PrincipalID || c.TransactionRef != r.req.TransactionRef {
			return false
		}
	}
	if len(byKey) == 1 {
		for _, c := range byTx {
			if c.CaseID == byKey[0].CaseID {
				return true
			}
		}
		return false
	}
	return true
}

// env builds the predicate environment from the transaction's cases. A status the descriptor does not list is schema
// drift: the evidence is UNKNOWN, not a case to be compared.
func (r *run) env(cases []Case) (ip.Env, bool) {
	status := r.v.scope.Entities[caseEntity]["status"]
	rows := make([]ip.Row, 0, len(cases))
	for _, c := range cases {
		if status.Kind == ip.KindEnum && !contains(status.Values, c.Status) {
			return ip.Env{}, false
		}
		row := ip.Row{"id": ip.Ref(c.CaseID), "subject": ip.Ref(c.TransactionRef), "status": ip.Enum(c.Status), "created_by_intent": ip.Ref(c.CreatedByIntent), "epoch": ip.Int(c.Epoch)}
		if c.OwnerQueue != "" {
			row["owner_queue"] = ip.Ref(c.OwnerQueue)
		}
		rows = append(rows, row)
	}
	vars := make(map[string]ip.Value, len(r.req.Bindings))
	for name, val := range r.req.Bindings {
		v, ok := variableValue(r.v.scope.Variables[name], val)
		if !ok {
			return ip.Env{}, false
		}
		vars[name] = v
	}
	return ip.Env{Entities: map[string][]ip.Row{caseEntity: rows}, Variables: vars, Now: r.v.cfg.Clock.Now().Unix()}, true
}

// variableValue types a bound string by its declared variable type. Only the types a caller can bind as text are served.
func variableValue(t ip.Type, s string) (ip.Value, bool) {
	switch t.Kind {
	case ip.KindReference:
		return ip.Ref(s), true
	case ip.KindString:
		return ip.Str(s), true
	case ip.KindEnum:
		return ip.Enum(s), contains(t.Values, s)
	}
	return ip.Value{}, false
}

func contains(set []string, s string) bool {
	for _, x := range set {
		if x == s {
			return true
		}
	}
	return false
}
