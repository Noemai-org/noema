package snapshot_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/snapshot"
	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Tests for the sealer and the nightly recompute (EVD-03 slice 2): REQ-CTX-004, INV-17, REQ-AUD-001 (the Record in the same transaction), D9.
// All data is SYNTHETIC.

func (w *world) sealer() *snapshot.Sealer {
	clk := clock.NewManual(time.Date(2026, 11, 5, 9, 1, 0, 0, time.UTC))
	g := ids.New(clk, nil)
	return &snapshot.Sealer{Rec: &chain.Appender{Clock: clk, IDs: g}, Clock: clk, IDs: g}
}

func (w *world) seal(t *testing.T, s *snapshot.Sealer, tenant string, evidenceIDs []string) (snapshot.Sealed, error) {
	t.Helper()
	tx, err := w.trust[tenant].Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := s.Seal(ctx(t), tx, snapshot.Input{TenantID: tenant, CorrelationID: "corr_synthetic", EvidenceIDs: evidenceIDs, ReconciliationID: "rec_synthetic", Classification: "CONSISTENT"})
	if err != nil {
		_ = tx.Rollback(ctx(t))
		return snapshot.Sealed{}, err
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	return sealed, nil
}

// REQ-CTX-004, INV-17, REQ-AUD-001: the sealer sorts the set, hashes it, stores it and appends CONTEXT_SNAPSHOT_SEALED in one transaction.
func TestTheSealerStoresTheSnapshotAndTheRecordEventTogether(t *testing.T) {
	w := newWorld(t)
	a1, a2, a3 := w.item(t, tenantA, 1), w.item(t, tenantA, 2), w.item(t, tenantA, 3)
	sealed, err := w.seal(t, w.sealer(), tenantA, []string{a3, a1, a2})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{a1, a2, a3}; strings.Join(sealed.EvidenceIDs, ",") != strings.Join(want, ",") {
		t.Errorf("evidence_ids = %v, want sorted %v", sealed.EvidenceIDs, want)
	}
	if sealed.EvidenceSetHash != honest(t, 1, 2, 3) || sealed.SchemaVersion != "1.0" {
		t.Errorf("hash %s schema_version %q, want %s", sealed.EvidenceSetHash, sealed.SchemaVersion, honest(t, 1, 2, 3))
	}
	if !strings.HasPrefix(sealed.SnapshotID, "evs_") || sealed.TenantID != tenantA || sealed.Reconciliation.ReconciliationID != "rec_synthetic" {
		t.Errorf("sealed = %+v", sealed)
	}
	var storedHash string
	var storedIDs []string
	if err := w.admin.QueryRow(ctx(t), `SELECT evidence_set_hash, evidence_ids FROM context_snapshot WHERE snapshot_id = $1`, sealed.SnapshotID).Scan(&storedHash, &storedIDs); err != nil {
		t.Fatal(err)
	}
	if storedHash != sealed.EvidenceSetHash || strings.Join(storedIDs, ",") != strings.Join(sealed.EvidenceIDs, ",") {
		t.Errorf("stored %s %v disagree with the sealed document", storedHash, storedIDs)
	}
	var n int
	var objectRef, writtenBy string
	var payload []byte
	if err := w.admin.QueryRow(ctx(t), `SELECT count(*), max(object_ref), max(written_by), max(payload::text)::bytea FROM record_event WHERE event_type = 'CONTEXT_SNAPSHOT_SEALED' AND tenant_id = $1`, tenantA).Scan(&n, &objectRef, &writtenBy, &payload); err != nil {
		t.Fatal(err)
	}
	if n != 1 || objectRef != sealed.SnapshotID || writtenBy != "core-trust" {
		t.Errorf("Record: %d events, object_ref %q, written_by %q", n, objectRef, writtenBy)
	}
	var doc snapshot.Sealed
	if err := json.Unmarshal(payload, &doc); err != nil || doc.EvidenceSetHash != sealed.EvidenceSetHash || doc.SnapshotID != sealed.SnapshotID {
		t.Errorf("the Record payload does not carry the sealed document: %v %s", err, payload)
	}
}

// INV-17: the sealer never seals the empty set, and never seals a set the database would not accept; nothing is left behind.
func TestTheSealerRefusesWhatItMustNotSeal(t *testing.T) {
	w := newWorld(t)
	a1 := w.item(t, tenantA, 1)
	b3 := w.item(t, tenantB, 3)
	for name, in := range map[string][]string{
		"the empty set":             {},
		"no set":                    nil,
		"a repeated id":             {a1, a1},
		"an id that does not exist": {a1, "evr_aa99"},
		"another tenant's evidence": {a1, b3},
		"an empty id":               {a1, ""},
	} {
		_, err := w.seal(t, w.sealer(), tenantA, in)
		if err == nil {
			t.Errorf("%s: sealed", name)
		}
		if name == "the empty set" || name == "no set" {
			if !errors.Is(err, snapshot.ErrEmptySet) {
				t.Errorf("%s: want ErrEmptySet, got %v", name, err)
			}
		}
	}
	if n := count(t, w.admin, `SELECT count(*) FROM context_snapshot`); n != 0 {
		t.Errorf("%d snapshots left behind", n)
	}
	if n := count(t, w.admin, `SELECT count(*) FROM record_event WHERE event_type = 'CONTEXT_SNAPSHOT_SEALED'`); n != 0 {
		t.Errorf("%d CONTEXT_SNAPSHOT_SEALED events left behind", n)
	}
}

// REQ-AUD-001, PERD 32.4: if the Record append fails, the snapshot is not stored (one transaction), and the caller gets the error.
func TestASnapshotIsNotStoredWhenTheRecordAppendFails(t *testing.T) {
	w := newWorld(t)
	a1 := w.item(t, tenantA, 1)
	s := w.sealer()
	s.Rec = &chain.Appender{Clock: s.Clock, IDs: failingIDs{}}
	if _, err := w.seal(t, s, tenantA, []string{a1}); err == nil {
		t.Fatal("sealed although the Record append failed")
	}
	if n := count(t, w.admin, `SELECT count(*) FROM context_snapshot`); n != 0 {
		t.Errorf("%d snapshots stored without a Record event", n)
	}
}

type failingIDs struct{}

func (failingIDs) ID(string) (string, error) { return "", errors.New("synthetic: no id") }

// INV-11: the sealer cannot be pointed at another tenant; the login decides.
func TestTheSealerCannotSealForAnotherTenant(t *testing.T) {
	w := newWorld(t)
	a1 := w.item(t, tenantA, 1)
	tx, err := w.trust[tenantB].Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx(t))) }()
	if _, err := w.sealer().Seal(ctx(t), tx, snapshot.Input{TenantID: tenantA, CorrelationID: "corr_synthetic", EvidenceIDs: []string{a1}, ReconciliationID: "rec_synthetic", Classification: "CONSISTENT"}); err == nil {
		t.Error("tenant B's Trust login sealed a tenant A snapshot")
	}
}

// D9: the recompute reads as evidence_verify, finds every honest snapshot OK, and names tenant and snapshot (never evidence, payloads or ids of items) when a hash disagrees.
func TestTheNightlyRecomputeFindsEveryHonestSnapshotOK(t *testing.T) {
	w := newWorld(t)
	a1, a2, b3 := w.item(t, tenantA, 1), w.item(t, tenantA, 2), w.item(t, tenantB, 3)
	s1, err := w.seal(t, w.sealer(), tenantA, []string{a1, a2})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := w.seal(t, w.sealer(), tenantB, []string{b3})
	if err != nil {
		t.Fatal(err)
	}
	res, err := snapshot.Recompute(ctx(t), testpg.EvidenceVerifyLogin(t, w.dsn))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, r := range res {
		got[r.TenantID+"/"+r.SnapshotID] = r.Status
	}
	want := map[string]string{tenantA + "/" + s1.SnapshotID: snapshot.StatusOK, tenantB + "/" + s2.SnapshotID: snapshot.StatusOK}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("recompute = %v, want %v", got, want)
	}
}

// D9, REQ-CTX-004: a stored hash that no longer matches the stored items, and an item that is gone, are found and named. The tampering is done as the
// administrator with the immutability triggers off, which no runtime login can do.
func TestTheNightlyRecomputeFindsTamperingAndGaps(t *testing.T) {
	w := newWorld(t)
	a1, a2 := w.item(t, tenantA, 1), w.item(t, tenantA, 2)
	b3, b4 := w.item(t, tenantB, 3), w.item(t, tenantB, 4)
	honestSnap, err := w.seal(t, w.sealer(), tenantA, []string{a1, a2})
	if err != nil {
		t.Fatal(err)
	}
	changed, err := w.seal(t, w.sealer(), tenantB, []string{b3})
	if err != nil {
		t.Fatal(err)
	}
	gone, err := w.seal(t, w.sealer(), tenantB, []string{b4})
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`ALTER TABLE evidence_item DISABLE TRIGGER USER`,
		fmt.Sprintf(`UPDATE evidence_item SET payload_hash = '%s' WHERE evidence_id = '%s'`, hashN(777), b3),
		fmt.Sprintf(`DELETE FROM evidence_item WHERE evidence_id = '%s'`, b4),
		`ALTER TABLE evidence_item ENABLE TRIGGER USER`,
	} {
		if _, err := w.admin.Exec(ctx(t), q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	res, err := snapshot.Recompute(ctx(t), testpg.EvidenceVerifyLogin(t, w.dsn))
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]snapshot.Result{}
	for _, r := range res {
		byID[r.SnapshotID] = r
	}
	if r := byID[honestSnap.SnapshotID]; r.Status != snapshot.StatusOK {
		t.Errorf("the untouched snapshot: %+v", r)
	}
	if r := byID[changed.SnapshotID]; r.Status != snapshot.StatusMismatch || r.TenantID != tenantB || r.StoredHash != changed.EvidenceSetHash || r.RecomputedHash == "" || r.RecomputedHash == r.StoredHash {
		t.Errorf("the changed payload hash: %+v", r)
	}
	if r := byID[gone.SnapshotID]; r.Status != snapshot.StatusMissingEvidence || r.TenantID != tenantB {
		t.Errorf("the missing item: %+v", r)
	}
	if !snapshot.AnyFailure(res) {
		t.Error("AnyFailure is false although two snapshots failed")
	}
}

// D9 and noema#222: the recompute's output carries the tenant, the snapshot and digests, and nothing else: no evidence id, no payload, no correlation.
func TestTheRecomputeOutputCarriesDigestsAndIdentifiersOnly(t *testing.T) {
	w := newWorld(t)
	a1 := w.item(t, tenantA, 1)
	if _, err := w.seal(t, w.sealer(), tenantA, []string{a1}); err != nil {
		t.Fatal(err)
	}
	res, err := snapshot.Recompute(ctx(t), testpg.EvidenceVerifyLogin(t, w.dsn))
	if err != nil || len(res) != 1 {
		t.Fatalf("%v %v", res, err)
	}
	raw, err := json.Marshal(res[0])
	if err != nil {
		t.Fatal(err)
	}
	var keys []string
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	if want := "recomputed_hash,snapshot_id,status,stored_hash,tenant_id"; strings.Join(keys, ",") != want {
		t.Errorf("result keys = %v, want %s", keys, want)
	}
	for _, leak := range []string{a1, "payload-body-must-not-leak", "corr_synthetic", "fin.src.core_ledger"} {
		if strings.Contains(string(raw), leak) {
			t.Errorf("the result contains %q", leak)
		}
	}
}

// D9: the recompute is refused by the database when it runs as a login that is not evidence_verify (here a power login sees one tenant only and must not be taken for a full check).
func TestTheRecomputeFailsClosedWhenItCannotReadEverything(t *testing.T) {
	w := newWorld(t)
	if _, err := snapshot.Recompute(ctx(t), w.trust[tenantA]); err == nil {
		t.Error("the recompute ran as a tenant's Trust login and reported a result for one tenant as if it were the whole")
	}
}
