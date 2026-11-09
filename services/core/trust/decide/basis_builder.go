package decide

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// Errors of the basis builder. Callers compare with errors.Is.
var (
	// ErrBasisRecorded is returned when a step records a set that was already recorded: a later step cannot rewrite what an earlier one read.
	ErrBasisRecorded = errors.New("decide: that part of the evaluated set is already recorded")
	// ErrBasisDiffers is returned when a decision's basis is not the evaluated set (TRU-13): it was changed, swapped or supplied by a caller.
	ErrBasisDiffers = errors.New("decide: the decision's basis differs from the evaluated set")
)

const basisDigestDomain = "noema/decision-basis/v1"

// Digest is "sha256:" and the SHA-256 of the basis: the domain tag, the pack version, the snapshot, the time in decimal, then the policy rules and the
// freshness items, each list as its length in decimal and then its members sorted, every string as a 4-byte big-endian length and the UTF-8 bytes.
// The order in which the steps recorded the members never changes it; moving a character between two fields does (the length prefix).
func (b Basis) Digest() (string, error) {
	if !b.valid() {
		return "", ErrBasisInvalid
	}
	h := sha256.New()
	lp := func(s string) {
		h.Write(binary.BigEndian.AppendUint32(nil, uint32(len(s))))
		h.Write([]byte(s))
	}
	num := func(n int64) { lp(fmt.Sprint(n)) }
	lp(basisDigestDomain)
	lp(b.PackVersion)
	lp(b.EvidenceSnapshotID)
	num(b.EvaluatedAt)
	rules := append([]PolicyRuleVersion(nil), b.PolicyRules...)
	sort.Slice(rules, func(i, j int) bool {
		return fmt.Sprint(rules[i].PolicyID, "\x00", rules[i].RuleID, "\x00", rules[i].Version) < fmt.Sprint(rules[j].PolicyID, "\x00", rules[j].RuleID, "\x00", rules[j].Version)
	})
	num(int64(len(rules)))
	for _, r := range rules {
		lp(r.PolicyID)
		lp(r.RuleID)
		lp(r.Version)
	}
	fresh := append([]Freshness(nil), b.EvidenceFreshness...)
	sort.Slice(fresh, func(i, j int) bool {
		if fresh[i].EvidenceID != fresh[j].EvidenceID {
			return fresh[i].EvidenceID < fresh[j].EvidenceID
		}
		return fresh[i].Source < fresh[j].Source
	})
	num(int64(len(fresh)))
	for _, f := range fresh {
		lp(f.EvidenceID)
		lp(f.Source)
		num(f.EffectiveAt)
		num(f.AgeSeconds)
		num(f.EffectiveLimitSeconds)
		lp(string(f.LimitSetBy))
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}

// BasisBuilder holds the set one evaluation actually read and evaluated (TRU-13). The gate step T9 records the evidence it read, T11 records the policy
// rules it evaluated; nobody else can. The decision's basis is then taken from the builder (Attach), not from a caller, and the Decision Record
// checks the decision against it (Check). One builder is for one evaluation.
type BasisBuilder struct {
	mu         sync.Mutex
	pack       string
	at         int64
	snapshot   string
	fresh      []Freshness
	rules      []PolicyRuleVersion
	haveEvid   bool
	havePolicy bool
}

// NewBasisBuilder starts the set of one evaluation at the pack version and the clock reading (Unix seconds).
func NewBasisBuilder(packVersion string, evaluatedAt int64) (*BasisBuilder, error) {
	if packVersion == "" || evaluatedAt <= 0 {
		return nil, ErrBasisInvalid
	}
	return &BasisBuilder{pack: packVersion, at: evaluatedAt}, nil
}

// RecordEvidence is what T9 calls with the snapshot it sealed and the freshness of each item it read. Once.
func (b *BasisBuilder) RecordEvidence(snapshotID string, items []Freshness) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.haveEvid {
		return ErrBasisRecorded
	}
	b.haveEvid, b.snapshot, b.fresh = true, snapshotID, append([]Freshness(nil), items...)
	return nil
}

// RecordPolicy is what T11 calls with the policy rules it evaluated, each with its policy and version. Once.
func (b *BasisBuilder) RecordPolicy(rules []PolicyRuleVersion) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.havePolicy {
		return ErrBasisRecorded
	}
	b.havePolicy, b.rules = true, append([]PolicyRuleVersion(nil), rules...)
	return nil
}

// Basis is the evaluated set as a Basis. It is not checked for completeness here: a refusal before any read has only the pack version and the time.
func (b *BasisBuilder) Basis() (Basis, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := Basis{PackVersion: b.pack, EvaluatedAt: b.at, EvidenceSnapshotID: b.snapshot,
		PolicyRules: append([]PolicyRuleVersion(nil), b.rules...), EvidenceFreshness: append([]Freshness(nil), b.fresh...)}
	if !out.valid() {
		return Basis{}, ErrBasisInvalid
	}
	return out, nil
}

// Digest is the digest of the evaluated set.
func (b *BasisBuilder) Digest() (string, error) {
	base, err := b.Basis()
	if err != nil {
		return "", err
	}
	return base.Digest()
}

// Attach gives a decision the evaluated set as its basis. A decision that already has a basis, because a caller supplied one, is refused
// (ErrBasisInvalid); an ALLOW or a CONFIRM whose steps recorded no evidence or no policy is refused (ErrBasisIncomplete, INV-16).
func (b *BasisBuilder) Attach(d Decision) (Decision, error) {
	base, err := b.Basis()
	if err != nil {
		return Decision{}, err
	}
	return d.WithBasis(base)
}

// Check is the Decision Record's test (TRU-13): the decision's basis must be exactly the evaluated set. A decision with no basis, or with one that was
// changed after the evaluation, is ErrBasisDiffers.
func (b *BasisBuilder) Check(d Decision) error {
	want, err := b.Digest()
	if err != nil {
		return err
	}
	got, err := d.Basis().Digest()
	if err != nil || got != want {
		return ErrBasisDiffers
	}
	return nil
}
