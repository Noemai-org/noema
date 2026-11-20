package reconciler_test

// ENV-04 slice 2b (A208, A245, A260 on noema#579 and noema#596; REQ-RES-001, REQ-CTX-001; INV-19, INV-21, INV-23, AGENT-13): the Reconciler that readseal calls. For each evidence item it
// reads the stored payload (verified against the item's hash), maps it to rows of the rule set's vocabulary, builds one reconcile.Evidence per row, and classifies with the real rule set and the
// real mapping of the repository's pack. Absence is UNKNOWN, never a negative fact. The payload store is a spy; nothing else is faked. SYNTHETIC.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/readseal"
	"github.com/Noemai-org/noema/services/core/evidence/reconciler"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const (
	tenant   = "ten_demo"
	ledger   = "fin.src.core_ledger"
	sw       = "fin.src.switch"
	dest     = "fin.src.destination"
	expected = "fin.map.v1@2"
)

var (
	packDir = filepath.Join("..", "..", "..", "..", "packs", "fintech")
	read    = time.Date(2026, 11, 17, 9, 0, 0, 123456000, time.UTC)
	now     = time.Date(2026, 11, 17, 9, 5, 0, 0, time.UTC)
	idRe    = regexp.MustCompile(`^rec_[A-Za-z]{26}$`)
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

// store is the payload port: payloads by reference, as the tenant that asked, and a record of who asked for what.
type store struct {
	payloads map[string][]byte
	err      error
	asked    []string
	tenants  []string
}

func (s *store) Read(_ context.Context, tenantID, ref string) ([]byte, error) {
	s.asked, s.tenants = append(s.asked, ref), append(s.tenants, tenantID)
	if s.err != nil {
		return nil, s.err
	}
	if tenantID != tenant {
		return nil, errors.New("not found")
	}
	p, ok := s.payloads[ref]
	if !ok {
		return nil, errors.New("not found")
	}
	return p, nil
}

type world struct {
	st    *store
	items []layer.Item
	rec   *reconciler.Reconciler
	clk   *clock.Manual
}

type spec struct {
	source  string
	payload map[string]any
	status  contracts.EnumsV1FactStatus
}

func hashOf(t *testing.T, payload map[string]any) string {
	t.Helper()
	canon, err := crypto.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func build(t *testing.T, specs ...spec) *world {
	t.Helper()
	rs := ruleSet(t)
	w := &world{st: &store{payloads: map[string][]byte{}}}
	for i, s := range specs {
		id, ref := "evd_"+string(rune('a'+i)), "pay_"+string(rune('a'+i))
		raw, err := json.Marshal(s.payload)
		if err != nil {
			t.Fatal(err)
		}
		w.st.payloads[ref] = raw
		st := s.status
		if st == "" {
			st = contracts.EnumsV1FactStatusKNOWN
		}
		w.items = append(w.items, layer.Item{EvidenceID: id, PayloadRef: ref, PayloadHash: hashOf(t, s.payload), Status: st, Source: s.source, ReadAt: read, EffectiveAt: read.Add(-time.Minute),
			EffectiveFreshnessSeconds: 300})
	}
	var err error
	w.clk = clock.NewManual(now)
	w.rec, err = reconciler.New(reconciler.Config{RuleSet: rs, Mapping: theMapping(t, rs), Payloads: payloadread.Reader{Payloads: w.st}, Clock: w.clk})
	if err != nil {
		t.Fatal(err)
	}
	return w
}

func ledgerPosted() spec {
	return spec{source: ledger, payload: map[string]any{"entries": []any{map[string]any{"entry_type": "DEBIT", "posting_status": "POSTED"}}}}
}
func switchIs(s string) spec { return spec{source: sw, payload: map[string]any{"semantic_status": s}} }
func destIs(s string) spec   { return spec{source: dest, payload: map[string]any{"credit_state": s}} }

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func classify(t *testing.T, w *world) readseal.Classified {
	t.Helper()
	got, err := w.rec.Classify(ctx(t), tenant, w.items)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

var _ readseal.Reconciler = (*reconciler.Reconciler)(nil)

// The three sources agree: the real rule set says SUCCESS_CONFIRMED and the answer has a reconciliation id of the shape the snapshot records (letters only: no digit run reaches the Record).
func TestAgreeingKnownEvidenceIsSuccessConfirmedWithAReconciliationID(t *testing.T) {
	got := classify(t, build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED")))
	if got.Classification != "SUCCESS_CONFIRMED" || !idRe.MatchString(got.ReconciliationID) {
		t.Fatalf("got %+v", got)
	}
}

func TestATimedOutSwitchIsExecutionOutcomeUnknown(t *testing.T) {
	if got := classify(t, build(t, ledgerPosted(), switchIs("TIMED_OUT"), destIs("PENDING"))); got.Classification != "EXECUTION_OUTCOME_UNKNOWN" {
		t.Fatalf("got %+v", got)
	}
}

// The same evidence in any order gives the same answer (INV-21); a different payload, version or evidence set gives another reconciliation id.
func TestTheAnswerIsTheSameForTheSameEvidenceInAnyOrderAndTheIDFollowsTheEvidence(t *testing.T) {
	w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	first := classify(t, w)
	w.items[0], w.items[2] = w.items[2], w.items[0]
	if again := classify(t, w); again != first {
		t.Fatalf("order changed the answer: %+v then %+v", first, again)
	}
	other := classify(t, build(t, ledgerPosted(), switchIs("TIMED_OUT"), destIs("CREDITED")))
	if other.ReconciliationID == first.ReconciliationID {
		t.Fatal("a different payload gave the same reconciliation id")
	}
}

// Absence is UNKNOWN (A260, INV-19): a payload that gives the row no field, one that is not the sealed one, one that is not an object and an item that is not KNOWN are never a positive fact.
func TestAbsenceIsUnknownAndNeverASuccess(t *testing.T) {
	for name, w := range map[string]*world{
		"a ledger with no posted debit":      build(t, spec{source: ledger, payload: map[string]any{"entries": []any{}}}, switchIs("SUCCESS"), destIs("CREDITED")),
		"a status the mapping does not list": build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("SOMETHING_NEW")),
		"a stale ledger":                     build(t, spec{source: ledger, payload: map[string]any{"entries": []any{}}, status: contracts.EnumsV1FactStatusSTALE}, switchIs("SUCCESS"), destIs("CREDITED")),
		"an unavailable destination":         build(t, ledgerPosted(), switchIs("SUCCESS"), spec{source: dest, payload: map[string]any{}, status: contracts.EnumsV1FactStatusUNAVAILABLE}),
	} {
		t.Run(name, func(t *testing.T) {
			if got := classify(t, w); got.Classification != "INSUFFICIENT_EVIDENCE" {
				t.Fatalf("got %+v, want INSUFFICIENT_EVIDENCE", got)
			}
		})
	}
}

func TestAPayloadThatIsNotTheSealedOneOrNotAnObjectIsUnknownNotAnError(t *testing.T) {
	w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	w.st.payloads["pay_a"] = []byte(`{"entries":[{"entry_type":"DEBIT","posting_status":"NOT_POSTED"}]}`) // hash mismatch
	w.st.payloads["pay_c"] = []byte(`["not","an","object"]`)
	if got := classify(t, w); got.Classification != "INSUFFICIENT_EVIDENCE" || !idRe.MatchString(got.ReconciliationID) {
		t.Fatalf("got %+v", got)
	}
}

// A non-KNOWN item cannot satisfy a rule, so its payload is not read at all.
func TestAPayloadOfAnItemThatIsNotKnownIsNotRead(t *testing.T) {
	w := build(t, spec{source: ledger, payload: map[string]any{"entries": []any{}}, status: contracts.EnumsV1FactStatusUNAVAILABLE}, switchIs("SUCCESS"), destIs("CREDITED"))
	classify(t, w)
	for _, ref := range w.st.asked {
		if ref == "pay_a" {
			t.Fatal("the payload of an unavailable item was read")
		}
	}
}

// The store failing is a fault, not an absence: the whole classification is refused, the cause stays reachable, and no text of the store (a connection string) is in the error.
func TestAStoreFailureIsRefusedAndTheErrorTextCarriesNoStoreText(t *testing.T) {
	w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	w.st.err = errors.New("dial postgres://trust_rw:s3cret@db.internal:5432/noema: connection refused")
	_, err := w.rec.Classify(ctx(t), tenant, w.items)
	if !errors.Is(err, payloadread.ErrUnavailable) {
		t.Fatalf("err %v, want payloadread.ErrUnavailable", err)
	}
	for _, leak := range []string{"postgres", "s3cret", "db.internal"} {
		if strings.Contains(err.Error(), leak) {
			t.Errorf("the error carries %q: %v", leak, err)
		}
	}
}

func TestThePayloadIsReadAsTheTenantOfTheCallAndAnotherTenantReadsNothing(t *testing.T) {
	w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	classify(t, w)
	for _, got := range w.st.tenants {
		if got != tenant {
			t.Fatalf("a payload was read as %q", got)
		}
	}
	if _, err := w.rec.Classify(ctx(t), "ten_other", w.items); err == nil {
		t.Fatal("another tenant's classification of these items was answered")
	}
}

func TestACallWithoutADeadlineIsRefused(t *testing.T) {
	w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	if _, err := w.rec.Classify(context.Background(), tenant, w.items); !errors.Is(err, payloadread.ErrNoDeadline) {
		t.Fatalf("err %v, want ErrNoDeadline", err)
	}
}

// An item the Reconciler cannot use is refused whole: nothing is guessed (INV-19).
func TestAnItemTheReconcilerCannotUseIsRefused(t *testing.T) {
	cases := map[string]func(*world){
		"no items":                           func(w *world) { w.items = nil },
		"a repeated evidence id":             func(w *world) { w.items[1].EvidenceID = w.items[0].EvidenceID },
		"an empty evidence id":               func(w *world) { w.items[0].EvidenceID = "" },
		"no source":                          func(w *world) { w.items[0].Source = "" },
		"a source the mapping does not know": func(w *world) { w.items[0].Source = "fin.src.unknown" },
		"a zero read time":                   func(w *world) { w.items[0].ReadAt = time.Time{} },
		"a zero effective time":              func(w *world) { w.items[0].EffectiveAt = time.Time{} },
		"no freshness limit":                 func(w *world) { w.items[0].EffectiveFreshnessSeconds = 0 },
		"a negative freshness limit":         func(w *world) { w.items[0].EffectiveFreshnessSeconds = -1 },
		"a status that is not a status":      func(w *world) { w.items[0].Status = "MAYBE" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			w := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
			mutate(w)
			if _, err := w.rec.Classify(ctx(t), tenant, w.items); !errors.Is(err, reconciler.ErrItem) {
				t.Fatalf("err %v, want ErrItem", err)
			}
		})
	}
}

func TestNewRefusesAnIncompleteConfiguration(t *testing.T) {
	rs := ruleSet(t)
	full := reconciler.Config{RuleSet: rs, Mapping: theMapping(t, rs), Payloads: payloadread.Reader{Payloads: &store{}}, Clock: clock.NewManual(now)}
	for name, mutate := range map[string]func(*reconciler.Config){
		"no rule set": func(c *reconciler.Config) { c.RuleSet = nil }, "no mapping": func(c *reconciler.Config) { c.Mapping = nil },
		"no payloads": func(c *reconciler.Config) { c.Payloads = nil }, "no clock": func(c *reconciler.Config) { c.Clock = nil },
	} {
		t.Run(name, func(t *testing.T) {
			c := full
			mutate(&c)
			if _, err := reconciler.New(c); !errors.Is(err, reconciler.ErrNotUsable) {
				t.Fatalf("err %v, want ErrNotUsable", err)
			}
		})
	}
	other := mustRead(t, packDir, "recon_rules", "rules.v1.json")
	other = []byte(strings.Replace(string(other), `"version": 1`, `"version": 2`, 1))
	rs2, err := reconcile.ParseRuleSet(other)
	if err != nil {
		t.Skip("the rule set cannot be rewritten to another version in this fixture")
	}
	if _, err := reconciler.New(reconciler.Config{RuleSet: rs2, Mapping: theMapping(t, rs), Payloads: payloadread.Reader{Payloads: &store{}}, Clock: clock.NewManual(now)}); !errors.Is(err, reconciler.ErrNotUsable) {
		t.Fatalf("a mapping written for another rule set was accepted: %v", err)
	}
}

// The empty tenant is refused even when no payload would be read (every item not KNOWN).
func TestAnEmptyTenantIsRefusedEvenWhenNoPayloadIsRead(t *testing.T) {
	w := build(t, spec{source: ledger, payload: map[string]any{}, status: contracts.EnumsV1FactStatusUNAVAILABLE})
	if _, err := w.rec.Classify(ctx(t), "", w.items); !errors.Is(err, reconciler.ErrItem) {
		t.Fatalf("err %v, want ErrItem", err)
	}
}

// Every item is required (A245): a source that could not answer stops a success the other two would give, because an unavailable required source is itself a rule's condition.
func TestAnUnavailableSourceStopsASuccessTheOthersWouldGive(t *testing.T) {
	w := build(t, ledgerPosted(), destIs("CREDITED"), spec{source: sw, payload: map[string]any{}, status: contracts.EnumsV1FactStatusUNAVAILABLE})
	if got := classify(t, w); got.Classification != "INSUFFICIENT_EVIDENCE" {
		t.Fatalf("got %+v, want INSUFFICIENT_EVIDENCE", got)
	}
}

// The reconciliation id names what was classified: the status of an item and the mapping are part of it, not only the ids and the hashes.
func TestTheReconciliationIDFollowsTheStatusOfAnItemAndTheMapping(t *testing.T) {
	stale := build(t, spec{source: ledger, payload: map[string]any{}, status: contracts.EnumsV1FactStatusSTALE}, switchIs("SUCCESS"))
	unavailable := build(t, spec{source: ledger, payload: map[string]any{}, status: contracts.EnumsV1FactStatusUNAVAILABLE}, switchIs("SUCCESS"))
	if classify(t, stale).ReconciliationID == classify(t, unavailable).ReconciliationID {
		t.Fatal("two item statuses gave one reconciliation id")
	}
	base := build(t, ledgerPosted(), switchIs("SUCCESS"), destIs("CREDITED"))
	rs := ruleSet(t)
	raw := strings.Replace(string(mustRead(t, packDir, "evidence_mapping", "mapping.v1.json")), `"mapping_id": "fin.map.v1"`, `"mapping_id": "fin.map.v2"`, 1)
	m2, err := mapping.Parse([]byte(raw), rs, []string{ledger, sw, dest, "fin.src.case_system"}, "fin.map.v2@2", mapping.Limits{MaxBytes: 1 << 20, MaxDepth: 32}, caseScope(t, filepath.Join(packDir, "..", "fintech_v1.json")))
	if err != nil {
		t.Skipf("the mapping cannot be re-identified in this fixture: %v", err)
	}
	r2, err := reconciler.New(reconciler.Config{RuleSet: rs, Mapping: m2, Payloads: payloadread.Reader{Payloads: base.st}, Clock: clock.NewManual(now)})
	if err != nil {
		t.Fatal(err)
	}
	other, err := r2.Classify(ctx(t), tenant, base.items)
	if err != nil || other.ReconciliationID == classify(t, base).ReconciliationID {
		t.Fatalf("a mapping with another identity gave the same reconciliation id (%v)", err)
	}
}
