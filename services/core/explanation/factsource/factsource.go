// Package factsource is the pure core of the real FactsSource (EXP-01, ruling A155; docs: explanation/facts-inventory.md): it turns typed source rows (what the Record and the Evidence
// Layer hold, already read by the caller) into the masked, bounded facts and unknowns of explanation-facts.v2. It reads no Record, no store and no clock.
//
// May import: the standard library, internal/contracts, record/payloadguard (the one digit definition, A194).
//
// Must never: show a full account reference or name, make a value from a row that is not KNOWN, default a time zone or an exponent, guess a missing field, or return a value that
// holds a run of more than four digits (INV-13, INV-19, AGENT-13, AGENT-17). A value that cannot be made safely is an unknown with a why-code.
package factsource

import (
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/record/payloadguard"
)

// Config is the deployment's and the pack's data; nothing here has a default (AGENT-16). Exponents is the currency table (contracts/currencies.json): money is integer minor units.
// Zone is the zone the times are told in. OwnerQueues maps an owner queue to the team's display name (pack data).
type Config struct {
	Exponents   map[string]int
	Zone        *time.Location
	OwnerQueues map[string]string
}

// Payment is the recorded evidence payload of the resolved transaction (the inventory's field names: account_ref, amount_minor with currency, occurred_at, direction)
// and the status of its evidence item. It holds no recipient: no name leaves the candidates read (A676, A711).
type Payment struct {
	AccountRef  string
	AmountMinor int64
	Currency    string
	OccurredAt  time.Time
	Direction   string
	Status      contracts.EnumsV1FactStatus
}

// CaseRow is what the create-case receipt and the plan hold: the target's own reference (`external_ack`), the owner queue and the time the SLA clock committed (zero when the SLA
// event carries none, A206).
type CaseRow struct {
	ExternalAck string
	OwnerQueue  string
	CommittedBy time.Time
	Status      contracts.EnumsV1FactStatus
}

const (
	maxValue = 200 // value_masked, explanation-facts.v2
	maxRef   = 64
)

var (
	yearGroup = regexp.MustCompile(`-(?:19|20)[0-9]{2}-`)
	// labelShape is a source label: a letter first, then letters, digits, space, dot, colon, underscore or hyphen, at most 64 characters (it goes to the customer's facts).
	labelShape = regexp.MustCompile(`\A[A-Za-z][A-Za-z0-9 ._:-]{0,63}\z`)
	fiveASCII  = regexp.MustCompile(`[0-9]{5}`)
)

type builder struct {
	asOf   string
	label  string
	unsafe bool // the source label failed labelSafe: every fact of the call is an unknown
	noAsOf bool // asOf is the zero time: every fact of the call is an unknown
	asOfT  time.Time
	facts  []contracts.ExplanationFactsV2FactsItem
	unkn   []contracts.ExplanationFactsV2UnknownsItem
	status contracts.EnumsV1FactStatus
}

func id(prefix, kind string) string { return prefix + strings.ReplaceAll(kind, ".", "_") }

func (b *builder) unknown(kind, why string) {
	st := b.status
	if st == contracts.EnumsV1FactStatusKNOWN || st == "" {
		st = contracts.EnumsV1FactStatusUNAVAILABLE
	}
	b.unkn = append(b.unkn, contracts.ExplanationFactsV2UnknownsItem{FactId: id("unk_", kind), Kind: kind, Status: st, WhyCode: why})
}

// add records a fact when its value is safe to show: bounded, no run of more than four digits by the one definition, and unchanged by it (already masked). A whole money string is safe
// (payment.amount): MaskText keeps it. This fact value is structured, not customer-visible text, so A331 ("money in customer-visible text only") would not exempt it; the exemption is kept
// here because the explanation templates render the customer's amount from it, and dropping it makes every amount with decimals an unknown. The Architect is asked to decide (status/CON-digit-tightenings.md).
func (b *builder) add(kind, value string) {
	if value == "" || utf8.RuneCountInString(value) > maxValue || payloadguard.MaskText(value) != value || fiveASCII.MatchString(value) {
		b.unknown(kind, "VALUE_NOT_SAFE")
		return
	}
	b.facts = append(b.facts, contracts.ExplanationFactsV2FactsItem{FactId: id("fct_", kind), Kind: kind, FactStatus: contracts.ExplanationFactsV2FactsItemFactStatusKNOWN, ValueMasked: value, SourceLabel: b.label, AsOf: contracts.CommonV1Timestamp(b.asOf)})
}

// labelSafe holds a source label to the rules of a value: the allowed shape and unchanged by the one digit definition (so a label cannot carry a run of more than four digits).
func labelSafe(label string) bool {
	return labelShape.MatchString(label) && payloadguard.Mask(label) == label && !fiveASCII.MatchString(label)
}

func newBuilder(asOf time.Time, label string, st contracts.EnumsV1FactStatus) *builder {
	return &builder{asOf: asOf.UTC().Format(time.RFC3339), asOfT: asOf, label: label, status: st, unsafe: !labelSafe(label), noAsOf: asOf.IsZero()}
}

// known is true only for a KNOWN row with a safe source label; otherwise the caller emits unknowns, with the why-code from why.
func (b *builder) known() bool {
	return !b.unsafe && !b.noAsOf && b.status == contracts.EnumsV1FactStatusKNOWN
}

func (b *builder) why() string {
	switch {
	case b.unsafe:
		return "LABEL_NOT_SAFE"
	case b.noAsOf:
		return "AS_OF_MISSING"
	}
	return "SOURCE_NOT_KNOWN"
}

// PaymentFacts makes payment.amount, account and payment.time. A row that is not KNOWN gives three unknowns and no fact (A155: only KNOWN fills a slot).
func PaymentFacts(cfg Config, p Payment, asOf time.Time, label string) ([]contracts.ExplanationFactsV2FactsItem, []contracts.ExplanationFactsV2UnknownsItem) {
	b := newBuilder(asOf, label, p.Status)
	kinds := []string{"payment.amount", "account", "payment.time"}
	if !b.known() {
		for _, k := range kinds {
			b.unknown(k, b.why())
		}
		return b.facts, b.unkn
	}
	if v, ok := amount(cfg, p); ok {
		b.add("payment.amount", v)
	} else {
		b.unknown("payment.amount", "FIELD_UNUSABLE")
	}
	if d := suffixDigits(p.AccountRef, 4); d != "" {
		b.add("account", "ending "+d)
	} else {
		b.unknown("account", "FIELD_UNUSABLE")
	}
	if v, ok := clockWords(cfg.Zone, p.OccurredAt, b.asOfT); ok {
		b.add("payment.time", v)
	} else {
		b.unknown("payment.time", "FIELD_UNUSABLE")
	}
	return b.facts, b.unkn
}

// Case makes case.ref, case.team and case.clock. A missing reference, an unknown queue and an SLA event with no committed time are unknowns (A206): nothing is invented.
func Case(cfg Config, c CaseRow, asOf time.Time, label string) ([]contracts.ExplanationFactsV2FactsItem, []contracts.ExplanationFactsV2UnknownsItem) {
	b := newBuilder(asOf, label, c.Status)
	if !b.known() {
		for _, k := range []string{"case.ref", "case.team", "case.clock"} {
			b.unknown(k, b.why())
		}
		return b.facts, b.unkn
	}
	if ref := strings.TrimSpace(yearGroup.ReplaceAllString(c.ExternalAck, "-")); ref != "" && utf8.RuneCountInString(payloadguard.Mask(ref)) <= maxRef {
		b.add("case.ref", payloadguard.Mask(ref))
	} else {
		b.unknown("case.ref", "FIELD_UNUSABLE")
	}
	if name, ok := cfg.OwnerQueues[c.OwnerQueue]; ok && name != "" {
		b.add("case.team", name)
	} else {
		b.unknown("case.team", "QUEUE_NOT_NAMED")
	}
	if v, ok := clockWords(cfg.Zone, c.CommittedBy, b.asOfT); ok {
		b.add("case.clock", v)
	} else {
		b.unknown("case.clock", "CLOCK_NOT_COMMITTED")
	}
	return b.facts, b.unkn
}

// amount is "<CUR> <major, grouped>[.<minor>]" from integer minor units and the currency table; a negative amount, a currency not in the table and an exponent outside 0 to 4 are refused.
func amount(cfg Config, p Payment) (string, bool) {
	exp, ok := cfg.Exponents[p.Currency]
	if !ok || p.Currency == "" || p.AmountMinor < 0 || exp < 0 || exp > 4 {
		return "", false
	}
	scale := int64(1)
	for i := 0; i < exp; i++ {
		scale *= 10
	}
	major, frac := p.AmountMinor/scale, p.AmountMinor%scale
	s := p.Currency + " " + group(strconv.FormatInt(major, 10))
	if frac != 0 {
		f := strconv.FormatInt(frac, 10)
		s += "." + strings.Repeat("0", exp-len(f)) + f
	}
	return s, true
}

func group(digits string) string {
	var out []byte
	for i := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out = append(out, ',')
		}
		out = append(out, digits[i])
	}
	return string(out)
}

// clockWords is the time in the configured zone in words ("10:42 on 12 Nov"), with the year when it is not the year of asOf ("10:42 on 12 Nov 2025"); no zone and a zero time are
// refused (never a default zone).
func clockWords(zone *time.Location, t, asOf time.Time) (string, bool) {
	if zone == nil || t.IsZero() {
		return "", false
	}
	local := t.In(zone)
	if local.Year() != asOf.In(zone).Year() {
		return local.Format("15:04 on 2 Jan 2006"), true
	}
	return local.Format("15:04 on 2 Jan"), true
}

// suffixDigits is the last n digits of a number: s must END in a run of at least n ASCII digits ("acc_0012345678904321" gives "4321"). Digits scattered through s, a run shorter
// than n, or digits that are not at the end give "": no "ending" is made from pieces (C1's P2 on noema#650).
func suffixDigits(s string, n int) string {
	end := len(s)
	start := end
	for start > 0 && s[start-1] >= '0' && s[start-1] <= '9' {
		start--
	}
	if end-start < n {
		return ""
	}
	return s[end-n:]
}
