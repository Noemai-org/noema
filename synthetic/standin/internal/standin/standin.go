// Package standin is the Noema-side stand-in for the Tushe-held US-01 and US-06 data (Option B for G1; the contract is
// benchmark/fixtures/standin.py). It is a read-store: the benchmark loads it once with the output of derive(fixture) through the control
// side, and the core's adapters read it through the data side, scoped to the calling principal. It has no write route on the data side, holds
// no state the load did not give it, and labels every answer "stand-in" and synthetic. It says nothing about how Tushe behaves.
package standin

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// Label is the data source every answer carries (AGENT-25).
	Label = "stand-in"
	// maxLoadBytes bounds a load; a fixture of the generated suite is a few kilobytes.
	maxLoadBytes = 4 << 20
	minTokenLen  = 32
	// audience binds an assertion to this service; maxAssertionLifetime keeps one short-lived (INV-12).
	audience             = "standin"
	maxAssertionLifetime = 5 * time.Minute
	assertionMACLabel    = "noema-standin-principal-v1"
)

// Options holds the two credentials and the identity key. They are read from files by the caller and must all differ: the key that loads never reads,
// the key that reads never loads, and the key that signs identities is neither. Now is the clock (nil means the system clock). ClockStart, when set,
// starts a simulated clock that only the benchmark advances (clock.go); it cannot be combined with Now.
type Options struct {
	BenchToken   string
	RuntimeToken string
	PrincipalKey string
	Now          func() time.Time
	ClockStart   time.Time
}

type principal struct {
	DisplayName   string           `json:"display_name"`
	Roles         []string         `json:"roles"`
	Accounts      []string         `json:"accounts"`
	Beneficiaries []map[string]any `json:"beneficiaries"`
}

type candidate struct {
	AccountRef     string      `json:"account_ref"`
	AmountMinor    json.Number `json:"amount_minor"`
	Counterparty   string      `json:"counterparty"`
	Currency       string      `json:"currency"`
	Direction      string      `json:"direction"`
	OccurredAt     string      `json:"occurred_at"`
	TransactionRef string      `json:"transaction_ref"`

	at     time.Time
	amount int64
}

// payload is the shape of derive(fixture). Unknown fields are refused: a changed contract must change this file too.
type payload struct {
	DataSource             string                    `json:"data_source"`
	Synthetic              bool                      `json:"synthetic"`
	FixtureID              string                    `json:"fixture_id"`
	ClockStart             string                    `json:"clock_start"`
	ExistingCases          int                       `json:"existing_cases"`
	Principals             map[string]principal      `json:"principals"`
	Candidates             map[string][]candidate    `json:"candidates"`
	CandidateListsComplete bool                      `json:"candidate_lists_complete"`
	LedgerEntries          map[string]map[string]any `json:"ledger_entries"`
	SwitchRecords          map[string]map[string]any `json:"switch_records"`
	DestinationStatuses    map[string]map[string]any `json:"destination_statuses"`
}

type loaded struct {
	p      payload
	owner  map[string]string // transaction ref -> owning principal
	digest string
}

// Server holds one stand-in.
type Server struct {
	opt Options
	sim *simClock // nil on the system clock
	mu  sync.RWMutex
	cur *loaded
}

// New checks the options and returns an empty stand-in.
func New(o Options) (*Server, error) {
	if len(o.BenchToken) < minTokenLen || len(o.RuntimeToken) < minTokenLen || len(o.PrincipalKey) < minTokenLen {
		return nil, fmt.Errorf("the bench credential, the runtime credential and the principal key are required and must be at least %d characters", minTokenLen)
	}
	if o.BenchToken == o.RuntimeToken || o.BenchToken == o.PrincipalKey || o.RuntimeToken == o.PrincipalKey {
		return nil, errors.New("the bench credential, the runtime credential and the principal key must all differ")
	}
	s := &Server{}
	switch {
	case !o.ClockStart.IsZero() && o.Now != nil:
		return nil, errors.New("set either ClockStart or Now, not both: two clocks are ambiguous")
	case !o.ClockStart.IsZero():
		s.sim = &simClock{now: o.ClockStart.UTC()}
		o.Now = s.sim.Now
	case o.Now == nil:
		o.Now = time.Now
	}
	s.opt = o
	return s, nil
}

func jsonOut(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	b, err := json.Marshal(v)
	if err != nil {
		b = []byte(`{"error":{"code":"INTERNAL","message":"could not encode the response"}}`)
	}
	_, _ = w.Write(append(b, '\n')) // a write error means the caller left; nothing is left to do
}

func fail(w http.ResponseWriter, status int, code, message string) {
	jsonOut(w, status, map[string]any{"data_source": Label, "synthetic": true, "error": map[string]any{"code": code, "message": message}})
}

func bearer(r *http.Request, want string) bool {
	h := r.Header.Get("Authorization")
	if len(h) <= 7 || !strings.EqualFold(h[:7], "Bearer ") {
		return false
	}
	got, exp := sha256.Sum256([]byte(h[7:])), sha256.Sum256([]byte(want))
	return subtle.ConstantTimeCompare(got[:], exp[:]) == 1
}

func (s *Server) current() *loaded {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cur
}

// ControlHandler is the loader's side: bench credential only.
func (s *Server) ControlHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /control/v1/standin", s.put)
	mux.HandleFunc("GET /control/v1/standin", s.status)
	mux.HandleFunc("DELETE /control/v1/standin", s.clear)
	mux.HandleFunc("GET /control/v1/clock", s.getClock)
	mux.HandleFunc("POST /control/v1/clock/advance", s.advanceClock)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Data-Source", Label)
		if !bearer(r, s.opt.BenchToken) {
			fail(w, http.StatusUnauthorized, "UNAUTHENTICATED", "a valid credential is required")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	c := s.current()
	if c == nil {
		jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "loaded": false})
		return
	}
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "loaded": true, "fixture_id": c.p.FixtureID, "digest": c.digest})
}

func (s *Server) clear(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	s.cur = nil
	s.mu.Unlock()
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "loaded": false})
}

func (s *Server) put(w http.ResponseWriter, r *http.Request) {
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxLoadBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			fail(w, http.StatusRequestEntityTooLarge, "TOO_LARGE", "the load is too large")
			return
		}
		fail(w, http.StatusBadRequest, "BAD_REQUEST", "the load could not be read")
		return
	}
	c, err := parse(raw)
	if err != nil {
		fail(w, http.StatusBadRequest, "BAD_LOAD", err.Error())
		return
	}
	s.mu.Lock()
	s.cur = c
	s.mu.Unlock()
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "loaded": true, "fixture_id": c.p.FixtureID, "digest": c.digest})
}

// parse checks a load against the contract and builds the read index. A load that is not whole is refused, never repaired.
func parse(raw []byte) (*loaded, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	dec.UseNumber()
	var p payload
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("not a stand-in load: %v", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("not a stand-in load: data after the document")
	}
	if p.DataSource != Label || !p.Synthetic {
		return nil, fmt.Errorf("only a load labelled %q and synthetic is accepted", Label)
	}
	if p.FixtureID == "" {
		return nil, errors.New("the load has no fixture_id")
	}
	owner := map[string]string{}
	for who, list := range p.Candidates {
		pr, ok := p.Principals[who]
		if !ok {
			return nil, fmt.Errorf("candidates are listed for %q, who is not a principal of the load", who)
		}
		accounts := map[string]bool{}
		for _, a := range pr.Accounts {
			accounts[a] = true
		}
		for i := range list {
			c := &list[i]
			if c.TransactionRef == "" {
				return nil, errors.New("a candidate has no transaction_ref")
			}
			if prev, dup := owner[c.TransactionRef]; dup {
				return nil, fmt.Errorf("transaction %s is listed for both %q and %q", c.TransactionRef, prev, who)
			}
			owner[c.TransactionRef] = who
			if !accounts[c.AccountRef] {
				return nil, fmt.Errorf("transaction %s names account %q, which %q does not own", c.TransactionRef, c.AccountRef, who)
			}
			at, err := time.Parse(time.RFC3339, c.OccurredAt)
			if err != nil {
				return nil, fmt.Errorf("transaction %s has an occurred_at that is not RFC 3339", c.TransactionRef)
			}
			amount, err := strconv.ParseInt(c.AmountMinor.String(), 10, 64)
			if err != nil || amount < 0 {
				return nil, fmt.Errorf("transaction %s has an amount_minor that is not a non-negative integer", c.TransactionRef)
			}
			c.at, c.amount = at, amount
		}
	}
	for name, m := range map[string]map[string]map[string]any{"ledger_entries": p.LedgerEntries, "switch_records": p.SwitchRecords, "destination_statuses": p.DestinationStatuses} {
		for ref := range m {
			if _, ok := owner[ref]; !ok {
				return nil, fmt.Errorf("%s holds %s, which no candidate list names", name, ref)
			}
		}
		for ref := range owner {
			if _, ok := m[ref]; !ok {
				return nil, fmt.Errorf("%s has no answer for %s", name, ref)
			}
		}
	}
	canon, err := json.Marshal(p)
	if err != nil {
		return nil, errors.New("the load could not be re-encoded")
	}
	sum := sha256.Sum256(canon)
	return &loaded{p: p, owner: owner, digest: "sha256:" + hex.EncodeToString(sum[:])}, nil
}

// DataHandler is the adapters' side: runtime credential, and the calling principal in X-Principal. Reads only.
func (s *Server) DataHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /standin/v1/principal", s.scoped(s.principal))
	mux.HandleFunc("GET /standin/v1/beneficiaries", s.scoped(s.beneficiaries))
	mux.HandleFunc("GET /standin/v1/transactions", s.scoped(s.search))
	mux.HandleFunc("GET /standin/v1/transactions/{ref}/ledger-entries", s.scoped(s.perRef(func(c *loaded) map[string]map[string]any { return c.p.LedgerEntries })))
	mux.HandleFunc("GET /standin/v1/transactions/{ref}/switch-status", s.scoped(s.perRef(func(c *loaded) map[string]map[string]any { return c.p.SwitchRecords })))
	mux.HandleFunc("GET /standin/v1/transactions/{ref}/destination-status", s.scoped(s.perRef(func(c *loaded) map[string]map[string]any { return c.p.DestinationStatuses })))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Data-Source", Label)
		if !bearer(r, s.opt.RuntimeToken) {
			fail(w, http.StatusUnauthorized, "UNAUTHENTICATED", "a valid credential is required")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

type scopedHandler func(w http.ResponseWriter, r *http.Request, c *loaded, who string)

// principalOf checks the caller's identity assertion and returns the principal it binds. The assertion is minted by the trusted adapter from the
// authenticated session (INV-12):
//
//	X-Principal-Assertion: v1.<base64url(principal)>.<expiry, unix seconds>.<hex HMAC-SHA-256>
//	MAC = HMAC-SHA-256 keyed with the principal key over four lines: noema-standin-principal-v1, standin, the principal, the expiry
//
// It is refused if it is missing, malformed, signed with another key or for another audience, expired, or valid for longer than five minutes.
// The runtime credential alone identifies no one, and a bare X-Principal header is ignored.
func (s *Server) principalOf(r *http.Request) (string, bool) {
	parts := strings.Split(r.Header.Get("X-Principal-Assertion"), ".")
	if len(parts) != 4 || parts[0] != "v1" {
		return "", false
	}
	who, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(who) == 0 {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || strconv.FormatInt(exp, 10) != parts[2] {
		return "", false
	}
	now := s.opt.Now()
	if now.Unix() >= exp || time.Unix(exp, 0).Sub(now) > maxAssertionLifetime {
		return "", false
	}
	got, err := hex.DecodeString(parts[3])
	if err != nil {
		return "", false
	}
	m := hmac.New(sha256.New, []byte(s.opt.PrincipalKey))
	_, _ = fmt.Fprintf(m, "%s\n%s\n%s\n%d", assertionMACLabel, audience, who, exp)
	if !hmac.Equal(got, m.Sum(nil)) {
		return "", false
	}
	return string(who), true
}

// scoped refuses a read until something is loaded, and then until the caller's assertion names a principal of the load (INV-12).
func (s *Server) scoped(h scopedHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		c := s.current()
		if c == nil {
			fail(w, http.StatusServiceUnavailable, "STANDIN_NOT_LOADED", "no stand-in data is loaded")
			return
		}
		who, ok := s.principalOf(r)
		if _, known := c.p.Principals[who]; !ok || !known {
			fail(w, http.StatusUnauthorized, "UNAUTHENTICATED", "a valid identity assertion for a known principal is required")
			return
		}
		h(w, r, c, who)
	}
}

func notFound(w http.ResponseWriter) { fail(w, http.StatusNotFound, "NOT_FOUND", "no such resource") }

func (s *Server) principal(w http.ResponseWriter, _ *http.Request, c *loaded, who string) {
	p := c.p.Principals[who]
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "principal_id": who, "display_name": p.DisplayName, "roles": p.Roles, "accounts": p.Accounts, "beneficiaries": p.Beneficiaries})
}

func (s *Server) beneficiaries(w http.ResponseWriter, _ *http.Request, c *loaded, who string) {
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "beneficiaries": c.p.Principals[who].Beneficiaries})
}

func (s *Server) search(w http.ResponseWriter, r *http.Request, c *loaded, who string) {
	q := r.URL.Query()
	if account := q.Get("account"); account != "" {
		own := false
		for _, a := range c.p.Principals[who].Accounts {
			own = own || a == account
		}
		if !own {
			notFound(w) // another principal's account looks absent
			return
		}
	}
	bound := func(name string) (time.Time, bool, bool) {
		v := q.Get(name)
		if v == "" {
			return time.Time{}, false, true
		}
		t, err := time.Parse(time.RFC3339, v)
		return t, true, err == nil
	}
	amount := func(name string) (int64, bool, bool) {
		v := q.Get(name)
		if v == "" {
			return 0, false, true
		}
		n, err := strconv.ParseInt(v, 10, 64)
		return n, true, err == nil && n >= 0 && strconv.FormatInt(n, 10) == v
	}
	from, hasFrom, okFrom := bound("from")
	to, hasTo, okTo := bound("to")
	lo, hasLo, okLo := amount("amount_min")
	hi, hasHi, okHi := amount("amount_max")
	if !okFrom || !okTo || !okLo || !okHi {
		fail(w, http.StatusBadRequest, "BAD_REQUEST", "from and to are RFC 3339 times; amount_min and amount_max are non-negative integers in minor units")
		return
	}
	counterparty := q.Get("counterparty")
	items := []candidate{}
	for _, cand := range c.p.Candidates[who] {
		switch {
		case q.Get("account") != "" && cand.AccountRef != q.Get("account"),
			hasFrom && cand.at.Before(from), hasTo && cand.at.After(to),
			hasLo && cand.amount < lo, hasHi && cand.amount > hi,
			counterparty != "" && cand.Counterparty != counterparty:
			continue
		}
		items = append(items, cand)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if !items[i].at.Equal(items[j].at) {
			return items[i].at.After(items[j].at)
		}
		return items[i].TransactionRef < items[j].TransactionRef
	})
	jsonOut(w, 200, map[string]any{"data_source": Label, "synthetic": true, "items": items, "complete": c.p.CandidateListsComplete})
}

func (s *Server) perRef(pick func(*loaded) map[string]map[string]any) scopedHandler {
	return func(w http.ResponseWriter, r *http.Request, c *loaded, who string) {
		ref := r.PathValue("ref")
		if c.owner[ref] != who { // not owned looks like not found
			notFound(w)
			return
		}
		out := map[string]any{}
		for k, v := range pick(c)[ref] {
			out[k] = v
		}
		out["data_source"], out["synthetic"] = Label, true
		jsonOut(w, 200, out)
	}
}
