// Package interpreterport is the orchestrator's Interpreter port over the core's Interpreter (UND-04; ruling A262 on noema#565; REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-API-001;
// INV-02, INV-12, INV-21; AGENT-09, AGENT-10).
//
// It builds the canonical input (canonical-input.v2) from the verified session and the admitted turn, asks the core's Interpreter (compile, gate, record, save), and maps the answer to the three
// kinds the orchestrator knows. The identity in the canonical input is the verified session's and never the text's. The orchestrator gets an opaque reference to the admitted intent, never its content.
//
// May import: the standard library, orchestrator, platform/clock, understanding and its interpreter. Nothing under evidence/, trust/ or outcome/ imports this package (the powers stay apart).
//
// Must never: take an identity from the text, read a clock but the one it is given (INV-21), normalise or rewrite the person's words beyond trimming, pass a proposal's content or an utterance into an
// error text (AGENT-13), or hold a number the pack or the contract owns (the 2000-character bound is canonical-input.v2's; the required slots are the pack's).
package interpreterport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

const prefix = "interpreterport: "

// maxTextChars is canonical-input.v2's bound on `text` (1 to 2000 characters).
const maxTextChars = 2000

var (
	// ErrNotUsable marks a configuration that cannot run: a missing part or a malformed pack reference.
	ErrNotUsable = errors.New(prefix + "the port is not usable")
	// ErrTurn marks a turn the port refuses before it calls anything: an empty or over-long text, a request id that is not a lower-case UUID, a correlation id off its shape.
	ErrTurn = errors.New(prefix + "the turn cannot be interpreted")
	// ErrSession marks a resolved session that is not the verified session's (another principal or tenant) or is not valid.
	ErrSession = errors.New(prefix + "the resolved session is not the verified session")
	// ErrPack marks pack data the port cannot trust, or an admitted node the pack does not describe.
	ErrPack = errors.New(prefix + "the pack does not describe the intent")
)

var (
	uuidRe    = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	corrRe    = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	packRefRe = regexp.MustCompile(`^[a-z][a-z0-9]*@[0-9]+\.[0-9]+\.[0-9]+$`)
	kindRe    = regexp.MustCompile(`^[a-z]+$`)
)

// Resolved is what the composition root knows about the verified session that the orchestrator's Session does not carry: the whole understanding.Session (its authentication reference, principal
// type and who asserted it) and the kind of actor the Record writes (`customer`), both from the verified session (A262, points 1 and 3).
type Resolved struct {
	Session   understanding.Session
	ActorKind string
}

// Sessions resolves the orchestrator's Session to the Interpreter's. The composition root implements it from the verified session (the edge's session.Principal); the port never invents a field.
type Sessions interface {
	Resolve(ctx context.Context, s orchestrator.Session) (Resolved, error)
}

// Interpreter is the core's Interpreter (understanding/interpreter.Interpreter implements it).
type Interpreter interface {
	Interpret(ctx context.Context, in interpreter.Input) (interpreter.Result, error)
}

// Required says which slots of an intent node the pack requires. RequiredFromPack reads it from the pack.
type Required interface {
	RequiredSlots(node string) ([]string, bool)
}

// Config is what New needs. Every part is required; there are no defaults.
type Config struct {
	Sessions    Sessions
	Interpreter Interpreter
	Clock       clock.Clock
	PackRef     string // `<vertical>@<x.y.z>`
	Required    Required
}

// Port implements orchestrator.Interpreter.
type Port struct{ c Config }

var _ orchestrator.Interpreter = (*Port)(nil)

// New refuses a configuration with a missing part or a malformed pack reference.
func New(c Config) (*Port, error) {
	if c.Sessions == nil || c.Interpreter == nil || c.Clock == nil || c.Required == nil || !packRefRe.MatchString(c.PackRef) {
		return nil, ErrNotUsable
	}
	return &Port{c: c}, nil
}

// canonicalInput is canonical-input.v2 as this port writes it: every required member, no optional one.
type canonicalInput struct {
	SchemaVersion string `json:"schema_version"`
	RequestID     string `json:"request_id"`
	CorrelationID string `json:"correlation_id"`
	TenantID      string `json:"tenant_id"`
	SessionID     string `json:"session_id"`
	PrincipalRef  string `json:"principal_ref"`
	Channel       string `json:"channel"`
	Mode          string `json:"mode"`
	Text          string `json:"text"`
	ReceivedAt    string `json:"received_at"`
}

// Interpret compiles one admitted turn. Order: the turn is checked, the session resolved and checked against the verified one, the canonical input built, the Interpreter asked; then the answer is
// mapped. A refusal at the gate is GATE_FAILED with no error (the Interpreter recorded it first); every other failure is returned as the Interpreter gave it and stops the runner.
func (p *Port) Interpret(ctx context.Context, s orchestrator.Session, t orchestrator.Turn) (orchestrator.Interpretation, error) {
	text := strings.TrimSpace(t.Text)
	if n := utf8.RuneCountInString(text); n < 1 || n > maxTextChars || !utf8.ValidString(text) || !uuidRe.MatchString(t.RequestID) || !corrRe.MatchString(t.CorrelationID) {
		return orchestrator.Interpretation{}, ErrTurn
	}
	r, err := p.c.Sessions.Resolve(ctx, s)
	if err != nil {
		return orchestrator.Interpretation{}, err
	}
	if !r.Session.Valid() || r.Session.TenantID != s.TenantID || r.Session.PrincipalID != s.PrincipalID || !kindRe.MatchString(r.ActorKind) {
		return orchestrator.Interpretation{}, ErrSession
	}
	ci, err := json.Marshal(canonicalInput{SchemaVersion: "2.0", RequestID: t.RequestID, CorrelationID: t.CorrelationID, TenantID: r.Session.TenantID, SessionID: r.Session.AuthnRef,
		PrincipalRef: r.Session.PrincipalID, Channel: "text", Mode: "MODE_1", Text: text, ReceivedAt: p.c.Clock.Now().UTC().Format("2006-01-02T15:04:05Z")})
	if err != nil {
		return orchestrator.Interpretation{}, ErrTurn
	}
	res, err := p.c.Interpreter.Interpret(ctx, interpreter.Input{Session: r.Session, Actor: r.ActorKind + ":" + r.Session.PrincipalID, CorrelationID: t.CorrelationID,
		Request: understanding.CompileRequest{CanonicalInput: ci, PackRef: p.c.PackRef}})
	if err != nil {
		var rej *understanding.Rejection
		if errors.As(err, &rej) {
			return orchestrator.Interpretation{Kind: orchestrator.GateFailed}, nil
		}
		return orchestrator.Interpretation{}, err
	}
	o := res.Intent.Object()
	ref := string(o.IntentId) + "@" + strconv.Itoa(int(o.Version))
	required, ok := p.c.Required.RequiredSlots(string(o.IntentNode))
	if !ok {
		return orchestrator.Interpretation{}, ErrPack
	}
	if len(o.AmbiguityFlags) > 0 || !satisfied(o.Slots, required) {
		return orchestrator.Interpretation{Kind: orchestrator.NeedsClarification, IntentRef: ref}, nil
	}
	return orchestrator.Interpretation{Kind: orchestrator.Admitted, IntentRef: ref}, nil
}

// satisfied: every required slot is present and, if it carries a status, that status is RESOLVED. A slot of a type with no status (a Reference) is as resolved as it is present.
func satisfied(slots map[string]contracts.IntentSlotV2Slot, required []string) bool {
	for _, name := range required {
		raw, ok := slots[name]
		if !ok {
			return false
		}
		var head struct {
			Status *string `json:"status"`
		}
		if json.Unmarshal(raw, &head) != nil || (head.Status != nil && *head.Status != "RESOLVED") {
			return false
		}
	}
	return true
}

type packNodes struct {
	IntentNodes []struct {
		NodeID string `json:"node_id"`
		Slots  []struct {
			Name     string `json:"name"`
			Required bool   `json:"required"`
		} `json:"slots"`
	} `json:"intent_nodes"`
}

type fromPack map[string][]string

// RequiredFromPack reads each intent node's required slots from the pack's data (A262 point 4: the required-slot set comes from the pack, never from a literal here). It refuses data it cannot
// trust: not a JSON object, data after it, no nodes, a node or a slot with no name, a repeated node or slot, a `required` that is not a boolean. The names are sorted.
func RequiredFromPack(raw []byte) (Required, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var f packNodes
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("%w: the pack is not a JSON object", ErrPack)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: the pack has data after the object", ErrPack)
	}
	if len(f.IntentNodes) == 0 {
		return nil, fmt.Errorf("%w: the pack has no intent nodes", ErrPack)
	}
	out := fromPack{}
	for _, n := range f.IntentNodes {
		if n.NodeID == "" {
			return nil, fmt.Errorf("%w: an intent node has no id", ErrPack)
		}
		if _, dup := out[n.NodeID]; dup {
			return nil, fmt.Errorf("%w: an intent node is repeated", ErrPack)
		}
		seen := map[string]bool{}
		req := []string{}
		for _, sl := range n.Slots {
			if sl.Name == "" || seen[sl.Name] {
				return nil, fmt.Errorf("%w: a slot has no name or is repeated", ErrPack)
			}
			seen[sl.Name] = true
			if sl.Required {
				req = append(req, sl.Name)
			}
		}
		sort.Strings(req)
		out[n.NodeID] = req
	}
	return out, nil
}

// RequiredSlots returns a copy of the node's required slot names; false for a node the pack does not describe.
func (f fromPack) RequiredSlots(node string) ([]string, bool) {
	r, ok := f[node]
	return append([]string(nil), r...), ok
}
