package trustport

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/ceilings"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

// Candidates is the adapter of the intake's T7 read (intake.Candidates; PERD 23.3; ruling A329). For an intent whose reference slot is not resolved it reads the candidates through
// the principal-scoped candidates read (REQ-CTX-006: candidates-request.v1 in, candidates-response.v1 out) as the session's tenant and principal, has them scored and labelled by
// two ports, and answers them with the pack's disambiguation data. It decides nothing: Trust sorts, floors and asks (INV-01), and the pack, not this file, holds every number
// (INV-14, AGENT-16). The adapter is one tenant's (A320: one instance per tenant); a session of another tenant is refused before anything is read (INV-11).
//
// Every error has fixed text and carries no row, no ID of another principal and no word of the customer (AGENT-13); the cause stays reachable through errors.Is and errors.As.
// An error is never an empty list: only a search that ran and found nothing answers with no candidates (REQ-AMB-002).

// Errors of the adapter. Callers compare with errors.Is. ErrSession, ErrConfig and ErrBadAnswer are the package's own (port.go); these are the candidate read's.
var (
	// ErrCandidatesDeadline is returned for a context with no deadline (AGENT-16).
	ErrCandidatesDeadline = errors.New("trustport: the candidates read needs a context with a deadline")
	// ErrCandidatesTenant is returned for a session of another tenant than the adapter's (INV-11).
	ErrCandidatesTenant = errors.New("trustport: the session is not of this adapter's tenant")
	// ErrCandidatesIntent is the one answer for an intent that is not the session's: another tenant's, another principal's, one that does not exist, one the store cannot give, and
	// one whose document is not usable (REQ-TRU-003).
	ErrCandidatesIntent = errors.New("trustport: the intent is not usable for this session")
	// ErrCandidatesNotNeeded is returned for an intent that needs no candidates: its node has no candidate data in the pack, or its reference slot is already resolved.
	ErrCandidatesNotNeeded = errors.New("trustport: the intent needs no candidates")
	// ErrCandidatesNoWindow is returned when the intent gives no usable time window. Nothing is read (REQ-AMB-002).
	ErrCandidatesNoWindow = errors.New("trustport: the intent gives no time window to search")
	// ErrCandidatesRead is returned when the candidates read fails.
	ErrCandidatesRead = errors.New("trustport: the candidates could not be read")
	// ErrCandidatesRank is returned when the ranking or the labelling port fails.
	ErrCandidatesRank = errors.New("trustport: the candidates could not be scored or labelled")
)

// candidatesError is an error whose text is its kind's alone. The cause is kept for errors.Is and errors.As and never printed.
type candidatesError struct{ kind, cause error }

func (e *candidatesError) Error() string        { return e.kind.Error() }
func (e *candidatesError) Unwrap() error        { return e.cause }
func (e *candidatesError) Is(target error) bool { return target == e.kind }

func refuseCandidates(kind, cause error) error { return &candidatesError{kind: kind, cause: cause} }

// CandidateReader is the principal-scoped candidates read (REQ-CTX-006), in its contract's own types. The real one is built in evidence (the orchestrator area may not import the
// adapters); this package never reaches an enterprise system.
type CandidateReader interface {
	Read(ctx context.Context, req contracts.CandidatesRequestV1) (contracts.CandidatesResponseV1, error)
}

// CandidateIntents reads one stored intent as the adapter's tenant; intentstore.Reader is the real one.
type CandidateIntents interface {
	Get(ctx context.Context, intentID string, version int) (intentstore.Admitted, error)
}

// CandidateSearch is what the ranking port gets besides the candidates: the intent's own terms, not the widened ones the search used. The surface form is the customer's text:
// data, never an instruction (AGENT-09), bounded to the contract's length.
type CandidateSearch struct {
	From, To            time.Time
	HasAmount           bool
	AmountMinor         int64
	Unit                string
	CounterpartySurface string
}

// CandidateScore is the score of one candidate in basis points (0 to 10000), the unit of the pack's floor.
type CandidateScore struct {
	CandidateID string
	ScoreBP     int64
}

// CandidateRanker scores candidates by the pack's weights (REQ-AMB-003). It has no default: an adapter without one is not built. It must answer one score for each candidate.
type CandidateRanker interface {
	Rank(ctx context.Context, terms CandidateSearch, rows []contracts.CandidatesResponseV1Candidate) ([]CandidateScore, error)
}

// CandidateLabel is the pre-formatted, masked text of one candidate: Display for OPTIONS mode and the value of each askable attribute (REQ-AMB-004).
type CandidateLabel struct {
	Display string
	Attrs   map[string]string
}

// CandidateLabeller makes labels from the pack's templates and the masked candidate. It has no default. The adapter checks what it returns.
type CandidateLabeller interface {
	Label(c contracts.CandidatesResponseV1Candidate) (CandidateLabel, error)
}

// CandidateNode is the pack's data for one intent node that needs candidates. The composition fills it from the pack; nothing in it is held here.
type CandidateNode struct {
	// ReferenceSlot is the slot of type Resource that candidate search resolves; TimeSlot, AmountSlot and PartySlot are the slots the search reads (AmountSlot and PartySlot
	// may be empty). ResourceKind is the pack's `<vertical>.<kind>` of the resource.
	ReferenceSlot, ResourceKind, TimeSlot, AmountSlot, PartySlot string
	// MaxRaw is the pack's bound on raw candidates (at most the contract's twenty).
	MaxRaw int
	// TimeToleranceSeconds and AmountToleranceBP are the pack's tolerances; the amount one is in basis points.
	TimeToleranceSeconds int
	AmountToleranceBP    int
	// Disambiguation is the pack's data for T7, with the floor in basis points.
	Disambiguation intake.Disambiguation
}

// CandidatesConfig is everything the adapter needs. None of it has a default.
type CandidatesConfig struct {
	TenantID string
	Intents  CandidateIntents
	Reader   CandidateReader
	Ranker   CandidateRanker
	Labeller CandidateLabeller
	// Nodes is the pack's candidate data by intent node ID.
	Nodes map[string]CandidateNode
}

// Candidates implements intake.Candidates for one tenant. The zero value refuses every call.
type Candidates struct {
	tenantID string
	intents  CandidateIntents
	reader   CandidateReader
	ranker   CandidateRanker
	labeller CandidateLabeller
	nodes    map[string]CandidateNode
	ready    bool
}

var _ intake.Candidates = Candidates{}

const (
	// candBPUnit is a whole in basis points. candMaxRaw is the contract's maxItems of candidates-response.v1. candMaxSurfaceRunes is the contract's maxLength of
	// counterparty_surface. candMaxMinor is the contract's MinorAmount maximum. candSchemaVersion is the version of both candidate contracts.
	candBPUnit           = 10_000
	candMaxRaw           = 20
	candMaxSurfaceRunes  = 64
	candMaxMinor         = 9007199254740991
	candSchemaVersion    = "1.0"
	candValuePlaceholder = "{value}"
)

var (
	candTenantShape    = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)
	candPrincipalShape = regexp.MustCompile(`\Ausr_[0-9A-Za-z]{2,32}\z`)
	candCorrShape      = regexp.MustCompile(`\Acorr_[0-9A-Za-z]{2,32}\z`)
	candIDShape        = regexp.MustCompile(`\Acand_[0-9A-Za-z]{2,32}\z`)
	candSlotShape      = regexp.MustCompile(`\A[a-z][a-z0-9_]{0,63}\z`)
	candKindShape      = regexp.MustCompile(`\A[a-z][a-z0-9]*\.[a-z][a-z0-9_]*\z`)
	candRefIDShape     = regexp.MustCompile(`\A[A-Za-z0-9_.-]{1,64}\z`)
	candLast4Shape     = regexp.MustCompile(`\A[0-9]{4}\z`)
	// candVersionShape is the answer's schema version: major.minor (common.v1.json SchemaVersion). This adapter implements major 1 of candidates-response, so a later minor of 1 is
	// accepted (additive by the contract's rule) and any other major is refused whole, because its shape is not the one checked here.
	candVersionShape = regexp.MustCompile(`\A1\.[0-9]+\z`)
	candInstantShape = regexp.MustCompile(`\A[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z\z`)
)

// NewCandidates builds the adapter. A missing part, a tenant or an ID off its shape, or pack data that is not usable (a floor below the platform's, more candidates in a question
// or a longer digit run than the platform allows, a question with no key, a bound over the contract's) is ErrConfig. The pack's data is copied: a later edit by the caller changes
// nothing here.
func NewCandidates(cfg CandidatesConfig) (Candidates, error) {
	if !candTenantShape.MatchString(cfg.TenantID) || cfg.Intents == nil || cfg.Reader == nil || cfg.Ranker == nil || cfg.Labeller == nil || len(cfg.Nodes) == 0 {
		return Candidates{}, ErrConfig
	}
	nodes := make(map[string]CandidateNode, len(cfg.Nodes))
	for id, n := range cfg.Nodes {
		if id == "" || !n.usable() {
			return Candidates{}, ErrConfig
		}
		n.Disambiguation = candCloneDisambiguation(n.Disambiguation)
		nodes[id] = n
	}
	return Candidates{tenantID: cfg.TenantID, intents: cfg.Intents, reader: cfg.Reader, ranker: cfg.Ranker, labeller: cfg.Labeller, nodes: nodes, ready: true}, nil
}

func (n CandidateNode) usable() bool {
	slots := []string{n.ReferenceSlot, n.TimeSlot}
	for _, s := range []string{n.AmountSlot, n.PartySlot} {
		if s != "" {
			slots = append(slots, s)
		}
	}
	seen := map[string]bool{}
	for _, s := range slots {
		if !candSlotShape.MatchString(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	switch {
	case !candKindShape.MatchString(n.ResourceKind),
		n.MaxRaw < 1 || n.MaxRaw > candMaxRaw,
		n.TimeToleranceSeconds < 0 || n.TimeToleranceSeconds > int(math.MaxInt64/int64(time.Second)),
		n.AmountToleranceBP < 0 || n.AmountToleranceBP > candBPUnit:
		return false
	}
	return candDisambiguationUsable(n.Disambiguation)
}

// candDisambiguationUsable says whether the pack's data for T7 can be passed on as it is: the intake refuses a value above a ceiling at every call, and this refuses it once, when the
// adapter is built.
func candDisambiguationUsable(d intake.Disambiguation) bool {
	if d.Floor < ceilings.MinAmbiguityAbsoluteFloorBP || d.Floor > candBPUnit || d.MaxCandidates < 1 || d.MaxCandidates > ceilings.MaxClarificationOptions ||
		d.MaxDigitRun < 0 || d.MaxDigitRun > ceilings.MaxLabelDigitRun || d.OptionsKey == "" || len(d.AttributeOrder) == 0 {
		return false
	}
	names := map[string]bool{}
	for _, a := range d.AttributeOrder {
		if a.Name == "" || a.QuestionKey == "" || !strings.Contains(a.LabelTemplate, candValuePlaceholder) || names[a.Name] {
			return false
		}
		names[a.Name] = true
	}
	return true
}

func candCloneDisambiguation(d intake.Disambiguation) intake.Disambiguation {
	d.AttributeOrder = append([]decide.AttributeSpec(nil), d.AttributeOrder...)
	return d
}

// ScopedCandidates is the T7 read. Nothing is read until the context has a deadline, the tenant and the principal are the session's and of the contract's shape, the intent is the
// session's and needs candidates, and the intent gives a time window.
func (c Candidates) ScopedCandidates(ctx context.Context, tenantID, principalID string, a intake.AdmittedIntent) (intake.TargetCandidates, error) {
	none := intake.TargetCandidates{}
	if !c.ready {
		return none, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return none, ErrCandidatesDeadline
	}
	if !candTenantShape.MatchString(tenantID) || !candPrincipalShape.MatchString(principalID) {
		return none, ErrSession
	}
	if tenantID != c.tenantID {
		return none, ErrCandidatesTenant
	}
	if a.TenantID != tenantID || a.PrincipalID != principalID || a.IntentID == "" || a.Version < 1 || !candCorrShape.MatchString(a.CorrelationID) {
		return none, refuseCandidates(ErrCandidatesIntent, nil)
	}
	node, ok := c.nodes[a.IntentNode]
	if !ok {
		return none, refuseCandidates(ErrCandidatesNotNeeded, nil)
	}
	if st, present := a.View.RequiredSlots[node.ReferenceSlot]; !present || st == decide.SlotResolved {
		return none, refuseCandidates(ErrCandidatesNotNeeded, nil)
	}
	stored, err := c.intents.Get(ctx, a.IntentID, a.Version)
	if err != nil {
		return none, refuseCandidates(ErrCandidatesIntent, err)
	}
	if stored.TenantID != tenantID || stored.IntentID != a.IntentID || stored.Version != a.Version || stored.IntentNode != a.IntentNode || stored.CorrelationID != a.CorrelationID {
		return none, refuseCandidates(ErrCandidatesIntent, nil)
	}
	tm, err := candReadTerms(stored.JSON, principalID, node)
	if err != nil {
		return none, err
	}
	req := candSearchRequest(tenantID, principalID, a.CorrelationID, node, tm)
	resp, err := c.reader.Read(ctx, req)
	if err != nil {
		return none, refuseCandidates(ErrCandidatesRead, err)
	}
	rows, err := candCheckAnswer(resp, req, node)
	if err != nil {
		return none, err
	}
	ranked, err := c.rankAndLabel(ctx, tm.search, rows, node)
	if err != nil {
		return none, err
	}
	return intake.TargetCandidates{Disambiguation: candCloneDisambiguation(node.Disambiguation), Ranked: ranked}, nil
}

// candTerms is what the stored intent says about the search, and the range the search will use.
type candTerms struct {
	search           CandidateSearch
	from, to         time.Time // the intent's window widened by the pack's tolerance
	ranged           bool
	rangeLo, rangeHi int64
	currency         contracts.MoneyV1CurrencyCode
}

type candStoredSlot struct {
	Type        string          `json:"type"`
	Window      json.RawMessage `json:"window"`
	Value       json.RawMessage `json:"value"`
	Unit        json.RawMessage `json:"unit"`
	SurfaceForm json.RawMessage `json:"surface_form"`
}

// candReadTerms reads only the members the search needs from the stored intent, and proves that it names exactly the session's principal (the database binds the tenant; nothing there
// binds the principal, INV-12). Anything else it cannot read is a refusal, never a guess and never a wider search than the pack's tolerance gives.
func candReadTerms(doc []byte, principalID string, n CandidateNode) (candTerms, error) {
	var d struct {
		Principal struct {
			PrincipalID string `json:"principal_id"`
		} `json:"principal"`
		Slots map[string]json.RawMessage `json:"slots"`
	}
	if json.Unmarshal(doc, &d) != nil || d.Principal.PrincipalID == "" || d.Principal.PrincipalID != principalID {
		return candTerms{}, refuseCandidates(ErrCandidatesIntent, nil)
	}
	slot := func(name string) (s candStoredSlot, present bool, err error) {
		raw, present := d.Slots[name]
		if !present {
			return candStoredSlot{}, false, nil
		}
		if json.Unmarshal(raw, &s) != nil {
			return candStoredSlot{}, true, refuseCandidates(ErrCandidatesIntent, nil)
		}
		return s, true, nil
	}
	var t candTerms
	ts, _, err := slot(n.TimeSlot) // an absent slot is the zero slot, whose type is not a time reference
	if err != nil {
		return candTerms{}, err
	}
	from, to, ok := candWindow(ts.Window)
	if ts.Type != "TimeRef" || !ok {
		return candTerms{}, refuseCandidates(ErrCandidatesNoWindow, nil)
	}
	t.search.From, t.search.To = from, to
	tol := time.Duration(n.TimeToleranceSeconds) * time.Second
	t.from, t.to = from.Add(-tol), to.Add(tol)
	if n.AmountSlot != "" {
		as, present, err := slot(n.AmountSlot)
		if err != nil {
			return candTerms{}, err
		}
		if present {
			if err := t.readAmount(as, n.AmountToleranceBP); err != nil {
				return candTerms{}, err
			}
		}
	}
	if n.PartySlot != "" {
		ps, present, err := slot(n.PartySlot)
		if err != nil {
			return candTerms{}, err
		}
		if present {
			surface, ok := candText(ps.SurfaceForm)
			if ps.Type != "Party" || !ok || utf8.RuneCountInString(surface) > candMaxSurfaceRunes {
				return candTerms{}, refuseCandidates(ErrCandidatesIntent, nil)
			}
			t.search.CounterpartySurface = surface
		}
	}
	return t, nil
}

// candWindow reads a window of exactly two UTC instants, the first not after the second.
func candWindow(raw json.RawMessage) (from, to time.Time, ok bool) {
	var w []string
	if json.Unmarshal(raw, &w) != nil || len(w) != 2 || !candInstantShape.MatchString(w[0]) || !candInstantShape.MatchString(w[1]) {
		return time.Time{}, time.Time{}, false
	}
	from, err1 := time.Parse(time.RFC3339Nano, w[0])
	to, err2 := time.Parse(time.RFC3339Nano, w[1])
	if err1 != nil || err2 != nil || from.After(to) {
		return time.Time{}, time.Time{}, false
	}
	return from, to, true
}

// candText reads a JSON string and nothing else.
func candText(raw json.RawMessage) (string, bool) {
	var s string
	if json.Unmarshal(raw, &s) != nil {
		return "", false
	}
	return s, true
}

// readAmount reads a Quantity slot: a whole positive number of minor units within the contract's bounds, and a unit. The range is the amount widened outward by the pack's
// tolerance, and only for a unit that is a currency of the contract; for any other unit the search has no amount filter, which is wider and never narrower.
func (t *candTerms) readAmount(s candStoredSlot, toleranceBP int) error {
	qty, err := strconv.ParseInt(string(bytes.TrimSpace(s.Value)), 10, 64)
	unit, unitOK := candText(s.Unit)
	if s.Type != "Quantity" || err != nil || qty < 1 || qty > candMaxMinor || !unitOK || unit == "" {
		return refuseCandidates(ErrCandidatesIntent, nil)
	}
	t.search.HasAmount, t.search.AmountMinor, t.search.Unit = true, qty, unit
	var cur contracts.MoneyV1CurrencyCode
	if json.Unmarshal([]byte(strconv.Quote(unit)), &cur) != nil {
		return nil
	}
	// Integer arithmetic that rounds the tolerance up: qty*bp/candBPUnit without the overflow of the product.
	bp := int64(toleranceBP)
	spread := qty/candBPUnit*bp + (qty%candBPUnit*bp+candBPUnit-1)/candBPUnit
	t.ranged, t.currency = true, cur
	t.rangeLo, t.rangeHi = qty-spread, min(qty+spread, candMaxMinor) // spread is at most qty: the tolerance is at most one whole
	return nil
}

func candSearchRequest(tenantID, principalID, correlationID string, n CandidateNode, t candTerms) contracts.CandidatesRequestV1 {
	req := contracts.CandidatesRequestV1{
		SchemaVersion: candSchemaVersion, TenantId: contracts.CommonV1TenantId(tenantID), CorrelationId: contracts.CommonV1CorrelationId(correlationID),
		PrincipalRef: contracts.CommonV1PrincipalId(principalID), ResourceKind: n.ResourceKind,
		Filters: contracts.CandidatesRequestV1Filters{TimeWindow: contracts.CandidatesRequestV1FiltersTimeWindow{
			From: contracts.CommonV1Timestamp(t.from.UTC().Format(time.RFC3339Nano)), To: contracts.CommonV1Timestamp(t.to.UTC().Format(time.RFC3339Nano))}},
	}
	if t.ranged {
		req.Filters.AmountRange = &contracts.CandidatesRequestV1AmountRange{
			MinMinor: contracts.MoneyV1MinorAmount(t.rangeLo), MaxMinor: contracts.MoneyV1MinorAmount(t.rangeHi), Currency: t.currency}
	}
	return req
}

// candCheckAnswer holds the answer to the contract before any of it is used: for this tenant and this correlation, within the pack's raw bound, and each candidate of the contract's
// shape and of the pack's resource kind, none twice. A candidate that fails is a refusal of the whole answer, never a trimmed list. The reader has already scoped its rows to the
// principal; the response has no member that could prove it again, so the request is where the scope is held (REQ-CTX-006).
func candCheckAnswer(r contracts.CandidatesResponseV1, req contracts.CandidatesRequestV1, n CandidateNode) ([]contracts.CandidatesResponseV1Candidate, error) {
	bad := refuseCandidates(ErrBadAnswer, nil)
	if !candVersionShape.MatchString(string(r.SchemaVersion)) || r.TenantId != req.TenantId || r.CorrelationId != req.CorrelationId || len(r.Candidates) > n.MaxRaw {
		return nil, bad
	}
	seen := make(map[string]bool, len(r.Candidates))
	for _, c := range r.Candidates {
		if seen[c.CandidateId] || !candidateOK(c, n.ResourceKind) {
			return nil, bad
		}
		seen[c.CandidateId] = true
	}
	return append([]contracts.CandidatesResponseV1Candidate(nil), r.Candidates...), nil
}

func candidateOK(c contracts.CandidatesResponseV1Candidate, kind string) bool {
	id, found := strings.CutPrefix(string(c.ResourceRef), kind+":")
	var cur contracts.MoneyV1CurrencyCode
	var dir contracts.CandidatesResponseV1CandidateDirection
	return candIDShape.MatchString(c.CandidateId) && found && candRefIDShape.MatchString(id) && c.AmountMinor >= 0 && c.AmountMinor <= candMaxMinor &&
		json.Unmarshal([]byte(strconv.Quote(string(c.Currency))), &cur) == nil && candInstantShape.MatchString(string(c.OccurredAt)) &&
		utf8.RuneCountInString(c.CounterpartyDisplay) >= 1 && utf8.RuneCountInString(c.CounterpartyDisplay) <= candMaxSurfaceRunes && !strings.Contains(c.CounterpartyDisplay, "\n") &&
		candDigitRun(c.CounterpartyDisplay) <= ceilings.MaxLabelDigitRun && candLast4Shape.MatchString(c.AccountLast4) &&
		json.Unmarshal([]byte(strconv.Quote(string(c.Direction))), &dir) == nil
}

// candDigitRun is the longest run of consecutive digits in s.
func candDigitRun(s string) int {
	longest, run := 0, 0
	for _, r := range s {
		if r >= '0' && r <= '9' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	return longest
}

// rankAndLabel scores and labels the candidates through the two ports and checks what they answer. No candidate is scored here, no label made, and none defaulted: a score
// that is missing, repeated, for a stranger or outside 0 to 10000, and a label that is empty, too long a digit run, outside the listed attributes or naming the resource, is a
// refusal of the whole answer.
func (c Candidates) rankAndLabel(ctx context.Context, search CandidateSearch, rows []contracts.CandidatesResponseV1Candidate, n CandidateNode) ([]intake.RankedCandidate, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	scores, err := c.ranker.Rank(ctx, search, append([]contracts.CandidatesResponseV1Candidate(nil), rows...))
	if err != nil {
		return nil, refuseCandidates(ErrCandidatesRank, err)
	}
	byID := make(map[string]int64, len(rows))
	for _, s := range scores {
		if _, dup := byID[s.CandidateID]; dup || s.ScoreBP < 0 || s.ScoreBP > candBPUnit {
			return nil, refuseCandidates(ErrBadAnswer, nil)
		}
		byID[s.CandidateID] = s.ScoreBP
	}
	if len(byID) != len(rows) {
		return nil, refuseCandidates(ErrBadAnswer, nil)
	}
	askable := make(map[string]bool, len(n.Disambiguation.AttributeOrder))
	for _, a := range n.Disambiguation.AttributeOrder {
		askable[a.Name] = true
	}
	out := make([]intake.RankedCandidate, 0, len(rows))
	for _, row := range rows {
		score, ok := byID[row.CandidateId]
		if !ok {
			return nil, refuseCandidates(ErrBadAnswer, nil)
		}
		l, err := c.labeller.Label(row)
		if err != nil {
			return nil, refuseCandidates(ErrCandidatesRank, err)
		}
		attrs, ok := candCheckLabel(l, row, n.Disambiguation.MaxDigitRun, askable)
		if !ok {
			return nil, refuseCandidates(ErrBadAnswer, nil)
		}
		out = append(out, intake.RankedCandidate{Candidate: decide.Candidate{ID: row.CandidateId, Display: l.Display, Attrs: attrs}, Score: score})
	}
	return out, nil
}

// candCheckLabel holds a label to what REQ-AMB-004 and AGENT-13 ask of it, and returns its own copy of the attributes.
func candCheckLabel(l CandidateLabel, row contracts.CandidatesResponseV1Candidate, maxRun int, askable map[string]bool) (map[string]string, bool) {
	_, resourceID, _ := strings.Cut(string(row.ResourceRef), ":")
	shows := func(s string) bool {
		return candDigitRun(s) <= maxRun && !strings.Contains(s, resourceID) && !strings.Contains(s, row.CandidateId)
	}
	if strings.TrimSpace(l.Display) == "" || !shows(l.Display) {
		return nil, false
	}
	attrs := make(map[string]string, len(l.Attrs))
	for k, v := range l.Attrs {
		if !askable[k] || !shows(v) {
			return nil, false
		}
		attrs[k] = v
	}
	return attrs, true
}
