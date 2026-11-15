// Package snapshot holds the sealed context snapshot of REQ-CTX-004: the evidence_set_hash (this file) and, in later slices, the sealer and the integrity check.
// Implements: REQ-CTX-004; INV-17; DEC-2026-10-16 C1; golden vectors CON-04.
package snapshot

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"sort"
)

const hashPrefix = "sha256:"

var payloadHashRe = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Pair is one member of the set: the evidence item and the hash of its immutable payload.
type Pair struct {
	EvidenceID  string `json:"evidence_id"`
	PayloadHash string `json:"payload_hash"`
}

// SetHash is "sha256:" and the SHA-256 of the pairs sorted by the UTF-8 bytes of evidence_id, each written as LP(evidence_id) LP(payload_hash), where LP is a
// 4-byte big-endian length and then the bytes. An empty evidence_id, a repeated evidence_id, or a payload hash that is not "sha256:" and 64 lowercase hex
// characters is an error. The empty set hashes to the SHA-256 of zero bytes: that is a fact about the function, not a permission (the sealer never seals it).
// The input is not modified.
func SetHash(items []Pair) (string, error) {
	ordered := append([]Pair(nil), items...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].EvidenceID < ordered[j].EvidenceID }) // Go compares strings bytewise, which is UTF-8 order
	h := sha256.New()
	for i, p := range ordered {
		if p.EvidenceID == "" {
			return "", errors.New("snapshot: an evidence_id is empty")
		}
		if i > 0 && ordered[i-1].EvidenceID == p.EvidenceID {
			return "", fmt.Errorf("snapshot: evidence_id %q appears more than once", p.EvidenceID)
		}
		if !payloadHashRe.MatchString(p.PayloadHash) {
			return "", fmt.Errorf("snapshot: the payload_hash of %q is not sha256: and 64 lowercase hex characters", p.EvidenceID)
		}
		writeLP(h, p.EvidenceID)
		writeLP(h, p.PayloadHash)
	}
	return hashPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

type writer interface{ Write(p []byte) (int, error) }

func writeLP(w writer, s string) {
	var n [4]byte
	binary.BigEndian.PutUint32(n[:], uint32(len(s))) // #nosec G115 -- an evidence id or hash is far below 4 GiB
	_, _ = w.Write(n[:])
	_, _ = w.Write([]byte(s))
}
