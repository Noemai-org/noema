package planner

import (
	"encoding/json"
	"fmt"

	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

const (
	errPrefix     = "planner: "
	schemaVersion = "1.0"
)

// StepSpec is one step a caller asks for. The capability is a pack ID that must resolve to an allowed capability in the
// registry. Params hold strings, integers, booleans and null only (plan.v1): never a float.
type StepSpec struct {
	NodeID         string
	CapabilityID   string
	Params         map[string]any
	Preconditions  []string
	Postconditions []string
}

// Plan is a plan.v1 document that was built from registry handles. Its fields are unexported, so the only way to hold a
// non-empty Plan is New, which refuses an unregistered or prohibited capability (INV-18). The zero Plan cannot be hashed.
type Plan struct{ doc contracts.PlanV1 }

func refuse(format string, args ...any) *registry.Error {
	return &registry.Error{Code: registry.CodeIncomplete, Reason: errPrefix + fmt.Sprintf(format, args...)}
}

// New builds a plan. Steps are numbered 0, 1, 2 in the order given. It refuses a nil registry, an empty graph version or
// intent node, no steps, a capability that is unregistered or prohibited (the registry's CAPABILITY_NOT_REGISTERED or
// CAPABILITY_PROHIBITED), a param outside the plan.v1 types, and a plan that cannot be canonicalised and hashed.
func New(reg *registry.Registry, graphVersion, intentNode string, specs []StepSpec) (Plan, error) {
	if reg == nil {
		return Plan{}, refuse("no registry")
	}
	if graphVersion == "" || intentNode == "" || len(specs) == 0 {
		return Plan{}, refuse("a plan needs a graph version, an intent node and at least one step")
	}
	doc := contracts.PlanV1{SchemaVersion: schemaVersion, GraphVersion: contracts.CommonV1Semver(graphVersion), IntentNode: contracts.CommonV1PackId(intentNode)}
	for i, s := range specs {
		if _, err := reg.Callable(s.CapabilityID); err != nil {
			return Plan{}, fmt.Errorf("%sstep %d: %w", errPrefix, i, err)
		}
		params, err := paramValues(s.Params)
		if err != nil {
			return Plan{}, fmt.Errorf("%sstep %d: %w", errPrefix, i, err)
		}
		doc.Steps = append(doc.Steps, contracts.PlanV1StepsItem{Step: int64(i), NodeId: contracts.CommonV1PackId(s.NodeID), CapabilityId: contracts.CommonV1PackId(s.CapabilityID),
			Params: params, Preconditions: predicates(s.Preconditions), ExpectedPostconditions: predicates(s.Postconditions)})
	}
	p := Plan{doc: doc}
	if _, err := p.Hash(); err != nil {
		return Plan{}, err
	}
	return p, nil
}

func predicates(in []string) []contracts.CommonV1Predicate {
	out := make([]contracts.CommonV1Predicate, 0, len(in))
	for _, s := range in {
		out = append(out, contracts.CommonV1Predicate(s))
	}
	return out
}

func paramValues(in map[string]any) (map[string]contracts.PlanV1StepsItemParamsValue, error) {
	out := make(map[string]contracts.PlanV1StepsItemParamsValue, len(in))
	for k, v := range in {
		switch v.(type) {
		case nil, string, bool, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		default:
			return nil, refuse("param %q is a %T; a param is a string, an integer, a boolean or null", k, v)
		}
		raw, err := json.Marshal(v)
		if err != nil {
			return nil, refuse("param %q: %v", k, err)
		}
		out[k] = contracts.PlanV1StepsItemParamsValue(raw)
	}
	return out, nil
}

// Document returns a deep copy of the plan.v1 document: the steps, every params map and each param's bytes, and both predicate
// lists. Writing into the result cannot change the plan or its hash.
func (p Plan) Document() contracts.PlanV1 {
	doc := p.doc
	doc.Steps = make([]contracts.PlanV1StepsItem, len(p.doc.Steps))
	for i, s := range p.doc.Steps {
		c := s
		c.Params = make(map[string]contracts.PlanV1StepsItemParamsValue, len(s.Params))
		for k, v := range s.Params {
			c.Params[k] = append(contracts.PlanV1StepsItemParamsValue(nil), v...)
		}
		c.Preconditions = append([]contracts.CommonV1Predicate{}, s.Preconditions...)
		c.ExpectedPostconditions = append([]contracts.CommonV1Predicate{}, s.ExpectedPostconditions...)
		doc.Steps[i] = c
	}
	return doc
}

// Hash is plan_hash: SHA-256 of the RFC 8785 form of the plan document (REQ-PLN-002), as "sha256:<64 hex>". The zero Plan
// has no hash.
func (p Plan) Hash() (string, error) {
	if len(p.doc.Steps) == 0 {
		return "", refuse("there is no plan to hash")
	}
	h, err := crypto.HashOf(p.doc)
	if err != nil {
		return "", refuse("the plan cannot be canonicalised: %v", err)
	}
	return h, nil
}

// HashDocument hashes a plan that arrives as a document, for example a stored row. It first checks the document against
// the registry and the shape rules (steps numbered 0, 1, 2 with no gap; every capability allowed), so a document naming a
// prohibited or unregistered capability has no hash (INV-18, REQ-PLN-002).
func HashDocument(reg *registry.Registry, doc contracts.PlanV1) (string, error) {
	if reg == nil {
		return "", refuse("no registry")
	}
	if doc.GraphVersion == "" || doc.IntentNode == "" || len(doc.Steps) == 0 {
		return "", refuse("a plan needs a graph version, an intent node and at least one step")
	}
	for i, s := range doc.Steps {
		if s.Step != int64(i) {
			return "", refuse("step %d is numbered %d; steps are numbered 0, 1, 2 in order", i, s.Step)
		}
		if _, err := reg.Callable(string(s.CapabilityId)); err != nil {
			return "", fmt.Errorf("%sstep %d: %w", errPrefix, i, err)
		}
	}
	h, err := crypto.HashOf(doc)
	if err != nil {
		return "", refuse("the plan cannot be canonicalised: %v", err)
	}
	return h, nil
}
