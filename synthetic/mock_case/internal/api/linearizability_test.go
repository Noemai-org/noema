package api_test

import (
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/mock_case/internal/lincheck"
)

// D7 (DEC-2026-10-20), PERD §27.3: concurrent operation histories of mock-case, recorded under faults, are
// checked by a Porcupine-style checker. Any non-linearizable history fails the build.
//
// One slot is one idempotency key and one subject (transaction and epoch). A rival is a second key that creates for the
// same subject: with create-if-absent, whichever of the two commits first holds the subject and the other is refused
// with PRECONDITION_FAILED, whatever the interleaving.
//
// One slot is one idempotency key. Operations on different slots are independent, so each slot's history is
// checked alone. The operations are exactly the ones the core's case adapter performs: create with a key (two
// payload variants, so a key can be reused with a different payload), find by key, read by id, and route with
// an expected version.

const defaultQueue = "PAYMENTS_OPERATIONS"

type lIn struct {
	slot     int
	kind     string // create, rival, find, get, route
	variant  int    // create: which payload
	expected int64  // route
	queue    string // route
}

type lOut struct {
	status   int
	code     string
	found    bool // find: an item was returned
	caseID   string
	version  int64
	queue    string
	current  int64  // route mismatch: the token the error names
	existing string // precondition failure: the case the error names
}

type slotState struct {
	exists  bool
	variant int
	caseID  string // "" until some response reveals it
	version int64
	queue   string
	rival   bool   // the rival key's case holds the subject
	rivalID string // "" until some response reveals it
}

func (s slotState) key() string {
	return fmt.Sprintf("%t/%d/%s/%d/%s/%t/%s", s.exists, s.variant, s.caseID, s.version, s.queue, s.rival, s.rivalID)
}

// adopt records the case ID the first time a response shows it, and reports a contradiction afterwards.
func (s *slotState) adopt(id string) bool {
	if id == "" {
		return false
	}
	if s.caseID == "" {
		s.caseID = id
		return true
	}
	return s.caseID == id
}

// adoptRival is adopt for the case held by the rival key.
func (s *slotState) adoptRival(id string) bool {
	if id == "" {
		return false
	}
	if s.rivalID == "" {
		s.rivalID = id
		return true
	}
	return s.rivalID == id
}

var mockCaseModel = lincheck.Model{
	Init: func() any { return slotState{} },
	Key:  func(s any) string { return s.(slotState).key() },
	Step: func(state, input, output any, pending bool) (any, bool) {
		st := state.(slotState)
		in := input.(lIn)
		var out lOut
		if !pending {
			out = output.(lOut)
		}
		switch in.kind {
		case "create":
			switch {
			case !st.exists && st.rival:
				if !pending && (out.status != 412 || out.code != "PRECONDITION_FAILED" || !st.adoptRival(out.existing)) {
					return nil, false
				}
			case !st.exists:
				if !pending && (out.status != 201 || out.version != 1 || out.queue != defaultQueue || !st.adopt(out.caseID)) {
					return nil, false
				}
				st.exists, st.variant, st.version, st.queue = true, in.variant, 1, defaultQueue
			case st.variant == in.variant:
				if !pending && (out.status != 200 || out.version != st.version || out.queue != st.queue || !st.adopt(out.caseID)) {
					return nil, false
				}
			default:
				if !pending && (out.status != 409 || out.code != "IDEMPOTENCY_CONFLICT") {
					return nil, false
				}
			}
		case "rival":
			switch {
			case st.rival:
				if !pending && (out.status != 200 || !st.adoptRival(out.caseID)) {
					return nil, false
				}
			case st.exists:
				if !pending && (out.status != 412 || out.code != "PRECONDITION_FAILED" || !st.adopt(out.existing)) {
					return nil, false
				}
			default:
				if !pending && (out.status != 201 || !st.adoptRival(out.caseID)) {
					return nil, false
				}
				st.rival = true
			}
		case "find":
			if pending {
				break
			}
			if !st.exists {
				if out.status != 200 || out.found {
					return nil, false
				}
			} else if out.status != 200 || !out.found || out.version != st.version || out.queue != st.queue || !st.adopt(out.caseID) {
				return nil, false
			}
		case "get":
			if pending {
				break
			}
			if !st.exists || out.status != 200 || out.version != st.version || out.queue != st.queue || !st.adopt(out.caseID) {
				return nil, false
			}
		case "route":
			switch {
			case !st.exists:
				if !pending && out.status != 404 {
					return nil, false
				}
			case in.expected == st.version:
				if !pending && (out.status != 200 || out.version != st.version+1 || out.queue != in.queue) {
					return nil, false
				}
				st.version, st.queue = st.version+1, in.queue
			default:
				if !pending && (out.status != 409 || out.code != "VERSION_MISMATCH" || out.current != st.version) {
					return nil, false
				}
			}
		}
		return st, true
	},
}

type recorder struct {
	tick atomic.Int64
	mu   sync.Mutex
	ops  []lincheck.Operation
	n    struct{ pending, replays, mismatches, conflicts, creates, preconditions int }
	lost []string // why each operation has no definitive response, for the failure message of a quiet run
}

func (r *recorder) lose(why string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lost = append(r.lost, why)
}

func (r *recorder) add(client int, in lIn, out lOut, pending bool, call int64) {
	op := lincheck.Operation{ClientID: client, Input: in, Output: out, Call: call, Pending: pending}
	if !pending {
		op.Return = r.tick.Add(1)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, op)
	switch {
	case pending:
		r.n.pending++
	case (in.kind == "create" || in.kind == "rival") && out.status == 200:
		r.n.replays++
	case (in.kind == "create" || in.kind == "rival") && out.status == 201:
		r.n.creates++
	case out.code == "PRECONDITION_FAILED":
		r.n.preconditions++
	case out.code == "VERSION_MISMATCH":
		r.n.mismatches++
	case out.code == "IDEMPOTENCY_CONFLICT":
		r.n.conflicts++
	}
}

// variantBody is the create of a slot: its own subject, and a payload that differs between variants (same key, other
// payload is a conflict).
func variantBody(slot, variant int) map[string]any {
	b := caseBodyFor(fmt.Sprintf("TX-S%d", slot))
	if variant == 1 {
		b["priority"] = "HIGH"
	}
	return b
}

func versionOf(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimPrefix(s, "v"), 10, 64)
	return n
}

func outOfCase(status int, c map[string]any, code string) lOut {
	o := lOut{status: status, code: code}
	if c != nil {
		o.caseID, _ = c["case_id"].(string)
		o.version = versionOf(fmt.Sprint(c["version_token"]))
		o.queue, _ = c["owner_queue"].(string)
	}
	return o
}

// faultClientTimeout is how long a client waits when faults are loaded: short on purpose, so a TIMEOUT_AFTER_COMMIT
// of 600ms is an operation with no seen response.
const faultClientTimeout = 300 * time.Millisecond

// quietClientTimeout is how long a client waits when no fault is loaded. Nothing in a quiet run is meant to time out,
// so the wait only has to outlast a slow machine: the 300ms of the fault runs is the same as a stall of 8 concurrent
// clients on a loaded CI runner against a real PostgreSQL, and a client that gives up there reports a lost response
// that no fault caused. The service itself bounds every database call at 30s.
const quietClientTimeout = 10 * time.Second

// workload runs `workers` concurrent clients for `perWorker` operations each against slots, with faults applied.
func workload(t *testing.T, e *env, seed int64, workers, perWorker, slots int) *recorder {
	t.Helper()
	return workloadWithTimeout(t, e, faultClientTimeout, seed, workers, perWorker, slots)
}

// workloadWithTimeout is workload with the time a client waits for an answer before it gives up.
func workloadWithTimeout(t *testing.T, e *env, clientTimeout time.Duration, seed int64, workers, perWorker, slots int) *recorder {
	t.Helper()
	rec := &recorder{}
	client := &http.Client{Timeout: clientTimeout, Transport: &http.Transport{DisableKeepAlives: true, MaxIdleConnsPerHost: -1}}
	var known sync.Map // slot -> case ID learned from a response
	var wg sync.WaitGroup
	queues := []string{"PAYMENTS_OPERATIONS", "DISPUTES", "FRAUD_REVIEW", "SETTLEMENT_OPS"}
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(seed*1000 + int64(w)))
			lastSeen := map[int]int64{} // slot -> the version this worker last saw
			for i := 0; i < perWorker; i++ {
				slot := rng.Intn(slots)
				idk := fmt.Sprintf("idk_slot%d", slot)
				id, haveID := known.Load(slot)
				kind := []string{"create", "create", "rival", "find", "get", "route", "route", "route"}[rng.Intn(8)]
				if (kind == "get" || kind == "route") && !haveID {
					kind = "find"
				}
				in := lIn{slot: slot, kind: kind}
				rivalIdk := fmt.Sprintf("idk_rival%d", slot)
				var method, url string
				var hdr map[string]string
				var body any
				switch kind {
				case "create":
					in.variant = 0
					if rng.Intn(5) == 0 {
						in.variant = 1
					}
					method, url, hdr, body = "POST", e.caseURL+"/v1/cases", newKey(idk), variantBody(slot, in.variant)
				case "rival":
					method, url, hdr, body = "POST", e.caseURL+"/v1/cases", newKey(rivalIdk), variantBody(slot, 0)
				case "find":
					method, url = "GET", e.caseURL+"/v1/cases?tenant_id=ten_a&idempotency_key="+idk
				case "get":
					method, url = "GET", e.caseURL+"/v1/cases/"+id.(string)+"?tenant_id=ten_a"
				case "route":
					in.queue = queues[rng.Intn(len(queues))]
					in.expected = lastSeen[slot]
					if in.expected == 0 || rng.Intn(4) == 0 {
						in.expected = 1 + int64(rng.Intn(3))
					}
					method, url = "POST", e.caseURL+"/v1/cases/"+id.(string)+"/route"
					body = map[string]any{"tenant_id": "ten_a", "owner_queue": in.queue, "expected_version_token": "v" + strconv.FormatInt(in.expected, 10)}
				}
				call := rec.tick.Add(1)
				r, err := e.send(client, method, url, runtimeToken, hdr, body)
				if err != nil || r.status >= 500 || r.status == 429 {
					why := fmt.Sprintf("status %d", r.status)
					if err != nil {
						why = err.Error()
					}
					rec.lose(fmt.Sprintf("%s %s: %s", method, url, why))
					rec.add(w, in, lOut{}, true, call) // no definitive response: the effect is unknown
					continue
				}
				var out lOut
				switch {
				case kind == "find" && r.status == 200:
					out = lOut{status: 200}
					if items := r.items(); len(items) > 0 {
						out = outOfCase(200, items[0], "")
						out.found = true
					}
				case r.status == 200 || r.status == 201:
					out = outOfCase(r.status, r.json, "")
				default:
					out = lOut{status: r.status, code: r.errCode()}
					if out.code == "VERSION_MISMATCH" {
						out.current = versionOf(detail(r, "current_version_token"))
					}
					if out.code == "PRECONDITION_FAILED" {
						out.existing = detail(r, "existing_case_id")
					}
					if r.status != 404 && r.status != 409 && r.status != 412 {
						t.Errorf("unexpected answer to %s %s: %d %s", method, url, r.status, r.raw)
					}
				}
				if kind == "rival" {
					// The rival's case is not the slot's case: later reads and routes address the slot's own.
					rec.add(w, in, out, false, call)
					continue
				}
				if out.caseID != "" {
					known.Store(slot, out.caseID)
				}
				if out.version > 0 {
					lastSeen[slot] = out.version
				}
				if out.current > 0 {
					lastSeen[slot] = out.current
				}
				rec.add(w, in, out, false, call)
			}
		}(w)
	}
	wg.Wait()
	return rec
}

func detail(r resp, field string) string {
	errObj, _ := r.json["error"].(map[string]any)
	details, _ := errObj["details"].([]any)
	for _, d := range details {
		m := d.(map[string]any)
		if m["field"] == field {
			s, _ := m["issue"].(string)
			return s
		}
	}
	return ""
}

// faultsEveryFewCalls spreads the commit-then-drop family over the run.
func faultsEveryFewCalls() string {
	var fs []string
	add := func(op string, nth int, effect, params string) {
		fs = append(fs, fault(op, fmt.Sprintf(`{"nth_call":%d}`, nth), effect, params))
	}
	for i := 0; i < 12; i++ {
		add("create_case", 3+9*i, "DROP_RESPONSE", `{}`)
		add("create_case", 5+9*i, "HTTP_5XX", `{"status":502,"after_commit":true}`)
		add("create_case", 7+9*i, "HTTP_5XX", `{"status":503}`)
		add("create_case", 8+9*i, "HTTP_429", `{"retry_after_seconds":1}`)
		add("route_case", 2+7*i, "DROP_RESPONSE", `{}`)
		add("route_case", 4+7*i, "HTTP_5XX", `{"status":500,"after_commit":true}`)
		add("route_case", 6+7*i, "TIMEOUT_BEFORE_COMMIT", `{"delay_ms":10}`)
		add("find_cases", 4+5*i, "DROP_RESPONSE", `{}`)
		add("get_case", 3+6*i, "HTTP_5XX", `{"status":500}`)
	}
	add("create_case", 40, "TIMEOUT_AFTER_COMMIT", `{"delay_ms":600}`)
	add("route_case", 30, "TIMEOUT_AFTER_COMMIT", `{"delay_ms":600}`)
	return strings.Join(fs, ",")
}

func checkHistory(t *testing.T, label string, rec *recorder) {
	t.Helper()
	slotKey := func(o lincheck.Operation) string { return strconv.Itoa(o.Input.(lIn).slot) }
	failed, res := lincheck.CheckPartitioned(mockCaseModel, rec.ops, slotKey, 0)
	if res.Inconclusive {
		t.Fatalf("%s: the checker ran out of budget on slot %s; an inconclusive history is a failure", label, failed)
	}
	if !res.Linearizable {
		t.Fatalf("%s: slot %s is NOT linearizable. Deepest prefix: %v", label, failed, res.Deepest)
	}
}

func TestMockCaseHistoriesUnderFaultsAreLinearizable(t *testing.T) {
	seeds := []int64{1, 2, 3, 4, 5, 6}
	if testing.Short() {
		seeds = seeds[:2]
	}
	var total struct{ ops, pending, replays, mismatches, conflicts, preconditions int }
	for _, seed := range seeds {
		t.Run(fmt.Sprintf("seed%d", seed), func(t *testing.T) {
			e := newEnv(t)
			e.loadPlan(plan(faultsEveryFewCalls(), ""))
			rec := workload(t, e, seed, 8, 40, 5)
			checkHistory(t, fmt.Sprintf("seed %d", seed), rec)
			total.ops += len(rec.ops)
			total.pending += rec.n.pending
			total.replays += rec.n.replays
			total.mismatches += rec.n.mismatches
			total.conflicts += rec.n.conflicts
			total.preconditions += rec.n.preconditions
			// The history is not vacuous, and the database agrees with the story the clients could not see.
			if len(e.bench("GET", "/truth/v1/commands?faults_only=true", nil).json["commands"].([]any)) == 0 {
				t.Fatal("no fault fired")
			}
			if n := e.count("cases"); n > 5 {
				t.Fatalf("%d cases for 5 subjects: a subject got more than one case", n)
			}
		})
	}
	t.Logf("checked %d operations: %d with no response, %d replays, %d version mismatches, %d key conflicts, %d refused creates", total.ops, total.pending, total.replays, total.mismatches, total.conflicts, total.preconditions)
	if total.pending == 0 || total.replays == 0 || total.mismatches == 0 || total.conflicts == 0 || total.preconditions == 0 {
		t.Fatalf("the workload did not exercise faults and contention: %+v", total)
	}
}

// The check can fail: corrupting one recorded answer of a real history makes it non-linearizable.
func TestACorruptedHistoryIsRejected(t *testing.T) {
	e := newEnv(t)
	rec := workload(t, e, 99, 6, 30, 3)
	checkHistory(t, "uncorrupted", rec)
	corruptions := map[string]func(o *lincheck.Operation) bool{
		"a version that skips ahead": func(o *lincheck.Operation) bool {
			out := o.Output.(lOut)
			if o.Pending || out.status != 200 || out.version == 0 || o.Input.(lIn).kind == "rival" {
				return false
			}
			out.version += 5
			o.Output = out
			return true
		},
		"a created case that was never created": func(o *lincheck.Operation) bool {
			out := o.Output.(lOut)
			if o.Pending || o.Input.(lIn).kind != "find" || out.found {
				return false
			}
			out.found, out.caseID, out.version, out.queue = true, "case_ghost", 1, defaultQueue
			o.Output = out
			return true
		},
		"a replay reported as a fresh create": func(o *lincheck.Operation) bool {
			out := o.Output.(lOut)
			if o.Pending || o.Input.(lIn).kind != "create" || out.status != 200 {
				return false
			}
			out.status = 201
			o.Output = out
			return true
		},
		"a refused create reported as created": func(o *lincheck.Operation) bool {
			out := o.Output.(lOut)
			if o.Pending || out.code != "PRECONDITION_FAILED" {
				return false
			}
			o.Output = lOut{status: 201, caseID: "case_ghost", version: 1, queue: defaultQueue}
			return true
		},
		"a stale route reported as applied": func(o *lincheck.Operation) bool {
			out := o.Output.(lOut)
			if o.Pending || o.Input.(lIn).kind != "route" || out.code != "VERSION_MISMATCH" {
				return false
			}
			o.Output = lOut{status: 200, version: o.Input.(lIn).expected + 1, queue: o.Input.(lIn).queue}
			return true
		},
	}
	for name, corrupt := range corruptions {
		bad := &recorder{ops: append([]lincheck.Operation(nil), rec.ops...)}
		done := false
		for i := range bad.ops {
			if corrupt(&bad.ops[i]) {
				done = true
				break
			}
		}
		if !done {
			t.Logf("%s: this history had no operation of that kind to corrupt", name)
			continue
		}
		slotKey := func(o lincheck.Operation) string { return strconv.Itoa(o.Input.(lIn).slot) }
		if k, res := lincheck.CheckPartitioned(mockCaseModel, bad.ops, slotKey, 0); res.Linearizable {
			t.Errorf("%s: the corruption on slot %s was not detected", name, k)
		}
	}
}

// A quiet run (no faults) is also linearizable, and every effect the clients saw is in the truth endpoint.
func TestMockCaseHistoriesWithoutFaultsAreLinearizable(t *testing.T) {
	e := newEnv(t)
	rec := workloadWithTimeout(t, e, quietClientTimeout, 7, 8, 40, 4)
	checkHistory(t, "no faults", rec)
	if rec.n.pending != 0 {
		t.Fatalf("%d operations lost their response with no fault loaded:\n  %s", rec.n.pending, strings.Join(rec.lost, "\n  "))
	}
	if rec.n.creates != e.count("cases") {
		t.Fatalf("clients saw %d creations, the truth endpoint holds %d cases", rec.n.creates, e.count("cases"))
	}
}

// Model-based property test (REQ-TST-002): with one client the order is fixed, so every random sequence of
// operations must match the sequential specification exactly, with and without faults, for many seeds.
func TestRandomSequentialSequencesMatchTheSpecification(t *testing.T) {
	seeds := 20
	if testing.Short() {
		seeds = 4
	}
	for seed := int64(100); seed < 100+int64(seeds); seed++ {
		e := newEnv(t)
		if seed%2 == 0 {
			e.loadPlan(plan(faultsEveryFewCalls(), ""))
		}
		rec := workload(t, e, seed, 1, 60, 3)
		checkHistory(t, fmt.Sprintf("sequential seed %d", seed), rec)
		if rec.n.pending == 0 && seed%2 == 0 {
			t.Fatalf("seed %d: no fault reached a single client", seed)
		}
	}
}
