package verifier_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/predicate"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

const (
	tenant  = "ten_aa"
	rcpt    = "rcp_0001"
	key     = "idk_v1_abc"
	subject = "TX1"
	intent  = "int_1"
	corr    = "corr_abc123"
	princ   = "usr_100"
	caseOK  = "fin.outcome.exception.case_opened"
)

var t0 = time.Date(2026, 11, 3, 9, 0, 0, 0, time.UTC) // SYNTHETIC

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// waiter advances the simulated clock, so backoffs cost no real time and the test can read what was waited.
type waiter struct {
	clk   *clock.Manual
	waits []time.Duration
}

func (w *waiter) Wait(_ context.Context, d time.Duration) error {
	w.waits = append(w.waits, d)
	w.clk.Advance(d)
	return nil
}

type step struct {
	page verifier.CasePage
	err  error
}

// reader plays a scripted sequence per kind of query. It is used for decision-logic tables, including answers the real
// mock-case cannot produce (two cases for one key); fault evidence comes from the end-to-end tests with real FaultPlans.
type reader struct {
	mu      sync.Mutex
	byKey   []step
	byTx    []step
	queries []verifier.CaseQuery
}

func (r *reader) FindCases(_ context.Context, q verifier.CaseQuery) (verifier.CasePage, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.queries = append(r.queries, q)
	script := &r.byTx
	if q.IdempotencyKey != "" {
		script = &r.byKey
	}
	if len(*script) == 0 {
		return verifier.CasePage{}, &verifier.ReadError{Kind: verifier.ReadUnavailable}
	}
	s := (*script)[0]
	if len(*script) > 1 {
		*script = (*script)[1:]
	}
	return s.page, s.err
}

func (r *reader) transactionQuery() (verifier.CaseQuery, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, q := range r.queries {
		if q.IdempotencyKey == "" {
			return q, true
		}
	}
	return verifier.CaseQuery{}, false
}

func (r *reader) reads() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.queries) }

func complete(cs ...verifier.Case) step {
	return step{page: verifier.CasePage{Items: cs, Complete: true}}
}
func down() step { return step{err: &verifier.ReadError{Kind: verifier.ReadUnavailable}} }

func theCase() verifier.Case {
	return verifier.Case{CaseID: "case_1", TenantID: tenant, PrincipalID: princ, IdempotencyKey: key, TransactionRef: subject, Status: "OPEN", OwnerQueue: "PAYMENTS_OPERATIONS", CreatedByIntent: intent, Epoch: 1}
}

type memStore struct {
	receipt verifier.Receipt
	loadErr error
	putErr  error
	writes  []verifier.Write
	found   map[string]verifier.Found
	race    map[string]verifier.Found // appear when the insert is attempted: another writer won between lookup and insert
}

func (m *memStore) LoadReceipt(_ context.Context, tenantID, receiptID string) (verifier.Receipt, error) {
	if m.loadErr != nil {
		return verifier.Receipt{}, m.loadErr
	}
	if tenantID != m.receipt.TenantID || receiptID != m.receipt.ReceiptID {
		return verifier.Receipt{}, errors.New("no such receipt")
	}
	return m.receipt, nil
}

func (m *memStore) FindVerdict(_ context.Context, tenantID, verificationID string) (verifier.Found, bool, error) {
	f, ok := m.found[tenantID+"/"+verificationID]
	return f, ok, nil
}

func (m *memStore) RecordVerdict(_ context.Context, w verifier.Write) error {
	if m.putErr != nil {
		return m.putErr
	}
	k := w.Verdict.TenantID + "/" + w.Verdict.VerificationID
	if m.race != nil {
		if m.found == nil {
			m.found = map[string]verifier.Found{}
		}
		for rk, rv := range m.race {
			m.found[rk] = rv
		}
		m.race = nil
		return verifier.ErrAlreadyVerified
	}
	if _, taken := m.found[k]; taken {
		return verifier.ErrAlreadyVerified
	}
	if m.found == nil {
		m.found = map[string]verifier.Found{}
	}
	m.found[k] = verifier.Found{Result: verifier.Result{VerdictID: w.Verdict.VerdictID, Verdict: w.Verdict.Verdict, Reason: w.Verdict.Reason}, ReceiptID: w.Verdict.ReceiptID}
	m.writes = append(m.writes, w)
	return nil
}

type signer struct {
	err    error
	signed [][]byte
}

func (s *signer) Sign(b []byte) (verifier.Sig, error) {
	if s.err != nil {
		return verifier.Sig{}, s.err
	}
	s.signed = append(s.signed, b)
	sum := sha256.Sum256(b)
	return verifier.Sig{KeyID: "k_outcome_test", Value: hex.EncodeToString(sum[:])}, nil
}

type authenticity struct{ result verifier.Authenticity }

func (a authenticity) Check(context.Context, verifier.Receipt) (verifier.Authenticity, error) {
	return a.result, nil
}

type world struct {
	clk   *clock.Manual
	wait  *waiter
	read  *reader
	store *memStore
	sign  *signer
	auth  authenticity
	v     *verifier.Verifier
	req   verifier.Request
}

// budget is the PERD 29.2 value (3 retries after 1, 3 and 9 s; DEC-2026-10-17): configuration, supplied here as the
// benchmark configuration would supply it.
var budget = verifier.ReadBudget{Backoffs: []time.Duration{time.Second, 3 * time.Second, 9 * time.Second}, MaxPages: 3}

func newWorld(t *testing.T, mutate ...func(*world)) *world {
	t.Helper()
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := predicate.LoadPack(raw)
	if err != nil {
		t.Fatal(err)
	}
	def := pack.Definitions[caseOK]
	clk := clock.NewManual(t0.Add(time.Duration(def.SettleWindowSecs) * time.Second)) // exactly at the end of the settle window
	w := &world{clk: clk, wait: &waiter{clk: clk}, read: &reader{byKey: []step{complete(theCase())}, byTx: []step{complete(theCase())}},
		sign: &signer{}, auth: authenticity{verifier.AuthNotChecked},
		store: &memStore{receipt: verifier.Receipt{ReceiptID: rcpt, TenantID: tenant, GrantID: "grt_0001", PrincipalID: princ, Attempt: 1, IdempotencyKey: key, LogicalAction: "ESCALATE_CASE",
			State: "OUTCOME_UNKNOWN", CorrelationID: corr, DispatchedAt: t0}},
		req: verifier.Request{TenantID: tenant, ReceiptID: rcpt, VerificationID: "vrf_0001", TransactionRef: subject, QueryableByKey: true,
			Bindings:   map[string]string{"subject_resource": subject, "intent_id": intent},
			Definition: verifier.Definition{OutcomeID: def.OutcomeID, SuccessPredicate: def.SuccessPredicate, SettleWindowSeconds: def.SettleWindowSecs}}}
	for _, m := range mutate {
		m(w)
	}
	w.build(t, pack)
	return w
}

func (w *world) build(t *testing.T, pack predicate.Pack) {
	t.Helper()
	v, err := verifier.New(verifier.Config{Clock: w.clk, Waiter: w.wait, IDs: ids.New(w.clk, nil), Cases: w.read, Store: w.store, Signer: w.sign,
		Authenticity: authenticityPort{w.auth}, Scope: pack.Scope, Budget: budget})
	if err != nil {
		t.Fatal(err)
	}
	w.v = v
}

type authenticityPort struct{ authenticity }

func (w *world) verify(t *testing.T) (verifier.Result, error) {
	t.Helper()
	return w.v.Verify(ctx(t), w.req)
}

// decided runs Verify and requires one stored verdict.
func (w *world) decided(t *testing.T) (verifier.Result, verifier.Write) {
	t.Helper()
	res, err := w.verify(t)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(w.store.writes) != 1 {
		t.Fatalf("stored %d verdicts, want 1", len(w.store.writes))
	}
	return res, w.store.writes[0]
}

func cfgWithout(t *testing.T, cut func(*verifier.Config)) verifier.Config {
	t.Helper()
	w := newWorld(t)
	c := verifier.Config{Clock: w.clk, Waiter: w.wait, IDs: ids.New(w.clk, nil), Cases: w.read, Store: w.store, Signer: w.sign, Authenticity: authenticityPort{w.auth}, Budget: budget}
	cut(&c)
	return c
}

// at starts the simulated clock offset after the dispatch (the manual clock never runs backwards, so it is set, not stepped).
func at(offset time.Duration) func(*world) {
	return func(w *world) {
		w.clk = clock.NewManual(t0.Add(offset))
		w.wait.clk = w.clk
	}
}
