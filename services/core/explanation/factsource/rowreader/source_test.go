package rowreader_test

// EXP-01 / A710 (the Architect's order of 26 Nov 2026, interim, relayed: the explainer row reader behind explanation.FactsSource is C2's; A550, A556, A665, A675 stand): the Source turns what
// the stored rows say (rowreader.Rows: the decision, its sealed classification, the subject of its intent, and the receipts and verdicts of its investigation) into the
// explanation-facts.v2 the Explainer reads. Only a KNOWN fact fills a slot (A155); an absent, ambiguous or mismatched row is a typed error and no fact; a receipt is a pointer and never proof
// (INV-04, INV-07): a case reference needs an ACKNOWLEDGED receipt with its acknowledgement AND an ACHIEVED verdict. Every value below is SYNTHETIC. REQ-EXP-001, REQ-EXP-002, REQ-SEC-004;
// INV-04, INV-07, INV-12, INV-13, INV-14, INV-19, INV-21; AGENT-09, AGENT-13, AGENT-17.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/explanation"
	"github.com/Noemai-org/noema/services/core/explanation/factsource"
	"github.com/Noemai-org/noema/services/core/explanation/factsource/rowreader"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	tenantA   = "ten_aa"
	principal = "usr_100"
	decision  = "dec_case01"
	corr      = "corr_abc123"
	txnRef    = contracts.CommonV1ResourceRef("syn.transaction:TX0001")
	caseCap   = "syn.cap.create_case"
	caseOut   = "syn.outcome.case_opened"
)

var (
	lagos = time.FixedZone("WAT", 3600)
	now   = time.Date(2026, 11, 12, 9, 50, 0, 0, time.UTC)
	ack   = "CMP-2026-0001"
)

func factsCfg() factsource.Config {
	return factsource.Config{Exponents: map[string]int{"NGN": 2}, Zone: lagos, OwnerQueues: map[string]string{"SYNTH_QUEUE": "Payments Operations"}}
}

func cfg() rowreader.Config {
	return rowreader.Config{Facts: factsCfg(), Label: "case system", CaseCapability: caseCap, QueueParam: "owner_queue"}
}

// caseExec is the case receipt of the decision, acknowledged and verified.
func caseExec() rowreader.Execution {
	a := ack
	return rowreader.Execution{GrantID: "grt_0001", CapabilityID: caseCap, ReceiptID: "rcp_0001", State: "ACKNOWLEDGED", ExternalAck: &a, OwnGrant: true, Verdict: "ACHIEVED", OutcomeID: caseOut}
}

func goodRows() rowreader.Rows {
	return rowreader.Rows{DecisionID: decision, CorrelationID: corr, Classification: "EXECUTION_OUTCOME_UNKNOWN", SubjectRef: string(txnRef), Executions: []rowreader.Execution{caseExec()}}
}

type readerFake struct {
	rows           rowreader.Rows
	err            error
	calls          int
	gotTen, gotDec string
}

func (r *readerFake) Read(_ context.Context, tenantID, decisionID string) (rowreader.Rows, error) {
	r.calls++
	r.gotTen, r.gotDec = tenantID, decisionID
	return r.rows, r.err
}

type paymentsFake struct {
	err    error
	calls  int
	gotTen string
	gotRef contracts.CommonV1ResourceRef
}

func (p *paymentsFake) PaymentFacts(_ context.Context, tenantID string, ref contracts.CommonV1ResourceRef) ([]contracts.ExplanationFactsV2FactsItem, []contracts.ExplanationFactsV2UnknownsItem, error) {
	p.calls++
	p.gotTen, p.gotRef = tenantID, ref
	if p.err != nil {
		return nil, nil, p.err
	}
	f := func(kind, v string) contracts.ExplanationFactsV2FactsItem {
		return contracts.ExplanationFactsV2FactsItem{FactId: "fct_" + strings.ReplaceAll(kind, ".", "_"), Kind: kind, FactStatus: contracts.ExplanationFactsV2FactsItemFactStatusKNOWN, ValueMasked: v, SourceLabel: "ledger", AsOf: "2026-11-12T09:50:00Z"}
	}
	return []contracts.ExplanationFactsV2FactsItem{f("payment.amount", "NGN 20,000"), f("account", "ending 4321"), f("payment.time", "10:42 on 12 Nov")}, nil, nil
}

type localesFake struct {
	loc   contracts.ExplanationFactsV2Locale
	err   error
	calls int
	gotC  string
}

func (l *localesFake) Locale(_ context.Context, _ explanation.Subject, correlationID string) (contracts.ExplanationFactsV2Locale, error) {
	l.calls++
	l.gotC = correlationID
	return l.loc, l.err
}

type world struct {
	r *readerFake
	p *paymentsFake
	l *localesFake
	s *rowreader.Source
}

func newWorld(t *testing.T, rows rowreader.Rows, edit ...func(*rowreader.Config)) *world {
	t.Helper()
	c := cfg()
	for _, e := range edit {
		e(&c)
	}
	w := &world{r: &readerFake{rows: rows}, p: &paymentsFake{}, l: &localesFake{loc: contracts.ExplanationFactsV2LocaleEnNG}}
	s, err := rowreader.NewSource(c, w.r, w.p, w.l, clock.NewManual(now))
	if err != nil {
		t.Fatalf("NewSource: %v", err)
	}
	w.s = s
	return w
}

func (w *world) facts(t *testing.T) contracts.ExplanationFactsV2 {
	t.Helper()
	f, err := w.s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
	if err != nil {
		t.Fatalf("Facts: %v", err)
	}
	return f
}

func byKind(f contracts.ExplanationFactsV2) map[string]contracts.ExplanationFactsV2FactsItem {
	m := map[string]contracts.ExplanationFactsV2FactsItem{}
	for _, x := range f.Facts {
		m[x.Kind] = x
	}
	return m
}

func unknownOf(f contracts.ExplanationFactsV2, kind string) (contracts.ExplanationFactsV2UnknownsItem, bool) {
	for _, u := range f.Unknowns {
		if u.Kind == kind {
			return u, true
		}
	}
	return contracts.ExplanationFactsV2UnknownsItem{}, false
}

// The whole answer of a verified case: the case reference (year dropped, masked), the payment facts of the seam, the pack's outcome from the verdict, the sealed classification, the
// correlation of the decision and the locale of the conversation; the team and the clock are unknowns because nothing the rows hold names them (A206, A692).
func TestAVerifiedCaseReceiptGivesTheCaseReferenceAndTheEnvelope(t *testing.T) {
	w := newWorld(t, goodRows())
	f := w.facts(t)
	if f.SchemaVersion != "2.0" || f.CorrelationId != corr || f.Locale != contracts.ExplanationFactsV2LocaleEnNG || string(f.OutcomeId) != caseOut || f.Classification != "EXECUTION_OUTCOME_UNKNOWN" {
		t.Fatalf("envelope %+v", f)
	}
	got := byKind(f)
	if got["case.ref"].ValueMasked != "CMP-0001" || got["case.ref"].SourceLabel != "case system" || got["case.ref"].FactStatus != contracts.ExplanationFactsV2FactsItemFactStatusKNOWN ||
		string(got["case.ref"].AsOf) != "2026-11-12T09:50:00Z" {
		t.Errorf("case.ref = %+v", got["case.ref"])
	}
	for _, k := range []string{"payment.amount", "account", "payment.time"} {
		if _, ok := got[k]; !ok {
			t.Errorf("no %s fact", k)
		}
	}
	if len(f.Facts) != 4 {
		t.Errorf("%d facts, want 4: %+v", len(f.Facts), f.Facts)
	}
	for _, k := range []string{"case.team", "case.clock"} {
		if _, ok := got[k]; ok {
			t.Errorf("a %s fact with no source", k)
		}
		if _, ok := unknownOf(f, k); !ok {
			t.Errorf("no unknown for %s", k)
		}
	}
	if w.r.gotTen != tenantA || w.r.gotDec != decision || w.p.gotTen != tenantA || w.p.gotRef != txnRef || w.l.gotC != corr {
		t.Errorf("the seams were asked for %q %q, %q %q, %q", w.r.gotTen, w.r.gotDec, w.p.gotTen, w.p.gotRef, w.l.gotC)
	}
	if f.Facts == nil || f.Unknowns == nil || f.Actions == nil || f.Advice == nil || f.NextSteps == nil {
		t.Errorf("a required array is nil: %+v", f)
	}
}

// INV-04, INV-07: a case reference needs the ACHIEVED verdict. A pending, failed, unknown, compensated or absent verdict gives the three case unknowns and no fact.
func TestACaseReferenceNeedsAnAchievedVerdict(t *testing.T) {
	for _, v := range []string{"PENDING_EXTERNAL", "NOT_ACHIEVED", "UNKNOWN", "PARTIALLY_ACHIEVED", "COMPENSATED", ""} {
		rows := goodRows()
		rows.Executions[0].Verdict = v
		if v == "" {
			rows.Executions[0].OutcomeID = ""
			rows.Executions = append(rows.Executions, rowreader.Execution{GrantID: "grt_0002", CapabilityID: "syn.cap.other", ReceiptID: "rcp_0002", State: "ACKNOWLEDGED", OwnGrant: true, Verdict: "ACHIEVED", OutcomeID: "syn.outcome.other"})
		}
		f := newWorld(t, rows).facts(t)
		if _, ok := byKind(f)["case.ref"]; ok {
			t.Errorf("verdict %q: a case.ref fact", v)
		}
		for _, k := range []string{"case.ref", "case.team", "case.clock"} {
			if _, ok := unknownOf(f, k); !ok {
				t.Errorf("verdict %q: no unknown for %s", v, k)
			}
		}
	}
}

// INV-07: a receipt that is not ACKNOWLEDGED shows no reference, whatever its row holds and whatever the verdict says; an ACKNOWLEDGED receipt with no acknowledgement shows none either.
func TestOnlyAnAcknowledgedReceiptWithItsAcknowledgementGivesAReference(t *testing.T) {
	for _, st := range []string{"DISPATCHED", "OUTCOME_UNKNOWN", "REJECTED_BY_TARGET", "REJECTED_STALE_VERSION", "FAILED_TERMINAL", ""} {
		rows := goodRows()
		rows.Executions[0].State = st
		if _, ok := byKind(newWorld(t, rows).facts(t))["case.ref"]; ok {
			t.Errorf("state %q: a case.ref fact", st)
		}
	}
	for name, a := range map[string]*string{"nil": nil, "empty": ptr(""), "blank": ptr("  ")} {
		rows := goodRows()
		rows.Executions[0].ExternalAck = a
		f := newWorld(t, rows).facts(t)
		if _, ok := byKind(f)["case.ref"]; ok {
			t.Errorf("acknowledgement %s: a case.ref fact", name)
		}
		if u, ok := unknownOf(f, "case.ref"); !ok || u.WhyCode != "FIELD_UNUSABLE" {
			t.Errorf("acknowledgement %s: unknown %+v", name, u)
		}
	}
}

func ptr(s string) *string { return &s }

// A case reference never shows more than four digits (A207, INV-19): the source masks, whatever the target acknowledged.
func TestAReferenceWithLongDigitRunsIsMasked(t *testing.T) {
	rows := goodRows()
	rows.Executions[0].ExternalAck = ptr("CASE-0012345678")
	f := newWorld(t, rows).facts(t)
	if v := byKind(f)["case.ref"].ValueMasked; v == "" || strings.Contains(v, "0012345") {
		t.Errorf("case.ref = %q", v)
	}
}

// The owner queue is a param of the signed receipt's step_params, under the name the pack's binding gives (config: no default); a name the table does not hold, a value that is not a
// string, and no configured param are an unknown, never a guess.
func TestTheOwnerQueueIsReadFromTheReceiptsStepParams(t *testing.T) {
	queue := func(v string) rowreader.Rows {
		rows := goodRows()
		rows.Executions[0].StepParams = map[string]json.RawMessage{"owner_queue": json.RawMessage(v), "other": json.RawMessage(`"SYNTH_QUEUE"`), "": json.RawMessage(`"SYNTH_QUEUE"`)}
		return rows
	}
	f := newWorld(t, queue(`"SYNTH_QUEUE"`)).facts(t)
	if got := byKind(f)["case.team"].ValueMasked; got != "Payments Operations" {
		t.Errorf("case.team = %q", got)
	}
	for name, v := range map[string]string{"not in the table": `"OTHER_QUEUE"`, "a number": `7`, "null": `null`, "an object": `{"a":1}`, "an empty string": `""`} {
		f := newWorld(t, queue(v)).facts(t)
		if _, ok := byKind(f)["case.team"]; ok {
			t.Errorf("%s: a case.team fact", name)
		}
		if u, ok := unknownOf(f, "case.team"); !ok || u.WhyCode != "QUEUE_NOT_NAMED" {
			t.Errorf("%s: unknown %+v", name, u)
		}
	}
	noParam := newWorld(t, queue(`"SYNTH_QUEUE"`), func(c *rowreader.Config) { c.QueueParam = "" }).facts(t)
	if _, ok := byKind(noParam)["case.team"]; ok {
		t.Errorf("no configured param: a case.team fact")
	}
	if _, ok := unknownOf(noParam, "case.team"); !ok {
		t.Errorf("no configured param: no unknown")
	}
}

// A decision with no case receipt is not an error (J1: a status answer, no case): the case kinds are unknowns. Two case receipts are a duplicate case, which this source cannot choose
// between: an error and no fact.
func TestNoCaseReceiptIsUnknownsAndTwoAreAmbiguous(t *testing.T) {
	rows := goodRows()
	rows.Executions[0].CapabilityID = "syn.cap.other"
	f := newWorld(t, rows).facts(t)
	if len(unknownKinds(f, "case.ref", "case.team", "case.clock")) != 3 {
		t.Errorf("unknowns %+v", f.Unknowns)
	}
	two := goodRows()
	second := caseExec()
	second.GrantID, second.ReceiptID = "grt_0002", "rcp_0002"
	two.Executions = append(two.Executions, second)
	f2, err := newWorld(t, two).s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
	if !errors.Is(err, rowreader.ErrAmbiguous) || len(f2.Facts) != 0 {
		t.Errorf("two case receipts: facts %+v err %v", f2, err)
	}
}

func unknownKinds(f contracts.ExplanationFactsV2, kinds ...string) []string {
	var out []string
	for _, k := range kinds {
		if _, ok := unknownOf(f, k); ok {
			out = append(out, k)
		}
	}
	return out
}

// The outcome is the pack's outcome of the verdict of the decision's own grant. A decision with no verdict has none (ErrNoVerdict); two outcomes are ambiguous; the same outcome twice is
// one; and the verdict of another decision's grant of the investigation never names the outcome.
func TestTheOutcomeComesFromTheDecisionsOwnVerdicts(t *testing.T) {
	ask := func(rows rowreader.Rows) (contracts.ExplanationFactsV2, error) {
		return newWorld(t, rows).s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
	}
	none := goodRows()
	none.Executions[0].Verdict, none.Executions[0].OutcomeID = "", ""
	if f, err := ask(none); !errors.Is(err, rowreader.ErrNoVerdict) || len(f.Facts) != 0 || f.OutcomeId != "" {
		t.Errorf("no verdict: %+v %v", f, err)
	}
	other := goodRows()
	other.Executions[0].OwnGrant = false
	if _, err := ask(other); !errors.Is(err, rowreader.ErrNoVerdict) {
		t.Errorf("another decision's verdict: %v", err)
	}
	two := goodRows()
	two.Executions = append(two.Executions, rowreader.Execution{GrantID: "grt_0002", CapabilityID: "syn.cap.other", ReceiptID: "rcp_0002", State: "ACKNOWLEDGED", OwnGrant: true, Verdict: "ACHIEVED", OutcomeID: "syn.outcome.other"})
	if f, err := ask(two); !errors.Is(err, rowreader.ErrAmbiguous) || len(f.Facts) != 0 {
		t.Errorf("two outcomes: %+v %v", f, err)
	}
	same := goodRows()
	same.Executions = append(same.Executions, rowreader.Execution{GrantID: "grt_0002", CapabilityID: "syn.cap.other", ReceiptID: "rcp_0002", State: "ACKNOWLEDGED", OwnGrant: true, Verdict: "ACHIEVED", OutcomeID: caseOut})
	if f, err := ask(same); err != nil || string(f.OutcomeId) != caseOut {
		t.Errorf("one outcome twice: %+v %v", f, err)
	}
}

// With no resolved subject the payment seam is not asked and amount, account and time are unknowns (A711: no counterparty kind); a failing seam is an error and no fact.
func TestThePaymentSeamIsAskedOnlyForAResolvedSubject(t *testing.T) {
	rows := goodRows()
	rows.SubjectRef = ""
	w := newWorld(t, rows)
	f := w.facts(t)
	if w.p.calls != 0 {
		t.Errorf("the payment seam was asked %d times with no subject", w.p.calls)
	}
	if got := unknownKinds(f, "payment.amount", "account", "payment.time"); len(got) != 3 {
		t.Errorf("unknowns %+v", f.Unknowns)
	}
	for _, u := range f.Unknowns {
		if u.Kind == "payment.counterparty" {
			t.Errorf("a counterparty unknown (A711 removed the kind from this source)")
		}
	}
	boom := errors.New("ledger down for " + string(txnRef))
	bad := newWorld(t, goodRows())
	bad.p.err = boom
	got, err := bad.s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
	if !errors.Is(err, rowreader.ErrRead) || !errors.Is(err, boom) || len(got.Facts) != 0 {
		t.Errorf("failing payment seam: %+v %v", got, err)
	}
	if strings.Contains(rowreader.ErrRead.Error(), string(txnRef)) {
		t.Errorf("ErrRead names a reference")
	}
}

// The locale is the conversation's, from a seam the composition fills; an error or a locale outside the five is an error and no fact.
func TestTheLocaleComesFromTheSeamAndFailsClosed(t *testing.T) {
	w := newWorld(t, goodRows())
	w.l.loc = contracts.ExplanationFactsV2LocaleYo
	if got := w.facts(t).Locale; got != contracts.ExplanationFactsV2LocaleYo {
		t.Errorf("locale %q", got)
	}
	for name, edit := range map[string]func(*localesFake){"error": func(l *localesFake) { l.err = errors.New("no session") }, "empty": func(l *localesFake) { l.loc = "" }, "unknown": func(l *localesFake) { l.loc = "fr" }} {
		bad := newWorld(t, goodRows())
		edit(bad.l)
		f, err := bad.s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
		if !errors.Is(err, rowreader.ErrLocale) || len(f.Facts) != 0 {
			t.Errorf("%s: %+v %v", name, f, err)
		}
	}
}

// A reader that cannot answer is an error with no fact and a fixed text that names no tenant, decision or reference (AGENT-13); the typed refusals pass through unchanged.
func TestAReaderRefusalIsAnErrorWithNoFact(t *testing.T) {
	for _, want := range []error{rowreader.ErrNotFound, rowreader.ErrMismatch, rowreader.ErrNoClassification, rowreader.ErrAmbiguous, rowreader.ErrStore, rowreader.ErrNoLogin} {
		w := newWorld(t, rowreader.Rows{})
		w.r.err = want
		f, err := w.s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
		if !errors.Is(err, want) || len(f.Facts) != 0 || w.p.calls != 0 || w.l.calls != 0 {
			t.Errorf("%v: facts %+v err %v, payment calls %d, locale calls %d", want, f, err, w.p.calls, w.l.calls)
		}
		if err != nil && err.Error() != want.Error() {
			t.Errorf("%v: the typed refusal's text changed to %q", want, err.Error())
		}
		for _, v := range []string{tenantA, decision, corr} {
			if err != nil && strings.Contains(err.Error(), v) {
				t.Errorf("%v: the error names %q", want, v)
			}
		}
	}
	// An error of the reader that is not one of the typed refusals (a driver's, which can name a server, a login or a row) is ErrRead with a fixed text; the cause stays reachable.
	boom := errors.New("pq: login " + tenantA + " refused for " + decision + " on " + corr)
	w := newWorld(t, rowreader.Rows{})
	w.r.err = boom
	if _, err := w.s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision); !errors.Is(err, rowreader.ErrRead) || !errors.Is(err, boom) ||
		err.Error() != rowreader.ErrRead.Error() {
		t.Errorf("an untyped reader error: %v", err)
	}
}

// An outcome id that is not a pack identifier is a mismatch, never a value the template key is built from (INV-13).
func TestAnOutcomeThatIsNotAPackIdentifierIsRefused(t *testing.T) {
	for _, bad := range []string{"Bad Outcome", "outcome", "syn.Outcome", "syn.outcome.case opened", "syn..x"} {
		rows := goodRows()
		rows.Executions[0].OutcomeID = bad
		if f, err := newWorld(t, rows).s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision); !errors.Is(err, rowreader.ErrMismatch) || len(f.Facts) != 0 {
			t.Errorf("outcome %q: %+v %v", bad, f, err)
		}
	}
}

// Rows that name another correlation or decision than the request are refused before any fact is made.
func TestRowsOfAnotherDecisionAreRefused(t *testing.T) {
	rows := goodRows()
	rows.DecisionID = "dec_other01"
	f, err := newWorld(t, rows).s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
	if !errors.Is(err, rowreader.ErrMismatch) || len(f.Facts) != 0 {
		t.Errorf("%+v %v", f, err)
	}
	empty := goodRows()
	empty.CorrelationID = ""
	if _, err := newWorld(t, empty).s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision); !errors.Is(err, rowreader.ErrMismatch) {
		t.Errorf("no correlation: %v", err)
	}
	noClass := goodRows()
	noClass.Classification = ""
	if _, err := newWorld(t, noClass).s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision); !errors.Is(err, rowreader.ErrNoClassification) {
		t.Errorf("no classification: %v", err)
	}
}

// INV-12: no tenant or principal, or a decision id that is not a reference, never reaches the reader.
func TestABadRequestNeverReachesTheReader(t *testing.T) {
	w := newWorld(t, goodRows())
	for name, c := range map[string]struct {
		s   explanation.Subject
		dec string
	}{
		"no tenant":    {explanation.Subject{PrincipalID: principal}, decision},
		"no principal": {explanation.Subject{TenantID: tenantA}, decision},
		"empty id":     {explanation.Subject{TenantID: tenantA, PrincipalID: principal}, ""},
		"shape":        {explanation.Subject{TenantID: tenantA, PrincipalID: principal}, "dec 1; DROP"},
	} {
		if _, err := w.s.Facts(context.Background(), c.s, c.dec); !errors.Is(err, rowreader.ErrBadRequest) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if w.r.calls != 0 || w.p.calls != 0 || w.l.calls != 0 {
		t.Errorf("the seams were asked: %d %d %d", w.r.calls, w.p.calls, w.l.calls)
	}
}

// The source refuses to be built without what it needs; nothing is defaulted (AGENT-16).
func TestTheSourceRefusesAnIncompleteConfiguration(t *testing.T) {
	good := func() (rowreader.Config, rowreader.RowsReader, rowreader.Payments, rowreader.Locales, clock.Clock) {
		return cfg(), &readerFake{}, &paymentsFake{}, &localesFake{}, clock.NewManual(now)
	}
	type args = func(*rowreader.Config, *rowreader.RowsReader, *rowreader.Payments, *rowreader.Locales, *clock.Clock)
	for name, edit := range map[string]args{
		"no reader": func(_ *rowreader.Config, r *rowreader.RowsReader, _ *rowreader.Payments, _ *rowreader.Locales, _ *clock.Clock) {
			*r = nil
		},
		"no payments": func(_ *rowreader.Config, _ *rowreader.RowsReader, p *rowreader.Payments, _ *rowreader.Locales, _ *clock.Clock) {
			*p = nil
		},
		"no locales": func(_ *rowreader.Config, _ *rowreader.RowsReader, _ *rowreader.Payments, l *rowreader.Locales, _ *clock.Clock) {
			*l = nil
		},
		"no clock": func(_ *rowreader.Config, _ *rowreader.RowsReader, _ *rowreader.Payments, _ *rowreader.Locales, k *clock.Clock) {
			*k = nil
		},
		"no label": func(c *rowreader.Config, _ *rowreader.RowsReader, _ *rowreader.Payments, _ *rowreader.Locales, _ *clock.Clock) {
			c.Label = ""
		},
		"no capability": func(c *rowreader.Config, _ *rowreader.RowsReader, _ *rowreader.Payments, _ *rowreader.Locales, _ *clock.Clock) {
			c.CaseCapability = ""
		},
		"no zone": func(c *rowreader.Config, _ *rowreader.RowsReader, _ *rowreader.Payments, _ *rowreader.Locales, _ *clock.Clock) {
			c.Facts.Zone = nil
		},
		"no currencies": func(c *rowreader.Config, _ *rowreader.RowsReader, _ *rowreader.Payments, _ *rowreader.Locales, _ *clock.Clock) {
			c.Facts.Exponents = nil
		},
	} {
		c, r, p, l, k := good()
		edit(&c, &r, &p, &l, &k)
		if _, err := rowreader.NewSource(c, r, p, l, k); !errors.Is(err, rowreader.ErrConfig) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// INV-21, INV-14, AGENT-13, A676: the source is a pure function of the rows, the seams and the clock; it holds no vocabulary of the pack, no name of a counterparty, no database and no
// float; the database is read.go's alone.
func TestTheSourceIsPureAndHoldsNoVocabulary(t *testing.T) {
	b, err := os.ReadFile("source.go")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	for _, banned := range []string{"float32", "float64", "time.Now(", "time.Since(", "math/rand", "net/http", "database/sql", "pgx", "\"fin.", "fin.cap", "fin.outcome", "counterparty", "Counterparty"} {
		if strings.Contains(src, banned) {
			t.Errorf("source.go holds %q", banned)
		}
	}
}

// EXP-01 / A727 (the Architect, 26 Nov 2026, interim, relayed; committed in DEC-2026-11-26-architect-rulings-a721-a737): a no-write outcome is explained from its Decision Record, because it
// has no write and so no verdict row (US-01). The choice is deterministic: a verdict row of the decision's own grant wins when it exists; the record alone explains only an ALLOW decision
// that has no grant of its own (and only when the pack's no-write outcome is configured); a write outcome with no verdict stays ErrNoVerdict, never explained from the record alone.
// REQ-EXP-001, REQ-EXP-002, REQ-SEC-004; INV-04, INV-12, INV-13, INV-14, AGENT-13, A676.

const noWriteOut = "syn.outcome.status_explained"

func withNoWrite(c *rowreader.Config) { c.NoWriteOutcome = noWriteOut }

func noWriteRows() rowreader.Rows {
	return rowreader.Rows{DecisionID: decision, CorrelationID: corr, Classification: "SUCCESS_CONFIRMED", SubjectRef: string(txnRef), DecisionOutcome: string(contracts.EnumsV1DecisionOutcomeALLOW)}
}

func askFacts(t *testing.T, w *world) (contracts.ExplanationFactsV2, error) {
	t.Helper()
	return w.s.Facts(context.Background(), explanation.Subject{TenantID: tenantA, PrincipalID: principal}, decision)
}

// US-01: an ALLOW decision with no grant of its own is explained from its record: the configured no-write outcome, the sealed classification (the reason class the templates key on), the
// payment facts, and the case kinds as unknowns; no write is claimed and nothing the record holds beyond that is shown.
func TestANoWriteDecisionIsExplainedFromItsRecord(t *testing.T) {
	w := newWorld(t, noWriteRows(), withNoWrite)
	f, err := askFacts(t, w)
	if err != nil {
		t.Fatalf("Facts: %v", err)
	}
	if string(f.OutcomeId) != noWriteOut || f.Classification != "SUCCESS_CONFIRMED" || f.CorrelationId != corr {
		t.Errorf("envelope %+v", f)
	}
	got := byKind(f)
	for _, k := range []string{"payment.amount", "account", "payment.time"} {
		if _, ok := got[k]; !ok {
			t.Errorf("no %s fact", k)
		}
	}
	if len(f.Facts) != 3 || len(unknownKinds(f, "case.ref", "case.team", "case.clock")) != 3 {
		t.Errorf("facts %+v unknowns %+v", f.Facts, f.Unknowns)
	}
}

// A verdict row of the decision's own grant wins over the record, whatever the pack's no-write outcome is.
func TestAVerdictRowWinsOverTheRecord(t *testing.T) {
	rows := goodRows()
	rows.DecisionOutcome = string(contracts.EnumsV1DecisionOutcomeALLOW)
	f, err := askFacts(t, newWorld(t, rows, withNoWrite))
	if err != nil || string(f.OutcomeId) != caseOut {
		t.Errorf("outcome %q err %v, want %q", f.OutcomeId, err, caseOut)
	}
}

// Never explain a write from the record alone: a decision with a grant of its own and no verdict (no receipt yet, or a receipt not yet verified) is ErrNoVerdict.
func TestAWriteWithNoVerdictIsNeverExplainedFromTheRecord(t *testing.T) {
	grantOnly := noWriteRows()
	grantOnly.Executions = []rowreader.Execution{{GrantID: "grt_0001", CapabilityID: caseCap, OwnGrant: true}}
	receiptOnly := noWriteRows()
	e := caseExec()
	e.Verdict, e.OutcomeID = "", ""
	receiptOnly.Executions = []rowreader.Execution{e}
	for name, rows := range map[string]rowreader.Rows{"grant only": grantOnly, "receipt, no verdict": receiptOnly} {
		f, err := askFacts(t, newWorld(t, rows, withNoWrite))
		if !errors.Is(err, rowreader.ErrNoVerdict) || len(f.Facts) != 0 || f.OutcomeId != "" {
			t.Errorf("%s: %+v %v", name, f, err)
		}
	}
}

// A grant of another decision of the investigation does not make this decision a write.
func TestAnotherDecisionsGrantDoesNotMakeThisDecisionAWrite(t *testing.T) {
	rows := noWriteRows()
	e := caseExec()
	e.OwnGrant = false
	rows.Executions = []rowreader.Execution{e}
	f, err := askFacts(t, newWorld(t, rows, withNoWrite))
	if err != nil || string(f.OutcomeId) != noWriteOut {
		t.Errorf("outcome %q err %v", f.OutcomeId, err)
	}
}

// With no no-write outcome configured the record explains nothing: ErrNoVerdict, as before (nothing is defaulted, AGENT-16).
func TestWithNoConfiguredNoWriteOutcomeTheRecordExplainsNothing(t *testing.T) {
	f, err := askFacts(t, newWorld(t, noWriteRows()))
	if !errors.Is(err, rowreader.ErrNoVerdict) || len(f.Facts) != 0 {
		t.Errorf("%+v %v", f, err)
	}
}

// Only ALLOW is explained from the record. Every other outcome, and a record outcome that is not one of the seven, is refused with no fact.
func TestOnlyAnAllowRecordIsExplainedAndAnUnknownOutcomeIsAMismatch(t *testing.T) {
	for _, o := range []string{"CLARIFY", "CONFIRM", "WAIT", "ESCALATE", "REJECT", "UNKNOWN"} {
		rows := noWriteRows()
		rows.DecisionOutcome = o
		if f, err := askFacts(t, newWorld(t, rows, withNoWrite)); !errors.Is(err, rowreader.ErrNoVerdict) || len(f.Facts) != 0 {
			t.Errorf("%s: %+v %v", o, f, err)
		}
	}
	for _, o := range []string{"", "allow", "BOGUS", "ALLOW "} {
		rows := noWriteRows()
		rows.DecisionOutcome = o
		if f, err := askFacts(t, newWorld(t, rows, withNoWrite)); !errors.Is(err, rowreader.ErrMismatch) || len(f.Facts) != 0 {
			t.Errorf("%q: %+v %v", o, f, err)
		}
	}
}

// The no-write outcome is the pack's identifier or the source is not built.
func TestTheNoWriteOutcomeMustBeAPackIdentifier(t *testing.T) {
	for _, bad := range []string{"Bad Outcome", "outcome", "syn.Outcome", "syn..x", " syn.outcome.x"} {
		c := cfg()
		c.NoWriteOutcome = bad
		if _, err := rowreader.NewSource(c, &readerFake{}, &paymentsFake{}, &localesFake{}, clock.NewManual(now)); !errors.Is(err, rowreader.ErrConfig) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}
