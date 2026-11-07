package proxy

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
)

// defaultRequestLogLimit bounds the log so a long run cannot grow it without end. Past the limit a request is still counted (in
// Counts and Dropped) but its entry is not kept.
const defaultRequestLogLimit = 10000

// RequestEntry is one request that reached the data side of the proxy, whether or not a fault fired. Time and durations are left out
// on purpose: the same plan, seed and calls must give the same log (REQ-ENV-004). It holds no header, no body and no query string.
// A body and an idempotency key are kept only as SHA-256 digests, so a test can say "the same key twice" without the log holding it.
type RequestEntry struct {
	Seq               int    `json:"seq"`
	Target            string `json:"target"`    // "-" for a path outside the route table
	Operation         string `json:"operation"` // "-" for a path outside the route table
	Method            string `json:"method"`
	Path              string `json:"path"` // as the caller sent it, without the query
	CallNo            int64  `json:"call_no"`
	Fault             string `json:"fault"`           // the effect that fired on this request, or ""
	Forwarded         bool   `json:"forwarded"`       // the request reached the system behind the proxy and an answer came back
	UpstreamStatus    int    `json:"upstream_status"` // that answer's status; 0 when not forwarded
	Outcome           string `json:"outcome"`         // "answered", "dropped" (the connection was cut with no answer) or "refused" (the proxy's own refusal)
	Status            int    `json:"status"`          // what the caller was told; 0 when dropped
	BodySHA256        string `json:"request_body_sha256,omitempty"`
	IdempotencyKeySHA string `json:"idempotency_key_sha256,omitempty"`
}

// generation identifies one life of the log: new at every start and at every reset (A22). A reader that took a baseline compares it
// and so tells a restart, or a reset refilled by identical calls, from "no new requests". It is random, not derived from the log, and
// is kept out of the log hash (REQ-ENV-004: the same plan, seed and calls give the same hash).
func newGeneration() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("hostility proxy: no randomness for the request log generation: " + err.Error()) // the process cannot make a safe claim without it
	}
	return hex.EncodeToString(b[:])
}

type requestLog struct {
	mu         sync.Mutex
	generation string
	limit      int
	total      int
	entries    []RequestEntry
	counts     map[string]int
}

func newRequestLog(limit int) *requestLog {
	if limit <= 0 {
		limit = defaultRequestLogLimit
	}
	return &requestLog{generation: newGeneration(), limit: limit, counts: map[string]int{}}
}

func routeKey(target, operation string) string { return target + " " + operation }

func (l *requestLog) add(e RequestEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total++
	l.counts[routeKey(e.Target, e.Operation)]++
	if len(l.entries) >= l.limit {
		return
	}
	e.Seq = l.total
	l.entries = append(l.entries, e)
}

func (l *requestLog) reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.total, l.entries, l.counts = 0, nil, map[string]int{}
	l.generation = newGeneration()
}

// snapshot returns the generation together with the kept entries, the counts and the dropped number, all read under one hold of the lock, so
// they are one consistent claim: the entries belong to the generation returned with them. Reading them in separate steps would let a reset land
// in between and pair an old generation with a new log (Codex review of noema#209).
func (l *requestLog) snapshot(target, operation string) (string, []RequestEntry, map[string]int, int) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entries, counts, dropped := l.viewLocked(target, operation)
	return l.generation, entries, counts, dropped
}

// view returns the kept entries of one route (all routes when target is empty), the request counts of that route, and how many
// requests in all were not kept.
func (l *requestLog) viewLocked(target, operation string) ([]RequestEntry, map[string]int, int) {
	entries := []RequestEntry{}
	for _, e := range l.entries {
		if (target == "" || e.Target == target) && (operation == "" || e.Operation == operation) {
			entries = append(entries, e)
		}
	}
	counts := map[string]int{}
	for k, n := range l.counts {
		if target == "" || k == routeKey(target, operation) || (operation == "" && len(k) > len(target) && k[:len(target)+1] == target+" ") {
			counts[k] = n
		}
	}
	return entries, counts, l.total - len(l.entries)
}

func hashRequests(entries []RequestEntry) (string, error) {
	b, err := json.Marshal(entries)
	if err != nil {
		return "", fmt.Errorf("hash the request log: %w", err)
	}
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func digest(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// loggingWriter watches what the caller is told. It writes the request's log entry exactly once: when the handler returns, or, for a
// connection that is cut, just before the cut, so a test that sees the connection drop can already read the entry.
type loggingWriter struct {
	http.ResponseWriter
	log     *requestLog
	e       RequestEntry
	status  int
	dropped bool
	refused bool
	once    sync.Once
}

func (l *loggingWriter) WriteHeader(code int) {
	if l.status == 0 {
		l.status = code
	}
	l.ResponseWriter.WriteHeader(code)
}

func (l *loggingWriter) Write(b []byte) (int, error) {
	if l.status == 0 {
		l.status = http.StatusOK
	}
	return l.ResponseWriter.Write(b)
}

// Unwrap lets http.NewResponseController reach the real connection.
func (l *loggingWriter) Unwrap() http.ResponseWriter { return l.ResponseWriter }

func (l *loggingWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	conn, rw, err := http.NewResponseController(l.ResponseWriter).Hijack()
	if err == nil {
		l.dropped = true
		l.finalize()
	}
	return conn, rw, err
}

func (l *loggingWriter) finalize() {
	l.once.Do(func() {
		switch {
		case l.dropped:
			l.e.Outcome, l.e.Status = "dropped", 0
		case l.refused:
			l.e.Outcome, l.e.Status = "refused", l.status
		default:
			l.e.Outcome, l.e.Status = "answered", l.status
		}
		l.log.add(l.e)
	})
}
