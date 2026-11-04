package policy

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// Errors. Callers compare with errors.Is.
var (
	// ErrInvalidPolicy is returned when a policy document or a set of versions is refused at load time.
	ErrInvalidPolicy = errors.New("policy: invalid policy")
	// ErrCondition is returned when a rule condition could not be evaluated. The caller turns it into UNKNOWN (INV-16).
	ErrCondition = errors.New("policy: a rule condition could not be evaluated")
)

// NoPermittingRule is the core reason code of default deny (PERD §23.3).
const NoPermittingRule = "NO_PERMITTING_RULE"

// Class is a rule class (PERD §23.4).
type Class string

// The six classes, highest precedence first.
const (
	Safety         Class = "SAFETY"
	Prohibition    Class = "PROHIBITION"
	TenantSecurity Class = "TENANT_SECURITY"
	Regulatory     Class = "REGULATORY"
	Workflow       Class = "WORKFLOW"
	Default        Class = "DEFAULT"
)

// classOrder is the precedence across classes, highest first.
func classOrder() []Class {
	return []Class{Safety, Prohibition, TenantSecurity, Regulatory, Workflow, Default}
}

// Effect is a rule effect (PERD §23.4).
type Effect string

// The five effects, strongest first.
const (
	Reject   Effect = "REJECT"
	Escalate Effect = "ESCALATE"
	Confirm  Effect = "CONFIRM"
	Wait     Effect = "WAIT"
	Permit   Effect = "PERMIT"
)

// effectOrder is the precedence within the winning class, strongest first.
func effectOrder() []Effect {
	return []Effect{Reject, Escalate, Confirm, Wait, Permit}
}

// EvaluationTime says which policy version a rule is read from (PERD §23.4, SoT §15.3).
type EvaluationTime string

// The two evaluation times.
const (
	AtDecision  EvaluationTime = "AT_DECISION"
	AtAdmission EvaluationTime = "AT_ADMISSION"
)

// Status is the lifecycle state of a policy version.
type Status string

// The three statuses.
const (
	Draft   Status = "DRAFT"
	Active  Status = "ACTIVE"
	Retired Status = "RETIRED"
)

// rank returns the position of v in order, or -1 when v is not in it.
func rank[T ~string](order []T, v T) int {
	for i, o := range order {
		if o == v {
			return i
		}
	}
	return -1
}

// ParamKind is the type of a rule parameter. There are no floats in policy parameters.
type ParamKind string

// The three parameter kinds.
const (
	ParamString  ParamKind = "STRING"
	ParamInteger ParamKind = "INTEGER"
	ParamBoolean ParamKind = "BOOLEAN"
)

// Param is one rule parameter, such as approver_requirement or wait_seconds. Only the field named by Kind is meaningful.
type Param struct {
	Kind ParamKind
	Str  string
	Int  int64
	Bool bool
}

func (p Param) String() string {
	switch p.Kind {
	case ParamInteger:
		return fmt.Sprint(p.Int)
	case ParamBoolean:
		return strconv.FormatBool(p.Bool)
	}
	return p.Str
}

// Vocabulary is what a policy is checked against when it loads: the typed names its conditions may use, and the
// capability IDs and intent nodes its rules may apply to.
type Vocabulary struct {
	Scope   predicate.Scope
	Targets []string
}

type rule struct {
	id       string
	class    Class
	evalTime EvaluationTime
	targets  map[string]bool
	effect   Effect
	reason   string
	params   map[string]Param
	cond     *predicate.Program
}

// Version is one version of one policy: its rules, compiled, and its lifecycle. It is immutable.
type Version struct {
	policyID      string
	version       string
	status        Status
	effectiveFrom int64
	publishedAt   int64
	rules         []rule
}

// PolicyID is the policy's ID, for example fin.pol.
func (v Version) PolicyID() string { return v.policyID }

// Version is the version string, for example 3.1.
func (v Version) Version() string { return v.version }

// Status is the version's lifecycle status; empty until WithMeta sets it.
func (v Version) Status() Status { return v.status }

// EffectiveFrom is the Unix second from which the version is in force.
func (v Version) EffectiveFrom() int64 { return v.effectiveFrom }

// Meta is the lifecycle of a version, set by the control plane when it publishes.
type Meta struct {
	Status        Status
	EffectiveFrom int64
	PublishedAt   int64
}

// WithMeta returns the version with its lifecycle set. A version that is ACTIVE or RETIRED needs an effective_from, because
// the version in force at a time is found by it.
func (v Version) WithMeta(m Meta) (Version, error) {
	switch m.Status {
	case Draft:
	case Active, Retired:
		if m.EffectiveFrom <= 0 {
			return Version{}, invalid(v.policyID + "@" + v.version + ": a " + string(m.Status) + " version needs an effective_from")
		}
	default:
		return Version{}, invalid(v.policyID + "@" + v.version + ": status " + strconv.Quote(string(m.Status)) + " is not DRAFT, ACTIVE or RETIRED")
	}
	v.status, v.effectiveFrom, v.publishedAt = m.Status, m.EffectiveFrom, m.PublishedAt
	return v, nil
}

func invalid(msg string) error { return fmt.Errorf("%w: %s", ErrInvalidPolicy, msg) }
