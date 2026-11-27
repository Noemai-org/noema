package controller

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"

	"github.com/Noemai-org/noema/services/core/platform/ceilings"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// A709 (SR's conditions on the Controller PR, interim, relayed; A692, OD-P-119; REQ-EXE-006, REQ-AUD-006, AGENT-13): the receipt's optional `step_params` member holds the params of
// the executed plan step that the outcome definition binds, and only those. A param that is a personal identifier is masked (the last four digits) or hashed, as its binding says; the
// raw value never reaches the signed receipt. The member is absent, never empty. The Controller learns the bound names from a StepParamPolicy the composition builds from the pack's
// outcome definitions: Execution does not import Outcome (SoT section 51).

// ParamHandling says how one bound plan param is written into the receipt.
type ParamHandling string

// The three ways a bound param is disclosed.
const (
	// HandlingVerbatim writes the value as it is: for a param that is not an identifier (an enumerated reason, a queue, a rank, a flag).
	HandlingVerbatim ParamHandling = "VERBATIM"
	// HandlingMasked keeps only the last four digits of every run of digits of a string value (REQ-AUD-003).
	HandlingMasked ParamHandling = "MASKED"
	// HandlingHashed writes the canonical hash of the value (crypto.HashOf).
	HandlingHashed ParamHandling = "HASHED"
)

// The bounds the schema states (contracts/schemas/execution-receipt.v1.json, `step_params`), held in platform/ceilings, where a test compares them with the schema.
const (
	MaxStepParams      = ceilings.MaxReceiptStepParams
	MaxStepParamLength = ceilings.MaxReceiptStepParamLength
)

// ErrStepParams is returned when the bound params cannot be disclosed as the schema allows. It is returned before the spend: nothing is spent, recorded as dispatched or dispatched.
var ErrStepParams = errors.New(errPrefix + "the bound params cannot be written into the receipt")

// StepParamPolicy names, for one capability, the plan params the outcome definition binds and how each is disclosed. A capability with no binding returns none. An error means
// "could not read", which stops the run before the spend (fail closed).
type StepParamPolicy interface {
	Bound(capabilityID string) (map[string]ParamHandling, error)
}

var slotName = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// DiscloseParams builds the receipt's step_params from a plan step's params and the policy's bound names. A bound name the plan step does not carry is left out. It returns nil when
// nothing is disclosed. Any value the schema would refuse, or that cannot be said as its handling asks, is ErrStepParams.
func DiscloseParams(params map[string]any, bound map[string]ParamHandling) (map[string]any, error) {
	out := map[string]any{}
	for name, handling := range bound {
		if !slotName.MatchString(name) {
			return nil, fmt.Errorf("%w: a bound name is not a slot name", ErrStepParams)
		}
		raw, ok := params[name]
		if !ok {
			continue
		}
		v, err := disclose(raw, handling)
		if err != nil {
			return nil, fmt.Errorf("%w: param %s: %v", ErrStepParams, name, err)
		}
		out[name] = v
	}
	if len(out) == 0 {
		return nil, nil
	}
	if len(out) > MaxStepParams {
		return nil, fmt.Errorf("%w: more than %d bound params", ErrStepParams, MaxStepParams)
	}
	return out, nil
}

func disclose(raw any, handling ParamHandling) (any, error) {
	switch handling {
	case HandlingMasked:
		s, ok := raw.(string)
		if !ok {
			return nil, errors.New("only a string can be masked")
		}
		return bounded(payloadguard.Mask(s))
	case HandlingHashed:
		if _, err := scalar(raw); err != nil {
			return nil, err
		}
		return crypto.HashOf(raw)
	case HandlingVerbatim:
		v, err := scalar(raw)
		if err != nil {
			return nil, err
		}
		if s, isString := v.(string); isString {
			return bounded(s)
		}
		return v, nil
	default:
		return nil, errors.New("unknown handling")
	}
}

// scalar accepts what a plan step param may hold: a string, an integer, a boolean or null. A json.Number is read as an integer or refused. A float is refused: money and every param are
// integers or strings in a plan (the plan reader refuses a float too), and a float that arrived here has already lost what it meant.
func scalar(v any) (any, error) {
	switch x := v.(type) {
	case nil, string, bool, int, int32, int64:
		return v, nil
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n, nil
		}
	}
	return nil, errors.New("not a string, integer, boolean or null")
}

func bounded(s string) (string, error) {
	if len(s) > MaxStepParamLength {
		return "", fmt.Errorf("longer than %d characters", MaxStepParamLength)
	}
	return s, nil
}
