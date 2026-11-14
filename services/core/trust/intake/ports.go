package intake

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// ErrNotFound is returned by Intents.Get for an intent that does not exist or that the tenant does not own. The two are the same answer (REQ-TRU-003).
var ErrNotFound = errors.New("intake: not found")

// ErrInvalid is returned by Intents.Verify for an admitted intent that no longer passes the gate's own checks (schema, status, signature, hash).
var ErrInvalid = errors.New("intake: invalid")

// Session is what Trust takes from the authenticated session (INV-12): never from the request body, never from the model.
type Session struct {
	TenantID    string
	PrincipalID string
	Role        string // the role of the session (ruling A205, Q5); empty is refused at T6
}

// Request is one thing to decide: a typed request, not a conversation (REQ-TRU-011).
type Request struct {
	Session       Session
	CorrelationID string
	IntentRef     string // `int_<id>@<version>`: the admitted intent and the version the caller means
	// EvidenceSnapshotID is the sealed evidence snapshot T10 to T12 work from, until T9 (not built yet) seals one itself and this field goes.
	EvidenceSnapshotID string
}

// AdmittedIntent is what Trust needs of a stored, admitted Intent Object. The adapter reads it from the intent store and builds it; Trust does not parse the model's text.
type AdmittedIntent struct {
	TenantID      string
	IntentID      string
	Version       int
	IntentNode    string
	IntentHash    string
	CorrelationID string
	PrincipalID   string // the principal the document names: the session's, written by the gate (INV-12)
	View          decide.IntentView
}

// Intents reads admitted intents. Get returns ErrNotFound for an unknown intent and for another tenant's, the same answer for both.
type Intents interface {
	Get(ctx context.Context, tenantID, intentID string, version int) (AdmittedIntent, error)
	// LatestVersion is the newest version of the intent; a request for an older one is refused (T1: latest version).
	LatestVersion(ctx context.Context, tenantID, intentID string) (int, error)
	// Verify repeats the gate's own checks on the stored intent (schema, status, signature, hash) and returns ErrInvalid when one fails.
	Verify(ctx context.Context, a AdmittedIntent) error
}

// Writes answers the two questions of T3: does this intent lead to a write, and are writes enabled for the tenant (the kill switch, and the pack loaded).
type Writes interface {
	NeedsWrite(ctx context.Context, a AdmittedIntent) (bool, error)
	Enabled(ctx context.Context, tenantID string) (bool, error)
}

// Capability is a capability the intent names.
type Capability struct {
	ID         string
	Registered bool
	Prohibited bool
}

// Capabilities answers T4: the capability the intent asks for, if it asks for one (the `requested_remedy` slot), with whether the registry knows it and whether it is
// prohibited. present is false when the intent names none.
type Capabilities interface {
	Requested(ctx context.Context, a AdmittedIntent) (c Capability, present bool, err error)
}

// Scope answers T5: is the intent node in scope for the tenant.
type Scope interface {
	InScope(ctx context.Context, tenantID, intentNode string) (bool, error)
}

// Authority answers T6: may a session with this role act on this intent node (the trust profile's `roles_allowed`).
type Authority interface {
	RoleAllowed(ctx context.Context, intentNode, role string) (bool, error)
}

// Ports are the adapters the intake needs for T1 to T6 and, in ports_t10_t12.go, for T10 to T12. A nil port makes Evaluate refuse to run.
type Ports struct {
	Intents      Intents
	Writes       Writes
	Capabilities Capabilities
	Scope        Scope
	Authority    Authority
	Reconciler   Reconciler
	Policies     Policies
	Clocks       Clocks
}
