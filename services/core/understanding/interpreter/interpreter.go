package interpreter

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"

	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

const prefix = "interpreter: "

var errEpochNotPositive = errors.New(prefix + "the epoch port answered an epoch below 1")

// The kinds of failure of Interpret. Callers compare with errors.Is. None of the messages carries the input, the proposal or an admitted document: a cause that may hold
// one (a database error with a key, a model service answer) is kept for errors.Is and errors.As, never printed.
var (
	// ErrInput is an input the Interpreter refuses before it calls anything: an incomplete session, an actor that is not the session's, a malformed correlation id. It is a
	// bug of the caller, never the model's, so nothing is recorded for it.
	ErrInput = errors.New(prefix + "the input is not complete")
	// ErrCompile is a failed compile call. A *CompileFailure says which of the model service's failure classes it was.
	ErrCompile = errors.New(prefix + "the model service could not compile")
	// ErrRecord means the Record could not take an event the Interpreter must record first: nothing was saved (EC-E16).
	ErrRecord = errors.New(prefix + "the Record is not available")
	// ErrSave means the store refused or failed to save the admitted intent: neither its event nor its row stands.
	ErrSave = errors.New(prefix + "the admitted intent could not be saved")
	// ErrEpoch means the logical resolution epoch of the intent's subject could not be named (the epoch port could not answer, or answered with something that is not a positive
	// epoch): nothing was saved, because a guessed epoch would reuse an earlier epoch's idempotency key (PERD 28.2).
	ErrEpoch = errors.New(prefix + "the logical resolution epoch could not be named")
)

// CompileFailure is a compile call that failed in one of the model service client's four classes. It is an ErrCompile.
type CompileFailure struct{ Failure understanding.Failure }

func (e *CompileFailure) Error() string {
	return prefix + "the model service could not compile: " + string(e.Failure)
}
func (e *CompileFailure) Unwrap() error { return ErrCompile }

// opError is a failure whose text is fixed: the cause stays reachable for errors.Is and errors.As and is never part of Error().
type opError struct{ kind, cause error }

func (e *opError) Error() string   { return e.kind.Error() }
func (e *opError) Unwrap() []error { return []error{e.kind, e.cause} }

func wrap(kind, cause error) error { return &opError{kind: kind, cause: cause} }

// Subject is who an event of the Interpreter is about: the tenant of the session, the principal that caused it (`<kind>:<id>`) and the investigation's correlation id.
type Subject struct {
	TenantID      string
	Actor         string
	CorrelationID string
}

// Compiler is the model service's client (understanding.Client implements it).
type Compiler interface {
	Compile(ctx context.Context, req understanding.CompileRequest) (understanding.Compiled, error)
}

// Admitter is the core's gate (understanding.Gate implements it).
type Admitter interface {
	Admit(raw []byte, s understanding.Session, correlationID string) (understanding.Admission, error)
}

// Recorder appends the two events of the Interpreter. An error means nothing was appended. The reasons and the field names are codes, never text of an input.
type Recorder interface {
	Rejected(ctx context.Context, s Subject, reasons []understanding.Reason) error
	OverrideAttempt(ctx context.Context, s Subject, fields []string) error
}

// Saver stores an admitted intent and appends INTENT_ADMITTED with it (intentstore.Store implements it).
type Saver interface {
	Save(ctx context.Context, actor string, a intentstore.Admitted) (intentstore.Ref, error)
}

// Epochs answers the logical resolution epoch of a new intent about a subject, in the scope of the session's tenant and principal (DEBT-54 part 2, ruling A520). The Evidence
// reader's case list, read without recording, implements it in the composition (evidence/portread.Reader.NextEpoch). An error means no epoch can be named.
type Epochs interface {
	EpochOf(ctx context.Context, tenantID, principalID, subjectRef string) (int, error)
}

// Config is what New needs. The first four parts are required; there are no defaults. Subject and Epochs are optional and come together: with neither, an admitted intent is
// saved with no subject and epoch 1 (the G1 path); with both, the subject of the intent is read from the pack's subject slot and its epoch from the case list.
type Config struct {
	Compiler Compiler
	Gate     Admitter
	Recorder Recorder
	Saver    Saver
	Subject  func(understanding.AdmittedIntent) (subjectRef string, ok bool) // understanding.SubjectOf(pack view)
	Epochs   Epochs
}

// Interpreter compiles, admits, records and saves.
type Interpreter struct{ c Config }

// New refuses a configuration with a missing part.
func New(c Config) (*Interpreter, error) {
	if c.Compiler == nil || c.Gate == nil || c.Recorder == nil || c.Saver == nil {
		return nil, errors.New(prefix + "a compiler, a gate, a recorder and a saver are all required")
	}
	if (c.Subject == nil) != (c.Epochs == nil) {
		return nil, errors.New(prefix + "a subject reader and an epoch port come together")
	}
	return &Interpreter{c: c}, nil
}

// Input is one compile. The session and the actor come from the authenticated session (INV-12) and are never read from the request.
type Input struct {
	Session       understanding.Session
	Actor         string // "<kind>:<id>" of the session's principal, as the Record writes it
	CorrelationID string // the investigation's, minted by the core
	Request       understanding.CompileRequest
	RawRef        string // the object_ref of the recorded INPUT_RECEIVED (A184, A219); the core writes it over the model's source.raw_ref
}

// Result is an admitted and saved intent: the admitted intent and the reference of its INTENT_ADMITTED event.
type Result struct {
	Intent understanding.AdmittedIntent
	Ref    intentstore.Ref
}

var (
	actorRe = regexp.MustCompile(`^[a-z]+:[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	corrRe  = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	// rawRefRe is the shape of the object_ref the core mints for an input (common.v1, A184). \A and \z: a trailing newline is not a match.
	rawRefRe = regexp.MustCompile(`\Areq_[0-9A-HJKMNP-TV-Z]{26}\z`)
)

func validate(in Input) error {
	s := in.Session
	if !s.Valid() { // the gate's own rule for the whole session, before the model service sees anything (Codex P2 on noema#519)
		return ErrInput
	}
	if !actorRe.MatchString(in.Actor) || !strings.HasSuffix(in.Actor, ":"+s.PrincipalID) || !corrRe.MatchString(in.CorrelationID) || !rawRefRe.MatchString(in.RawRef) {
		return ErrInput
	}
	return nil
}

// Interpret runs one compile. Order: the input, the model service, the gate; then, by what the gate found, the Record first and the store last. A refusal at the gate is
// recorded as INTENT_REJECTED_AT_GATE and returned as the gate's *understanding.Rejection. A real identity override is recorded as SECURITY_EVENT
// PRINCIPAL_OVERRIDE_ATTEMPT before the save, and the intent saved is the session's. A normal compile records nothing of its own (A166, item 3).
func (i *Interpreter) Interpret(ctx context.Context, in Input) (Result, error) {
	if err := validate(in); err != nil {
		return Result{}, err
	}
	compiled, err := i.c.Compiler.Compile(ctx, in.Request)
	if err != nil {
		var ce *understanding.CompileError
		if errors.As(err, &ce) {
			return Result{}, &CompileFailure{Failure: ce.Failure}
		}
		return Result{}, wrap(ErrCompile, err)
	}
	doc, err := withRawRef(compiled.IntentObject, in.RawRef)
	if err != nil {
		return Result{}, wrap(ErrCompile, err)
	}
	compiled.IntentObject = doc
	subject := Subject{TenantID: in.Session.TenantID, Actor: in.Actor, CorrelationID: in.CorrelationID}
	adm, err := i.c.Gate.Admit(compiled.IntentObject, in.Session, in.CorrelationID)
	if err != nil {
		var rej *understanding.Rejection
		if !errors.As(err, &rej) {
			return Result{}, wrap(ErrCompile, err)
		}
		for _, r := range rej.Reasons {
			if r == understanding.ReasonIdentityNotInOrder {
				return Result{}, wrap(ErrInput, err) // the session the caller passed is incomplete: not the model's doing, so not a rejection to record
			}
		}
		if err := i.c.Recorder.Rejected(ctx, subject, rej.Reasons); err != nil {
			return Result{}, wrap(ErrRecord, err)
		}
		return Result{}, rej
	}
	if len(adm.Overrides) > 0 {
		if err := i.c.Recorder.OverrideAttempt(ctx, subject, adm.Overrides); err != nil {
			return Result{}, wrap(ErrRecord, err)
		}
	}
	saved := admitted(adm.Intent)
	if i.c.Subject != nil {
		if subject, ok := i.c.Subject(adm.Intent); ok { // an intent whose subject is not resolved has no subject: epoch 1, nothing to ask
			epoch, err := i.c.Epochs.EpochOf(ctx, in.Session.TenantID, in.Session.PrincipalID, subject)
			if err != nil {
				return Result{}, wrap(ErrEpoch, err)
			}
			if epoch < 1 {
				return Result{}, wrap(ErrEpoch, errEpochNotPositive)
			}
			saved.SubjectRef, saved.LogicalResolutionEpoch = subject, epoch
		}
	}
	ref, err := i.c.Saver.Save(ctx, in.Actor, saved)
	if err != nil {
		return Result{}, wrap(ErrSave, err)
	}
	return Result{Intent: adm.Intent, Ref: ref}, nil
}

// admitted is the store's view of an admitted intent: the columns the keys use, the hash the gate computed and the canonical document it covers.
func admitted(a understanding.AdmittedIntent) intentstore.Admitted {
	o := a.Object()
	sup := ""
	if o.Supersedes != nil {
		sup = string(*o.Supersedes)
	}
	return intentstore.Admitted{TenantID: string(o.TenantId), IntentID: string(o.IntentId), Version: int(o.Version), Supersedes: sup, IntentNode: string(o.IntentNode),
		IntentHash: a.Hash(), CorrelationID: string(o.CorrelationId), JSON: a.JSON()}
}

// withRawRef writes the core's reference to the recorded input over whatever the model put in `source.raw_ref`, including the unset marker (A219). It runs after the model
// service's schema gate and before the core's gate, so the gate and the store only ever see the core's value. It fails closed: a document with no `source`, a `source` that is
// not an object, or a `raw_ref` that is not a string is an error, never passed through with the model's value. Only raw_ref is written; a document that is not an object at all
// is returned as it is, for the gate to refuse.
func withRawRef(doc []byte, rawRef string) ([]byte, error) {
	var top map[string]json.RawMessage
	if json.Unmarshal(doc, &top) != nil || top == nil {
		return doc, nil
	}
	var source map[string]json.RawMessage
	if json.Unmarshal(top["source"], &source) != nil || source == nil {
		return nil, errBadSource
	}
	if cur, ok := source["raw_ref"]; ok {
		var s string
		if json.Unmarshal(cur, &s) != nil || string(cur) == "null" {
			return nil, errBadSource
		}
	}
	ref, err := json.Marshal(rawRef)
	if err != nil {
		return nil, errBadSource
	}
	source["raw_ref"] = ref
	src, err := json.Marshal(source)
	if err != nil {
		return nil, errBadSource
	}
	top["source"] = src
	return json.Marshal(top)
}

var errBadSource = errors.New("the compiled intent has no usable source")
