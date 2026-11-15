package adapters

import (
	"context"
	"regexp"
	"time"
)

// This file is the read side of the Tushe-dependent logical services: transactions, ledger, switch and
// settlement/destination (PERD 34.4; REQ-ENV-001, REQ-NFR-006). The payloads are the normalised fin.* shapes of
// PERD 34.4 [REC]. Exact field names are fixed when Tushe's interface is frozen; until then they are our reading,
// the same one the stand-in serves (synthetic/standin/README.md). The Tushe route mapping follows Tushe's final
// written response and is not inferred here (DEC-2026-11-06 A10, DEC-2026-11-04 R13).
//
// Every method is a read. There is no method that writes, moves money, or reaches a control or truth listener
// (BD-08, REQ-ENV-002). Every read is scoped to one authenticated principal (INV-12); there is no global search.
//
// UNKNOWN and UNAVAILABLE are real answers (PERD 34.4): a transport failure is a ReadError, an upstream that says
// "I cannot tell" is an Unavailable read, and neither is ever an empty answer. A value the port does not know is
// MALFORMED, never a guess (EVD-05).

// LabelSynthetic marks a raw code that belongs to the synthetic environment and carries no real switch meaning
// (AGENT-25).
const LabelSynthetic = "SYNTHETIC"

// MaxCandidatePage is the largest page a candidate search may ask for.
const MaxCandidatePage = 100

// Provenance is what the Evidence Layer needs next to a decoded answer: the SHA-256 of the exact bytes the adapter received
// (layer.Observation.RawHash), and the label the source put on its data, passed through so the Record shows it (AGENT-25).
// The stand-in labels everything data_source "stand-in" and synthetic true.
type Provenance struct {
	RawHash    string // "sha256:" and 64 lower-case hex digits
	DataSource string
	Synthetic  bool
}

var rawHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Validate refuses a provenance that cannot back an evidence item: no wire hash, or data that is not labelled synthetic.
func (p Provenance) Validate() error {
	if !rawHashPattern.MatchString(p.RawHash) {
		return malformed("provenance.raw_hash")
	}
	if p.DataSource == "" || !p.Synthetic {
		return malformed("provenance.label")
	}
	return nil
}

// Scope says whose data a read may reach. PrincipalID comes from the authenticated session only, never from model
// output or user text (INV-12, AGENT-10).
type Scope struct {
	TenantID    string
	PrincipalID string
}

// Validate refuses a scope that would reach nothing in particular.
func (s Scope) Validate() error {
	if s.TenantID == "" || s.PrincipalID == "" {
		return &ReadError{Failure: ReadInvalid, RawCode: "FIN:SCOPE_INCOMPLETE"}
	}
	return nil
}

// EntryType is the kind of a ledger entry (PERD 34.4 fin.ledger_entry).
type EntryType string

// Every entry type.
const (
	EntryDebit    EntryType = "DEBIT"
	EntryCredit   EntryType = "CREDIT"
	EntryHold     EntryType = "HOLD"
	EntryRelease  EntryType = "RELEASE"
	EntryReversal EntryType = "REVERSAL"
)

// PostingStatus is how far a ledger entry has got.
type PostingStatus string

// Every posting status.
const (
	PostingPending  PostingStatus = "PENDING"
	PostingPosted   PostingStatus = "POSTED"
	PostingReversed PostingStatus = "REVERSED"
)

// LedgerEntry is the normalised fin.ledger_entry payload.
type LedgerEntry struct {
	EntryID        string
	AccountRef     string
	TransactionRef string
	EntryType      EntryType
	AmountMinor    int64 // integer minor units, never a float (BD-08 money rule)
	Currency       string
	PostingStatus  PostingStatus
	PostedAt       string // RFC 3339, the target's clock (SYNTHETIC in the POC)
	Version        int64
}

// Validate refuses an entry with a missing field or a value the port does not know.
func (e LedgerEntry) Validate() error {
	switch e.EntryType {
	case EntryDebit, EntryCredit, EntryHold, EntryRelease, EntryReversal:
	default:
		return malformed("ledger.entry_type")
	}
	switch e.PostingStatus {
	case PostingPending, PostingPosted, PostingReversed:
	default:
		return malformed("ledger.posting_status")
	}
	for field, v := range map[string]string{"entry_id": e.EntryID, "account_ref": e.AccountRef, "transaction_ref": e.TransactionRef, "currency": e.Currency, "posted_at": e.PostedAt} {
		if v == "" {
			return malformed("ledger." + field)
		}
	}
	if e.AmountMinor <= 0 {
		return malformed("ledger.amount_minor")
	}
	if e.Version <= 0 {
		return malformed("ledger.version")
	}
	return nil
}

// LedgerRead is the answer to a ledger read. Unavailable means the source could not say; Entries is then empty and
// the caller must not read it as "no entries" (PERD 34.4).
type LedgerRead struct {
	Entries     []LedgerEntry
	Unavailable bool
	Provenance  Provenance
}

// IsUnavailable reports whether the source could not answer.
func (r LedgerRead) IsUnavailable() bool { return r.Unavailable }

// SwitchSemanticStatus is the switch status mapped to Noema's words, kept beside the raw code.
type SwitchSemanticStatus string

// Every semantic switch status.
const (
	SwitchAccepted SwitchSemanticStatus = "ACCEPTED"
	SwitchSuccess  SwitchSemanticStatus = "SUCCESS"
	SwitchPending  SwitchSemanticStatus = "PENDING"
	SwitchRejected SwitchSemanticStatus = "REJECTED"
	SwitchTimedOut SwitchSemanticStatus = "TIMED_OUT"
	SwitchUnknown  SwitchSemanticStatus = "UNKNOWN"
)

// SwitchRecord is the normalised fin.switch_record payload. IsTerminal matters: a switch success that is not
// terminal must never be read as a credited transfer (PERD 34.4). The port keeps the flag exactly as the source
// gave it.
type SwitchRecord struct {
	SwitchRef      string
	TransactionRef string
	RawCode        string
	RawCodeLabel   string // always LabelSynthetic in the POC
	SemanticStatus SwitchSemanticStatus
	IsTerminal     bool
	DestinationAck bool
	ReceivedAt     string // RFC 3339
	RawPayloadHash string
}

// Validate refuses a record with a missing field, an unknown status, or a raw code without its SYNTHETIC label.
func (s SwitchRecord) Validate() error {
	switch s.SemanticStatus {
	case SwitchAccepted, SwitchSuccess, SwitchPending, SwitchRejected, SwitchTimedOut, SwitchUnknown:
	default:
		return malformed("switch.semantic_status")
	}
	for field, v := range map[string]string{"switch_ref": s.SwitchRef, "transaction_ref": s.TransactionRef, "raw_code": s.RawCode, "received_at": s.ReceivedAt, "raw_payload_hash": s.RawPayloadHash} {
		if v == "" {
			return malformed("switch." + field)
		}
	}
	if s.RawCodeLabel != LabelSynthetic {
		return malformed("switch.raw_code_label")
	}
	return nil
}

// SwitchRead is the answer to a switch read; see LedgerRead for Unavailable.
type SwitchRead struct {
	Record      SwitchRecord
	Unavailable bool
	Provenance  Provenance
}

// CreditState is the beneficiary-credit lifecycle (SoT 15.2 lifecycles).
type CreditState string

// Every credit state.
const (
	CreditNotApplicable CreditState = "NOT_APPLICABLE"
	CreditPending       CreditState = "PENDING"
	CreditCredited      CreditState = "CREDITED"
	CreditNotCredited   CreditState = "NOT_CREDITED"
	CreditUnknown       CreditState = "UNKNOWN"
)

// SettlementState is the settlement lifecycle. The SoT lifecycle is NOT_REQUIRED, PENDING, SETTLED, FAILED, REVERSED and UNKNOWN; REVERSAL_PENDING is
// added because the stand-in data contract emits it and the fintech pack reads it. By ruling A31 (DEC-2026-11-06) it is non-terminal and is never
// SUCCEEDED, FAILED or REVERSED: a decision on it is wait or escalate. IsTerminal says which states are final.
type SettlementState string

// Every settlement state.
const (
	SettlementNotRequired     SettlementState = "NOT_REQUIRED"
	SettlementPending         SettlementState = "PENDING"
	SettlementSettled         SettlementState = "SETTLED"
	SettlementFailed          SettlementState = "FAILED"
	SettlementReversed        SettlementState = "REVERSED"
	SettlementReversalPending SettlementState = "REVERSAL_PENDING"
	SettlementUnknown         SettlementState = "UNKNOWN"
)

// IsTerminal reports whether the settlement can still change. REVERSAL_PENDING, PENDING and UNKNOWN can; a state the port does not know is not terminal.
func (s SettlementState) IsTerminal() bool {
	switch s {
	case SettlementNotRequired, SettlementSettled, SettlementFailed, SettlementReversed:
		return true
	}
	return false
}

// DestinationStatus is the normalised fin.destination_status payload.
type DestinationStatus struct {
	TransactionRef  string
	CreditState     CreditState
	SettlementState SettlementState
	EffectiveAt     string // when it became true at the source, RFC 3339
	ObservedAt      string // when Noema's read saw it, RFC 3339
}

// Validate refuses a status with a missing field or an unknown state. UNAVAILABLE is not a state; it is
// DestinationRead.Unavailable.
func (d DestinationStatus) Validate() error {
	switch d.CreditState {
	case CreditNotApplicable, CreditPending, CreditCredited, CreditNotCredited, CreditUnknown:
	default:
		return malformed("destination.credit_state")
	}
	switch d.SettlementState {
	case SettlementNotRequired, SettlementPending, SettlementSettled, SettlementFailed, SettlementReversed, SettlementReversalPending, SettlementUnknown:
	default:
		return malformed("destination.settlement_state")
	}
	for field, v := range map[string]string{"transaction_ref": d.TransactionRef, "effective_at": d.EffectiveAt, "observed_at": d.ObservedAt} {
		if v == "" {
			return malformed("destination." + field)
		}
	}
	return nil
}

// DestinationRead is the answer to a settlement and destination read; see LedgerRead for Unavailable.
type DestinationRead struct {
	Status      DestinationStatus
	Unavailable bool
	Provenance  Provenance
}

// Candidate is one transaction a principal may be asking about. The shape is the stand-in's reading of PERD 34.4.
type Candidate struct {
	TransactionRef string
	AccountRef     string
	Direction      string
	AmountMinor    int64
	Currency       string
	Counterparty   string
	OccurredAt     string // RFC 3339
}

// Direction of a candidate transaction, as the benchmark fixture and the stand-in write it.
const (
	DirectionOutgoing = "OUTGOING"
	DirectionIncoming = "INCOMING"
)

// Validate refuses a candidate with a missing field, a direction that is not OUTGOING or INCOMING, a non-positive amount or a time that is not
// RFC 3339 (Codex review of noema#205): a malformed candidate is MALFORMED, never an ordinary one.
func (c Candidate) Validate() error {
	if c.Direction != DirectionOutgoing && c.Direction != DirectionIncoming {
		return malformed("candidate.direction")
	}
	for field, v := range map[string]string{"transaction_ref": c.TransactionRef, "account_ref": c.AccountRef, "currency": c.Currency} {
		if v == "" {
			return malformed("candidate." + field)
		}
	}
	if c.AmountMinor <= 0 {
		return malformed("candidate.amount_minor")
	}
	if _, err := time.Parse(time.RFC3339, c.OccurredAt); err != nil {
		return malformed("candidate.occurred_at")
	}
	return nil
}

// TransactionQuery is a scoped, bounded candidate search: by account, time window, amount range and counterparty.
// An unset From, To, AmountMaxMinor or Counterparty is no filter; Limit is required and capped.
type TransactionQuery struct {
	AccountRef     string
	From           string
	To             string
	AmountMinMinor int64
	AmountMaxMinor int64
	Counterparty   string
	Limit          int
	Cursor         string
}

// Validate refuses a query the source would have to guess about.
func (q TransactionQuery) Validate() error {
	if q.Limit <= 0 || q.Limit > MaxCandidatePage {
		return invalid("TRANSACTIONS:LIMIT")
	}
	if q.AmountMinMinor < 0 || q.AmountMaxMinor < 0 || (q.AmountMaxMinor > 0 && q.AmountMinMinor > q.AmountMaxMinor) {
		return invalid("TRANSACTIONS:AMOUNT_RANGE")
	}
	var from, to time.Time
	var err error
	if q.From != "" {
		if from, err = time.Parse(time.RFC3339, q.From); err != nil {
			return invalid("TRANSACTIONS:FROM")
		}
	}
	if q.To != "" {
		if to, err = time.Parse(time.RFC3339, q.To); err != nil {
			return invalid("TRANSACTIONS:TO")
		}
	}
	if q.From != "" && q.To != "" && from.After(to) {
		return invalid("TRANSACTIONS:WINDOW")
	}
	return nil
}

// CandidatePage is one page of candidates. Complete is the explicit completeness marker.
type CandidatePage struct {
	Items      []Candidate
	Complete   bool
	NextCursor string
	Provenance Provenance
}

// ProvesAbsence is true only for an empty page that the source marked complete. An empty incomplete page proves
// nothing: the caller must not conclude "no such transaction" (PERD 34.4, EC-S11).
func (p CandidatePage) ProvesAbsence() bool { return len(p.Items) == 0 && p.Complete }

// Transactions is the candidate search of the transaction family.
type Transactions interface {
	FindCandidates(ctx context.Context, scope Scope, q TransactionQuery) (CandidatePage, error)
}

// Ledger is the double-entry ledger read.
type Ledger interface {
	LedgerEntries(ctx context.Context, scope Scope, transactionRef string) (LedgerRead, error)
}

// Switch is the switch status read (the provider's answer and transition log).
type Switch interface {
	SwitchStatus(ctx context.Context, scope Scope, transactionRef string) (SwitchRead, error)
}

// Destination is the credit and settlement status read.
type Destination interface {
	DestinationStatus(ctx context.Context, scope Scope, transactionRef string) (DestinationRead, error)
}

func malformed(what string) error { return &ReadError{Failure: ReadMalformed, RawCode: "FIN:" + what} }

func invalid(what string) error { return &ReadError{Failure: ReadInvalid, RawCode: "FIN:" + what} }
