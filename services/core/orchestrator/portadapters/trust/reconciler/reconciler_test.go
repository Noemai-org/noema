package reconcilerport_test

// TRU-07 (A329, the Reconciler of the Trust intake): REQ-RES-001, REQ-RES-002, REQ-TRU-003, REQ-TRU-007, REQ-CTX-004; INV-11, INV-12, INV-16, INV-17, INV-19, INV-21; AGENT-13, AGENT-16.
// The adapter answers T10's question from the sealed snapshot alone: it reads the snapshot as the tenant, checks that it is intact, classifies the stored items again with the
// real Reconciler (the real rule set and the real mapping of packs/fintech) at the snapshot's own time, and refuses (fails closed) when the answer is not the sealed one.
// The snapshot store, the payload store and the pack's routing are the only fakes. SYNTHETIC.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/injection"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/readseal"
	"github.com/Noemai-org/noema/services/core/evidence/reconciler"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	reconcilerport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/reconciler"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const (
	tenant   = "ten_demo"
	foreign  = "ten_other"
	snapID   = "evs_synth01"
	ledger   = "fin.src.core_ledger"
	sw       = "fin.src.switch"
	dest     = "fin.src.destination"
	expected = "fin.map.v1@3"
	leak     = "payload-body-must-not-leak"
	dsnLeak  = "postgres://trust_rw:hunter2@db.internal:5432/noema"
)

var (
	packDir  = filepath.Join("..", "..", "..", "..", "..", "..", "packs", "fintech")
	readAt   = time.Date(2026, 11, 17, 9, 0, 0, 123456000, time.UTC) // the column keeps microseconds
	sealedAt = time.Date(2026, 11, 17, 9, 1, 0, 0, time.UTC)
	allowed  = []string{"fin.cap.create_case", "fin.cap.record_evidence", "fin.cap.route_internal"}
)

func mustRead(t *testing.T, parts ...string) []byte {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(parts...))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func ruleSet(t *testing.T) *reconcile.RuleSet {
	t.Helper()
	rs, err := reconcile.ParseRuleSet(mustRead(t, packDir, "recon_rules", "rules.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	return rs
}

func theMapping(t *testing.T, rs *reconcile.RuleSet) *mapping.Mapping {
	t.Helper()
	m, err := mapping.Parse(mustRead(t, packDir, "evidence_mapping", "mapping.v1.json"), rs, []string{ledger, sw, dest, "fin.src.case_system"}, expected, mapping.Limits{MaxBytes: 1 << 20, MaxDepth: 32}, caseScope(t, filepath.Join(packDir, "..", "fintech_v1.json")))
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func ctx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

// payloads is the payload port the real Reconciler reads through: payloads by reference, as the tenant that asked.
type payloads struct {
	mu      sync.Mutex
	byRef   map[string][]byte
	err     error
	tenants []string
}

func (p *payloads) Read(_ context.Context, tenantID, ref string) ([]byte, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.tenants = append(p.tenants, tenantID)
	if p.err != nil {
		return nil, p.err
	}
	raw, ok := p.byRef[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	return raw, nil
}

// snaps is the Snapshots port: sealed snapshots by (tenant, id), the answer of a store whose login is the tenant's own, so another tenant's id is not found.
type snaps struct {
	mu    sync.Mutex
	rows  map[string]reconcilerport.Sealed
	asked [][2]string
	err   error
	fixed *reconcilerport.Sealed // a misbehaving store: it answers this for any question
	wait  bool                   // block until the context is over
}

func (s *snaps) Sealed(c context.Context, tenantID, snapshotID string) (reconcilerport.Sealed, error) {
	s.mu.Lock()
	s.asked = append(s.asked, [2]string{tenantID, snapshotID})
	s.mu.Unlock()
	if s.wait {
		<-c.Done()
		return reconcilerport.Sealed{}, c.Err()
	}
	if s.err != nil {
		return reconcilerport.Sealed{}, s.err
	}
	if s.fixed != nil {
		return *s.fixed, nil
	}
	if r, ok := s.rows[tenantID+"/"+snapshotID]; ok {
		return r, nil
	}
	return reconcilerport.Sealed{}, reconcilerport.ErrNotFound
}

// pack is the pack's routing of a classification, as data.
type pack struct {
	reasons map[reconcile.Classification]string
	allowed []string
}

func (p pack) EscalateReason(c reconcile.Classification) string { return p.reasons[c] }
func (p pack) AllowedUnderFreeze() []string                     { return p.allowed }

func finPack() pack {
	return pack{reasons: map[reconcile.Classification]string{
		reconcile.ContradictoryState:      "fin.reason.contradictory_state",
		reconcile.ExecutionOutcomeUnknown: "fin.reason.execution_outcome_unknown",
	}, allowed: allowed}
}

type spec struct {
	source  string
	payload map[string]any
	status  contracts.EnumsV1FactStatus
}

func ledgerPosted() spec {
	return spec{source: ledger, payload: map[string]any{"entries": []any{map[string]any{"entry_type": "DEBIT", "posting_status": "POSTED"}}, "note": leak}}
}
func switchIs(s string) spec { return spec{source: sw, payload: map[string]any{"semantic_status": s}} }
func destIs(s string) spec   { return spec{source: dest, payload: map[string]any{"credit_state": s}} }

func hashOf(t *testing.T, payload map[string]any) string {
	t.Helper()
	canon, err := crypto.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// world is one sealed snapshot of the tenant, made the way readseal seals one: the items are classified by the real Reconciler and the snapshot holds the classification and the hash.
type world struct {
	t        *testing.T
	pay      *payloads
	sealed   reconcilerport.Sealed
	store    *snaps
	rs       *reconcile.RuleSet
	mp       *mapping.Mapping
	baseConf reconciler.Config
}

func newWorld(t *testing.T, specs ...spec) *world {
	t.Helper()
	rs := ruleSet(t)
	w := &world{t: t, pay: &payloads{byRef: map[string][]byte{}}, rs: rs, mp: theMapping(t, rs)}
	w.baseConf = reconciler.Config{RuleSet: rs, Mapping: w.mp, Payloads: payloadread.Reader{Screen: cleanScreen{}, Correlation: corrOf, Alert: noAlert{}, Payloads: w.pay}}
	var items []layer.Item
	for i, s := range specs {
		id, ref := "evr_"+string(rune('a'+i)), "pay_"+string(rune('a'+i))
		raw, err := json.Marshal(s.payload)
		if err != nil {
			t.Fatal(err)
		}
		w.pay.byRef[ref] = raw
		st := s.status
		if st == "" {
			st = contracts.EnumsV1FactStatusKNOWN
		}
		items = append(items, layer.Item{EvidenceID: id, PayloadRef: ref, PayloadHash: hashOf(t, s.payload), Status: st, Source: s.source, ReadAt: readAt,
			EffectiveAt: readAt.Add(-time.Minute), EffectiveFreshnessSeconds: 300})
	}
	var pairs []snapshot.Pair
	ids := make([]string, len(items))
	for i, it := range items {
		pairs = append(pairs, snapshot.Pair{EvidenceID: it.EvidenceID, PayloadHash: it.PayloadHash})
		ids[i] = it.EvidenceID
	}
	hash, err := snapshot.SetHash(pairs)
	if err != nil {
		t.Fatal(err)
	}
	cl, err := reconcilerport.ReconcilerAt(w.baseConf)(sealedAt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cl.Classify(ctx(t), tenant, items)
	if err != nil {
		t.Fatal(err)
	}
	w.sealed = reconcilerport.Sealed{TenantID: tenant, SnapshotID: snapID, EvidenceIDs: ids, ReconciliationID: got.ReconciliationID, Classification: got.Classification,
		EvidenceSetHash: hash, SealedAt: sealedAt, Items: items}
	w.publish()
	return w
}

func (w *world) publish() {
	w.store = &snaps{rows: map[string]reconcilerport.Sealed{tenant + "/" + snapID: w.sealed}}
}

func (w *world) adapter(t *testing.T, p reconcilerport.Pack) *reconcilerport.Adapter {
	t.Helper()
	a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: reconcilerport.ReconcilerAt(w.baseConf), Pack: p, Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

var _ intake.Reconciler = (*reconcilerport.Adapter)(nil)
var _ reconcilerport.Classifier = (*reconciler.Reconciler)(nil)
var _ readseal.Reconciler = (*reconciler.Reconciler)(nil)

func agree() []spec { return []spec{ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED")} }

// ---- the answer T10 needs (REQ-RES-001, REQ-RES-002) -------------------------------------------------------------------------------------------------------------------------

// REQ-RES-001: three sources that agree are SUCCESS_CONFIRMED, read from the sealed snapshot with the real rule set; nothing is frozen and the pack maps nothing to escalate.
func TestASettledSnapshotIsReturnedAsItsSealedClassification(t *testing.T) {
	w := newWorld(t, agree()...)
	got, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != reconcile.SuccessConfirmed || got.Frozen || got.EscalateReason != "" {
		t.Fatalf("got %+v, want SUCCESS_CONFIRMED, not frozen, no escalation", got)
	}
}

// REQ-RES-002: a contradiction (the switch says failed, the destination says credited) freezes the target, carries the pack's escalation reason and the pack's allow-list.
func TestAContradictionIsFrozenWithThePacksReasonAndAllowList(t *testing.T) {
	w := newWorld(t, ledgerPosted(), switchIs("REJECTED"), destIs("CREDITED"))
	if w.sealed.Classification != "CONTRADICTORY_STATE" {
		t.Fatalf("the fixture is %s, not a contradiction", w.sealed.Classification)
	}
	got, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != reconcile.ContradictoryState || !got.Frozen || got.EscalateReason != "fin.reason.contradictory_state" || !reflect.DeepEqual(got.AllowedUnderFreeze, allowed) {
		t.Fatalf("got %+v", got)
	}
}

// REQ-RES-001, EC-S01: an outcome that is unknown is escalated with the pack's reason and is not a freeze.
func TestAnUnknownOutcomeEscalatesWithThePacksReasonAndIsNotFrozen(t *testing.T) {
	w := newWorld(t, ledgerPosted(), switchIs("TIMED_OUT"), destIs("PENDING"))
	got, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Classification != reconcile.ExecutionOutcomeUnknown || got.Frozen || got.EscalateReason != "fin.reason.execution_outcome_unknown" {
		t.Fatalf("got %+v", got)
	}
}

// REQ-RES-002: of the twelve classifications of PERD 22.3 only CONTRADICTORY_STATE freezes, whatever the pack says; the sealed classification is what comes back and the pack's reason
// is what the pack gave for it.
func TestOnlyAContradictionFreezesAndEveryClassificationComesBackAsSealed(t *testing.T) {
	declared := []reconcile.Classification{reconcile.SuccessConfirmed, reconcile.RejectedConfirmed, reconcile.DebitPostedCreditUnconfirmed, reconcile.SettlementPending, reconcile.ReversalPending,
		reconcile.ReversedConfirmed, reconcile.PossibleDuplicate, reconcile.Partial, reconcile.ExecutionOutcomeUnknown, reconcile.ContradictoryState, reconcile.EventOrderUncertain,
		reconcile.InsufficientEvidence}
	for _, c := range declared {
		w := newWorld(t, agree()...)
		w.sealed.Classification = string(c)
		w.publish()
		p := pack{reasons: map[reconcile.Classification]string{c: "fin.reason.some_reason"}, allowed: allowed}
		a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: func(time.Time) (reconcilerport.Classifier, error) {
			return fixedClassifier{readseal.Classified{ReconciliationID: "rec_synth", Classification: string(c)}}, nil
		}, Pack: p, Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.Reconcile(ctx(t), tenant, snapID)
		if err != nil {
			t.Fatalf("%s: %v", c, err)
		}
		if got.Classification != c || got.Frozen != (c == reconcile.ContradictoryState) || got.EscalateReason != "fin.reason.some_reason" {
			t.Errorf("%s: got %+v", c, got)
		}
	}
}

type fixedClassifier struct{ c readseal.Classified }

func (f fixedClassifier) Classify(context.Context, string, []layer.Item) (readseal.Classified, error) {
	return f.c, nil
}

// A value the sealed snapshot holds that is not one of the twelve classifications is not an answer (INV-16, the set is closed): the adapter refuses it before it asks anyone.
func TestASealedValueThatIsNotAClassificationIsRefused(t *testing.T) {
	for _, bad := range []string{"", "MADE_UP_BY_AN_ADAPTER", "success_confirmed", "SUCCESS_CONFIRMED "} {
		w := newWorld(t, agree()...)
		w.sealed.Classification = bad
		w.publish()
		got, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
		if !errors.Is(err, reconcilerport.ErrBadAnswer) || got.Classification != "" {
			t.Errorf("%q: got %+v, %v; want ErrBadAnswer and no classification", bad, got, err)
		}
	}
}

// What the pack gives is data and is checked: a reason code that is not `<vertical>.reason.<name>` is refused (INV-16), and an empty one is "the pack maps nothing". The allow-list
// the adapter returns is a copy, so nothing T10 does to it changes the pack's own (INV-14: the adapter names no capability).
func TestThePacksReasonMustBeAPackReasonCodeAndItsAllowListIsCopied(t *testing.T) {
	for _, bad := range []string{"contradictory_state", "FIN.REASON.X", "fin.reason.", "fin.reason.a b", "INTERNAL_ERROR", "fin.cap.create_case", " fin.reason.x"} {
		w := newWorld(t, ledgerPosted(), switchIs("REJECTED"), destIs("CREDITED"))
		p := pack{reasons: map[reconcile.Classification]string{reconcile.ContradictoryState: bad}, allowed: allowed}
		if _, err := w.adapter(t, p).Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrBadAnswer) {
			t.Errorf("reason %q: err = %v, want ErrBadAnswer", bad, err)
		}
	}
	w := newWorld(t, ledgerPosted(), switchIs("REJECTED"), destIs("CREDITED"))
	own := []string{"fin.cap.a", "fin.cap.b"}
	got, err := w.adapter(t, pack{reasons: finPack().reasons, allowed: own}).Reconcile(ctx(t), tenant, snapID)
	if err != nil {
		t.Fatal(err)
	}
	got.AllowedUnderFreeze[0] = "fin.cap.tampered"
	if own[0] != "fin.cap.a" {
		t.Fatal("the pack's allow-list was changed through the answer")
	}
}

// ---- reproducible from the snapshot alone (INV-21, REQ-TRU-007) ---------------------------------------------------------------------------------------------------------------

// INV-21: the same snapshot gives the same answer, however many times it is asked and in whatever order its items are stored; the classification is made again at the snapshot's own
// time and never at the time of the question, and from the items of the snapshot alone.
func TestTheAnswerIsReproducibleFromTheSnapshotAlone(t *testing.T) {
	w := newWorld(t, agree()...)
	var ats []time.Time
	var seen [][]layer.Item
	var mu sync.Mutex
	base := reconcilerport.ReconcilerAt(w.baseConf)
	factory := func(at time.Time) (reconcilerport.Classifier, error) {
		mu.Lock()
		ats = append(ats, at)
		mu.Unlock()
		c, err := base(at)
		return spyClassifier{c, func(items []layer.Item) { mu.Lock(); seen = append(seen, items); mu.Unlock() }}, err
	}
	a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: factory, Pack: finPack(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	first, err := a.Reconcile(ctx(t), tenant, snapID)
	if err != nil {
		t.Fatal(err)
	}
	w.sealed.Items[0], w.sealed.Items[2] = w.sealed.Items[2], w.sealed.Items[0]
	w.publish()
	a, _ = reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: factory, Pack: finPack(), Timeout: time.Second})
	for i := 0; i < 3; i++ {
		again, err := a.Reconcile(ctx(t), tenant, snapID)
		if err != nil || !reflect.DeepEqual(again, first) {
			t.Fatalf("ask %d: %+v, %v; want %+v", i, again, err, first)
		}
	}
	for _, at := range ats {
		if !at.Equal(sealedAt) {
			t.Errorf("classified at %v, want the snapshot's sealed time %v", at, sealedAt)
		}
	}
	for _, items := range seen {
		if len(items) != 3 {
			t.Errorf("classified %d items, want the snapshot's 3", len(items))
		}
	}
}

type spyClassifier struct {
	reconcilerport.Classifier
	saw func([]layer.Item)
}

func (s spyClassifier) Classify(c context.Context, tenantID string, items []layer.Item) (readseal.Classified, error) {
	s.saw(items)
	return s.Classifier.Classify(c, tenantID, items)
}

// The snapshot's seal time is truncated to the millisecond and an item's read time is kept to the microsecond, so an item can be read a few microseconds after the truncated time. The
// Reconciler refuses an item read after its clock (it would push the item's expiry out), so the adapter classifies at the later of the seal time and the latest read time, which is still a
// function of the snapshot alone. Without it, a good snapshot would fail closed now and then.
func TestAReadTimeAfterTheTruncatedSealTimeStillClassifies(t *testing.T) {
	w := newWorld(t, agree()...)
	w.sealed.SealedAt = readAt.Add(-400 * time.Microsecond)
	w.publish()
	got, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if err != nil || got.Classification != reconcile.SuccessConfirmed {
		t.Fatalf("got %+v, %v", got, err)
	}
}

// INV-21 and "a signal, never an overwrite": when the classification made again is not the one the snapshot sealed, the adapter fails closed with ErrDrift. It returns neither
// classification: not the sealed one (it could be the stale one) and not the fresh one (a snapshot is immutable and its classification is not the adapter's to replace).
func TestADriftBetweenTheSealedAndTheFreshClassificationFailsClosed(t *testing.T) {
	w := newWorld(t, agree()...)
	w.sealed.Classification = "CONTRADICTORY_STATE" // sealed over items that now classify as SUCCESS_CONFIRMED
	w.publish()
	got, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if !errors.Is(err, reconcilerport.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
	if !reflect.DeepEqual(got, intake.Reconciliation{}) {
		t.Fatalf("a drift returned %+v, want nothing", got)
	}
	// and the other way round: a contradiction found now over items sealed as settled must not come back as settled
	w = newWorld(t, ledgerPosted(), switchIs("REJECTED"), destIs("CREDITED"))
	w.sealed.Classification = "SUCCESS_CONFIRMED"
	w.publish()
	got, err = w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if !errors.Is(err, reconcilerport.ErrDrift) || got.Classification != "" {
		t.Fatalf("got %+v, %v; want ErrDrift and nothing", got, err)
	}
}

// A payload that is no longer the sealed one makes its item UNKNOWN (A260), so the classification made again is not the sealed one: the drift is the signal.
func TestAStoredPayloadThatChangedAfterSealingIsADrift(t *testing.T) {
	w := newWorld(t, agree()...)
	w.pay.byRef["pay_a"] = []byte(`{"entries": []}`)
	if _, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
}

// The classifier's answer must itself be a classification: a value that is not one is not compared, it is refused.
func TestAFreshAnswerThatIsNotAClassificationIsRefused(t *testing.T) {
	w := newWorld(t, agree()...)
	a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: func(time.Time) (reconcilerport.Classifier, error) {
		return fixedClassifier{readseal.Classified{ReconciliationID: "rec_synth", Classification: ""}}, nil
	}, Pack: finPack(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrDrift) {
		t.Fatalf("err = %v, want ErrDrift", err)
	}
}

// ---- the snapshot must be intact (REQ-CTX-004, INV-17) ------------------------------------------------------------------------------------------------------------------------

// REQ-CTX-004, INV-17: a snapshot whose items are not the ones its hash covers is not classified: the hash is recomputed from the items read, and the Reconciler is never asked.
func TestASnapshotThatIsNotIntactIsRefusedBeforeAnythingIsClassified(t *testing.T) {
	other := "sha256:" + strings.Repeat("ab", 32)
	for name, mutate := range map[string]func(*reconcilerport.Sealed){
		"a changed payload hash": func(s *reconcilerport.Sealed) { s.Items[1].PayloadHash = other },
		"a changed stored hash":  func(s *reconcilerport.Sealed) { s.EvidenceSetHash = other },
		"a missing item":         func(s *reconcilerport.Sealed) { s.Items = s.Items[:2] },
		"an extra item": func(s *reconcilerport.Sealed) {
			s.Items = append(s.Items, layer.Item{EvidenceID: "evr_z", PayloadHash: other})
		},
		"a repeated item":                func(s *reconcilerport.Sealed) { s.Items[2] = s.Items[1] },
		"an item that is not in the set": func(s *reconcilerport.Sealed) { s.Items[2].EvidenceID = "evr_zz" },
		"an empty evidence set":          func(s *reconcilerport.Sealed) { s.EvidenceIDs, s.Items = nil, nil },
		"an empty set with its own honest hash": func(s *reconcilerport.Sealed) {
			s.EvidenceIDs, s.Items = nil, nil
			s.EvidenceSetHash, _ = snapshot.SetHash(nil)
		},
		"a snapshot with no seal time": func(s *reconcilerport.Sealed) { s.SealedAt = time.Time{} },
		"an id that is not sorted": func(s *reconcilerport.Sealed) {
			s.EvidenceIDs[0], s.EvidenceIDs[1] = s.EvidenceIDs[1], s.EvidenceIDs[0]
		},
		"a hash that is not a hash": func(s *reconcilerport.Sealed) { s.EvidenceSetHash = "sha256:xyz" },
	} {
		w := newWorld(t, agree()...)
		w.sealed.EvidenceIDs = append([]string(nil), w.sealed.EvidenceIDs...)
		w.sealed.Items = append([]layer.Item(nil), w.sealed.Items...)
		mutate(&w.sealed)
		w.publish()
		called := false
		a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: func(time.Time) (reconcilerport.Classifier, error) {
			called = true
			return reconcilerport.ReconcilerAt(w.baseConf)(sealedAt)
		}, Pack: finPack(), Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.Reconcile(ctx(t), tenant, snapID)
		if !errors.Is(err, reconcilerport.ErrIntegrity) || got.Classification != "" || called {
			t.Errorf("%s: got %+v, %v, classifier asked %v; want ErrIntegrity, nothing, never asked", name, got, err, called)
		}
	}
}

// ---- one tenant's view (REQ-TRU-003, INV-11, INV-12) --------------------------------------------------------------------------------------------------------------------------

// REQ-TRU-003: another tenant's snapshot id answers exactly as an id that does not exist: the same error, the same text. The store is asked as the caller's tenant, never another.
func TestAnotherTenantsSnapshotIsTheSameAnswerAsUnknown(t *testing.T) {
	w := newWorld(t, agree()...)
	w.store.rows[foreign+"/evs_foreign01"] = reconcilerport.Sealed{TenantID: foreign, SnapshotID: "evs_foreign01"}
	a := w.adapter(t, finPack())
	_, errUnknown := a.Reconcile(ctx(t), tenant, "evs_nosuch01")
	_, errForeign := a.Reconcile(ctx(t), tenant, "evs_foreign01")
	_, errMalformed := a.Reconcile(ctx(t), tenant, "not a snapshot id; drop table")
	_, errEmpty := a.Reconcile(ctx(t), tenant, "")
	for name, err := range map[string]error{"unknown": errUnknown, "foreign": errForeign, "malformed": errMalformed, "empty": errEmpty} {
		if !errors.Is(err, reconcilerport.ErrNotFound) {
			t.Errorf("%s: %v, want ErrNotFound", name, err)
		}
		if err != nil && err.Error() != errUnknown.Error() {
			t.Errorf("%s: text %q differs from the unknown id's %q", name, err.Error(), errUnknown.Error())
		}
	}
	for _, q := range w.store.asked {
		if q[0] != tenant {
			t.Errorf("the store was asked as %q, want the caller's tenant", q[0])
		}
	}
}

// INV-11: a store that answers with another tenant's snapshot (a wiring fault; the database would not) is refused as not found: nothing of it is classified or named in the error.
func TestASnapshotOfAnotherTenantFromTheStoreIsRefusedAsNotFound(t *testing.T) {
	w := newWorld(t, agree()...)
	for name, mutate := range map[string]func(*reconcilerport.Sealed){
		"another tenant": func(s *reconcilerport.Sealed) { s.TenantID = foreign },
		"another id":     func(s *reconcilerport.Sealed) { s.SnapshotID = "evs_other0001" },
	} {
		s := w.sealed
		mutate(&s)
		a, err := reconcilerport.New(reconcilerport.Config{Snapshots: &snaps{fixed: &s}, Classifiers: reconcilerport.ReconcilerAt(w.baseConf), Pack: finPack(), Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.Reconcile(ctx(t), tenant, snapID)
		if !errors.Is(err, reconcilerport.ErrNotFound) || got.Classification != "" {
			t.Fatalf("%s: got %+v, %v", name, got, err)
		}
		for _, secret := range []string{foreign, "evs_other0001", snapID, tenant} {
			if strings.Contains(err.Error(), secret) {
				t.Errorf("%s: the error names %q: %v", name, secret, err)
			}
		}
	}
}

// INV-12: the payloads are read as the caller's tenant, whatever the snapshot or its items say.
func TestThePayloadsAreReadAsTheCallersTenant(t *testing.T) {
	w := newWorld(t, agree()...)
	if _, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID); err != nil {
		t.Fatal(err)
	}
	if len(w.pay.tenants) == 0 {
		t.Fatal("no payload was read")
	}
	for _, got := range w.pay.tenants {
		if got != tenant {
			t.Errorf("a payload was read as %q", got)
		}
	}
}

// ---- deadlines, configuration and errors that carry nothing (AGENT-16, AGENT-13) -----------------------------------------------------------------------------------------------

// A deadline is required (AGENT-16 and the repository's rule): without one nothing is read. The adapter also bounds the call with its own timeout.
func TestThereIsNoCallWithoutADeadlineAndTheCallIsBoundedByTheTimeout(t *testing.T) {
	w := newWorld(t, agree()...)
	if _, err := w.adapter(t, finPack()).Reconcile(context.Background(), tenant, snapID); !errors.Is(err, reconcilerport.ErrNoDeadline) {
		t.Fatalf("err = %v, want ErrNoDeadline", err)
	}
	if len(w.store.asked) != 0 {
		t.Fatal("the store was asked without a deadline")
	}
	w.store.wait = true
	a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: reconcilerport.ReconcilerAt(w.baseConf), Pack: finPack(), Timeout: 30 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	wall := clock.System{}
	start := wall.Now()
	_, err = a.Reconcile(ctx(t), tenant, snapID) // the caller's deadline is a minute away
	if !errors.Is(err, reconcilerport.ErrRead) || !errors.Is(err, context.DeadlineExceeded) || wall.Now().Sub(start) > 10*time.Second {
		t.Fatalf("err = %v after %v, want ErrRead over a deadline well before the caller's", err, wall.Now().Sub(start))
	}
}

// AGENT-16: nothing has a default. Every part and the timeout are required.
func TestTheAdapterIsNotUsableWithoutEveryPart(t *testing.T) {
	w := newWorld(t, agree()...)
	ok := reconcilerport.Config{Snapshots: w.store, Classifiers: reconcilerport.ReconcilerAt(w.baseConf), Pack: finPack(), Timeout: time.Second}
	for name, c := range map[string]reconcilerport.Config{
		"no snapshots":   {Classifiers: ok.Classifiers, Pack: ok.Pack, Timeout: ok.Timeout},
		"no classifiers": {Snapshots: ok.Snapshots, Pack: ok.Pack, Timeout: ok.Timeout},
		"no pack":        {Snapshots: ok.Snapshots, Classifiers: ok.Classifiers, Timeout: ok.Timeout},
		"no timeout":     {Snapshots: ok.Snapshots, Classifiers: ok.Classifiers, Pack: ok.Pack},
		"a negative one": {Snapshots: ok.Snapshots, Classifiers: ok.Classifiers, Pack: ok.Pack, Timeout: -time.Second},
	} {
		if _, err := reconcilerport.New(c); !errors.Is(err, reconcilerport.ErrConfig) {
			t.Errorf("%s: err = %v, want ErrConfig", name, err)
		}
	}
	var none *reconcilerport.Adapter
	if _, err := none.Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrConfig) {
		t.Errorf("a nil adapter: err = %v, want ErrConfig", err)
	}
	if _, err := (&reconcilerport.Adapter{}).Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrConfig) {
		t.Errorf("a zero adapter: err = %v, want ErrConfig", err)
	}
	if _, err := reconcilerport.New(ok); err != nil {
		t.Fatal(err)
	}
}

// INV-12: a call with no tenant is refused and nothing is read.
func TestACallWithNoTenantReadsNothing(t *testing.T) {
	w := newWorld(t, agree()...)
	if _, err := w.adapter(t, finPack()).Reconcile(ctx(t), "", snapID); !errors.Is(err, reconcilerport.ErrSession) {
		t.Fatalf("err = %v, want ErrSession", err)
	}
	if len(w.store.asked) != 0 {
		t.Fatal("the store was asked with no tenant")
	}
}

// INV-19, INV-16: a store that cannot answer is not "there is no snapshot": it is ErrRead, with the cause reachable and never printed (AGENT-13: a connection string, a server name).
func TestAStoreFailureIsNotAnAbsenceAndPrintsNothingOfItsCause(t *testing.T) {
	w := newWorld(t, agree()...)
	cause := errors.New("dial " + dsnLeak + ": connection refused")
	w.store.err = cause
	_, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if !errors.Is(err, reconcilerport.ErrRead) || !errors.Is(err, cause) || errors.Is(err, reconcilerport.ErrNotFound) {
		t.Fatalf("err = %v, want ErrRead over the cause and not ErrNotFound", err)
	}
	for _, s := range []string{"hunter2", "db.internal", "trust_rw", tenant, snapID} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("the error carries %q: %v", s, err)
		}
	}
}

// INV-19: a payload store that fails is not a payload that says nothing: the reclassification is refused, and no payload, store text or id is in the error (AGENT-13).
func TestAFailingPayloadStoreRefusesTheReclassificationAndLeaksNothing(t *testing.T) {
	w := newWorld(t, agree()...)
	w.pay.err = errors.New("read " + dsnLeak + ": " + leak)
	_, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	if !errors.Is(err, reconcilerport.ErrReclassify) || errors.Is(err, reconcilerport.ErrDrift) {
		t.Fatalf("err = %v, want ErrReclassify and not a drift", err)
	}
	for _, s := range []string{"hunter2", "db.internal", leak, tenant, snapID, "evr_a", "pay_a"} {
		if strings.Contains(err.Error(), s) {
			t.Errorf("the error carries %q: %v", s, err)
		}
	}
}

// A classifier that cannot be made, and one that refuses, are both errors and never an answer.
func TestAClassifierThatCannotBeMadeOrRefusesIsAnError(t *testing.T) {
	w := newWorld(t, agree()...)
	a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: func(time.Time) (reconcilerport.Classifier, error) { return nil, errors.New("no") }, Pack: finPack(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrConfig) {
		t.Errorf("a classifier that cannot be made: err = %v, want ErrConfig", err)
	}
	a, err = reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: func(time.Time) (reconcilerport.Classifier, error) { return nil, nil }, Pack: finPack(), Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrConfig) {
		t.Errorf("no classifier: err = %v, want ErrConfig", err)
	}
	w.sealed.Items[0].Status = "NOT_A_STATUS" // the real Reconciler refuses a status that is not one
	w.sealed.Items[0].PayloadHash = hashOf(t, map[string]any{"x": 1})
	hash, _ := snapshot.SetHash([]snapshot.Pair{{EvidenceID: w.sealed.Items[0].EvidenceID, PayloadHash: w.sealed.Items[0].PayloadHash}, {EvidenceID: w.sealed.Items[1].EvidenceID, PayloadHash: w.sealed.Items[1].PayloadHash},
		{EvidenceID: w.sealed.Items[2].EvidenceID, PayloadHash: w.sealed.Items[2].PayloadHash}})
	w.sealed.EvidenceSetHash = hash
	w.publish()
	if _, err := w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID); !errors.Is(err, reconcilerport.ErrReclassify) {
		t.Errorf("a refused item set: err = %v, want ErrReclassify", err)
	}
}

// ReconcilerAt makes the real Reconciler with a clock that reads the time asked for, and keeps the rest of the configuration; a configuration the Reconciler refuses is an error.
func TestReconcilerAtBuildsTheRealReconcilerAtTheTimeAskedFor(t *testing.T) {
	w := newWorld(t, agree()...)
	cl, err := reconcilerport.ReconcilerAt(w.baseConf)(sealedAt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := cl.Classify(ctx(t), tenant, w.sealed.Items)
	if err != nil || got.Classification != w.sealed.Classification || got.ReconciliationID != w.sealed.ReconciliationID {
		t.Fatalf("got %+v, %v; want the sealed %s %s (the same inputs at the same time)", got, err, w.sealed.ReconciliationID, w.sealed.Classification)
	}
	later, _ := reconcilerport.ReconcilerAt(w.baseConf)(sealedAt.Add(time.Minute))
	if again, _ := later.Classify(ctx(t), tenant, w.sealed.Items); again.ReconciliationID == got.ReconciliationID {
		t.Error("another time gave the same reconciliation id: the clock is not the one asked for")
	}
	if _, err := reconcilerport.ReconcilerAt(reconciler.Config{})(sealedAt); err == nil {
		t.Error("a configuration with no rule set was accepted")
	}
	if _, err := reconcilerport.ReconcilerAt(w.baseConf)(time.Time{}); err == nil {
		t.Error("the zero time was accepted")
	}
}

// AGENT-13 and REQ-TRU-003 in one: no error of the adapter, whatever the path, carries a payload, a row, an id or a tenant. The paths are driven with a payload that holds a marker.
func TestNoErrorCarriesAPayloadAnIDOrATenant(t *testing.T) {
	w := newWorld(t, agree()...)
	paths := map[string]error{}
	w.sealed.Items[1].PayloadHash = "sha256:" + strings.Repeat("cd", 32)
	w.publish()
	_, paths["integrity"] = w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	w = newWorld(t, agree()...)
	w.sealed.Classification = "CONTRADICTORY_STATE"
	w.publish()
	_, paths["drift"] = w.adapter(t, finPack()).Reconcile(ctx(t), tenant, snapID)
	_, paths["unknown"] = w.adapter(t, finPack()).Reconcile(ctx(t), tenant, "evs_nosuch01")
	_, paths["no tenant"] = w.adapter(t, finPack()).Reconcile(ctx(t), "", snapID)
	_, paths["no deadline"] = w.adapter(t, finPack()).Reconcile(context.Background(), tenant, snapID)
	for name, err := range paths {
		if err == nil {
			t.Errorf("%s: no error", name)
			continue
		}
		for _, s := range []string{leak, tenant, foreign, snapID, "evr_", "pay_", "sha256:", "entries", "SUCCESS", "CONTRADICTORY"} {
			if strings.Contains(err.Error(), s) {
				t.Errorf("%s: the error carries %q: %v", name, s, err)
			}
		}
	}
}

// cleanScreen is a screen that finds nothing (SYNTHETIC); the Reader needs one (A372).
type cleanScreen struct{}

func (cleanScreen) Screen(context.Context, injection.Subject, map[string]string) (injection.Result, error) {
	return injection.Result{}, nil
}

func corrOf(context.Context) string { return "cor_SYNTHETIC" }

type noAlert struct{}

func (noAlert) ScreenFlagged(context.Context, payloadread.Flag) {}

// RES-05 (REQ-RES-005; interim, relayed): a snapshot whose items still wait inside their window comes back with the end of the window; one that does not wait comes back with none.
func TestAWaitingReclassificationCarriesTheEndOfTheWindow(t *testing.T) {
	for name, until := range map[string]int64{"waits": 1794906540, "does not wait": 0} {
		w := newWorld(t, agree()...)
		a, err := reconcilerport.New(reconcilerport.Config{Snapshots: w.store, Classifiers: func(time.Time) (reconcilerport.Classifier, error) {
			return fixedClassifier{readseal.Classified{ReconciliationID: "rec_synth", Classification: w.sealed.Classification, WaitUntil: until}}, nil
		}, Pack: finPack(), Timeout: time.Second})
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.Reconcile(ctx(t), tenant, snapID)
		if err != nil || got.WaitUntil != until {
			t.Errorf("%s: got %+v %v, want WaitUntil %d", name, got, err, until)
		}
	}
}
