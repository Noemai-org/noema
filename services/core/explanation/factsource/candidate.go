package factsource

// The payment facts of the explainer come from the candidate item (EXP-05; ruling A556 under A665, interim, relayed; A711 drops the recipient kind): the candidate
// (candidates-response.v2) holds the three values the payment kinds show: the amount, the account's last four digits and the time. No name of any kind leaves the candidates read
// (A676), so this file reads, copies, masks and emits none and makes no recipient kind. It is the source of that one item and the mapping to the masking core (PaymentFacts); it adds
// no masking rule of its own. It reads through a consumer-side port, never the Record or a store, and holds no clock, no float and no word of the pack's vocabulary (INV-14, INV-21).

import (
	"context"
	"errors"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// The typed errors of the payment source. Their texts name no tenant, reference or value (AGENT-13).
var (
	ErrNoReader       = errors.New("factsource: no candidate reader")
	ErrBadRequest     = errors.New("factsource: the tenant and the resolved resource are both required")
	ErrRead           = errors.New("factsource: the candidate item could not be read")
	ErrTenantMismatch = errors.New("factsource: the candidate item is not the caller's tenant's")
	ErrWrongResource  = errors.New("factsource: the candidate item is not the resolved resource's")
)

// CandidateItem is the one candidate record of the resolved transaction, as the candidates read returned it (candidates-response.v2: no name), with the evidence item's status, the time it was
// read and its source label.
type CandidateItem struct {
	TenantID    string
	Candidate   contracts.CandidatesResponseV2Candidate
	Status      contracts.EnumsV1FactStatus
	ReadAt      time.Time
	SourceLabel string
}

// CandidateReader yields the candidate item of the resolved transaction for the caller's tenant; found is false when there is none. It is the consumer-side port: the adapter that
// implements it (over the candidates read and the sealed snapshot) is wired by the composition.
type CandidateReader interface {
	Resolved(ctx context.Context, tenantID string, ref contracts.CommonV1ResourceRef) (item CandidateItem, found bool, err error)
}

// PaymentSource builds the payment kinds from the candidate item.
type PaymentSource struct {
	cfg Config
	r   CandidateReader
}

// NewPaymentSource refuses a nil reader; the Config is the pack's and the deployment's data (it has no default).
func NewPaymentSource(cfg Config, r CandidateReader) (*PaymentSource, error) {
	if r == nil {
		return nil, ErrNoReader
	}
	return &PaymentSource{cfg: cfg, r: r}, nil
}

// readError carries the cause for errors.Is and errors.As while its text stays fixed: the cause may name a value.
type readError struct{ cause error }

func (e *readError) Error() string   { return ErrRead.Error() }
func (e *readError) Unwrap() []error { return []error{ErrRead, e.cause} }

// PaymentFacts makes payment.amount, account and payment.time of the resolved transaction. No item is three unknowns and no fact (only KNOWN fills a slot, A155; an AMBIGUOUS item is
// not KNOWN). An item of another tenant or another resource (an empty tenant or resource included), a reader error and a bad request are typed errors with no fact.
func (s *PaymentSource) PaymentFacts(ctx context.Context, tenantID string, ref contracts.CommonV1ResourceRef) ([]contracts.ExplanationFactsV2FactsItem, []contracts.ExplanationFactsV2UnknownsItem, error) {
	if tenantID == "" || ref == "" {
		return nil, nil, ErrBadRequest
	}
	item, found, err := s.r.Resolved(ctx, tenantID, ref)
	if err != nil {
		return nil, nil, &readError{cause: err}
	}
	if !found {
		b := newBuilder(time.Time{}, "", contracts.EnumsV1FactStatusUNAVAILABLE)
		for _, k := range paymentKinds {
			b.unknown(k, "SOURCE_NOT_KNOWN")
		}
		return b.facts, b.unkn, nil
	}
	if item.TenantID != tenantID {
		return nil, nil, ErrTenantMismatch
	}
	if item.Candidate.ResourceRef != ref {
		return nil, nil, ErrWrongResource
	}
	facts, unknowns := PaymentFacts(s.cfg, paymentOf(item), item.ReadAt, item.SourceLabel)
	return facts, unknowns, nil
}

// CaseFacts is the three case kinds as unknowns (A556): nothing here reads a case receipt, a plan or an SLA event, so nothing is invented.
func (s *PaymentSource) CaseFacts(asOf time.Time, label string) ([]contracts.ExplanationFactsV2FactsItem, []contracts.ExplanationFactsV2UnknownsItem) {
	return Case(s.cfg, CaseRow{Status: contracts.EnumsV1FactStatusUNAVAILABLE}, asOf, label)
}

var paymentKinds = []string{"payment.amount", "account", "payment.time"}

// paymentOf maps the candidate to the masking core's row, carrying only what the three kinds show. The account is the candidate's last four digits and only when they are exactly
// four ASCII digits; a time that is not RFC 3339 with an offset is the zero time (an unknown); the amount and the currency go through as given, and the core refuses what it cannot
// show. The direction and the opaque reference are not carried.
func paymentOf(it CandidateItem) Payment {
	c := it.Candidate
	p := Payment{AmountMinor: int64(c.AmountMinor), Currency: string(c.Currency), Status: it.Status}
	if last4(string(c.AccountLast4)) {
		p.AccountRef = string(c.AccountLast4)
	}
	if t, err := time.Parse(time.RFC3339, string(c.OccurredAt)); err == nil {
		p.OccurredAt = t.UTC()
	}
	return p
}

func last4(s string) bool {
	if len(s) != 4 {
		return false
	}
	for i := 0; i < 4; i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
