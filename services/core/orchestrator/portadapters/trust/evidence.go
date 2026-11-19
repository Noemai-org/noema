package trustport

import (
	"context"
	"errors"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/reconciler"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/decide"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// Errors of the Evidence adapter (A329). Each ends the T9 step in UNKNOWN, never in a pass (REQ-NFR-002, INV-16). None carries a tenant, an identifier of the evidence or any
// row text (AGENT-13). Callers compare with errors.Is.
var (
	// ErrEvidenceNoDeadline is returned for a call whose context has no deadline.
	ErrEvidenceNoDeadline = errors.New("trustport: the evidence read needs a context with a deadline")
	// ErrSnapshotNotFound is returned for a snapshot that does not exist, that another tenant owns, or whose ID is not the shape of one: the same answer for all three
	// (REQ-TRU-003).
	ErrSnapshotNotFound = errors.New("trustport: no such sealed snapshot")
	// ErrSnapshotNotIntact is returned when the stored snapshot is not exactly what was sealed or cannot be read as evidence: the hash does not verify, an item is missing,
	// repeated or foreign, or an item has no usable status, source, limit or time (INV-17, INV-19).
	ErrSnapshotNotIntact = errors.New("trustport: the sealed snapshot is not usable as evidence")
	// ErrSnapshotIntent is returned when the snapshot cannot be attributed to an intent with certainty (A226).
	ErrSnapshotIntent = errors.New("trustport: the sealed snapshot cannot be bound to an intent")
	// ErrSnapshotStore is returned when the database or the tenant's login could not answer. The cause is kept for errors.Is and its text is not shown.
	ErrSnapshotStore = errors.New("trustport: the sealed snapshot could not be read")
	// ErrSourceRules is returned when the trust profile's evidence rules cannot be had or cannot be used. The cause is kept for errors.Is and its text is not shown.
	ErrSourceRules = errors.New("trustport: the evidence rules are not usable")
)

// SourceRules are the evidence rules of the trust profile that governs an intent node, as the pack carries them (trust-profile.v1: `evidence_required` and
// `max_evidence_age_seconds`). They are data: this package holds no number of its own (AGENT-16).
type SourceRules struct {
	// RequiredSources are the sources the profile requires evidence from.
	RequiredSources []string
	// MaxAgeSeconds is the profile's cap on the age of an item. It can only tighten a source's limit (INV-23). It is at least 1.
	MaxAgeSeconds int64
}

// SourceProfiles gives the evidence rules of the trust profile that governs an intent node. An unknown node is an error, not an empty set of rules.
type SourceProfiles interface {
	SourceRules(ctx context.Context, tenantID, intentNode string) (SourceRules, error)
}

// ItemFacts gives the mapped, normalized facts of one evidence item, as the tenant: the facts the decision rests on (A365 refined). evidence/reconciler.Reconciler is the real one, so
// the mapping is the Reconciler's own and there is no second one.
type ItemFacts interface {
	Facts(ctx context.Context, tenantID string, it layer.Item) ([]snapshot.Fact, error)
}

var _ ItemFacts = (*reconciler.Reconciler)(nil)

// Evidence implements intake.Evidence: the T9 read of the Trust intake. It reports the freshness and the fact status of the items of a sealed snapshot as the Decision Record
// keeps them, from what was sealed and verified, never from what a caller supplied (REQ-TRU-001, REQ-CTX-003, REQ-CTX-004). The freshness and the status are the stored columns; the value the confirmation binds is the digest of the facts the mapping gives from the stored payloads (the facts source).
type Evidence struct {
	snapshots SnapshotReader
	profiles  SourceProfiles
	clock     clock.Clock
	facts     ItemFacts
}

var _ intake.Evidence = Evidence{}

// NewEvidence builds the adapter. A missing reader, profile source, clock or facts source is ErrConfig.
func NewEvidence(snapshots SnapshotReader, profiles SourceProfiles, clk clock.Clock, facts ItemFacts) (Evidence, error) {
	if snapshots == nil || profiles == nil || clk == nil || facts == nil {
		return Evidence{}, ErrConfig
	}
	return Evidence{snapshots: snapshots, profiles: profiles, clock: clk, facts: facts}, nil
}

var snapshotIDRe = regexp.MustCompile(`\Aevs_[0-9A-Za-z]{2,32}\z`) // the shape migration 0020 gives context_snapshot.snapshot_id

// SnapshotFreshness reads the sealed snapshot as the tenant, verifies it, and reports each item. On any error the answer is empty: T9 has nothing to decide on and ends UNKNOWN.
func (e Evidence) SnapshotFreshness(ctx context.Context, tenantID, snapshotID string) (intake.SnapshotEvidence, error) {
	if e.snapshots == nil || e.profiles == nil || e.clock == nil || e.facts == nil {
		return intake.SnapshotEvidence{}, ErrConfig
	}
	if !chain.ValidTenantID(tenantID) {
		return intake.SnapshotEvidence{}, ErrSession
	}
	if _, ok := ctx.Deadline(); !ok {
		return intake.SnapshotEvidence{}, ErrEvidenceNoDeadline
	}
	if !snapshotIDRe.MatchString(snapshotID) {
		return intake.SnapshotEvidence{}, ErrSnapshotNotFound
	}
	s, err := e.snapshots.Sealed(ctx, tenantID, snapshotID)
	switch {
	case errors.Is(err, ErrSnapshotNotFound):
		return intake.SnapshotEvidence{}, ErrSnapshotNotFound
	case err != nil:
		return intake.SnapshotEvidence{}, wrapped(ErrSnapshotStore, err)
	}
	now := e.clock.Now()
	ordered, err := intact(s, tenantID, snapshotID)
	if err != nil {
		return intake.SnapshotEvidence{}, err
	}
	statuses, err := readable(ordered, now)
	if err != nil {
		return intake.SnapshotEvidence{}, err
	}
	if err := attributable(s.Intent, ordered); err != nil {
		return intake.SnapshotEvidence{}, err
	}
	rules, err := e.profiles.SourceRules(ctx, tenantID, s.Intent.IntentNode)
	if err != nil {
		return intake.SnapshotEvidence{}, wrapped(ErrSourceRules, err)
	}
	if err := usable(rules); err != nil {
		return intake.SnapshotEvidence{}, err
	}
	content, err := e.contentHash(ctx, tenantID, ordered)
	if err != nil {
		return intake.SnapshotEvidence{}, err
	}
	out := intake.SnapshotEvidence{IntentID: s.Intent.IntentID, IntentVersion: s.Intent.Version, EvidenceSetHash: content, Items: make([]intake.SourceItem, len(ordered))}
	present := make(map[string]bool, len(ordered))
	for i, it := range ordered {
		limit, by := it.SLASeconds, decide.LimitBySource
		if rules.MaxAgeSeconds < limit { // a profile can only tighten a source's limit (INV-23)
			limit, by = rules.MaxAgeSeconds, decide.LimitByTrustProfile
		}
		out.Items[i] = intake.SourceItem{Status: statuses[i], Freshness: decide.Freshness{EvidenceID: it.EvidenceID, EffectiveAt: it.EffectiveAt.Unix(), Source: it.Source,
			AgeSeconds: ageSeconds(now, it.ReadAt), EffectiveLimitSeconds: limit, LimitSetBy: by}}
		present[it.Source] = true
	}
	for _, src := range rules.RequiredSources {
		if !present[src] {
			out.RequiredUnavailable = true // a required source with no item at all could not be read
		}
	}
	return out, nil
}

// contentHash is the evidence value the confirmation binds (A365 refined): the content hash of the items' (source, source record ID, facts digest), where an item's facts digest is the
// digest of its MAPPED, NORMALIZED facts, which the facts source (the Reconciler) reads from the stored payload and maps with the evidence mapping the Reconciler classifies with. A re-read
// of unchanged facts gives the same value under new evidence IDs and whatever differs in the raw payload (a volatile or unmapped field); a changed mapped fact gives another. The set
// hash the snapshot stores (over the evidence IDs) is verified by intact and is not what is given.
//
// Fail closed (INV-19, REQ-NFR-002): an item with no source record ID, whose payload cannot be read or is not the sealed one, that the mapping cannot use, or whose facts cannot be
// digested makes the snapshot unusable as evidence. The cause stays reachable behind the fixed text. No payload is read before every item has a record ID.
func (e Evidence) contentHash(ctx context.Context, tenantID string, items []SealedItem) (string, error) {
	for _, it := range items {
		if it.SourceRecordID == "" {
			return "", notIntact()
		}
	}
	in := make([]snapshot.ContentItem, len(items))
	for i, it := range items {
		facts, err := e.facts.Facts(ctx, tenantID, layer.Item{EvidenceID: it.EvidenceID, PayloadRef: it.PayloadRef, PayloadHash: it.PayloadHash, Status: contracts.EnumsV1FactStatus(it.Status),
			Source: it.Source, ReadAt: it.ReadAt, EffectiveAt: it.EffectiveAt})
		if err != nil {
			return "", wrapped(ErrSnapshotNotIntact, err)
		}
		digest, err := snapshot.FactsDigest(facts)
		if err != nil {
			return "", wrapped(ErrSnapshotNotIntact, err)
		}
		in[i] = snapshot.ContentItem{Source: it.Source, SourceRecordID: it.SourceRecordID, FactsDigest: digest}
	}
	h, err := snapshot.ContentHash(in)
	if err != nil {
		return "", wrapped(ErrSnapshotNotIntact, err)
	}
	return h, nil
}

func notIntact() error { return wrapped(ErrSnapshotNotIntact, nil) }

// intact checks that the stored snapshot is the one asked for and exactly what was sealed: the ids sorted and unique, every id's item there once and of the snapshot's own case,
// and the hash over the (id, payload hash) pairs the one stored (INV-17). It returns the items in the order of the sealed ids and changes nothing it was given.
func intact(s SealedSnapshot, tenantID, snapshotID string) ([]SealedItem, error) {
	if s.TenantID != tenantID || s.SnapshotID != snapshotID || s.CorrelationID == "" || len(s.EvidenceIDs) == 0 || len(s.Items) != len(s.EvidenceIDs) {
		return nil, notIntact()
	}
	byID := make(map[string]SealedItem, len(s.Items))
	for _, it := range s.Items {
		if _, dup := byID[it.EvidenceID]; dup || it.EvidenceID == "" || it.CorrelationID != s.CorrelationID {
			return nil, notIntact()
		}
		byID[it.EvidenceID] = it
	}
	ordered := make([]SealedItem, len(s.EvidenceIDs))
	pairs := make([]snapshot.Pair, len(s.EvidenceIDs))
	for i, id := range s.EvidenceIDs {
		it, ok := byID[id]
		if !ok || id == "" || (i > 0 && s.EvidenceIDs[i-1] >= id) {
			return nil, notIntact()
		}
		ordered[i], pairs[i] = it, snapshot.Pair{EvidenceID: id, PayloadHash: it.PayloadHash}
	}
	hash, err := snapshot.SetHash(pairs)
	if err != nil || hash != s.EvidenceSetHash {
		return nil, notIntact()
	}
	return ordered, nil
}

// readable checks that every item can be read as evidence and returns each item's fact status as the contract's enum: a status that is missing or not one of the eight, no
// source, a limit below one second, no read or effective time, or a read after the evaluation is an error for the whole snapshot (INV-19; A226: no negative age).
func readable(items []SealedItem, now time.Time) ([]contracts.EnumsV1FactStatus, error) {
	out := make([]contracts.EnumsV1FactStatus, len(items))
	for i, it := range items {
		st, ok := factStatus(it.Status)
		if !ok || it.Source == "" || it.SLASeconds < 1 || it.ReadAt.IsZero() || it.ReadAt.After(now) || it.EffectiveAt.IsZero() || it.EffectiveAt.Unix() <= 0 {
			return nil, notIntact()
		}
		out[i] = st
	}
	return out, nil
}

func factStatus(s string) (contracts.EnumsV1FactStatus, bool) {
	switch st := contracts.EnumsV1FactStatus(s); st {
	case contracts.EnumsV1FactStatusKNOWN, contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusAMBIGUOUS, contracts.EnumsV1FactStatusUNAVAILABLE,
		contracts.EnumsV1FactStatusSTALE, contracts.EnumsV1FactStatusCONTRADICTORY, contracts.EnumsV1FactStatusUNKNOWN, contracts.EnumsV1FactStatusUNAUTHORIZED:
		return st, true
	}
	return "", false
}

// attributable says whether the sealed snapshot can be attributed to the latest intent of its correlation with certainty (A226). The snapshot does not name its intent, so it is
// attributed only when that intent was admitted no later than the first item was read: a newer version admitted after a read means the evidence was read for an older one.
func attributable(in SealedIntent, items []SealedItem) error {
	if in.IntentID == "" || in.Version < 1 || in.IntentNode == "" || in.AdmittedAt.IsZero() {
		return wrapped(ErrSnapshotIntent, nil)
	}
	for _, it := range items {
		if in.AdmittedAt.After(it.ReadAt) {
			return wrapped(ErrSnapshotIntent, nil)
		}
	}
	return nil
}

// usable checks the profile's rules: a cap below one second is not a cap, and a required source has a name.
func usable(r SourceRules) error {
	if r.MaxAgeSeconds < 1 {
		return wrapped(ErrSourceRules, nil)
	}
	for _, src := range r.RequiredSources {
		if src == "" {
			return wrapped(ErrSourceRules, nil)
		}
	}
	return nil
}

// ageSeconds is the time from the read to the evaluation in whole seconds, rounded up, so that an age compared with the limit says "older than the limit" exactly when the
// elapsed time is (the status function's rule). The read is not after the evaluation: readable refused that.
func ageSeconds(now, readAt time.Time) int64 {
	d := now.Sub(readAt)
	secs := int64(d / time.Second)
	if d%time.Second != 0 {
		secs++
	}
	return secs
}
