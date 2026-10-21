package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/auth"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/clock"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/faultplan"
	"github.com/Noemai-org/noema/synthetic/mock_case/internal/store"
)

func (s *Server) benchRoutes() []route {
	return []route{
		{"GET", "/bench/v1/openapi.json", s.describe(benchDescription)},
		{"GET", "/truth/v1/state", s.truthState},
		{"GET", "/truth/v1/commands", s.truthCommands},
		{"GET", "/truth/v1/digest", s.truthDigest},
		{"PUT", "/control/v1/fault-plan", s.putPlan},
		{"GET", "/control/v1/fault-plan", s.getPlan},
		{"DELETE", "/control/v1/fault-plan", s.deletePlan},
		{"GET", "/control/v1/clock", s.getClock},
		{"POST", "/control/v1/clock/advance", s.advanceClock},
		{"POST", "/control/v1/reset", s.reset},
	}
}

// BenchRoutes lists "METHOD path" for every route of the bench listener.
func (s *Server) BenchRoutes() []string { return routeNames(s.benchRoutes()) }

// BenchHandler is the truth and control API: bench credential only, and no path is open without it.
// A runtime identity is refused here with 403, and the refusal is logged (REQ-ENV-002).
func (s *Server) BenchHandler() http.Handler {
	mux := http.NewServeMux()
	for _, rt := range s.benchRoutes() {
		mux.HandleFunc(rt.method+" "+rt.path, rt.handler)
	}
	mux.HandleFunc("/", notFoundRoute)
	return s.gate(auth.Bench, nil, noHead(mux), "bench")
}

const clockSource = "SYNTHETIC"

func (s *Server) benchDB(w http.ResponseWriter, err error) bool {
	if err != nil {
		s.log.Error("bench store failure", "err", err)
		writeErr(w, "", internalErr())
		return false
	}
	return true
}

type stateBody struct {
	ClockSource string `json:"clock_source"`
	store.Snapshot
}

func (s *Server) truthState(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withDeadline(r.Context())
	defer cancel()
	snap, err := s.st.Snapshot(ctx)
	if !s.benchDB(w, err) {
		return
	}
	writeJSON(w, 200, stateBody{ClockSource: clockSource, Snapshot: snap})
}

const maxCommands = 1000

func (s *Server) truthCommands(w http.ResponseWriter, r *http.Request) {
	q, e := queryParams(r, "after_seq", "limit", "faults_only")
	if e != nil {
		writeErr(w, "", e)
		return
	}
	after, limit, faults := int64(0), 200, false
	var err error
	if v := q["after_seq"]; v != "" {
		if after, err = strconv.ParseInt(v, 10, 64); err != nil || after < 0 {
			writeErr(w, "", badRequest("after_seq", "must be an integer of 0 or more"))
			return
		}
	}
	if v := q["limit"]; v != "" {
		if limit, err = strconv.Atoi(v); err != nil || limit < 1 || limit > maxCommands {
			writeErr(w, "", badRequest("limit", "must be an integer from 1 to "+strconv.Itoa(maxCommands)))
			return
		}
	}
	if v := q["faults_only"]; v != "" {
		if faults, err = strconv.ParseBool(v); err != nil {
			writeErr(w, "", badRequest("faults_only", "must be true or false"))
			return
		}
	}
	ctx, cancel := withDeadline(r.Context())
	defer cancel()
	rows, err := s.st.Commands(ctx, after, limit, faults)
	if !s.benchDB(w, err) {
		return
	}
	writeJSON(w, 200, map[string]any{"clock_source": clockSource, "commands": rows, "complete": len(rows) < limit})
}

// traceEntry is a fault-trace line without the fields that differ between replays (row number, time).
type traceEntry struct {
	Operation          string          `json:"operation"`
	CallNo             int64           `json:"call_no"`
	FaultIndex         *int            `json:"fault_index"`
	FaultEffect        *string         `json:"fault_effect"`
	FaultParams        json.RawMessage `json:"fault_params"`
	Committed          bool            `json:"committed"`
	ResponseSuppressed bool            `json:"response_suppressed"`
	StatusCode         int             `json:"status_code"`
}

func sha(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // fixed structs of plain values
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// truthDigest returns the hashes that let a replay be compared in one call: two runs of the same plan and seed
// on the simulated clock must give identical digests (REQ-ENV-004).
func (s *Server) truthDigest(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withDeadline(r.Context())
	defer cancel()
	snap, err := s.st.Snapshot(ctx)
	if !s.benchDB(w, err) {
		return
	}
	rows, err := s.st.Commands(ctx, 0, 1_000_000, true)
	if !s.benchDB(w, err) {
		return
	}
	trace := make([]traceEntry, 0, len(rows))
	for _, c := range rows {
		trace = append(trace, traceEntry{c.Operation, c.CallNo, c.FaultIndex, c.FaultEffect, c.FaultParams, c.Committed, c.ResponseSuppressed, c.StatusCode})
	}
	_, simulated := s.now()
	writeJSON(w, 200, map[string]any{
		"clock_source": clockSource, "clock_simulated": simulated,
		"state_hash": sha(snap), "fault_trace_hash": sha(trace), "fault_trace_length": len(trace),
	})
}

func (s *Server) planBody() map[string]any {
	c := s.engine.Current()
	id, scenario, seed := s.engine.Identity()
	out := map[string]any{"fault_plan_id": id, "scenario_id": scenario, "seed": seed, "loaded": c != nil}
	if c != nil {
		out["plan"] = json.RawMessage(c.JSON())
	}
	return out
}

func (s *Server) putPlan(w http.ResponseWriter, r *http.Request) {
	data, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		writeErr(w, "", newErr(413, "PAYLOAD_TOO_LARGE", "the plan is too large", false))
		return
	}
	c, err := faultplan.Parse(data)
	if err != nil {
		writeErr(w, "", newErr(422, "INVALID_FAULT_PLAN", err.Error(), false))
		return
	}
	// Load the plan and the clock together so no call sees one without the other.
	s.clkMu.Lock()
	if c.Start != nil {
		s.sim = clock.NewSimulated(*c.Start)
		s.clk = s.sim
	} else {
		s.sim, s.clk = nil, clock.System{}
	}
	s.engine.Load(c)
	s.clkMu.Unlock()
	writeJSON(w, 200, s.planBody())
}

func (s *Server) getPlan(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, s.planBody()) }

func (s *Server) deletePlan(w http.ResponseWriter, _ *http.Request) {
	s.clkMu.Lock()
	s.sim, s.clk = nil, clock.System{}
	s.engine.Load(nil)
	s.clkMu.Unlock()
	writeJSON(w, 200, s.planBody())
}

func (s *Server) getClock(w http.ResponseWriter, _ *http.Request) {
	now, simulated := s.now()
	writeJSON(w, 200, map[string]any{"clock_source": clockSource, "simulated": simulated, "now": now.Format(time.RFC3339Nano)})
}

func (s *Server) advanceClock(w http.ResponseWriter, r *http.Request) {
	var b struct {
		By   string `json:"by"`
		Step string `json:"step"`
	}
	if e := decodeBody(w, r, &b); e != nil {
		writeErr(w, "", e)
		return
	}
	if (b.By == "") == (b.Step == "") {
		writeErr(w, "", badRequest("by", "give exactly one of by and step"))
		return
	}
	var total time.Duration
	if b.By != "" {
		d, err := clock.ParseDuration(b.By)
		if err != nil {
			writeErr(w, "", badRequest("by", err.Error()))
			return
		}
		total = d
	} else {
		c := s.engine.Current()
		var steps []time.Duration
		if c != nil {
			steps = c.StepAdvance(b.Step)
		}
		if len(steps) == 0 {
			writeErr(w, "", newErr(404, "UNKNOWN_STEP", "the loaded fault plan has no clock advance for this step", false))
			return
		}
		for _, d := range steps {
			total += d
		}
	}
	if err := s.advance(total); err != nil {
		writeErr(w, "", newErr(409, "CLOCK_NOT_SIMULATED", err.Error(), false))
		return
	}
	s.getClock(w, r)
}

func (s *Server) reset(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := withDeadline(r.Context())
	defer cancel()
	if err := s.st.Reset(ctx); !s.benchDB(w, err) {
		return
	}
	s.clkMu.Lock()
	s.sim, s.clk = nil, clock.System{}
	s.engine.Load(nil)
	s.clkMu.Unlock()
	writeJSON(w, 200, map[string]string{"status": "reset", "clock_source": clockSource})
}
