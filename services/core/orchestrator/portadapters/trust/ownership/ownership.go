// Package ownershipport is the Trust intake's Ownership (T8; TRU-07; rulings A329 and A344; REQ-TRU-002, REQ-TRU-003; INV-11, INV-12, INV-19; AGENT-13, AGENT-16; A347). It implements
// intake.Ownership: T8 asks whether the session's principal owns the target the stored intent names, and the adapter answers from the principal-scoped read of the enterprise read port
// (candidates-request.v1 in, candidates-response.v1 out; REQ-CTX-006) and from nothing else.
//
// The read is made as the session's tenant and principal for the pack's resource kind over the principal's whole history. The contract cannot name one resource, so the target is looked
// for in the rows the principal's scope returns: found there it is owned; an answer that is complete and lacks it is "not owned"; an answer that may have been cut off at the contract's
// most is neither, and is an error (INV-19). Another principal's resource, an absent resource, another tenant's resource and a reference that is not a reference are one answer, false
// with no error, and the reader is never told which target is asked about (REQ-TRU-003). A read that fails, a context that has expired or an answer outside the contract is an error,
// never false (INV-19). Nothing is inferred from any enterprise response (A344).
//
// Every error has fixed text and carries no row, no ID, no tenant and no word of the cause; the cause stays reachable through errors.Is and errors.As (AGENT-13, A347). The adapter is one
// tenant's (A320: one instance per tenant). It holds no number and no default: the deadline is the caller's, and the pack's data comes in through Config (INV-14, AGENT-16).
//
// May import: the standard library, internal/contracts and trust/intake.
//
// Must never: write, call a model, take the tenant or the principal from anything but the session, read for a tenant it was not built for, answer "not owned" for a read that did not
// finish, return intake.ErrNotFound (T8 turns it into "not owned"), or put a row, an ID, a tenant or the text of a cause into an error.
package ownershipport

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const prefix = "ownershipport: "

// Errors of the adapter. Callers compare with errors.Is. Every text is fixed.
var (
	// ErrConfig is returned for a missing part, a tenant off its shape, or pack data that is not usable, and by the zero value.
	ErrConfig = errors.New(prefix + "the adapter is not configured")
	// ErrNoDeadline is returned for a context with no deadline. Nothing is read (AGENT-16).
	ErrNoDeadline = errors.New(prefix + "the context needs a deadline")
	// ErrSession is returned for a tenant or a principal that is not of the contract's shape.
	ErrSession = errors.New(prefix + "the session is not usable")
	// ErrTenant is returned for a session of another tenant than the adapter's (INV-11).
	ErrTenant = errors.New(prefix + "the session is not of this adapter's tenant")
	// ErrIntent is returned for an intent that is not the session's: another tenant's or principal's, or one the adapter cannot use (INV-12).
	ErrIntent = errors.New(prefix + "the intent is not usable for this session")
	// ErrRead is returned when the read fails or the context ends. It is never "not owned" (INV-19).
	ErrRead = errors.New(prefix + "the ownership could not be read")
	// ErrBadAnswer is returned for an answer outside its contract or not for this request. None of it is used.
	ErrBadAnswer = errors.New(prefix + "the answer is outside its contract")
	// ErrInconclusive is returned when the answer is as long as the contract allows and lacks the target: the target may be among the rows that were cut off (INV-19).
	ErrInconclusive = errors.New(prefix + "the answer cannot show whether the target is owned")
)

// opError is a failure whose text is its kind's alone: the cause stays reachable for errors.Is and errors.As and is never part of Error().
type opError struct{ kind, cause error }

func (e *opError) Error() string { return e.kind.Error() }
func (e *opError) Unwrap() []error {
	if e.cause == nil {
		return []error{e.kind}
	}
	return []error{e.kind, e.cause}
}

func refuse(kind, cause error) error { return &opError{kind: kind, cause: cause} }

// ScopedReader is the principal-scoped enterprise read (REQ-CTX-006), in its contract's own types. It is the same shape as the Candidates adapter's reader; the composition connects
// both to one read port. This package never reaches an enterprise system.
type ScopedReader interface {
	Read(ctx context.Context, req contracts.CandidatesRequestV1) (contracts.CandidatesResponseV1, error)
}

// Node is the pack's data for one intent node whose capability needs `principal_must_own`: the `<vertical>.<kind>` of the resource that is owned.
type Node struct {
	ResourceKind string
}

// Config is everything the adapter needs. None of it has a default.
type Config struct {
	TenantID string
	Reader   ScopedReader
	// Nodes is the pack's ownership data by intent node ID. An intent node that is not in it has no ownership slot.
	Nodes map[string]Node
}

// Ownership implements intake.Ownership for one tenant. The zero value refuses every call.
type Ownership struct {
	tenantID string
	reader   ScopedReader
	nodes    map[string]Node
	ready    bool
}

var _ intake.Ownership = Ownership{}

const (
	// schemaVersion is the version of both candidate contracts. windowFrom and windowTo are the earliest and the latest instants of common.v1 Timestamp: the whole history.
	schemaVersion = "1.0"
	windowFrom    = "0001-01-01T00:00:00Z"
	windowTo      = "9999-12-31T23:59:59Z"
	// maxRows is the maxItems of candidates-response.v1; maxMinor is the maximum of money.v1 MinorAmount; maxDisplayRunes is the maxLength of counterparty_display; maxDigitRun is
	// the longest run of digits its pattern allows.
	maxRows         = 20
	maxMinor        = 9007199254740991
	maxDisplayRunes = 64
	maxDigitRun     = 4
)

var (
	tenantShape    = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)
	principalShape = regexp.MustCompile(`\Ausr_[0-9A-Za-z]{2,32}\z`)
	corrShape      = regexp.MustCompile(`\Acorr_[0-9A-Za-z]{2,32}\z`)
	candIDShape    = regexp.MustCompile(`\Acand_[0-9A-Za-z]{2,32}\z`)
	kindShape      = regexp.MustCompile(`\A[a-z][a-z0-9]*\.[a-z][a-z0-9_]*\z`)
	refIDShape     = regexp.MustCompile(`\A[A-Za-z0-9_.-]{1,64}\z`)
	last4Shape     = regexp.MustCompile(`\A[0-9]{4}\z`)
	// versionShape is the answer's schema version: this adapter implements major 1, so a later minor of 1 is accepted (additive by the contract's rule) and any other major is refused.
	versionShape = regexp.MustCompile(`\A1\.[0-9]+\z`)
	instantShape = regexp.MustCompile(`\A[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z\z`)
)

// New builds the adapter. A missing part, a tenant off its shape, no node, a node with no name or with a resource kind off its shape is ErrConfig. The pack's data is copied: a later
// edit by the caller changes nothing here.
func New(cfg Config) (Ownership, error) {
	if !tenantShape.MatchString(cfg.TenantID) || cfg.Reader == nil || len(cfg.Nodes) == 0 {
		return Ownership{}, ErrConfig
	}
	nodes := make(map[string]Node, len(cfg.Nodes))
	for id, n := range cfg.Nodes {
		if id == "" || !kindShape.MatchString(n.ResourceKind) {
			return Ownership{}, ErrConfig
		}
		nodes[id] = n
	}
	return Ownership{tenantID: cfg.TenantID, reader: cfg.Reader, nodes: nodes, ready: true}, nil
}

// OwnsTarget is the T8 read. Nothing is read until the context has a deadline, the tenant and the principal are of the contract's shape, the tenant is the adapter's, and the intent
// is the session's. An intent whose node has no ownership slot is owned. For one that has, a target that is not a reference of the node's kind is not owned, with no read.
func (o Ownership) OwnsTarget(ctx context.Context, tenantID, principalID string, a intake.AdmittedIntent) (bool, error) {
	if !o.ready {
		return false, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return false, ErrNoDeadline
	}
	if !tenantShape.MatchString(tenantID) || !principalShape.MatchString(principalID) {
		return false, ErrSession
	}
	if tenantID != o.tenantID {
		return false, ErrTenant
	}
	if a.TenantID != tenantID || a.PrincipalID != principalID || a.IntentID == "" || a.Version < 1 || !corrShape.MatchString(a.CorrelationID) {
		return false, refuse(ErrIntent, nil)
	}
	node, ok := o.nodes[a.IntentNode]
	if !ok {
		return true, nil
	}
	if !isReference(a.TargetRef, node.ResourceKind) {
		return false, nil
	}
	req := contracts.CandidatesRequestV1{
		SchemaVersion: schemaVersion, TenantId: contracts.CommonV1TenantId(tenantID), CorrelationId: contracts.CommonV1CorrelationId(a.CorrelationID),
		PrincipalRef: contracts.CommonV1PrincipalId(principalID), ResourceKind: node.ResourceKind,
		Filters: contracts.CandidatesRequestV1Filters{TimeWindow: contracts.CandidatesRequestV1FiltersTimeWindow{From: windowFrom, To: windowTo}},
	}
	resp, err := o.reader.Read(ctx, req)
	if err != nil {
		return false, refuse(ErrRead, err)
	}
	if !answerOK(resp, req) {
		return false, refuse(ErrBadAnswer, nil)
	}
	for _, c := range resp.Candidates {
		if string(c.ResourceRef) == a.TargetRef {
			return true, nil
		}
	}
	if len(resp.Candidates) >= maxRows {
		return false, refuse(ErrInconclusive, nil)
	}
	return false, nil
}

// isReference says whether ref is `<kind>:<id>` for the given kind, with an id of the contract's shape.
func isReference(ref, kind string) bool {
	id, found := strings.CutPrefix(ref, kind+":")
	return found && refIDShape.MatchString(id)
}

// answerOK holds the answer to the contract and to the request before any of it is used: for this tenant and this correlation, within the contract's most, and each row of the contract's
// shape and of the request's resource kind, none twice. One row that fails refuses the whole answer. The reader has scoped its rows to the principal; the response has no member that
// could prove it again, so the request is where the scope is held (REQ-CTX-006).
func answerOK(r contracts.CandidatesResponseV1, req contracts.CandidatesRequestV1) bool {
	if !versionShape.MatchString(string(r.SchemaVersion)) || r.TenantId != req.TenantId || r.CorrelationId != req.CorrelationId || len(r.Candidates) > maxRows {
		return false
	}
	ids := make(map[string]bool, len(r.Candidates))
	refs := make(map[string]bool, len(r.Candidates))
	for _, c := range r.Candidates {
		if ids[c.CandidateId] || refs[string(c.ResourceRef)] || !rowOK(c, req.ResourceKind) {
			return false
		}
		ids[c.CandidateId], refs[string(c.ResourceRef)] = true, true
	}
	return true
}

func rowOK(c contracts.CandidatesResponseV1Candidate, kind string) bool {
	var cur contracts.MoneyV1CurrencyCode
	var dir contracts.CandidatesResponseV1CandidateDirection
	runes := utf8.RuneCountInString(c.CounterpartyDisplay)
	return candIDShape.MatchString(c.CandidateId) && isReference(string(c.ResourceRef), kind) && c.AmountMinor >= 0 && c.AmountMinor <= maxMinor &&
		json.Unmarshal([]byte(strconv.Quote(string(c.Currency))), &cur) == nil && instantShape.MatchString(string(c.OccurredAt)) &&
		runes >= 1 && runes <= maxDisplayRunes && !strings.Contains(c.CounterpartyDisplay, "\n") && digitRun(c.CounterpartyDisplay) <= maxDigitRun &&
		last4Shape.MatchString(c.AccountLast4) && json.Unmarshal([]byte(strconv.Quote(string(c.Direction))), &dir) == nil
}

// digitRun is the longest run of consecutive digits in s.
func digitRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}
