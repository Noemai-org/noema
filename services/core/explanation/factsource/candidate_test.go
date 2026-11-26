package factsource_test

// EXP-05 / A556 under A665 (interim, relayed), as A711 (interim, relayed) and A676 now require: the explainer's payment facts come from the CANDIDATE item (candidates-response.v2:
// amount, currency, occurred_at, account_last4; no name of any kind). The source asks a consumer-side CandidateReader for the one item of the resolved transaction, scoped to the
// caller's tenant, and builds THREE payment kinds (payment.amount, account, payment.time) through the one masking core (PaymentFacts). There is no recipient kind: no name leaves the
// candidates read (A676), so nothing here reads, copies, masks or emits one, and no template or inventory row may name the dropped kind (A711). The case kinds are unknowns (A556):
// nothing reads a case receipt here. REQ-EXP-001, REQ-EXP-002, REQ-SEC-004; INV-12, INV-13, INV-14, INV-19; AGENT-09, AGENT-13, AGENT-17. Every value below is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/explanation/factsource"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const (
	tenantA = "tnt_synthA"
	txnRef  = contracts.CommonV1ResourceRef("res_transaction_synth0001")
)

type readerFake struct {
	item   factsource.CandidateItem
	found  bool
	err    error
	calls  int
	gotTen string
	gotRef contracts.CommonV1ResourceRef
}

func (r *readerFake) Resolved(ctx context.Context, tenantID string, ref contracts.CommonV1ResourceRef) (factsource.CandidateItem, bool, error) {
	r.calls++
	r.gotTen, r.gotRef = tenantID, ref
	if err := ctx.Err(); err != nil {
		return factsource.CandidateItem{}, false, err
	}
	return r.item, r.found, r.err
}

func goodItem() factsource.CandidateItem {
	return factsource.CandidateItem{
		TenantID: tenantA,
		Candidate: contracts.CandidatesResponseV2Candidate{
			CandidateId: "cand_synth0001", ResourceRef: txnRef, AmountMinor: 2000000, Currency: "NGN", OccurredAt: "2026-11-12T09:42:00Z",
			CounterpartyRef: "cp_1", RecipientMatch: contracts.CandidatesResponseV2CandidateRecipientMatch{Numerator: 0, Denominator: 1}, AccountLast4: "4321",
			Direction: contracts.CandidatesResponseV2CandidateDirectionOUTGOING,
		},
		Status: contracts.EnumsV1FactStatusKNOWN, ReadAt: asOf, SourceLabel: "ledger",
	}
}

func newSource(t *testing.T, r factsource.CandidateReader) *factsource.PaymentSource {
	t.Helper()
	s, err := factsource.NewPaymentSource(cfg, r)
	if err != nil {
		t.Fatalf("NewPaymentSource: %v", err)
	}
	return s
}

// The three values come from the candidate item, in the inventory's words, and no recipient kind is made (A711); the reader is asked for the caller's tenant and the resolved
// reference only (A665).
func TestTheCandidateItemMakesTheThreePaymentFacts(t *testing.T) {
	r := &readerFake{item: goodItem(), found: true}
	facts, unknowns, err := newSource(t, r).PaymentFacts(context.Background(), tenantA, txnRef)
	if err != nil || len(unknowns) != 0 {
		t.Fatalf("err %v unknowns %+v", err, unknowns)
	}
	want := map[string]string{"payment.amount": "NGN 20,000", "account": "ending 4321", "payment.time": "10:42 on 12 Nov"}
	got := byKind(facts)
	if len(facts) != 3 {
		t.Fatalf("facts %+v", facts)
	}
	for k, v := range want {
		if got[k].ValueMasked != v || got[k].SourceLabel != "ledger" {
			t.Errorf("%s = %+v, want %q", k, got[k], v)
		}
	}
	if r.calls != 1 || r.gotTen != tenantA || r.gotRef != txnRef {
		t.Errorf("reader asked %d times for %q %q", r.calls, r.gotTen, r.gotRef)
	}
}

// An item that is not there is three unknowns and no fact (A155: only KNOWN fills a slot), not an error: the explainer then blocks the slot.
func TestNoItemIsThreeUnknownsAndNoFact(t *testing.T) {
	facts, unknowns, err := newSource(t, &readerFake{found: false}).PaymentFacts(context.Background(), tenantA, txnRef)
	if err != nil || len(facts) != 0 || len(unknowns) != 3 {
		t.Fatalf("err %v facts %+v unknowns %+v", err, facts, unknowns)
	}
	for _, u := range unknowns {
		if u.WhyCode != "SOURCE_NOT_KNOWN" {
			t.Errorf("%s why %q", u.Kind, u.WhyCode)
		}
	}
}

// Only a KNOWN item fills a slot: an INFERRED, STALE, AMBIGUOUS or UNAVAILABLE item is three unknowns (an AMBIGUOUS item is the ambiguous-candidate case: it fails closed).
func TestOnlyAKnownItemFillsASlot(t *testing.T) {
	for _, st := range []contracts.EnumsV1FactStatus{contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusSTALE, contracts.EnumsV1FactStatusAMBIGUOUS, contracts.EnumsV1FactStatusUNAVAILABLE, ""} {
		it := goodItem()
		it.Status = st
		facts, unknowns, err := newSource(t, &readerFake{item: it, found: true}).PaymentFacts(context.Background(), tenantA, txnRef)
		if err != nil || len(facts) != 0 || len(unknowns) != 3 {
			t.Errorf("status %q: err %v facts %+v unknowns %+v", st, err, facts, unknowns)
		}
	}
}

// Tenant scope (REQ-SEC-004): an item of another tenant, or an item of another resource than the one asked for, is refused with a typed error and no fact; nothing is shown.
func TestAnItemOfAnotherTenantOrResourceIsRefused(t *testing.T) {
	other := goodItem()
	other.TenantID = "tnt_synthB"
	if facts, unknowns, err := newSource(t, &readerFake{item: other, found: true}).PaymentFacts(context.Background(), tenantA, txnRef); !errors.Is(err, factsource.ErrTenantMismatch) || facts != nil || unknowns != nil {
		t.Errorf("tenant: err %v facts %+v unknowns %+v", err, facts, unknowns)
	}
	wrong := goodItem()
	wrong.Candidate.ResourceRef = "res_transaction_synth0002"
	if facts, unknowns, err := newSource(t, &readerFake{item: wrong, found: true}).PaymentFacts(context.Background(), tenantA, txnRef); !errors.Is(err, factsource.ErrWrongResource) || facts != nil || unknowns != nil {
		t.Errorf("resource: err %v facts %+v unknowns %+v", err, facts, unknowns)
	}
}

// A reader that fails (or a cancelled context) fails closed with a typed error that wraps the cause and names no value; the error text never carries the tenant or a reference.
func TestAReaderErrorFailsClosedAndNamesNoValue(t *testing.T) {
	boom := errors.New("ledger down for " + string(txnRef))
	facts, unknowns, err := newSource(t, &readerFake{err: boom}).PaymentFacts(context.Background(), tenantA, txnRef)
	if !errors.Is(err, factsource.ErrRead) || !errors.Is(err, boom) || facts != nil || unknowns != nil {
		t.Fatalf("err %v facts %+v unknowns %+v", err, facts, unknowns)
	}
	if strings.Contains(factsource.ErrRead.Error(), tenantA) || strings.Contains(factsource.ErrRead.Error(), string(txnRef)) {
		t.Errorf("ErrRead text %q names a value", factsource.ErrRead.Error())
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := newSource(t, &readerFake{item: goodItem(), found: true}).PaymentFacts(ctx, tenantA, txnRef); !errors.Is(err, factsource.ErrRead) {
		t.Errorf("cancelled context: err %v", err)
	}
}

// An empty tenant or reference is refused before the reader is asked; a nil reader refuses at construction.
func TestABadRequestNeverReachesTheReader(t *testing.T) {
	r := &readerFake{item: goodItem(), found: true}
	s := newSource(t, r)
	if _, _, err := s.PaymentFacts(context.Background(), "", txnRef); !errors.Is(err, factsource.ErrBadRequest) {
		t.Errorf("empty tenant: %v", err)
	}
	if _, _, err := s.PaymentFacts(context.Background(), tenantA, ""); !errors.Is(err, factsource.ErrBadRequest) {
		t.Errorf("empty ref: %v", err)
	}
	if r.calls != 0 {
		t.Errorf("reader asked %d times for a bad request", r.calls)
	}
	if _, err := factsource.NewPaymentSource(cfg, nil); !errors.Is(err, factsource.ErrNoReader) {
		t.Errorf("nil reader: %v", err)
	}
}

// A field the candidate cannot give safely is an unknown for that kind only; the other kinds stay known (never empty-as-known, never a guess).
func TestAnUnusableFieldIsAnUnknownForThatKindOnly(t *testing.T) {
	cases := map[string]struct {
		edit func(*contracts.CandidatesResponseV2Candidate)
		kind string
	}{
		"last4 of three digits":     {func(c *contracts.CandidatesResponseV2Candidate) { c.AccountLast4 = "432" }, "account"},
		"last4 not digits":          {func(c *contracts.CandidatesResponseV2Candidate) { c.AccountLast4 = "43a1" }, "account"},
		"last4 of five digits":      {func(c *contracts.CandidatesResponseV2Candidate) { c.AccountLast4 = "04321" }, "account"},
		"empty last4":               {func(c *contracts.CandidatesResponseV2Candidate) { c.AccountLast4 = "" }, "account"},
		"negative amount":           {func(c *contracts.CandidatesResponseV2Candidate) { c.AmountMinor = -1 }, "payment.amount"},
		"currency not in the table": {func(c *contracts.CandidatesResponseV2Candidate) { c.Currency = "ZZZ" }, "payment.amount"},
		"time not RFC 3339":         {func(c *contracts.CandidatesResponseV2Candidate) { c.OccurredAt = "yesterday" }, "payment.time"},
		"time without an offset":    {func(c *contracts.CandidatesResponseV2Candidate) { c.OccurredAt = "2026-11-12T09:42:00" }, "payment.time"},
		"empty time":                {func(c *contracts.CandidatesResponseV2Candidate) { c.OccurredAt = "" }, "payment.time"},
	}
	for name, tc := range cases {
		it := goodItem()
		tc.edit(&it.Candidate)
		facts, unknowns, err := newSource(t, &readerFake{item: it, found: true}).PaymentFacts(context.Background(), tenantA, txnRef)
		if err != nil || len(facts) != 2 || len(unknowns) != 1 || unknowns[0].Kind != tc.kind || unknowns[0].WhyCode != "FIELD_UNUSABLE" {
			t.Errorf("%s: err %v facts %d unknowns %+v", name, err, len(facts), unknowns)
		}
	}
}

// A zero read time builds nothing (no as-of): three unknowns; and an unsafe source label builds nothing either, as the core does.
func TestAMissingReadTimeOrAnUnsafeLabelBuildsNothing(t *testing.T) {
	noTime := goodItem()
	noTime.ReadAt = time.Time{}
	if facts, unknowns, err := newSource(t, &readerFake{item: noTime, found: true}).PaymentFacts(context.Background(), tenantA, txnRef); err != nil || len(facts) != 0 || len(unknowns) != 3 || unknowns[0].WhyCode != "AS_OF_MISSING" {
		t.Errorf("no read time: err %v facts %d unknowns %+v", err, len(facts), unknowns)
	}
	bad := goodItem()
	bad.SourceLabel = "ledger 12345678"
	if facts, unknowns, err := newSource(t, &readerFake{item: bad, found: true}).PaymentFacts(context.Background(), tenantA, txnRef); err != nil || len(facts) != 0 || len(unknowns) != 3 {
		t.Errorf("unsafe label: err %v facts %d unknowns %d", err, len(facts), len(unknowns))
	}
}

// The case kinds are unknowns (A556): the source reads no case receipt, plan or SLA event, and says so with a why-code; it never invents a reference, team or clock.
func TestTheCaseKindsAreUnknown(t *testing.T) {
	facts, unknowns := newSource(t, &readerFake{}).CaseFacts(asOf, "ledger")
	if len(facts) != 0 || len(unknowns) != 3 {
		t.Fatalf("facts %+v unknowns %+v", facts, unknowns)
	}
	seen := map[string]bool{}
	for _, u := range unknowns {
		seen[u.Kind] = true
	}
	for _, k := range []string{"case.ref", "case.team", "case.clock"} {
		if !seen[k] {
			t.Errorf("no unknown for %s", k)
		}
	}
}

// No float, no clock read, no network or random in the new file (INV-21: the facts are a pure function of the item), and the direction word is never spelled in code (INV-14: the
// pack's template vocabulary decides how a direction reads; the three kinds here do not show it).
func TestTheCandidateSourceIsPureAndHoldsNoVocabulary(t *testing.T) {
	b, err := os.ReadFile("candidate.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"float32", "float64", "time.Now(", "time.Since(", "math/rand", "net/http", "database/sql", "\"OUTGOING\"", "\"INCOMING\"", "strconv.ParseFloat"} {
		if strings.Contains(src, banned) {
			t.Errorf("candidate.go holds %q", banned)
		}
	}
	if strings.Contains(strings.ToLower(src), "counterparty") {
		t.Errorf("candidate.go names the counterparty: no name, no reference and no kind of the recipient is read or emitted (A676, A711)")
	}
}

// Every payment kind the source can emit, over every shape of item: facts and unknowns alike.
func emittedKinds(t *testing.T) map[string]bool {
	t.Helper()
	items := []factsource.CandidateItem{goodItem()}
	for _, st := range []contracts.EnumsV1FactStatus{contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusAMBIGUOUS, ""} {
		it := goodItem()
		it.Status = st
		items = append(items, it)
	}
	bad := goodItem()
	bad.Candidate.AccountLast4, bad.Candidate.OccurredAt, bad.Candidate.AmountMinor = "x", "no", -1
	items = append(items, bad)
	kinds := map[string]bool{}
	for _, it := range items {
		facts, unknowns, err := newSource(t, &readerFake{item: it, found: true}).PaymentFacts(context.Background(), tenantA, txnRef)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range facts {
			kinds[f.Kind] = true
		}
		for _, u := range unknowns {
			kinds[u.Kind] = true
		}
	}
	_, unknowns, _ := newSource(t, &readerFake{found: false}).PaymentFacts(context.Background(), tenantA, txnRef)
	for _, u := range unknowns {
		kinds[u.Kind] = true
	}
	return kinds
}

// The whitelist (A711): the source emits exactly the payment kinds the outcome declares, which is what the pack's status_explained templates hold as slots, and nothing else; no
// recipient kind, and no value or label carries the opaque reference or the direction.
func TestOnlyTheOutcomesDeclaredPaymentKindsAreEmitted(t *testing.T) {
	declared := map[string]bool{}
	slot := regexp.MustCompile(`\{fact:([a-z][a-z0-9_.]*)\}`)
	for key, text := range readTemplates(t) {
		if strings.HasPrefix(key, "fin.outcome.exception.status_explained/") {
			for _, m := range slot.FindAllStringSubmatch(text, -1) {
				declared[m[1]] = true
			}
		}
	}
	got := emittedKinds(t)
	want := map[string]bool{"payment.amount": true, "account": true, "payment.time": true}
	if len(declared) != len(want) || len(got) != len(want) {
		t.Fatalf("declared %v emitted %v want %v", declared, got, want)
	}
	for k := range want {
		if !declared[k] || !got[k] {
			t.Errorf("kind %s: declared %v emitted %v", k, declared[k], got[k])
		}
	}
	facts, _, _ := newSource(t, &readerFake{item: goodItem(), found: true}).PaymentFacts(context.Background(), tenantA, txnRef)
	for _, f := range facts {
		if strings.Contains(f.ValueMasked, "cp_") || strings.Contains(f.SourceLabel, "cp_") || strings.Contains(strings.ToLower(f.ValueMasked), "outgoing") {
			t.Errorf("%s: %+v carries the opaque reference or the direction", f.Kind, f)
		}
	}
}

// Tenant and transaction binding (REQ-SEC-004): an item with no tenant or no resource is not the caller's item either; it is refused, never shown.
func TestAnItemWithNoTenantOrNoResourceIsRefused(t *testing.T) {
	noTenant := goodItem()
	noTenant.TenantID = ""
	if facts, unknowns, err := newSource(t, &readerFake{item: noTenant, found: true}).PaymentFacts(context.Background(), tenantA, txnRef); !errors.Is(err, factsource.ErrTenantMismatch) || facts != nil || unknowns != nil {
		t.Errorf("no tenant: err %v facts %+v unknowns %+v", err, facts, unknowns)
	}
	noRes := goodItem()
	noRes.Candidate.ResourceRef = ""
	if facts, unknowns, err := newSource(t, &readerFake{item: noRes, found: true}).PaymentFacts(context.Background(), tenantA, txnRef); !errors.Is(err, factsource.ErrWrongResource) || facts != nil || unknowns != nil {
		t.Errorf("no resource: err %v facts %+v unknowns %+v", err, facts, unknowns)
	}
}
