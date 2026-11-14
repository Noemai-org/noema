package understanding

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

const prefix = "understanding: "

// Reason says why the gate refused an Intent Object. Reasons are a closed list; a reason never carries the object's text (AGENT-13).
type Reason string

// What the model service's own gate writes where the model has nothing to say (noema#489). Ruling A166, item 3: principal_type and asserted_by are the session's, so the
// model service fills the two members the contract requires with unsetMarker and the core writes the session's values over them. Item 1: the model service never signs,
// so `sig` holds unsignedMarker, which the core holds as it is.
const (
	unsetMarker    = "unset"
	unsignedMarker = "unsigned"
)

// Every reason the gate gives.
const (
	ReasonSize               Reason = "SIZE"
	ReasonNotJSON            Reason = "NOT_JSON"
	ReasonShape              Reason = "SHAPE" // an unknown or repeated member, a missing or null member, a wrong type (the contract's closed shape)
	ReasonStatus             Reason = "STATUS_NOT_VALIDATED"
	ReasonSchemaVersion      Reason = "SCHEMA_VERSION"
	ReasonIntentID           Reason = "INTENT_ID"
	ReasonVersion            Reason = "VERSION"
	ReasonSupersedes         Reason = "SUPERSEDES"
	ReasonCreatedAt          Reason = "CREATED_AT"
	ReasonCorrelation        Reason = "CORRELATION_MISMATCH"
	ReasonModeNotEnabled     Reason = "MODE_NOT_ENABLED"
	ReasonSource             Reason = "SOURCE"
	ReasonPackMismatch       Reason = "PACK_MISMATCH"
	ReasonUnknownNode        Reason = "UNKNOWN_NODE"
	ReasonSlotName           Reason = "SLOT_NAME"
	ReasonUnknownSlot        Reason = "UNKNOWN_SLOT"
	ReasonSlotType           Reason = "SLOT_TYPE"
	ReasonSlotInvalid        Reason = "SLOT_INVALID"
	ReasonAmbiguityFlag      Reason = "AMBIGUITY_FLAG"
	ReasonOutcomeNotInNode   Reason = "OUTCOME_NOT_IN_NODE"
	ReasonSig                Reason = "SIG"
	ReasonHashUnavailable    Reason = "HASH_UNAVAILABLE" // the object holds a number the intent hash cannot cover (see ErrHashUnavailable)
	ReasonIdentityNotInOrder Reason = "SESSION_INVALID"  // the session the caller passed is incomplete: a bug of the caller, never the model's
)

// Rejection is the error of a refused Intent Object: INTENT_REJECTED_AT_GATE with its reasons (REQ-INT-001 audit). The reasons are sorted and unique.
type Rejection struct{ Reasons []Reason }

func (r *Rejection) Error() string {
	parts := make([]string, len(r.Reasons))
	for i, x := range r.Reasons {
		parts[i] = string(x)
	}
	return prefix + "the Intent Object is refused at the gate: " + strings.Join(parts, ", ")
}

// ErrHashUnavailable is returned inside a Rejection (reason HASH_UNAVAILABLE) when an Intent Object carries a number that is not an integer, for example a
// slot confidence of 0.91. The core's RFC 8785 form refuses every non-integer number so that no float reaches a hash (platform/crypto), and the Intent
// Object's `confidence` and `asr_confidence` are ratios in [0, 1]. Which form the intent hash covers is an open conflict (see the PR); until it is decided the
// gate fails closed and admits no such object.
var ErrHashUnavailable = errors.New(prefix + "the intent hash cannot cover a non-integer number")

var (
	sessionPrincipalRe = regexp.MustCompile(`^usr_[0-9A-Za-z]{2,32}$`)
	sessionTenantRe    = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}$`)
	sessionAuthnRe     = regexp.MustCompile(`^ses_[0-9A-Za-z]{2,32}$`)
	lowerSnakeRe       = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
	assertedByRe       = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`) // the contract's asserted_by pattern since ruling A163: the POC's own provider is noema-poc-idp
	intentIDRe         = regexp.MustCompile(`^int_[0-9A-Za-z]{2,32}$`)
	intentRefRe        = regexp.MustCompile(`^(int_[0-9A-Za-z]{2,32})@([0-9]+)$`)
	correlationRe      = regexp.MustCompile(`^corr_[0-9A-Za-z]{2,32}$`)
	timestampRe        = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]{1,9})?Z$`)
	localeRe           = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	packIDRe           = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)
	verticalRe         = regexp.MustCompile(`^[a-z][a-z0-9]*$`)
	semverRe           = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+$`)
	flagRe             = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
	resourceRefRe      = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*:[A-Za-z0-9_.-]{1,64}$`)
	candidateSetRe     = regexp.MustCompile(`^cns_[0-9A-Za-z]{2,32}$`)
	packUnitRe         = regexp.MustCompile(`^[a-z][a-z0-9]*\.unit\.[a-z][a-z0-9_]*$`)
)

const maxSafeInt = 9007199254740991

// Session is the authenticated caller as the edge established it (INV-12). Every field is required; the gate refuses to run on an incomplete session.
type Session struct {
	PrincipalID   string // usr_…
	TenantID      string // ten_…
	AuthnRef      string // ses_…, the session's identifier
	PrincipalType string // for example end_user
	AssertedBy    string // for example enterprise_idp
}

// Valid reports whether every field of the session has its shape: the one rule Gate.Admit applies first and refuses on (SESSION_INVALID). A caller that holds a
// session uses it to refuse a malformed one before it calls anything else, so the user's input is not sent anywhere for a caller that should already be refused.
func (s Session) Valid() bool { return s.valid() }

func (s Session) valid() bool {
	return sessionPrincipalRe.MatchString(s.PrincipalID) && sessionTenantRe.MatchString(s.TenantID) && sessionAuthnRe.MatchString(s.AuthnRef) &&
		lowerSnakeRe.MatchString(s.PrincipalType) && assertedByRe.MatchString(s.AssertedBy)
}

// Node is what the pack says about one intent node: its slots by name and type, and its outcome definitions.
type Node struct {
	Slots    map[string]string // slot name to slot type (Resource, Quantity, Party, TimeRef or Reference)
	Outcomes map[string]bool   // the node's outcome definition ids
}

// PackView is the loaded pack as the gate needs it. The caller adapts the pack it loaded; the gate reads no file.
type PackView interface {
	Ref() (packID, packVersion string)
	Node(id string) (Node, bool)
}

// Gate is the second schema gate. Build it once with NewGate.
type Gate struct {
	maxBytes int
	pack     PackView
}

// NewGate needs a positive size bound from configuration (AGENT-16: no literal here) and the loaded pack.
func NewGate(maxBytes int, pack PackView) (*Gate, error) {
	if maxBytes <= 0 || pack == nil {
		return nil, errors.New(prefix + "the gate needs a positive size bound and a pack")
	}
	if id, v := pack.Ref(); !verticalRe.MatchString(id) || !semverRe.MatchString(v) {
		return nil, errors.New(prefix + "the pack reference is malformed")
	}
	return &Gate{maxBytes: maxBytes, pack: pack}, nil
}

// AdmittedIntent is an Intent Object that passed the gate, with the session's identity written over whatever the model service returned and its intent hash.
// Only Admit makes one (the fields are unexported), so a type other than AdmittedIntent cannot reach Trust (REQ-INT-001 acceptance).
type AdmittedIntent struct {
	obj  contracts.IntentObjectV1
	hash string
	raw  []byte
}

// Object is a copy of the admitted object.
func (a AdmittedIntent) Object() contracts.IntentObjectV1 { return a.obj }

// Hash is "sha256:" and the SHA-256 of the object's canonical form (RFC 8785), after the identity overwrite.
func (a AdmittedIntent) Hash() string { return a.hash }

// JSON is the canonical form the hash was taken over.
func (a AdmittedIntent) JSON() []byte { return append([]byte(nil), a.raw...) }

// Admission is the result of Admit: the intent and the names of the fields the model service had set differently from the session. Names only, never values:
// the caller records SECURITY_EVENT PRINCIPAL_OVERRIDE_ATTEMPT when Overrides is not empty (REQ-INP-002).
type Admission struct {
	Intent    AdmittedIntent
	Overrides []string
}

// Admit runs the gate on the bytes the model service returned. The session and the correlation id are the edge's. On any failure the error is a *Rejection and
// nothing is admitted; Admit never repairs a field (REQ-INT-002).
func (g *Gate) Admit(raw []byte, s Session, correlationID string) (Admission, error) {
	if !s.valid() || !correlationRe.MatchString(correlationID) {
		return Admission{}, &Rejection{Reasons: []Reason{ReasonIdentityNotInOrder}}
	}
	var reasons reasonSet
	if len(raw) == 0 || len(raw) > g.maxBytes {
		return Admission{}, &Rejection{Reasons: []Reason{ReasonSize}}
	}
	if err := wholeDocument(raw); err != nil {
		return Admission{}, &Rejection{Reasons: []Reason{ReasonNotJSON}}
	}
	var o contracts.IntentObjectV1
	if err := json.Unmarshal(raw, &o); err != nil {
		return Admission{}, &Rejection{Reasons: []Reason{ReasonShape}} // an unknown, repeated, missing or null member, or a wrong type
	}
	g.check(&o, correlationID, &reasons)
	if !reasons.empty() {
		return Admission{}, reasons.rejection()
	}
	overrides := overwrite(&o, s)
	canon, err := canonical(o)
	if err != nil {
		return Admission{}, &Rejection{Reasons: []Reason{ReasonHashUnavailable}}
	}
	return Admission{Intent: AdmittedIntent{obj: o, hash: hashOf(canon), raw: canon}, Overrides: overrides}, nil
}

// wholeDocument says that raw is exactly one JSON value with nothing after it.
func wholeDocument(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var v json.RawMessage
	if err := dec.Decode(&v); err != nil {
		return err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data")
	}
	return nil
}

type reasonSet map[Reason]bool

func (r *reasonSet) add(x Reason) {
	if *r == nil {
		*r = reasonSet{}
	}
	(*r)[x] = true
}
func (r reasonSet) empty() bool { return len(r) == 0 }
func (r reasonSet) rejection() *Rejection {
	out := make([]Reason, 0, len(r))
	for x := range r {
		out = append(out, x)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return &Rejection{Reasons: out}
}

func (g *Gate) check(o *contracts.IntentObjectV1, correlationID string, r *reasonSet) {
	if o.Status != contracts.EnumsV1IntentStatusVALIDATED {
		r.add(ReasonStatus)
	}
	if string(o.SchemaVersion) != "1.0" {
		r.add(ReasonSchemaVersion)
	}
	if !intentIDRe.MatchString(string(o.IntentId)) {
		r.add(ReasonIntentID)
	}
	version := int64(o.Version)
	if version < 1 || version > maxSafeInt {
		r.add(ReasonVersion)
	}
	switch {
	case version == 1 && o.Supersedes != nil:
		r.add(ReasonSupersedes)
	case version > 1:
		m := (*string)(nil)
		if o.Supersedes != nil {
			s := string(*o.Supersedes)
			m = &s
		}
		if m == nil {
			r.add(ReasonSupersedes)
			break
		}
		sm := intentRefRe.FindStringSubmatch(*m)
		prev, perr := strconv.ParseInt(func() string {
			if len(sm) == 3 {
				return sm[2]
			}
			return "x"
		}(), 10, 64)
		if sm == nil || sm[1] != string(o.IntentId) || perr != nil || prev != version-1 {
			r.add(ReasonSupersedes)
		}
	}
	if !timestampRe.MatchString(string(o.CreatedAt)) {
		r.add(ReasonCreatedAt)
	} else if _, err := time.Parse(time.RFC3339Nano, string(o.CreatedAt)); err != nil {
		r.add(ReasonCreatedAt)
	}
	if string(o.CorrelationId) != correlationID {
		r.add(ReasonCorrelation)
	}
	g.checkSource(o, r)
	packID, packVersion := g.pack.Ref()
	if string(o.Pack.PackId) != packID || string(o.Pack.PackVersion) != packVersion {
		r.add(ReasonPackMismatch)
	}
	// Ruling A166 item 1: the model service never signs. Its own gate leaves the neutral marker; anything else is a signature the model made.
	if o.Sig.KeyId != unsignedMarker || o.Sig.Value != unsignedMarker {
		r.add(ReasonSig)
	}
	// Ruling A166 item 3: the model's output carries no principal_type and no asserted_by. The contract requires the members, so the model service fills them with the
	// marker; a model that sends a value anyway, even the session's own, is a shape error.
	if o.Principal.PrincipalType != unsetMarker || o.Principal.AssertedBy != unsetMarker {
		r.add(ReasonShape)
	}
	for _, f := range o.AmbiguityFlags {
		if !flagRe.MatchString(f) {
			r.add(ReasonAmbiguityFlag)
		}
	}
	if dup(o.AmbiguityFlags) {
		r.add(ReasonAmbiguityFlag)
	}
	node, known := Node{}, false
	if packIDRe.MatchString(string(o.IntentNode)) {
		node, known = g.pack.Node(string(o.IntentNode))
	}
	if !known {
		r.add(ReasonUnknownNode)
	}
	g.checkSlots(o, node, known, r)
	seen := map[string]bool{}
	for _, ref := range o.DesiredOutcomeRefs {
		id := string(ref)
		if seen[id] || !packIDRe.MatchString(id) || (known && !node.Outcomes[id]) {
			r.add(ReasonOutcomeNotInNode)
		}
		seen[id] = true
	}
}

func dup(xs []string) bool {
	seen := map[string]bool{}
	for _, x := range xs {
		if seen[x] {
			return true
		}
		seen[x] = true
	}
	return false
}

func (g *Gate) checkSource(o *contracts.IntentObjectV1, r *reasonSet) {
	if o.Source.Mode != contracts.EnumsV1ModeMODE1 {
		if o.Source.Mode == contracts.EnumsV1ModeMODE2 {
			r.add(ReasonModeNotEnabled)
		} else {
			r.add(ReasonSource)
		}
	}
	if ch := string(o.Source.Channel); ch != "text" && ch != "voice" {
		r.add(ReasonSource)
	}
	for _, l := range o.Source.LocaleDetected {
		if !localeRe.MatchString(l) {
			r.add(ReasonSource)
		}
	}
	if o.Source.RawRef == "" || len(o.Source.RawRef) > 256 {
		r.add(ReasonSource)
	}
	if o.Source.AsrConfidence != nil && !ratio(string(*o.Source.AsrConfidence)) {
		r.add(ReasonSource)
	}
}

func ratio(n string) bool {
	f, err := strconv.ParseFloat(n, 64)
	return err == nil && f >= 0 && f <= 1
}
