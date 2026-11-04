package policy

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

type rawRule struct {
	RuleID         string                     `json:"rule_id"`
	Class          string                     `json:"class"`
	EvaluationTime string                     `json:"evaluation_time"`
	AppliesTo      []string                   `json:"applies_to"`
	Condition      string                     `json:"condition"`
	Effect         string                     `json:"effect"`
	ReasonCode     string                     `json:"reason_code"`
	Params         map[string]json.RawMessage `json:"params"`
}

type rawPolicy struct {
	PolicyID string    `json:"policy_id"`
	Version  string    `json:"version"`
	Rules    []rawRule `json:"rules"`
}

// Parse decodes a policy document, strictly, and checks every rule (REQ-TRU-004): an unknown field, a duplicate JSON member,
// a missing evaluation_time, an unknown class, effect or evaluation_time, an empty applies_to, a target that is not in the
// vocabulary, a duplicate rule_id, a missing reason code, a parameter that is not a string, an integer or a boolean, and a
// condition that does not parse, does not type-check against the vocabulary's scope or is not Boolean all refuse the
// document. Nothing is partly loaded. The returned Version has no lifecycle yet: see WithMeta.
func Parse(data []byte, v Vocabulary) (Version, error) {
	if err := noDuplicateMembers(data); err != nil {
		return Version{}, invalid(err.Error())
	}
	var raw rawPolicy
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Version{}, invalid(err.Error())
	}
	if raw.PolicyID == "" || raw.Version == "" {
		return Version{}, invalid("policy_id and version are required")
	}
	targets := make(map[string]bool, len(v.Targets))
	for _, t := range v.Targets {
		targets[t] = true
	}
	out := Version{policyID: raw.PolicyID, version: raw.Version}
	seen := map[string]bool{}
	for _, r := range raw.Rules {
		compiled, err := compileRule(r, v.Scope, targets)
		if err != nil {
			return Version{}, invalid(raw.PolicyID + "@" + raw.Version + ": rule " + strconv.Quote(r.RuleID) + ": " + err.Error())
		}
		if seen[compiled.id] {
			return Version{}, invalid(raw.PolicyID + "@" + raw.Version + ": duplicate rule_id " + strconv.Quote(compiled.id))
		}
		seen[compiled.id] = true
		out.rules = append(out.rules, compiled)
	}
	return out, nil
}

func compileRule(r rawRule, scope predicate.Scope, targets map[string]bool) (rule, error) {
	if r.RuleID == "" {
		return rule{}, errors.New("rule_id is required")
	}
	if r.ReasonCode == "" {
		return rule{}, errors.New("reason_code is required")
	}
	class := Class(r.Class)
	if rank(classOrder(), class) < 0 {
		return rule{}, errors.New("class " + strconv.Quote(r.Class) + " is not one of the six rule classes")
	}
	effect := Effect(r.Effect)
	if rank(effectOrder(), effect) < 0 {
		return rule{}, errors.New("effect " + strconv.Quote(r.Effect) + " is not REJECT, ESCALATE, CONFIRM, WAIT or PERMIT")
	}
	et := EvaluationTime(r.EvaluationTime)
	if et != AtDecision && et != AtAdmission {
		return rule{}, errors.New("evaluation_time is required and must be AT_DECISION or AT_ADMISSION")
	}
	if len(r.AppliesTo) == 0 {
		return rule{}, errors.New("applies_to is empty")
	}
	set := make(map[string]bool, len(r.AppliesTo))
	for _, t := range r.AppliesTo {
		if !targets[t] {
			return rule{}, errors.New("applies_to names " + strconv.Quote(t) + ", which is not a registered capability or intent node")
		}
		set[t] = true
	}
	if r.Condition == "" {
		return rule{}, errors.New("condition is required")
	}
	prog, err := predicate.Compile(r.Condition, scope)
	if err != nil {
		return rule{}, errors.New("condition: " + err.Error())
	}
	if prog.Type().Kind != predicate.KindBoolean {
		return rule{}, errors.New("condition is not Boolean")
	}
	params, err := parseParams(r.Params)
	if err != nil {
		return rule{}, err
	}
	return rule{id: r.RuleID, class: class, evalTime: et, targets: set, effect: effect, reason: r.ReasonCode, params: params, cond: prog}, nil
}

// parseParams accepts a string, an integer or a boolean; a float, a null, an array or an object is refused.
func parseParams(raw map[string]json.RawMessage) (map[string]Param, error) {
	out := make(map[string]Param, len(raw))
	for k, b := range raw {
		if k == "" {
			return nil, errors.New("a parameter has an empty name")
		}
		s := strings.TrimSpace(string(b))
		switch {
		case strings.HasPrefix(s, `"`):
			var str string
			if err := json.Unmarshal(b, &str); err != nil {
				return nil, errors.New("parameter " + strconv.Quote(k) + ": " + err.Error())
			}
			out[k] = Param{Kind: ParamString, Str: str}
		case s == "true" || s == "false":
			out[k] = Param{Kind: ParamBoolean, Bool: s == "true"}
		default:
			var n int64 // the JSON decoder refuses a fraction, an exponent, null, an array and an object here
			if err := json.Unmarshal(b, &n); err != nil {
				return nil, errors.New("parameter " + strconv.Quote(k) + " must be a string, an integer or a boolean")
			}
			out[k] = Param{Kind: ParamInteger, Int: n}
		}
	}
	return out, nil
}

// noDuplicateMembers refuses a JSON object that repeats a member name (the standard decoder keeps the last one silently)
// and any data after the document.
func noDuplicateMembers(data []byte) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := walk(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("data after the policy document")
	}
	return nil
}

func walk(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	if d == '{' {
		seen := map[string]bool{}
		for dec.More() {
			kt, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := kt.(string)
			if seen[key] {
				return errors.New("duplicate JSON member " + strconv.Quote(key))
			}
			seen[key] = true
			if err := walk(dec); err != nil {
				return err
			}
		}
	} else {
		for dec.More() {
			if err := walk(dec); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token()
	return err
}
