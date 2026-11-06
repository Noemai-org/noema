package store_test

import (
	"context"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/outcome/predicate"
	"github.com/Noemai-org/noema/services/core/outcome/store"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	subject    = "TX1"
	intentID   = "int_1"
	theKey     = "idk_v1_grt_0001" // the key the seeded receipt was dispatched under
	settleSecs = 30
)

// pages plays the target's answers per query kind. The case system itself is exercised end to end in outcome/e2e; here
// the point is what the store keeps.
type pages struct {
	byKey, byTx []verifier.CasePage
	err         error
}

func (p *pages) FindCases(_ context.Context, q verifier.CaseQuery) (verifier.CasePage, error) {
	if p.err != nil {
		return verifier.CasePage{}, p.err
	}
	list := p.byTx
	if q.IdempotencyKey != "" {
		list = p.byKey
	}
	return list[0], nil
}

type waiter struct{ clk *clock.Manual }

func (w waiter) Wait(_ context.Context, d time.Duration) error { w.clk.Advance(d); return nil }

type hmacSigner struct{ keys *crypto.KeyRing }

func (h hmacSigner) Sign(b []byte) (verifier.Sig, error) {
	v, err := h.keys.SignWith("k_out", b)
	return verifier.Sig{KeyID: "k_out", Value: v}, err
}

type authResult struct{ r verifier.Authenticity }

func (a authResult) Check(context.Context, verifier.Receipt) (verifier.Authenticity, error) {
	return a.r, nil
}

// failingIDs fails the nth Record event ID: the append of that event fails and, with it, the whole verdict (EC-E16).
type failingIDs struct {
	inner chain.IDSource
	n     int32
	count atomic.Int32
}

func (f *failingIDs) ID(prefix string) (string, error) {
	if prefix == "recev" && f.count.Add(1) == f.n {
		return "", errors.New("synthetic: the Record refused the event")
	}
	return f.inner.ID(prefix)
}

type flow struct {
	*world
	clk   *clock.Manual
	pages *pages
	v     *verifier.Verifier
	st    *store.Store
	req   verifier.Request
}

func theCase(status string) verifier.Case {
	return verifier.Case{CaseID: "case_1", TenantID: tenant, PrincipalID: "usr_100", IdempotencyKey: theKey, TransactionRef: subject, Status: status, CreatedByIntent: intentID}
}

func done(cs ...verifier.Case) verifier.CasePage { return verifier.CasePage{Items: cs, Complete: true} }

func newFlow(t *testing.T, auth verifier.Authenticity, failAppend int32) *flow {
	t.Helper()
	return newFlowOn(t, auth, failAppend, (*world).outcome)
}

// newFlowOn is newFlow with Outcome's pool chosen by the caller, to show what the database does with a login that is not the tenant's own.
func newFlowOn(t *testing.T, auth verifier.Authenticity, failAppend int32, pool func(*world, *testing.T) *pgxpool.Pool) *flow {
	t.Helper()
	w := newWorld(t)
	raw, err := os.ReadFile("../../../../packs/fintech_v1.json")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := predicate.LoadPack(raw)
	if err != nil {
		t.Fatal(err)
	}
	def := pack.Definitions["fin.outcome.exception.case_opened"]
	clk := clock.NewManual(verifiedAt.Add(settleSecs * time.Second))
	gen := ids.New(clk, nil)
	var recIDs chain.IDSource = gen
	if failAppend > 0 {
		recIDs = &failingIDs{inner: gen, n: failAppend}
	}
	st := &store.Store{Pool: pool(w, t), Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: recIDs}, RollbackTimeout: 5 * time.Second}
	pg := &pages{byKey: []verifier.CasePage{done(theCase("OPEN"))}, byTx: []verifier.CasePage{done(theCase("OPEN"))}}
	keys := crypto.NewKeyRing(map[string][]byte{"k_out": []byte("synthetic-test-key-not-a-secret")})
	v, err := verifier.New(verifier.Config{Clock: clk, Waiter: waiter{clk}, IDs: gen, Cases: pg, Store: st, Signer: hmacSigner{keys}, Authenticity: authResult{auth}, Scope: pack.Scope,
		Budget: verifier.ReadBudget{Backoffs: []time.Duration{time.Second, 3 * time.Second, 9 * time.Second}, MaxPages: 3}})
	if err != nil {
		t.Fatal(err)
	}
	return &flow{world: w, clk: clk, pages: pg, v: v, st: st, req: verifier.Request{TenantID: tenant, ReceiptID: receipt1, VerificationID: "vrf_flow1", TransactionRef: subject, QueryableByKey: true,
		Bindings:   map[string]string{"subject_resource": subject, "intent_id": intentID},
		Definition: verifier.Definition{OutcomeID: def.OutcomeID, SuccessPredicate: def.SuccessPredicate, SettleWindowSeconds: def.SettleWindowSecs}}}
}

func (f *flow) events(t *testing.T) []string {
	t.Helper()
	rows, err := f.admin.Query(ctx(t), `SELECT event_type FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func eq(a, b []string) bool { return fmt.Sprint(a) == fmt.Sprint(b) }

func (f *flow) verifyChain(t *testing.T) {
	t.Helper()
	if _, err := chain.Verify(ctx(t), f.admin, tenant); err != nil {
		t.Fatalf("the Record chain does not verify: %v", err)
	}
}

// REQ-VER-001, REQ-AUD-001: an ACHIEVED verdict is auditable: its reads, then the verdict, on a chain that verifies.
func TestAchievedIsOnTheRecordWithItsReads(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	res, err := f.v.Verify(ctx(t), f.req)
	if err != nil || res.Verdict != verifier.Achieved {
		t.Fatalf("%+v %v", res, err)
	}
	if got := f.events(t); !eq(got, []string{"VERIFICATION_READ", "VERIFICATION_READ", "VERDICT"}) {
		t.Fatalf("events = %v", got)
	}
	f.verifyChain(t)
	if n := f.count(t, `SELECT count(*) FROM outbox_event WHERE topic = 'recovery.requested'`); n != 0 {
		t.Fatalf("ACHIEVED enqueued %d recovery events", n)
	}
	var sig, stored string
	if err := f.admin.QueryRow(ctx(t), `SELECT sig_value, json->>'verdict' FROM outcome_verdict WHERE verdict_id = $1`, res.VerdictID).Scan(&sig, &stored); err != nil || sig == "" || stored != "ACHIEVED" {
		t.Fatalf("stored verdict: %q %q %v", sig, stored, err)
	}
}

// REQ-VER-003: every other verdict reaches the Record with its RecoveryRequest, and Trust's side is woken by the outbox.
func TestNotAchievedRaisesOneRecoveryRequestOnTheRecordAndTheOutbox(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	f.pages.byKey, f.pages.byTx = []verifier.CasePage{done()}, []verifier.CasePage{done()}
	res, err := f.v.Verify(ctx(t), f.req)
	if err != nil || res.Verdict != verifier.NotAchieved || res.Reason != verifier.ReasonVerifiedAbsent {
		t.Fatalf("%+v %v", res, err)
	}
	if got := f.events(t); !eq(got, []string{"VERIFICATION_READ", "VERIFICATION_READ", "VERDICT", "RECOVERY_REQUESTED"}) {
		t.Fatalf("events = %v", got)
	}
	f.verifyChain(t)
	if n := f.count(t, `SELECT count(*) FROM recovery_request WHERE verdict_id = $1 AND grant_id IS NULL AND status = 'OPEN'`, res.VerdictID); n != 1 {
		t.Fatalf("open verdict-sourced recovery requests = %d", n)
	}
	if n := f.count(t, `SELECT count(*) FROM outbox_event WHERE topic = 'recovery.requested' AND payload->>'verdict_id' = $1`, res.VerdictID); n != 1 {
		t.Fatalf("outbox events = %d", n)
	}
}

// REQ-VER-004, INV-28: a pending verdict has its watch in the database and on the Record.
func TestPendingExternalIsOnTheRecordWithItsWatch(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	f.pages.byKey, f.pages.byTx = []verifier.CasePage{done(theCase("CLOSED"))}, []verifier.CasePage{done(theCase("CLOSED"))}
	now := f.clk.Now()
	f.req.Definition.Pending = &verifier.Pending{Predicate: "exists(case where case.status == 'CLOSED')", Triggers: []string{"fin.evt.settlement.updated"},
		NextCheckAt: now.Add(5 * time.Minute), DeadlineAt: now.Add(72 * time.Hour)}
	res, err := f.v.Verify(ctx(t), f.req)
	if err != nil || res.Verdict != verifier.PendingExternal {
		t.Fatalf("%+v %v", res, err)
	}
	if got := f.events(t); !eq(got, []string{"VERIFICATION_READ", "VERIFICATION_READ", "VERDICT", "RECOVERY_REQUESTED", "WATCH_CREATED"}) {
		t.Fatalf("events = %v", got)
	}
	f.verifyChain(t)
	if n := f.count(t, `SELECT count(*) FROM outcome_watch WHERE verdict_id = $1 AND status = 'OPEN' AND deadline_at > next_check_at`, res.VerdictID); n != 1 {
		t.Fatalf("open watches = %d", n)
	}
}

// AGENT-16: every read attempt is on the Record, not only the one that worked.
func TestEveryReadAttemptIsOnTheRecord(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	f.pages.err = &verifier.ReadError{Kind: verifier.ReadUnavailable}
	res, err := f.v.Verify(ctx(t), f.req)
	if err != nil || res.Verdict != verifier.Unknown || res.Reason != verifier.ReasonReadsExhausted {
		t.Fatalf("%+v %v", res, err)
	}
	if got := f.events(t); !eq(got, []string{"VERIFICATION_READ", "VERIFICATION_READ", "VERIFICATION_READ", "VERIFICATION_READ", "VERDICT", "RECOVERY_REQUESTED"}) {
		t.Fatalf("events = %v", got)
	}
	if n := f.count(t, `SELECT count(*) FROM outcome_verdict WHERE cardinality(evidence_reads) = 0 AND verdict = 'UNKNOWN'`); n != 1 {
		t.Fatalf("UNKNOWN verdicts with no evidence reads = %d", n)
	}
}

// SoT 13.1 slot: an invalid receipt signature raises SECURITY_EVENT SIGNATURE_INVALID first and reads nothing.
func TestAnInvalidReceiptSignatureRaisesASecurityEvent(t *testing.T) {
	f := newFlow(t, verifier.AuthInvalid, 0)
	res, err := f.v.Verify(ctx(t), f.req)
	if err != nil || res.Reason != verifier.ReasonReceiptUnverifiable {
		t.Fatalf("%+v %v", res, err)
	}
	if got := f.events(t); !eq(got, []string{"SECURITY_EVENT", "VERDICT", "RECOVERY_REQUESTED"}) {
		t.Fatalf("events = %v", got)
	}
	if n := f.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'SECURITY_EVENT' AND body->>'kind' = 'SIGNATURE_INVALID'`); n != 1 {
		t.Fatalf("SIGNATURE_INVALID events = %d", n)
	}
}

// EC-E16, PERD 32.4: if any Record append fails, no verdict, request, watch, outbox event or earlier event is stored.
func TestAFailedRecordAppendStoresNothing(t *testing.T) {
	for failAt := int32(1); failAt <= 5; failAt++ { // 2 reads + VERDICT + RECOVERY_REQUESTED + WATCH_CREATED
		t.Run(fmt.Sprintf("append %d", failAt), func(t *testing.T) {
			f := newFlow(t, verifier.AuthValid, failAt)
			f.pages.byKey, f.pages.byTx = []verifier.CasePage{done(theCase("CLOSED"))}, []verifier.CasePage{done(theCase("CLOSED"))}
			now := f.clk.Now()
			f.req.Definition.Pending = &verifier.Pending{Predicate: "exists(case where case.status == 'CLOSED')", Triggers: []string{"fin.evt.settlement.updated"}, NextCheckAt: now.Add(time.Minute), DeadlineAt: now.Add(time.Hour)}
			if _, err := f.v.Verify(ctx(t), f.req); err == nil {
				t.Fatal("Verify reported success although a Record append failed")
			}
			for _, q := range []string{`SELECT count(*) FROM outcome_verdict`, `SELECT count(*) FROM outcome_watch`, `SELECT count(*) FROM recovery_request WHERE verdict_id IS NOT NULL`,
				`SELECT count(*) FROM outbox_event`, `SELECT count(*) FROM record_event`} {
				if n := f.count(t, q); n != 0 {
					t.Fatalf("%s = %d after a failed append", q, n)
				}
			}
		})
	}
}

func TestLoadReceiptCarriesTheGrantsCorrelationAndRefusesAForeignTenant(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	st := &store.Store{Pool: f.outcome(t)}
	r, err := st.LoadReceipt(ctx(t), tenant, receipt1)
	if err != nil || r.CorrelationID != corr || r.PrincipalID != "usr_100" || r.IdempotencyKey != theKey || !r.DispatchedAt.Equal(verifiedAt) || r.State != "OUTCOME_UNKNOWN" {
		t.Fatalf("%+v %v", r, err)
	}
	if _, err := st.LoadReceipt(ctx(t), other, receipt1); err == nil {
		t.Fatal("another tenant's read of the receipt succeeded")
	}
	if _, err := st.LoadReceipt(ctx(t), tenant, "rcp_nope"); err == nil {
		t.Fatal("a receipt that does not exist was loaded")
	}
}

// Duplicate delivery of one verification, concurrently (the outbox is at least once): one verdict, one RecoveryRequest, one
// VERDICT event and one outbox event, and every caller is given that verdict.
func TestConcurrentDeliveriesOfOneVerificationStoreOneVerdict(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	f.pages.byKey, f.pages.byTx = []verifier.CasePage{done()}, []verifier.CasePage{done()} // NOT_ACHIEVED, so a request is at stake
	const callers = 6
	results := make([]verifier.Result, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = f.v.Verify(ctx(t), f.req)
		}()
	}
	wg.Wait()
	replayed := 0
	for i := range results {
		if errs[i] != nil || results[i].VerdictID != results[0].VerdictID || results[i].Verdict != verifier.NotAchieved {
			t.Fatalf("caller %d: %+v %v (first %+v)", i, results[i], errs[i], results[0])
		}
		if results[i].Replayed {
			replayed++
		}
	}
	if replayed != callers-1 {
		t.Fatalf("replayed = %d, want %d: exactly one caller writes", replayed, callers-1)
	}
	for q, want := range map[string]int{
		`SELECT count(*) FROM outcome_verdict`:                                      1,
		`SELECT count(*) FROM recovery_request WHERE verdict_id IS NOT NULL`:        1,
		`SELECT count(*) FROM record_event WHERE event_type = 'VERDICT'`:            1,
		`SELECT count(*) FROM record_event WHERE event_type = 'RECOVERY_REQUESTED'`: 1,
		`SELECT count(*) FROM outbox_event WHERE topic = 'recovery.requested'`:      1,
	} {
		if n := f.count(t, q); n != want {
			t.Fatalf("%s = %d, want %d", q, n, want)
		}
	}
	f.verifyChain(t)
}

// The verdict's instant is in the signed document as text and in a column as a timestamp with microsecond precision. A real
// clock has nanoseconds; the two must still agree, or the database refuses the verdict.
func TestAVerdictAtNanosecondPrecisionIsStoredAndAgrees(t *testing.T) {
	f := newFlow(t, verifier.AuthValid, 0)
	f.clk.Advance(123_456_789 * time.Nanosecond)
	res, err := f.v.Verify(ctx(t), f.req)
	if err != nil || res.Verdict != verifier.Achieved {
		t.Fatalf("%+v %v", res, err)
	}
	if n := f.count(t, `SELECT count(*) FROM outcome_verdict WHERE (json->>'verified_at')::timestamptz = verified_at`); n != 1 {
		t.Fatalf("verdicts whose document instant equals the column = %d", n)
	}
}
