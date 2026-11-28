// Package resume resolves the open investigation a follow-up turn is bound to (INP-01 slice 2; REQ-INP-004; A320, A404, A712; EC-C03; INV-12). It decides nothing about the follow-up itself: it
// finds the prior the Interpreter is given (Input.Prior) and answers the same way for every reference that is not the session's own.
package resume

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

var errIncomplete = errors.New("resume: the resolver needs a reader, a state source and a place to record a denial")

var (
	// ErrNotFound is the one answer for a conversation that is not this session's open investigation, whatever the cause.
	ErrNotFound = errors.New("resume: no investigation for this conversation")
	// ErrTerminal is a follow-up on an investigation that has ended: the customer is asked for a new request (A320, A404).
	ErrTerminal = errors.New("resume: the investigation has ended")
	// ErrRead is a store that could not answer.
	ErrRead = errors.New("resume: the investigation could not be read")
	// ErrRecord is a security event that could not be recorded.
	ErrRecord = errors.New("resume: the security event could not be recorded")
)

// Reader reads the latest admitted intent of an investigation as the tenant (intentstore.Reader implements it).
type Reader interface {
	Latest(ctx context.Context, correlationID string) (intentstore.Admitted, error)
}

// States says whether an investigation has ended. The tenant and the principal are the verified session's (INV-12): the adapter reads the investigation of that tenant and never selects a row by the
// principal.
type States interface {
	Terminal(ctx context.Context, tenantID, principalID, correlationID string) (bool, error)
}

// Denials records a follow-up that named another principal's investigation (SECURITY_EVENT, REQ-INP-004).
type Denials interface {
	Denied(ctx context.Context, tenantID, actor, correlationID string) error
}

// Resolver resolves a conversation reference to the prior of a follow-up.
type Resolver struct {
	Reader  Reader
	States  States
	Denials Denials
}

// investigationRe is the conversation reference of A234: `inv_` and the ULID of the investigation's first correlation id.
var investigationRe = regexp.MustCompile(`\Ainv_([0-9A-Za-z]{2,32})\z`)

// Open resolves a conversation reference to the latest admitted intent of the investigation it names, for the session that holds it. In order: an incomplete resolver is a configuration error and
// nothing is read; a session that is not valid, or a reference that is not an investigation id, is ErrNotFound with no read; the latest intent of `corr_<id>` is read as the session's tenant (a
// row of another tenant, an unknown chain and a chain other than the one asked all end as ErrNotFound); the stored document must name the session's own principal, else a SECURITY_EVENT is recorded
// and the answer is the same ErrNotFound (a record that cannot be written is named beside it, never instead of it); only then is the state asked, and an ended investigation is ErrTerminal. The
// session's principal and tenant come from the verified session alone (INV-12).
func (r Resolver) Open(ctx context.Context, s understanding.Session, actor, conversationRef string) (intentstore.Admitted, error) {
	if r.Reader == nil || r.States == nil || r.Denials == nil {
		return intentstore.Admitted{}, errIncomplete
	}
	m := investigationRe.FindStringSubmatch(conversationRef)
	if m == nil || !s.Valid() {
		return intentstore.Admitted{}, ErrNotFound
	}
	chain := "corr_" + m[1]
	prior, err := r.Reader.Latest(ctx, chain)
	if errors.Is(err, intentstore.ErrNotFound) {
		return intentstore.Admitted{}, ErrNotFound
	}
	if err != nil {
		return intentstore.Admitted{}, fmt.Errorf("%w: %w", ErrRead, err)
	}
	if prior.TenantID != s.TenantID || prior.CorrelationID != chain {
		return intentstore.Admitted{}, ErrNotFound
	}
	if principalOf(prior.JSON) != s.PrincipalID {
		if err := r.Denials.Denied(ctx, s.TenantID, actor, chain); err != nil {
			return intentstore.Admitted{}, fmt.Errorf("%w: %w: %w", ErrNotFound, ErrRecord, err)
		}
		return intentstore.Admitted{}, ErrNotFound
	}
	ended, err := r.States.Terminal(ctx, s.TenantID, s.PrincipalID, chain)
	if err != nil {
		return intentstore.Admitted{}, fmt.Errorf("%w: %w", ErrRead, err)
	}
	if ended {
		return intentstore.Admitted{}, ErrTerminal
	}
	return prior, nil
}

// principalOf is the principal the stored document names, or "" when it names none.
func principalOf(doc []byte) string {
	var d struct {
		Principal struct {
			PrincipalID string `json:"principal_id"`
		} `json:"principal"`
	}
	if json.Unmarshal(doc, &d) != nil {
		return ""
	}
	return d.Principal.PrincipalID
}
