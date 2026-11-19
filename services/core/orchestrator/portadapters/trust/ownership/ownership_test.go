package ownershipport_test

// TRU-07 (A329, A344: the Ownership adapter of the Trust intake, T8): REQ-TRU-002, REQ-TRU-003; INV-11, INV-12, INV-19; AGENT-13, AGENT-16; A347.
// Ownership is a principal-scoped read through the enterprise read port of the candidates read (candidates-request.v1 in, candidates-response.v1 out). The store below is a real
// reader of the contract: it scopes its rows by the tenant and the principal of the request, filters by resource kind and window, orders newest first and stops at the contract's
// twenty. The faults are real: a reader that returns an error, a context that has expired, and answers that are out of the contract. SYNTHETIC.

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	ownershipport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/ownership"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

const (
	tenant        = "ten_demo"
	foreignTenant = "ten_other"
	alice         = "usr_alice"
	bob           = "usr_bob"
	corr          = "corr_synth01"
	kind          = "fin.transaction"
	otherKind     = "fin.account"
	ownNode       = "fin.intent.lookup"
	freeNode      = "fin.intent.free"
	leak          = "row-secret-TX0001-ten_demo-usr_alice-postgres://u:pw@db"
	capRows       = 20 // the contract's maxItems of candidates-response.v1
)

var _ intake.Ownership = ownershipport.Ownership{}

// ---- the store: a reader of the contract that scopes by the request ----

type owned struct {
	tenant, principal string
	c                 contracts.CandidatesResponseV1Candidate
}

type store struct {
	mu     sync.Mutex
	rows   []owned
	calls  []contracts.CandidatesRequestV1
	fail   error
	tamper func(*contracts.CandidatesResponseV1)
}

func (s *store) Read(ctx context.Context, req contracts.CandidatesRequestV1) (contracts.CandidatesResponseV1, error) {
	if err := ctx.Err(); err != nil {
		return contracts.CandidatesResponseV1{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, req)
	if s.fail != nil {
		return contracts.CandidatesResponseV1{}, s.fail
	}
	from, _ := time.Parse(time.RFC3339Nano, string(req.Filters.TimeWindow.From))
	to, _ := time.Parse(time.RFC3339Nano, string(req.Filters.TimeWindow.To))
	var out []contracts.CandidatesResponseV1Candidate
	for _, r := range s.rows {
		at, _ := time.Parse(time.RFC3339Nano, string(r.c.OccurredAt))
		if string(contracts.CommonV1TenantId(r.tenant)) != string(req.TenantId) || string(contracts.CommonV1PrincipalId(r.principal)) != string(req.PrincipalRef) ||
			!strings.HasPrefix(string(r.c.ResourceRef), req.ResourceKind+":") || at.Before(from) || at.After(to) {
			continue
		}
		out = append(out, r.c)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].OccurredAt != out[j].OccurredAt {
			return out[i].OccurredAt > out[j].OccurredAt
		}
		return out[i].CandidateId < out[j].CandidateId
	})
	if len(out) > capRows {
		out = out[:capRows]
	}
	resp := contracts.CandidatesResponseV1{SchemaVersion: "1.0", TenantId: req.TenantId, CorrelationId: req.CorrelationId, Candidates: out}
	if s.tamper != nil {
		s.tamper(&resp)
	}
	return resp, nil
}

func (s *store) reads() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func cand(id string, k string, at time.Time) contracts.CandidatesResponseV1Candidate {
	return contracts.CandidatesResponseV1Candidate{
		CandidateId: "cand_" + id, ResourceRef: contracts.CommonV1ResourceRef(k + ":" + id), AmountMinor: 1000, Currency: contracts.MoneyV1CurrencyCodeNGN,
		OccurredAt: contracts.CommonV1Timestamp(at.UTC().Format(time.RFC3339Nano)), CounterpartyDisplay: "Synthetic Shop", AccountLast4: "1234",
		Direction: contracts.CandidatesResponseV1CandidateDirectionOUTGOING,
	}
}

var base = time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)

func newStore() *store {
	return &store{rows: []owned{
		{tenant, alice, cand("TX0001", kind, base)},
		{tenant, alice, cand("TX0002", kind, base.Add(time.Minute))},
		{tenant, bob, cand("TX0003", kind, base.Add(2*time.Minute))},
		{foreignTenant, alice, cand("TX0004", kind, base.Add(3*time.Minute))},
		{tenant, alice, cand("AC0001", otherKind, base.Add(4*time.Minute))},
		{tenant, alice, cand("ATX0007", kind, base.Add(5*time.Minute))},
		{tenant, alice, cand("TX0005", kind, time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC))},
		{tenant, alice, cand("TX0006", kind, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC))},
	}}
}

func (s *store) fill(n int) { // n more rows of alice's, newer than the others
	for i := 0; i < n; i++ {
		s.rows = append(s.rows, owned{tenant, alice, cand(fmt.Sprintf("FILL%02d", i), kind, time.Date(2200, 1, 1, 0, i, 0, 0, time.UTC))})
	}
}

func build(t *testing.T, s *store) ownershipport.Ownership {
	t.Helper()
	o, err := ownershipport.New(ownershipport.Config{TenantID: tenant, Reader: s, Nodes: map[string]ownershipport.Node{ownNode: {ResourceKind: kind}}})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return o
}

func intent(principal, ref string) intake.AdmittedIntent {
	return intake.AdmittedIntent{TenantID: tenant, IntentID: "int_synth01", Version: 1, IntentNode: ownNode, CorrelationID: corr, PrincipalID: principal, TargetRef: ref}
}

func ctxWithDeadline(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return ctx
}

func ask(t *testing.T, o ownershipport.Ownership, principal, ref string) (bool, error) {
	t.Helper()
	return o.OwnsTarget(ctxWithDeadline(t), tenant, principal, intent(principal, ref))
}

// ---- T8 answers (REQ-TRU-002, REQ-TRU-003) ----

// REQ-TRU-002 (T8): the principal's own resource is owned, and the request the reader gets is exactly the principal-scoped read of the session: the session's tenant and principal, the
// correlation of the intent, the pack's resource kind, the whole history and no other filter (INV-11, INV-12).
func TestOwnedTargetIsTrueAndTheReadIsScopedToTheSession(t *testing.T) {
	s := newStore()
	o := build(t, s)
	got, err := ask(t, o, alice, kind+":TX0001")
	if err != nil || !got {
		t.Fatalf("own resource: owned=%v err=%v, want true, nil", got, err)
	}
	want := contracts.CandidatesRequestV1{
		SchemaVersion: "1.0", TenantId: tenant, CorrelationId: corr, PrincipalRef: alice, ResourceKind: kind,
		Filters: contracts.CandidatesRequestV1Filters{TimeWindow: contracts.CandidatesRequestV1FiltersTimeWindow{From: "0001-01-01T00:00:00Z", To: "9999-12-31T23:59:59Z"}},
	}
	if s.reads() != 1 || !reflect.DeepEqual(s.calls[0], want) {
		t.Fatalf("reads=%d request=%+v, want one read of %+v", s.reads(), s.calls, want)
	}
}

// REQ-TRU-002: the read covers the principal's whole history (the oldest and the newest rows are found); a window narrower than the whole would call an old target not owned.
func TestTheWholeHistoryIsRead(t *testing.T) {
	o := build(t, newStore())
	for _, id := range []string{"TX0005", "TX0006"} {
		if got, err := ask(t, o, alice, kind+":"+id); err != nil || !got {
			t.Errorf("%s: owned=%v err=%v, want true, nil", id, got, err)
		}
	}
}

// REQ-TRU-003, INV-11, INV-12: another principal's resource, another tenant's resource, an absent resource and every malformed reference give one answer: false, with no error.
// The text of the answer is identical. The reader is never given the target, so its reply cannot differ by it.
func TestNotOwnedAbsentAndMalformedAreOneAnswer(t *testing.T) {
	refs := map[string]string{
		"another principal's":  kind + ":TX0003",
		"another tenant's":     kind + ":TX0004",
		"absent":               kind + ":TX9999",
		"another kind":         otherKind + ":AC0001",
		"empty":                "",
		"no kind":              "TX0001",
		"colon only":           ":",
		"kind only":            kind + ":",
		"space in id":          kind + ":TX 0001",
		"newline in id":        kind + ":TX0001\n",
		"too long id":          kind + ":" + strings.Repeat("A", 65),
		"upper-case kind":      "Fin.transaction:TX0001",
		"id with a slash":      kind + ":TX/0001",
		"a different casing":   kind + ":tx0001",
		"a prefix of an owned": kind + ":TX000",
		"an extension":         kind + ":TX00010",
		"a suffix of an owned": kind + ":TX0007",
	}
	var seen []string
	var requests []contracts.CandidatesRequestV1
	for name, ref := range refs {
		s := newStore()
		o := build(t, s)
		got, err := ask(t, o, alice, ref)
		if got || err != nil {
			t.Errorf("%s: owned=%v err=%v, want false, nil", name, got, err)
		}
		seen = append(seen, fmt.Sprintf("%v|%v", got, err))
		requests = append(requests, s.calls...)
	}
	for _, x := range seen[1:] {
		if x != seen[0] {
			t.Fatalf("answers differ: %q and %q", x, seen[0])
		}
	}
	if len(requests) < 3 {
		t.Fatalf("only %d reads for the well-formed targets, want at least 3", len(requests))
	}
	for _, r := range requests[1:] {
		if !reflect.DeepEqual(r, requests[0]) {
			t.Fatalf("the reader was asked differently for different targets: %+v and %+v", r, requests[0])
		}
	}
}

// REQ-TRU-003: a malformed reference is answered without a read: nothing malformed goes anywhere.
func TestMalformedReferenceMakesNoRead(t *testing.T) {
	for _, ref := range []string{"", "TX0001", kind + ":", kind + ":a b", otherKind + ":AC0001"} {
		s := newStore()
		if got, err := ask(t, build(t, s), alice, ref); got || err != nil || s.reads() != 0 {
			t.Errorf("%q: owned=%v err=%v reads=%d, want false, nil, 0", ref, got, err, s.reads())
		}
	}
}

// INV-12: the same target is owned by the principal who owns it and by no one else; each answer comes from the session's own principal.
func TestOwnershipFollowsTheSessionPrincipal(t *testing.T) {
	o := build(t, newStore())
	if got, err := ask(t, o, bob, kind+":TX0003"); err != nil || !got {
		t.Errorf("bob's own: owned=%v err=%v, want true, nil", got, err)
	}
	if got, err := ask(t, o, bob, kind+":TX0001"); err != nil || got {
		t.Errorf("alice's, asked by bob: owned=%v err=%v, want false, nil", got, err)
	}
	if got, err := ask(t, o, alice, kind+":TX0003"); err != nil || got {
		t.Errorf("bob's, asked by alice: owned=%v err=%v, want false, nil", got, err)
	}
}

// REQ-TRU-002: an intent whose node has no ownership slot (the pack names none) is owned, with no read.
func TestNodeWithoutAnOwnershipSlotIsOwnedWithoutARead(t *testing.T) {
	s := newStore()
	a := intent(alice, "")
	a.IntentNode = freeNode
	got, err := build(t, s).OwnsTarget(ctxWithDeadline(t), tenant, alice, a)
	if err != nil || !got || s.reads() != 0 {
		t.Fatalf("owned=%v err=%v reads=%d, want true, nil, 0", got, err, s.reads())
	}
}

// A node that needs ownership and names no target is not owned: false, with no read.
func TestOwnershipNodeWithNoTargetIsNotOwned(t *testing.T) {
	s := newStore()
	if got, err := ask(t, build(t, s), alice, ""); got || err != nil || s.reads() != 0 {
		t.Fatalf("owned=%v err=%v reads=%d, want false, nil, 0", got, err, s.reads())
	}
}

// ---- the session (INV-11, INV-12) ----

// INV-12: the principal and tenant the intent names must be the session's. A mismatch is refused before anything is read, and the target is never checked for another principal.
func TestIntentMustBeTheSessions(t *testing.T) {
	cases := map[string]func(*intake.AdmittedIntent){
		"another principal": func(a *intake.AdmittedIntent) { a.PrincipalID = bob },
		"another tenant":    func(a *intake.AdmittedIntent) { a.TenantID = foreignTenant },
		"no intent":         func(a *intake.AdmittedIntent) { a.IntentID = "" },
		"version zero":      func(a *intake.AdmittedIntent) { a.Version = 0 },
		"no correlation":    func(a *intake.AdmittedIntent) { a.CorrelationID = "" },
		"bad correlation":   func(a *intake.AdmittedIntent) { a.CorrelationID = "c-1" },
	}
	for name, mut := range cases {
		s := newStore()
		a := intent(alice, kind+":TX0001")
		mut(&a)
		got, err := build(t, s).OwnsTarget(ctxWithDeadline(t), tenant, alice, a)
		if got || !errors.Is(err, ownershipport.ErrIntent) || s.reads() != 0 {
			t.Errorf("%s: owned=%v err=%v reads=%d, want false, ErrIntent, 0", name, got, err, s.reads())
		}
	}
}

// INV-11: a session of another tenant than the adapter's is refused and nothing is read; the adapter never reads for a tenant it was not built for.
func TestSessionOfAnotherTenantIsRefused(t *testing.T) {
	s := newStore()
	a := intent(alice, kind+":TX0004")
	a.TenantID = foreignTenant
	got, err := build(t, s).OwnsTarget(ctxWithDeadline(t), foreignTenant, alice, a)
	if got || !errors.Is(err, ownershipport.ErrTenant) || s.reads() != 0 {
		t.Fatalf("owned=%v err=%v reads=%d, want false, ErrTenant, 0", got, err, s.reads())
	}
}

// INV-11: a tenant or a principal not of the contract's shape is refused before any read.
func TestTenantAndPrincipalShapes(t *testing.T) {
	bad := []string{"", "demo", "ten_", "ten_x", "usr_alice", "ten_demo\n", "ten_" + strings.Repeat("a", 33), " ten_demo"}
	for _, tn := range bad {
		s := newStore()
		a := intent(alice, kind+":TX0001")
		a.TenantID = tn
		if got, err := build(t, s).OwnsTarget(ctxWithDeadline(t), tn, alice, a); got || !errors.Is(err, ownershipport.ErrSession) || s.reads() != 0 {
			t.Errorf("tenant %q: owned=%v err=%v reads=%d, want false, ErrSession, 0", tn, got, err, s.reads())
		}
	}
	for _, pn := range []string{"", "alice", "usr_", "usr_x", "ten_demo", "usr_alice\n", "usr_" + strings.Repeat("a", 33)} {
		s := newStore()
		a := intent(pn, kind+":TX0001")
		if got, err := build(t, s).OwnsTarget(ctxWithDeadline(t), tenant, pn, a); got || !errors.Is(err, ownershipport.ErrSession) || s.reads() != 0 {
			t.Errorf("principal %q: owned=%v err=%v reads=%d, want false, ErrSession, 0", pn, got, err, s.reads())
		}
	}
}

// INV-11: a row of another tenant is not a row of this one: the same target reference under another tenant is not owned. (The store scopes by the request's tenant.)
func TestAnotherTenantsRowIsNotOwned(t *testing.T) {
	if got, err := ask(t, build(t, newStore()), alice, kind+":TX0004"); got || err != nil {
		t.Fatalf("owned=%v err=%v, want false, nil", got, err)
	}
}

// ---- deadline and failures (AGENT-16, INV-19) ----

// AGENT-16: a context with no deadline is refused and nothing is read.
func TestDeadlineIsRequired(t *testing.T) {
	s := newStore()
	got, err := build(t, s).OwnsTarget(context.Background(), tenant, alice, intent(alice, kind+":TX0001"))
	if got || !errors.Is(err, ownershipport.ErrNoDeadline) || s.reads() != 0 {
		t.Fatalf("owned=%v err=%v reads=%d, want false, ErrNoDeadline, 0", got, err, s.reads())
	}
}

// INV-19: a context that has expired is a read failure, never "not owned". The reader really sees the expired context.
func TestExpiredContextIsAReadFailure(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()
	got, err := build(t, newStore()).OwnsTarget(ctx, tenant, alice, intent(alice, kind+":TX0001"))
	if got || !errors.Is(err, ownershipport.ErrRead) || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("owned=%v err=%v, want false, ErrRead over DeadlineExceeded", got, err)
	}
}

// INV-19: a reader that fails is an error. It is neither "not owned" (false, nil) nor ErrNotFound, which T8 turns into false.
func TestReadFailureIsAnErrorNotFalse(t *testing.T) {
	cause := errors.New(leak)
	s := newStore()
	s.fail = cause
	for _, ref := range []string{kind + ":TX0001", kind + ":TX9999", kind + ":TX0003"} {
		got, err := ask(t, build(t, s), alice, ref)
		if got || err == nil || !errors.Is(err, ownershipport.ErrRead) || !errors.Is(err, cause) || errors.Is(err, intake.ErrNotFound) {
			t.Errorf("%s: owned=%v err=%v, want false, ErrRead over the cause and not ErrNotFound", ref, got, err)
		}
	}
}

// INV-19: an answer that is full (the contract's most) and does not hold the target cannot show that the target is not owned: the target may be among the rows the cap cut off. It is
// an error, not false. A full answer that holds the target is owned; an answer one short of full that lacks it is a complete list and gives false.
func TestFullAnswerWithoutTheTargetIsInconclusive(t *testing.T) {
	s := newStore()
	s.fill(capRows) // alice now has more than the contract's most, all newer than TX0001
	o := build(t, s)
	got, err := ask(t, o, alice, kind+":TX0001")
	if got || !errors.Is(err, ownershipport.ErrInconclusive) || errors.Is(err, intake.ErrNotFound) {
		t.Fatalf("cut off: owned=%v err=%v, want false, ErrInconclusive", got, err)
	}
	if got, err := ask(t, o, alice, kind+":TX9999"); got || !errors.Is(err, ownershipport.ErrInconclusive) {
		t.Fatalf("absent, full answer: owned=%v err=%v, want false, ErrInconclusive", got, err)
	}
	if got, err := ask(t, o, alice, kind+":FILL00"); err != nil || !got {
		t.Fatalf("in a full answer: owned=%v err=%v, want true, nil", got, err)
	}
}

func TestAnswerOneShortOfFullIsComplete(t *testing.T) {
	s := &store{}
	for i := 0; i < capRows-1; i++ {
		s.rows = append(s.rows, owned{tenant, alice, cand(fmt.Sprintf("ROW%02d", i), kind, base.Add(time.Duration(i)*time.Minute))})
	}
	if got, err := ask(t, build(t, s), alice, kind+":TX9999"); got || err != nil {
		t.Fatalf("owned=%v err=%v, want false, nil", got, err)
	}
}

// ---- the answer is checked against the request (REQ-CTX-006, INV-11) ----

// An answer outside the contract or not for this request is refused whole, even when it holds the target: nothing of it is used.
func TestAnswerOutsideTheContractIsRefused(t *testing.T) {
	target := kind + ":TX0001"
	rowAt := func(r *contracts.CandidatesResponseV1, f func(c *contracts.CandidatesResponseV1Candidate)) {
		f(&r.Candidates[0])
	}
	cases := map[string]func(*contracts.CandidatesResponseV1){
		"another tenant":       func(r *contracts.CandidatesResponseV1) { r.TenantId = foreignTenant },
		"another correlation":  func(r *contracts.CandidatesResponseV1) { r.CorrelationId = "corr_other01" },
		"another major":        func(r *contracts.CandidatesResponseV1) { r.SchemaVersion = "2.0" },
		"not a version":        func(r *contracts.CandidatesResponseV1) { r.SchemaVersion = "one" },
		"no version":           func(r *contracts.CandidatesResponseV1) { r.SchemaVersion = "" },
		"more than the cap":    func(r *contracts.CandidatesResponseV1) { r.Candidates = append(r.Candidates, fillers(capRows)...) },
		"same candidate twice": func(r *contracts.CandidatesResponseV1) { r.Candidates = append(r.Candidates, r.Candidates[0]) },
		"same candidate id, another ref": func(r *contracts.CandidatesResponseV1) {
			c := cand("TX0009", kind, base)
			c.CandidateId = r.Candidates[0].CandidateId
			r.Candidates = append(r.Candidates, c)
		},
		"same ref twice": func(r *contracts.CandidatesResponseV1) {
			c := r.Candidates[0]
			c.CandidateId = "cand_other"
			r.Candidates = append(r.Candidates, c)
		},
		"a ref of another kind": func(r *contracts.CandidatesResponseV1) {
			c := cand("AC0009", otherKind, base)
			r.Candidates = append(r.Candidates, c)
		},
		"a ref off its shape": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.ResourceRef = kind + ":TX 0001" })
		},
		"a ref with no id": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.ResourceRef = kind + ":" })
		},
		"a candidate id off": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.CandidateId = "cand_" })
		},
		"no candidate id": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.CandidateId = "" })
		},
		"a negative amount": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.AmountMinor = -1 })
		},
		"an amount over max": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.AmountMinor = 9007199254740992 })
		},
		"a currency off": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.Currency = "ZZZ" })
		},
		"a time off its shape": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.OccurredAt = "2026-11-01 09:00:00" })
		},
		"a last4 off": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.AccountLast4 = "12" })
		},
		"a direction off": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.Direction = "SIDEWAYS" })
		},
		"a display with a long digit run": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.CounterpartyDisplay = "Shop 12345" })
		},
		"a display with a newline": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.CounterpartyDisplay = "Shop\nTwo" })
		},
		"no display": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.CounterpartyDisplay = "" })
		},
		"a display too long": func(r *contracts.CandidatesResponseV1) {
			rowAt(r, func(c *contracts.CandidatesResponseV1Candidate) { c.CounterpartyDisplay = strings.Repeat("x", 65) })
		},
	}
	for name, mut := range cases {
		s := newStore()
		s.tamper = func(r *contracts.CandidatesResponseV1) {
			r.Candidates = []contracts.CandidatesResponseV1Candidate{cand("TX0001", kind, base)}
			mut(r)
		}
		got, err := ask(t, build(t, s), alice, target)
		if got || !errors.Is(err, ownershipport.ErrBadAnswer) || errors.Is(err, intake.ErrNotFound) {
			t.Errorf("%s: owned=%v err=%v, want false, ErrBadAnswer", name, got, err)
		}
	}
}

func fillers(n int) []contracts.CandidatesResponseV1Candidate {
	out := make([]contracts.CandidatesResponseV1Candidate, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, cand(fmt.Sprintf("FIL%02d", i), kind, base.Add(time.Duration(i)*time.Hour)))
	}
	return out
}

// The contract's own bounds are accepted: the most rows, the largest amount, the longest display.
func TestAnswerAtTheContractsBoundsIsAccepted(t *testing.T) {
	s := newStore()
	s.tamper = func(r *contracts.CandidatesResponseV1) {
		rows := fillers(capRows - 1)
		top := cand("TX0001", kind, base)
		top.AmountMinor = 9007199254740991
		top.CounterpartyDisplay = strings.Repeat("x", 64-4) + "1234"
		top.AccountLast4 = "0000"
		top.Direction = contracts.CandidatesResponseV1CandidateDirectionINCOMING
		r.Candidates = append(rows, top)
		r.SchemaVersion = "1.7"
	}
	if got, err := ask(t, build(t, s), alice, kind+":TX0001"); err != nil || !got {
		t.Fatalf("owned=%v err=%v, want true, nil", got, err)
	}
}

// ---- errors (AGENT-13, A347) ----

// AGENT-13, A347: every error has fixed public text. None carries the tenant, the principal, a reference, a row, the text of the cause or a driver's text; the cause is reachable only
// through errors.Is and errors.As. No error is the intake's ErrNotFound, which T8 would turn into "not owned".
func TestErrorsHaveFixedTextAndKeepTheCause(t *testing.T) {
	type tc struct {
		name string
		want error
		run  func() error
	}
	cause := &causeError{msg: leak}
	failing := newStore()
	failing.fail = cause
	badAnswer := newStore()
	badAnswer.tamper = func(r *contracts.CandidatesResponseV1) { r.TenantId = foreignTenant }
	full := newStore()
	full.fill(capRows)
	wrongIntent := intent(alice, kind+":TX0001")
	wrongIntent.PrincipalID = bob
	call := func(s *store, a intake.AdmittedIntent, ctx context.Context, tn, pn string) error {
		_, err := build(t, s).OwnsTarget(ctx, tn, pn, a)
		return err
	}
	cases := []tc{
		{"no deadline", ownershipport.ErrNoDeadline, func() error {
			return call(newStore(), intent(alice, kind+":TX0001"), context.Background(), tenant, alice)
		}},
		{"session", ownershipport.ErrSession, func() error {
			return call(newStore(), intent(alice, kind+":TX0001"), ctxWithDeadline(t), "x", alice)
		}},
		{"tenant", ownershipport.ErrTenant, func() error {
			return call(newStore(), intent(alice, kind+":TX0001"), ctxWithDeadline(t), foreignTenant, alice)
		}},
		{"intent", ownershipport.ErrIntent, func() error { return call(newStore(), wrongIntent, ctxWithDeadline(t), tenant, alice) }},
		{"read", ownershipport.ErrRead, func() error {
			return call(failing, intent(alice, kind+":TX0001"), ctxWithDeadline(t), tenant, alice)
		}},
		{"bad answer", ownershipport.ErrBadAnswer, func() error {
			return call(badAnswer, intent(alice, kind+":TX0001"), ctxWithDeadline(t), tenant, alice)
		}},
		{"inconclusive", ownershipport.ErrInconclusive, func() error {
			return call(full, intent(alice, kind+":TX0001"), ctxWithDeadline(t), tenant, alice)
		}},
	}
	texts := map[string]string{}
	for _, c := range cases {
		err := c.run()
		if err == nil || !errors.Is(err, c.want) {
			t.Errorf("%s: err=%v, want %v", c.name, err, c.want)
			continue
		}
		if err.Error() != c.want.Error() {
			t.Errorf("%s: text %q is not the fixed text %q", c.name, err.Error(), c.want.Error())
		}
		for _, banned := range []string{"ten_", "usr_", "corr_", "cand_", "TX0001", "TX0003", "TX9999", "postgres", "row-secret", "secret", "pw@", "fin.", leak} {
			if strings.Contains(err.Error(), banned) {
				t.Errorf("%s: text %q carries %q", c.name, err.Error(), banned)
			}
		}
		if errors.Is(err, intake.ErrNotFound) {
			t.Errorf("%s: matches intake.ErrNotFound", c.name)
		}
		if prev, dup := texts[err.Error()]; dup {
			t.Errorf("%s and %s share the text %q", c.name, prev, err.Error())
		}
		texts[err.Error()] = c.name
		if !strings.HasPrefix(err.Error(), "ownershipport: ") {
			t.Errorf("%s: text %q lacks the package prefix", c.name, err.Error())
		}
	}
	err := cases[4].run()
	var ce *causeError
	if !errors.Is(err, cause) || !errors.As(err, &ce) || ce != cause {
		t.Errorf("the cause of a read failure is not reachable: %v", err)
	}
	if strings.Contains(fmt.Sprintf("%v %s %q", err, err, err), leak) {
		t.Errorf("a format of the error prints its cause")
	}
}

type causeError struct{ msg string }

func (e *causeError) Error() string { return e.msg }

// ---- wiring (AGENT-16) ----

// AGENT-16: nothing has a default. A missing reader, a tenant off its shape, no node, a node with no or a malformed kind, or a node with no name is ErrConfig; the zero value refuses
// every call.
func TestConfigIsRefusedWhenNotUsable(t *testing.T) {
	good := func() ownershipport.Config {
		return ownershipport.Config{TenantID: tenant, Reader: newStore(), Nodes: map[string]ownershipport.Node{ownNode: {ResourceKind: kind}}}
	}
	cases := map[string]func(*ownershipport.Config){
		"no reader":         func(c *ownershipport.Config) { c.Reader = nil },
		"no tenant":         func(c *ownershipport.Config) { c.TenantID = "" },
		"tenant off shape":  func(c *ownershipport.Config) { c.TenantID = "demo" },
		"no nodes":          func(c *ownershipport.Config) { c.Nodes = nil },
		"empty nodes":       func(c *ownershipport.Config) { c.Nodes = map[string]ownershipport.Node{} },
		"node with no name": func(c *ownershipport.Config) { c.Nodes = map[string]ownershipport.Node{"": {ResourceKind: kind}} },
		"node with no kind": func(c *ownershipport.Config) { c.Nodes = map[string]ownershipport.Node{ownNode: {}} },
		"kind off its shape": func(c *ownershipport.Config) {
			c.Nodes = map[string]ownershipport.Node{ownNode: {ResourceKind: "Fin.Transaction"}}
		},
		"kind with a colon": func(c *ownershipport.Config) {
			c.Nodes = map[string]ownershipport.Node{ownNode: {ResourceKind: kind + ":x"}}
		},
		"one bad of two": func(c *ownershipport.Config) {
			c.Nodes = map[string]ownershipport.Node{ownNode: {ResourceKind: kind}, freeNode: {ResourceKind: "x"}}
		},
	}
	for name, mut := range cases {
		c := good()
		mut(&c)
		if _, err := ownershipport.New(c); !errors.Is(err, ownershipport.ErrConfig) {
			t.Errorf("%s: err=%v, want ErrConfig", name, err)
		}
	}
	if _, err := ownershipport.New(good()); err != nil {
		t.Errorf("a good config: %v", err)
	}
	if got, err := (ownershipport.Ownership{}).OwnsTarget(ctxWithDeadline(t), tenant, alice, intent(alice, kind+":TX0001")); got || !errors.Is(err, ownershipport.ErrConfig) {
		t.Errorf("zero value: owned=%v err=%v, want false, ErrConfig", got, err)
	}
}

// The pack's data is copied at wiring: a later edit by the caller changes nothing here.
func TestNodesAreCopiedAtWiring(t *testing.T) {
	nodes := map[string]ownershipport.Node{ownNode: {ResourceKind: kind}}
	o, err := ownershipport.New(ownershipport.Config{TenantID: tenant, Reader: newStore(), Nodes: nodes})
	if err != nil {
		t.Fatal(err)
	}
	nodes[ownNode] = ownershipport.Node{ResourceKind: otherKind}
	nodes[freeNode] = ownershipport.Node{ResourceKind: kind}
	if got, err := ask(t, o, alice, kind+":TX0001"); err != nil || !got {
		t.Errorf("after the edit of the map: owned=%v err=%v, want true, nil", got, err)
	}
	a := intent(alice, "")
	a.IntentNode = freeNode
	if got, err := o.OwnsTarget(ctxWithDeadline(t), tenant, alice, a); err != nil || !got {
		t.Errorf("a node added later has no ownership slot: owned=%v err=%v, want true, nil", got, err)
	}
}

// ---- concurrency ----

// The adapter holds no state between calls: concurrent calls of two principals each get their own answer (run with -race).
func TestConcurrentCallsKeepTheirOwnAnswers(t *testing.T) {
	s := newStore()
	o := build(t, s)
	ctx := ctxWithDeadline(t)
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			p, ref, want := alice, kind+":TX0001", true
			if i%2 == 1 {
				p, ref, want = bob, kind+":TX0001", false
			}
			got, err := o.OwnsTarget(ctx, tenant, p, intent(p, ref))
			if err != nil || got != want {
				t.Errorf("%s: owned=%v err=%v, want %v, nil", p, got, err, want)
			}
		}(i)
	}
	wg.Wait()
}
