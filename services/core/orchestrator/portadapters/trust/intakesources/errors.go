package intakesources

import "errors"

// Errors of the readers. Callers compare with errors.Is. None is a decision: the intake turns each into UNKNOWN INTERNAL_ERROR (INV-16).
var (
	// ErrConfig marks a reader built without a part it needs, or with a timeout below one (AGENT-16), and a trust limits document that was not loaded (AtRiskFromLimits).
	ErrConfig = errors.New("intakesources: the reader is not configured")
	// ErrQuery marks a query the reader refuses before it asks the database: a tenant, an intent or a version outside its shape, or no intent node.
	ErrQuery = errors.New("intakesources: the query is not usable")
	// ErrNoDeadline marks a call whose context has no deadline.
	ErrNoDeadline = errors.New("intakesources: the context needs a deadline")
	// ErrNotFound is the answer for an intent version that does not exist for the tenant and for one that another tenant owns: the same answer, in value and in text.
	ErrNotFound = errors.New("intakesources: no such intent version for this tenant")
	// ErrStore marks a database that could not answer (no login, a driver error, a deadline). The cause is reachable with errors.Is and is never printed.
	ErrStore = errors.New("intakesources: the database could not answer")
	// ErrBadAnswer marks an answer outside the contract of the column read (an admission time before 1 Unix second).
	ErrBadAnswer = errors.New("intakesources: the database gave an answer outside its contract")
	// ErrNoSource marks a fact no reader here can give: an evidence entity, or a variable that no slot of the stored intent carries. The fact is named after the error text.
	ErrNoSource = errors.New("intakesources: no source gives this fact")
	// ErrIntent marks a stored intent that cannot give a fact it does name: a slot without a status, a status outside the contract, a resolved reference on a slot that is not
	// RESOLVED, an intent node other than the query's.
	ErrIntent = errors.New("intakesources: the stored intent cannot give this fact")
	// ErrNoSnapshot is the answer for a snapshot that does not exist for the tenant, that is not sealed, and that another tenant owns: the same answer, in value and in text.
	ErrNoSnapshot = errors.New("intakesources: no such sealed snapshot for this tenant")
	// ErrSnapshotIntegrity marks a stored snapshot that is not exactly what was sealed: an item missing, repeated, of another case, or a set hash that does not verify (INV-17).
	ErrSnapshotIntegrity = errors.New("intakesources: the stored snapshot is not what was sealed")
	// ErrListItem marks a sealed snapshot that has no usable item for a list: none, more than one, or one whose fact status is not KNOWN. Never "no such row" (INV-19).
	ErrListItem = errors.New("intakesources: the sealed snapshot has no usable item for this list")
	// ErrNoPayload is the answer for a payload that does not exist for the tenant and for one that another tenant owns: the same answer, in value and in text.
	ErrNoPayload = errors.New("intakesources: no such payload for this tenant")
	// ErrPayloadTooLarge marks a payload over the configured bound of the store.
	ErrPayloadTooLarge = errors.New("intakesources: the payload is larger than the configured bound")
	// ErrListPayload marks an item whose stored payload cannot be had as the one the snapshot sealed (unreadable, or its hash is not the item's).
	ErrListPayload = errors.New("intakesources: the item's payload cannot be used as the sealed one")
	// ErrList marks a payload the evidence mapping cannot read as the list: not marked complete, not the shape the mapping names, no such list. The mapping's own error is the cause.
	ErrList = errors.New("intakesources: the list cannot be read from the payload")
)

// failure is an error with a fixed text and a cause that errors.Is can still find: a database or login error can name a server, a login, a tenant or a row, so its text is never
// shown (AGENT-13).
type failure struct{ kind, cause error }

func (f *failure) Error() string { return f.kind.Error() }

func (f *failure) Unwrap() []error {
	if f.cause == nil {
		return []error{f.kind}
	}
	return []error{f.kind, f.cause}
}

func wrapped(kind, cause error) error { return &failure{kind: kind, cause: cause} }
