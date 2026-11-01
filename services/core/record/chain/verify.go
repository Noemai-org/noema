package chain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

// BreakKind says how a chain failed verification.
type BreakKind string

// The ways a chain can break.
const (
	BreakSeqGap      BreakKind = "SEQ_GAP"      // an event is missing or inserted
	BreakPrevHash    BreakKind = "PREV_HASH"    // an event does not extend the one before it
	BreakHash        BreakKind = "HASH"         // the stored hash is not the hash of the stored event
	BreakColumns     BreakKind = "COLUMNS"      // a projection column differs from the hashed event
	BreakPayloadHash BreakKind = "PAYLOAD_HASH" // the stored payload is not the object payload_hash commits to
	BreakSchema      BreakKind = "SCHEMA"       // the event is not a record-event.v3 event (its schema_version, emitter or principal is not one this verifier accepts)
)

// BreakError is the first break found. Verification stops there.
type BreakError struct {
	TenantID string
	Seq      int64
	Kind     BreakKind
	Detail   string
}

func (e *BreakError) Error() string {
	return fmt.Sprintf("record: chain break for %s at seq %d: %s (%s)", e.TenantID, e.Seq, e.Kind, e.Detail)
}

// Queryer is what Verify needs: a transaction or a pool.
type Queryer interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// Row is one stored event exactly as record_read_chain returns it: the hashed body, the projection columns and the
// payload stored beside the event (migration 0003).
type Row struct {
	Seq                                                             int64
	EventID, PrevHash, Hash, WrittenBy, Actor, EventType, ObjectRef string
	PayloadHash, CorrelationID, SchemaVersion, Body                 string
	Payload                                                         *string
}

// ReadChain returns a tenant's whole chain in chain order through record_read_chain, the one read path into the
// Record (migration 0003): the caller needs EXECUTE on that function (record_read) and no privilege on the tables.
// Run it inside a REPEATABLE READ transaction when what is verified must be what is served (REQ-AUD-003).
func ReadChain(ctx context.Context, q Queryer, tenantID string) ([]Row, error) {
	if _, ok := ctx.Deadline(); !ok {
		return nil, errors.New(errPrefix + "the context needs a deadline")
	}
	rows, err := q.Query(ctx, `SELECT seq, event_id, prev_hash, hash, written_by, actor, event_type, object_ref, payload_hash,
		correlation_id, schema_version, body, payload FROM record_read_chain($1)`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("%sread chain: %w", errPrefix, err)
	}
	defer rows.Close()
	var out []Row
	for rows.Next() {
		var r Row
		if err := rows.Scan(&r.Seq, &r.EventID, &r.PrevHash, &r.Hash, &r.WrittenBy, &r.Actor, &r.EventType, &r.ObjectRef, &r.PayloadHash,
			&r.CorrelationID, &r.SchemaVersion, &r.Body, &r.Payload); err != nil {
			return nil, fmt.Errorf("%sscan: %w", errPrefix, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("%sread chain: %w", errPrefix, err)
	}
	return out, nil
}

// Verify recomputes every hash of a tenant's chain from genesis, in order, from the stored event bodies, and
// checks the projection columns and the stored payloads against them (REQ-AUD-001, REQ-AUD-005). It returns
// the number of events checked and a *BreakError at the first break. The chain verifier job (REC-05, REC-06)
// and reconstruction (REC-03) call this; it reads only and never repairs.
func Verify(ctx context.Context, q Queryer, tenantID string) (int64, error) {
	rows, err := ReadChain(ctx, q, tenantID)
	if err != nil {
		return 0, err
	}
	return VerifyRows(tenantID, rows)
}

// VerifyRows is Verify over rows already read with ReadChain, so a caller can verify and then serve the same rows.
func VerifyRows(tenantID string, rows []Row) (int64, error) {
	prev := GenesisHash(tenantID)
	var want, checked int64 = 1, 0
	for _, r := range rows {
		brk := func(k BreakKind, d string) error {
			return &BreakError{TenantID: tenantID, Seq: r.Seq, Kind: k, Detail: d}
		}
		if r.Seq != want {
			return checked, brk(BreakSeqGap, fmt.Sprintf("expected seq %d", want))
		}
		if r.PrevHash != prev {
			return checked, brk(BreakPrevHash, "prev_hash is not the hash of the previous event")
		}
		var body map[string]any
		dec := json.NewDecoder(stringsReader(r.Body))
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			return checked, brk(BreakHash, "the stored body is not JSON")
		}
		if r.SchemaVersion != SchemaVersion || body["schema_version"] != SchemaVersion {
			return checked, brk(BreakSchema, "schema_version is not "+SchemaVersion)
		}
		if !emitters[r.WrittenBy] || !validActor(r.Actor, r.WrittenBy) {
			return checked, brk(BreakSchema, "written_by or actor is not an emitter or principal of PERD v1.17 section 32.1")
		}
		for k, v := range map[string]string{"event_id": r.EventID, "prev_hash": r.PrevHash, "written_by": r.WrittenBy, "actor": r.Actor, "event_type": r.EventType,
			"object_ref": r.ObjectRef, "payload_hash": r.PayloadHash, "correlation_id": r.CorrelationID, "tenant_id": tenantID} {
			if body[k] != v {
				return checked, brk(BreakColumns, k+" differs between the column and the hashed event")
			}
		}
		if n, ok := body["seq"].(json.Number); !ok || n.String() != fmt.Sprint(r.Seq) {
			return checked, brk(BreakColumns, "seq differs between the column and the hashed event")
		}
		got, err := Hash(r.PrevHash, body)
		if err != nil {
			return checked, brk(BreakHash, err.Error())
		}
		if got != r.Hash {
			return checked, brk(BreakHash, "the stored hash is not the hash of the stored event")
		}
		if r.Payload != nil {
			var p any
			pd := json.NewDecoder(stringsReader(*r.Payload))
			pd.UseNumber()
			if err := pd.Decode(&p); err != nil {
				return checked, brk(BreakPayloadHash, "the stored payload is not JSON")
			}
			canon, err := crypto.CanonicalJSON(p)
			if err != nil {
				return checked, brk(BreakPayloadHash, "the stored payload cannot be hashed: "+err.Error())
			}
			if crypto.Hash(canon) != r.PayloadHash {
				return checked, brk(BreakPayloadHash, "the stored payload does not match payload_hash")
			}
		}
		prev = r.Hash
		want++
		checked++
	}
	return checked, nil
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
