package trustpg2_test

// TRU-12: the Postgres store of confirmation requests and answers (migration 0032; REQ-TRU-009, REQ-AUD-001, REQ-SEC-004; INV-11, INV-12, INV-17, INV-25). Real PostgreSQL, real tenant
// logins, forced row security: they need the CI database (pgsodium). All data is SYNTHETIC; the clock is SYNTHETIC.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpg2"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

func TestMain(m *testing.M) { testpg.Main(m) }

var reqBase = time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC)

type reqSink struct {
	mu       sync.Mutex
	fail     bool
	requests []trustpg2.RequestedEvent
	received []trustpg2.ReceivedEvent
	expired  []trustpg2.ExpiredEvent
}

var errReqSink = errors.New("the Record refused the append")

func (s *reqSink) Requested(_ context.Context, e trustpg2.RequestedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errReqSink
	}
	s.requests = append(s.requests, e)
	return nil
}

func (s *reqSink) Received(_ context.Context, e trustpg2.ReceivedEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errReqSink
	}
	s.received = append(s.received, e)
	return nil
}

func (s *reqSink) Expired(_ context.Context, e trustpg2.ExpiredEvent) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fail {
		return errReqSink
	}
	s.expired = append(s.expired, e)
	return nil
}

type reqWorld struct {
	admin *pgxpool.Pool
	clk   *clock.Manual
	sink  *reqSink
	a, b  trustpg2.Requests
}

func reqCtx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func newReqWorld(t *testing.T) *reqWorld {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(reqCtx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"ten_aa", "ten_bb"} {
		if _, err := admin.Exec(reqCtx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'T')`, id); err != nil {
			t.Fatal(err)
		}
	}
	clk := clock.NewManual(reqBase)
	sink := &reqSink{}
	mk := func(tenant string) trustpg2.Requests {
		return trustpg2.Requests{Pool: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenant), Clock: clk, IDs: ids.New(clk, nil), Events: sink}
	}
	return &reqWorld{admin: admin, clk: clk, sink: sink, a: mk("ten_aa"), b: mk("ten_bb")}
}

func reqBinding(plan string) confirm.BindingInput {
	return confirm.BindingInput{PrincipalID: "prn_1", IntentHash: "sha256:i1", CapabilityID: "cap.change", TargetRef: "tgt_1", ParamsHash: "sha256:p1", EvidenceSetHash: "sha256:e1",
		PlanHash: plan, PolicyVersion: "pol_v1"}
}

func (w *reqWorld) newReq(plan string, ttl time.Duration, supersedes string) trustpg2.NewRequest {
	return trustpg2.NewRequest{TenantID: "ten_aa", CorrelationID: "cor_1", IntentID: "int_1", Version: 1, Binding: reqBinding(plan), Requirement: "MAKER_CHECKER", RequesterID: "prn_1",
		ExpiresAt: w.clk.Now().Add(ttl).Unix(), Supersedes: supersedes}
}

func (w *reqWorld) rows(t *testing.T, table string) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(reqCtx(t), `SELECT count(*) FROM `+table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// REQ-TRU-009: a request is stored with its eight values, requirement, requester and expiry, and read back as the newest of the intent.
func TestOpenStoresTheRequestAndLatestReadsItBack(t *testing.T) {
	w := newReqWorld(t)
	if _, ok, err := w.a.Latest(reqCtx(t), "ten_aa", "prn_1", "int_1", 1); err != nil || ok {
		t.Fatalf("Latest before any request: ok=%v err=%v", ok, err)
	}
	n := w.newReq("sha256:l1", time.Hour, "")
	id, err := w.a.Open(reqCtx(t), n)
	if err != nil {
		t.Fatal(err)
	}
	got, ok, err := w.a.Latest(reqCtx(t), "ten_aa", "prn_1", "int_1", 1)
	if err != nil || !ok {
		t.Fatalf("Latest: ok=%v err=%v", ok, err)
	}
	if got.Request.ID != id || got.Request.Binding != n.Binding || got.Request.Requirement != "MAKER_CHECKER" || got.Request.RequesterID != "prn_1" || got.Request.ExpiresAt != n.ExpiresAt || got.Answer != nil {
		t.Fatalf("Latest = %+v, want the stored request without an answer", got)
	}
	if len(w.sink.requests) != 1 || w.sink.requests[0].RequestID != id || w.sink.requests[0].CorrelationID != "cor_1" {
		t.Fatalf("Record events = %+v, want one CONFIRMATION_REQUESTED for %s", w.sink.requests, id)
	}
}

// The Record comes first: if the append fails, no request is stored (PERD 32.4).
func TestARecordFailureStoresNoRequest(t *testing.T) {
	w := newReqWorld(t)
	w.sink.fail = true
	if _, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, "")); err == nil {
		t.Fatal("Open succeeded although the Record refused the append")
	}
	if n := w.rows(t, "confirmation_request"); n != 0 {
		t.Fatalf("%d requests stored after a refused append", n)
	}
}

// Never a second request beside an open one; a fresh one supersedes after the expiry; Latest returns the head.
func TestASecondRequestNeedsTheFirstToBeExpiredOrAnsweredAndSupersedesIt(t *testing.T) {
	w := newReqWorld(t)
	first, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, "")); !errors.Is(err, trustpg2.ErrRequestConflict) {
		t.Fatalf("a second root request: %v, want ErrRequestConflict", err)
	}
	if _, err := w.a.Open(reqCtx(t), w.newReq("sha256:l2", time.Hour, first)); !errors.Is(err, trustpg2.ErrRequestConflict) {
		t.Fatalf("a request beside an open one: %v, want ErrRequestConflict", err)
	}
	w.clk.Advance(2 * time.Hour)
	second, err := w.a.Open(reqCtx(t), w.newReq("sha256:l2", time.Hour, first))
	if err != nil {
		t.Fatalf("a request after the expiry: %v", err)
	}
	got, ok, err := w.a.Latest(reqCtx(t), "ten_aa", "prn_1", "int_1", 1)
	if err != nil || !ok || got.Request.ID != second || got.Request.Binding.PlanHash != "sha256:l2" {
		t.Fatalf("Latest = %+v ok=%v err=%v, want the superseding request %s", got, ok, err, second)
	}
	if n := w.rows(t, "confirmation_request"); n != 2 {
		t.Fatalf("%d requests, want 2 (a request is superseded by a new row)", n)
	}
	if w.sink.requests[1].Supersedes != first {
		t.Fatalf("the second CONFIRMATION_REQUESTED names %q, want the request it supersedes", w.sink.requests[1].Supersedes)
	}
}

// INV-12: the answer carries the approver and roles the caller gives; the first answer is the one Latest returns; the same answer again is idempotent.
func TestAnAnswerIsStoredOnceAndAReplayReturnsTheSameEvent(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	in := trustpg2.AnswerInput{TenantID: "ten_aa", RequestID: id, ApproverID: "prn_2", Roles: []string{"reviewer", "ops"}, Response: confirm.Accept, At: w.clk.Now().Unix()}
	ev1, err := w.a.Answer(reqCtx(t), in)
	if err != nil || ev1 == "" {
		t.Fatalf("Answer: %q %v", ev1, err)
	}
	ev2, err := w.a.Answer(reqCtx(t), in)
	if err != nil || ev2 != ev1 {
		t.Fatalf("a replayed ACCEPT: %q %v, want the first event %q", ev2, err, ev1)
	}
	if n := w.rows(t, "confirmation_answer"); n != 1 || len(w.sink.received) != 1 {
		t.Fatalf("%d answers, %d CONFIRMATION_RECEIVED events after a replay, want 1 and 1", n, len(w.sink.received))
	}
	in.Response = confirm.Decline
	if _, err := w.a.Answer(reqCtx(t), in); !errors.Is(err, trustpg2.ErrRequestConflict) {
		t.Fatalf("the same approver answering differently: %v, want ErrRequestConflict", err)
	}
	later := in
	later.ApproverID, later.Response, later.At = "prn_3", confirm.Decline, w.clk.Now().Unix()+60
	if _, err := w.a.Answer(reqCtx(t), later); err != nil {
		t.Fatal(err)
	}
	got, ok, err := w.a.Latest(reqCtx(t), "ten_aa", "prn_1", "int_1", 1)
	if err != nil || !ok || got.Answer == nil {
		t.Fatalf("Latest: %+v ok=%v err=%v", got, ok, err)
	}
	ev := got.Answer.Event
	if got.Answer.EventID != ev1 || ev.RequestID != id || ev.ApproverID != "prn_2" || ev.Response != confirm.Accept || ev.At != in.At || len(ev.ApproverRoles) != 2 || ev.ApproverRoles[0] != "reviewer" {
		t.Fatalf("answer = %+v, want the first (prn_2 ACCEPT)", got.Answer)
	}
	if !w.sink.received[0].Accepted || w.sink.received[0].ApproverID != "prn_2" || w.sink.received[0].CorrelationID != "cor_1" {
		t.Fatalf("CONFIRMATION_RECEIVED = %+v", w.sink.received[0])
	}
}

// Two ACCEPTs of one approver at once store one answer.
func TestTwoConcurrentAnswersStoreOne(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	in := trustpg2.AnswerInput{TenantID: "ten_aa", RequestID: id, ApproverID: "prn_2", Roles: []string{"reviewer"}, Response: confirm.Accept, At: w.clk.Now().Unix()}
	var wg sync.WaitGroup
	evs := make([]string, 2)
	errs := make([]error, 2)
	for i := range evs {
		wg.Add(1)
		go func() {
			defer wg.Done()
			evs[i], errs[i] = w.a.Answer(reqCtx(t), in)
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil || evs[0] != evs[1] || w.rows(t, "confirmation_answer") != 1 {
		t.Fatalf("events %v errors %v rows %d, want one answer and the same event twice", evs, errs, w.rows(t, "confirmation_answer"))
	}
}

// The Record comes first for an answer too, and an answer to a request that does not exist (or is another tenant's) is refused.
func TestAnAnswerIsRefusedWithoutARecordOrWithoutItsRequest(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	in := trustpg2.AnswerInput{TenantID: "ten_aa", RequestID: id, ApproverID: "prn_2", Roles: []string{"reviewer"}, Response: confirm.Accept, At: w.clk.Now().Unix()}
	w.sink.fail = true
	if _, err := w.a.Answer(reqCtx(t), in); err == nil {
		t.Fatal("Answer succeeded although the Record refused the append")
	}
	w.sink.fail = false
	if n := w.rows(t, "confirmation_answer"); n != 0 {
		t.Fatalf("%d answers stored after a refused append", n)
	}
	bad := in
	bad.RequestID = "cfr_nope"
	if _, err := w.a.Answer(reqCtx(t), bad); err == nil {
		t.Fatal("an answer to an unknown request was stored")
	}
	other := in
	other.TenantID = "ten_bb"
	if _, err := w.b.Answer(reqCtx(t), other); err == nil {
		t.Fatal("tenant B answered tenant A's request")
	}
	bad = in
	bad.Response = "MAYBE"
	if _, err := w.a.Answer(reqCtx(t), bad); !errors.Is(err, trustpg2.ErrRequestForm) {
		t.Fatalf("a response that is neither ACCEPT nor DECLINE: %v, want ErrRequestForm", err)
	}
	bad = in
	bad.Roles = nil
	if _, err := w.a.Answer(reqCtx(t), bad); err != nil {
		t.Fatalf("an approver with no roles may answer (the eligibility is the resolver's): %v", err)
	}
}

// CONFIRMATION_EXPIRED is recorded once, and never before the request expires.
func TestExpireRecordsOnceAndNotBeforeTheExpiry(t *testing.T) {
	w := newReqWorld(t)
	id, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, ""))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.a.Expire(reqCtx(t), "ten_aa", id); !errors.Is(err, trustpg2.ErrNotExpired) {
		t.Fatalf("Expire of an open request: %v, want ErrNotExpired", err)
	}
	w.clk.Advance(time.Hour + time.Second)
	w.sink.fail = true
	if err := w.a.Expire(reqCtx(t), "ten_aa", id); err == nil {
		t.Fatal("Expire succeeded although the Record refused the append")
	}
	w.sink.fail = false
	for i := 0; i < 2; i++ {
		if err := w.a.Expire(reqCtx(t), "ten_aa", id); err != nil {
			t.Fatalf("Expire %d: %v", i, err)
		}
	}
	if len(w.sink.expired) != 1 || w.sink.expired[0].RequestID != id || w.sink.expired[0].CorrelationID != "cor_1" || w.rows(t, "confirmation_expiry") != 1 {
		t.Fatalf("CONFIRMATION_EXPIRED events %+v, rows %d, want one", w.sink.expired, w.rows(t, "confirmation_expiry"))
	}
}

// REQ-SEC-004: tenant B reads none of tenant A's requests; a request outside the contract's form is refused before anything is written; the context needs a deadline.
func TestRequestsAreTenantScopedAndFormChecked(t *testing.T) {
	w := newReqWorld(t)
	if _, err := w.a.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, "")); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := w.b.Latest(reqCtx(t), "ten_bb", "prn_1", "int_1", 1); err != nil || ok {
		t.Fatalf("tenant B Latest: ok=%v err=%v, want none", ok, err)
	}
	if _, ok, err := w.b.Latest(reqCtx(t), "ten_aa", "prn_1", "int_1", 1); err != nil || ok {
		t.Fatalf("tenant B asking for tenant A: ok=%v err=%v, want none", ok, err)
	}
	if _, err := w.b.Open(reqCtx(t), w.newReq("sha256:l1", time.Hour, "")); err == nil {
		t.Fatal("tenant B opened a request for tenant A")
	}
	bad := w.newReq("sha256:l1", time.Hour, "")
	bad.Binding.TargetRef = `a"b`
	bad.IntentID = "int_2"
	if _, err := w.a.Open(reqCtx(t), bad); !errors.Is(err, trustpg2.ErrRequestForm) {
		t.Fatalf("an unsafe value: %v, want ErrRequestForm", err)
	}
	bad = w.newReq("sha256:l1", time.Hour, "")
	bad.IntentID, bad.Requirement = "int_3", "ANYONE"
	if _, err := w.a.Open(reqCtx(t), bad); !errors.Is(err, trustpg2.ErrRequestForm) {
		t.Fatalf("an unknown requirement: %v, want ErrRequestForm", err)
	}
	bad = w.newReq("sha256:l1", -time.Hour, "")
	bad.IntentID = "int_4"
	if _, err := w.a.Open(reqCtx(t), bad); !errors.Is(err, trustpg2.ErrRequestForm) {
		t.Fatalf("an expiry before the clock: %v, want ErrRequestForm", err)
	}
	if _, err := w.a.Open(context.Background(), w.newReq("sha256:l1", time.Hour, "")); !errors.Is(err, trustpg2.ErrRequestForm) {
		t.Fatalf("no deadline: %v, want ErrRequestForm", err)
	}
	if n := w.rows(t, "confirmation_request"); n != 1 {
		t.Fatalf("%d requests, want 1", n)
	}
}
