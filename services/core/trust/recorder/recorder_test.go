package recorder_test

// TRU-05 (REQ-TRU-008, REQ-TRU-001, REQ-AUD-001; INV-03, INV-25, INV-16): the Decision Record is written first, signed, and the grants of an ALLOW are issued
// after it, each with its GRANT_ISSUED event, in the one transaction the caller supplies: all of it commits or none of it does. Real PostgreSQL, real
// tenant logins, the real hash chain. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/migrations"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/db"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/recorder"
)

const (
	tenant     = "ten_aa"
	capability = "fin.cap.create_escalation_case"
	target     = "fin.transaction:TX88213"
)

var epoch = time.Date(2026, 11, 6, 9, 0, 0, 0, time.UTC)

type world struct {
	admin  *pgxpool.Pool
	trust  *pgxpool.Pool
	reader *pgxpool.Pool
	keys   *grants.Keys
	w      *recorder.Writer
}

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return c
}

// newAppender is the one place this package builds a chain.Appender. Since noema#263 (REC-08) Append requires a signing key for the emitter, so the fixture signer
// for core-trust comes from internal/recordsig (SYNTHETIC key, made per test).
func newAppender(t testing.TB, admin *pgxpool.Pool, clk clock.Clock, idg chain.IDSource) *chain.Appender {
	t.Helper()
	return &chain.Appender{Clock: clk, IDs: idg, Signers: recordsig.For(t, admin, "recorder", nil, "core-trust")}
}

func newWorld(t *testing.T) *world {
	t.Helper()
	admin, dsn := testpg.NewDatabase(t)
	if _, err := db.Migrate(ctx(t), admin, migrations.FS); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ($1, 'A')`, tenant); err != nil {
		t.Fatal(err)
	}
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	keys, err := grants.NewKeys([]grants.Key{{ID: "k2", Secret: secret}})
	if err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(epoch)
	idg := ids.New(clk, rand.New(rand.NewSource(1)))
	issuer, err := grants.NewIssuer(keys, idg, rand.New(rand.NewSource(2)), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	w := &recorder.Writer{Keys: keys, Issuer: issuer, Appender: newAppender(t, admin, clk, idg), Clock: clk, IDs: idg, RecordLifetime: 15 * time.Minute}
	return &world{admin: admin, trust: testpg.TenantLogin(t, admin, dsn, "trust_rw", tenant), reader: testpg.TenantLogin(t, admin, dsn, "record_read", tenant), keys: keys, w: w}
}

func freshness() decide.Freshness {
	return decide.Freshness{EvidenceID: "ev_1", EffectiveAt: epoch.Unix() - 30, Source: "fin.src.ledger", AgeSeconds: 30, EffectiveLimitSeconds: 300, LimitSetBy: decide.LimitByTrustProfile}
}

func evaluated(t *testing.T) *decide.BasisBuilder {
	t.Helper()
	b, err := decide.NewBasisBuilder("fin.pack@1.0.0", epoch.Unix())
	if err != nil {
		t.Fatal(err)
	}
	if err := b.RecordEvidence("evs_01ABC", []decide.Freshness{freshness()}); err != nil {
		t.Fatal(err)
	}
	if err := b.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"}}); err != nil {
		t.Fatal(err)
	}
	return b
}

func resolved(t *testing.T) decide.ResolvedIntent {
	t.Helper()
	r, ok := decide.Admit(decide.IntentView{Version: 2}).Resolved()
	if !ok {
		t.Fatal("not resolved")
	}
	return r
}

var pass = decide.Authority{Technical: decide.Pass, Policy: decide.Pass, Business: decide.Pass}

func decision(t *testing.T, o decide.Outcome, b *decide.BasisBuilder) decide.Decision {
	t.Helper()
	var d decide.Decision
	var err error
	reason, rerr := decide.Core("NOT_AUTHORIZED")
	if rerr != nil {
		t.Fatal(rerr)
	}
	switch o {
	case decide.Allow:
		d, err = resolved(t).Allow(pass)
	case decide.Reject:
		d, err = resolved(t).Reject(decide.Authority{Technical: decide.Pass, Policy: decide.Fail, Business: decide.NotEvaluated}, reason)
	case decide.Escalate:
		u, _ := decide.Admit(decide.IntentView{Version: 2, MaterialFlags: []string{"X"}}).Unresolved()
		d, err = u.Escalate(reason)
	default:
		t.Fatalf("outcome %s", o)
	}
	if err != nil {
		t.Fatal(err)
	}
	d, err = b.Attach(d)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func h(c byte) contracts.CommonV1Hash {
	s := "sha256:"
	for i := 0; i < 64; i++ {
		s += string("0123456789abcdef"[int(c)%16])
	}
	return contracts.CommonV1Hash(s)
}

func step(t *testing.T) grants.Step {
	t.Helper()
	p, err := reconcile.NewFreezeGate(nil).PermitTarget(false, capability, target)
	if err != nil {
		t.Fatal(err)
	}
	return grants.Step{PrincipalID: "usr_88213", CapabilityID: capability, TargetRef: target, ParamsHash: h(2), EvidenceSetHash: h(3), PlanHash: h(4), PlanStep: 1,
		TargetVersions: []contracts.DecisionGrantV1TargetVersionsItem{{ResourceRef: target, Source: "fin.src.ledger", VersionToken: "v7"}}, Permission: p}
}

func request(t *testing.T, d decide.Decision, b *decide.BasisBuilder, steps ...grants.Step) recorder.Request {
	return recorder.Request{TenantID: tenant, CorrelationID: "corr_ABC123", IntentID: "int_01J9X3", IntentHash: h(1), PolicyID: "fin.pol", Decision: d, Evaluated: b, Steps: steps}
}

func (w *world) write(t *testing.T, r recorder.Request, commit bool) (recorder.Result, error) {
	t.Helper()
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	res, werr := w.w.Write(ctx(t), tx, r)
	if werr != nil || !commit {
		_ = tx.Rollback(ctx(t))
		return res, werr
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return res, nil
}

func (w *world) count(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func (w *world) events(t *testing.T) []string {
	t.Helper()
	rows, err := chain.ReadChain(ctx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range rows {
		out = append(out, r.EventType+":"+r.ObjectRef)
	}
	return out
}

// An ALLOW writes the signed record first, then each grant with its event, in the caller's one transaction.
func TestAnAllowWritesTheSignedRecordThenEachGrantAndEachEventInOneTransaction(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	res, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t), func() grants.Step { s := step(t); s.PlanStep = 2; return s }()), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Grants) != 2 || res.Record.Outcome != "ALLOW" || string(res.Record.TenantId) != tenant {
		t.Fatalf("result %+v", res)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 1 {
		t.Fatalf("%d decision records, want 1", n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 2 {
		t.Fatalf("%d grants, want 2", n)
	}
	// the stored record is the v2 document, its signature verifies, and every bound field is in it
	var stored contracts.DecisionRecordV2
	var raw []byte
	if err := w.admin.QueryRow(ctx(t), `SELECT json::text FROM decision_record`).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if err := stored.UnmarshalJSON(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.keys.VerifyRecordAt(stored, epoch.Add(time.Minute)); err != nil {
		t.Fatalf("the stored record does not verify: %v", err)
	}
	if stored.IntentId != "int_01J9X3" || stored.CorrelationId != "corr_ABC123" || stored.PolicyVersion != "fin.pol@3.1" || stored.IssuedAt == "" || stored.ExpiresAt == "" || stored.EvidenceSnapshotId != "evs_01ABC" {
		t.Fatalf("a bound field is missing: %+v", stored)
	}
	// each stored grant verifies and belongs to the record
	for _, g := range res.Grants {
		if err := w.keys.Verify(g); err != nil || string(g.DecisionId) != string(stored.DecisionId) {
			t.Fatalf("grant %s: %v", g.GrantId, err)
		}
	}
	// the Record holds DECISION_MADE first, then one GRANT_ISSUED per grant, and the chain verifies
	got := w.events(t)
	if len(got) != 3 || got[0] != "DECISION_MADE:"+string(stored.DecisionId) || got[1][:12] != "GRANT_ISSUED" || got[2][:12] != "GRANT_ISSUED" {
		t.Fatalf("events %v", got)
	}
	if n, err := chain.Verify(ctx(t), w.admin, tenant); err != nil || n != 3 {
		t.Fatalf("chain verify: %d %v", n, err)
	}
}

// All or nothing: if the caller rolls back, no record, no grant and no event remain.
func TestARolledBackTransactionLeavesNoRecordNoGrantAndNoEvent(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	if _, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t)), false); err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{`SELECT count(*) FROM decision_record`, `SELECT count(*) FROM decision_grant`, `SELECT count(*) FROM record_event`} {
		if n := w.count(t, q); n != 0 {
			t.Errorf("%s: %d rows after a rollback", q, n)
		}
	}
}

// A failure while issuing a grant fails the write; with the caller's rollback nothing of the record or the event remains (the Record comes first, INV-03).
func TestAGrantThatCannotBeIssuedFailsTheWholeWrite(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	bad := step(t)
	bad.TargetRef = "not a target"
	_, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t), bad), false)
	if err == nil {
		t.Fatal("a write with an invalid step succeeded")
	}
	for _, q := range []string{`SELECT count(*) FROM decision_record`, `SELECT count(*) FROM decision_grant`, `SELECT count(*) FROM record_event`} {
		if n := w.count(t, q); n != 0 {
			t.Errorf("%s: %d rows after a failed write", q, n)
		}
	}
}

// Only an ALLOW has grants. A decision with another outcome writes its record and event and never a grant; asking for grants with it is refused before anything is written.
func TestOnlyAnAllowIssuesGrants(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	d := decision(t, decide.Reject, b)
	if _, err := w.write(t, request(t, d, b, step(t)), true); !errors.Is(err, recorder.ErrGrantsNeedAllow) {
		t.Fatalf("grants requested for a REJECT: %v", err)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 0 {
		t.Fatalf("a refused request wrote %d records", n)
	}
	res, err := w.write(t, request(t, d, b), true)
	if err != nil || len(res.Grants) != 0 || res.Record.Outcome != "REJECT" {
		t.Fatalf("%+v %v", res, err)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 0 {
		t.Fatalf("%d grants for a REJECT", n)
	}
	if got := w.events(t); len(got) != 1 || got[0][:13] != "DECISION_MADE" {
		t.Fatalf("events %v", got)
	}
}

// TRU-13: a decision whose basis is not the evaluated set is refused, and nothing is written.
func TestADecisionWhoseBasisIsNotTheEvaluatedSetIsRefused(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	forged := decide.Basis{PackVersion: "fin.pack@9.9.9", PolicyRules: []decide.PolicyRuleVersion{{RuleID: "fin.pol.x", PolicyID: "fin.pol", Version: "9.9"}}, EvidenceSnapshotID: "evs_01ZZZ",
		EvidenceFreshness: []decide.Freshness{freshness()}, EvaluatedAt: epoch.Unix()}
	d, err := resolved(t).Allow(pass)
	if err != nil {
		t.Fatal(err)
	}
	d, err = d.WithBasis(forged)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.write(t, request(t, d, b, step(t)), true); !errors.Is(err, decide.ErrBasisDiffers) {
		t.Fatalf("a forged basis: %v", err)
	}
	for _, q := range []string{`SELECT count(*) FROM decision_record`, `SELECT count(*) FROM decision_grant`, `SELECT count(*) FROM record_event`} {
		if n := w.count(t, q); n != 0 {
			t.Errorf("%s: %d rows", q, n)
		}
	}
}

// Refused before anything is written: no deadline, a malformed identifier, a CLARIFY (its clarification is not mapped yet), a decision that has no
// evaluated basis (a refusal before any read cannot be a v2 record: the contract requires a snapshot and a policy).
func TestRequestsTheWriterCannotHonourAreRefusedBeforeAnythingIsWritten(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	good := request(t, decision(t, decide.Allow, b), b, step(t))
	tx, err := w.trust.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx(t)) }()
	if _, err := w.w.Write(context.Background(), tx, good); err == nil {
		t.Error("a context with no deadline was accepted")
	}
	for name, mutate := range map[string]func(*recorder.Request){
		"a malformed intent id":      func(r *recorder.Request) { r.IntentID = "intent 1" },
		"a malformed correlation id": func(r *recorder.Request) { r.CorrelationID = "x" },
		"a malformed tenant":         func(r *recorder.Request) { r.TenantID = "tenant" },
		"a malformed intent hash":    func(r *recorder.Request) { r.IntentHash = "sha256:abc" },
		"no evaluated builder":       func(r *recorder.Request) { r.Evaluated = nil },
		"a policy the basis lacks":   func(r *recorder.Request) { r.PolicyID = "fin.other" },
	} {
		r := good
		mutate(&r)
		if _, err := w.w.Write(ctx(t), tx, r); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	early, _ := decide.NewBasisBuilder("fin.pack@1.0.0", epoch.Unix())
	refusal, err := decide.FailAtGate(decide.T4Capability, 2)
	if err != nil {
		t.Fatal(err)
	}
	refusal, err = early.Attach(refusal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.w.Write(ctx(t), tx, request(t, refusal, early)); !errors.Is(err, recorder.ErrNoEvaluatedBasis) {
		t.Errorf("a refusal before any read: %v", err)
	}
}

// Codex P2 on noema#237: the pack version is in the signed record, and decision-record.v2 gives it a pattern. A builder that accepted a malformed one must not
// get a record signed that every contract-validating reader rejects.
func TestAPackVersionThatIsNotTheContractsIsRefusedBeforeAnythingIsWritten(t *testing.T) {
	w := newWorld(t)
	for _, pack := range []string{"NotAPack", "fin.pack", "fin.pack@1", "fin.pack@1.0.0.0", "fin pack@1.0.0", "@1.0.0"} {
		b, err := decide.NewBasisBuilder(pack, epoch.Unix())
		if err != nil {
			continue // the builder itself refused it, which is also fine
		}
		if err := b.RecordEvidence("evs_01ABC", []decide.Freshness{freshness()}); err != nil {
			t.Fatal(err)
		}
		if err := b.RecordPolicy([]decide.PolicyRuleVersion{{RuleID: "fin.pol.default.permit_case_writes", PolicyID: "fin.pol", Version: "3.1"}}); err != nil {
			t.Fatal(err)
		}
		tx, err := w.trust.Begin(ctx(t))
		if err != nil {
			t.Fatal(err)
		}
		_, err = w.w.Write(ctx(t), tx, request(t, decision(t, decide.Allow, b), b, step(t)))
		_ = tx.Rollback(ctx(t))
		if !errors.Is(err, recorder.ErrRequest) {
			t.Errorf("pack version %q: err %v, want ErrRequest", pack, err)
		}
	}
}

// REQ-AUD-003 / the reconstruct projection (record/reconstruct/project.go): the events this writer appends carry the facts a reconstruction reads, under the
// keys it reads them by. DECISION_MADE: decision_outcome, reason_codes, policy_id and policy_version. GRANT_ISSUED: grant_id, capability_id, plan_step and
// expires_at (a reader derives the grant's state and capability from it). A payload under other keys would be left out of the reconstruction.
func TestTheEventsCarryTheFactsAReconstructionReads(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	res, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t)), true)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := chain.ReadChain(ctx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	payload := func(i int) map[string]any {
		var m map[string]any
		if rows[i].Payload == nil || json.Unmarshal([]byte(*rows[i].Payload), &m) != nil {
			t.Fatalf("event %d has no readable payload", i)
		}
		return m
	}
	dm := payload(0)
	if dm["decision_outcome"] != "ALLOW" || dm["policy_id"] != "fin.pol" || dm["policy_version"] != "fin.pol@3.1" || dm["decision_id"] != string(res.Record.DecisionId) {
		t.Errorf("DECISION_MADE payload %v, want decision_outcome ALLOW, policy_id fin.pol, policy_version fin.pol@3.1 and the decision id", dm)
	}
	if _, ok := dm["reason_codes"].([]any); !ok { // an ALLOW may carry no reason code; the list must still be there
		t.Errorf("DECISION_MADE payload %v carries no reason_codes list", dm)
	}
	gi := payload(1)
	if gi["capability_id"] != capability || gi["grant_id"] != string(res.Grants[0].GrantId) || gi["expires_at"] == nil || gi["plan_step"] == nil {
		t.Errorf("GRANT_ISSUED payload %v, want grant_id, capability_id %s, plan_step and expires_at", gi, capability)
	}
}

// INV-12 / AGENT-10 (Codex P1, noema#237): the Record actor of a decision is never chosen by the caller. A request has no field that names an actor or a
// principal, and DECISION_MADE and GRANT_ISSUED are written as the emitter's own identity ("no authenticated person caused the event", chain.Event).
func TestARequestCannotChooseTheRecordActor(t *testing.T) {
	typ := reflect.TypeOf(recorder.Request{})
	for i := 0; i < typ.NumField(); i++ {
		switch n := strings.ToLower(typ.Field(i).Name); n {
		case "actor", "principal", "principalid", "person", "user", "userid":
			t.Errorf("recorder.Request has a field %q: a caller could choose the Record actor", typ.Field(i).Name)
		}
	}
	w := newWorld(t)
	b := evaluated(t)
	if _, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t)), true); err != nil {
		t.Fatal(err)
	}
	rows, err := chain.ReadChain(ctx(t), w.reader)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d events, want DECISION_MADE and GRANT_ISSUED", len(rows))
	}
	for _, r := range rows {
		if r.Actor != "core-trust" || r.WrittenBy != "core-trust" {
			t.Errorf("%s: actor %q written_by %q, want both core-trust", r.EventType, r.Actor, r.WrittenBy)
		}
	}
}

// ---- Record-first: a failed append or insert fails the whole write (PERD 32.4, EC-E16, AGENTS section 5, INV-03, INV-17) ----
// Each test makes the real component fail (an ID source that errors, a duplicate primary key in the real database), then checks that Write returns the
// cause and that nothing remains after the caller's rollback. Each was also run against a mutant that swallows that one error (see the PR).

var errBoom = errors.New("synthetic id source failure")

// failingIDs delegates to inner and fails the nth call (1-based) for prefix; every other call passes through.
type failingIDs struct {
	inner  interface{ ID(string) (string, error) }
	prefix string
	nth    int
	calls  int
}

func (f *failingIDs) ID(prefix string) (string, error) {
	if prefix == f.prefix {
		f.calls++
		if f.calls == f.nth {
			return "", errBoom
		}
	}
	return f.inner.ID(prefix)
}

// fixedIDs always returns the same ID for prefix and delegates for any other prefix.
type fixedIDs struct {
	inner  interface{ ID(string) (string, error) }
	prefix string
	id     string
}

func (f fixedIDs) ID(prefix string) (string, error) {
	if prefix == f.prefix {
		return f.id, nil
	}
	return f.inner.ID(prefix)
}

func (w *world) nothingRemains(t *testing.T, what string) {
	t.Helper()
	if n := w.count(t, `SELECT count(*) FROM decision_record`); n != 0 {
		t.Errorf("%s: %d decision records remain after the rollback", what, n)
	}
	if n := w.count(t, `SELECT count(*) FROM decision_grant`); n != 0 {
		t.Errorf("%s: %d grants remain after the rollback", what, n)
	}
	if got := w.events(t); len(got) != 0 {
		t.Errorf("%s: events remain after the rollback: %v", what, got)
	}
}

func TestAFailedDecisionMadeAppendFailsTheWriteAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	clk := clock.NewManual(epoch)
	base := ids.New(clk, rand.New(rand.NewSource(3)))
	w.w.Appender = newAppender(t, w.admin, clk, &failingIDs{inner: base, prefix: "recev", nth: 1})
	b := evaluated(t)
	res, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t)), true)
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "append DECISION_MADE") {
		t.Fatalf("got %v, want the DECISION_MADE append failure", err)
	}
	if res.Record.DecisionId != "" || len(res.Grants) != 0 {
		t.Errorf("a failed write returned a record or grants: %+v", res)
	}
	w.nothingRemains(t, "DECISION_MADE append failed")
}

func TestAFailedGrantIssuedAppendFailsTheWriteAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	clk := clock.NewManual(epoch)
	base := ids.New(clk, rand.New(rand.NewSource(3)))
	w.w.Appender = newAppender(t, w.admin, clk, &failingIDs{inner: base, prefix: "recev", nth: 2}) // the second append is the first GRANT_ISSUED
	b := evaluated(t)
	_, err := w.write(t, request(t, decision(t, decide.Allow, b), b, step(t)), true)
	if !errors.Is(err, errBoom) || !strings.Contains(err.Error(), "append GRANT_ISSUED 0") {
		t.Fatalf("got %v, want the GRANT_ISSUED append failure", err)
	}
	w.nothingRemains(t, "GRANT_ISSUED append failed")
}

// A REJECT has no grants, so nothing after the record insert could surface a swallowed insert error (a later statement in an aborted transaction would):
// the write must fail at the insert itself.
func TestAFailedDecisionRecordInsertFailsTheWriteAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	b := evaluated(t)
	first := request(t, decision(t, decide.Reject, b), b)
	if _, err := w.write(t, first, true); err != nil {
		t.Fatal(err)
	}
	clk := clock.NewManual(epoch)
	dup := *w.w
	dup.IDs = fixedIDs{inner: ids.New(clk, rand.New(rand.NewSource(4))), prefix: "dec", id: "dec_FIXEDDUP"}
	dup.Appender = newAppender(t, w.admin, clk, ids.New(clk, rand.New(rand.NewSource(5))))
	w.w = &dup
	// A186: a request for an input that is already stored returns the stored decision and never reaches the insert, so the two uses of the fixed decision ID
	// are for two inputs (two intent hashes); the second collides on the primary key.
	other, another := first, first
	other.IntentHash, another.IntentHash = h(5), h(6)
	if _, err := w.write(t, other, true); err != nil { // the first use of the fixed decision id succeeds
		t.Fatal(err)
	}
	before := w.count(t, `SELECT count(*) FROM decision_record`)
	_, err := w.write(t, another, true) // the second use collides on the primary key
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" || !strings.Contains(err.Error(), "insert decision record") {
		t.Fatalf("got %v, want the unique violation of the decision_record insert", err)
	}
	if after := w.count(t, `SELECT count(*) FROM decision_record`); after != before {
		t.Errorf("a failed insert changed the record count from %d to %d", before, after)
	}
}

// Two steps whose grants get the same ID: the second grant insert hits the primary key. A swallowed error would be followed by an append in an aborted
// transaction (a different SQLSTATE), so the test asserts the insert's own unique violation.
func TestAFailedGrantInsertFailsTheWriteAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	clk := clock.NewManual(epoch)
	dupGrants := fixedIDs{inner: ids.New(clk, rand.New(rand.NewSource(6))), prefix: "grt", id: "grt_FIXEDDUP"}
	issuer, err := grants.NewIssuer(w.keys, dupGrants, rand.New(rand.NewSource(7)), 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	w.w.Issuer = issuer
	b := evaluated(t)
	s1, s2 := step(t), step(t)
	s2.PlanStep = 2
	_, err = w.write(t, request(t, decision(t, decide.Allow, b), b, s1, s2), true)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "23505" || !strings.Contains(err.Error(), "insert grant 1") {
		t.Fatalf("got %v, want the unique violation of the second grant insert", err)
	}
	w.nothingRemains(t, "grant insert failed")
}

// INV-12 / REQ-SEC-004 (definition of done 3a): the tenant on a Request is a claim, not an authority. A Request naming another tenant, written through this
// tenant's login, is refused by the database (the Record append and the decision_record tenant guard bind the tenant to the login), and nothing remains.
func TestARequestNamingAnotherTenantIsRefusedByTheDatabaseAndLeavesNothing(t *testing.T) {
	w := newWorld(t)
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant (tenant_id, name) VALUES ('ten_bb', 'B')`); err != nil {
		t.Fatal(err)
	}
	b := evaluated(t)
	r := request(t, decision(t, decide.Allow, b), b, step(t))
	r.TenantID = "ten_bb"
	res, err := w.write(t, r, true)
	if err == nil {
		t.Fatalf("a Request naming ten_bb was written through ten_aa's login: %+v", res)
	}
	w.nothingRemains(t, "another tenant named in the Request")
	if n := w.count(t, `SELECT count(*) FROM decision_record WHERE tenant_id = 'ten_bb'`); n != 0 {
		t.Errorf("%d decision records were written for ten_bb", n)
	}
}
