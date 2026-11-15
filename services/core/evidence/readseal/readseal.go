package readseal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/understanding/intentstore"
)

// Intents reads one admitted intent; intentstore.Reader is the real one.
type Intents interface {
	Get(ctx context.Context, intentID string, version int) (intentstore.Admitted, error)
}

// Transactions reads one transaction through the read ports and records the evidence; portread.Reader is the real one.
type Transactions interface {
	ReadTransaction(ctx context.Context, scope adapters.Scope, correlationID, transactionRef string) ([]layer.Item, error)
}

// Classified is the state reconciliation of one evidence set, as the snapshot records it.
type Classified struct{ ReconciliationID, Classification string }

// Reconciler classifies the evidence items just read. It has no default: an adapter without one is not wired.
type Reconciler interface {
	Classify(ctx context.Context, tenantID string, items []layer.Item) (Classified, error)
}

// Sealer seals a snapshot in the caller's transaction; snapshot.Sealer is the real one.
type Sealer interface {
	Seal(ctx context.Context, tx pgx.Tx, in snapshot.Input) (snapshot.Sealed, error)
}

// TxSource begins a transaction of the tenant's Trust login; *pgxpool.Pool is the real one.
type TxSource interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Errors of the adapter. Callers compare with errors.Is.
var (
	ErrNotWired      = errors.New("readseal: the adapter is not fully wired")
	ErrIntentRef     = errors.New("readseal: the intent reference is not intent_id@version")
	ErrTenant        = errors.New("readseal: the session or the intent is not of this adapter's tenant")
	ErrNoTransaction = errors.New("readseal: the intent names no resolved transaction")
	ErrNoDeadline    = errors.New("readseal: the context needs a deadline")
	ErrIncomplete    = errors.New("readseal: the Reconciler's answer is not a reconciliation ID and a classification of the shape a snapshot records")
)

// Adapter implements orchestrator.Evidence for one tenant.
type Adapter struct {
	TenantID string
	// TransactionSlot and ResourceKind come from the pack: the name of the intent's slot that holds the transaction, and the resource kind its resolved reference
	// must have (<vertical>.<kind>). Nothing here names an industry (INV-14); an adapter without them is not wired.
	TransactionSlot string
	ResourceKind    string
	Intents         Intents
	Transactions    Transactions
	Reconciler      Reconciler
	Sealer          Sealer
	Tx              TxSource
	Timeout         time.Duration // the deadline of the seal transaction; from configuration, zero refuses every read (AGENT-16)
}

var (
	intentRefRe = regexp.MustCompile(`^(int_[0-9A-Za-z_]{2,64})@([1-9][0-9]{0,8})$`)
	slotNameRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	kindRe      = regexp.MustCompile(`^[a-z][a-z0-9]*\.[a-z][a-z0-9_]*$`)
	recordIDRe  = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`) // the portread reader's own rule for a transaction reference
	// The shapes migration 0020 gives context_snapshot (reconciliation_id, reconciliation_classification). The database checks the shape only, not the closed set of
	// classifications: that set is the Reconciler's contract (PERD 22.3), and a real Reconciler returns one of its own constants.
	reconciliationRe = regexp.MustCompile(`^rec_[0-9A-Za-z]{2,32}$`)
	classificationRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
)

func (a *Adapter) configured() error {
	switch {
	case a == nil, a.TenantID == "", a.Intents == nil, a.Transactions == nil, a.Reconciler == nil, a.Sealer == nil, a.Tx == nil, a.Timeout <= 0,
		!slotNameRe.MatchString(a.TransactionSlot), !kindRe.MatchString(a.ResourceKind):
		return ErrNotWired
	}
	return nil
}

// Read reads the transaction the admitted intent names, as the session, has the items classified, seals the snapshot and returns its ID. Nothing is read for an
// intent or a session of another tenant, a malformed reference, or an intent whose transaction was never resolved. A source that could not answer is not an
// error: its item says so, the snapshot is sealed over it, and Unavailable is true (INV-19); Trust decides what that means.
func (a *Adapter) Read(ctx context.Context, s orchestrator.Session, intentRef string) (orchestrator.EvidenceRef, error) {
	if err := a.configured(); err != nil {
		return orchestrator.EvidenceRef{}, err
	}
	if _, ok := ctx.Deadline(); !ok {
		return orchestrator.EvidenceRef{}, ErrNoDeadline
	}
	if s.TenantID == "" || s.PrincipalID == "" {
		return orchestrator.EvidenceRef{}, orchestrator.ErrNoSession
	}
	if s.TenantID != a.TenantID {
		return orchestrator.EvidenceRef{}, ErrTenant
	}
	m := intentRefRe.FindStringSubmatch(intentRef)
	if m == nil {
		return orchestrator.EvidenceRef{}, ErrIntentRef
	}
	version, err := strconv.Atoi(m[2])
	if err != nil {
		return orchestrator.EvidenceRef{}, ErrIntentRef
	}
	admitted, err := a.Intents.Get(ctx, m[1], version)
	if err != nil {
		return orchestrator.EvidenceRef{}, fmt.Errorf("readseal: read the intent: %w", err)
	}
	// The intent reader's login binds the tenant in the database; nothing binds the principal there, so it is checked here (INV-12, review of noema#548). An intent of another
	// tenant or of another principal, and one that names no principal, answer exactly as an intent that does not exist: the caller learns nothing about what exists.
	if admitted.TenantID != a.TenantID || !ownedBy(admitted.JSON, s.PrincipalID) {
		return orchestrator.EvidenceRef{}, fmt.Errorf("readseal: read the intent: %w", intentstore.ErrNotFound)
	}
	txRef, err := a.transaction(admitted.JSON)
	if err != nil {
		return orchestrator.EvidenceRef{}, err
	}
	items, err := a.Transactions.ReadTransaction(ctx, adapters.Scope{TenantID: s.TenantID, PrincipalID: s.PrincipalID}, admitted.CorrelationID, txRef)
	if err != nil {
		return orchestrator.EvidenceRef{}, fmt.Errorf("readseal: read the transaction: %w", err)
	}
	if len(items) == 0 {
		return orchestrator.EvidenceRef{}, fmt.Errorf("readseal: nothing was read: %w", snapshot.ErrEmptySet)
	}
	class, err := a.Reconciler.Classify(ctx, s.TenantID, items)
	if err != nil {
		return orchestrator.EvidenceRef{}, fmt.Errorf("readseal: classify: %w", err)
	}
	if !reconciliationRe.MatchString(class.ReconciliationID) || !classificationRe.MatchString(class.Classification) {
		return orchestrator.EvidenceRef{}, ErrIncomplete
	}
	ids, unavailable := make([]string, len(items)), false
	for i, it := range items {
		ids[i] = it.EvidenceID
		switch it.Status {
		case contracts.EnumsV1FactStatusUNAVAILABLE, contracts.EnumsV1FactStatusUNKNOWN, contracts.EnumsV1FactStatusUNAUTHORIZED:
			unavailable = true
		}
	}
	sealed, err := a.seal(ctx, snapshot.Input{TenantID: s.TenantID, CorrelationID: admitted.CorrelationID, EvidenceIDs: ids, ReconciliationID: class.ReconciliationID,
		Classification: class.Classification})
	if err != nil {
		return orchestrator.EvidenceRef{}, err
	}
	return orchestrator.EvidenceRef{SnapshotID: sealed.SnapshotID, Unavailable: unavailable}, nil
}

// ownedBy says that the stored intent names exactly the session's principal. Only the one member it needs is read, so it does not depend on the object's version; an
// intent that names none, or a principal that is not text, is not provably the caller's.
func ownedBy(doc []byte, principalID string) bool {
	var d struct {
		Principal struct {
			PrincipalID string `json:"principal_id"`
		} `json:"principal"`
	}
	return json.Unmarshal(doc, &d) == nil && d.Principal.PrincipalID != "" && d.Principal.PrincipalID == principalID
}

// transaction takes the transaction ID from the intent's resolved transaction slot. It reads only the two members it needs, so it does not depend on the intent
// object's version; anything it cannot read as a resolved reference of the configured kind is ErrNoTransaction, never a guess from a surface form or a candidate.
func (a *Adapter) transaction(doc []byte) (string, error) {
	var d struct {
		Slots map[string]struct {
			Status      string          `json:"status"`
			ResolvedRef json.RawMessage `json:"resolved_ref"`
		} `json:"slots"`
	}
	if err := json.Unmarshal(doc, &d); err != nil {
		return "", ErrNoTransaction
	}
	slot, ok := d.Slots[a.TransactionSlot]
	if !ok || slot.Status != "RESOLVED" {
		return "", ErrNoTransaction
	}
	var ref string
	if err := json.Unmarshal(slot.ResolvedRef, &ref); err != nil {
		return "", ErrNoTransaction
	}
	id, found := strings.CutPrefix(ref, a.ResourceKind+":")
	if !found || !recordIDRe.MatchString(id) || strings.Contains(id, "..") {
		return "", ErrNoTransaction
	}
	return id, nil
}

// seal runs the Sealer in one transaction of the tenant's Trust login: the snapshot row and its Record event stand or fall together (REQ-AUD-001, EC-E16).
func (a *Adapter) seal(ctx context.Context, in snapshot.Input) (sealed snapshot.Sealed, err error) {
	sctx, cancel := context.WithTimeout(ctx, a.Timeout)
	defer cancel()
	tx, err := a.Tx.Begin(sctx)
	if err != nil {
		return snapshot.Sealed{}, fmt.Errorf("readseal: begin: %w", err)
	}
	committed := false
	defer func() {
		if committed {
			return
		}
		rctx, stop := context.WithTimeout(context.WithoutCancel(ctx), a.Timeout) // the rollback must run even when the caller's context is already over
		defer stop()
		if rbErr := tx.Rollback(rctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("readseal: rollback: %w", rbErr))
		}
	}()
	sealed, err = a.Sealer.Seal(sctx, tx, in)
	if err != nil {
		return snapshot.Sealed{}, fmt.Errorf("readseal: seal: %w", err)
	}
	if err := tx.Commit(sctx); err != nil {
		return snapshot.Sealed{}, fmt.Errorf("readseal: commit: %w", err)
	}
	committed = true
	return sealed, nil
}

var _ orchestrator.Evidence = (*Adapter)(nil)
