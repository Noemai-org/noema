package epochport

// UND-04, the epoch wiring (rulings A520, A513, A482, interim, relayed, not yet committed; REQ-INT-001, REQ-SEC-004; INV-12, INV-14, INV-19): an adapter turns the Interpreter's
// Epochs port into portread.Reader.NextEpoch, and Subject and Epochs are set together or not at all. The case source is a real implementation of adapters.Case that serves the
// cases it was given; the faults are real answers (an incomplete page, a failing read). Identifiers are SYNTHETIC.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/portread"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// caseSource is a case system that holds cases and answers FindCases from them. SYNTHETIC.
type caseSource struct {
	mu       sync.Mutex
	cases    []adapters.CaseRecord
	complete bool
	err      error
	queries  []adapters.CaseQuery
}

func (s *caseSource) CreateCase(context.Context, adapters.CreateCaseInput) adapters.WriteResult {
	return adapters.WriteResult{}
}
func (s *caseSource) RouteCase(context.Context, adapters.RouteCaseInput) adapters.WriteResult {
	return adapters.WriteResult{}
}
func (s *caseSource) GetCase(context.Context, string, string) (adapters.CaseRecord, error) {
	return adapters.CaseRecord{}, errors.New("unused")
}
func (s *caseSource) FindCases(_ context.Context, q adapters.CaseQuery) (adapters.CasePage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.queries = append(s.queries, q)
	if s.err != nil {
		return adapters.CasePage{}, s.err
	}
	var items []adapters.CaseRecord
	for _, c := range s.cases {
		if c.TenantID == q.TenantID && c.PrincipalID == q.PrincipalID && c.TransactionRef == q.TransactionRef {
			items = append(items, c)
		}
	}
	page := adapters.CasePage{Items: items, Complete: s.complete}
	if !s.complete {
		page.NextCursor = "next"
	}
	return page, nil
}

func (s *caseSource) calls() int { s.mu.Lock(); defer s.mu.Unlock(); return len(s.queries) }

const (
	epTenant    = "ten_aa"
	epPrincipal = "usr_ops1"
	epKind      = "fin.transaction"
)

func epCase(id, txn, status string, epoch int64) adapters.CaseRecord {
	return adapters.CaseRecord{CaseID: id, Status: status, TenantID: epTenant, PrincipalID: epPrincipal, TransactionRef: txn, Epoch: epoch}
}

func epSetup(t *testing.T, src *caseSource) Setup {
	t.Helper()
	r := &portread.Reader{Cases: src, ReadTimeout: time.Second, Clock: clock.NewManual(time.Unix(1_800_000_000, 0))}
	return Setup{Cases: r, Kind: epKind, Timeout: 2 * time.Second}
}

func epAdapter(t *testing.T, src *caseSource) *Epochs {
	t.Helper()
	a, err := New(epSetup(t, src))
	if err != nil {
		t.Fatalf("newCaseEpochs: %v", err)
	}
	return a
}

// The subject ref is `<vertical>.<kind>:<id>`; the case list is keyed by the id, in the session's tenant and principal (INV-12). The highest CLOSED epoch plus one.
func TestEpochOfAsksTheCaseListByTheTransactionInTheSessionsScope(t *testing.T) {
	src := &caseSource{complete: true, cases: []adapters.CaseRecord{epCase("cas_1", "txn_1", "CLOSED", 1), epCase("cas_2", "txn_1", "CLOSED", 2), epCase("cas_3", "txn_1", "OPEN", 3), epCase("cas_4", "txn_2", "CLOSED", 9)}}
	got, err := epAdapter(t, src).EpochOf(context.Background(), epTenant, epPrincipal, epKind+":txn_1")
	if err != nil || got != 3 {
		t.Fatalf("got %d, %v; want 3", got, err)
	}
	if len(src.queries) != 1 || src.queries[0].TenantID != epTenant || src.queries[0].PrincipalID != epPrincipal || src.queries[0].TransactionRef != "txn_1" {
		t.Errorf("the case list was asked %+v; want one query for txn_1 in the session's scope", src.queries)
	}
}

func TestEpochOfIsOneWhenNoCaseIsClosed(t *testing.T) {
	src := &caseSource{complete: true, cases: []adapters.CaseRecord{epCase("cas_1", "txn_1", "OPEN", 1)}}
	if got, err := epAdapter(t, src).EpochOf(context.Background(), epTenant, epPrincipal, epKind+":txn_1"); err != nil || got != 1 {
		t.Fatalf("got %d, %v; want 1", got, err)
	}
}

// A list that cannot be relied on gives no epoch, never 1 (a guessed 1 would reuse an earlier epoch's idempotency key).
func TestEpochOfIsUnknownNotOneWhenTheListCannotBeRelied(t *testing.T) {
	for name, src := range map[string]*caseSource{
		"incomplete": {complete: false},
		"failed":     {complete: true, err: errors.New("down")},
	} {
		got, err := epAdapter(t, src).EpochOf(context.Background(), epTenant, epPrincipal, epKind+":txn_1")
		if !errors.Is(err, portread.ErrEpochUnknown) || got != 0 {
			t.Errorf("%s: got %d, %v; want 0 and ErrEpochUnknown", name, got, err)
		}
	}
}

// A subject that is not `<this kind>:<id>` is refused before the case system is asked.
func TestEpochOfRefusesASubjectItCannotTurnIntoATransaction(t *testing.T) {
	for _, subject := range []string{"", "txn_1", epKind + ":", ":txn_1", "fin.other:txn_1", epKind + ":txn_1:x", epKind + ":a b"} {
		src := &caseSource{complete: true}
		got, err := epAdapter(t, src).EpochOf(context.Background(), epTenant, epPrincipal, subject)
		if err == nil || got != 0 || src.calls() != 0 {
			t.Errorf("subject %q: got %d, %v, %d calls; want a refusal and no read", subject, got, err, src.calls())
		}
	}
}

// The scope is the tenant and principal passed in, which the Interpreter takes from the verified session; an incomplete one is refused and reads nothing.
func TestEpochOfNeedsTheSessionsTenantAndPrincipal(t *testing.T) {
	for _, c := range [][2]string{{"", epPrincipal}, {epTenant, ""}} {
		src := &caseSource{complete: true}
		if _, err := epAdapter(t, src).EpochOf(context.Background(), c[0], c[1], epKind+":txn_1"); err == nil || src.calls() != 0 {
			t.Errorf("scope %v: got %v, %d calls; want a refusal and no read", c, err, src.calls())
		}
	}
}

// Another tenant's case never counts: the source answers only for the scope it is asked in.
func TestEpochOfDoesNotSeeAnotherTenantsClosedCase(t *testing.T) {
	other := epCase("cas_9", "txn_1", "CLOSED", 5)
	other.TenantID = "ten_bb"
	src := &caseSource{complete: true, cases: []adapters.CaseRecord{other}}
	if got, err := epAdapter(t, src).EpochOf(context.Background(), epTenant, epPrincipal, epKind+":txn_1"); err != nil || got != 1 {
		t.Fatalf("got %d, %v; want 1", got, err)
	}
}

func TestEpochAdapterIsRefusedWithAHalfSetup(t *testing.T) {
	src := &caseSource{complete: true}
	for name, s := range map[string]Setup{
		"no cases":   {Kind: epKind, Timeout: time.Second},
		"no kind":    {Cases: epSetup(t, src).Cases, Timeout: time.Second},
		"no timeout": {Cases: epSetup(t, src).Cases, Kind: epKind},
	} {
		if a, err := New(s); !errors.Is(err, ErrRefused) || a != nil {
			t.Errorf("%s: got %v, %v; want ErrRefused and no adapter", name, a, err)
		}
	}
}
