package policystore_test

// TRU-02 (REQ-TRU-005, REQ-AUD-001, REQ-AUD-003; INV-09, INV-21, AGENT-13; ruling on noema#771, INTERIM: relayed by the Founder in session, not yet committed, AGENTS section 2.3): the
// POLICY_PUBLISHED Record event carries document_hash, the same value the publication row stores, written in the same transaction. The payload of an event is free-form in
// contracts/schemas/record-event.v3.json, so no contract changes. The tests run on real PostgreSQL with the real migrations and the real Record signer. All data is SYNTHETIC.

import (
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/testpg"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// publishIn publishes in the caller's transaction and leaves it open.
func publishIn(t *testing.T, w *world, tx pgx.Tx, policyID, version, hash string) error {
	t.Helper()
	return w.store.Publish(ctx(t), tx, policystore.Request{TenantID: tenantA, CorrelationID: "corr_pub0001", PolicyID: policyID, Version: version, PublishedBy: "adm_synth01", DocumentHash: hash})
}

// testpgRecordRead is the tenant's own record_read login, the one the reconstruction reads as.
func testpgRecordRead(t *testing.T, w *world, tenant string) *pgxpool.Pool {
	t.Helper()
	return testpg.TenantLogin(t, w.admin, w.dsn, "record_read", tenant)
}

// A hash that holds a run of ten digits, with letters in its body: the shape of a real digest now and then (SYNTHETIC).
const digitRunDocHash = "sha256:1234567890" + "abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// eventHashes returns the document_hash member of the payload of every POLICY_PUBLISHED event of the tenant, in chain order, "" for an event without one.
func eventHashes(t *testing.T, w *world, tenant string) []string {
	t.Helper()
	rows, err := w.admin.Query(ctx(t), `SELECT coalesce(payload->>'document_hash', '') FROM record_event WHERE tenant_id = $1 AND event_type = 'POLICY_PUBLISHED' ORDER BY seq`, tenant)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var h string
		if err := rows.Scan(&h); err != nil {
			t.Fatal(err)
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// REQ-TRU-005, REQ-AUD-001: the event of a publication carries document_hash, equal to the value the publication row stores, byte for byte.
func TestThePublishedEventCarriesTheHashTheRowStores(t *testing.T) {
	w := newWorld(t)
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", otherDocHash); err != nil {
		t.Fatal(err)
	}
	stored := storedHash(t, w, tenantA, "fin.pol", "3.1")
	got := eventHashes(t, w, tenantA)
	if stored == nil || len(got) != 1 || got[0] != *stored || got[0] != otherDocHash {
		t.Fatalf("event hashes %v, stored %v, want one event carrying %s", got, stored, otherDocHash)
	}
	var payload string
	if err := w.admin.QueryRow(ctx(t), `SELECT payload::text FROM record_event WHERE tenant_id = $1 AND event_type = 'POLICY_PUBLISHED'`, tenantA).Scan(&payload); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"policy_id": "fin.pol"`, `"version": "3.1"`, `"published_by": "adm_synth01"`, `"document_hash": "` + otherDocHash + `"`} {
		if !strings.Contains(payload, want) {
			t.Errorf("payload %s lacks %s", payload, want)
		}
	}
}

// REQ-AUD-003, AGENT-13: the payload guard refuses a payload string with a run of more than four digits, but a typed `sha256:` and 64 hex value is exempt. A document hash with a
// ten-digit run in it goes through a real append, on the Record, whole.
func TestAHashWithALongDigitRunIsAcceptedByTheRecordAppend(t *testing.T) {
	w := newWorld(t)
	if len(digitRunDocHash) != len("sha256:")+64 {
		t.Fatalf("the test hash has length %d", len(digitRunDocHash))
	}
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", digitRunDocHash); err != nil {
		t.Fatalf("a well-formed hash with a digit run was refused: %v", err)
	}
	if got := eventHashes(t, w, tenantA); len(got) != 1 || got[0] != digitRunDocHash {
		t.Fatalf("event hashes %v, want [%s]", got, digitRunDocHash)
	}
}

// INV-09 (the Record first): a hash that is only digits is a well-formed hash by the pattern, but it is not a typed value for the guard (the body holds no letter), so the append is
// refused and the publication fails closed: no row, no event.
func TestAHashOfOnlyDigitsFailsClosedAtTheRecordAppend(t *testing.T) {
	w := newWorld(t)
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", "sha256:"+strings.Repeat("7", 64)); err == nil {
		t.Fatal("a hash of 64 digits was published")
	}
	if n := publicationCount(t, w); n != 0 {
		t.Fatalf("%d publications after a refused append", n)
	}
	if got := eventHashes(t, w, tenantA); len(got) != 0 {
		t.Fatalf("%d events after a refused append", len(got))
	}
}

// INV-09: the row and the event are one transaction. A publication that is rolled back leaves neither, and one that is committed leaves both.
func TestTheRowAndTheEventAreOneTransaction(t *testing.T) {
	w := newWorld(t)
	tx, err := w.trustA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := publishIn(t, w, tx, "fin.pol", "3.1", otherDocHash); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx(t)); err != nil {
		t.Fatal(err)
	}
	if n, ev := publicationCount(t, w), eventHashes(t, w, tenantA); n != 0 || len(ev) != 0 {
		t.Fatalf("after a rollback: %d publications, %d events, want none", n, len(ev))
	}
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", otherDocHash); err != nil {
		t.Fatal(err)
	}
	if n, ev := publicationCount(t, w), eventHashes(t, w, tenantA); n != 1 || len(ev) != 1 {
		t.Fatalf("after a commit: %d publications, %d events, want one of each", n, len(ev))
	}
}

// A publication that is not newer (the same version again, or a downgrade) publishes nothing: no second event, and the first event keeps its hash.
func TestARefusedPublicationWritesNoSecondEvent(t *testing.T) {
	w := newWorld(t)
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", otherDocHash); err != nil {
		t.Fatal(err)
	}
	for _, version := range []string{"3.1", "3.0"} {
		if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", version, testDocHash); err == nil {
			t.Fatalf("version %s was published again", version)
		}
	}
	if got := eventHashes(t, w, tenantA); len(got) != 1 || got[0] != otherDocHash {
		t.Fatalf("event hashes %v, want exactly [%s]", got, otherDocHash)
	}
}

// REQ-AUD-003: the reconstruction does not project POLICY_PUBLISHED (it is a control-plane event: the timeline only), so the new member is not served as a fact, and the
// reconstruction of the correlation still builds. A projection that started to serve the member would show here.
func TestTheReconstructionIsUnaffectedByTheHashInThePayload(t *testing.T) {
	w := newWorld(t)
	if err := w.publishHashed(t, w.trustA, tenantA, "fin.pol", "3.1", otherDocHash); err != nil {
		t.Fatal(err)
	}
	reader := testpgRecordRead(t, w, tenantA)
	body, err := reconstruct.Build(ctx(t), reader, tenantA, "corr_pub0001")
	if err != nil {
		t.Fatalf("reconstruct: %v", err)
	}
	s := string(body)
	if !strings.Contains(s, "Policy published (core-trust)") {
		t.Errorf("the timeline does not show the publication: %s", s)
	}
	if strings.Contains(s, "document_hash") || strings.Contains(s, otherDocHash) {
		t.Errorf("the reconstruction serves the document hash: %s", s)
	}
}
