package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

// A774 (interim, relayed; A709; REQ-EXE-006, REQ-AUD-006; INV-14, AGENT-13, AGENT-16): the Controller learns which params of a plan step the receipt carries, and how each is
// written, from the pack's `outcome_bindings`: a `PLAN_PARAM:<param>` source names a bound param, and the binding's optional `disclosure` says VERBATIM, MASKED or HASHED. A param the
// binding does not list is HASHED, the safest of the three. The core holds no capability, param or outcome name (INV-14).

const planParamSource = "PLAN_PARAM:"

// stepParamPolicy is the policy read from the pack: for each capability, its bound params and their handling. It is immutable once built.
type stepParamPolicy struct {
	byCapability map[string]map[string]controller.ParamHandling
}

// Bound returns a copy of the capability's bound params; a capability with no binding, or one that binds no plan param, has none.
func (p *stepParamPolicy) Bound(capabilityID string) (map[string]controller.ParamHandling, error) {
	bound := p.byCapability[capabilityID]
	out := make(map[string]controller.ParamHandling, len(bound))
	for name, how := range bound {
		out[name] = how
	}
	return out, nil
}

type packBinding struct {
	CapabilityID string            `json:"capability_id"`
	Variables    map[string]string `json:"variables"`
	Disclosure   map[string]string `json:"disclosure"`
}

// stepParamPolicyFromPack builds the Controller's StepParamPolicy from the verified pack bytes. A pack that cannot be read, a binding that is not as the contract says and a disclosure
// that is none of the three words give an error and no policy (fail closed: nothing is dispatched without a policy, A774). A pack with no bindings gives an empty policy.
func stepParamPolicyFromPack(pack []byte) (controller.StepParamPolicy, error) {
	var doc struct {
		Bindings []packBinding `json:"outcome_bindings"`
	}
	if err := json.Unmarshal(pack, &doc); err != nil {
		return nil, errors.New("cmd/core: the pack's outcome bindings cannot be read")
	}
	policy := &stepParamPolicy{byCapability: map[string]map[string]controller.ParamHandling{}}
	for _, b := range doc.Bindings {
		bound := map[string]controller.ParamHandling{}
		for _, source := range b.Variables {
			param, ok := strings.CutPrefix(source, planParamSource)
			if !ok {
				continue
			}
			how, err := handlingOf(b.Disclosure, param)
			if err != nil {
				return nil, err
			}
			bound[param] = how
		}
		for param := range b.Disclosure {
			if _, ok := bound[param]; !ok {
				return nil, fmt.Errorf("cmd/core: a binding's disclosure names a param it does not bind")
			}
		}
		if len(bound) > 0 {
			policy.byCapability[b.CapabilityID] = bound
		}
	}
	return policy, nil
}

func handlingOf(disclosure map[string]string, param string) (controller.ParamHandling, error) {
	word, listed := disclosure[param]
	if !listed {
		return controller.HandlingHashed, nil
	}
	switch how := controller.ParamHandling(word); how {
	case controller.HandlingVerbatim, controller.HandlingMasked, controller.HandlingHashed:
		return how, nil
	}
	return "", errors.New("cmd/core: a binding's disclosure is none of VERBATIM, MASKED or HASHED")
}
