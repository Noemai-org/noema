// Package verify checks a webhook delivery against the Standard Webhooks signature scheme
// (https://www.standardwebhooks.com): the headers webhook-id, webhook-timestamp and webhook-signature; the signed
// content "<id>.<timestamp>.<body>"; HMAC-SHA-256 with the shared secret; a signature header that carries one or
// more space-separated "v1,<base64>" entries; and a timestamp within five minutes of now, in either direction.
// Only version v1 is accepted. The relay holds nothing else (DEC-2026-10-16 E6, REQ-API-008).
package verify

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Tolerance is the five-minute window of DEC-2026-10-16 E6.
const Tolerance = 5 * time.Minute

// Reason says why a delivery was refused. The names are the counters of the relay.
type Reason string

const (
	MissingHeader Reason = "missing_header"
	BadID         Reason = "bad_id"
	BadTimestamp  Reason = "bad_timestamp"
	StaleTime     Reason = "timestamp_too_old"
	FutureTime    Reason = "timestamp_in_the_future"
	BadSignature  Reason = "bad_signature"
)

// Error is a refusal with its reason.
type Error struct{ Reason Reason }

func (e *Error) Error() string { return "delivery refused: " + string(e.Reason) }

var idRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// Verifier holds the signing key.
type Verifier struct{ key []byte }

// ParseSecret reads a signing secret: "whsec_" followed by standard base64, as Standard Webhooks writes it, or else
// the raw bytes. It must decode to at least 24 bytes.
func ParseSecret(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	key := []byte(s)
	if rest, ok := strings.CutPrefix(s, "whsec_"); ok {
		b, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return nil, errors.New("the signing secret starts with whsec_ but is not base64")
		}
		key = b
	}
	if len(key) < 24 {
		return nil, errors.New("the signing secret is too short: at least 24 bytes")
	}
	return key, nil
}

// New returns a Verifier for a parsed key.
func New(key []byte) *Verifier { return &Verifier{key: append([]byte(nil), key...)} }

func (v *Verifier) mac(id, timestamp string, body []byte) []byte {
	h := hmac.New(sha256.New, v.key)
	h.Write([]byte(id + "." + timestamp + "."))
	h.Write(body)
	return h.Sum(nil)
}

// Sign returns the "v1,<base64>" signature of a delivery. The relay never signs; tests and the sender side do.
func (v *Verifier) Sign(id string, timestamp int64, body []byte) string {
	return "v1," + base64.StdEncoding.EncodeToString(v.mac(id, strconv.FormatInt(timestamp, 10), body))
}

// Delivery is a verified delivery.
type Delivery struct {
	ID        string
	Timestamp int64
}

// Check verifies one delivery at time now. The body is the exact bytes received.
func (v *Verifier) Check(h http.Header, body []byte, now time.Time) (Delivery, error) {
	id, ts, sig := h.Get("webhook-id"), h.Get("webhook-timestamp"), h.Get("webhook-signature")
	if id == "" || ts == "" || sig == "" {
		return Delivery{}, &Error{MissingHeader}
	}
	if !idRE.MatchString(id) {
		return Delivery{}, &Error{BadID}
	}
	if len(ts) > 12 {
		return Delivery{}, &Error{BadTimestamp}
	}
	secs, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || secs < 0 || strconv.FormatInt(secs, 10) != ts {
		return Delivery{}, &Error{BadTimestamp}
	}
	sent := time.Unix(secs, 0)
	switch {
	case now.Sub(sent) > Tolerance:
		return Delivery{}, &Error{StaleTime}
	case sent.Sub(now) > Tolerance:
		return Delivery{}, &Error{FutureTime}
	}
	want := v.mac(id, ts, body)
	ok := 0
	entries := strings.Fields(sig)
	if len(entries) > 8 {
		entries = entries[:8] // a key rotation sends two; more is not a rotation
	}
	for _, e := range entries {
		version, b64, found := strings.Cut(e, ",")
		if !found || version != "v1" {
			continue
		}
		got, err := base64.StdEncoding.DecodeString(b64)
		if err != nil || len(got) != sha256.Size {
			continue
		}
		ok |= subtle.ConstantTimeCompare(got, want) // every entry is compared: the time does not say which matched
	}
	if ok != 1 {
		return Delivery{}, &Error{BadSignature}
	}
	return Delivery{ID: id, Timestamp: secs}, nil
}

// PullToken derives the credential the core uses to pull verified deliveries. The relay holds one secret only, so
// the pull credential is derived from it: the core holds the derived token, which does not reveal the secret.
func PullToken(key []byte) string {
	h := hmac.New(sha256.New, key)
	h.Write([]byte("noema/webhook-relay/pull/v1"))
	return fmt.Sprintf("%x", h.Sum(nil))
}
