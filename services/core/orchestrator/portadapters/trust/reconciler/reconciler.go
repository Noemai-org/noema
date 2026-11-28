// Package reconcilerport is the Trust intake's Reconciler (TRU-07; ruling A329; REQ-RES-001, REQ-RES-002, REQ-TRU-003, REQ-TRU-007, REQ-CTX-004; INV-11, INV-12, INV-16, INV-17, INV-19, INV-21;
// AGENT-13, AGENT-16). It implements intake.Reconciler: T10 asks it to reconcile the sealed evidence snapshot of a tenant, and it answers from that snapshot alone.
//
// It reads the snapshot as the tenant's own Trust login (Snapshots; Store is the real one), checks that the snapshot is intact (the evidence set hash is recomputed from the items read), classifies
// the stored items again with the Reconciler C2 built (evidence/reconciler: the real rule set and mapping, payloads read through payloadread) at the snapshot's own time, and returns the sealed
// classification only when the new one is the same. A different one is ErrDrift: a signal to the operator and a failure closed for the evaluation, never an overwrite of what was sealed. The pack's
// routing (the reason a classification escalates with, the capabilities allowed under a freeze) is data the composition passes in (Pack); this package names no industry term and no capability (INV-14).
//
// May import: the standard library, pgx (the Store only), evidence/layer, evidence/snapshot (the hash), evidence/readseal (the Classified type), evidence/reconciler, platform/clock, trust/intake and
// trust/reconcile.
//
// Must never: decide what a classification allows (Trust does, intake T10), write anything, read a store with a login that is not the tenant's, fall back to another tenant's login, call a model, take
// the tenant from the snapshot or an item (INV-12), default a setting (AGENT-16), or put a payload, a row, an ID, a tenant or a store's error text into an error (AGENT-13).
package reconcilerport

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/readseal"
	"github.com/Noemai-org/noema/services/core/evidence/reconciler"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/reconcile"
)

const prefix = "reconcilerport: "

// Errors of the adapter. Callers compare with errors.Is. Every text is fixed: none carries a payload, a row, an ID, a tenant or the text of a store's error.
var (
	// ErrConfig is returned for an adapter or a store that is missing a part, a timeout that is not positive, or a classifier that cannot be made.
	ErrConfig = errors.New(prefix + "the adapter is not configured")
	// ErrNoDeadline is returned for a context without a deadline: nothing is read.
	ErrNoDeadline = errors.New(prefix + "the context needs a deadline")
	// ErrSession is returned for a call that names no tenant.
	ErrSession = errors.New(prefix + "the call names no tenant")
	// ErrNotFound is the one answer for a snapshot that does not exist, one that is another tenant's and an ID that is not the shape of a snapshot ID (REQ-TRU-003).
	ErrNotFound = errors.New(prefix + "there is no such snapshot for this tenant")
	// ErrIntegrity is returned for a snapshot that is not intact: its items are not the set its hash covers, or it holds none (INV-17).
	ErrIntegrity = errors.New(prefix + "the snapshot is not intact")
	// ErrBadAnswer is returned for a value outside its contract: a sealed classification that is not one of the twelve, a reason code that is not a pack's.
	ErrBadAnswer = errors.New(prefix + "a value is outside its contract")
	// ErrDrift is returned when the classification made again from the snapshot's items is not the one sealed. It is a finding, and the evaluation fails closed (INV-21).
	ErrDrift = errors.New(prefix + "the snapshot's items no longer give the classification it was sealed with")
	// ErrRead is returned when the snapshot could not be read for a reason that is not the snapshot's (the store, the context). The cause is reachable with errors.Is and never printed.
	ErrRead = errors.New(prefix + "the snapshot could not be read")
	// ErrReclassify is returned when the Reconciler could not classify the items. The cause is reachable with errors.Is and never printed.
	ErrReclassify = errors.New(prefix + "the snapshot's items could not be classified")
)

// opError is a failure whose text is fixed: the cause stays reachable for errors.Is and errors.As and is never part of Error().
type opError struct{ kind, cause error }

func (e *opError) Error() string   { return e.kind.Error() }
func (e *opError) Unwrap() []error { return []error{e.kind, e.cause} }

// Sealed is one sealed snapshot as the tenant's own login reads it: the context-snapshot.v1 members the reconciliation needs, and the evidence items they name.
type Sealed struct {
	TenantID         string
	SnapshotID       string
	EvidenceIDs      []string // sorted ascending by bytes, no duplicates, as the sealer stored them
	ReconciliationID string
	Classification   string
	EvidenceSetHash  string
	SealedAt         time.Time
	Items            []layer.Item
}

// Snapshots reads a sealed snapshot of a tenant as that tenant's own Trust login. The answer for an absent snapshot and for another tenant's is the same: ErrNotFound.
type Snapshots interface {
	Sealed(ctx context.Context, tenantID, snapshotID string) (Sealed, error)
}

// Classifier classifies evidence items as a tenant; evidence/reconciler.Reconciler is the real one.
type Classifier interface {
	Classify(ctx context.Context, tenantID string, items []layer.Item) (readseal.Classified, error)
}

var _ Classifier = (*reconciler.Reconciler)(nil)

// ClassifierAt makes a Classifier whose clock reads at. The classification is a function of the items, the rule set and the clock (a rule can read now()), so it is made again at the time the
// snapshot names, not at the time of the question (INV-21).
type ClassifierAt func(at time.Time) (Classifier, error)

// ReconcilerAt is the ClassifierAt over the real Reconciler: the base configuration, with a clock that reads at. The base configuration's own clock is not used.
func ReconcilerAt(base reconciler.Config) ClassifierAt {
	return func(at time.Time) (Classifier, error) {
		if at.IsZero() {
			return nil, ErrConfig
		}
		c := base
		c.Clock = readingAt{at: at}
		r, err := reconciler.New(c)
		if err != nil {
			return nil, err
		}
		return r, nil
	}
}

// readingAt is a clock that always reads one instant, the snapshot's own. It is not the simulated clock of platform/clock (which only tests and replays build, TEN-05): it never advances, so the
// classification made again is a function of the sealed items alone.
type readingAt struct{ at time.Time }

// Now returns the instant the clock was made for.
func (r readingAt) Now() time.Time { return r.at }

// Pack is the pack's routing of a classification, as data the composition loads (SoT 51): this package holds neither a reason code nor a capability ID.
type Pack interface {
	// EscalateReason is the reason code `<vertical>.reason.<name>` the pack maps the classification to when it maps it to ESCALATE; empty when it maps it to nothing.
	EscalateReason(c reconcile.Classification) string
	// AllowedUnderFreeze is the capability IDs the pack allows on a frozen target (the descriptor's allowed_under_freeze).
	AllowedUnderFreeze() []string
}

// Config is what New needs. Every part is required and none has a default (AGENT-16). Timeout bounds one call; the composition takes it from NOEMA_TRUST_RECONCILE_TIMEOUT_MS.
type Config struct {
	Snapshots   Snapshots
	Classifiers ClassifierAt
	Pack        Pack
	Timeout     time.Duration
}

// Adapter implements intake.Reconciler.
type Adapter struct{ c Config }

var _ intake.Reconciler = (*Adapter)(nil)

// New refuses a configuration with a missing part or a timeout that is not positive.
func New(c Config) (*Adapter, error) {
	a := &Adapter{c: c}
	if !a.usable() {
		return nil, ErrConfig
	}
	return a, nil
}

func (a *Adapter) usable() bool {
	return a != nil && a.c.Snapshots != nil && a.c.Classifiers != nil && a.c.Pack != nil && a.c.Timeout > 0
}

var (
	snapshotIDRe = regexp.MustCompile(`\Aevs_[0-9A-Za-z]{2,32}\z`) // the shape migration 0020 checks
	reasonRe     = regexp.MustCompile(`\A[a-z][a-z0-9]*\.reason\.[a-z][a-z0-9_]*\z`)
)

// classifications is the closed set of PERD 22.3.
var classifications = map[reconcile.Classification]bool{
	reconcile.SuccessConfirmed: true, reconcile.RejectedConfirmed: true, reconcile.DebitPostedCreditUnconfirmed: true, reconcile.SettlementPending: true, reconcile.ReversalPending: true,
	reconcile.ReversedConfirmed: true, reconcile.PossibleDuplicate: true, reconcile.Partial: true, reconcile.ExecutionOutcomeUnknown: true, reconcile.ContradictoryState: true,
	reconcile.EventOrderUncertain: true, reconcile.InsufficientEvidence: true,
}

// Reconcile answers T10 for the tenant's sealed snapshot. The classification returned is the sealed one, and only when the same items give it again; a target is frozen exactly when the
// classification is CONTRADICTORY_STATE (REQ-RES-002); the escalation reason and the freeze allow-list are the pack's.
func (a *Adapter) Reconcile(ctx context.Context, tenantID, snapshotID string) (intake.Reconciliation, error) {
	none := intake.Reconciliation{}
	if !a.usable() {
		return none, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return none, ErrNoDeadline
	}
	if tenantID == "" {
		return none, ErrSession
	}
	if !snapshotIDRe.MatchString(snapshotID) {
		return none, ErrNotFound
	}
	ctx, cancel := context.WithTimeout(ctx, a.c.Timeout)
	defer cancel()
	s, err := a.c.Snapshots.Sealed(ctx, tenantID, snapshotID)
	switch {
	case errors.Is(err, ErrNotFound):
		return none, ErrNotFound
	case err != nil:
		return none, &opError{kind: ErrRead, cause: err}
	}
	if s.TenantID != tenantID || s.SnapshotID != snapshotID {
		return none, ErrNotFound
	}
	if err := verify(s); err != nil {
		return none, err
	}
	sealed := reconcile.Classification(s.Classification)
	if !classifications[sealed] {
		return none, ErrBadAnswer
	}
	waitUntil, err := a.reclassify(ctx, tenantID, s, sealed)
	if err != nil {
		return none, err
	}
	reason := a.c.Pack.EscalateReason(sealed)
	if reason != "" && !reasonRe.MatchString(reason) {
		return none, ErrBadAnswer
	}
	return intake.Reconciliation{Classification: sealed, Frozen: sealed == reconcile.ContradictoryState, AllowedUnderFreeze: append([]string(nil), a.c.Pack.AllowedUnderFreeze()...),
		EscalateReason: reason, WaitUntil: waitUntil}, nil
}

// reclassify classifies the snapshot's items again, at the snapshot's own time, and fails with ErrDrift when the answer is not the sealed classification. The time is the later of the seal time
// (kept to the millisecond) and the latest read time (kept to the microsecond), so that an item read within the same millisecond as the seal is not "read in the future"; both are the snapshot's.
func (a *Adapter) reclassify(ctx context.Context, tenantID string, s Sealed, sealed reconcile.Classification) (int64, error) {
	at := s.SealedAt
	for _, it := range s.Items {
		if it.ReadAt.After(at) {
			at = it.ReadAt
		}
	}
	cl, err := a.c.Classifiers(at)
	if err != nil {
		return 0, &opError{kind: ErrConfig, cause: err}
	}
	if cl == nil {
		return 0, ErrConfig
	}
	fresh, err := cl.Classify(ctx, tenantID, append([]layer.Item(nil), s.Items...))
	if err != nil {
		return 0, &opError{kind: ErrReclassify, cause: err}
	}
	if reconcile.Classification(fresh.Classification) != sealed {
		return 0, ErrDrift
	}
	return fresh.WaitUntil, nil
}

// verify checks that the snapshot is intact: it holds evidence (INV-17), the items read are exactly its ids (sorted and unique, as the sealer stored them: the items are compared in order with the ids), and the evidence set hash
// recomputed from their payload hashes is the stored one (REQ-CTX-004).
func verify(s Sealed) error {
	if s.SealedAt.IsZero() || len(s.EvidenceIDs) == 0 || len(s.Items) != len(s.EvidenceIDs) {
		return ErrIntegrity
	}
	items := append([]layer.Item(nil), s.Items...)
	sort.Slice(items, func(i, j int) bool { return items[i].EvidenceID < items[j].EvidenceID })
	pairs := make([]snapshot.Pair, len(items))
	for i, it := range items {
		if it.EvidenceID != s.EvidenceIDs[i] {
			return ErrIntegrity
		}
		pairs[i] = snapshot.Pair{EvidenceID: it.EvidenceID, PayloadHash: it.PayloadHash}
	}
	hash, err := snapshot.SetHash(pairs)
	if err != nil || hash != s.EvidenceSetHash {
		return ErrIntegrity
	}
	return nil
}
