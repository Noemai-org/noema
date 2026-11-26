package controller

import "errors"

// ErrStepParams is returned when the bound params cannot be disclosed as the schema allows.
var ErrStepParams = errors.New(errPrefix + "the bound params cannot be written into the receipt")

// A709 (SR's conditions on the Controller PR, interim, relayed): the receipt's `step_params` member. This file is a stub that discloses nothing, so the tests of the red commit fail.

// ParamHandling says how one bound plan param is written into the receipt.
type ParamHandling string

// The three ways a bound param is disclosed.
const (
	HandlingVerbatim ParamHandling = "VERBATIM"
	HandlingMasked   ParamHandling = "MASKED"
	HandlingHashed   ParamHandling = "HASHED"
)

// The bounds the schema states (execution-receipt.v1 `step_params`).
const (
	MaxStepParams      = 0
	MaxStepParamLength = 0
)

// StepParamPolicy names, for one capability, the plan params the outcome definition binds and how each is disclosed.
type StepParamPolicy interface {
	Bound(capabilityID string) (map[string]ParamHandling, error)
}

// DiscloseParams builds the receipt's step_params from a plan step's params.
func DiscloseParams(params map[string]any, bound map[string]ParamHandling) (map[string]any, error) {
	return nil, nil
}
