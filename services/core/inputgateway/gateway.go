package inputgateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	prefix     = "input gateway: "
	minHashKey = 32
)

// The errors of Accept. Callers compare with errors.Is. None of them carries the text of an input.
var (
	// ErrInvalidInput is a body the gateway refuses: not one JSON object, an unknown, repeated or null member, a field off its shape, or longer than the bound.
	ErrInvalidInput = errors.New(prefix + "the input is not valid")
	// ErrNoIdentity is an identity the caller did not complete: a bug of the caller (the session mapping), never of the client.
	ErrNoIdentity = errors.New(prefix + "the session identity is incomplete")
	// ErrRecord means the Record could not be read or appended to: the input is refused and nothing proceeds (EC-E16).
	ErrRecord = errors.New(prefix + "the Record is not available")
	// ErrRequestIDReused is the same request_id with another body (REQ-INP-003): 409, and SECURITY_EVENT REQUEST_ID_REUSED was appended.
	ErrRequestIDReused = errors.New(prefix + "the request id was used before with another body")
	// ErrStart means the input was recorded but the orchestrator could not take the turn.
	ErrStart = errors.New(prefix + "the turn could not be started")
)

// Identity is the authenticated session, as the edge established it (INV-12). It is never built from a request body, a query or a header.
type Identity struct {
	TenantID      string
	PrincipalID   string // the session subject (PERD 32.1 id rule)
	PrincipalKind string // the principal kind of the session's role, for example "customer" (PERD 32.1)
	SessionID     string
}

// Ref names a recorded event: what the orchestrator is handed with the turn and what a retry gets back.
type Ref struct {
	EventID string
	Seq     int64
	Hash    string
}

// Turn is what the orchestrator is handed (A161 item 3): the identity, the correlation of the investigation, the validated fields of the input, the body as it
// came (for the model service's gateway, which normalises and bounds the text), and the reference of the INPUT_RECEIVED event that is already in the Record.
type Turn struct {
	Identity
	CorrelationID   string
	RequestID       string
	Channel         string
	Locale          string
	ConversationRef string
	Text            string
	Body            []byte
	Input           Ref
}

// Result is the answer to the caller. For a retry it is the original one: the reference of the first INPUT_RECEIVED and its correlation id (the stored
// response reference of REQ-INP-003).
type Result struct {
	CorrelationID string
	Input         Ref
	Deduplicated  bool
}

// Received is the payload of INPUT_RECEIVED: channel, locale and request_id exactly as the reconstruction reads them (reconstruct/project.go), plus the hash of
// the body so that a redelivery can be told from a reuse. The text is never in it.
type Received struct {
	RequestID string
	Channel   string
	Locale    string // empty when the client sent no locale_hint; the key is then left out
	InputHash string
}

// Prior is an earlier INPUT_RECEIVED of the same tenant and principal for the same request_id, within the window.
type Prior struct {
	Ref           Ref
	CorrelationID string
	InputHash     string
}

// Recorder appends the three events of the gateway, as core-edge, for the identity's tenant. An error means nothing was appended.
type Recorder interface {
	Received(ctx context.Context, id Identity, correlationID string, in Received) (Ref, error)
	Deduplicated(ctx context.Context, id Identity, correlationID, requestID string, original Ref) error
	Reused(ctx context.Context, id Identity, correlationID, requestID, inputHash string, original Ref) error
}

// Index finds the earlier input of this tenant and principal for a request_id within the window. An error means it could not tell; the gateway then refuses.
type Index interface {
	Find(ctx context.Context, id Identity, requestID string) (Prior, bool, error)
}

// Config is what New needs. Nothing has a default (AGENT-16): the body bound and the window come from configuration.
type Config struct {
	Recorder Recorder
	Index    Index
	Start    func(ctx context.Context, t Turn) error // the orchestrator seam; called only after the input is in the Record, on every delivery of it (A168), so it must be idempotent per Turn.Input
	IDs      chain.IDSource
	MaxBody  int64
	// HashKey is the core's secret for the hash the Record keeps of an input (REQ-SEC-003: no derivative of raw text a reader of the Record could confirm a guess
	// against). At least 32 bytes, from configuration (file-mounted, PLT-05), never defaulted. Each tenant's hash key is derived from it. Rotating it makes a
	// retry of an older input look new (the window is 24 hours), so a rotation waits for a quiet moment.
	HashKey []byte
}

// Gateway is the input gateway of the core.
type Gateway struct {
	rec     Recorder
	idx     Index
	start   func(ctx context.Context, t Turn) error
	ids     chain.IDSource
	maxBody int64
	key     []byte
	locks   keyedLock
}

// New refuses a configuration with a missing part.
func New(c Config) (*Gateway, error) {
	if c.Recorder == nil || c.Index == nil || c.Start == nil || c.IDs == nil || c.MaxBody <= 0 {
		return nil, errors.New(prefix + "a recorder, an index, the orchestrator seam, an ID source and a positive body bound are all required")
	}
	if len(c.HashKey) < minHashKey {
		return nil, errors.New(prefix + "a hash key of at least 32 bytes is required")
	}
	return &Gateway{rec: c.Recorder, idx: c.Index, start: c.Start, ids: c.IDs, maxBody: c.MaxBody, key: append([]byte(nil), c.HashKey...)}, nil
}

var (
	principalID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
	kindRe      = regexp.MustCompile(`^[a-z]+$`)
	sessionRe   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)
	requestRe   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	localeRe    = regexp.MustCompile(`^[a-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)
	refRe       = regexp.MustCompile(`^[a-z]{2,6}_[0-9A-Za-z_]{2,64}$`)
)

func validIdentity(id Identity) bool {
	return chain.ValidTenantID(id.TenantID) && principalID.MatchString(id.PrincipalID) && kindRe.MatchString(id.PrincipalKind) &&
		sessionRe.MatchString(id.SessionID)
}

// Accept handles one input. Order: identity, body, the lookup of an earlier input, then one append (INPUT_RECEIVED, INPUT_DEDUPLICATED or the security event),
// and only after the input is in the Record, the turn goes to the orchestrator: on the first delivery and, by ruling A168, on every retry of the same body (a retry
// of a stranded input must not be answered as accepted while nothing runs). Every failure before the append leaves the Record as it was.
func (g *Gateway) Accept(ctx context.Context, id Identity, raw []byte) (Result, error) {
	if !validIdentity(id) {
		return Result{}, ErrNoIdentity
	}
	in, err := parseBody(raw, g.maxBody)
	if err == nil {
		in.hash = inputHash(g.key, id.TenantID, in.canon)
	}
	if err != nil {
		return Result{}, err
	}
	unlock := g.locks.lock(id.TenantID + "|" + id.PrincipalID + "|" + in.requestID)
	locked := true
	defer func() {
		if locked {
			unlock()
		}
	}()
	prior, found, err := g.idx.Find(ctx, id, in.requestID)
	if err != nil {
		return Result{}, fmt.Errorf("%w: cannot tell whether the request id was used before: %v", ErrRecord, err)
	}
	if found {
		if prior.InputHash == in.hash {
			if err := g.rec.Deduplicated(ctx, id, prior.CorrelationID, in.requestID, prior.Ref); err != nil {
				return Result{}, fmt.Errorf("%w: %v", ErrRecord, err)
			}
			unlock()
			locked = false
			// A168 (Codex P1 on noema#466): a retry of a recorded input is not an acknowledgement that a turn runs. The first Start may have failed after INPUT_RECEIVED
			// was stored, and nothing else would ever start it. So the retry calls Start again for the same recorded input; Start is idempotent per recorded input (the
			// investigation store of noema#490), so a turn that did start is found, not run twice. If this Start fails too, the client is told so.
			if err := g.start(ctx, turnOf(id, prior.CorrelationID, in, prior.Ref)); err != nil {
				return Result{}, fmt.Errorf("%w: %v", ErrStart, err)
			}
			return Result{CorrelationID: prior.CorrelationID, Input: prior.Ref, Deduplicated: true}, nil
		}
		// Record first: the 409 tells the client that the reuse was recorded, so it is sent only once the SECURITY_EVENT is in the Record. If that append fails the
		// answer is the Record's failure (503), never the 409 (review of noema#466, note 3).
		if err := g.rec.Reused(ctx, id, prior.CorrelationID, in.requestID, in.hash, prior.Ref); err != nil {
			return Result{}, fmt.Errorf("%w: the security event was not appended: %v", ErrRecord, err)
		}
		return Result{}, ErrRequestIDReused
	}
	corr, err := g.ids.ID("corr")
	if err != nil {
		return Result{}, fmt.Errorf("%w: correlation id: %v", ErrRecord, err)
	}
	ref, err := g.rec.Received(ctx, id, corr, Received{RequestID: in.requestID, Channel: in.channel, Locale: in.locale, InputHash: in.hash})
	if err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrRecord, err)
	}
	unlock() // a redelivery from here on finds the event in the Record; the turn can take as long as it takes
	locked = false
	if err := g.start(ctx, turnOf(id, corr, in, ref)); err != nil {
		return Result{}, fmt.Errorf("%w: %v", ErrStart, err)
	}
	return Result{CorrelationID: corr, Input: ref}, nil
}

// turnOf is the turn of a recorded input: the same for the first delivery and for every retry (the retry's body has the same hash, so the same content). Its Body is
// the canonical body without voice.audio_ref: no reference to retained audio is handed on (CR-2026-11-08-VOI-canonical-input-audio-ref, A1).
func turnOf(id Identity, corr string, in input, ref Ref) Turn {
	return Turn{Identity: id, CorrelationID: corr, RequestID: in.requestID, Channel: in.channel, Locale: in.locale, ConversationRef: in.conversationRef, Text: in.text,
		Body: append([]byte(nil), in.canon...), Input: ref}
}

// input is a body that passed the closed-shape check. canon is its canonical form (members sorted, no insignificant whitespace, numbers as written) without
// voice.audio_ref; hash is the keyed hash of canon that the Record keeps (set by Accept, which knows the tenant).
type input struct {
	requestID, channel, locale, conversationRef, text, hash string
	canon                                                   []byte
}

var allowed = map[string]bool{"request_id": true, "channel": true, "text": true, "locale_hint": true, "voice": true, "conversation_ref": true}

// parseBody reads exactly one JSON object with only the members a client may send (REQ-INP-001), none repeated and none null, and hashes its canonical form
// (members sorted, whitespace dropped, numbers as written) so that a redelivery with other whitespace or member order is the same body. A voice member is read: its
// audio_ref must be null or absent, because no retention path exists (CR-2026-11-08-VOI-canonical-input-audio-ref, A1), and it is dropped from what is hashed and
// handed on. An error never quotes the body.
func parseBody(raw []byte, max int64) (input, error) {
	bad := func(why string) (input, error) { return input{}, fmt.Errorf("%w: %s", ErrInvalidInput, why) }
	if int64(len(raw)) > max {
		return bad("the body is longer than the bound")
	}
	if !utf8.Valid(raw) {
		return bad("the body is not UTF-8")
	}
	members, err := members(raw)
	if err != nil {
		return bad(err.Error())
	}
	var in input
	get := func(name string, dst *string, required bool, re *regexp.Regexp) error {
		v, ok := members[name]
		if !ok {
			if required {
				return fmt.Errorf("%s is missing", name)
			}
			return nil
		}
		if err := json.Unmarshal(v, dst); err != nil {
			return fmt.Errorf("%s is not a string", name)
		}
		if re != nil && !re.MatchString(*dst) {
			return fmt.Errorf("%s is off its shape", name)
		}
		return nil
	}
	for _, step := range []error{
		get("request_id", &in.requestID, true, requestRe),
		get("channel", &in.channel, true, nil),
		get("text", &in.text, true, nil),
		get("locale_hint", &in.locale, false, localeRe),
		get("conversation_ref", &in.conversationRef, false, refRe),
	} {
		if step != nil {
			return bad(step.Error())
		}
	}
	if in.channel != "text" && in.channel != "voice" {
		return bad("channel is not text or voice")
	}
	if strings.TrimSpace(in.text) == "" {
		return bad("text is empty")
	}
	if v, ok := members["voice"]; ok {
		clean, err := withoutAudioRef(v)
		if err != nil {
			return bad(err.Error())
		}
		members["voice"] = clean
	}
	canon, err := canonicalBody(members)
	if err != nil {
		return bad("the body cannot be made canonical")
	}
	in.canon = canon
	return in, nil
}

// members decodes the body into its top-level members. It refuses anything but one object, a repeated member, an unknown member and a null member.
func members(raw []byte) (map[string]json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("the body is not one JSON object")
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, errors.New("the body is not valid JSON")
		}
		key, _ := k.(string)
		if !allowed[key] {
			return nil, errors.New("the body has a member a client may not send")
		}
		if _, dup := out[key]; dup {
			return nil, errors.New("the body repeats a member")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, errors.New("the body is not valid JSON")
		}
		if bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return nil, errors.New("the body has a null member")
		}
		out[key] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errors.New("the body is not valid JSON")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("the body has data after the object")
	}
	return out, nil
}

// withoutAudioRef reads the voice object (one object, no repeated member) and returns it without audio_ref. A non-null audio_ref is refused with one fixed
// sentence whatever it holds: the check reads nothing else, so a reference that exists in another tenant's Record and one that exists nowhere get the same answer.
func withoutAudioRef(raw json.RawMessage) (json.RawMessage, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return nil, errors.New("voice is not an object")
	}
	out := map[string]json.RawMessage{}
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, errors.New("voice is not valid JSON")
		}
		key, _ := k.(string)
		if _, dup := out[key]; dup {
			return nil, errors.New("voice repeats a member")
		}
		var v json.RawMessage
		if err := dec.Decode(&v); err != nil {
			return nil, errors.New("voice is not valid JSON")
		}
		out[key] = v
	}
	if t, err := dec.Token(); err != nil || t != json.Delim('}') {
		return nil, errors.New("voice is not valid JSON")
	}
	if ref, ok := out["audio_ref"]; ok {
		if !bytes.Equal(bytes.TrimSpace(ref), []byte("null")) {
			return nil, errors.New("voice.audio_ref is not null: no audio is retained")
		}
		delete(out, "audio_ref")
	}
	b, err := json.Marshal(out)
	if err != nil {
		return nil, errors.New("voice cannot be made canonical")
	}
	return b, nil
}

// canonicalBody is the members with sorted names, nested objects sorted too, no insignificant whitespace, numbers as written.
func canonicalBody(members map[string]json.RawMessage) ([]byte, error) {
	doc := make(map[string]any, len(members))
	for k, v := range members {
		dec := json.NewDecoder(bytes.NewReader(v))
		dec.UseNumber()
		var x any
		if err := dec.Decode(&x); err != nil {
			return nil, err
		}
		doc[k] = x
	}
	return json.Marshal(doc) // encoding/json writes map keys in sorted order at every level
}

// inputHash is what the Record keeps in place of the body (REQ-SEC-003): "hmac-sha256:" and the HMAC-SHA-256 of the canonical body under a key derived from the core's
// key and the tenant. It tells a redelivery from a reuse, and nobody who reads the Record can confirm a guess at the text against it, or tell one tenant's input from
// another's.
func inputHash(key []byte, tenantID string, canon []byte) string {
	d := hmac.New(sha256.New, key)
	d.Write([]byte("noema/input-hash/v1|" + tenantID))
	m := hmac.New(sha256.New, d.Sum(nil))
	m.Write(canon)
	return "hmac-sha256:" + hex.EncodeToString(m.Sum(nil))
}

// keyedLock serialises the work of one key and holds nothing for a key nobody waits on.
type keyedLock struct {
	mu sync.Mutex
	m  map[string]*lockEntry
}

type lockEntry struct {
	mu sync.Mutex
	n  int
}

func (k *keyedLock) lock(key string) (unlock func()) {
	k.mu.Lock()
	if k.m == nil {
		k.m = map[string]*lockEntry{}
	}
	e := k.m[key]
	if e == nil {
		e = &lockEntry{}
		k.m[key] = e
	}
	e.n++
	k.mu.Unlock()
	e.mu.Lock()
	return func() {
		e.mu.Unlock()
		k.mu.Lock()
		if e.n--; e.n == 0 {
			delete(k.m, key)
		}
		k.mu.Unlock()
	}
}
