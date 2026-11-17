// Package reconciler is the Reconciler of readseal (ENV-04 slice 2b; rulings A208, A245, A260 on noema#579 and noema#596; REQ-RES-001, REQ-CTX-001; INV-19, INV-21, INV-23; AGENT-13).
//
// For the evidence items just read it loads each KNOWN item's stored payload (verified against the item's hash by payloadread), maps it to rows of the rule set's vocabulary (the pack's evidence
// mapping), builds one reconcile.Evidence per row, and classifies with the rule set. Absence is UNKNOWN and never a negative fact: a row the payload gave no field for, a payload that is not the
// sealed one or not an object, and an item that is not KNOWN are each evidence that cannot satisfy a rule. Every item is required (A245). The time is the injected clock's; the answer is a
// function of the items, the rule set and the mapping (INV-21), so the same evidence in any order gives the same reconciliation id.
//
// May import: the standard library, evidence/layer, evidence/payloadread, evidence/readseal, platform/clock, trust/reconcile and its mapping (pure packages).
//
// Must never: read a store itself (the payload reader is given), call a model, decide what a classification allows (Trust does), put a payload, a store's error text or a connection string in an
// error message, or default anything it was not given.
package reconciler

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"math/big"
	"sort"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/readseal"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
	"github.com/Noemai-org/noema/services/core/trust/reconcile/mapping"
)

const prefix = "reconciler: "

var (
	// ErrNotUsable marks a configuration that cannot run: a missing part, or a mapping written for another rule set.
	ErrNotUsable = errors.New(prefix + "the reconciler is not usable")
	// ErrItem marks an item set the reconciler refuses whole: no items, an empty or repeated id, no source, a source the mapping does not know, a zero time, no freshness limit, a status that is not one.
	ErrItem = errors.New(prefix + "the evidence items cannot be classified")
	// ErrRead marks a stored payload that could not be read for a reason that is not the payload's (the store, the context): the cause is reachable with errors.Is and is never printed.
	ErrRead = errors.New(prefix + "a stored payload could not be read")
	// ErrRule marks a rule that could not be evaluated over the evidence: nothing is guessed, the classification is refused.
	ErrRule = errors.New(prefix + "the rule set could not classify the evidence")
)

// opError is a failure whose text is fixed: the cause stays reachable for errors.Is and errors.As and is never part of Error().
type opError struct{ kind, cause error }

func (e *opError) Error() string   { return e.kind.Error() }
func (e *opError) Unwrap() []error { return []error{e.kind, e.cause} }

// Payloads is payloadread.Reader's one method: the verified payload of an item, read as the tenant.
type Payloads interface {
	Payload(ctx context.Context, tenantID string, it layer.Item) (map[string]any, error)
}

// Config is what New needs. Every part is required; there are no defaults.
type Config struct {
	RuleSet  *reconcile.RuleSet
	Mapping  *mapping.Mapping
	Payloads Payloads
	Clock    clock.Clock
}

// Reconciler implements readseal.Reconciler.
type Reconciler struct{ c Config }

var _ readseal.Reconciler = (*Reconciler)(nil)

// New refuses a configuration with a missing part, or a mapping that was not written for the rule set.
func New(c Config) (*Reconciler, error) {
	if c.RuleSet == nil || c.Mapping == nil || c.Payloads == nil || c.Clock == nil || c.Mapping.RuleSet() != c.RuleSet.ID() {
		return nil, ErrNotUsable
	}
	return &Reconciler{c: c}, nil
}

var statuses = map[contracts.EnumsV1FactStatus]reconcile.FactStatus{
	contracts.EnumsV1FactStatusKNOWN: reconcile.Known, contracts.EnumsV1FactStatusINFERRED: reconcile.Inferred, contracts.EnumsV1FactStatusAMBIGUOUS: reconcile.Ambiguous,
	contracts.EnumsV1FactStatusUNAVAILABLE: reconcile.Unavailable, contracts.EnumsV1FactStatusSTALE: reconcile.Stale, contracts.EnumsV1FactStatusCONTRADICTORY: reconcile.Contradictory,
	contracts.EnumsV1FactStatusUNKNOWN: reconcile.Unknown, contracts.EnumsV1FactStatusUNAUTHORIZED: reconcile.Unauthorized,
}

// Classify classifies the items as the tenant. The tenant is the caller's, never an item's or a payload's.
func (r *Reconciler) Classify(ctx context.Context, tenantID string, items []layer.Item) (readseal.Classified, error) {
	if tenantID == "" || len(items) == 0 {
		return readseal.Classified{}, ErrItem
	}
	sorted := append([]layer.Item(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].EvidenceID < sorted[j].EvidenceID })
	var evidence []reconcile.Evidence
	for i, it := range sorted {
		if err := r.check(it, i > 0 && sorted[i-1].EvidenceID == it.EvidenceID); err != nil {
			return readseal.Classified{}, err
		}
		rows, err := r.rows(ctx, tenantID, it)
		if err != nil {
			return readseal.Classified{}, err
		}
		evidence = append(evidence, rows...)
	}
	res, err := reconcile.Reconcile(r.c.RuleSet, evidence, r.c.Clock.Now().Unix())
	if err != nil {
		return readseal.Classified{}, &opError{kind: ErrRule, cause: err}
	}
	return readseal.Classified{ReconciliationID: r.reconciliationID(sorted), Classification: string(res.Classification)}, nil
}

func (r *Reconciler) check(it layer.Item, repeated bool) error {
	if _, ok := statuses[it.Status]; !ok || it.EvidenceID == "" || repeated || it.Source == "" || it.ReadAt.IsZero() || it.EffectiveAt.IsZero() || it.EffectiveFreshnessSeconds <= 0 {
		return ErrItem
	}
	return nil
}

// rows gives the evidence of one item: one per row of the rule set's vocabulary its source feeds. The payload is read only for a KNOWN item, since no other can satisfy a rule.
func (r *Reconciler) rows(ctx context.Context, tenantID string, it layer.Item) ([]reconcile.Evidence, error) {
	status := statuses[it.Status]
	payload, trusted := map[string]any{}, false
	if status == reconcile.Known {
		obj, err := r.c.Payloads.Payload(ctx, tenantID, it)
		switch {
		case err == nil:
			payload, trusted = obj, true
		case errors.Is(err, payloadread.ErrHashMismatch), errors.Is(err, payloadread.ErrNotObject), errors.Is(err, payloadread.ErrNotCanonical):
			// the payload is not the sealed one, or not one object: the item is UNKNOWN, not an error (A260)
		default:
			return nil, &opError{kind: ErrRead, cause: err}
		}
	}
	mapped, err := r.c.Mapping.Rows(it.Source, payload)
	if err != nil || len(mapped) == 0 {
		return nil, ErrItem
	}
	expires := expiry(it)
	out := make([]reconcile.Evidence, 0, len(mapped))
	for _, row := range mapped {
		st := status
		if st == reconcile.Known && (!trusted || len(row.Fields) == 0) {
			st = reconcile.Unknown
		}
		out = append(out, reconcile.Evidence{ID: it.EvidenceID + "/" + row.Entity, Source: it.Source, Entity: row.Entity, Status: st, EffectiveAt: it.EffectiveAt.Unix(), ExpiresAt: expires,
			Required: true, Fields: row.Fields})
	}
	return out, nil
}

// expiry is the end of the item's effective freshness: its read time plus the limit AssignStatus applied (the stricter of the source's and the profile's, INV-23), so no clock is needed for it.
func expiry(it layer.Item) int64 {
	return it.ReadAt.Add(time.Duration(it.EffectiveFreshnessSeconds) * time.Second).Unix()
}

// reconciliationID is `rec_` and 26 letters from the SHA-256 of the rule set, the mapping and the sorted items (id, payload hash, status): it names what was classified (INV-21) and holds no digit,
// so the Record's guard against digit runs never meets one. The contract allows 2 to 32 letters or digits.
func (r *Reconciler) reconciliationID(sorted []layer.Item) string {
	h := sha256.New()
	put := func(s string) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put(r.c.RuleSet.ID())
	put(r.c.Mapping.ID())
	for _, it := range sorted {
		put(it.EvidenceID)
		put(it.PayloadHash)
		put(string(it.Status))
	}
	number, base, letters := new(big.Int).SetBytes(h.Sum(nil)), big.NewInt(26), make([]byte, 26)
	for i := range letters {
		var rest big.Int
		number.DivMod(number, base, &rest)
		letters[i] = byte('a' + rest.Int64())
	}
	return "rec_" + string(letters)
}
