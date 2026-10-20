package decide

import (
	"errors"
	"math/big"
	"strconv"
	"strings"
)

// Mode is how a clarification asks (REQ-AMB-004, D4).
type Mode string

// OPTIONS lists the candidates; ATTRIBUTE asks by the one attribute whose values separate every candidate.
const (
	ModeOptions   Mode = "OPTIONS"
	ModeAttribute Mode = "ATTRIBUTE"
)

// Limits are the numbers the clarification builder obeys. They come from pack and platform configuration, never from code.
type Limits struct {
	MaxOptions  int // more candidates than this is an ESCALATE, not a question
	MaxDigitRun int // the longest run of consecutive digits a label may show
}

// Candidate is one principal-scoped candidate. Display is the pre-formatted label for OPTIONS mode; Attrs holds the
// pre-formatted, user-verifiable value of each attribute. Fields the pack has not listed as askable are never used.
type Candidate struct {
	ID      string
	Display string
	Attrs   map[string]string
}

// AttributeSpec is one askable attribute, in the pack's fixed order. LabelTemplate holds {value}; wording is pack data.
type AttributeSpec struct {
	Name          string
	QuestionKey   string
	LabelTemplate string
}

// Option is one answer choice and the candidate it selects.
type Option struct {
	ID          string
	Label       string
	CandidateID string
}

// Clarification is the question of a CLARIFY decision. AllowNone is always true: "none of these" is always offered.
type Clarification struct {
	Mode        Mode
	Attribute   string
	QuestionKey string
	Options     []Option
	AllowNone   bool
}

// Errors from BuildClarification. The caller turns each into an ESCALATE (handoff) or a different question.
var (
	ErrNoCandidates      = errors.New("decide: there are no candidates to ask about")
	ErrNothingToAsk      = errors.New("decide: a single candidate needs no question")
	ErrTooManyCandidates = errors.New("decide: more candidates than a question may list")
	ErrCannotDistinguish = errors.New("decide: the labels cannot tell the options apart")
)

const valuePlaceholder = "{value}"

func maxDigitRun(s string) int {
	longest, run := 0, 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			run++
			if run > longest {
				longest = run
			}
		} else {
			run = 0
		}
	}
	return longest
}

func labelOK(label string, lim Limits) bool {
	return strings.TrimSpace(label) != "" && maxDigitRun(label) <= lim.MaxDigitRun
}

// partitionScore is the product of g^g over the group sizes. The expected information gain of asking by an attribute is
// log2(n) minus (sum of g*log2(g))/n, so the larger the gain, the smaller this product. Integers keep the comparison
// exact and free of floating point.
func partitionScore(sizes []int) *big.Int {
	score := big.NewInt(1)
	for _, g := range sizes {
		term := new(big.Int).Exp(big.NewInt(int64(g)), big.NewInt(int64(g)), nil)
		score.Mul(score, term)
	}
	return score
}

type attributePlan struct {
	spec   AttributeSpec
	labels []string
	score  *big.Int
}

// planAttribute returns the plan when the attribute separates every candidate with valid labels.
func planAttribute(cands []Candidate, spec AttributeSpec, lim Limits) (attributePlan, bool) {
	seen := map[string]int{}
	labels := make([]string, 0, len(cands))
	for _, c := range cands {
		v, ok := c.Attrs[spec.Name]
		if !ok || v == "" {
			return attributePlan{}, false
		}
		label := strings.Replace(spec.LabelTemplate, valuePlaceholder, v, 1)
		if !labelOK(label, lim) {
			return attributePlan{}, false
		}
		seen[v]++
		labels = append(labels, label)
	}
	sizes := make([]int, 0, len(seen))
	for _, g := range seen {
		sizes = append(sizes, g)
	}
	if len(seen) != len(cands) {
		return attributePlan{}, false // not every candidate is separated
	}
	return attributePlan{spec: spec, labels: labels, score: partitionScore(sizes)}, true
}

// BuildClarification builds the question for a candidate set (REQ-AMB-004, D4). It computes, for each askable
// attribute in the pack's fixed order, the partition the attribute induces, and asks by the attribute that separates
// every candidate with the greatest expected information gain; a tie goes to the earlier attribute in the pack's order.
// When no attribute separates every candidate it lists the candidates (OPTIONS mode). Labels and option values come from
// the pack's templates and the candidates' pre-formatted fields, never from a model, never show more than
// Limits.MaxDigitRun consecutive digits, and use no field the pack did not list.
func BuildClarification(cands []Candidate, order []AttributeSpec, optionsKey string, lim Limits) (Clarification, error) {
	switch {
	case len(cands) == 0:
		return Clarification{}, ErrNoCandidates
	case len(cands) == 1:
		return Clarification{}, ErrNothingToAsk
	case len(cands) > lim.MaxOptions:
		return Clarification{}, ErrTooManyCandidates
	}
	var best *attributePlan
	for _, spec := range order {
		p, ok := planAttribute(cands, spec, lim)
		if ok && (best == nil || p.score.Cmp(best.score) < 0) {
			cp := p
			best = &cp
		}
	}
	if best != nil {
		return Clarification{Mode: ModeAttribute, Attribute: best.spec.Name, QuestionKey: best.spec.QuestionKey,
			Options: options(cands, best.labels), AllowNone: true}, nil
	}
	labels := make([]string, 0, len(cands))
	seen := map[string]bool{}
	for _, c := range cands {
		if !labelOK(c.Display, lim) || seen[c.Display] {
			return Clarification{}, ErrCannotDistinguish
		}
		seen[c.Display] = true
		labels = append(labels, c.Display)
	}
	return Clarification{Mode: ModeOptions, QuestionKey: optionsKey, Options: options(cands, labels), AllowNone: true}, nil
}

func options(cands []Candidate, labels []string) []Option {
	out := make([]Option, 0, len(cands))
	for i, c := range cands {
		out = append(out, Option{ID: "opt_" + strconv.Itoa(i+1), Label: labels[i], CandidateID: c.ID})
	}
	return out
}
