package candidates_api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/masking"
)

// MaxCandidates is the most candidates one answer holds: the maxItems of candidates-response.v1 (REQ-CTX-006). It is the contract's number, not a setting.
const MaxCandidates = 20

const (
	schemaVersion  = "1.0"
	maxDisplayRune = 64 // maxLength of counterparty_display in the contract
	maxSurfaceRune = 64 // maxLength of counterparty_surface in the contract
	idHexLen       = 16 // hex characters of the candidate id taken from the digest
)

var (
	tenantRe     = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	principalRe  = regexp.MustCompile(`^usr_[0-9A-Za-z]{2,32}$`)
	correlRe     = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	kindRe       = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$`)
	refTailRe    = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)
	currencyRe   = regexp.MustCompile(`^[A-Z]{3}$`)
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

// Config is everything the reader needs; nothing has a default (AGENT-16).
type Config struct {
	Source       adapters.Transactions // the principal-scoped candidate search
	Masking      masking.Limits        // the pack's masking limits; must be loaded
	ReadTimeout  time.Duration         // the deadline of the search; from configuration, zero refuses
	ResourceKind string                // the pack's <vertical>.<kind> of the resource this reader serves
}

// Reader is the principal-scoped candidates read (REQ-CTX-006). It reads as the request's tenant and principal, which the caller took from the authenticated session,
// never from the model (INV-12). It decides nothing and calls no model (INV-01): it returns the masked, candidate-shaped rows, newest first.
type Reader struct {
	cfg Config
	ok  bool
}

// New refuses a configuration with a missing part.
func New(cfg Config) (Reader, error) {
	switch {
	case cfg.Source == nil, !cfg.Masking.Loaded(), cfg.ReadTimeout <= 0, !kindRe.MatchString(cfg.ResourceKind):
		return Reader{}, refuse(ErrConfig, nil)
	}
	return Reader{cfg: cfg, ok: true}, nil
}

// Read answers one candidates request, or refuses it. An error is never an empty list, and the answer is never cut: more rows than the contract allows, or a page the
// source did not mark complete, is refused whole.
func (r Reader) Read(ctx context.Context, req contracts.CandidatesRequestV1) (contracts.CandidatesResponseV1, error) {
	if !r.ok {
		return contracts.CandidatesResponseV1{}, refuse(ErrConfig, nil)
	}
	q, scope, err := r.query(req)
	if err != nil {
		return contracts.CandidatesResponseV1{}, err
	}
	cctx, cancel := context.WithTimeout(ctx, r.cfg.ReadTimeout)
	defer cancel()
	page, err := r.cfg.Source.FindCandidates(cctx, scope, q)
	if err != nil {
		return contracts.CandidatesResponseV1{}, refuse(ErrRead, err)
	}
	if len(page.Items) > MaxCandidates {
		return contracts.CandidatesResponseV1{}, refuse(ErrTooMany, nil)
	}
	if !page.Complete || page.NextCursor != "" {
		return contracts.CandidatesResponseV1{}, refuse(ErrIncomplete, nil)
	}
	out, err := r.shape(req, page.Items)
	if err != nil {
		return contracts.CandidatesResponseV1{}, err
	}
	return contracts.CandidatesResponseV1{
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
		if lo < 0 || hi < 0 || lo > hi || !currencyRe.MatchString(string(a.Currency)) {
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

// shape turns the rows into masked candidates, newest first, ties on the candidate id.
func (r Reader) shape(req contracts.CandidatesRequestV1, rows []adapters.Candidate) ([]contracts.CandidatesResponseV1Candidate, error) {
	out := make([]contracts.CandidatesResponseV1Candidate, 0, len(rows))
	seen := make(map[string]bool, len(rows))
	times := make(map[string]time.Time, len(rows))
	for _, c := range rows {
		cand, at, err := r.candidate(req, c)
		if err != nil || seen[c.TransactionRef] {
			return nil, refuse(ErrMalformed, nil)
		}
		seen[c.TransactionRef] = true
		times[cand.CandidateId] = at
		out = append(out, cand)
	}
	sort.Slice(out, func(i, j int) bool {
		ti, tj := times[out[i].CandidateId], times[out[j].CandidateId]
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return out[i].CandidateId < out[j].CandidateId
	})
	return out, nil
}

func (r Reader) candidate(req contracts.CandidatesRequestV1, c adapters.Candidate) (contracts.CandidatesResponseV1Candidate, time.Time, error) {
	var zero contracts.CandidatesResponseV1Candidate
	if err := c.Validate(); err != nil || !refTailRe.MatchString(c.TransactionRef) || !currencyRe.MatchString(c.Currency) {
		return zero, time.Time{}, ErrMalformed
	}
	at, err := time.Parse(time.RFC3339Nano, c.OccurredAt)
	if err != nil || !timestampRe.MatchString(c.OccurredAt) {
		return zero, time.Time{}, ErrMalformed
	}
	last4 := lastFourRe.FindString(c.AccountRef)
	if last4 == "" {
		return zero, time.Time{}, ErrMalformed
	}
	display := r.maskDisplay(c.Counterparty)
	if display == "" {
		return zero, time.Time{}, ErrMalformed
	}
	dir := contracts.CandidatesResponseV1CandidateDirection(c.Direction)
	return contracts.CandidatesResponseV1Candidate{
		CandidateId:         candidateID(string(req.TenantId), string(req.PrincipalRef), c.TransactionRef),
		ResourceRef:         contracts.CommonV1ResourceRef(r.cfg.ResourceKind + ":" + c.TransactionRef),
		AmountMinor:         contracts.MoneyV1MinorAmount(c.AmountMinor),
		Currency:            contracts.MoneyV1CurrencyCode(c.Currency),
		OccurredAt:          contracts.CommonV1Timestamp(c.OccurredAt),
		CounterpartyDisplay: display,
		AccountLast4:        last4,
		Direction:           dir,
	}, at, nil
}

// candidateID is derived from the tenant, the principal and the reference, so it is stable, and it is not the reference.
func candidateID(tenant, principal, ref string) string {
	sum := sha256.Sum256([]byte(tenant + "\x00" + principal + "\x00" + ref))
	return "cand_" + hex.EncodeToString(sum[:])[:idHexLen]
}

// maskDisplay masks every digit run of the masking limit plus one or more, keeping the last KeptDigits digits (AGENT-13); every Unicode decimal digit counts. It then
// trims, and cuts at the contract's length on a rune boundary. An empty result is returned empty.
func (r Reader) maskDisplay(s string) string {
	min, keep := r.cfg.Masking.MinMaskedRun(), r.cfg.Masking.KeptDigits()
	runes := []rune(strings.TrimSpace(s))
	out := make([]rune, 0, len(runes))
	for i := 0; i < len(runes); {
		if !unicode.IsDigit(runes[i]) {
			out = append(out, runes[i])
			i++
			continue
		}
		j := i
		for j < len(runes) && unicode.IsDigit(runes[j]) {
			j++
		}
		run := runes[i:j]
		if len(run) >= min {
			for k := range run {
				if k < len(run)-keep {
					out = append(out, '*')
				} else {
					out = append(out, run[k])
				}
			}
		} else {
			out = append(out, run...)
		}
		i = j
	}
	res := strings.TrimSpace(string(out))
	if utf8.RuneCountInString(res) > maxDisplayRune {
		res = strings.TrimSpace(string([]rune(res)[:maxDisplayRune]))
	}
	return res
}
