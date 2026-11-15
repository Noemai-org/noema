package adapters

import (
	"errors"
	"strings"
	"testing"
)

// These tests pin the normalised fin.* payloads of PERD 34.4 [REC] and the one rule every consumer relies on:
// a value the port does not know is MALFORMED (unknown), never a guess (AGENT-02, EVD-05). Field names are the
// stand-in's reading of PERD 34.4, not Tushe's; the Tushe route mapping follows Tushe's final written response
// (DEC-2026-11-06 A10).

func validLedgerEntry() LedgerEntry {
	return LedgerEntry{EntryID: "le_1_1", AccountRef: "acc_usr_a", TransactionRef: "tx:1", EntryType: EntryDebit, AmountMinor: 500000,
		Currency: "NGN", PostingStatus: PostingPosted, PostedAt: "2026-10-01T09:30:00+01:00", Version: 1}
}

func validSwitchRecord() SwitchRecord {
	return SwitchRecord{SwitchRef: "sw_1", TransactionRef: "tx:1", RawCode: "SYN-00", RawCodeLabel: LabelSynthetic, SemanticStatus: SwitchAccepted,
		IsTerminal: true, DestinationAck: false, ReceivedAt: "2026-10-01T09:30:00+01:00", RawPayloadHash: "sha256:" + strings.Repeat("0", 64)}
}

func validDestinationStatus() DestinationStatus {
	return DestinationStatus{TransactionRef: "tx:1", CreditState: CreditPending, SettlementState: SettlementPending,
		EffectiveAt: "2026-10-01T09:30:00+01:00", ObservedAt: "2026-10-01T09:40:00+01:00"}
}

func TestAValidPayloadPassesItsOwnCheck(t *testing.T) {
	if err := validLedgerEntry().Validate(); err != nil {
		t.Fatalf("ledger entry: %v", err)
	}
	if err := validSwitchRecord().Validate(); err != nil {
		t.Fatalf("switch record: %v", err)
	}
	if err := validDestinationStatus().Validate(); err != nil {
		t.Fatalf("destination status: %v", err)
	}
}

func TestEveryEnumeratedValueOfPERD344IsAccepted(t *testing.T) {
	for _, v := range []EntryType{EntryDebit, EntryCredit, EntryHold, EntryRelease, EntryReversal} {
		e := validLedgerEntry()
		e.EntryType = v
		if err := e.Validate(); err != nil {
			t.Errorf("entry type %q: %v", v, err)
		}
	}
	for _, v := range []PostingStatus{PostingPending, PostingPosted, PostingReversed} {
		e := validLedgerEntry()
		e.PostingStatus = v
		if err := e.Validate(); err != nil {
			t.Errorf("posting status %q: %v", v, err)
		}
	}
	for _, v := range []SwitchSemanticStatus{SwitchAccepted, SwitchSuccess, SwitchPending, SwitchRejected, SwitchTimedOut, SwitchUnknown} {
		s := validSwitchRecord()
		s.SemanticStatus = v
		if err := s.Validate(); err != nil {
			t.Errorf("semantic status %q: %v", v, err)
		}
	}
}

func TestAValueThePortDoesNotKnowIsMalformedNeverGuessed(t *testing.T) {
	cases := map[string]func() error{
		"entry type":       func() error { e := validLedgerEntry(); e.EntryType = "REFUND"; return e.Validate() },
		"posting status":   func() error { e := validLedgerEntry(); e.PostingStatus = "NOT_POSTED"; return e.Validate() },
		"semantic status":  func() error { s := validSwitchRecord(); s.SemanticStatus = "APPROVED"; return s.Validate() },
		"credit state":     func() error { d := validDestinationStatus(); d.CreditState = "UNAVAILABLE"; return d.Validate() },
		"settlement state": func() error { d := validDestinationStatus(); d.SettlementState = "DONE"; return d.Validate() },
		"lowercase":        func() error { e := validLedgerEntry(); e.EntryType = "debit"; return e.Validate() },
	}
	for name, run := range cases {
		err := run()
		var re *ReadError
		if !errors.As(err, &re) || re.Failure != ReadMalformed {
			t.Errorf("%s: want a ReadError with ReadMalformed, got %v", name, err)
		}
	}
}

func TestARequiredFieldThatIsMissingIsMalformed(t *testing.T) {
	cases := map[string]func() error{
		"ledger entry id":      func() error { e := validLedgerEntry(); e.EntryID = ""; return e.Validate() },
		"ledger account":       func() error { e := validLedgerEntry(); e.AccountRef = ""; return e.Validate() },
		"ledger transaction":   func() error { e := validLedgerEntry(); e.TransactionRef = ""; return e.Validate() },
		"ledger currency":      func() error { e := validLedgerEntry(); e.Currency = ""; return e.Validate() },
		"ledger posted at":     func() error { e := validLedgerEntry(); e.PostedAt = ""; return e.Validate() },
		"ledger version":       func() error { e := validLedgerEntry(); e.Version = 0; return e.Validate() },
		"switch ref":           func() error { s := validSwitchRecord(); s.SwitchRef = ""; return s.Validate() },
		"switch transaction":   func() error { s := validSwitchRecord(); s.TransactionRef = ""; return s.Validate() },
		"switch raw code":      func() error { s := validSwitchRecord(); s.RawCode = ""; return s.Validate() },
		"switch received at":   func() error { s := validSwitchRecord(); s.ReceivedAt = ""; return s.Validate() },
		"switch payload hash":  func() error { s := validSwitchRecord(); s.RawPayloadHash = ""; return s.Validate() },
		"destination txn":      func() error { d := validDestinationStatus(); d.TransactionRef = ""; return d.Validate() },
		"destination effect":   func() error { d := validDestinationStatus(); d.EffectiveAt = ""; return d.Validate() },
		"destination observed": func() error { d := validDestinationStatus(); d.ObservedAt = ""; return d.Validate() },
	}
	for name, run := range cases {
		err := run()
		var re *ReadError
		if !errors.As(err, &re) || re.Failure != ReadMalformed {
			t.Errorf("%s: want a ReadError with ReadMalformed, got %v", name, err)
		}
	}
}

func TestAMoneyAmountIsAPositiveIntegerOfMinorUnits(t *testing.T) {
	for _, bad := range []int64{0, -1} {
		e := validLedgerEntry()
		e.AmountMinor = bad
		if err := e.Validate(); err == nil {
			t.Errorf("amount_minor %d must be refused", bad)
		}
	}
}

func TestTheRawSwitchCodeStaysBesideItsSyntheticLabel(t *testing.T) {
	s := validSwitchRecord()
	s.RawCodeLabel = ""
	if err := s.Validate(); err == nil {
		t.Fatal("a raw code without the SYNTHETIC label must be refused (AGENT-25)")
	}
	s.RawCodeLabel = "REAL"
	if err := s.Validate(); err == nil {
		t.Fatal("any label other than SYNTHETIC must be refused in the POC")
	}
}

func TestANonTerminalSwitchSuccessIsValidAndStaysNonTerminal(t *testing.T) {
	// PERD 34.4: "A switch 'success' that is not terminal must never be read as a credited transfer." The port keeps
	// the flag exactly as the source gave it; it never promotes a success to a terminal one.
	s := validSwitchRecord()
	s.SemanticStatus = SwitchSuccess
	s.IsTerminal = false
	if err := s.Validate(); err != nil {
		t.Fatalf("a non-terminal success is a real answer: %v", err)
	}
	if s.IsTerminal {
		t.Fatal("Validate must not change IsTerminal")
	}
}

func TestADestinationReadOfAnotherStateIsKeptAsGiven(t *testing.T) {
	for _, credit := range []CreditState{CreditNotApplicable, CreditPending, CreditCredited, CreditNotCredited, CreditUnknown} {
		d := validDestinationStatus()
		d.CreditState = credit
		if err := d.Validate(); err != nil {
			t.Errorf("credit state %q: %v", credit, err)
		}
	}
	for _, s := range []SettlementState{SettlementNotRequired, SettlementPending, SettlementSettled, SettlementFailed, SettlementReversed, SettlementReversalPending, SettlementUnknown} {
		d := validDestinationStatus()
		d.SettlementState = s
		if err := d.Validate(); err != nil {
			t.Errorf("settlement state %q: %v", s, err)
		}
	}
}

func TestAScopeNeedsATenantAndAnAuthenticatedPrincipal(t *testing.T) {
	// INV-12: the principal comes from the authenticated session. A read with no principal reaches nothing.
	for name, s := range map[string]Scope{"no tenant": {PrincipalID: "usr_a"}, "no principal": {TenantID: "t1"}, "neither": {}} {
		err := s.Validate()
		var re *ReadError
		if !errors.As(err, &re) || re.Failure != ReadInvalid {
			t.Errorf("%s: want ReadInvalid, got %v", name, err)
		}
	}
	if err := (Scope{TenantID: "t1", PrincipalID: "usr_a"}).Validate(); err != nil {
		t.Fatalf("a full scope: %v", err)
	}
}

func TestACandidateQueryIsBoundedAndNeverGlobal(t *testing.T) {
	ok := TransactionQuery{AccountRef: "acc_usr_a", Limit: 20}
	if err := ok.Validate(); err != nil {
		t.Fatalf("a bounded query: %v", err)
	}
	for name, q := range map[string]TransactionQuery{
		"no limit":           {AccountRef: "acc_usr_a"},
		"negative limit":     {AccountRef: "acc_usr_a", Limit: -1},
		"limit over cap":     {AccountRef: "acc_usr_a", Limit: MaxCandidatePage + 1},
		"inverted amounts":   {AccountRef: "acc_usr_a", Limit: 5, AmountMinMinor: 10, AmountMaxMinor: 5},
		"negative min":       {AccountRef: "acc_usr_a", Limit: 5, AmountMinMinor: -1},
		"window not RFC3339": {AccountRef: "acc_usr_a", Limit: 5, From: "yesterday"},
		"inverted window":    {AccountRef: "acc_usr_a", Limit: 5, From: "2026-10-02T00:00:00Z", To: "2026-10-01T00:00:00Z"},
	} {
		err := q.Validate()
		var re *ReadError
		if !errors.As(err, &re) || re.Failure != ReadInvalid {
			t.Errorf("%s: want ReadInvalid, got %v", name, err)
		}
	}
}

func TestAnEmptyCandidatePageIsAnAbsenceOnlyWhenComplete(t *testing.T) {
	if (CandidatePage{Complete: true}).ProvesAbsence() != true {
		t.Fatal("an empty complete page proves absence")
	}
	if (CandidatePage{Complete: false}).ProvesAbsence() {
		t.Fatal("an empty incomplete page must not prove absence")
	}
	if (CandidatePage{Complete: true, Items: []Candidate{{TransactionRef: "tx:1"}}}).ProvesAbsence() {
		t.Fatal("a page with an item proves no absence")
	}
}

func TestAnUnavailableReadIsNotAnEmptyAnswer(t *testing.T) {
	if !(LedgerRead{Unavailable: true}).IsUnavailable() {
		t.Fatal("a ledger read marked unavailable must say so")
	}
	l := LedgerRead{Unavailable: true}
	if len(l.Entries) != 0 {
		t.Fatal("an unavailable ledger read carries no entries")
	}
	if (LedgerRead{}).IsUnavailable() {
		t.Fatal("an empty available ledger read is a real empty answer, not unavailable")
	}
}

func TestProvenanceKeepsTheWireHashAndTheSyntheticLabel(t *testing.T) {
	good := Provenance{RawHash: "sha256:" + strings.Repeat("a", 64), DataSource: "stand-in", Synthetic: true}
	if err := good.Validate(); err != nil {
		t.Fatalf("a full provenance: %v", err)
	}
	for name, p := range map[string]Provenance{
		"no hash":                {DataSource: "stand-in", Synthetic: true},
		"no prefix":              {RawHash: strings.Repeat("a", 64), DataSource: "stand-in", Synthetic: true},
		"upper case":             {RawHash: "sha256:" + strings.Repeat("A", 64), DataSource: "stand-in", Synthetic: true},
		"short":                  {RawHash: "sha256:abc", DataSource: "stand-in", Synthetic: true},
		"no source":              {RawHash: good.RawHash, Synthetic: true},
		"not labelled synthetic": {RawHash: good.RawHash, DataSource: "stand-in"},
	} {
		err := p.Validate()
		var re *ReadError
		if !errors.As(err, &re) || re.Failure != ReadMalformed {
			t.Errorf("%s: want ReadMalformed, got %v", name, err)
		}
	}
}

func validCandidate() Candidate {
	return Candidate{TransactionRef: "tx:1", AccountRef: "acc_usr_a", Direction: DirectionOutgoing, AmountMinor: 500000, Currency: "NGN", Counterparty: "ben_1", OccurredAt: "2026-10-01T09:30:00+01:00"}
}

// Codex review of noema#205 (P2): a candidate with a missing or arbitrary direction, or a timestamp that is not RFC 3339, must be MALFORMED,
// not an ordinary candidate.
func TestACandidateWithAnUnknownDirectionOrAMalformedTimeIsMalformed(t *testing.T) {
	if err := validCandidate().Validate(); err != nil {
		t.Fatalf("a valid candidate: %v", err)
	}
	in := validCandidate()
	in.Direction = DirectionIncoming
	if err := in.Validate(); err != nil {
		t.Fatalf("an incoming candidate: %v", err)
	}
	for name, mut := range map[string]func(*Candidate){
		"no direction":      func(c *Candidate) { c.Direction = "" },
		"unknown direction": func(c *Candidate) { c.Direction = "SIDEWAYS" },
		"lower-case":        func(c *Candidate) { c.Direction = "outgoing" },
		"no time":           func(c *Candidate) { c.OccurredAt = "" },
		"not RFC 3339":      func(c *Candidate) { c.OccurredAt = "yesterday" },
		"date only":         func(c *Candidate) { c.OccurredAt = "2026-10-01" },
		"no transaction":    func(c *Candidate) { c.TransactionRef = "" },
		"no account":        func(c *Candidate) { c.AccountRef = "" },
		"no currency":       func(c *Candidate) { c.Currency = "" },
		"zero amount":       func(c *Candidate) { c.AmountMinor = 0 },
		"negative amount":   func(c *Candidate) { c.AmountMinor = -5 },
	} {
		c := validCandidate()
		mut(&c)
		err := c.Validate()
		var re *ReadError
		if !errors.As(err, &re) || re.Failure != ReadMalformed {
			t.Errorf("%s: want a ReadError with ReadMalformed, got %v", name, err)
		}
	}
}

// Evidence Layer request (noema#205 comment): an error read that received a body carries the hash of those bytes.
func TestAReadErrorCanCarryTheHashOfTheBytesItReceived(t *testing.T) {
	e := &ReadError{Failure: ReadMalformed, RawCode: "200", RawHash: "sha256:" + strings.Repeat("a", 64)}
	if e.RawHash == "" {
		t.Fatal("RawHash must be a field of ReadError")
	}
}

// A31 (DEC-2026-11-06): REVERSAL_PENDING is non-terminal and is never SUCCEEDED, FAILED or REVERSED; a decision on it is wait or escalate.
// The port keeps it as its own state and says so, so no caller can read it as a final one.
func TestReversalPendingIsNonTerminalAndNeverAFinalState(t *testing.T) {
	terminal := map[SettlementState]bool{SettlementNotRequired: true, SettlementSettled: true, SettlementFailed: true, SettlementReversed: true,
		SettlementPending: false, SettlementReversalPending: false, SettlementUnknown: false}
	for s, want := range terminal {
		if got := s.IsTerminal(); got != want {
			t.Errorf("%s: IsTerminal = %v, want %v", s, got, want)
		}
	}
	if SettlementReversalPending == SettlementSettled || SettlementReversalPending == SettlementFailed || SettlementReversalPending == SettlementReversed {
		t.Fatal("REVERSAL_PENDING must be its own state")
	}
	if SettlementState("SOMETHING_ELSE").IsTerminal() {
		t.Fatal("a state the port does not know is never terminal")
	}
}
