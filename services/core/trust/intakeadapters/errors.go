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
	// ErrFacts marks facts the policy cannot be evaluated over: a fact the rules read is missing, undeclared or of another type. An absence is never a negative fact (INV-19).
	ErrFacts = errors.New("intakeadapters: the facts are not usable")
	// ErrAnchor marks an anchor time that is not a time (zero or negative) for an event the source says happened.
	ErrAnchor = errors.New("intakeadapters: the anchor time is not usable")
	// ErrNotBuilt marks a part that is not built yet.
	ErrNotBuilt = errors.New("intakeadapters: not built")
	// ErrRule marks a winning rule whose parameters cannot give the answer the effect needs (a WAIT with no usable wait_seconds).
	ErrRule = errors.New("intakeadapters: the winning rule cannot be turned into an answer")
)
