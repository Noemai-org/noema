package synthetic_tushe

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// StandinClient is the core's client of the Noema-side stand-in for the Tushe-held US-01 and US-06 data (synthetic/standin;
// ENV-05; REQ-ENV-001, REQ-ENV-002; DEC-2026-11-06 A10). It implements the read ports of adapters/fin.go. Evidence from it is
// labelled stand-in and says nothing about Tushe; the field names are the stand-in's reading of PERD 34.4, not Tushe's, and the
// mapping to live Tushe replaces this client, not the ports.
//
// Read-only: it has no code for a write, the control listener or the loader's credential. It holds the runtime token and the
// principal key, and mints the identity assertion from the authenticated session's principal on each call (INV-12): the caller
// passes a Scope and never a header, and the Evidence Layer never sees the key.
type StandinClient struct {
	base string
	// token and key are never logged, returned in an error or sent anywhere but to base.
	token string
	key   []byte
	now   func() time.Time
	http  *http.Client
}

var (
	_ adapters.Transactions = (*StandinClient)(nil)
	_ adapters.Ledger       = (*StandinClient)(nil)
	_ adapters.Switch       = (*StandinClient)(nil)
	_ adapters.Destination  = (*StandinClient)(nil)
)

const (
	standinLabel          = "stand-in"
	assertionMACLabel     = "noema-standin-principal-v2"
	assertionAudience     = "standin"
	assertionLifetime     = time.Minute // the stand-in refuses more than five
	standinBasePath       = "/standin/v1"
	candidateFilterAmount = "amount_min"
)

// StandinOptions configures NewStandinClient. Now is the injected clock (no wall-clock reads in the client).
type StandinOptions struct {
	BaseURL          string
	TokenFile        string
	PrincipalKeyFile string
	Now              func() time.Time
	HTTP             *http.Client
}

// NewStandinClient reads the two credentials from files (local secrets are file-mounted, OD-P-22). They must be present, non-empty
// and different from each other.
func NewStandinClient(o StandinOptions) (*StandinClient, error) {
	u, err := url.Parse(o.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.RawQuery != "" || u.Fragment != "" || strings.Trim(u.Path, "/") != "" {
		return nil, errors.New("stand-in client: base URL must be http(s)://host[:port] with no path or query")
	}
	if o.Now == nil {
		return nil, errors.New("stand-in client: a clock is required")
	}
	token, err := readCredential(o.TokenFile)
	if err != nil {
		return nil, err
	}
	key, err := readCredential(o.PrincipalKeyFile)
	if err != nil {
		return nil, err
	}
	if token == key {
		return nil, errors.New("stand-in client: the runtime token and the principal key must differ")
	}
	hc := o.HTTP
	if hc == nil {
		hc = &http.Client{}
	}
	c := *hc
	c.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &StandinClient{base: strings.TrimRight(o.BaseURL, "/"), token: token, key: []byte(key), now: o.Now, http: &c}, nil
}

func readCredential(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("stand-in client: cannot read the credential file %q", path)
	}
	v := strings.TrimSpace(string(raw))
	if v == "" {
		return "", fmt.Errorf("stand-in client: the credential file %q is empty", path)
	}
	return v, nil
}

// standinAssertion is the contract of synthetic/standin/README.md: v2.<base64url(principal)>.<base64url(tenant)>.<expiry>.<hex MAC>, the MAC being
// HMAC-SHA-256 over the five lines label, audience, principal, tenant and expiry. Principal and tenant both come from the session scope (INV-12, A135).
func standinAssertion(key []byte, principal, tenant string, expiry int64) string {
	m := hmac.New(sha256.New, key)
	_, _ = fmt.Fprintf(m, "%s\n%s\n%s\n%s\n%d", assertionMACLabel, assertionAudience, principal, tenant, expiry)
	return fmt.Sprintf("v2.%s.%s.%d.%s", base64.RawURLEncoding.EncodeToString([]byte(principal)), base64.RawURLEncoding.EncodeToString([]byte(tenant)), expiry, hex.EncodeToString(m.Sum(nil)))
}

// answer is one HTTP 200 body with the hash of the exact bytes received.
type answer struct {
	raw    []byte
	hash   string
	tenant string // the tenant of the session that asked: an answer must name the same one (A135)
}

// get sends one GET and maps every outcome that is not a readable 200 to an *adapters.ReadError. The caller's context must carry a
// deadline; without one nothing is sent.
func (c *StandinClient) get(ctx context.Context, scope adapters.Scope, path string, query url.Values) (answer, error) {
	if _, ok := ctx.Deadline(); !ok {
		return answer{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:NO_DEADLINE"}
	}
	// The tenant is validated here but not sent: the stand-in assertion carries the principal only (stand-in README), so isolation rests on principal ids being unique across tenants. Binding the tenant into the assertion needs a stand-in contract ruling (Voice review of noema#205, N1).
	if err := scope.Validate(); err != nil {
		return answer{}, err
	}
	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return answer{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("X-Principal-Assertion", standinAssertion(c.key, scope.PrincipalID, scope.TenantID, c.now().Add(assertionLifetime).Unix()))
	req.Header.Set("Accept", "application/json")
	res, err := c.http.Do(req)
	if err != nil {
		return answer{}, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: transportFailure(ctx, err).RawCode}
	}
	defer func() { _ = res.Body.Close() }() // the body is read in full below; a failed close afterwards is not actionable
	raw, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return answer{}, &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: transportFailure(ctx, err).RawCode}
	}
	sum := sha256.Sum256(raw)
	hash := "sha256:" + hex.EncodeToString(sum[:])
	if e := readStatusError(res.StatusCode, res.Header, raw); e != nil {
		// readStatusError has already put the hash of these bytes on e (an error body is evidence too).
		return answer{}, e
	}
	if !isJSONResponse(res.Header) {
		return answer{}, &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: strconv.Itoa(res.StatusCode), RawHash: hash}
	}
	return answer{raw: raw, hash: hash, tenant: scope.TenantID}, nil
}

// malformed200 is a 2xx the client cannot trust; it carries the hash of the bytes that were received.
func malformed200(hash string) error {
	return &adapters.ReadError{Failure: adapters.ReadMalformed, RawCode: "200", RawHash: hash}
}

// label is the pair every stand-in answer carries (AGENT-25).
type label struct {
	DataSource string `json:"data_source"`
	Synthetic  bool   `json:"synthetic"`
	TenantID   string `json:"tenant_id"`
}

func (a answer) provenance(l label) (adapters.Provenance, error) {
	if l.DataSource != standinLabel || !l.Synthetic || a.tenant == "" || l.TenantID != a.tenant { // an answer for another tenant, or for none, is a misrouted read
		return adapters.Provenance{}, malformed200(a.hash)
	}
	p := adapters.Provenance{RawHash: a.hash, DataSource: l.DataSource, Synthetic: l.Synthetic}
	if p.Validate() != nil {
		return adapters.Provenance{}, malformed200(a.hash)
	}
	return p, nil
}

type wireLedger struct {
	label
	Unavailable *bool `json:"unavailable"`
	Entries     []struct {
		EntryID        string `json:"entry_id"`
		AccountRef     string `json:"account_ref"`
		TransactionRef string `json:"transaction_ref"`
		EntryType      string `json:"entry_type"`
		AmountMinor    int64  `json:"amount_minor"`
		Currency       string `json:"currency"`
		PostingStatus  string `json:"posting_status"`
		PostedAt       string `json:"posted_at"`
		Version        int64  `json:"version"`
	} `json:"entries"`
}

// LedgerEntries implements adapters.Ledger.
func (c *StandinClient) LedgerEntries(ctx context.Context, scope adapters.Scope, transactionRef string) (adapters.LedgerRead, error) {
	a, err := c.perRef(ctx, scope, transactionRef, "ledger-entries")
	if err != nil {
		return adapters.LedgerRead{}, err
	}
	var w wireLedger
	if strict(a.raw, &w) != nil || w.Unavailable == nil {
		return adapters.LedgerRead{}, malformed200(a.hash)
	}
	prov, err := a.provenance(w.label)
	if err != nil {
		return adapters.LedgerRead{}, err
	}
	// A list that is absent is not an empty list: only a present `[]` means "no entries" (a JSON null or a missing key is nil).
	if w.Entries == nil {
		return adapters.LedgerRead{}, malformed200(a.hash)
	}
	if *w.Unavailable {
		if len(w.Entries) != 0 {
			return adapters.LedgerRead{}, malformed200(a.hash)
		}
		return adapters.LedgerRead{Unavailable: true, Provenance: prov}, nil
	}
	out := adapters.LedgerRead{Provenance: prov}
	for _, e := range w.Entries {
		entry := adapters.LedgerEntry{EntryID: e.EntryID, AccountRef: e.AccountRef, TransactionRef: e.TransactionRef, EntryType: adapters.EntryType(e.EntryType),
			AmountMinor: e.AmountMinor, Currency: e.Currency, PostingStatus: adapters.PostingStatus(e.PostingStatus), PostedAt: e.PostedAt, Version: e.Version}
		if entry.Validate() != nil || entry.TransactionRef != transactionRef { // like the switch and destination reads: another transaction's entry is a misrouted answer, never evidence about this one
			return adapters.LedgerRead{}, malformed200(a.hash)
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}

type wireSwitch struct {
	label
	Unavailable    *bool  `json:"unavailable"`
	SwitchRef      string `json:"switch_ref"`
	TransactionRef string `json:"transaction_ref"`
	RawCode        string `json:"raw_code"`
	RawCodeLabel   string `json:"raw_code_label"`
	SemanticStatus string `json:"semantic_status"`
	IsTerminal     bool   `json:"is_terminal"`
	DestinationAck bool   `json:"destination_ack"`
	ReceivedAt     string `json:"received_at"`
	RawPayloadHash string `json:"raw_payload_hash"`
}

// SwitchStatus implements adapters.Switch.
func (c *StandinClient) SwitchStatus(ctx context.Context, scope adapters.Scope, transactionRef string) (adapters.SwitchRead, error) {
	a, err := c.perRef(ctx, scope, transactionRef, "switch-status")
	if err != nil {
		return adapters.SwitchRead{}, err
	}
	var w wireSwitch
	if strict(a.raw, &w) != nil || w.Unavailable == nil {
		return adapters.SwitchRead{}, malformed200(a.hash)
	}
	prov, err := a.provenance(w.label)
	if err != nil {
		return adapters.SwitchRead{}, err
	}
	if *w.Unavailable {
		return adapters.SwitchRead{Unavailable: true, Provenance: prov}, nil
	}
	rec := adapters.SwitchRecord{SwitchRef: w.SwitchRef, TransactionRef: w.TransactionRef, RawCode: w.RawCode, RawCodeLabel: w.RawCodeLabel,
		SemanticStatus: adapters.SwitchSemanticStatus(w.SemanticStatus), IsTerminal: w.IsTerminal, DestinationAck: w.DestinationAck, ReceivedAt: w.ReceivedAt, RawPayloadHash: w.RawPayloadHash}
	if rec.Validate() != nil || rec.TransactionRef != transactionRef {
		return adapters.SwitchRead{}, malformed200(a.hash)
	}
	return adapters.SwitchRead{Record: rec, Provenance: prov}, nil
}

type wireDestination struct {
	label
	Unavailable     *bool  `json:"unavailable"`
	TransactionRef  string `json:"transaction_ref"`
	CreditState     string `json:"credit_state"`
	SettlementState string `json:"settlement_state"`
	EffectiveAt     string `json:"effective_at"`
	ObservedAt      string `json:"observed_at"`
}

// DestinationStatus implements adapters.Destination. When the source says unavailable, its state strings (the stand-in sends
// UNAVAILABLE there) are not states and are not read.
func (c *StandinClient) DestinationStatus(ctx context.Context, scope adapters.Scope, transactionRef string) (adapters.DestinationRead, error) {
	a, err := c.perRef(ctx, scope, transactionRef, "destination-status")
	if err != nil {
		return adapters.DestinationRead{}, err
	}
	var w wireDestination
	if strict(a.raw, &w) != nil || w.Unavailable == nil {
		return adapters.DestinationRead{}, malformed200(a.hash)
	}
	prov, err := a.provenance(w.label)
	if err != nil {
		return adapters.DestinationRead{}, err
	}
	if *w.Unavailable {
		return adapters.DestinationRead{Unavailable: true, Provenance: prov}, nil
	}
	st := adapters.DestinationStatus{TransactionRef: w.TransactionRef, CreditState: adapters.CreditState(w.CreditState),
		SettlementState: adapters.SettlementState(w.SettlementState), EffectiveAt: w.EffectiveAt, ObservedAt: w.ObservedAt}
	if st.Validate() != nil || st.TransactionRef != transactionRef {
		return adapters.DestinationRead{}, malformed200(a.hash)
	}
	return adapters.DestinationRead{Status: st, Provenance: prov}, nil
}

func (c *StandinClient) perRef(ctx context.Context, scope adapters.Scope, transactionRef, leaf string) (answer, error) {
	if transactionRef == "" {
		return answer{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "ADAPTER:INVALID_INPUT"}
	}
	return c.get(ctx, scope, standinBasePath+"/transactions/"+url.PathEscape(transactionRef)+"/"+leaf, nil)
}

type wireCandidates struct {
	label
	Complete *bool `json:"complete"`
	Items    []struct {
		TransactionRef string `json:"transaction_ref"`
		AccountRef     string `json:"account_ref"`
		Direction      string `json:"direction"`
		AmountMinor    int64  `json:"amount_minor"`
		Currency       string `json:"currency"`
		Counterparty   string `json:"counterparty"`
		OccurredAt     string `json:"occurred_at"`
	} `json:"items"`
}

// FindCandidates implements adapters.Transactions. The stand-in has no pagination: an answer with more items than the caller's
// limit is not cut (that would hide some), it is MALFORMED, which is UNKNOWN. A page the source does not mark complete is returned
// incomplete and proves no absence.
func (c *StandinClient) FindCandidates(ctx context.Context, scope adapters.Scope, q adapters.TransactionQuery) (adapters.CandidatePage, error) {
	if err := q.Validate(); err != nil {
		return adapters.CandidatePage{}, err
	}
	// The stand-in has no pagination: a cursor cannot be honoured, and dropping it would return the first page again as if it were the next
	// (Codex review of noema#205). Refuse it before anything is sent.
	if q.Cursor != "" {
		return adapters.CandidatePage{}, &adapters.ReadError{Failure: adapters.ReadInvalid, RawCode: "FIN:CURSOR_UNSUPPORTED"}
	}
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set("account", q.AccountRef)
	set("from", q.From)
	set("to", q.To)
	set("counterparty", q.Counterparty)
	if q.AmountMinMinor > 0 {
		v.Set(candidateFilterAmount, strconv.FormatInt(q.AmountMinMinor, 10))
	}
	if q.AmountMaxMinor > 0 {
		v.Set("amount_max", strconv.FormatInt(q.AmountMaxMinor, 10))
	}
	a, err := c.get(ctx, scope, standinBasePath+"/transactions", v)
	if err != nil {
		return adapters.CandidatePage{}, err
	}
	var w wireCandidates
	if strict(a.raw, &w) != nil || w.Complete == nil || w.Items == nil || len(w.Items) > q.Limit {
		return adapters.CandidatePage{}, malformed200(a.hash)
	}
	prov, err := a.provenance(w.label)
	if err != nil {
		return adapters.CandidatePage{}, err
	}
	page := adapters.CandidatePage{Complete: *w.Complete, Provenance: prov}
	for _, it := range w.Items {
		cand := adapters.Candidate{TransactionRef: it.TransactionRef, AccountRef: it.AccountRef, Direction: it.Direction,
			AmountMinor: it.AmountMinor, Currency: it.Currency, Counterparty: it.Counterparty, OccurredAt: it.OccurredAt}
		if cand.Validate() != nil {
			return adapters.CandidatePage{}, malformed200(a.hash)
		}
		page.Items = append(page.Items, cand)
	}
	return page, nil
}
