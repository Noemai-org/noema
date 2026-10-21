package proxy

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/clock"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/jsonx"
	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/plan"
)

// ControlHandler is the fault-plan control API (PERD §34.4 control plane, REQ-ENV-005): bench credential only, and
// served on the bench_net listener. Nothing on it is open without the credential.
func (s *Server) ControlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /control/v1/fault-plan", s.putPlan)
	mux.HandleFunc("GET /control/v1/fault-plan", s.getPlan)
	mux.HandleFunc("DELETE /control/v1/fault-plan", s.deletePlan)
	mux.HandleFunc("GET /control/v1/trace", s.getTrace)
	mux.HandleFunc("GET /control/v1/stats", s.getStats)
	mux.HandleFunc("GET /control/v1/clock", s.getClock)
	mux.HandleFunc("POST /control/v1/clock/advance", s.advanceClock)
	mux.HandleFunc("POST /control/v1/numbers/map", s.mapNumbers)
	mux.HandleFunc("POST /control/v1/reset", s.reset)
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		controlJSON(w, 404, map[string]any{"error": "no such route"})
	})
	want := sha256.Sum256([]byte(s.opt.BenchToken))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		ok := false
		if len(h) > 7 && strings.EqualFold(h[:7], "Bearer ") {
			got := sha256.Sum256([]byte(h[7:]))
			ok = subtle.ConstantTimeCompare(got[:], want[:]) == 1
		}
		if !ok {
			s.opt.Log.Warn("refused", "listener", "control", "method", r.Method, "path", r.URL.Path, "status", 401)
			controlJSON(w, 401, map[string]any{"error": "a valid credential is required"})
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func controlJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{"error":"could not encode the response"}`)
	}
	w.Write(append(b, '\n')) //nolint:errcheck
}

func (s *Server) planBody() map[string]any {
	c := s.engine.Current()
	out := map[string]any{"loaded": c != nil, "fault_plan_id": plan.NoPlanID, "scenario_id": plan.NoScenario}
	if c != nil {
		out["fault_plan_id"], out["scenario_id"], out["seed"] = c.Plan.FaultPlanID, c.Plan.ScenarioID, c.Plan.Seed
		out["source"], out["applied_faults"], out["ignored_faults"] = c.Plan.Source, c.Applied, c.Ignored
		out["plan"] = json.RawMessage(c.JSON())
	}
	return out
}

func (s *Server) putPlan(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxRequestBody))
	if err != nil {
		controlJSON(w, 413, map[string]any{"error": "the plan is too large"})
		return
	}
	hasFixture := func(name string) bool { _, ok := s.opt.Fixtures[name]; return ok }
	c, err := plan.Parse(data, plan.Env{Routes: s.opt.Routes, HasFixture: hasFixture})
	if err != nil {
		controlJSON(w, 422, map[string]any{"error": map[string]any{"code": "INVALID_FAULT_PLAN", "message": err.Error()}})
		return
	}
	s.clock.mu.Lock()
	if c.Start != nil {
		s.clock.sim = clock.NewSimulated(*c.Start)
		s.clock.clk = s.clock.sim
	} else {
		s.clock.sim, s.clock.clk = nil, clock.System{}
	}
	s.engine.Load(c)
	s.clock.mu.Unlock()
	s.trace.reset()
	s.held.reset()
	s.stale.reset()
	controlJSON(w, 200, s.planBody())
}

func (s *Server) getPlan(w http.ResponseWriter, _ *http.Request) { controlJSON(w, 200, s.planBody()) }

func (s *Server) deletePlan(w http.ResponseWriter, _ *http.Request) {
	s.clearRun()
	controlJSON(w, 200, s.planBody())
}

// clearRun forgets the plan, its counters, the trace, remembered responses and withheld events, and goes back to the wall clock.
func (s *Server) clearRun() {
	s.clock.mu.Lock()
	s.clock.sim, s.clock.clk = nil, clock.System{}
	s.engine.Load(nil)
	s.clock.mu.Unlock()
	s.trace.reset()
	s.held.reset()
	s.stale.reset()
}

func (s *Server) getTrace(w http.ResponseWriter, _ *http.Request) {
	entries := s.trace.snapshot()
	_, simulated := s.clock.now()
	controlJSON(w, 200, map[string]any{"clock_source": "SYNTHETIC", "clock_simulated": simulated, "entries": entries, "trace_hash": hashTrace(entries), "held_events": s.held.pending()})
}

func (s *Server) getStats(w http.ResponseWriter, _ *http.Request) {
	st := &s.stats
	controlJSON(w, 200, map[string]any{
		"requests": st.Requests.Load(), "forwarded": st.Forwarded.Load(),
		"test_numbers_rewritten": st.NumbersOut.Load(), "synthetic_numbers_restored": st.NumbersIn.Load(),
		"unmappable_numbers": st.Unmappable.Load(), "responses_refused_for_a_leaked_test_number": st.LeaksBlocked.Load(),
		"upstream_failures": st.UpstreamFailure.Load(),
	})
}

func (s *Server) getClock(w http.ResponseWriter, _ *http.Request) {
	now, simulated := s.clock.now()
	controlJSON(w, 200, map[string]any{"clock_source": "SYNTHETIC", "simulated": simulated, "now": now.Format(time.RFC3339Nano)})
}

func (s *Server) advanceClock(w http.ResponseWriter, r *http.Request) {
	var b struct {
		By   string `json:"by"`
		Step string `json:"step"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil || (b.By == "") == (b.Step == "") {
		controlJSON(w, 400, map[string]any{"error": "give exactly one of by and step"})
		return
	}
	var total time.Duration
	if b.By != "" {
		d, err := clock.ParseDuration(b.By)
		if err != nil {
			controlJSON(w, 400, map[string]any{"error": err.Error()})
			return
		}
		total = d
	} else {
		var steps []time.Duration
		if c := s.engine.Current(); c != nil {
			steps = c.StepAdvance(b.Step)
		}
		if len(steps) == 0 {
			controlJSON(w, 404, map[string]any{"error": "the loaded fault plan has no clock advance for this step"})
			return
		}
		for _, d := range steps {
			total += d
		}
	}
	if !s.clock.advance(total) {
		controlJSON(w, 409, map[string]any{"error": "the clock is not simulated: load a fault plan with a clock block first"})
		return
	}
	s.getClock(w, r)
}

// mapNumbers tells the runner the synthetic number of each test number, so it can write expectations in the terms
// the system under test sees. Bench credential only: the mapping must never reach a runtime identity.
func (s *Server) mapNumbers(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Numbers []string `json:"numbers"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxRequestBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil || len(b.Numbers) == 0 || len(b.Numbers) > 1000 {
		controlJSON(w, 400, map[string]any{"error": "give numbers: 1 to 1000 test numbers"})
		return
	}
	out := map[string]string{}
	for _, n := range b.Numbers {
		syn, ok := s.opt.Numbers.Synthetic(n)
		if !ok {
			controlJSON(w, 422, map[string]any{"error": "not a test number with a synthetic number: " + jsonx.Redact(n)})
			return
		}
		out[n] = syn
	}
	controlJSON(w, 200, map[string]any{"clock_source": "SYNTHETIC", "mapping": out})
}

func (s *Server) reset(w http.ResponseWriter, _ *http.Request) {
	s.clearRun()
	controlJSON(w, 200, map[string]any{"status": "reset"})
}
