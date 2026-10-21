// Package faultplan reads a FaultPlan (PERD §37.1, JSON encoding; contracts/schemas/fault-plan.v1.json) and decides,
// call by call, which fault applies.
//
// A plan may carry faults for several targets: the faults aimed at another component are that component's, and
// mock-case counts them and ignores them. It applies exactly the faults aimed at mock-case, and refuses the plan
// when one of those cannot be applied exactly: an effect it does not produce, params or match keys that mean nothing
// for the operation. A target or effect no plan may name is refused too, so a typo can never become a plan that
// silently injects nothing (REQ-ENV-004). The benchmark owner owns every plan; this package only applies them.
package faultplan

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/clock"
)

// Operation names the case API calls a fault can target.
const (
	OpCreateCase      = "create_case"
	OpFindCases       = "find_cases"
	OpGetCase         = "get_case"
	OpRouteCase       = "route_case"
	OpCreateComplaint = "create_complaint"
	OpFindComplaints  = "find_complaints"
	OpGetComplaint    = "get_complaint"
	OpAppendEvidence  = "append_evidence"
	OpFindEvidence    = "find_evidence"
	OpGetEvidence     = "get_evidence"
)

// Effect values are a subset of the PERD §37.1 effect enum.
const (
	EffectLatency                = "LATENCY"
	EffectTimeoutBeforeCommit    = "TIMEOUT_BEFORE_COMMIT"
	EffectTimeoutAfterCommit     = "TIMEOUT_AFTER_COMMIT"
	EffectDropResponse           = "DROP_RESPONSE"
	EffectHTTP5XX                = "HTTP_5XX"
	EffectHTTP429                = "HTTP_429"
	EffectPaginate               = "PAGINATE"
	EffectConcurrentModification = "CONCURRENT_MODIFICATION"
)

// Source and target names (REQ-ENV-004, PERD §34.1, §37.1).
const (
	SourceMockCase = "MOCK_CASE"
	TargetMockCase = "mock-case"
	// NoPlanID, NoScenario and NoSeed describe a run with no plan loaded.
	NoPlanID   = "fp_none"
	NoScenario = "unscoped"
	NoSeed     = int64(0)
)

// maxDelayMS only catches unit mistakes (seconds written as milliseconds); it is not a product threshold.
const maxDelayMS = 600_000

var sources = map[string]bool{"TUSHE": true, "PROXY": true, SourceMockCase: true}

// targets are the logical services of PERD §34.1; effects are the PERD §37.1 enum. Both are copied from
// contracts/schemas/fault-plan.v1.json and checked against it by TestContractFixtures.
var targets = map[string]bool{"mock-ledger": true, "mock-switch": true, "mock-destination": true, "mock-customer": true, "mock-policy": true,
	TargetMockCase: true, "mock-clock": true, "fault-controller": true, "fixture-loader": true}

var allEffects = map[string]bool{
	"LATENCY": true, "TIMEOUT_BEFORE_COMMIT": true, "TIMEOUT_AFTER_COMMIT": true, "NEAR_TIMEOUT_BEFORE": true, "NEAR_TIMEOUT_AFTER": true,
	"DROP_RESPONSE": true, "HTTP_200_SEMANTIC_PENDING": true, "HTTP_5XX": true, "HTTP_429": true, "MALFORMED_RESPONSE": true,
	"DROP_WEBHOOK": true, "DUPLICATE_WEBHOOK": true, "REORDER_EVENTS": true, "LATE_EVENT": true,
	"STALE_READ": true, "PAGINATE": true, "SCHEMA_DRIFT": true, "CLOCK_SKEW": true, "SERVICE_UNAVAILABLE": true,
	"SPLIT_STATE": true, "DEBIT_WITHOUT_CREDIT": true, "DELAYED_SETTLEMENT": true, "DELAYED_REVERSAL": true, "IMPOSSIBLE_STATE": true,
	"CONCURRENT_MODIFICATION": true, "POLICY_MUTATION": true, "IDEMPOTENCY_COLLISION": true, "INJECTION_PAYLOAD": true,
}

var operationKeys = map[string][]string{
	OpCreateCase:      {"nth_call", "transaction_ref", "idempotency_key", "principal_id", "tenant_id"},
	OpFindCases:       {"nth_call", "transaction_ref", "idempotency_key", "principal_id", "tenant_id"},
	OpGetCase:         {"nth_call", "case_id", "tenant_id"},
	OpRouteCase:       {"nth_call", "case_id", "tenant_id"},
	OpCreateComplaint: {"nth_call", "case_id", "idempotency_key", "tenant_id"},
	OpFindComplaints:  {"nth_call", "case_id", "idempotency_key", "tenant_id"},
	OpGetComplaint:    {"nth_call", "tenant_id"},
	OpAppendEvidence:  {"nth_call", "case_id", "idempotency_key", "tenant_id"},
	OpFindEvidence:    {"nth_call", "case_id", "idempotency_key", "tenant_id"},
	OpGetEvidence:     {"nth_call", "tenant_id"},
}

var listOperations = map[string]bool{OpFindCases: true, OpFindComplaints: true, OpFindEvidence: true}

var (
	planIDRE     = regexp.MustCompile(`^fp_[a-z0-9][a-z0-9_]{0,95}$`)
	scenarioIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	stepRE       = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// Plan is a FaultPlan (PERD §37.1).
type Plan struct {
	FaultPlanID         string            `json:"fault_plan_id"`
	ScenarioID          string            `json:"scenario_id"`
	Seed                int64             `json:"seed"`
	Deterministic       bool              `json:"deterministic"`
	Source              string            `json:"source"`
	Faults              []Fault           `json:"faults"`
	Clock               *ClockBlock       `json:"clock,omitempty"`
	ExpectedTraceEvents []json.RawMessage `json:"expected_trace_events,omitempty"`
}

// Fault is one entry of faults.
type Fault struct {
	Target    string                     `json:"target"`
	Operation string                     `json:"operation"`
	Match     map[string]json.RawMessage `json:"match,omitempty"`
	Effect    string                     `json:"effect"`
	Params    map[string]json.RawMessage `json:"params,omitempty"`
	// Probability is 0 (never) or 1 (always) for every fault of a deterministic plan, and for every fault mock-case
	// applies. It is optional; a fault without one always fires when its match holds.
	Probability *float64 `json:"probability,omitempty"`
}

// ClockBlock drives mock-case's simulated clock (PERD §37.1).
type ClockBlock struct {
	Start    string    `json:"start"`
	Advances []Advance `json:"advances,omitempty"`
}

// Advance moves the simulated clock when the runner announces the step.
type Advance struct {
	AtStep string `json:"at_step"`
	By     string `json:"by"`
}

// Compiled is a validated plan with its typed fault conditions.
type Compiled struct {
	Plan      Plan
	Start     *time.Time
	Applied   int // faults aimed at mock-case
	Ignored   int // faults aimed at another component
	steps     map[string][]time.Duration
	conds     []cond
	exportRaw []byte
}

type cond struct {
	planIndex  int // position in the plan's faults, for the fault trace
	op         string
	attrs      map[string]string
	nth        int64
	effect     string
	delay      time.Duration
	status     int
	afterCmt   bool
	retry      int
	hasRetry   bool
	pageSize   int
	paramsJSON json.RawMessage
}

// Parse reads and validates one plan. Unknown fields at any level are refused.
func Parse(data []byte) (*Compiled, error) {
	var p Plan
	if err := strictDecode(data, &p); err != nil {
		return nil, fmt.Errorf("fault plan: %w", err)
	}
	c := &Compiled{Plan: p, steps: map[string][]time.Duration{}}
	if err := c.validate(); err != nil {
		return nil, fmt.Errorf("fault plan %q: %w", p.FaultPlanID, err)
	}
	canon, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	c.exportRaw = canon
	return c, nil
}

// JSON returns the plan as loaded, for the control API to echo.
func (c *Compiled) JSON() []byte { return c.exportRaw }

func strictDecode(data []byte, into any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the plan")
	}
	return nil
}

func (c *Compiled) validate() error {
	p := c.Plan
	if !planIDRE.MatchString(p.FaultPlanID) {
		return errors.New("fault_plan_id must match ^fp_[a-z0-9][a-z0-9_]*$")
	}
	if p.FaultPlanID == NoPlanID {
		return errors.New("fp_none names the absence of a plan; it cannot be loaded")
	}
	if !scenarioIDRE.MatchString(p.ScenarioID) {
		return errors.New("scenario_id is missing or malformed")
	}
	if p.Seed < 0 {
		return errors.New("seed must be 0 or greater")
	}
	for i, ev := range p.ExpectedTraceEvents {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(ev, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected_trace_events[%d] must be an object", i)
		}
	}
	if !sources[p.Source] {
		return fmt.Errorf("source must be TUSHE, PROXY or %s, got %q", SourceMockCase, p.Source)
	}
	if p.Faults == nil {
		return errors.New("faults is required (use [] for a plan with no fault)")
	}
	for i, f := range p.Faults {
		if !targets[f.Target] {
			return fmt.Errorf("faults[%d]: unknown target %q", i, f.Target)
		}
		if !allEffects[f.Effect] {
			return fmt.Errorf("faults[%d]: unknown effect %q", i, f.Effect)
		}
		if f.Probability != nil && (*f.Probability < 0 || *f.Probability > 1) {
			return fmt.Errorf("faults[%d]: probability must be from 0 to 1", i)
		}
		if p.Deterministic && f.Probability != nil && *f.Probability != 0 && *f.Probability != 1 {
			return fmt.Errorf("faults[%d]: a deterministic plan allows only probability 0 or 1", i)
		}
		if f.Target != TargetMockCase {
			if p.Source == SourceMockCase {
				return fmt.Errorf("faults[%d]: a %s plan holds faults for %s only, not %q", i, SourceMockCase, TargetMockCase, f.Target)
			}
			c.Ignored++
			continue
		}
		cd, err := compileFault(f)
		if err != nil {
			return fmt.Errorf("faults[%d]: %w", i, err)
		}
		cd.planIndex = i
		c.Applied++
		if f.Probability != nil && *f.Probability == 0 {
			continue // a fault that never fires: valid, and it consumes no call
		}
		if f.Probability != nil && *f.Probability != 1 {
			return fmt.Errorf("faults[%d]: mock-case has no probabilistic fault; probability must be 0 or 1", i)
		}
		c.conds = append(c.conds, cd)
	}
	if p.Clock != nil {
		start, err := time.Parse(time.RFC3339, p.Clock.Start)
		if err != nil {
			return fmt.Errorf("clock.start: %w", err)
		}
		u := start.UTC()
		c.Start = &u
		for i, a := range p.Clock.Advances {
			if !stepRE.MatchString(a.AtStep) {
				return fmt.Errorf("clock.advances[%d].at_step is malformed", i)
			}
			d, err := clock.ParseDuration(a.By)
			if err != nil {
				return fmt.Errorf("clock.advances[%d].by: %w", i, err)
			}
			c.steps[a.AtStep] = append(c.steps[a.AtStep], d)
		}
	}
	return nil
}

func compileFault(f Fault) (cond, error) {
	cd := cond{op: f.Operation, effect: f.Effect, attrs: map[string]string{}, status: 500}
	if f.Target != TargetMockCase {
		return cd, fmt.Errorf("target must be %s, got %q", TargetMockCase, f.Target)
	}
	allowed, ok := operationKeys[f.Operation]
	if !ok {
		return cd, fmt.Errorf("unknown operation %q", f.Operation)
	}
	for key, raw := range f.Match {
		if !contains(allowed, key) {
			return cd, fmt.Errorf("match key %q cannot be supplied to %s (allowed: %s)", key, f.Operation, strings.Join(allowed, ", "))
		}
		if key == "nth_call" {
			n, err := intParam(raw)
			if err != nil || n < 1 {
				return cd, errors.New("match.nth_call must be an integer of 1 or more")
			}
			cd.nth = int64(n)
			continue
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s == "" {
			return cd, fmt.Errorf("match.%s must be a non-empty string", key)
		}
		cd.attrs[key] = s
	}
	params := f.Params
	take := func(name string) (json.RawMessage, bool) {
		raw, ok := params[name]
		return raw, ok
	}
	var err error
	known := map[string]bool{}
	switch f.Effect {
	case EffectLatency, EffectTimeoutBeforeCommit, EffectTimeoutAfterCommit:
		known["delay_ms"] = true
		raw, ok := take("delay_ms")
		if !ok {
			return cd, fmt.Errorf("%s needs params.delay_ms", f.Effect)
		}
		var n int
		if n, err = intParam(raw); err != nil || n < 0 || n > maxDelayMS {
			return cd, fmt.Errorf("params.delay_ms must be an integer from 0 to %d", maxDelayMS)
		}
		cd.delay = time.Duration(n) * time.Millisecond
	case EffectDropResponse, EffectConcurrentModification:
	case EffectHTTP5XX:
		known["status"], known["after_commit"] = true, true
		if raw, ok := take("status"); ok {
			var n int
			if n, err = intParam(raw); err != nil || n < 500 || n > 599 {
				return cd, errors.New("params.status must be an integer from 500 to 599")
			}
			cd.status = n
		}
		if raw, ok := take("after_commit"); ok {
			if err = json.Unmarshal(raw, &cd.afterCmt); err != nil {
				return cd, errors.New("params.after_commit must be true or false")
			}
		}
	case EffectHTTP429:
		known["retry_after_seconds"] = true
		if raw, ok := take("retry_after_seconds"); ok {
			var n int
			if n, err = intParam(raw); err != nil || n < 0 {
				return cd, errors.New("params.retry_after_seconds must be an integer of 0 or more")
			}
			cd.retry, cd.hasRetry = n, true
		}
	case EffectPaginate:
		known["page_size"] = true
		raw, ok := take("page_size")
		if !ok {
			return cd, errors.New("PAGINATE needs params.page_size")
		}
		var n int
		if n, err = intParam(raw); err != nil || n < 1 {
			return cd, errors.New("params.page_size must be an integer of 1 or more")
		}
		cd.pageSize = n
		if !listOperations[f.Operation] {
			return cd, fmt.Errorf("PAGINATE applies only to the list operations, not %s", f.Operation)
		}
	default:
		return cd, fmt.Errorf("effect %q is not one mock-case produces", f.Effect)
	}
	if f.Effect == EffectConcurrentModification && f.Operation != OpRouteCase {
		return cd, errors.New("CONCURRENT_MODIFICATION applies only to route_case")
	}
	for name := range params {
		if !known[name] {
			return cd, fmt.Errorf("params.%s is not a parameter of %s", name, f.Effect)
		}
	}
	if len(params) > 0 {
		if cd.paramsJSON, err = json.Marshal(params); err != nil {
			return cd, err
		}
	}
	return cd, nil
}

func intParam(raw json.RawMessage) (int, error) {
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return 0, err
	}
	v, err := n.Int64()
	if err != nil || v > 1<<31 {
		return 0, errors.New("not an integer")
	}
	return int(v), nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// StepAdvance returns the durations a plan attaches to a named step (empty if none).
func (c *Compiled) StepAdvance(step string) []time.Duration { return c.steps[step] }

// Effect is the fault a call received.
type Effect struct {
	Index      int // position in the plan's faults
	Name       string
	Delay      time.Duration
	Status     int             // HTTP_5XX
	AfterCmt   bool            // HTTP_5XX: the command commits first
	RetryAfter *int            // HTTP_429: seconds, nil when the header is omitted
	PageSize   int             // PAGINATE
	Params     json.RawMessage // the fault's params as written in the plan, for the fault trace
}

// AfterCommit reports whether the command commits before the response is lost or replaced.
func (e *Effect) AfterCommit() bool {
	switch e.Name {
	case EffectTimeoutAfterCommit, EffectDropResponse:
		return true
	case EffectHTTP5XX:
		return e.AfterCmt
	}
	return false
}

// Engine counts calls and picks the fault for each. Every fault keeps its own counter of the calls that match
// its other match keys, so concurrent traffic on unrelated keys never moves a fault's nth_call.
type Engine struct {
	mu      sync.Mutex
	plan    *Compiled
	seen    []int64
	opCalls map[string]int64
}

// NewEngine returns an engine with no plan loaded.
func NewEngine() *Engine { return &Engine{opCalls: map[string]int64{}} }

// Load replaces the plan and zeroes every counter. Passing nil unloads the plan.
func (e *Engine) Load(c *Compiled) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.plan = c
	e.opCalls = map[string]int64{}
	e.seen = nil
	if c != nil {
		e.seen = make([]int64, len(c.conds))
	}
}

// Current returns the loaded plan (nil when none).
func (e *Engine) Current() *Compiled {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.plan
}

// Identity returns the fault plan ID, scenario and seed stamped on every log line.
func (e *Engine) Identity() (planID, scenario string, seed int64) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.plan == nil {
		return NoPlanID, NoScenario, NoSeed
	}
	return e.plan.Plan.FaultPlanID, e.plan.Plan.ScenarioID, e.plan.Plan.Seed
}

// Decision is the result of Next.
type Decision struct {
	CallNo int64 // 1-based count of calls to this operation since the plan was loaded
	Effect *Effect
	// The identity of the plan this call was counted against, read in the same step as the count.
	PlanID, ScenarioID string
	Seed               int64
}

// Next records one call and returns the fault that applies to it, if any. The first fault in plan order
// whose conditions hold wins; every fault's counter still advances.
func (e *Engine) Next(op string, attrs map[string]string) Decision {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.opCalls[op]++
	d := Decision{CallNo: e.opCalls[op], PlanID: NoPlanID, ScenarioID: NoScenario, Seed: NoSeed}
	if e.plan == nil {
		return d
	}
	d.PlanID, d.ScenarioID, d.Seed = e.plan.Plan.FaultPlanID, e.plan.Plan.ScenarioID, e.plan.Plan.Seed
	for i := range e.plan.conds {
		cd := &e.plan.conds[i]
		if cd.op != op || !matches(cd.attrs, attrs) {
			continue
		}
		e.seen[i]++
		if d.Effect != nil || (cd.nth != 0 && e.seen[i] != cd.nth) {
			continue
		}
		eff := &Effect{Index: cd.planIndex, Name: cd.effect, Delay: cd.delay, Status: cd.status, AfterCmt: cd.afterCmt, PageSize: cd.pageSize, Params: cd.paramsJSON}
		if cd.hasRetry {
			r := cd.retry
			eff.RetryAfter = &r
		}
		d.Effect = eff
	}
	return d
}

func matches(want, have map[string]string) bool {
	keys := make([]string, 0, len(want))
	for k := range want {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if have[k] != want[k] {
			return false
		}
	}
	return true
}
