package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

// contentDomain separates the content hash from every other hash in the system (the set hash above included): it is written, length-prefixed, before any item.
const contentDomain = "noema.evidence_content_set.v1"

// ErrContentHashInput is returned by ContentHash for a set that cannot be bound: no item, an empty source or source record ID, or a payload hash that is not "sha256:" and 64
// lowercase hex characters. It is never a hash of what is left.
var ErrContentHashInput = errors.New("snapshot: the evidence content cannot be bound")

// ContentItem is one fact of a snapshot as its content, without the identifier the evidence layer minted for the read: where it came from, which record there, and the hash of what
// was read.
type ContentItem struct {
	Source         string
	SourceRecordID string
	FactsDigest    string
}

// ContentHash is the evidence value a confirmation binds (A365, REQ-TRU-009, INV-25): "sha256:" and the SHA-256 of the domain tag and then, for each distinct item in sorted order,
// LP(source) LP(source_record_id) LP(payload_hash), where LP is a 4-byte big-endian length and then the bytes (the same framing as SetHash, so no field can run into the next).
// The order is by source, then source record ID, then payload hash, each compared bytewise (UTF-8 order), so the order of the items does not matter.
//
// It does not depend on any evidence ID. The evidence layer mints a new evidence ID for every read, so SetHash is never the same for two snapshots of the same data; this value is,
// and it changes when any source, any record or any payload changes (a payload hash commits to the payload), and when an item is added or removed. A repeated triple is one
// fact: the same record read twice with the same payload is the same content, so it is hashed once (defined, not refused). The input is not modified.
//
// No items, an empty source or record ID, or a payload hash outside the contract's form is ErrContentHashInput: a snapshot that cannot be bound by its content is not bound at all.
//
// Example: the one item (source "ledger.synth", record "rec-1", payload hash sha256:00...01) hashes to the value pinned in content_hash_test.go.
func ContentHash(items []ContentItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("%w: no item", ErrContentHashInput)
	}
	ordered := append([]ContentItem(nil), items...)
	for _, it := range ordered {
		if it.Source == "" || it.SourceRecordID == "" || !payloadHashRe.MatchString(it.FactsDigest) {
			return "", fmt.Errorf("%w: an item has no source or record ID, or a payload hash that is not sha256: and 64 lowercase hex characters", ErrContentHashInput)
		}
	}
	sort.Slice(ordered, func(i, j int) bool { return lessContent(ordered[i], ordered[j]) })
	h := sha256.New()
	writeLP(h, contentDomain)
	for i, it := range ordered {
		if i > 0 && ordered[i-1] == it {
			continue
		}
		writeLP(h, it.Source)
		writeLP(h, it.SourceRecordID)
		writeLP(h, it.FactsDigest)
	}
	return hashPrefix + hex.EncodeToString(h.Sum(nil)), nil
}

func lessContent(a, b ContentItem) bool { // Go compares strings bytewise, which is UTF-8 order
	if a.Source != b.Source {
		return a.Source < b.Source
	}
	if a.SourceRecordID != b.SourceRecordID {
		return a.SourceRecordID < b.SourceRecordID
	}
	return a.FactsDigest < b.FactsDigest
}
