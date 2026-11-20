package snapshot

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

// contentDomain separates the content hash from every other hash in the system (the set hash and FactsDigest included): it is written, length-prefixed, before any item. v3: an
// entry also carries the item's status (A435), so a v2 value and a v3 value can never collide (v2: the third element was a facts digest, v1: a payload hash).
const contentDomain = "noema.evidence_content_set.v3"

// ErrContentHashInput is returned by ContentHash for a set that cannot be bound: no item, an empty source, source record ID or status, or a facts digest that is not "sha256:" and 64
// lowercase hex characters. It is never a hash of what is left.
var ErrContentHashInput = errors.New("snapshot: the evidence content cannot be bound")

// ContentItem is one entry of a snapshot as its content, without the identifier the evidence layer minted for the read: where it came from, which record there, the digest
// of the item's mapped, normalized facts (FactsDigest), never of its raw payload, and the item's fact status as the Reconciler gives it.
type ContentItem struct {
	Source         string
	SourceRecordID string
	FactsDigest    string
	// Status is the item's fact status as the Reconciler gives it (the contract's enum name): the status of the item that the decision rests on is part of the content (A435).
	Status string
}

// ContentHash is the evidence value a confirmation binds (A365 refined, A435, REQ-TRU-009, INV-25): "sha256:" and the SHA-256 of the domain tag and then, for each distinct entry in
// sorted order, LP(source) LP(source_record_id) LP(facts_digest) LP(status), where LP is a 4-byte big-endian length and then the bytes (the same framing as SetHash, so no field can
// run into the next). The order is by source, then source record ID, then facts digest, then status, each compared bytewise (UTF-8 order), so the order of the items does not matter.
//
// It does not depend on any evidence ID, and not on the raw payload: the evidence layer mints a new evidence ID for every read, and two reads of unchanged evidence can differ in a
// volatile or unmapped field, so the digest of the mapped facts is what is bound. The value changes when any source, any record or any mapped fact changes, and when an item is
// added or removed. A repeated entry (all four fields equal) is one fact: the same record read twice with the same facts and status is the same content, so it is hashed once (defined, not refused);
// two entries that differ only in status are two. The input is
// not modified.
//
// No items, an empty source, record ID or status, or a facts digest outside the contract's form is ErrContentHashInput: a snapshot that cannot be bound by its content is not bound at all.
//
// Example: the one item (source "ledger.synth", record "rec-1", the digest of the fact debit.state=POSTED, status KNOWN) hashes to the value pinned in content_hash_test.go.
func ContentHash(items []ContentItem) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("%w: no item", ErrContentHashInput)
	}
	ordered := append([]ContentItem(nil), items...)
	for _, it := range ordered {
		if it.Source == "" || it.SourceRecordID == "" || it.Status == "" || !payloadHashRe.MatchString(it.FactsDigest) {
			return "", fmt.Errorf("%w: an item has no source, record ID or status, or a facts digest that is not sha256: and 64 lowercase hex characters", ErrContentHashInput)
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
		writeLP(h, it.Status)
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
	if a.FactsDigest != b.FactsDigest {
		return a.FactsDigest < b.FactsDigest
	}
	return a.Status < b.Status
}
