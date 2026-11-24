package trustpack

import (
	"encoding/json"
	"regexp"

	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// The pack accessors of the final composition (STM-01; A550, A551, interim, relayed, not yet committed; REQ-TRU-009, REQ-STM-001; INV-14, INV-16, INV-21). Each is read-only, answers from
// the bytes the Pack was built from (the manifest-verified bytes: LoadVerified), returns a copy the caller owns, and names no capability, node or industry term (INV-14). A Pack that
// failed verification does not exist, so it yields none; a nil or zero Pack is ErrConfig, and a Pack whose data cannot give a sound answer gives no answer at all (ErrPack), never a
// partial one.

// approverRe is the form of an approver requirement (confirmation-request.v1): the pack's own words, checked here and never invented.
var approverRe = regexp.MustCompile(`^(SELF|MAKER_CHECKER|ROLE:[A-Za-z0-9_.-]+)$`)

const approverParam = "approver_requirement"

// ConfirmationRequirements is what the pack's CONFIRM rules ask of the approver, by capability ID: the rule's `approver_requirement` param, for each capability the rule applies to. It
// holds the pack's data only. Whether a rule's condition holds for an intent, and how long a request lasts, are not here: the first is the policy engine's, and the second is not in the
// pack. It refuses the whole answer, with ErrPack, when a CONFIRM rule has no param of that name, one that is not a string, or one outside SELF, MAKER_CHECKER and ROLE:<role>, and when
// two rules ask different things of one capability (the accessor never picks one).
func (p *Pack) ConfirmationRequirements() (map[string]string, error) {
	doc, err := p.document()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, r := range doc.Policy.Rules {
		if r.Effect != contracts.PolicyRuleV1EffectCONFIRM {
			continue
		}
		req, err := requirementOf(r)
		if err != nil {
			return nil, err
		}
		for _, c := range r.AppliesTo {
			id := string(c)
			if prev, dup := out[id]; dup && prev != req {
				return nil, packErr("a capability has two different confirmation requirements")
			}
			out[id] = req
		}
	}
	return out, nil
}

func requirementOf(r contracts.PolicyRuleV1) (string, error) {
	if r.Params == nil {
		return "", packErr("a confirmation rule names no approver requirement")
	}
	raw, ok := (*r.Params)[approverParam]
	var req string
	if !ok || json.Unmarshal(raw, &req) != nil || !approverRe.MatchString(req) {
		return "", packErr("a confirmation rule's approver requirement is not usable")
	}
	return req, nil
}

// Registry is the capability registry the Planner reads (REQ-CAP-002), built again from the verified bytes on every call: the caller owns the value and the pack's own registry is never
// exposed. It refuses with ErrPack what the load already refuses, and a nil or zero Pack with ErrConfig.
func (p *Pack) Registry() (*registry.Registry, error) {
	doc, err := p.document()
	if err != nil {
		return nil, err
	}
	reg, err := registry.New(doc.Capabilities)
	if err != nil {
		return nil, packErr("the capability registry refuses the pack: %v", err)
	}
	return reg, nil
}

// document decodes the verified bytes the Pack holds. It is the one place an accessor reads them, so no accessor can read anything else.
func (p *Pack) document() (contracts.PackV1, error) {
	var doc contracts.PackV1
	if !p.usable() || len(p.raw) == 0 {
		return doc, ErrConfig
	}
	if err := strictDecode(p.raw, &doc); err != nil {
		return contracts.PackV1{}, packErr("the pack does not decode: %v", err)
	}
	return doc, nil
}
