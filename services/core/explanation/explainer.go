package explanation

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// The Explainer is the part of EXPLAINING (PERD 28.2) that renders and validates the answer for a recorded decision (EXP-01 slice 4, ruling A155; REQ-EXP-002,
// REQ-AUD-002; INV-13, EC-E16). It renders the deterministic fallback from the pack's templates, runs the PERD 31.2 validator, and appends EXPLANATION_RENDERED or
// EXPLANATION_BLOCKED through a Recorder before it returns anything. It asks a FactsSource for the facts and holds no database, model or network client of its own.
// The composition root adapts it to the orchestrator's Explainer port; this package does not import the orchestrator.

// Errors of the Explainer. Callers compare with errors.Is.
var (
	// ErrNotWired is returned by NewExplainer for a missing part: an explainer that cannot do everything does nothing.
	ErrNotWired = errors.New("explanation: the explainer is not fully wired")
	// ErrNoSubject is returned for a call without a tenant or a principal (INV-12).
	ErrNoSubject = errors.New("explanation: the call has no tenant or no principal")
	// ErrBadDecision is returned for a decision ID that is not a reference.
	ErrBadDecision = errors.New("explanation: the decision id is not a reference")
	// ErrRecord wraps a refused append: nothing is returned after it (the Record comes first, EC-E16).
	ErrRecord = errors.New("explanation: the Record append failed")
)

// Subject is the authenticated caller: the tenant and principal of the verified session, never of a request body (INV-12). It has the fields of orchestrator.Session
// in the same order, so the composition root converts one into the other.
type Subject struct{ TenantID, PrincipalID string }

// FactsSource builds the explanation facts of a recorded decision for the caller's tenant (A155). The real source is the orchestrator's (STM-01); it is the only
// part that reads the Record and the Evidence Layer, so this package reads neither.
type FactsSource interface {
	Facts(ctx context.Context, s Subject, decisionID string) (contracts.ExplanationFactsV2, error)
}

// Event is what the Recorder appends. Rendered is EXPLANATION_RENDERED, otherwise EXPLANATION_BLOCKED. A blocked event carries Rules and never Text.
type Event struct {
	Rendered        bool
	CorrelationID   string
	Text            string
	FactIDs         []string
	TemplateID      string
	TemplateVersion string
	Rules           []string
}

// Recorder appends the event to the Record (as the edge emitter, PERD 32.2). A returned error means the event is not in the Record.
type Recorder interface {
	AppendExplanation(ctx context.Context, s Subject, e Event) error
}

// Explanation is the answer. Blocked is true when no text may be shown; Rules then holds the codes, never text of the explanation.
type Explanation struct {
	Text            string
	Blocked         bool
	Rules           []string
	TemplateID      string
	TemplateVersion string
}

// Config is everything an Explainer needs. Timeout bounds each call; it comes from configuration and is not defaulted.
type Config struct {
	Templates Templates
	Forbidden map[string][]string // the forbidden-claim patterns of each locale that has templates (LoadForbidden)
	Facts     FactsSource
	Recorder  Recorder
	Timeout   time.Duration
}

// Explainer renders, validates and records. It keeps no state between calls.
type Explainer struct{ c Config }

// NewExplainer refuses a Config with a missing part, no template, a template locale without a usable forbidden-claim list, or no timeout.
func NewExplainer(c Config) (*Explainer, error) {
	locales := c.Templates.Locales()
	if c.Facts == nil || c.Recorder == nil || c.Timeout <= 0 || len(locales) == 0 {
		return nil, fmt.Errorf("%w: it needs templates, a facts source, a recorder and a positive timeout", ErrNotWired)
	}
	for _, loc := range locales {
		list := c.Forbidden[loc]
		if len(list) == 0 {
			return nil, fmt.Errorf("%w: locale %s has no forbidden-claim list", ErrNotWired, loc)
		}
		for _, p := range list {
			if _, err := regexp.Compile(p); strings.TrimSpace(p) == "" || err != nil {
				return nil, fmt.Errorf("%w: locale %s has a forbidden pattern that is blank or does not compile", ErrNotWired, loc)
			}
		}
	}
	return &Explainer{c: c}, nil
}

var decisionRe = regexp.MustCompile(`\A[a-z]{2,6}_[0-9A-Za-z_]{2,64}\z`)

// Explain reads the facts of the decision, renders and validates the fallback, records the result, and only then returns it. A refused append returns ErrRecord and no
// text. A template, slot or validator problem is a block, recorded with rule codes; a facts source that cannot answer is an error and records nothing.
func (e *Explainer) Explain(ctx context.Context, s Subject, decisionID string) (Explanation, error) {
	switch {
	case e == nil:
		return Explanation{}, fmt.Errorf("%w: no explainer", ErrNotWired)
	case s.TenantID == "" || s.PrincipalID == "":
		return Explanation{}, ErrNoSubject
	case !decisionRe.MatchString(decisionID):
		return Explanation{}, ErrBadDecision
	}
	ctx, cancel := context.WithTimeout(ctx, e.c.Timeout)
	defer cancel()
	f, err := e.c.Facts.Facts(ctx, s, decisionID)
	if err != nil {
		return Explanation{}, fmt.Errorf("explanation: facts: %w", err)
	}
	if f.CorrelationId == "" {
		return Explanation{}, errors.New("explanation: the facts carry no correlation id")
	}
	corr := string(f.CorrelationId)
	r, err := Fallback(e.c.Templates, f)
	switch {
	case errors.Is(err, ErrNoTemplate):
		return e.block(ctx, s, corr, "NO_TEMPLATE")
	case errors.Is(err, ErrSlot):
		return e.block(ctx, s, corr, "SLOT_UNRESOLVED")
	case err != nil:
		return Explanation{}, err
	}
	forbidden := e.c.Forbidden[string(f.Locale)]
	if len(forbidden) == 0 {
		return e.block(ctx, s, corr, "NO_FORBIDDEN_LIST")
	}
	if problems := Validate(e.c.Templates, r, f, forbidden); len(problems) > 0 {
		return e.block(ctx, s, corr, ruleCodes(problems)...)
	}
	ev := Event{Rendered: true, CorrelationID: corr, Text: r.Text, FactIDs: r.FactIDs, TemplateID: r.TemplateID, TemplateVersion: r.TemplateVersion}
	if err := e.c.Recorder.AppendExplanation(ctx, s, ev); err != nil {
		return Explanation{}, fmt.Errorf("%w: %v", ErrRecord, err)
	}
	return Explanation{Text: r.Text, TemplateID: r.TemplateID, TemplateVersion: r.TemplateVersion}, nil
}

// block records EXPLANATION_BLOCKED with the rule codes and returns a block. No text of the explanation is in the event or the answer.
func (e *Explainer) block(ctx context.Context, s Subject, corr string, rules ...string) (Explanation, error) {
	if err := e.c.Recorder.AppendExplanation(ctx, s, Event{CorrelationID: corr, Rules: rules}); err != nil {
		return Explanation{}, fmt.Errorf("%w: %v", ErrRecord, err)
	}
	return Explanation{Blocked: true, Rules: rules}, nil
}

// ruleCodes turns the validator's problems into the sorted, de-duplicated codes of the blocked event. The codes name the check of PERD 31.2 that failed, never the text.
func ruleCodes(problems []string) []string {
	set := map[string]bool{}
	for _, p := range problems {
		set[ruleOf(p)] = true
	}
	out := make([]string, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

func ruleOf(problem string) string {
	switch {
	case strings.HasPrefix(problem, "check 1"):
		return "UNCOVERED_SENTENCE"
	case strings.HasPrefix(problem, "check 2: the word"):
		return "UNSUPPORTED_NAME"
	case strings.HasPrefix(problem, "check 2: the text holds a brace"):
		return "UNFILLED_PLACEHOLDER"
	case strings.HasPrefix(problem, "check 2"):
		return "UNSUPPORTED_FIGURE"
	case strings.HasPrefix(problem, "check 3"):
		return "FORBIDDEN_CLAIM"
	case strings.HasPrefix(problem, "check 4"):
		return "UNMASKED_DIGITS"
	case strings.HasPrefix(problem, "claim"):
		return "BAD_CLAIM"
	case strings.HasPrefix(problem, "input"):
		return "AMBIGUOUS_FACTS"
	}
	return "VALIDATION_FAILED"
}
