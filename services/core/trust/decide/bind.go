package decide

import "errors"

// Errors from Bind.
var (
	ErrStaleCandidateSet   = errors.New("decide: STALE_CANDIDATE_SET: the answer is for another candidate set or has expired")
	ErrWrongClarification  = errors.New("decide: the answer is for another clarification")
	ErrEmptyAnswer         = errors.New("decide: the answer selects, rejects or declines nothing")
	ErrUnknownOption       = errors.New("decide: the answer names an option that was not offered")
	ErrContradictoryAnswer = errors.New("decide: the answer selects an option it also rejects")
)

// Constraint is a typed constraint an answer adds to the candidate set. Only exclusion exists: a negation ("not that
// one") removes a candidate, and nothing an answer says can add one.
type Constraint struct{ ExcludeCandidate string }

// Open is a clarification as Trust holds it while it waits for the answer.
type Open struct {
	ID                  string
	CandidateSetVersion int
	ExpiresAt           int64
	Round               int
	Options             []Option
	Candidates          []string // candidate IDs, in order
}

// Answer is what the authenticated principal sent. Selected is an option ID; Rejected are option IDs; None is "none of these".
type Answer struct {
	ClarificationID     string
	CandidateSetVersion int
	Selected            string
	Rejected            []string
	None                bool
}

// BindKind is the result of binding an answer.
type BindKind string

// The four results.
const (
	BoundSelected BindKind = "SELECTED" // an explicit, unique selection: the slot may become RESOLVED
	Narrowed      BindKind = "NARROWED" // candidates were excluded; ask again, even if one is left
	NoneOfThese   BindKind = "NONE"     // nothing offered fits: a narrowing question or a handoff
	EscalateRound BindKind = "ESCALATE" // the round limit is reached without a selection
)

// Binding is the result of Bind.
type Binding struct {
	Kind        BindKind
	CandidateID string // set when Kind is BoundSelected
	Remaining   []string
	Constraints []Constraint
}

// ApplyConstraints removes excluded candidates and keeps the order. It is monotone (more constraints give a subset),
// idempotent, and independent of the order of the constraints.
func ApplyConstraints(candidates []string, cs []Constraint) []string {
	excluded := map[string]bool{}
	for _, c := range cs {
		excluded[c.ExcludeCandidate] = true
	}
	out := make([]string, 0, len(candidates))
	for _, id := range candidates {
		if !excluded[id] {
			out = append(out, id)
		}
	}
	return out
}

// Bind applies an answer to an open clarification (REQ-AMB-005). A slot is resolved only by an explicit unique selection
// of an offered option, never by elimination: a negation narrows the set, and a single candidate left is still a
// question. After maxRounds rounds without a selection the result is a handoff.
func Bind(open Open, a Answer, now int64, maxRounds int) (Binding, error) {
	switch {
	case a.ClarificationID != open.ID:
		return Binding{}, ErrWrongClarification
	case a.CandidateSetVersion != open.CandidateSetVersion || now > open.ExpiresAt:
		return Binding{}, ErrStaleCandidateSet
	case a.Selected == "" && len(a.Rejected) == 0 && !a.None:
		return Binding{}, ErrEmptyAnswer
	}
	candidateOf := map[string]string{}
	for _, o := range open.Options {
		candidateOf[o.ID] = o.CandidateID
	}
	var cs []Constraint
	for _, id := range a.Rejected {
		c, ok := candidateOf[id]
		if !ok {
			return Binding{}, ErrUnknownOption
		}
		if id == a.Selected {
			return Binding{}, ErrContradictoryAnswer
		}
		cs = append(cs, Constraint{ExcludeCandidate: c})
	}
	if a.Selected != "" {
		c, ok := candidateOf[a.Selected]
		if !ok {
			return Binding{}, ErrUnknownOption
		}
		return Binding{Kind: BoundSelected, CandidateID: c, Remaining: []string{c}, Constraints: cs}, nil
	}
	remaining := ApplyConstraints(open.Candidates, cs)
	kind := Narrowed
	if a.None || len(remaining) == 0 {
		kind, remaining = NoneOfThese, ApplyConstraints(open.Candidates, nil)
	}
	if open.Round >= maxRounds {
		kind = EscalateRound
	}
	return Binding{Kind: kind, Remaining: remaining, Constraints: cs}, nil
}
