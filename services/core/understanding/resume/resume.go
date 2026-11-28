// Package resume resolves the open investigation a follow-up turn is bound to (INP-01 slice 2; REQ-INP-004; A320, A404, A712; INV-12). This file is the stub of the red commit: it names the types and
// answers nothing yet.
package resume

import (
	"context"
	"errors"

	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

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

// States says whether an investigation has ended.
type States interface {
	Terminal(ctx context.Context, tenantID, correlationID string) (bool, error)
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

// Open is not built yet.
func (r Resolver) Open(ctx context.Context, s understanding.Session, actor, conversationRef string) (intentstore.Admitted, error) {
	return intentstore.Admitted{}, nil
}
