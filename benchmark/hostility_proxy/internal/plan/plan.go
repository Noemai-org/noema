// Package plan reads a FaultPlan (PERD §37.1; contracts/schemas/fault-plan.v1.json) and decides, call by call, which
// fault the hostility proxy applies.
//
// The proxy applies a fault only when the plan's source is PROXY and the fault is not aimed at mock-case. It refuses
// a PROXY plan it cannot apply exactly: an effect the proxy does not produce, a target and operation with no route,
// a match key the route cannot supply, a missing fixture. Nothing is silently skipped, so a typo or a plan meant for
// another producer can never pass for a plan that injects something (REQ-ENV-004, REQ-ENV-005).
package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"sync"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/clock"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/routes"
)

// Effects the proxy produces.
const (
	Latency             = "LATENCY"
	TimeoutBeforeCommit = "TIMEOUT_BEFORE_COMMIT"
	TimeoutAfterCommit  = "TIMEOUT_AFTER_COMMIT"
	NearTimeoutBefore   = "NEAR_TIMEOUT_BEFORE"
	NearTimeoutAfter    = "NEAR_TIMEOUT_AFTER"
	DropResponse        = "DROP_RESPONSE"
	HTTP5XX             = "HTTP_5XX"
	HTTP429             = "HTTP_429"
	Malformed           = "MALFORMED_RESPONSE"
	ServiceUnavailable  = "SERVICE_UNAVAILABLE"
	StaleRead           = "STALE_READ"
	SchemaDrift         = "SCHEMA_DRIFT"
	ClockSkew           = "CLOCK_SKEW"
	DropWebhook         = "DROP_WEBHOOK"
	DuplicateWebhook    = "DUPLICATE_WEBHOOK"
	ReorderEvents       = "REORDER_EVENTS"
	LateEvent           = "LATE_EVENT"
	ImpossibleState     = "IMPOSSIBLE_STATE"
	InjectionPayload    = "INJECTION_PAYLOAD"
)

// Source and target names.
const (
	SourceProxy    = "PROXY"
	TargetMockCase = "mock-case"
	NoPlanID       = "fp_none"
	NoScenario     = "unscoped"
)

// maxDelayMS only catches seconds written as milliseconds; it is not a product threshold.
const maxDelayMS = 600_000

var proxyEffects = map[string]bool{
	Latency: true, TimeoutBeforeCommit: true, TimeoutAfterCommit: true, NearTimeoutBefore: true, NearTimeoutAfter: true,
	DropResponse: true, HTTP5XX: true, HTTP429: true, Malformed: true, ServiceUnavailable: true, StaleRead: true,
	SchemaDrift: true, ClockSkew: true, DropWebhook: true, DuplicateWebhook: true, ReorderEvents: true, LateEvent: true,
	ImpossibleState: true, InjectionPayload: true,
}

// allEffects, targets and sources are copied from the schema and checked against it by the contract test.
var allEffects = map[string]bool{
	"LATENCY": true, "TIMEOUT_BEFORE_COMMIT": true, "TIMEOUT_AFTER_COMMIT": true, "NEAR_TIMEOUT_BEFORE": true, "NEAR_TIMEOUT_AFTER": true,
	"DROP_RESPONSE": true, "HTTP_200_SEMANTIC_PENDING": true, "HTTP_5XX": true, "HTTP_429": true, "MALFORMED_RESPONSE": true,
	"DROP_WEBHOOK": true, "DUPLICATE_WEBHOOK": true, "REORDER_EVENTS": true, "LATE_EVENT": true,
	"STALE_READ": true, "PAGINATE": true, "SCHEMA_DRIFT": true, "CLOCK_SKEW": true, "SERVICE_UNAVAILABLE": true,
	"SPLIT_STATE": true, "DEBIT_WITHOUT_CREDIT": true, "DELAYED_SETTLEMENT": true, "DELAYED_REVERSAL": true, "IMPOSSIBLE_STATE": true,
	"CONCURRENT_MODIFICATION": true, "POLICY_MUTATION": true, "IDEMPOTENCY_COLLISION": true, "INJECTION_PAYLOAD": true,
}

var targets = map[string]bool{"mock-ledger": true, "mock-switch": true, "mock-destination": true, "mock-customer": true, "mock-policy": true,
	TargetMockCase: true, "mock-clock": true, "fault-controller": true, "fixture-loader": true}

var sources = map[string]bool{"TUSHE": true, SourceProxy: true, "MOCK_CASE": true}

var (
	planIDRE     = regexp.MustCompile(`^fp_[a-z0-9][a-z0-9_]{0,95}$`)
	scenarioIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)
	stepRE       = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	fieldRE      = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]{0,127}$`)
	fixtureRE    = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

// Plan is a FaultPlan.
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
	Target      string                     `json:"target"`
	Operation   string                     `json:"operation"`
	Match       map[string]json.RawMessage `json:"match,omitempty"`
	Effect      string                     `json:"effect"`
	Params      map[string]json.RawMessage `json:"params,omitempty"`
	Probability *float64                   `json:"probability,omitempty"`
}

// ClockBlock drives the simulated clock.
type ClockBlock struct {
	Start    string    `json:"start"`
	Advances []Advance `json:"advances,omitempty"`
}

// Advance moves the simulated clock when the runner announces the step.
type Advance struct {
	AtStep string `json:"at_step"`
	By     string `json:"by"`
}

// Env is what a plan is checked against: the route table and the fixtures the proxy holds.
type Env struct {
	Routes     *routes.Table
	HasFixture func(name string) bool
}

type fault struct {
	planIndex int
	target    string
	op        string
	attrs     map[string]string
	nth       int64
	prob      *float64
	effect    string
	paramsRaw json.RawMessage

	delay       time.Duration
	status      int
	afterCommit bool
	retryAfter  *int
	seconds     int
	skew        int
	change      string
	field       string
	fixture     string
}

// Compiled is a validated plan.
type Compiled struct {
	Plan    Plan
	Start   *time.Time
	Applied int // faults this proxy applies
	Ignored int // faults aimed at another component or produced by another source
	steps   map[string][]time.Duration
	faults  []fault
	raw     []byte
}

// JSON returns the plan as loaded.
func (c *Compiled) JSON() []byte { return c.raw }

// StepAdvance returns the durations a plan attaches to a named step.
func (c *Compiled) StepAdvance(step string) []time.Duration { return c.steps[step] }

// Parse reads and validates one plan against the environment.
func Parse(data []byte, env Env) (*Compiled, error) {
	var p Plan
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("fault plan: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("fault plan: trailing data after the plan")
	}
	c := &Compiled{Plan: p, steps: map[string][]time.Duration{}}
	if err := c.validate(env); err != nil {
		return nil, fmt.Errorf("fault plan %q: %w", p.FaultPlanID, err)
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	c.raw = raw
	return c, nil
}

func (c *Compiled) validate(env Env) error {
	p := c.Plan
	if !planIDRE.MatchString(p.FaultPlanID) || p.FaultPlanID == NoPlanID {
		return errors.New("fault_plan_id must match ^fp_[a-z0-9][a-z0-9_]*$ and cannot be fp_none")
	}
	if !scenarioIDRE.MatchString(p.ScenarioID) {
		return errors.New("scenario_id is missing or malformed")
	}
	if p.Seed < 0 {
		return errors.New("seed must be 0 or greater")
	}
	if !sources[p.Source] {
		return fmt.Errorf("unknown source %q", p.Source)
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
		if p.Source == "MOCK_CASE" && f.Target != TargetMockCase {
			return fmt.Errorf("faults[%d]: a MOCK_CASE plan holds faults for mock-case only", i)
		}
		if p.Source != SourceProxy || f.Target == TargetMockCase {
			c.Ignored++ // another producer's fault: mock-case's, Tushe's, or the proxy's none
			continue
		}
		ft, err := compile(i, f, env)
		if err != nil {
			return fmt.Errorf("faults[%d]: %w", i, err)
		}
		c.Applied++
		c.faults = append(c.faults, ft)
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
	for i, ev := range p.ExpectedTraceEvents {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(ev, &obj); err != nil || obj == nil {
			return fmt.Errorf("expected_trace_events[%d] must be an object", i)
		}
	}
	return nil
}

func intParam(raw json.RawMessage) (int, error) {
	var n json.Number
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&n); err != nil {
		return 0, err
	}
	v, err := n.Int64()
	if err != nil || v > 1<<31 || v < -(1<<31) {
		return 0, errors.New("not an integer")
	}
	return int(v), nil
}

func compile(index int, f Fault, env Env) (fault, error) {
	ft := fault{planIndex: index, target: f.Target, op: f.Operation, effect: f.Effect, attrs: map[string]string{}, status: 500, prob: f.Probability}
	if !proxyEffects[f.Effect] {
		return ft, fmt.Errorf("the proxy does not produce %s (Tushe or mock-case does); use the producer's source", f.Effect)
	}
	rt := env.Routes.Lookup(f.Target, f.Operation)
	if rt == nil {
		return ft, fmt.Errorf("no route serves %s %s", f.Target, f.Operation)
	}
	for key, raw := range f.Match {
		if key == "nth_call" {
			n, err := intParam(raw)
			if err != nil || n < 1 {
				return ft, errors.New("match.nth_call must be an integer of 1 or more")
			}
			ft.nth = int64(n)
			continue
		}
		if _, ok := rt.Attrs[key]; !ok {
			return ft, fmt.Errorf("match key %q is not an attribute of %s %s", key, f.Target, f.Operation)
		}
		var s string
		if err := json.Unmarshal(raw, &s); err != nil || s == "" {
			return ft, fmt.Errorf("match.%s must be a non-empty string", key)
		}
		ft.attrs[key] = s
	}
	known := map[string]bool{}
	need := func(name string) (json.RawMessage, error) {
		known[name] = true
		raw, ok := f.Params[name]
		if !ok {
			return nil, fmt.Errorf("%s needs params.%s", f.Effect, name)
		}
		return raw, nil
	}
	opt := func(name string) (json.RawMessage, bool) {
		known[name] = true
		raw, ok := f.Params[name]
		return raw, ok
	}
	var err error
	switch f.Effect {
	case Latency, TimeoutBeforeCommit, TimeoutAfterCommit, NearTimeoutBefore, NearTimeoutAfter, LateEvent:
		raw, e := need("delay_ms")
		if e != nil {
			return ft, e
		}
		n, e := intParam(raw)
		if e != nil || n < 0 || n > maxDelayMS {
			return ft, fmt.Errorf("params.delay_ms must be an integer from 0 to %d", maxDelayMS)
		}
		ft.delay = time.Duration(n) * time.Millisecond
	case HTTP5XX:
		if raw, ok := opt("status"); ok {
			n, e := intParam(raw)
			if e != nil || n < 500 || n > 599 {
				return ft, errors.New("params.status must be an integer from 500 to 599")
			}
			ft.status = n
		}
		if raw, ok := opt("after_commit"); ok {
			if err = json.Unmarshal(raw, &ft.afterCommit); err != nil {
				return ft, errors.New("params.after_commit must be true or false")
			}
		}
	case HTTP429:
		if raw, ok := opt("retry_after_seconds"); ok {
			n, e := intParam(raw)
			if e != nil || n < 0 {
				return ft, errors.New("params.retry_after_seconds must be an integer of 0 or more")
			}
			ft.retryAfter = &n
		}
	case StaleRead:
		raw, e := need("seconds")
		if e != nil {
			return ft, e
		}
		n, e := intParam(raw)
		if e != nil || n < 1 {
			return ft, errors.New("params.seconds must be an integer of 1 or more")
		}
		ft.seconds = n
		if rt.Method != "GET" {
			return ft, errors.New("STALE_READ applies to GET routes only")
		}
	case ClockSkew:
		raw, e := need("skew_seconds")
		if e != nil {
			return ft, e
		}
		if ft.skew, e = intParam(raw); e != nil {
			return ft, errors.New("params.skew_seconds must be an integer")
		}
	case SchemaDrift:
		raw, e := need("change")
		if e != nil {
			return ft, e
		}
		if e = json.Unmarshal(raw, &ft.change); e != nil || (ft.change != "add" && ft.change != "rename" && ft.change != "remove" && ft.change != "unknown_enum") {
			return ft, errors.New("params.change must be add, rename, remove or unknown_enum")
		}
		if raw, ok := opt("field"); ok {
			if e = json.Unmarshal(raw, &ft.field); e != nil || !fieldRE.MatchString(ft.field) {
				return ft, errors.New("params.field is malformed")
			}
		}
	case InjectionPayload:
		raw, e := need("field")
		if e != nil {
			return ft, e
		}
		if e = json.Unmarshal(raw, &ft.field); e != nil || !fieldRE.MatchString(ft.field) {
			return ft, errors.New("params.field is malformed")
		}
	case ImpossibleState:
		raw, e := need("fixture")
		if e != nil {
			return ft, e
		}
		if e = json.Unmarshal(raw, &ft.fixture); e != nil || !fixtureRE.MatchString(ft.fixture) {
			return ft, errors.New("params.fixture is malformed")
		}
		if !env.HasFixture(ft.fixture) {
			return ft, fmt.Errorf("the proxy has no fixture %q", ft.fixture)
		}
	case DropWebhook, DuplicateWebhook, ReorderEvents:
		// no params
	}
	switch f.Effect {
	case DropWebhook, DuplicateWebhook, ReorderEvents, LateEvent:
		if rt.EventFeed == nil {
			return ft, fmt.Errorf("%s needs an event-feed route; %s %s is not one", f.Effect, f.Target, f.Operation)
		}
	}
	for name := range f.Params {
		if !known[name] {
			return ft, fmt.Errorf("params.%s is not a parameter of %s", name, f.Effect)
		}
	}
	if len(f.Params) > 0 {
		if ft.paramsRaw, err = json.Marshal(f.Params); err != nil {
			return ft, err
		}
	}
	return ft, nil
}

// Effect is the fault a call received.
type Effect struct {
	Index       int // position in the plan's faults
	Name        string
	Delay       time.Duration
	Status      int
	AfterCommit bool
	RetryAfter  *int
	Seconds     int
	Skew        int
	Change      string
	Field       string
	Fixture     string
	Params      json.RawMessage
	// Pick is a number drawn from (seed, fault, nth match): the same plan and seed always give the same Pick.
	Pick uint64
}

// Decision is the result of Next.
type Decision struct {
	CallNo             int64 // 1-based count of calls to this target and operation since the plan was loaded
	Effect             *Effect
	PlanID, ScenarioID string
	Seed               int64
}

// Engine counts calls and picks the fault for each. Every fault keeps its own counter of the calls that match its
// other match keys, so concurrent traffic on other keys never moves a fault's nth_call.
type Engine struct {
	mu    sync.Mutex
	plan  *Compiled
	seen  []int64
	calls map[string]int64
}

// NewEngine returns an engine with no plan.
func NewEngine() *Engine { return &Engine{calls: map[string]int64{}} }

// Load replaces the plan and zeroes every counter. A nil plan unloads.
func (e *Engine) Load(c *Compiled) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.plan, e.calls, e.seen = c, map[string]int64{}, nil
	if c != nil {
		e.seen = make([]int64, len(c.faults))
	}
}

// Current returns the loaded plan.
func (e *Engine) Current() *Compiled {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.plan
}

// Draw returns a number in [0, 2^64) that is a pure function of its arguments.
func Draw(seed int64, faultIndex int, n int64) uint64 {
	var buf [24]byte
	binary.BigEndian.PutUint64(buf[0:], uint64(seed))
	binary.BigEndian.PutUint64(buf[8:], uint64(faultIndex))
	binary.BigEndian.PutUint64(buf[16:], uint64(n))
	sum := sha256.Sum256(append([]byte("hostility-proxy/draw/v1\x00"), buf[:]...))
	return binary.BigEndian.Uint64(sum[:8])
}

func fires(seed int64, f *fault, n int64) bool {
	if f.prob == nil || *f.prob == 1 {
		return true
	}
	if *f.prob == 0 {
		return false
	}
	return float64(Draw(seed, f.planIndex, n)>>11)/float64(1<<53) < *f.prob
}

// Next records one call and returns the fault that applies, if any. The first fault in plan order whose conditions
// hold wins; every fault's counter still advances.
func (e *Engine) Next(target, op string, attrs map[string]string) Decision {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := target + "/" + op
	e.calls[key]++
	d := Decision{CallNo: e.calls[key], PlanID: NoPlanID, ScenarioID: NoScenario}
	if e.plan == nil {
		return d
	}
	d.PlanID, d.ScenarioID, d.Seed = e.plan.Plan.FaultPlanID, e.plan.Plan.ScenarioID, e.plan.Plan.Seed
	for i := range e.plan.faults {
		f := &e.plan.faults[i]
		if f.target != target || f.op != op || !matches(f.attrs, attrs) {
			continue
		}
		e.seen[i]++
		if d.Effect != nil || (f.nth != 0 && e.seen[i] != f.nth) || !fires(d.Seed, f, e.seen[i]) {
			continue
		}
		d.Effect = &Effect{Index: f.planIndex, Name: f.effect, Delay: f.delay, Status: f.status, AfterCommit: f.afterCommit,
			RetryAfter: f.retryAfter, Seconds: f.seconds, Skew: f.skew, Change: f.change, Field: f.field, Fixture: f.fixture,
			Params: f.paramsRaw, Pick: Draw(d.Seed, f.planIndex, -e.seen[i])}
	}
	return d
}

func matches(want, have map[string]string) bool {
	for k, v := range want {
		if have[k] != v {
			return false
		}
	}
	return true
}
