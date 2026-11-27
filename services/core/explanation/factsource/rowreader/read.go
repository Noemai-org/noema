package rowreader

import (
	"context"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

var (
	// ErrConfig is returned for a reader or source built without what it needs.
	ErrConfig = errors.New("rowreader: it is not fully configured")
	// ErrNoDeadline is returned for a read whose context has no deadline.
	ErrNoDeadline = errors.New("rowreader: the read has no deadline")
	// ErrNoLogin is returned for a call that names no tenant, and for a tenant with no Trust login. It never falls back to another tenant's login.
	ErrNoLogin = errors.New("rowreader: no Trust login is configured for this tenant")
	// ErrNotFound is returned for a decision that is absent, another tenant's, or not the shape of a decision id: the answers are the same, so the tenant learns nothing about a decision it cannot see.
	ErrNotFound = errors.New("rowreader: the decision is not found")
	// ErrStore is returned when the database could not answer, or a stored document could not be read. The cause is reachable with errors.Is and is never printed: a driver's error can name a server or a login.
	ErrStore = errors.New("rowreader: the database could not answer")
	// ErrNoClassification is returned for a decision with no sealed snapshot: it has no classification to explain.
	ErrNoClassification = errors.New("rowreader: the decision has no sealed classification")
	// ErrMismatch is returned when rows that must agree do not: another correlation than the decision's, no admitted intent of the decision's hash, a grant with no capability, a receipt that is not an object of its own.
	ErrMismatch = errors.New("rowreader: the rows do not agree")
	// ErrAmbiguous is returned when a row that must be one is several: two receipts of a grant, two verdicts of one instant that disagree, two admitted intents of one hash.
	ErrAmbiguous = errors.New("rowreader: the rows are ambiguous")
)

// failure is an error with a fixed text and a cause that errors.Is can still find: a database or seam error can name a server, a login, a tenant or a row, so its text is never shown (AGENT-13).
type failure struct{ kind, cause error }

func (f *failure) Error() string { return f.kind.Error() }

func (f *failure) Unwrap() []error {
	if f.cause == nil {
		return []error{f.kind}
	}
	return []error{f.kind, f.cause}
}

func wrapped(kind, cause error) error { return &failure{kind: kind, cause: cause} }

// Rows is what the stored rows say about one decision.
type Rows struct {
	DecisionID     string
	CorrelationID  string // the decision's: every other row below names it
	Classification string // the sealed snapshot's reconciliation classification
	SubjectRef     string // the resolved subject of the decision's admitted intent, or empty
	Executions     []Execution
}

// Execution is one grant of the investigation with the receipt and the current verdict it led to. The receipt fields are empty for a grant that has no receipt, and Verdict and OutcomeID
// for a receipt with no verdict.
type Execution struct {
	GrantID      string
	CapabilityID string
	OwnGrant     bool // the grant was issued under the decision asked about
	ReceiptID    string
	State        string
	ExternalAck  *string
	StepParams   map[string]json.RawMessage // the signed receipt's step_params, absent when the receipt has none
	Verdict      string                     // the latest verdict of the receipt
	OutcomeID    string                     // the outcome definition that verdict names
}

// Beginner begins a transaction of one tenant's own Trust login; *pgxpool.Pool is the real one.
type Beginner interface {
	BeginTx(ctx context.Context, o pgx.TxOptions) (pgx.Tx, error)
}

// PoolsOf is the logins of cmd/core's per-tenant Trust pools as Beginners. A tenant with no pool has no entry, so it has no login.
func PoolsOf(pools map[string]*pgxpool.Pool) map[string]Beginner {
	out := make(map[string]Beginner, len(pools))
	for tenantID, p := range pools {
		if p != nil {
			out[tenantID] = p
		}
	}
	return out
}

var (
	tenantRe   = regexp.MustCompile(`\Aten_[0-9A-Za-z]{2,32}\z`)
	decisionRe = regexp.MustCompile(`\A[a-z]{2,6}_[0-9A-Za-z_]{2,64}\z`)
)

// Reader reads the rows over one Trust login per tenant (trust_rw holds SELECT on every table below and nothing is written; the database, through row-level security on the login's
// tenant, decides which rows a connection sees, migrations 0008, 0012, 0018 and 0020).
type Reader struct {
	pools       map[string]Beginner
	timeout     time.Duration
	subjectSlot string
}

// NewReader refuses no login at all, a tenant ID that is not one, a nil login, no timeout and no subject slot. The slot is the name the pack gives the Resource slot that is the
// subject of its intents (A520): configuration, never held here (INV-14).
func NewReader(pools map[string]Beginner, timeout time.Duration, subjectSlot string) (*Reader, error) {
	if len(pools) == 0 || timeout <= 0 || subjectSlot == "" {
		return nil, ErrConfig
	}
	own := make(map[string]Beginner, len(pools))
	for tenantID, p := range pools {
		if !tenantRe.MatchString(tenantID) || p == nil {
			return nil, ErrConfig
		}
		own[tenantID] = p
	}
	return &Reader{pools: own, timeout: timeout, subjectSlot: subjectSlot}, nil
}

const (
	decisionSQL = `SELECT json ->> 'correlation_id', json ->> 'intent_id', intent_hash, json ->> 'evidence_snapshot_id'
		FROM decision_record WHERE tenant_id = $1 AND decision_id = $2`
	snapshotSQL = `SELECT correlation_id, reconciliation_classification FROM context_snapshot WHERE tenant_id = $1 AND snapshot_id = $2`
	intentSQL   = `SELECT json FROM admitted_intent WHERE tenant_id = $1 AND intent_id = $2 AND intent_hash = $3 AND correlation_id = $4`
	grantsSQL   = `SELECT grant_id, decision_id, COALESCE(json ->> 'capability_id', '') FROM decision_grant WHERE tenant_id = $1 AND json ->> 'correlation_id' = $2 ORDER BY grant_id`
	receiptsSQL = `SELECT receipt_id, grant_id, execution_state, external_ack, json -> 'step_params', COALESCE(json ->> 'correlation_id', '')
		FROM execution_receipt WHERE tenant_id = $1 AND grant_id = ANY ($2) ORDER BY grant_id, attempt`
	verdictsSQL = `SELECT receipt_id, verdict, outcome_definition, correlation_id, verified_at
		FROM outcome_verdict WHERE tenant_id = $1 AND receipt_id = ANY ($2) ORDER BY receipt_id, verified_at DESC, verdict_id`
)

// Read reads the decision's rows in one read-only repeatable-read transaction of the tenant's login, so they are one view, and ends it by rollback. A decision that is absent, another
// tenant's, or not the shape of a decision id is ErrNotFound. The investigation's grants are those whose signed document names the decision's correlation, and a row that names another
// is ErrMismatch. A receipt's current verdict is its latest.
func (r *Reader) Read(ctx context.Context, tenantID, decisionID string) (Rows, error) {
	if r == nil || len(r.pools) == 0 {
		return Rows{}, ErrConfig
	}
	if _, ok := ctx.Deadline(); !ok {
		return Rows{}, ErrNoDeadline
	}
	pool, ok := r.pools[tenantID]
	if !ok || tenantID == "" {
		return Rows{}, ErrNoLogin
	}
	if !decisionRe.MatchString(decisionID) {
		return Rows{}, ErrNotFound
	}
	qctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := pool.BeginTx(qctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return Rows{}, wrapped(ErrStore, err)
	}
	defer func() {
		rctx, stop := context.WithTimeout(context.WithoutCancel(ctx), r.timeout) // the rollback must run when the read's own deadline has passed
		defer stop()
		_ = tx.Rollback(rctx) // a read-only transaction has nothing to lose: a failed rollback only returns the connection to the pool late
	}()
	return r.read(qctx, tx, tenantID, decisionID)
}

func (r *Reader) read(ctx context.Context, tx pgx.Tx, tenantID, decisionID string) (Rows, error) {
	out := Rows{DecisionID: decisionID}
	var intentID, intentHash string
	var snapshotID *string
	switch err := tx.QueryRow(ctx, decisionSQL, tenantID, decisionID).Scan(&out.CorrelationID, &intentID, &intentHash, &snapshotID); {
	case errors.Is(err, pgx.ErrNoRows):
		return Rows{}, ErrNotFound
	case err != nil:
		return Rows{}, wrapped(ErrStore, err)
	}
	if err := r.classification(ctx, tx, tenantID, snapshotID, &out); err != nil {
		return Rows{}, err
	}
	subject, err := r.subject(ctx, tx, tenantID, intentID, intentHash, out.CorrelationID)
	if err != nil {
		return Rows{}, err
	}
	out.SubjectRef = subject
	execs, err := executions(ctx, tx, tenantID, decisionID, out.CorrelationID)
	if err != nil {
		return Rows{}, err
	}
	out.Executions = execs
	return out, nil
}

func (r *Reader) classification(ctx context.Context, tx pgx.Tx, tenantID string, snapshotID *string, out *Rows) error {
	if snapshotID == nil || *snapshotID == "" {
		return ErrNoClassification
	}
	var corr string
	switch err := tx.QueryRow(ctx, snapshotSQL, tenantID, *snapshotID).Scan(&corr, &out.Classification); {
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNoClassification
	case err != nil:
		return wrapped(ErrStore, err)
	}
	if corr != out.CorrelationID {
		return ErrMismatch
	}
	return nil
}

// subject reads the one admitted intent of the decision's hash and takes the resolved reference of the pack's subject slot. An intent with no such slot, or one not resolved, has no subject.
func (r *Reader) subject(ctx context.Context, tx pgx.Tx, tenantID, intentID, intentHash, correlation string) (string, error) {
	rows, err := tx.Query(ctx, intentSQL, tenantID, intentID, intentHash, correlation)
	if err != nil {
		return "", wrapped(ErrStore, err)
	}
	docs, err := pgx.CollectRows(rows, pgx.RowTo[[]byte])
	if err != nil {
		return "", wrapped(ErrStore, err)
	}
	switch len(docs) {
	case 0:
		return "", ErrMismatch
	case 1:
	default:
		return "", ErrAmbiguous
	}
	var doc struct {
		Slots map[string]json.RawMessage `json:"slots"`
	}
	if err := json.Unmarshal(docs[0], &doc); err != nil {
		return "", wrapped(ErrStore, err)
	}
	raw, ok := doc.Slots[r.subjectSlot]
	if !ok {
		return "", nil
	}
	var slot contracts.IntentSlotV2ResourceSlot
	if slot.UnmarshalJSON(raw) != nil || slot.Status != contracts.IntentSlotV2SlotStatusRESOLVED || slot.ResolvedRef == nil {
		return "", nil
	}
	return string(*slot.ResolvedRef), nil
}

type receiptRow struct {
	id, state string
	ack       *string
	params    map[string]json.RawMessage
}

func executions(ctx context.Context, tx pgx.Tx, tenantID, decisionID, correlation string) ([]Execution, error) {
	rows, err := tx.Query(ctx, grantsSQL, tenantID, correlation)
	if err != nil {
		return nil, wrapped(ErrStore, err)
	}
	execs, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Execution, error) {
		var e Execution
		var dec string
		err := r.Scan(&e.GrantID, &dec, &e.CapabilityID)
		e.OwnGrant = dec == decisionID
		return e, err
	})
	if err != nil {
		return nil, wrapped(ErrStore, err)
	}
	if len(execs) == 0 {
		return execs, nil
	}
	grantIDs := make([]string, len(execs))
	for i, e := range execs {
		if e.CapabilityID == "" {
			return nil, ErrMismatch
		}
		grantIDs[i] = e.GrantID
	}
	receipts, err := receiptsOf(ctx, tx, tenantID, grantIDs, correlation)
	if err != nil {
		return nil, err
	}
	var receiptIDs []string
	for i := range execs {
		if rc, ok := receipts[execs[i].GrantID]; ok {
			execs[i].ReceiptID, execs[i].State, execs[i].ExternalAck, execs[i].StepParams = rc.id, rc.state, rc.ack, rc.params
			receiptIDs = append(receiptIDs, rc.id)
		}
	}
	if len(receiptIDs) == 0 {
		return execs, nil
	}
	verdicts, err := verdictsOf(ctx, tx, tenantID, receiptIDs, correlation)
	if err != nil {
		return nil, err
	}
	for i := range execs {
		if v, ok := verdicts[execs[i].ReceiptID]; ok && execs[i].ReceiptID != "" {
			execs[i].Verdict, execs[i].OutcomeID = v[0], v[1]
		}
	}
	return execs, nil
}

// receiptsOf reads the receipts of the grants, by grant. A grant has one receipt: a second is ErrAmbiguous (a retry needs a new grant, AGENT-08), and a receipt whose signed document
// names another correlation or whose step_params is not an object is ErrMismatch.
func receiptsOf(ctx context.Context, tx pgx.Tx, tenantID string, grantIDs []string, correlation string) (map[string]receiptRow, error) {
	rows, err := tx.Query(ctx, receiptsSQL, tenantID, grantIDs)
	if err != nil {
		return nil, wrapped(ErrStore, err)
	}
	defer rows.Close()
	out := map[string]receiptRow{}
	for rows.Next() {
		var rc receiptRow
		var grantID, corr string
		var params []byte
		if err := rows.Scan(&rc.id, &grantID, &rc.state, &rc.ack, &params, &corr); err != nil {
			return nil, wrapped(ErrStore, err)
		}
		if corr != correlation {
			return nil, ErrMismatch
		}
		if len(params) > 0 && string(params) != "null" {
			if json.Unmarshal(params, &rc.params) != nil {
				return nil, ErrMismatch
			}
		}
		if _, dup := out[grantID]; dup {
			return nil, ErrAmbiguous
		}
		out[grantID] = rc
	}
	if err := rows.Err(); err != nil {
		return nil, wrapped(ErrStore, err)
	}
	return out, nil
}

// verdictsOf reads the current verdict of each receipt, as its verdict and its outcome definition: the latest by verification time. Two at that instant that differ are ErrAmbiguous, and
// a verdict that names another correlation is ErrMismatch.
func verdictsOf(ctx context.Context, tx pgx.Tx, tenantID string, receiptIDs []string, correlation string) (map[string][2]string, error) {
	rows, err := tx.Query(ctx, verdictsSQL, tenantID, receiptIDs)
	if err != nil {
		return nil, wrapped(ErrStore, err)
	}
	defer rows.Close()
	out := map[string][2]string{}
	at := map[string]time.Time{}
	for rows.Next() {
		var receiptID, verdict, outcome, corr string
		var verifiedAt time.Time
		if err := rows.Scan(&receiptID, &verdict, &outcome, &corr, &verifiedAt); err != nil {
			return nil, wrapped(ErrStore, err)
		}
		if corr != correlation {
			return nil, ErrMismatch
		}
		cur, seen := out[receiptID]
		switch {
		case !seen:
			out[receiptID], at[receiptID] = [2]string{verdict, outcome}, verifiedAt
		case verifiedAt.Equal(at[receiptID]) && cur != [2]string{verdict, outcome}:
			return nil, ErrAmbiguous
		}
	}
	if err := rows.Err(); err != nil {
		return nil, wrapped(ErrStore, err)
	}
	return out, nil
}
