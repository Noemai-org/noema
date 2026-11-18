package intakeadapters

import "errors"

// Errors of the adapters. Callers compare with errors.Is. None is a decision: the intake turns each into UNKNOWN INTERNAL_ERROR (INV-16).
var (
	// ErrPack marks pack data the adapters cannot use: a pack with no policy or no clocks, a document that does not load, two documents that disagree.
	ErrPack = errors.New("intakeadapters: the pack data is not usable")
	// ErrConfig marks an adapter built without a part it needs, or with a value it cannot use.
	ErrConfig = errors.New("intakeadapters: the adapter is not configured")
	// ErrQuery marks a query the adapter refuses before it asks any port: a missing tenant, intent or reading, an admission after the reading.
	ErrQuery = errors.New("intakeadapters: the query is not usable")
	// ErrNoDocument marks a published policy version for which the pack holds no document: what was in force cannot be evaluated, so nothing is evaluated.
	ErrNoDocument = errors.New("intakeadapters: a published policy version has no document")
	// ErrPublications marks a history of publications that cannot be put in order in time (two versions with one time, or a newer version with an earlier time).
	ErrPublications = errors.New("intakeadapters: the publication history is not usable")
	// ErrDrift marks a published policy version whose stored document hash is missing (a row from before the hash existed) or is not the hash of the document the pack carries for that
	// version: what is in force cannot be shown to be what the pack evaluates, so nothing is evaluated (A413; INV-09, INV-21).
	ErrDrift = errors.New("intakeadapters: the published policy document is not the one the pack carries")
	// ErrFacts marks facts the policy cannot be evaluated over: a fact the rules read is missing, undeclared or of another type. An absence is never a negative fact (INV-19).
	ErrFacts = errors.New("intakeadapters: the facts are not usable")
	// ErrAnchor marks an anchor time that is not a time (zero or negative) for an event the source says happened.
	ErrAnchor = errors.New("intakeadapters: the anchor time is not usable")
	// ErrRule marks a winning rule whose parameters cannot give the answer the effect needs (a WAIT with no usable wait_seconds).
	ErrRule = errors.New("intakeadapters: the winning rule cannot be turned into an answer")
)

// adapterError is a failure of the adapters as callers see it (A347, AGENT-13): a fixed public text, with the cause kept for errors.Is and errors.As and never printed. The tenant's ID,
// the identifiers of the pack and of a fact (a clock, a profile, a node, a variable, an entity, a rule, a policy version), the database's message and a dependency's text are not in the
// text; where an identifier helps to debug, it is in the cause. kind, when set, is the package's sentinel that errors.Is finds; cause may be nil.
type adapterError struct {
	kind  error
	text  string
	cause error
}

func (e *adapterError) Error() string { return e.text }

func (e *adapterError) Unwrap() error { return e.cause }

func (e *adapterError) Is(target error) bool { return e.kind != nil && errors.Is(e.kind, target) }

// fail is an error of the adapters: the sentinel's text (or the package's prefix when there is no sentinel), then the fixed reason.
func fail(kind error, reason string, cause error) error {
	text := "intakeadapters: " + reason
	if kind != nil {
		text = kind.Error()
		if reason != "" {
			text += ": " + reason
		}
	}
	return &adapterError{kind: kind, text: text, cause: cause}
}
