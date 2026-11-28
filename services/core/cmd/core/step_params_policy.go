package main

import (
	"errors"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// errStepParamPolicyNotBuilt is the red stub's answer.
var errStepParamPolicyNotBuilt = errors.New("cmd/core: the step param policy is not built")

// stepParamPolicyFromPack builds the Controller's StepParamPolicy from the pack's outcome_bindings. Red stub (A774): not built.
func stepParamPolicyFromPack(pack []byte) (controller.StepParamPolicy, error) {
	return nil, errStepParamPolicyNotBuilt
}
