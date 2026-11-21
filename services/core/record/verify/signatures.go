package verify

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/execution/signing"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// checkSignatures verifies, in the snapshot of the chain check, that every event of the tenant carries a signature that verifies (REC-08, REQ-AUD-006,
// DEC-2026-11-01 D5, D6, D7). It runs in Go, with the public keys of signing_key and the standard library's Ed25519, and does not call the database's own
// verifier: an event written while that trigger was switched off must still be found. The first event that fails is the finding. An event passes when
//   - its signed bytes are exactly the RFC 8785 bytes of the stored body (the body is the signed document, D7),
//   - the key it names is registered and belongs to the emitter that wrote the event (K1: the key ID names one identity), and
//   - the signature verifies over the record-event domain tag and those bytes.
//
// A retired key still verifies what it signed. An event signed under a revoked key at or after the key's compromised_since is not silently accepted: it is
// reported as UNTRUSTED_KEY for review. The finding carries identifiers and a fixed description, never the signature or the bytes.
func checkSignatures(ctx context.Context, tx pgx.Tx, tenantID string) (*Finding, error) {
	rows, err := tx.Query(ctx, `SELECT e.seq, e.written_by, e.at, e.body::text, e.sig_key_id, e.sig_value, e.signed_jcs,
		k.identity, k.public_key, k.status, k.compromised_since
		FROM record_event e LEFT JOIN signing_key k ON k.key_id = e.sig_key_id
		WHERE e.tenant_id = $1 ORDER BY e.seq`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("%sread signatures: %w", errPrefix, err)
	}
	defer rows.Close()
	for rows.Next() {
		var (
			seq                            int64
			writtenBy, body, keyID, sigHex string
			at                             time.Time
			signed                         []byte
			identity, status               *string
			pub                            []byte
			compromisedSince               *time.Time
		)
		if err := rows.Scan(&seq, &writtenBy, &at, &body, &keyID, &sigHex, &signed, &identity, &pub, &status, &compromisedSince); err != nil {
			return nil, fmt.Errorf("%sscan signature: %w", errPrefix, err)
		}
		finding := func(kind chain.BreakKind, detail string) *Finding {
			return &Finding{TenantID: tenantID, Seq: seq, Kind: kind, Detail: detail}
		}
		if f := signedBytesAreTheBody(body, signed); f != "" {
			return finding(BreakSignature, f), nil
		}
		switch {
		case identity == nil:
			return finding(BreakSignature, "the signing key is not registered"), nil
		case *identity != writtenBy:
			return finding(BreakSignature, "the signing key does not belong to the emitter that wrote the event"), nil
		case len(pub) != ed25519.PublicKeySize || !signing.Verify(ed25519.PublicKey(pub), signing.RecordEventTag, signed, sigHex):
			return finding(BreakSignature, "the signature does not verify"), nil
		case status != nil && *status == "REVOKED" && compromisedSince != nil && !at.Before(*compromisedSince):
			return finding(BreakUntrustedKey, "the event was signed under a revoked key at or after compromised_since"), nil
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%sread signatures: %w", errPrefix, err)
	}
	return nil, nil
}

// signedBytesAreTheBody returns a description when the signed bytes are not the canonical form of the stored body, and "" when they are.
func signedBytesAreTheBody(body string, signed []byte) string {
	var doc map[string]any
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	if err := dec.Decode(&doc); err != nil {
		return "the stored body is not a JSON object"
	}
	canon, err := crypto.CanonicalJSON(doc)
	if err != nil {
		return "the stored body cannot be canonicalised"
	}
	if !bytes.Equal(canon, signed) {
		return "the signed bytes are not the stored event"
	}
	return ""
}
