package recorder

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	prefix  = "explanation recorder: "
	emitter = "core-edge"

	eventRendered = "EXPLANATION_RENDERED"
	eventBlocked  = "EXPLANATION_BLOCKED"

	// maxClaimText is the longest claim text the reconstruction serves (reconstruction.v3.json; record/reconstruct refuses a longer one and so would refuse the whole
	// correlation). maxDigitRun is the longest unmasked digit run on any outward surface (ruling A154).
	maxClaimText = 300
	maxDigitRun  = 4
)

var (
	correlationRe = regexp.MustCompile(`\Acorr_[0-9A-Za-z]{2,32}\z`)
	factIDRe      = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9_.-]{0,63}\z`)
	ruleRe        = regexp.MustCompile(`\A[A-Z][A-Z0-9_]{0,63}\z`)
	templateIDRe  = regexp.MustCompile(`\A[A-Za-z][A-Za-z0-9-]*/[a-z][a-z0-9._]*/[A-Z][A-Z0-9_]*\z`)
	versionRe     = regexp.MustCompile(`\Asha256:[0-9a-f]{64}\z`)
	digitRun      = regexp.MustCompile(`\p{Nd}+`)
)

// Recorder appends the Explainer's events. Its Pool must log in as the tenant's own core-edge emitter login: the database, not this code, binds the event to the
// tenant and to that emitter. Each event goes in its own transaction, so a failed append stores nothing.
type Recorder struct {
	Pool     *pgxpool.Pool
	Appender *chain.Appender
	Timeout  time.Duration // from configuration; each append has its own deadline
}

// AppendExplanation appends one event. A malformed event, one the reconstruction could not serve, or one whose append the Record refuses, is an error and stores
// nothing. The actor is the emitter's own identity: no authenticated person caused the event (the person asked a question; the system rendered the answer).
func (r *Recorder) AppendExplanation(ctx context.Context, s explanation.Subject, e explanation.Event) error {
	if r == nil || r.Pool == nil || r.Appender == nil || r.Timeout <= 0 {
		return errors.New(prefix + "the recorder needs a pool, an appender and a positive timeout")
	}
	if s.TenantID == "" || s.PrincipalID == "" {
		return errors.New(prefix + "the subject has no tenant or no principal")
	}
	ev, err := build(s, e)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("%sbegin: %w", prefix, err)
	}
	if _, err := r.Appender.Append(ctx, tx, ev); err != nil {
		rb, stop := context.WithTimeout(context.Background(), r.Timeout)
		defer stop()
		_ = tx.Rollback(rb)
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("%scommit: %w", prefix, err)
	}
	return nil
}

func build(s explanation.Subject, e explanation.Event) (chain.Event, error) {
	if !correlationRe.MatchString(e.CorrelationID) {
		return chain.Event{}, errors.New(prefix + "the correlation id is not well formed")
	}
	ev := chain.Event{TenantID: s.TenantID, CorrelationID: e.CorrelationID, WrittenBy: emitter, Actor: emitter,
		ObjectRef: "exp_" + strings.TrimPrefix(e.CorrelationID, "corr_")}
	if e.Rendered {
		payload, err := renderedPayload(e)
		if err != nil {
			return chain.Event{}, err
		}
		ev.EventType, ev.Payload = eventRendered, payload
		return ev, nil
	}
	payload, err := blockedPayload(e)
	if err != nil {
		return chain.Event{}, err
	}
	ev.EventType, ev.Payload = eventBlocked, payload
	return ev, nil
}

func renderedPayload(e explanation.Event) (map[string]any, error) {
	switch {
	case e.Text == "" || utf8.RuneCountInString(e.Text) > maxClaimText:
		return nil, fmt.Errorf("%sthe text is not 1 to %d characters, the most the reconstruction serves", prefix, maxClaimText)
	case longestDigitRun(e.Text) > maxDigitRun:
		return nil, errors.New(prefix + "the text holds an unmasked digit run longer than the limit (A154)")
	case !templateIDRe.MatchString(e.TemplateID) || !versionRe.MatchString(e.TemplateVersion):
		return nil, errors.New(prefix + "a rendered explanation needs the template id and version it came from")
	case len(e.Rules) != 0:
		return nil, errors.New(prefix + "a rendered explanation carries no rule")
	}
	seen := map[string]bool{}
	for _, id := range e.FactIDs {
		if !factIDRe.MatchString(id) || seen[id] {
			return nil, errors.New(prefix + "a fact id is malformed or repeated")
		}
		seen[id] = true
	}
	claim := map[string]any{"text": e.Text, "disclosed_refs": []string{}, "bound_fact_status": nil}
	if len(e.FactIDs) > 0 {
		claim["bound_fact_status"] = "KNOWN" // the validator admits only KNOWN facts into a claim (INV-13)
		claim["fact_ids"] = e.FactIDs
	}
	if len(e.FactIDs) == 1 {
		claim["fact_id"] = e.FactIDs[0]
	}
	return map[string]any{"claims": []any{claim}, "validator_result": "PASSED", "template_id": e.TemplateID, "template_version": e.TemplateVersion}, nil
}

func blockedPayload(e explanation.Event) (map[string]any, error) {
	if e.Text != "" || len(e.FactIDs) != 0 || e.TemplateID != "" || e.TemplateVersion != "" {
		return nil, errors.New(prefix + "a blocked explanation carries rule codes only, never text")
	}
	if len(e.Rules) == 0 {
		return nil, errors.New(prefix + "a blocked explanation needs at least one rule")
	}
	rules := make([]string, 0, len(e.Rules))
	for _, rule := range e.Rules {
		if !ruleRe.MatchString(rule) {
			return nil, errors.New(prefix + "a rule is not an upper-case code")
		}
		rules = append(rules, rule)
	}
	// Sorted and without repeats whatever the caller's order, so the Record holds one form of the same finding; the caller's slice is left as it was.
	sort.Strings(rules)
	rules = slices.Compact(rules)
	return map[string]any{"rule": rules[0], "rules": rules}, nil
}

func longestDigitRun(s string) int {
	longest := 0
	for _, run := range digitRun.FindAllString(s, -1) {
		if n := utf8.RuneCountInString(run); n > longest {
			longest = n
		}
	}
	return longest
}
