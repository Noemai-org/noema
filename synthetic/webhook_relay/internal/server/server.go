// Package server is the relay's HTTP surface.
//
//	POST /webhooks          Tushe delivers here. Verified, stored, acknowledged with 200 only once on disk.
//	GET  /v1/deliveries     The core pulls here (bearer token, derived from the signing secret). ?after=<seq>&limit=<n>.
//	GET  /v1/stats          Counters, same credential.
//	GET  /healthz           Liveness, no credential, no data.
//
// A refused delivery is never stored, whatever it says (REQ-API-008; DEC-2026-10-16 E6).
package server

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/store"
	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/verify"
)

const (
	maxBody      = 1 << 20
	defaultLimit = 100
	maxLimit     = 500
)

// Server wires the verifier and the store.
type Server struct {
	v     *verify.Verifier
	st    *store.Store
	token string
	now   func() time.Time

	mu       sync.Mutex
	counters map[string]int64
}

// New returns a Server. now is the clock the tolerance window is measured against.
func New(v *verify.Verifier, st *store.Store, pullToken string, now func() time.Time) *Server {
	return &Server{v: v, st: st, token: pullToken, now: now, counters: map[string]int64{}}
}

func (s *Server) count(name string) {
	s.mu.Lock()
	s.counters[name]++
	s.mu.Unlock()
}

// Handler is the mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/webhooks", s.webhook)
	mux.HandleFunc("/v1/deliveries", s.auth(s.deliveries))
	mux.HandleFunc("/v1/stats", s.auth(s.stats))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) webhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, 405, map[string]string{"error": "method_not_allowed"})
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxBody))
	if err != nil {
		s.count("refused_body_unreadable_or_too_large")
		writeJSON(w, 413, map[string]string{"error": "body_unreadable_or_too_large"})
		return
	}
	now := s.now()
	d, err := s.v.Check(r.Header, body, now)
	if err != nil {
		var ve *verify.Error
		reason := string(verify.BadSignature)
		if errors.As(err, &ve) {
			reason = string(ve.Reason)
		}
		s.count("refused_" + reason)
		// One answer for every refusal, so a probe learns nothing about which check failed.
		writeJSON(w, 400, map[string]string{"error": "delivery_refused"})
		return
	}
	out, _, err := s.st.Add(d.ID, d.Timestamp, body, now)
	if err != nil {
		s.count("store_errors")
		writeJSON(w, 500, map[string]string{"error": "not_stored"}) // the sender retries; nothing was acknowledged
		return
	}
	switch out {
	case store.Conflict:
		s.count("refused_id_conflict")
		writeJSON(w, 409, map[string]string{"error": "id_reused_with_a_different_body"})
	case store.Duplicate:
		s.count("duplicates")
		writeJSON(w, 200, map[string]string{"status": "duplicate"})
	default:
		s.count("stored")
		writeJSON(w, 200, map[string]string{"status": "stored"})
	}
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		if !ok || subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) != 1 {
			s.count("pull_unauthorized")
			w.Header().Set("WWW-Authenticate", "Bearer")
			writeJSON(w, 401, map[string]string{"error": "unauthorized"})
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			writeJSON(w, 405, map[string]string{"error": "method_not_allowed"})
			return
		}
		next(w, r)
	}
}

func (s *Server) deliveries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, limit := int64(0), defaultLimit
	if v := q.Get("after"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, 400, map[string]string{"error": "bad_after"})
			return
		}
		after = n
	}
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxLimit {
			writeJSON(w, 400, map[string]string{"error": "bad_limit"})
			return
		}
		limit = n
	}
	items := s.st.After(after, limit)
	next := after
	if len(items) > 0 {
		next = items[len(items)-1].Seq
	}
	if items == nil {
		items = []store.Entry{}
	}
	writeJSON(w, 200, map[string]any{"deliveries": items, "next_after": next})
}

func (s *Server) stats(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	c := make(map[string]int64, len(s.counters))
	for k, v := range s.counters {
		c[k] = v
	}
	s.mu.Unlock()
	writeJSON(w, 200, map[string]any{"stored_total": s.st.Len(), "counters": c, "synthetic": true})
}
