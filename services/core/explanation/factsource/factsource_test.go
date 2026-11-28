package factsource_test

// EXP-01 / noema#586, ruling A155 (Founder approval of 17 Nov, relayed): the real FactsSource builds the `{fact:<kind>}` values of the slot inventory (explanation/facts-inventory.md)
// from what the Record and the Evidence Layer hold. This package is its pure core: typed source rows in, masked, bounded facts out, with no read of the Record or a clock. A value that
// cannot be made safely is an UNKNOWN with a why-code, never a guess or a partial value (INV-13, INV-19). Kinds: payment.amount, account, payment.time (no recipient kind: A711, A676), case.ref,
// case.team, case.clock. REQ-EXP-001, REQ-EXP-002, REQ-SEC-003, REQ-AUD-003; AGENT-13, AGENT-17. Every value below is SYNTHETIC.

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/explanation/factsource"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

var (
	lagos = time.FixedZone("WAT", 3600)
	asOf  = time.Date(2026, 11, 12, 9, 50, 0, 0, time.UTC)
	cfg   = factsource.Config{Exponents: map[string]int{"NGN": 2}, Zone: lagos, OwnerQueues: map[string]string{"PAYMENTS_OPERATIONS": "Payments Operations"}}
	fiveD = regexp.MustCompile(`[0-9]{5}`)
)

func payment() factsource.Payment {
	return factsource.Payment{AccountRef: "acc_NG0012345678904321", AmountMinor: 2000000, Currency: "NGN", OccurredAt: time.Date(2026, 11, 12, 9, 42, 0, 0, time.UTC), Direction: "DEBIT", Status: contracts.EnumsV1FactStatusKNOWN}
}

func byKind(fs []contracts.ExplanationFactsV2FactsItem) map[string]contracts.ExplanationFactsV2FactsItem {
	m := map[string]contracts.ExplanationFactsV2FactsItem{}
	for _, f := range fs {
		m[f.Kind] = f
	}
	return m
}

// The three payment values, as the slot inventory words them: the amount grouped in major units (money is integer minor units), the account's last four digits and the time in local
// words. No recipient kind exists (A711, A676).
func TestThePaymentFactsAreMaskedAndInTheInventorysWords(t *testing.T) {
	facts, unknowns := factsource.PaymentFacts(cfg, payment(), asOf, "ledger")
	if len(unknowns) != 0 {
		t.Fatalf("unknowns %+v", unknowns)
	}
	m := byKind(facts)
	want := map[string]string{"payment.amount": "NGN 20,000", "account": "ending 4321", "payment.time": "10:42 on 12 Nov"}
	for k, v := range want {
		if m[k].ValueMasked != v {
			t.Errorf("%s = %q, want %q", k, m[k].ValueMasked, v)
		}
		if m[k].FactStatus != contracts.ExplanationFactsV2FactsItemFactStatusKNOWN || m[k].SourceLabel != "ledger" || m[k].FactId == "" || len(m[k].FactId) > 64 {
			t.Errorf("%s: %+v", k, m[k])
		}
	}
	for _, f := range facts {
		if fiveD.MatchString(f.ValueMasked) || len(f.ValueMasked) > 200 || payloadguard.Mask(f.ValueMasked) != f.ValueMasked {
			t.Errorf("%s: %q is not masked and bounded", f.Kind, f.ValueMasked)
		}
		if strings.Contains(f.ValueMasked, "0012345678") || strings.Contains(f.ValueMasked, "Doe") || strings.Contains(f.ValueMasked, "NG00") {
			t.Errorf("%s: %q holds more than the masked form", f.Kind, f.ValueMasked)
		}
	}
}

// Minor units, not floats: 150 minor is "NGN 1.50", 100 is "NGN 1", a large amount is grouped, zero is allowed, negative and unsupported currencies are unknowns.
func TestTheAmountIsFormattedFromIntegerMinorUnits(t *testing.T) {
	cases := map[int64]string{150: "NGN 1.50", 100: "NGN 1", 5: "NGN 0.05", 1050: "NGN 10.50", 0: "NGN 0", 2000000000: "NGN 20,000,000"}
	for minor, want := range cases {
		p := payment()
		p.AmountMinor = minor
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		if got := byKind(facts)["payment.amount"].ValueMasked; got != want || len(unknowns) != 0 {
			t.Errorf("%d: %q, %+v; want %q", minor, got, unknowns, want)
		}
	}
	// A154/A194, then A308 (interim): the display definition joins digits across a dot, so "1,234.50" (the run "234.50" is five digits) used to be an unknown here, never a value with a digit
	// hidden or a cent dropped (noema#586). A308 gives the money exemption to Mask: an amount the core renders from integer minor units, three capitals, a space, grouped thousands and two
	// decimals, is served as it is. An amount of ten or more integer digits is still refused by the shape and stays an unknown.
	for minor, want := range map[int64]string{123450: "NGN 1,234.50", 123456789: "NGN 1,234,567.89", 100050: "NGN 1,000.50"} {
		p := payment()
		p.AmountMinor = minor
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		if got := byKind(facts)["payment.amount"].ValueMasked; got != want || len(unknowns) != 0 {
			t.Errorf("%d: %q, %+v; want %q", minor, got, unknowns, want)
		}
	}
	for _, minor := range []int64{100000000050} { // 1,000,000,000.50: ten integer digits with decimals, over the money rule's nine
		p := payment()
		p.AmountMinor = minor
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		if _, has := byKind(facts)["payment.amount"]; has || !hasUnknown(unknowns, "payment.amount") {
			t.Errorf("%d: facts %+v unknowns %+v; want an unknown and no amount", minor, facts, unknowns)
		}
	}
	for name, mut := range map[string]func(*factsource.Payment){
		"a negative amount":       func(p *factsource.Payment) { p.AmountMinor = -1 },
		"a currency not in table": func(p *factsource.Payment) { p.Currency = "XYZ" },
		"no currency":             func(p *factsource.Payment) { p.Currency = "" },
	} {
		p := payment()
		mut(&p)
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		if _, has := byKind(facts)["payment.amount"]; has || !hasUnknown(unknowns, "payment.amount") {
			t.Errorf("%s: facts %+v unknowns %+v; want an unknown and no amount", name, facts, unknowns)
		}
	}
}

func hasUnknown(us []contracts.ExplanationFactsV2UnknownsItem, kind string) bool {
	for _, u := range us {
		if u.Kind == kind && u.WhyCode != "" && regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`).MatchString(u.WhyCode) {
			return true
		}
	}
	return false
}

// A missing or unusable source field is an unknown for that kind alone; the other facts still build. An account reference with fewer than four digits is never shown.
func TestAMissingFieldIsAnUnknownForThatKindOnly(t *testing.T) {
	for name, tc := range map[string]struct {
		mut  func(*factsource.Payment)
		kind string
	}{
		"no account":        {func(p *factsource.Payment) { p.AccountRef = "" }, "account"},
		"three digits only": {func(p *factsource.Payment) { p.AccountRef = "acc_123" }, "account"},
		"no time":           {func(p *factsource.Payment) { p.OccurredAt = time.Time{} }, "payment.time"},
	} {
		p := payment()
		tc.mut(&p)
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		m := byKind(facts)
		if _, has := m[tc.kind]; has || !hasUnknown(unknowns, tc.kind) {
			t.Errorf("%s: want an unknown for %s and no fact; got %+v / %+v", name, tc.kind, facts, unknowns)
		}
		if len(facts) != 2 {
			t.Errorf("%s: %d facts built, want the 2 others", name, len(facts))
		}
	}
}

// A source row that is not KNOWN gives no fact (A155: KNOWN only fills a slot; INFERRED blocks), and a status that is neither is an unknown too.
func TestOnlyAKnownRowBecomesAFact(t *testing.T) {
	for _, st := range []contracts.EnumsV1FactStatus{contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusSTALE, contracts.EnumsV1FactStatusCONTRADICTORY, contracts.EnumsV1FactStatusUNKNOWN, ""} {
		p := payment()
		p.Status = st
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		if len(facts) != 0 || len(unknowns) != 3 {
			t.Errorf("status %q: %d facts, %d unknowns; want 0 and 3", st, len(facts), len(unknowns))
		}
	}
}

// The time is read in the configured zone with no default: no zone is an error, never UTC.
func TestTheTimeUsesTheConfiguredZoneAndNeverADefault(t *testing.T) {
	c := cfg
	c.Zone = nil
	facts, unknowns := factsource.PaymentFacts(c, payment(), asOf, "ledger")
	if _, has := byKind(facts)["payment.time"]; has || !hasUnknown(unknowns, "payment.time") {
		t.Errorf("no zone: %+v / %+v", facts, unknowns)
	}
}

// The case reference: the target's own reference without its year and masked to at most four digits (inventory; A194's mask), the owning team's display name from pack data, and
// the committed time only when the SLA event carries one (A206): never a literal.
func TestTheCaseFacts(t *testing.T) {
	facts, unknowns := factsource.Case(cfg, factsource.CaseRow{ExternalAck: "CMP-2026-00001", OwnerQueue: "PAYMENTS_OPERATIONS", CommittedBy: time.Date(2026, 11, 13, 14, 30, 0, 0, time.UTC), Status: contracts.EnumsV1FactStatusKNOWN}, asOf, "case system")
	if len(unknowns) != 0 {
		t.Fatalf("unknowns %+v", unknowns)
	}
	m := byKind(facts)
	if m["case.ref"].ValueMasked != "CMP-*0001" || m["case.team"].ValueMasked != "Payments Operations" || m["case.clock"].ValueMasked != "15:30 on 13 Nov" {
		t.Errorf("%q %q %q", m["case.ref"].ValueMasked, m["case.team"].ValueMasked, m["case.clock"].ValueMasked)
	}
	for _, f := range facts {
		if fiveD.MatchString(f.ValueMasked) {
			t.Errorf("%s: %q has a digit run", f.Kind, f.ValueMasked)
		}
	}
	// no committed time (A206): no clock fact and an unknown, never a made-up time; an unknown queue has no team name; no reference, no fact
	bad, us := factsource.Case(cfg, factsource.CaseRow{ExternalAck: "", OwnerQueue: "OTHER_QUEUE", Status: contracts.EnumsV1FactStatusKNOWN}, asOf, "case system")
	if len(bad) != 0 || !hasUnknown(us, "case.ref") || !hasUnknown(us, "case.team") || !hasUnknown(us, "case.clock") {
		t.Errorf("%+v / %+v", bad, us)
	}
}

// A reference whose digits cannot be masked into the contract (more than four digits in all after the year is removed, in any script) is masked by the one definition; the same
// definition refuses nothing here, it hides. A reference that is only digits still carries a mask mark.
func TestACaseReferenceNeverShowsMoreThanFourDigits(t *testing.T) {
	for _, ack := range []string{"CMP-2026-00001", "CMP-123456789", "٠١٢٣٤٥٦٧٨٩", "REF 12 34 56 78 90", "C-2026-99-0001-0002"} {
		facts, _ := factsource.Case(cfg, factsource.CaseRow{ExternalAck: ack, OwnerQueue: "PAYMENTS_OPERATIONS", Status: contracts.EnumsV1FactStatusKNOWN}, asOf, "case system")
		v := byKind(facts)["case.ref"].ValueMasked
		digits := 0
		for _, r := range v {
			if r >= '0' && r <= '9' || (r >= '٠' && r <= '٩') {
				digits++
			}
		}
		if v == "" || digits > 4 || len(v) > 64 {
			t.Errorf("%q gave %q (%d digits)", ack, v, digits)
		}
	}
}

// Codex P2 on #650: the source label goes into the customer-facing `source_label`, so it is held to the same safe-output rules as a value: non-empty, short, plain characters, and
// unchanged by the one digit definition. An unsafe label makes every fact of that call an unknown (LABEL_NOT_SAFE); nothing is built with it.
func TestAnUnsafeSourceLabelBuildsNothing(t *testing.T) {
	for name, label := range map[string]string{
		"account data":      "Account 1234567890",
		"empty":             "",
		"too long":          strings.Repeat("a", 65),
		"a newline":         "ledger\nCMP-1",
		"a control mark":    "ledger\x00",
		"leading digit":     "1ledger",
		"a digit run split": "ledger 1234-5678",
		"arabic-indic":      "ledger ٠١٢٣٤٥٦",
	} {
		facts, unknowns := factsource.PaymentFacts(cfg, payment(), asOf, label)
		if len(facts) != 0 || len(unknowns) != 3 {
			t.Errorf("%s: %d facts and %d unknowns, want 0 and 3", name, len(facts), len(unknowns))
		}
		for _, u := range unknowns {
			if u.WhyCode != "LABEL_NOT_SAFE" {
				t.Errorf("%s: why code %q", name, u.WhyCode)
			}
		}
		cf, cu := factsource.Case(cfg, factsource.CaseRow{ExternalAck: "CMP-2026-00001", OwnerQueue: "PAYMENTS_OPERATIONS", Status: contracts.EnumsV1FactStatusKNOWN}, asOf, label)
		if len(cf) != 0 || len(cu) != 3 {
			t.Errorf("%s (case): %d facts and %d unknowns, want 0 and 3", name, len(cf), len(cu))
		}
	}
	for _, label := range []string{"ledger", "case system", "SYNTHETIC ledger", "case-system.v2", strings.Repeat("a", 64)} {
		if facts, _ := factsource.PaymentFacts(cfg, payment(), asOf, label); len(facts) != 3 || facts[0].SourceLabel != label {
			t.Errorf("%q: %d facts", label, len(facts))
		}
	}
}

// C1's P2 on #650 (REQ-EXP-001, REQ-SEC-003; INV-13, AGENT-13): the account's last four digits are the last four of a number, not digits picked from anywhere in the reference. The
// reference must END in a run of at least four digits; anything else is an unknown, never a made-up "ending".
func TestTheAccountEndingIsTheLastFourDigitsOfTheNumber(t *testing.T) {
	for ref, want := range map[string]string{"acc_NG0012345678904321": "ending 4321", "4321": "ending 4321", "acc-9990": "ending 9990"} {
		p := payment()
		p.AccountRef = ref
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		if want != "" {
			if got := byKind(facts)["account"].ValueMasked; got != want {
				t.Errorf("%q: %q, want %q", ref, got, want)
			}
			continue
		}
		if _, has := byKind(facts)["account"]; has || !hasUnknown(unknowns, "account") {
			t.Errorf("%q: facts %+v unknowns %+v; want an unknown and no account", ref, facts, unknowns)
		}
	}
	for _, ref := range []string{"acc_01J9X7K2M4Q8", "acc_12 34 56 78", "acc_NG00123456789043X21", "abcd", "acc_4321x", "acc_٠١٢٣٤٥٦٧"} {
		p := payment()
		p.AccountRef = ref
		facts, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		if _, has := byKind(facts)["account"]; has || !hasUnknown(unknowns, "account") {
			t.Errorf("%q gave an account fact %+v / %+v; want an unknown", ref, facts, unknowns)
		}
	}
}

// C1's P2 on #650: a time with no year is ambiguous. The year is told whenever it is not the year of asOf; asOf itself must be set.
func TestATimeFromAnotherYearTellsTheYear(t *testing.T) {
	p := payment()
	p.OccurredAt = time.Date(2025, 11, 12, 9, 42, 0, 0, time.UTC)
	facts, _ := factsource.PaymentFacts(cfg, p, asOf, "ledger")
	if got := byKind(facts)["payment.time"].ValueMasked; got != "10:42 on 12 Nov 2025" {
		t.Errorf("%q", got)
	}
	cf, _ := factsource.Case(cfg, factsource.CaseRow{ExternalAck: "CMP-1", OwnerQueue: "PAYMENTS_OPERATIONS", CommittedBy: time.Date(2027, 1, 3, 14, 30, 0, 0, time.UTC), Status: contracts.EnumsV1FactStatusKNOWN}, asOf, "case system")
	if got := byKind(cf)["case.clock"].ValueMasked; got != "15:30 on 3 Jan 2027" {
		t.Errorf("%q", got)
	}
	for _, f := range append(facts, cf...) {
		if fiveD.MatchString(f.ValueMasked) || payloadguard.Mask(f.ValueMasked) != f.ValueMasked {
			t.Errorf("%s: %q is not safe", f.Kind, f.ValueMasked)
		}
	}
}

func TestAZeroAsOfBuildsNothing(t *testing.T) {
	facts, unknowns := factsource.PaymentFacts(cfg, payment(), time.Time{}, "ledger")
	cf, cu := factsource.Case(cfg, factsource.CaseRow{ExternalAck: "CMP-1", OwnerQueue: "PAYMENTS_OPERATIONS", Status: contracts.EnumsV1FactStatusKNOWN}, time.Time{}, "case system")
	if len(facts) != 0 || len(unknowns) != 3 || len(cf) != 0 || len(cu) != 3 {
		t.Fatalf("%d/%d and %d/%d", len(facts), len(unknowns), len(cf), len(cu))
	}
	for _, u := range append(unknowns, cu...) {
		if u.WhyCode != "AS_OF_MISSING" {
			t.Errorf("why code %q", u.WhyCode)
		}
	}
}

// C1's notes on #650: the as-of time of a fact is the caller's, an unknown item is never KNOWN, and the 200-character bound holds (pack data, the team name, is the longest value).
func TestAsOfStatusAndBoundOfTheItems(t *testing.T) {
	facts, _ := factsource.PaymentFacts(cfg, payment(), asOf, "ledger")
	for _, f := range facts {
		if string(f.AsOf) != "2026-11-12T09:50:00Z" {
			t.Errorf("%s: as_of %q", f.Kind, f.AsOf)
		}
	}
	for _, st := range []contracts.EnumsV1FactStatus{contracts.EnumsV1FactStatusKNOWN, contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusSTALE} {
		p := payment()
		p.Status = st
		p.AccountRef = ""
		_, unknowns := factsource.PaymentFacts(cfg, p, asOf, "ledger")
		for _, u := range unknowns {
			if u.Status == contracts.EnumsV1FactStatusKNOWN || u.FactId == "" {
				t.Errorf("status %s: unknown item %+v is KNOWN or has no id", st, u)
			}
		}
	}
	long := cfg
	long.OwnerQueues = map[string]string{"Q": strings.Repeat("T", 200), "R": strings.Repeat("T", 201)}
	ok, _ := factsource.Case(long, factsource.CaseRow{ExternalAck: "CMP-1", OwnerQueue: "Q", Status: contracts.EnumsV1FactStatusKNOWN}, asOf, "case system")
	tooLong, us := factsource.Case(long, factsource.CaseRow{ExternalAck: "CMP-1", OwnerQueue: "R", Status: contracts.EnumsV1FactStatusKNOWN}, asOf, "case system")
	if _, has := byKind(ok)["case.team"]; !has {
		t.Error("a 200-character team name was refused")
	}
	if _, has := byKind(tooLong)["case.team"]; has || !hasUnknown(us, "case.team") {
		t.Error("a 201-character team name was shown")
	}
}
