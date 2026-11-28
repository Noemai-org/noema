// Package reconciler is the Reconciler of readseal (ENV-04 slice 2b; rulings A208, A245, A260 on noema#579 and noema#596; REQ-RES-001, REQ-CTX-001; INV-19, INV-21, INV-23; AGENT-13).
//
// For the evidence items just read it loads each KNOWN item's stored payload (verified against the item's hash by payloadread), maps it to rows of the rule set's vocabulary (the pack's evidence
// mapping), builds one reconcile.Evidence per row, and classifies with the rule set. Absence is UNKNOWN and never a negative fact: a row the payload gave no field for, a payload that is not the
// sealed one or not an object, and an item that is not KNOWN are each evidence that cannot satisfy a rule. Every item is required (A245). The time is the injected clock's; the answer is a
// function of the items, the rule set and the mapping (INV-21), so the same evidence in any order gives the same reconciliation id.
//
// May import: the standard library, evidence/layer, evidence/payloadread, evidence/readseal, evidence/snapshot (the Fact type), internal/predicate (the mapped values), platform/clock, trust/reconcile and its mapping (pure packages).
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
	"strconv"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/evidence/readseal"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/internal/predicate"
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
	now := r.c.Clock.Now()
	sorted := append([]layer.Item(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].EvidenceID < sorted[j].EvidenceID })
	var evidence []reconcile.Evidence
	for i, it := range sorted {
		if err := r.check(it, i > 0 && sorted[i-1].EvidenceID == it.EvidenceID, now); err != nil {
			return readseal.Classified{}, err
		}
		rows, err := r.rows(ctx, tenantID, it)
		if err != nil {
			return readseal.Classified{}, err
		}
		evidence = append(evidence, rows...)
	}
	res, err := reconcile.Reconcile(r.c.RuleSet, evidence, now.Unix())
	if err != nil {
		return readseal.Classified{}, &opError{kind: ErrRule, cause: err}
	}
	out := readseal.Classified{ReconciliationID: r.reconciliationID(sorted, now), Classification: string(res.Classification)}
	if res.Wait != nil {
		out.WaitUntil = res.Wait.Until
	}
	return out, nil
}

func (r *Reconciler) check(it layer.Item, repeated bool, now time.Time) error {
	if _, ok := statuses[it.Status]; !ok || it.EvidenceID == "" || repeated || it.Source == "" || it.ReadAt.IsZero() || it.EffectiveAt.IsZero() || it.EffectiveFreshnessSeconds <= 0 || it.ReadAt.After(now) {
		return ErrItem
	}
	return nil
}

// itemRows is what one item gives the Reconciler, read once for Classify and for Facts: the rows the mapping gives for its source, the item's status, whether the payload they were
// made from is the sealed one, and, when it is not, why. The payload is read only for a KNOWN item, since no other can satisfy a rule. A store or context failure is an error here.
type itemRows struct {
	mapped   []mapping.Row
	status   reconcile.FactStatus
	trusted  bool
	unsealed error
}

func (r *Reconciler) itemRows(ctx context.Context, tenantID string, it layer.Item) (itemRows, error) {
	out := itemRows{status: statuses[it.Status]}
	payload := map[string]any{}
	if out.status == reconcile.Known {
		obj, err := r.c.Payloads.Payload(ctx, tenantID, it)
		switch {
		case err == nil:
			payload, out.trusted = obj, true
		case errors.Is(err, payloadread.ErrHashMismatch), errors.Is(err, payloadread.ErrNotObject), errors.Is(err, payloadread.ErrNotCanonical):
			out.unsealed = err // the payload is not the sealed one, or not one object: the item is UNKNOWN, not an error (A260)
		default:
			return itemRows{}, &opError{kind: ErrRead, cause: err}
		}
	}
	mapped, err := r.c.Mapping.Rows(it.Source, payload)
	if err != nil || len(mapped) == 0 {
		return itemRows{}, ErrItem
	}
	out.mapped = mapped
	return out, nil
}

// rowStatus is the status a row of the item has: a KNOWN item's row is UNKNOWN when the payload is not the sealed one or gave the row no field (absence is never a fact, INV-19).
func (g itemRows) rowStatus(row mapping.Row) reconcile.FactStatus {
	if g.status == reconcile.Known && (!g.trusted || len(row.Fields) == 0) {
		return reconcile.Unknown
	}
	return g.status
}

// rows gives the evidence of one item: one per row of the rule set's vocabulary its source feeds.
func (r *Reconciler) rows(ctx context.Context, tenantID string, it layer.Item) ([]reconcile.Evidence, error) {
	g, err := r.itemRows(ctx, tenantID, it)
	if err != nil {
		return nil, err
	}
	expires := expiry(it)
	out := make([]reconcile.Evidence, 0, len(g.mapped))
	for _, row := range g.mapped {
		out = append(out, reconcile.Evidence{ID: it.EvidenceID + "/" + row.Entity, Source: it.Source, Entity: row.Entity, Status: g.rowStatus(row), EffectiveAt: it.EffectiveAt.Unix(), ExpiresAt: expires,
			Required: true, Fields: row.Fields})
	}
	return out, nil
}

// ErrPayload marks an item whose stored payload is not the sealed one, is not one object or is not canonical: Classify turns it into an UNKNOWN row (A260); Facts, which a
// confirmation's binding rests on, refuses it (A365 refined).
var ErrPayload = errors.New(prefix + "the stored payload is not usable as the sealed one")

// Facts gives the mapped, normalized facts of one evidence item (A365 refined, REQ-TRU-009, REQ-CTX-001, INV-25): one fact per row the evidence mapping gives for the item's source,
// each with the status Classify gives the row (the item's, or UNKNOWN when a KNOWN item's payload gave the row no field) and the typed fields the payload gave. It is the same read
// and the same mapping as Classify's (itemRows), so the facts are exactly what the decision rests on; a field the mapping does not look at is not in them. Their digest
// (snapshot.FactsDigest) is what a confirmation's binding holds for the item.
//
// It needs no time and no freshness limit of the item, and no clock. Where Classify turns a payload that is not the sealed one into an UNKNOWN row, Facts refuses it with ErrPayload
// (the cause is kept, never printed): a binding cannot rest on a payload that cannot be verified. A store or context failure is ErrRead; an item that cannot be used (no tenant, no
// evidence id, no source, a status that is not one, a source the mapping does not know) is ErrItem. On any error the facts are nil.
func (r *Reconciler) Facts(ctx context.Context, tenantID string, it layer.Item) ([]snapshot.Fact, error) {
	if _, ok := statuses[it.Status]; !ok || tenantID == "" || it.EvidenceID == "" || it.Source == "" {
		return nil, ErrItem
	}
	g, err := r.itemRows(ctx, tenantID, it)
	if err != nil {
		return nil, err
	}
	if g.unsealed != nil {
		return nil, &opError{kind: ErrPayload, cause: g.unsealed}
	}
	out := make([]snapshot.Fact, 0, len(g.mapped))
	for _, row := range g.mapped {
		fields := make(map[string]snapshot.FactValue, len(row.Fields))
		for name, v := range row.Fields {
			fv, ok := factValue(v)
			if !ok {
				return nil, ErrItem
			}
			fields[name] = fv
		}
		out = append(out, snapshot.Fact{Entity: row.Entity, Status: factStatusName(g.rowStatus(row), it), Fields: fields})
	}
	return out, nil
}

// factStatusName is the contract's name of a row's status: the item's own, or UNKNOWN when the row was demoted.
func factStatusName(rowStatus reconcile.FactStatus, it layer.Item) string {
	if rowStatus == reconcile.Unknown {
		return string(contracts.EnumsV1FactStatusUNKNOWN)
	}
	return string(it.Status)
}

// factValue is a mapped value as a typed fact value with canonical text. A null value, or a kind this does not know, is not a fact.
func factValue(v predicate.Value) (snapshot.FactValue, bool) {
	switch v.Kind {
	case predicate.KindEnum:
		return snapshot.FactValue{Kind: snapshot.KindEnum, Text: v.S}, true
	case predicate.KindString:
		return snapshot.FactValue{Kind: snapshot.KindString, Text: v.S}, true
	case predicate.KindReference:
		return snapshot.FactValue{Kind: snapshot.KindReference, Text: v.S}, true
	case predicate.KindBoolean:
		return snapshot.FactValue{Kind: snapshot.KindBoolean, Text: strconv.FormatBool(v.B)}, true
	case predicate.KindInteger:
		return snapshot.FactValue{Kind: snapshot.KindInteger, Text: strconv.FormatInt(v.I, 10)}, true
	case predicate.KindTimestamp:
		return snapshot.FactValue{Kind: snapshot.KindTimestamp, Text: strconv.FormatInt(v.I, 10)}, true
	case predicate.KindDuration:
		return snapshot.FactValue{Kind: snapshot.KindDuration, Text: strconv.FormatInt(v.I, 10)}, true
	}
	return snapshot.FactValue{}, false
}

// expiry is the end of the item's effective freshness: its read time plus the limit AssignStatus applied (the stricter of the source's and the profile's, INV-23), so no clock is needed for it.
func expiry(it layer.Item) int64 {
	return it.ReadAt.Add(time.Duration(it.EffectiveFreshnessSeconds) * time.Second).Unix()
}

// reconciliationID is `rec_` and 26 letters from the SHA-256 of everything the classification depends on: the rule set, the mapping, the clock reading it was computed at, and for each sorted item its
// id, source, payload hash, status, read time, effective time and freshness limit (INV-21, INV-23). So the same inputs give the same id and a different clock reading, expiry or source gives another:
// it names what was classified. It holds no digit, so the Record's guard against digit runs never meets one. The contract allows 2 to 32 letters or digits.
func (r *Reconciler) reconciliationID(sorted []layer.Item, now time.Time) string {
	h := sha256.New()
	put := func(s string) {
		var n [4]byte
		binary.BigEndian.PutUint32(n[:], uint32(len(s)))
		h.Write(n[:])
		h.Write([]byte(s))
	}
	put(r.c.RuleSet.ID())
	put(r.c.Mapping.ID())
	put(strconv.FormatInt(now.UnixNano(), 10))
	for _, it := range sorted {
		put(it.EvidenceID)
		put(it.Source)
		put(it.PayloadHash)
		put(string(it.Status))
		put(strconv.FormatInt(it.ReadAt.UnixNano(), 10))
		put(strconv.FormatInt(it.EffectiveAt.UnixNano(), 10))
		put(strconv.FormatInt(it.EffectiveFreshnessSeconds, 10))
	}
	number, base, letters := new(big.Int).SetBytes(h.Sum(nil)), big.NewInt(26), make([]byte, 26)
	for i := range letters {
		var rest big.Int
		number.DivMod(number, base, &rest)
		letters[i] = byte('a' + rest.Int64())
	}
	return "rec_" + string(letters)
}
