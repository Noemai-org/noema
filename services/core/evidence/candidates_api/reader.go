package candidates_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// MaxCandidates is the most candidates one answer holds: the maxItems of candidates-response.v2 (REQ-CTX-006). It is the contract's number, not a setting.
const MaxCandidates = 20

const (
	schemaVersion  = "2.0"         // candidates-response.v2 (A676, AGENT-15): no name of any kind leaves the read
	maxSurfaceRune = 64            // maxLength of counterparty_surface in the contract
	maxMatchPart   = 1_000_000_000 // the contract's bound on a recipient_match numerator and denominator
	// maxMinorAmount is the maximum of money.v1 MinorAmount (2^53-1). The generated Go type (`MoneyV1MinorAmount`, an int64) carries no bound, so the bound lives here and a test reads it from
	// contracts/schemas/money.v1.json and fails if the two differ (A689).
	maxMinorAmount = 9007199254740991
	idHexLen       = 16 // hex characters of the candidate id taken from the digest
)

var (
	tenantRe     = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	principalRe  = regexp.MustCompile(`^usr_[0-9A-Za-z]{2,32}$`)
	correlRe     = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	kindRe       = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$`)
	refTailRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	timestampRe  = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$`)
	lastFourRe   = regexp.MustCompile(`[0-9]{4}$`)
	errorNoValue = "candidates_api: "
)

// The refusals. Their text is fixed and carries no row, no reference, no identifier and no word of the cause (AGENT-13); the cause stays reachable with errors.Is.
var (
	ErrConfig     = errors.New(errorNoValue + "the reader is not configured")
	ErrRequest    = errors.New(errorNoValue + "the request is not one this reader answers")
	ErrRead       = errors.New(errorNoValue + "the candidate search failed")
	ErrTooMany    = errors.New(errorNoValue + "the search found more than the contract allows")
	ErrIncomplete = errors.New(errorNoValue + "the search did not mark its answer complete")
	ErrMalformed  = errors.New(errorNoValue + "a row of the answer cannot be made safe")
)

type refusal struct {
	kind  error
	cause error
}

func (e *refusal) Error() string        { return e.kind.Error() }
func (e *refusal) Is(target error) bool { return target == e.kind }
func (e *refusal) Unwrap() error        { return e.cause }

func refuse(kind, cause error) error { return &refusal{kind: kind, cause: cause} }

// Match is how well a counterparty's name matched what the customer said, as an exact share in [0, 1] (A676). It is exact so that ranking does not change by rounding: the same fractions the
// reference matcher gives, and nothing of the name.
type Match struct{ Numerator, Denominator int64 }

// RecipientMatcher is the recipient match, computed in core (A676): it is given what the customer said (the request's counterparty_surface) and a counterparty's name, and returns only the match.
// The name goes in and never comes out: an error from it is dropped for a fixed refusal. The production matcher is the pack's recipient matcher; it is not this package's.
type RecipientMatcher interface {
	Match(surface, name string) (Match, error)
}

// Config is everything the reader needs; nothing has a default (AGENT-16).
type Config struct {
	Source       adapters.Transactions // the principal-scoped candidate search
	Matcher      RecipientMatcher      // the recipient match, computed in core from the request's counterparty_surface
	ReadTimeout  time.Duration         // the deadline of the search; from configuration, zero refuses
	ResourceKind string                // the pack's <vertical>.<kind> of the resource this reader serves
}

// Reader is the principal-scoped candidates read (REQ-CTX-006). It reads as the request's tenant and principal, which the caller took from the authenticated session,
// never from the model (INV-12). It decides nothing and calls no model (INV-01): it returns the candidate-shaped rows, newest first, with no name of any kind (A676).
type Reader struct {
	cfg Config
	ok  bool
}

// New refuses a configuration with a missing part.
func New(cfg Config) (Reader, error) {
	switch {
	case cfg.Source == nil, cfg.Matcher == nil, cfg.ReadTimeout <= 0, !kindRe.MatchString(cfg.ResourceKind):
		return Reader{}, refuse(ErrConfig, nil)
	}
	return Reader{cfg: cfg, ok: true}, nil
}

// Read answers one candidates request, or refuses it. An error is never an empty list, and the answer is never cut: more rows than the contract allows, or a page the
// source did not mark complete, is refused whole.
func (r Reader) Read(ctx context.Context, req contracts.CandidatesRequestV1) (contracts.CandidatesResponseV2, error) {
	if !r.ok {
		return contracts.CandidatesResponseV2{}, refuse(ErrConfig, nil)
	}
	q, scope, err := r.query(req)
	if err != nil {
		return contracts.CandidatesResponseV2{}, err
	}
	if a := req.Filters.AmountRange; a != nil && a.MinMinor == 0 && a.MaxMinor == 0 {
		// A689: a candidate's amount is above zero, so a range of 0 to 0 can match none: the answer is empty and complete, and the source is not called.
		return contracts.CandidatesResponseV2{SchemaVersion: schemaVersion, TenantId: req.TenantId, CorrelationId: req.CorrelationId, Candidates: []contracts.CandidatesResponseV2Candidate{}}, nil
	}
	cctx, cancel := context.WithTimeout(ctx, r.cfg.ReadTimeout)
	defer cancel()
	page, err := r.cfg.Source.FindCandidates(cctx, scope, q)
	if err != nil {
		return contracts.CandidatesResponseV2{}, refuse(ErrRead, err)
	}
	if len(page.Items) > MaxCandidates {
		return contracts.CandidatesResponseV2{}, refuse(ErrTooMany, nil)
	}
	if !page.Complete || page.NextCursor != "" {
		return contracts.CandidatesResponseV2{}, refuse(ErrIncomplete, nil)
	}
	out, err := r.shape(req, page.Items)
	if err != nil {
		return contracts.CandidatesResponseV2{}, err
	}
	return contracts.CandidatesResponseV2{
		SchemaVersion: schemaVersion, TenantId: req.TenantId, CorrelationId: req.CorrelationId, Candidates: out,
	}, nil
}

// query checks the request and builds the search. No window, no read (REQ-AMB-002).
func (r Reader) query(req contracts.CandidatesRequestV1) (adapters.TransactionQuery, adapters.Scope, error) {
	bad := func() (adapters.TransactionQuery, adapters.Scope, error) {
		return adapters.TransactionQuery{}, adapters.Scope{}, refuse(ErrRequest, nil)
	}
	if !tenantRe.MatchString(string(req.TenantId)) || !principalRe.MatchString(string(req.PrincipalRef)) || !correlRe.MatchString(string(req.CorrelationId)) ||
		req.ResourceKind != r.cfg.ResourceKind {
		return bad()
	}
	from, to := string(req.Filters.TimeWindow.From), string(req.Filters.TimeWindow.To)
	if !timestampRe.MatchString(from) || !timestampRe.MatchString(to) {
		return bad()
	}
	f, errF := time.Parse(time.RFC3339Nano, from)
	t, errT := time.Parse(time.RFC3339Nano, to)
	if errF != nil || errT != nil || f.After(t) {
		return bad()
	}
	q := adapters.TransactionQuery{From: from, To: to, Limit: MaxCandidates + 1}
	if a := req.Filters.AmountRange; a != nil {
		lo, hi := int64(a.MinMinor), int64(a.MaxMinor)
		// A689 round 2: both bounds are held to the contract's maximum (the generated type is an int64 with none) and the currency to the generated money.v1 enum, before the source is asked.
		if lo < 0 || hi < 0 || lo > hi || lo > maxMinorAmount || hi > maxMinorAmount || !currencyOK(string(a.Currency)) {
			return bad()
		}
		q.AmountMinMinor, q.AmountMaxMinor = lo, hi
	}
	if s := req.Filters.CounterpartySurface; s != nil {
		if n := utf8.RuneCountInString(*s); n < 1 || n > maxSurfaceRune {
			return bad()
		}
		q.Counterparty = *s
	}
	if err := q.Validate(); err != nil {
		return bad()
	}
	return q, adapters.Scope{TenantID: string(req.TenantId), PrincipalID: string(req.PrincipalRef)}, nil
}

// shape turns the rows into candidates, newest first, ties on the candidate id, and numbers the counterparties in that order. A name is used for two things inside this function and for nothing
// else: the recipient match, which is computed here, and the equality of two names after normalisation, which decides whether two candidates share a counterparty_ref. Neither puts any of it
// in the answer.
func (r Reader) shape(req contracts.CandidatesRequestV1, rows []adapters.Candidate) ([]contracts.CandidatesResponseV2Candidate, error) {
	out := make([]contracts.CandidatesResponseV2Candidate, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	times := make(map[string]time.Time, len(rows))
	norms := make(map[string]string, len(rows))
	for _, c := range rows {
		cand, at, err := r.candidate(req, c)
		if err != nil || seen[c.TransactionRef] {
			return nil, refuse(ErrMalformed, nil)
		}
		seen[c.TransactionRef] = true
		times[cand.CandidateId] = at
		norms[cand.CandidateId] = normalName(c.Counterparty)
		out = append(out, cand)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := times[out[i].CandidateId], times[out[j].CandidateId]
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].CandidateId < out[j].CandidateId
	})
	ordinal := make(map[string]int, len(out))
	for i := range out {
		n := norms[out[i].CandidateId]
		if _, ok := ordinal[n]; !ok {
			ordinal[n] = len(ordinal) + 1
		}
		out[i].CounterpartyRef = "cp_" + strconv.Itoa(ordinal[n])
	}
	return out, nil
}

// normalName is what makes two spellings one counterparty: lower case, white space collapsed to single spaces, trimmed. It is for equality only and is never returned.
func normalName(s string) string { return strings.Join(strings.Fields(strings.ToLower(s)), " ") }

func (r Reader) candidate(req contracts.CandidatesRequestV1, c adapters.Candidate) (contracts.CandidatesResponseV2Candidate, time.Time, error) {
	var zero contracts.CandidatesResponseV2Candidate
	if err := c.Validate(); err != nil || !refTailRe.MatchString(c.TransactionRef) || !moneyOK(c.AmountMinor, c.Currency) {
		return zero, time.Time{}, ErrMalformed
	}
	at, err := time.Parse(time.RFC3339Nano, c.OccurredAt)
	if err != nil || !timestampRe.MatchString(c.OccurredAt) {
		return zero, time.Time{}, ErrMalformed
	}
	last4 := lastFourRe.FindString(c.AccountRef)
	if last4 == "" || normalName(c.Counterparty) == "" {
		return zero, time.Time{}, ErrMalformed
	}
	match, err := r.match(req, c.Counterparty)
	if err != nil {
		return zero, time.Time{}, ErrMalformed
	}
	dir := contracts.CandidatesResponseV2CandidateDirection(c.Direction)
	return contracts.CandidatesResponseV2Candidate{
		CandidateId:    candidateID(string(req.TenantId), string(req.PrincipalRef), c.TransactionRef),
		ResourceRef:    contracts.CommonV1ResourceRef(r.cfg.ResourceKind + ":" + c.TransactionRef),
		AmountMinor:    contracts.MoneyV1MinorAmount(c.AmountMinor),
		Currency:       contracts.MoneyV1CurrencyCode(c.Currency),
		OccurredAt:     contracts.CommonV1Timestamp(c.OccurredAt),
		RecipientMatch: contracts.CandidatesResponseV2CandidateRecipientMatch{Numerator: match.Numerator, Denominator: match.Denominator},
		AccountLast4:   last4,
		Direction:      dir,
	}, at, nil
}

// match is the recipient match: 0/1 when the request carries no counterparty_surface (nothing to match, nothing asked), else the matcher's, which must be an exact share in [0, 1]. A matcher that
// fails or answers outside that is a refusal of the whole answer with fixed text, and the matcher's own text is dropped (it may hold the name).
func (r Reader) match(req contracts.CandidatesRequestV1, name string) (Match, error) {
	surface := req.Filters.CounterpartySurface
	if surface == nil {
		return Match{Numerator: 0, Denominator: 1}, nil
	}
	m, err := r.cfg.Matcher.Match(*surface, name)
	if err != nil || m.Denominator < 1 || m.Denominator > maxMatchPart || m.Numerator < 0 || m.Numerator > m.Denominator {
		return Match{}, ErrMalformed
	}
	return m, nil
}

// candidateID is derived from the tenant, the principal and the reference, so it is stable, and it is not the reference.
func candidateID(tenant, principal, ref string) string {
	sum := sha256.Sum256([]byte(tenant + "\x00" + principal + "\x00" + ref))
	return "cand_" + hex.EncodeToString(sum[:])[:idHexLen]
}

// currencyOK is true when the code is a value of the generated money.v1 currency enum (the POC set): it is decoded through `contracts.MoneyV1CurrencyCode`, never matched by a hand-written pattern.
func currencyOK(code string) bool {
	raw, err := json.Marshal(code)
	if err != nil {
		return false
	}
	var c contracts.MoneyV1CurrencyCode
	return json.Unmarshal(raw, &c) == nil
}

// moneyOK checks a candidate's amount and currency with the generated money.v1 type (A689): the pair is decoded through `contracts.MoneyV1Money`, so the currency must be a value of the generated
// enum (the POC set, NGN) and the amount an integer, and the amount must be within the contract's bound. Nothing about the row is in a refusal.
func moneyOK(amount int64, currency string) bool {
	cur, err := json.Marshal(currency)
	if err != nil {
		return false
	}
	raw := []byte(`{"amount_minor":` + strconv.FormatInt(amount, 10) + `,"currency":` + string(cur) + `}`)
	var m contracts.MoneyV1Money
	if json.Unmarshal(raw, &m) != nil {
		return false
	}
	return m.AmountMinor >= 0 && int64(m.AmountMinor) <= maxMinorAmount
}
