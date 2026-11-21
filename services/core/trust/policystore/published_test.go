package policystore_test

// TRU-adapters-policies-clocks: the read of every version a tenant has published, with the time the database recorded it (REQ-TRU-005, REQ-TRU-007; INV-09, INV-21; D8).
// The Policies adapter of the intake needs the history, not only the newest version, because an AT_ADMISSION rule is read from the version in force when the intent was
// admitted. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// publishAt inserts a publication with a time of its own, as the tenant's Trust login (the guard still decides who may write and which version is higher).
func (w *world) publishAt(t *testing.T, p *pgxpool.Pool, tenant, policyID, version, at string) {
	t.Helper()
	_, err := p.Exec(ctx(t), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, published_at, document_hash)
		VALUES ($1, $2, $3, policy_version_key($3), 'adm_synth01', $4::timestamptz, $5)`, tenant, policyID, version, at, testDocHash)
	if err != nil {
		t.Fatalf("publish %s@%s at %s: %v", policyID, version, at, err)
	}
}

func published(t *testing.T, q policystore.Querier, tenant, policyID string) []policystore.Publication {
	t.Helper()
	got, err := policystore.Published(ctx(t), q, tenant, policyID)
	if err != nil {
		t.Fatalf("Published(%s, %s): %v", tenant, policyID, err)
	}
	return got
}

// Every version is returned, oldest first, as numbers (1.9 before 1.10) and not as text, each with its time in whole Unix seconds (the fraction is dropped, never rounded up:
// a version is not in force before the database recorded it).
func TestPublishedIsEveryVersionOldestFirstAsNumbersWithItsTimeInWholeSeconds(t *testing.T) {
	w := newWorld(t)
	w.publishAt(t, w.trustA, tenantA, "fin.pol", "1.9", "2026-11-01 10:00:00.900+00")
	w.publishAt(t, w.trustA, tenantA, "fin.pol", "1.10", "2026-11-02 10:00:00.100+00")
	w.publishAt(t, w.trustA, tenantA, "fin.pol", "2.0", "2026-11-03 10:00:59.999+00")
	want := []policystore.Publication{
		{Version: "1.9", PublishedAt: time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC).Unix(), DocumentHash: testDocHash},
		{Version: "1.10", PublishedAt: time.Date(2026, 11, 2, 10, 0, 0, 0, time.UTC).Unix(), DocumentHash: testDocHash},
		{Version: "2.0", PublishedAt: time.Date(2026, 11, 3, 10, 0, 59, 0, time.UTC).Unix(), DocumentHash: testDocHash},
	}
	if got := published(t, w.trustA, tenantA, "fin.pol"); !reflect.DeepEqual(got, want) {
		t.Fatalf("published %v, want %v", got, want)
	}
	// the newest of the list is the active version the other read gives
	if v := active(t, w.trustA, tenantA, "fin.pol"); v != "fin.pol@2.0" {
		t.Fatalf("active %q", v)
	}
}

// A policy with no publication has none, and the answer is an empty list and no error. Another policy_id's versions are never in it.
func TestPublishedIsEmptyForAPolicyWithNoPublicationAndHoldsOnlyThatPolicy(t *testing.T) {
	w := newWorld(t)
	if got := published(t, w.trustA, tenantA, "fin.pol"); len(got) != 0 {
		t.Fatalf("published %v before any publication", got)
	}
	if err := w.publish(t, w.trustA, tenantA, "fin.other", "7.0"); err != nil {
		t.Fatal(err)
	}
	if got := published(t, w.trustA, tenantA, "fin.pol"); len(got) != 0 {
		t.Fatalf("another policy's version is in the list: %v", got)
	}
	if got := published(t, w.trustA, tenantA, "fin.other"); len(got) != 1 || got[0].Version != "7.0" {
		t.Fatalf("published %v", got)
	}
}

// Tenant isolation (REQ-SEC-004): a login reads its own tenant's publications, and a read of another tenant's policy through one's own login finds nothing.
func TestPublishedIsTenantScoped(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "5.0"); err != nil {
		t.Fatal(err)
	}
	if err := w.publish(t, w.trustB, tenantB, "fin.pol", "1.0"); err != nil {
		t.Fatal(err)
	}
	if a, b := published(t, w.trustA, tenantA, "fin.pol"), published(t, w.trustB, tenantB, "fin.pol"); len(a) != 1 || a[0].Version != "5.0" || len(b) != 1 || b[0].Version != "1.0" {
		t.Fatalf("A %v, B %v", a, b)
	}
	if got := published(t, w.trustA, tenantB, "fin.pol"); len(got) != 0 {
		t.Fatalf("tenant A's login read tenant B's publications: %v", got)
	}
	if got := published(t, w.execA, tenantB, "fin.pol"); len(got) != 0 {
		t.Fatalf("tenant A's Execution login read tenant B's publications: %v", got)
	}
}

// A request outside the patterns of a publication, and a context with no deadline, are refused before the database is asked (the same refusals as Active).
func TestPublishedRefusesAMalformedRequestAndAContextWithNoDeadline(t *testing.T) {
	w := newWorld(t)
	for name, c := range map[string][2]string{"a tenant off pattern": {"tenant-a", "fin.pol"}, "an empty tenant": {"", "fin.pol"}, "a policy id off pattern": {tenantA, "Fin.pol"}, "an empty policy id": {tenantA, ""}} {
		if _, err := policystore.Published(ctx(t), w.trustA, c[0], c[1]); !errors.Is(err, policystore.ErrRequest) {
			t.Errorf("%s: err %v, want ErrRequest", name, err)
		}
	}
	if _, err := policystore.Published(context.Background(), w.trustA, tenantA, "fin.pol"); err == nil {
		t.Error("a context with no deadline was accepted")
	}
}

// A database error is an error, never an empty list: an unreadable history must not read as "nothing published".
func TestPublishedAnUnreadableTableIsAnErrorAndNeverAnEmptyList(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "1.0"); err != nil {
		t.Fatal(err)
	}
	// an Outcome login has no privilege on the table (the same login Active refuses)
	if got, err := policystore.Published(ctx(t), w.outcomeA, tenantA, "fin.pol"); err == nil {
		t.Fatalf("an Outcome login read the publications: %v", got)
	}
}

// A query that cannot even start (a context already cancelled, a closed pool) is an error and never an empty list.
func TestPublishedAQueryThatCannotStartIsAnErrorAndNeverAnEmptyList(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "1.0"); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithTimeout(context.Background(), time.Minute)
	cancel()
	if got, err := policystore.Published(cancelled, w.trustA, tenantA, "fin.pol"); err == nil {
		t.Fatalf("a cancelled context read %v and no error", got)
	}
	w.trustA.Close()
	if got, err := policystore.Published(ctx(t), w.trustA, tenantA, "fin.pol"); err == nil {
		t.Fatalf("a closed pool read %v and no error", got)
	}
}

// A413, INV-09, INV-21: Published carries the document hash each row was published with, and an empty hash for a row that has none (a row from before migration 0035, NULL), so the
// loader of the pack can tell a document that drifted from one that was never hashed. The NULL row is made by switching the hash guard off for the fixture, as the administrator.
func TestPublishedCarriesTheDocumentHashOfEachRowAndNothingForANullOne(t *testing.T) {
	w := newWorld(t)
	w.publishAt(t, w.trustA, tenantA, "fin.pol", "3.1", "2026-11-01 10:00:00+00")
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE policy_publication DISABLE TRIGGER policy_publication_guard_document_hash`); err != nil {
		t.Fatal(err)
	}
	if _, err := w.trustA.Exec(ctx(t), `INSERT INTO policy_publication (tenant_id, policy_id, version, version_key, published_by, published_at)
		VALUES ($1, 'fin.pol', '3.2', policy_version_key('3.2'), 'adm_synth01', '2026-11-02 10:00:00+00'::timestamptz)`, tenantA); err != nil {
		t.Fatalf("a publication with no hash, guard off: %v", err)
	}
	if _, err := w.admin.Exec(ctx(t), `ALTER TABLE policy_publication ENABLE TRIGGER policy_publication_guard_document_hash`); err != nil {
		t.Fatal(err)
	}
	got := published(t, w.trustA, tenantA, "fin.pol")
	if len(got) != 2 || got[0].DocumentHash != testDocHash || got[1].DocumentHash != "" {
		t.Fatalf("published %+v, want the row's hash first and an empty one for the NULL row", got)
	}
}
