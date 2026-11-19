// Package payloadread gives the Reconciler a payload that is the one its snapshot sealed (ENV-04 slice 2a; noema#596; REQ-CTX-001, REQ-AUD-001; INV-12, INV-19, INV-21; AGENT-13).
//
// The Trust login's payload reader (cmd/core, ruling A228) returns PostgreSQL's jsonb text of an evidence payload. The evidence item's PayloadHash covers something else: the SHA-256 of
// the payload's RFC 8785 form (layer.Builder.Record). So this package decodes the text with exact numbers, makes the object canonical again, hashes it and refuses an item whose hash
// differs: a payload that is not the one the snapshot sealed never classifies.
//
// Implements: REQ-CTX-001 (the stored payload is what the item says it is), INV-19 (a read that cannot be verified is an item that cannot be classified, never a pass), INV-21 (the same sealed
// payload gives the same object).
//
// May import: the standard library, evidence/layer (the Item), platform/crypto (the canonical form).
//
// Must never: read a store itself (the port does), take the tenant from the item or the payload (INV-12), default a missing field, round a number, or put a payload, a hash or a reference into an error (AGENT-13).
package payloadread

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"

	"github.com/Noemai-org/noema/services/core/evidence/injection"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// PayloadReader reads the recorded payload of an evidence item by (tenant, reference) as that tenant's own Trust login. cmd/core's trustPayloads is the real one (A228).
type PayloadReader interface {
	Read(ctx context.Context, tenantID, ref string) ([]byte, error)
}

// Errors of the package. Callers compare with errors.Is. None carries a payload.
var (
	ErrNoDeadline   = errors.New("payloadread: the context needs a deadline")
	ErrUnusable     = errors.New("payloadread: the call cannot be answered safely")
	ErrUnavailable  = errors.New("payloadread: the payload could not be read")
	ErrNotObject    = errors.New("payloadread: the payload is not one JSON object")
	ErrNotCanonical = errors.New("payloadread: the payload has a value the contracts do not allow")
	ErrHashMismatch = errors.New("payloadread: the payload is not the one the item's hash covers")
)

var hashRe = regexp.MustCompile(`\Asha256:[0-9a-f]{64}\z`)

// ErrScreenRefused means the injection screen was refused (A372, interim, relayed, not yet committed): it is missing, it failed, it ran out of time, it panicked, or it answered outside its
// contract. The payload is then not admitted: no text and no partial object is returned. It is never "the screen passed".
var ErrScreenRefused = errors.New("payloadread: the injection screen was refused")

// Screen is the consumer-defined view of injection.Screener (REQ-CTX-007, SoT 29).
type Screen interface {
	Screen(ctx context.Context, sub injection.Subject, fields map[string]string) (injection.Result, error)
}

// CorrelationOf gives the correlation chain the screen's event goes on. An empty answer refuses the read: a flag that cannot be put on the Record must not pass unseen.
type CorrelationOf func(ctx context.Context) string

// Flag is what the Alert gets for a payload that was flagged and whose event the Record refused: names only (tenant, evidence id, field names and pattern ids), never a value.
type Flag struct {
	TenantID   string
	EvidenceID string
	Findings   []injection.Finding
}

// Alert is the operational alert seam for a flagged payload whose Record event failed. It never blocks the read.
type Alert interface {
	ScreenFlagged(ctx context.Context, f Flag)
}

// Reader verifies each payload it reads against the item's hash, then screens its text. Payloads, Screen and Correlation have no default: a Reader without one refuses every call.
type Reader struct {
	Payloads    PayloadReader
	Screen      Screen
	Correlation CorrelationOf
	Alert       Alert
}

// NewReader builds a Reader. Every argument is required.
func NewReader(p PayloadReader, s Screen, c CorrelationOf, a Alert) (Reader, error) {
	if p == nil || s == nil || c == nil || a == nil {
		return Reader{}, ErrUnusable
	}
	return Reader{Payloads: p, Screen: s, Correlation: c, Alert: a}, nil
}

// Payload returns the object of the item's stored payload, only when its canonical hash is the item's PayloadHash. The tenant is the caller's, never the item's or the payload's. On any error
// the object is nil.
func (r Reader) Payload(ctx context.Context, tenantID string, it layer.Item) (map[string]any, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, ErrNoDeadline
	}
	if r.Payloads == nil || tenantID == "" || it.EvidenceID == "" || it.PayloadRef == "" || !hashRe.MatchString(it.PayloadHash) {
		return nil, ErrUnusable
	}
	raw, err := r.Payloads.Read(ctx, tenantID, it.PayloadRef)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnavailable, err)
	}
	obj, err := decode(raw)
	if err != nil {
		return nil, err
	}
	canon, err := crypto.CanonicalJSON(obj)
	if err != nil {
		return nil, ErrNotCanonical // the cause names the number or the type, never the payload; it is dropped so no payload text can ride in an error
	}
	sum := sha256.Sum256(canon)
	if "sha256:"+hex.EncodeToString(sum[:]) != it.PayloadHash {
		return nil, ErrHashMismatch
	}
	return obj, nil
}

// decode reads exactly one JSON object, with numbers kept as text (json.Number) so none is rounded, and nothing after it.
func decode(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		return nil, ErrNotObject
	}
	obj, ok := v.(map[string]any)
	if !ok {
		return nil, ErrNotObject
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrNotObject
	}
	return obj, nil
}
