package chain

import (
	"context"
	"encoding/json"
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
	BreakSchema      BreakKind = "SCHEMA"       // the event is not a record-event.v2 event (its schema_version is not the one this verifier checks)
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

// Verify recomputes every hash of a tenant's chain from genesis, in order, from the stored event bodies, and
// checks the projection columns and the stored payloads against them (REQ-AUD-001, REQ-AUD-005). It returns
// the number of events checked and a *BreakError at the first break. The chain verifier job (REC-05, REC-06)
// calls this; it reads only and never repairs.
func Verify(ctx context.Context, q Queryer, tenantID string) (int64, error) {
	rows, err := q.Query(ctx, `SELECT seq, event_id, prev_hash, hash, written_by, event_type, object_ref, payload_hash,
		correlation_id, schema_version, body::text, payload::text FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenantID)
	if err != nil {
		return 0, fmt.Errorf("%sread chain: %w", errPrefix, err)
	}
	defer rows.Close()
	prev := GenesisHash(tenantID)
	var want, checked int64 = 1, 0
	for rows.Next() {
		var seq int64
		var eventID, prevHash, hash, writtenBy, eventType, objectRef, payloadHash, correlation, schemaVersion, bodyText string
		var payloadText *string
		if err := rows.Scan(&seq, &eventID, &prevHash, &hash, &writtenBy, &eventType, &objectRef, &payloadHash, &correlation, &schemaVersion, &bodyText, &payloadText); err != nil {
			return checked, fmt.Errorf("%sscan: %w", errPrefix, err)
		}
		brk := func(k BreakKind, d string) error {
			return &BreakError{TenantID: tenantID, Seq: seq, Kind: k, Detail: d}
		}
		if seq != want {
			return checked, brk(BreakSeqGap, fmt.Sprintf("expected seq %d", want))
		}
		if prevHash != prev {
			return checked, brk(BreakPrevHash, "prev_hash is not the hash of the previous event")
		}
		var body map[string]any
		dec := json.NewDecoder(stringsReader(bodyText))
		dec.UseNumber()
		if err := dec.Decode(&body); err != nil {
			return checked, brk(BreakHash, "the stored body is not JSON")
		}
		if schemaVersion != SchemaVersion || body["schema_version"] != SchemaVersion {
			return checked, brk(BreakSchema, "schema_version is not "+SchemaVersion)
		}
		for k, v := range map[string]string{"event_id": eventID, "prev_hash": prevHash, "written_by": writtenBy, "event_type": eventType,
			"object_ref": objectRef, "payload_hash": payloadHash, "correlation_id": correlation, "tenant_id": tenantID} {
			if body[k] != v {
				return checked, brk(BreakColumns, k+" differs between the column and the hashed event")
			}
		}
		if n, ok := body["seq"].(json.Number); !ok || n.String() != fmt.Sprint(seq) {
			return checked, brk(BreakColumns, "seq differs between the column and the hashed event")
		}
		got, err := Hash(prevHash, body)
		if err != nil {
			return checked, brk(BreakHash, err.Error())
		}
		if got != hash {
			return checked, brk(BreakHash, "the stored hash is not the hash of the stored event")
		}
		if payloadText != nil {
			var p any
			pd := json.NewDecoder(stringsReader(*payloadText))
			pd.UseNumber()
			if err := pd.Decode(&p); err != nil {
				return checked, brk(BreakPayloadHash, "the stored payload is not JSON")
			}
			canon, err := crypto.CanonicalJSON(p)
			if err != nil || crypto.Hash(canon) != payloadHash {
				return checked, brk(BreakPayloadHash, "the stored payload does not match payload_hash")
			}
		}
		prev = hash
		want++
		checked++
	}
	if err := rows.Err(); err != nil {
		return checked, fmt.Errorf("%sread chain: %w", errPrefix, err)
	}
	return checked, nil
}

func stringsReader(s string) *strings.Reader { return strings.NewReader(s) }
