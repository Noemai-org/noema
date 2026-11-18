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
	// EvidenceSnapshotID and EvidenceUnavailable are the orchestrator's EvidenceRef: the sealed snapshot the Evidence Layer built for the intent, and whether a source could not
	// be read. T9 decides what they mean (and records the evidence read from the snapshot); T10 to T12 work from the same snapshot; nothing before T9 looks at them.
	EvidenceSnapshotID  string
	EvidenceUnavailable bool
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
	// TargetRef is the target the stored intent names, resolved by the adapter from the intent store (never from the request): the target T8 checks the principal owns and the
	// only target a plan step may act on. Empty for an intent that names none.
	TargetRef string
	View      decide.IntentView
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
	// RoleAllowed is the registry's answer for the role of the session (A255): whether a session with that role may use this capability (the capability descriptor's roles).
	// Check fills it; Requested does not need it (T6 already checked the role for the intent node).
	RoleAllowed bool
}

// Capabilities answers T4: the capability the intent asks for, if it asks for one (the `requested_remedy` slot), with whether the registry knows it and whether it is
// prohibited. present is false when the intent names none.
type Capabilities interface {
	Requested(ctx context.Context, a AdmittedIntent) (c Capability, present bool, err error)
	// Check is the registry's answer for one capability ID and the role of the session: the capabilities of a plan's steps are checked here at T14, one by one, not only the
	// one the intent names, and each for the role (A255, the safer reading: the role of the session is never assumed to be allowed).
	Check(ctx context.Context, capabilityID, role string) (Capability, error)
	// Authorized is the set of capability IDs the intent and the tenant's policy authorize for this intent node, as pack data (the capabilities of the pack's action graph the
	// intent's capability roots, reads included; a pack names them, the core holds no capability ID, INV-14). The intake adds the intent's own capability and reads the set once per
	// evaluation, at T11 for a write. For a write it is what the evaluation checks, each capability of it: T11 asks the tenant's policy about every one (A306: a capability the
	// policy denies for the session's role ends the evaluation, whichever step of the graph uses it), T12 asks the dispatch budget of every one (Codex P2 on noema#572; the Dispatches
	// adapter must answer Limit >= 1 for a read capability, a Limit of 0 is UNKNOWN), and T14 refuses (PLAN_INVALID) a plan step outside it (Codex P1) or one the registry and the
	// role do not allow. For a read (T3: no write) T11 asks the policy about the intent's own capability only, as before. An error is never "none": the evaluation ends in UNKNOWN.
	Authorized(ctx context.Context, a AdmittedIntent) ([]string, error)
}

// Scope answers T5: is the intent node in scope for the tenant.
type Scope interface {
	InScope(ctx context.Context, tenantID, intentNode string) (bool, error)
}

// Authority answers T6: may a session with this role act on this intent node (the trust profile's `roles_allowed`).
type Authority interface {
	RoleAllowed(ctx context.Context, intentNode, role string) (bool, error)
}

// Ports are the adapters the intake needs: ports.go for T1 to T6, ports_t7_t9.go (Candidates, Ownership and Evidence, the only ports that read about a target, held only by
// T7, T8 and T9), ports_t10_t12.go and ports_t13_t15.go (Confirmations, Planner and Recorder). A nil port makes Evaluate refuse to run.
type Ports struct {
	Intents       Intents
	Writes        Writes
	Capabilities  Capabilities
	Scope         Scope
	Authority     Authority
	Candidates    Candidates
	Ownership     Ownership
	Evidence      Evidence
	Reconciler    Reconciler
	Policies      Policies
	Clocks        Clocks
	Confirmations Confirmations
	Planner       Planner
	Recorder      Recorder
	Dispatches    Dispatches
}
