package candidates_api_test

// REQ-CTX-006, REQ-CTX-007, REQ-AMB-002, REQ-SEC-003, REQ-SEC-004 (US-07); INV-01, INV-11, INV-12, INV-14, INV-21; AGENT-13, AGENT-16, AGENT-25. All data is SYNTHETIC.

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/candidates_api"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/masking"
)

const (
	tTenant    = "ten_synth01"
	tPrincipal = "usr_synthA1"
	tCorr      = "corr_synth01"
	tKind      = "fin.transaction"
)

var (
	candIDRe  = regexp.MustCompile(`^cand_[0-9A-Za-z]{2,32}$`)
	refRe     = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$`)
	displayRe = regexp.MustCompile(`^(?:[^\n0-9]|[0-9]{1,4}(?:[^0-9]|$))+$`)
)

// source is a candidate search that scopes its rows by the scope it is given, as the real one does, and records what it was asked.
type source struct {
	rows  map[string][]adapters.Candidate // keyed by tenant|principal
	page  *adapters.CandidatePage         // when set, answers this whatever the scope
	err   error
	block bool
	got   []adapters.TransactionQuery
	scope []adapters.Scope
}

func (s *source) FindCandidates(ctx context.Context, sc adapters.Scope, q adapters.TransactionQuery) (adapters.CandidatePage, error) {
	s.got, s.scope = append(s.got, q), append(s.scope, sc)
	if s.block {
		<-ctx.Done()
		return adapters.CandidatePage{}, ctx.Err()
	}
	if s.err != nil {
		return adapters.CandidatePage{}, s.err
	}
	if s.page != nil {
		return *s.page, nil
	}
	return adapters.CandidatePage{Items: s.rows[sc.TenantID+"|"+sc.PrincipalID], Complete: true}, nil
}

func maskingLimits(t *testing.T) masking.Limits {
	t.Helper()
	l, err := masking.Load(strings.NewReader(`{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC","max_unmasked_digit_run":4}`))
	if err != nil {
		t.Fatalf("masking limits: %v", err)
	}
	return l
}

func reader(t *testing.T, s adapters.Transactions) candidates_api.Reader {
	t.Helper()
	r, err := candidates_api.New(candidates_api.Config{Source: s, Masking: maskingLimits(t), ReadTimeout: 2 * time.Second, ResourceKind: tKind})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return r
}

func request() contracts.CandidatesRequestV1 {
	return contracts.CandidatesRequestV1{
		SchemaVersion: "1.0", TenantId: tTenant, CorrelationId: tCorr, PrincipalRef: tPrincipal, ResourceKind: tKind,
		Filters: contracts.CandidatesRequestV1Filters{TimeWindow: contracts.CandidatesRequestV1FiltersTimeWindow{From: "2026-11-01T00:00:00Z", To: "2026-11-02T00:00:00Z"}},
	}
}

func row(ref, at string, amount int64) adapters.Candidate {
	return adapters.Candidate{TransactionRef: ref, AccountRef: "acct_synth_00001234", Direction: adapters.DirectionOutgoing, AmountMinor: amount, Currency: "NGN", Counterparty: "Tunde Synthetic", OccurredAt: at}
}

func world() *source {
	return &source{rows: map[string][]adapters.Candidate{
		tTenant + "|" + tPrincipal:  {row("txn_synth0001", "2026-11-01T08:00:00Z", 500000), row("txn_synth0002", "2026-11-01T09:30:00Z", 500000), row("txn_synth0003", "2026-11-01T09:30:00Z", 250000)},
		tTenant + "|usr_synthB2":    {row("txn_other0001", "2026-11-01T10:00:00Z", 500000)},
		"ten_synth02|" + tPrincipal: {row("txn_other0002", "2026-11-01T11:00:00Z", 500000)},
	}}
}

// US-07: the read is the session's tenant and principal; another principal's or another tenant's transaction never appears; the search asks for one more than the contract allows.
func TestTheReadIsScopedToTheRequestsTenantAndPrincipalAndAsksForOneMore(t *testing.T) {
	s := world()
	resp, err := reader(t, s).Read(context.Background(), request())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if len(s.scope) != 1 || s.scope[0] != (adapters.Scope{TenantID: tTenant, PrincipalID: tPrincipal}) {
		t.Fatalf("scope = %+v, want the request's tenant and principal only", s.scope)
	}
	if q := s.got[0]; q.Limit != candidates_api.MaxCandidates+1 || q.From != "2026-11-01T00:00:00Z" || q.To != "2026-11-02T00:00:00Z" || q.AccountRef != "" {
		t.Fatalf("query = %+v", q)
	}
	if len(resp.Candidates) != 3 || string(resp.TenantId) != tTenant || string(resp.CorrelationId) != tCorr || string(resp.SchemaVersion) != "1.0" {
		t.Fatalf("response = %+v", resp)
	}
	for _, c := range resp.Candidates {
		if strings.Contains(string(c.ResourceRef), "other") {
			t.Fatalf("another principal's or tenant's row appeared: %s", c.ResourceRef)
		}
	}
}

// Newest first, a stable order on a tie, an id that is not the reference, the resource reference of the pack's kind, and nothing the contract does not allow.
func TestTheAnswerIsNewestFirstDeterministicAndOfTheContractsShape(t *testing.T) {
	s := world()
	r := reader(t, s)
	a, err := r.Read(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if a.Candidates[2].OccurredAt != "2026-11-01T08:00:00Z" || a.Candidates[0].OccurredAt != "2026-11-01T09:30:00Z" || a.Candidates[1].OccurredAt != "2026-11-01T09:30:00Z" {
		t.Fatalf("order = %v, %v, %v", a.Candidates[0].OccurredAt, a.Candidates[1].OccurredAt, a.Candidates[2].OccurredAt)
	}
	if !(a.Candidates[0].CandidateId < a.Candidates[1].CandidateId) {
		t.Fatalf("a tie must break on the candidate id: %q then %q", a.Candidates[0].CandidateId, a.Candidates[1].CandidateId)
	}
	rev := world()
	rows := rev.rows[tTenant+"|"+tPrincipal]
	rows[0], rows[2] = rows[2], rows[0]
	b, err := reader(t, rev).Read(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	if ja, jb := mustJSON(t, a), mustJSON(t, b); ja != jb {
		t.Fatalf("the answer depends on the order the source gave (INV-21):\n%s\n%s", ja, jb)
	}
	for _, c := range a.Candidates {
		if !candIDRe.MatchString(c.CandidateId) || !refRe.MatchString(string(c.ResourceRef)) || !strings.HasPrefix(string(c.ResourceRef), tKind+":txn_synth") {
			t.Fatalf("candidate %+v is not of the contract's shape", c)
		}
		if strings.Contains(c.CandidateId, "synth0") {
			t.Fatalf("the candidate id carries the reference: %q", c.CandidateId)
		}
		if c.BeneficiaryAliasMatch != nil {
			t.Fatalf("beneficiary_alias_match has no source and must stay unset")
		}
	}
}

// REQ-SEC-003, AGENT-13: no account number and no digit run longer than four leaves the read.
func TestNoAccountNumberAndNoLongDigitRunLeavesTheRead(t *testing.T) {
	s := world()
	rows := s.rows[tTenant+"|"+tPrincipal]
	rows[0].Counterparty = "Ref 0123456789 Tunde 12 345 6789012"
	resp, err := reader(t, s).Read(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	out := mustJSON(t, resp)
	for _, bad := range []string{"acct_synth", "00001234", "0123456789", "6789012"} {
		if strings.Contains(out, bad) {
			t.Fatalf("%q leaked in %s", bad, out)
		}
	}
	for _, c := range resp.Candidates {
		if c.AccountLast4 != "1234" {
			t.Fatalf("account_last4 = %q, want the last four digits of the account", c.AccountLast4)
		}
		if !displayRe.MatchString(c.CounterpartyDisplay) || len(c.CounterpartyDisplay) > 64 {
			t.Fatalf("display %q has a digit run over four or is too long", c.CounterpartyDisplay)
		}
	}
	var masked string
	for _, c := range resp.Candidates {
		if c.OccurredAt == "2026-11-01T08:00:00Z" {
			masked = c.CounterpartyDisplay
		}
	}
	if !strings.Contains(masked, "6789") || !strings.Contains(masked, "Tunde") {
		t.Fatalf("masking must keep the last four digits and the words: %q", masked)
	}
}

// REQ-AMB-002: no window, no read. Nothing reaches the source for a request the reader would have to guess about.
func TestARequestWithoutAUsableWindowOrOfAnotherKindIsRefusedBeforeTheSource(t *testing.T) {
	cases := map[string]func(*contracts.CandidatesRequestV1){
		"no window": func(r *contracts.CandidatesRequestV1) {
			r.Filters.TimeWindow = contracts.CandidatesRequestV1FiltersTimeWindow{}
		},
		"from after to": func(r *contracts.CandidatesRequestV1) {
			r.Filters.TimeWindow.From, r.Filters.TimeWindow.To = r.Filters.TimeWindow.To, r.Filters.TimeWindow.From
		},
		"bad timestamp":  func(r *contracts.CandidatesRequestV1) { r.Filters.TimeWindow.From = "yesterday" },
		"other kind":     func(r *contracts.CandidatesRequestV1) { r.ResourceKind = "fin.beneficiary" },
		"no tenant":      func(r *contracts.CandidatesRequestV1) { r.TenantId = "" },
		"no principal":   func(r *contracts.CandidatesRequestV1) { r.PrincipalRef = "" },
		"no correlation": func(r *contracts.CandidatesRequestV1) { r.CorrelationId = "" },
		"long surface": func(r *contracts.CandidatesRequestV1) {
			s := strings.Repeat("x", 65)
			r.Filters.CounterpartySurface = &s
		},
		"min over max": func(r *contracts.CandidatesRequestV1) {
			r.Filters.AmountRange = &contracts.CandidatesRequestV1AmountRange{MinMinor: 10, MaxMinor: 5, Currency: "NGN"}
		},
	}
	for name, mutate := range cases {
		s := world()
		req := request()
		mutate(&req)
		resp, err := reader(t, s).Read(context.Background(), req)
		if !errors.Is(err, candidates_api.ErrRequest) || len(resp.Candidates) != 0 || len(s.got) != 0 {
			t.Errorf("%s: err = %v, candidates = %d, source calls = %d; want ErrRequest and no read", name, err, len(resp.Candidates), len(s.got))
		}
	}
}

// An error is never an empty list; an answer that is more than the contract allows, or not complete, is refused whole.
func TestAnErrorAnOverfullOrAnIncompletePageIsNeverAnEmptyOrCutList(t *testing.T) {
	over := adapters.CandidatePage{Complete: true}
	for i := 0; i <= candidates_api.MaxCandidates; i++ {
		over.Items = append(over.Items, row("txn_over"+strings.Repeat("a", 1)+string(rune('a'+i)), "2026-11-01T08:00:00Z", int64(1000+i)))
	}
	boom := errors.New("boom with usr_synthB2")
	for name, c := range map[string]struct {
		s    *source
		want error
	}{
		"source error":  {&source{err: boom}, candidates_api.ErrRead},
		"more than 20":  {&source{page: &over}, candidates_api.ErrTooMany},
		"not complete":  {&source{page: &adapters.CandidatePage{Items: []adapters.Candidate{row("txn_synth0001", "2026-11-01T08:00:00Z", 5)}}}, candidates_api.ErrIncomplete},
		"empty and not": {&source{page: &adapters.CandidatePage{}}, candidates_api.ErrIncomplete},
	} {
		resp, err := reader(t, c.s).Read(context.Background(), request())
		if !errors.Is(err, c.want) || len(resp.Candidates) != 0 || resp.TenantId != "" {
			t.Errorf("%s: err = %v, resp = %+v; want %v and a zero answer", name, err, resp, c.want)
		}
		if err != nil && (strings.Contains(err.Error(), "boom") || strings.Contains(err.Error(), "usr_") || strings.Contains(err.Error(), "txn_")) {
			t.Errorf("%s: the error text carries a value: %q", name, err.Error())
		}
	}
	if _, err := reader(t, &source{err: boom}).Read(context.Background(), request()); !errors.Is(err, boom) {
		t.Errorf("the cause must stay reachable with errors.Is")
	}
}

// An empty, complete page is a valid answer with no candidates (the intake turns it into ESCALATE).
func TestACompleteEmptyPageIsAnEmptyAnswer(t *testing.T) {
	resp, err := reader(t, &source{page: &adapters.CandidatePage{Complete: true, Items: []adapters.Candidate{}}}).Read(context.Background(), request())
	if err != nil || resp.Candidates == nil || len(resp.Candidates) != 0 || resp.TenantId != tTenant {
		t.Fatalf("resp = %+v, err = %v", resp, err)
	}
}

// A row the reader cannot make safe (a duplicate, no four digits to show, a direction or amount the contract lacks) refuses the whole answer.
func TestARowTheReaderCannotMakeSafeRefusesTheWholeAnswer(t *testing.T) {
	bad := map[string]func(rows []adapters.Candidate){
		"duplicate":        func(r []adapters.Candidate) { r[1].TransactionRef = r[0].TransactionRef },
		"no four digits":   func(r []adapters.Candidate) { r[0].AccountRef = "acct_synth_xx" },
		"direction":        func(r []adapters.Candidate) { r[0].Direction = "SIDEWAYS" },
		"no amount":        func(r []adapters.Candidate) { r[0].AmountMinor = 0 },
		"empty name":       func(r []adapters.Candidate) { r[0].Counterparty = "  " },
		"ref off pattern":  func(r []adapters.Candidate) { r[0].TransactionRef = "txn synth/0001" },
		"currency":         func(r []adapters.Candidate) { r[0].Currency = "ngn" },
		"time not RFC3339": func(r []adapters.Candidate) { r[0].OccurredAt = "2026-11-01" },
	}
	for name, mutate := range bad {
		s := world()
		mutate(s.rows[tTenant+"|"+tPrincipal])
		resp, err := reader(t, s).Read(context.Background(), request())
		if !errors.Is(err, candidates_api.ErrMalformed) || len(resp.Candidates) != 0 {
			t.Errorf("%s: err = %v, candidates = %d; want ErrMalformed and nothing", name, err, len(resp.Candidates))
		}
	}
}

// The deadline is the reader's own: a source that never answers ends in ErrRead when the configured time is up (a real context; the test needs no stopwatch, because a reader with no deadline would not return at all).
func TestASourceThatNeverAnswersEndsAtTheReadTimeout(t *testing.T) {
	r, err := candidates_api.New(candidates_api.Config{Source: &source{block: true}, Masking: maskingLimits(t), ReadTimeout: 50 * time.Millisecond, ResourceKind: tKind})
	if err != nil {
		t.Fatal(err)
	}
	_, rerr := r.Read(context.Background(), request())
	if !errors.Is(rerr, candidates_api.ErrRead) || !errors.Is(rerr, context.DeadlineExceeded) {
		t.Fatalf("err = %v", rerr)
	}
}

// The boundary of the masking rule: a run of four digits stays, a run of five is masked down to its last four, and a bad principal shape is refused before the source.
func TestADigitRunOfFourStaysAndOfFiveIsMasked(t *testing.T) {
	s := world()
	rows := s.rows[tTenant+"|"+tPrincipal]
	rows[0].Counterparty = "Ref 1234 Tunde"
	rows[1].Counterparty = "Ref 12345 Tunde"
	resp, err := reader(t, s).Read(context.Background(), request())
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, c := range resp.Candidates {
		got[c.CounterpartyDisplay] = true
	}
	if !got["Ref 1234 Tunde"] || !got["Ref *2345 Tunde"] {
		t.Fatalf("displays = %v; want a four-digit run kept and a five-digit run masked to its last four", got)
	}
}

func TestAPrincipalOffItsShapeIsRefusedBeforeTheSource(t *testing.T) {
	s := world()
	req := request()
	req.PrincipalRef = "someone"
	if _, err := reader(t, s).Read(context.Background(), req); !errors.Is(err, candidates_api.ErrRequest) || len(s.got) != 0 {
		t.Fatalf("err = %v, source calls = %d", err, len(s.got))
	}
}

// AGENT-16: no part has a default; every missing or unloaded part is a refusal at construction.
func TestAMissingPartIsARefusalAtConstruction(t *testing.T) {
	ok := candidates_api.Config{Source: world(), Masking: maskingLimits(t), ReadTimeout: time.Second, ResourceKind: tKind}
	for name, mutate := range map[string]func(*candidates_api.Config){
		"no source":          func(c *candidates_api.Config) { c.Source = nil },
		"masking not loaded": func(c *candidates_api.Config) { c.Masking = masking.Limits{} },
		"no timeout":         func(c *candidates_api.Config) { c.ReadTimeout = 0 },
		"no kind":            func(c *candidates_api.Config) { c.ResourceKind = "" },
		"kind off shape":     func(c *candidates_api.Config) { c.ResourceKind = "Transaction" },
	} {
		cfg := ok
		mutate(&cfg)
		if _, err := candidates_api.New(cfg); !errors.Is(err, candidates_api.ErrConfig) {
			t.Errorf("%s: err = %v, want ErrConfig", name, err)
		}
	}
	var zero candidates_api.Reader
	if _, err := zero.Read(context.Background(), request()); !errors.Is(err, candidates_api.ErrConfig) {
		t.Errorf("a zero Reader must refuse: %v", err)
	}
}

func mustJSON(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
