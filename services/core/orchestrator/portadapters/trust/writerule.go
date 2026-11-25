package trustport

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The production WriteRule (TRU-02; A543, A364, A551, interim, relayed, not yet committed; REQ-TRU-004, REQ-NFR-002; INV-01, INV-14). An intent node's `write_when` is a predicate in the
// pack's predicate language over the node's own slots, evaluated on the admitted intent's resolved slot VALUES. True means the intent needs write evaluation.
//
//   - A node with no write_when never writes: false, no error. This is the fail-closed reading (a node the pack does not classify as a write is not one).
//   - A predicate that cannot be evaluated is not a write: false and ErrWriteUnevaluable, never true. That is: a required slot with no value (an absent entry or an empty one is an
//     unresolved slot), a value outside a slot's closed vocabulary (the wrong kind), or a null operand where the language needs a value. An absent OPTIONAL slot is null, as A364
//     says, and the predicate is evaluated over it.
//   - A node the rule does not hold is ErrNoWriteAnswer.
//
// The evaluation is the pure predicate evaluator of internal/predicate: no clock, no I/O, no model (INV-01). A slot value is data, never an instruction (AGENT-09): it is compared and
// never parsed, and no error names one (AGENT-13). The rule names no node, slot or industry term; it reads the pack's data (INV-14).

// ErrWriteUnevaluable is what MayWrite returns, with false, when the node's write_when cannot be evaluated over the slot values it was given. The text names no slot value.
var ErrWriteUnevaluable = errors.New("trustport: the write_when predicate cannot be evaluated over the slot values")

// nowWord finds the clock in a predicate. The rule has no clock (INV-01), so a predicate that names one is refused at build. A text literal that contains the word is refused too:
// the safer reading.
var nowWord = regexp.MustCompile(`\bnow\b`)

type writeRuleSlot struct {
	required bool
	// vocabulary is the closed vocabulary of a Reference slot, nil for any other slot.
	vocabulary map[string]struct{}
}

type writeRuleNode struct {
	// program is nil for a node with no write_when.
	program *predicate.Program
	slots   map[string]writeRuleSlot
}

// PackWriteRule implements WriteRule over the pack's `write_when` data. Build it with NewPackWriteRule. It holds its own copy of the data and is safe for concurrent use.
type PackWriteRule struct {
	nodes map[string]writeRuleNode
}

var _ WriteRule = (*PackWriteRule)(nil)

// NewPackWriteRule builds the rule from the pack's data: pass trustpack.Pack.WriteWhens() of the pack the process loaded (the same pack the other Trust adapters were built from).
// It compiles every node's predicate over the node's own slots (a Reference with a vocabulary is an Enum of it, any other slot is an opaque text) and refuses the WHOLE rule, with
// ErrConfig, when one does not parse, names something that is not a slot of its node, is not Boolean, or names the clock: the failure is at build, never at run time (INV-16). No
// nodes is ErrConfig.
func NewPackWriteRule(nodes map[string]trustpack.WriteWhen) (*PackWriteRule, error) {
	if len(nodes) == 0 {
		return nil, ErrConfig
	}
	out := make(map[string]writeRuleNode, len(nodes))
	for id, w := range nodes {
		n, err := buildWriteRuleNode(w)
		if err != nil {
			return nil, fmt.Errorf("%w: an intent node's write_when is refused: %v", ErrConfig, err)
		}
		out[id] = n
	}
	return &PackWriteRule{nodes: out}, nil
}

func buildWriteRuleNode(w trustpack.WriteWhen) (writeRuleNode, error) {
	n := writeRuleNode{slots: make(map[string]writeRuleSlot, len(w.Slots))}
	scope := predicate.Scope{Variables: make(map[string]predicate.Type, len(w.Slots))}
	for _, s := range w.Slots {
		slot := writeRuleSlot{required: s.Required}
		scope.Variables[s.Name] = predicate.Reference
		if s.Type == "Reference" && len(s.Vocabulary) > 0 {
			slot.vocabulary = make(map[string]struct{}, len(s.Vocabulary))
			for _, v := range s.Vocabulary {
				slot.vocabulary[v] = struct{}{}
			}
			scope.Variables[s.Name] = predicate.EnumOf(s.Vocabulary...)
		}
		n.slots[s.Name] = slot
	}
	if w.Predicate == "" {
		return n, nil
	}
	if nowWord.MatchString(w.Predicate) {
		return writeRuleNode{}, errors.New("the predicate names the clock")
	}
	prog, err := predicate.Compile(w.Predicate, scope)
	if err != nil {
		return writeRuleNode{}, fmt.Errorf("%s", errorCode(err))
	}
	if prog.Type().Kind != predicate.KindBoolean {
		return writeRuleNode{}, errors.New("the predicate is not Boolean")
	}
	n.program = prog
	return n, nil
}

// errorCode is the predicate error's code, never its message, which can quote the pack's literals.
func errorCode(err error) string {
	var pe *predicate.Error
	if errors.As(err, &pe) {
		return string(pe.Code)
	}
	return "UNKNOWN"
}

// MayWrite answers from the node's write_when over the intent's slot values. See the file comment for the three outcomes. An answer is never returned with an error as true.
func (r *PackWriteRule) MayWrite(ctx context.Context, a intake.AdmittedIntent) (bool, error) {
	if r == nil || r.nodes == nil {
		return false, ErrConfig
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	n, ok := r.nodes[a.IntentNode]
	if !ok {
		return false, fmt.Errorf("%w: the pack does not have the intent node", ErrNoWriteAnswer)
	}
	env, err := n.env(a.Slots)
	if err != nil {
		return false, err
	}
	if n.program == nil {
		return false, nil
	}
	may, err := n.program.EvalBool(env)
	if err != nil {
		return false, fmt.Errorf("%w: %s", ErrWriteUnevaluable, errorCode(err))
	}
	return may, nil
}

// env reads the node's declared slots from the values and nothing else: an entry for a slot the node does not declare is ignored. An absent or empty value is null for an optional
// slot and an error for a required one.
func (n writeRuleNode) env(values map[string]string) (predicate.Env, error) {
	env := predicate.Env{Variables: make(map[string]predicate.Value, len(n.slots))}
	for name, slot := range n.slots {
		v := values[name]
		if v == "" {
			if slot.required {
				return predicate.Env{}, fmt.Errorf("%w: a required slot has no value", ErrWriteUnevaluable)
			}
			continue
		}
		if slot.vocabulary == nil {
			env.Variables[name] = predicate.Ref(v)
			continue
		}
		if _, ok := slot.vocabulary[v]; !ok {
			return predicate.Env{}, fmt.Errorf("%w: a slot value is outside the slot's vocabulary", ErrWriteUnevaluable)
		}
		env.Variables[name] = predicate.Enum(v)
	}
	return env, nil
}
