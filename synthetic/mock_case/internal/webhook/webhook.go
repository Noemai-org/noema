// Package webhook delivers a webhook from mock-case to the relay, signed with mock-case's own SYNTHETIC key (A682).
//
// The scheme is Standard Webhooks v1, which Tushe's response of 7 Oct 2026 (section 4.7) names: headers webhook-id, webhook-timestamp and webhook-signature; HMAC-SHA-256 over
// "<id>.<timestamp>.<body>" with the raw body bytes; a timestamp the receiver accepts within five minutes; and the same webhook-id on a replay (delta v1.3 section 1). Nothing else of Tushe's
// delivery is imitated: its body members, event names, retry ladder and timeout are not stated in any governing document, so none is built here (see status/ENV-05.md). The key is mock-case's
// own and is not Tushe's. Everything is SYNTHETIC (AGENT-25).
package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// MaxBodyBytes bounds a delivery body. It is mock-case's own bound, not a number from Tushe.
const MaxBodyBytes = 64 * 1024

// DefaultTimeout bounds one delivery attempt when the caller gives none.
const DefaultTimeout = 15 * time.Second

var idRE = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,128}$`)

// Signer signs deliveries with one secret. It never prints or returns the secret.
type Signer struct{ key []byte }

// NewSigner reads a signing secret: "whsec_" and standard base64 as Standard Webhooks writes it, or else the raw bytes. At least 24 bytes. An error never repeats the secret.
func NewSigner(secret string) (*Signer, error) {
	s := strings.TrimSpace(secret)
	key := []byte(s)
	if rest, ok := strings.CutPrefix(s, "whsec_"); ok {
		b, err := base64.StdEncoding.DecodeString(rest)
		if err != nil {
			return nil, errors.New("webhook: the signing secret starts with whsec_ but is not base64")
		}
		key = b
	}
	if len(key) < 24 {
		return nil, errors.New("webhook: the signing secret is too short: at least 24 bytes")
	}
	return &Signer{key: append([]byte(nil), key...)}, nil
}

// Sign returns the "v1,<base64>" signature of a delivery.
func (s *Signer) Sign(id string, timestamp int64, body []byte) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte(id + "." + strconv.FormatInt(timestamp, 10) + "."))
	m.Write(body)
	return "v1," + base64.StdEncoding.EncodeToString(m.Sum(nil))
}

// Options are the sender's settings. A zero value takes the wall clock and DefaultTimeout.
type Options struct {
	Now     func() time.Time
	Timeout time.Duration
}

// Result is what the relay answered. Accepted is true only for a 2xx; any other status is reported as it is.
type Result struct {
	Status   int
	Accepted bool
}

// Sender posts signed deliveries to one target and remembers what it sent, so a replay sends the same id and the same body.
type Sender struct {
	target *url.URL
	signer *Signer
	client *http.Client
	now    func() time.Time

	mu   sync.Mutex
	sent map[string][]byte
}

// NewSender checks the target (http or https, a host, no credential in it, no fragment) and returns a Sender. Redirects are never followed.
func NewSender(target string, signer *Signer, opt Options) (*Sender, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.Hostname() == "" || u.User != nil || u.Fragment != "" {
		return nil, errors.New("webhook: the target must be an http or https URL with a host, no credential and no fragment")
	}
	if signer == nil {
		return nil, errors.New("webhook: a signer is required")
	}
	if opt.Now == nil {
		opt.Now = time.Now
	}
	if opt.Timeout <= 0 {
		opt.Timeout = DefaultTimeout
	}
	c := &http.Client{Timeout: opt.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &Sender{target: u, signer: signer, client: c, now: opt.Now, sent: map[string][]byte{}}, nil
}

// Deliver signs and posts one message. The id is the idempotency key: the same id with another body is refused here and nothing is sent. The body must be one JSON object.
func (s *Sender) Deliver(ctx context.Context, id string, body []byte) (Result, error) {
	if !idRE.MatchString(id) {
		return Result{}, errors.New("webhook: the message id must be 1 to 128 letters, digits and . _ : -")
	}
	if len(body) == 0 || len(body) > MaxBodyBytes {
		return Result{}, fmt.Errorf("webhook: the body must be 1 to %d bytes", MaxBodyBytes)
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(body, &obj); err != nil {
		return Result{}, errors.New("webhook: the body must be one JSON object")
	}
	s.mu.Lock()
	if prev, ok := s.sent[id]; ok && !bytes.Equal(prev, body) {
		s.mu.Unlock()
		return Result{}, errors.New("webhook: this message id was sent with another body; an id is the idempotency key")
	}
	s.sent[id] = append([]byte(nil), body...)
	s.mu.Unlock()
	return s.post(ctx, id, body)
}

// Replay sends again the exact body of an id that was sent before, under the same id and a fresh timestamp and signature. An id never sent is an error and nothing is sent.
func (s *Sender) Replay(ctx context.Context, id string) (Result, error) {
	s.mu.Lock()
	body, ok := s.sent[id]
	s.mu.Unlock()
	if !ok {
		return Result{}, errors.New("webhook: no message with this id was sent")
	}
	return s.post(ctx, id, body)
}

func (s *Sender) post(ctx context.Context, id string, body []byte) (Result, error) {
	ts := s.now().Unix()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.target.String(), bytes.NewReader(body))
	if err != nil {
		return Result{}, errors.New("webhook: the request could not be built")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Mock-Case-Synthetic", "true")
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("webhook-signature", s.signer.Sign(id, ts, body))
	resp, err := s.client.Do(req)
	if err != nil {
		return Result{}, errors.New("webhook: the delivery got no answer from the target") // never the URL or a header
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return Result{Status: resp.StatusCode, Accepted: resp.StatusCode >= 200 && resp.StatusCode < 300}, nil
}
