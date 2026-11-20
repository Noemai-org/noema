package policystore_test

// TRU-02 (REQ-TRU-005, REQ-AUD-001; INV-12, AGENT-13; ruling A474): the publisher of a policy version is the authenticated database role of the connection, filled in by the database
// in the statement that inserts the row and read back from that statement for the Record event. No caller supplies it: policystore.Request has no publisher member, so an identity
// cannot come from an argument, a claim or a flag. All data is SYNTHETIC.

import (
	"net/url"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// sessionUser is the login the pool authenticated as, read as the database reads it.
func sessionUser(t *testing.T, p *pgxpool.Pool) string {
	t.Helper()
	var role string
	if err := p.QueryRow(ctx(t), `SELECT session_user::text`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	return role
}

// rowPublisher is the published_by of the publication row of tenant A.
func rowPublisher(t *testing.T, w *world, version string) string {
	t.Helper()
	var by string
	if err := w.admin.QueryRow(ctx(t), `SELECT published_by FROM policy_publication WHERE tenant_id = $1 AND policy_id = 'fin.pol' AND version = $2`, tenantA, version).Scan(&by); err != nil {
		t.Fatal(err)
	}
	return by
}

// eventActor and eventPayload are the actor and the stored payload of the POLICY_PUBLISHED events of tenant A, in chain order.
func eventActors(t *testing.T, w *world) []string {
	t.Helper()
	rows, err := w.admin.Query(ctx(t), `SELECT actor FROM record_event WHERE tenant_id = $1 AND event_type = 'POLICY_PUBLISHED' ORDER BY seq`, tenantA)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// INV-12: the request has no member that names the publisher. A new member of that kind needs a ruling, and this test fails until it has one.
func TestTheRequestHasNoMemberThatNamesThePublisher(t *testing.T) {
	var names []string
	rt := reflect.TypeOf(policystore.Request{})
	for i := 0; i < rt.NumField(); i++ {
		names = append(names, rt.Field(i).Name)
	}
	sort.Strings(names)
	if got, want := strings.Join(names, ","), "CorrelationID,DocumentHash,PolicyID,TenantID,Version"; got != want {
		t.Fatalf("Request members %s, want %s", got, want)
	}
}

// INV-12, REQ-AUD-001, A474: the row's published_by is the database role of the connection, and the event's actor is admin:<that role>; the two are the same value.
func TestThePublisherOfTheRowAndOfTheEventIsTheAuthenticatedDatabaseRole(t *testing.T) {
	w := newWorld(t)
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "3.1"); err != nil {
		t.Fatal(err)
	}
	role := sessionUser(t, w.trustA)
	if got := rowPublisher(t, w, "3.1"); got != role {
		t.Fatalf("row published_by %q, want the authenticated role %q", got, role)
	}
	if got := eventActors(t, w); len(got) != 1 || got[0] != "admin:"+role {
		t.Fatalf("event actors %v, want [admin:%s]", got, role)
	}
}

// A474: a record of another login names that login: the publisher is read from the connection each time, not remembered by the store.
func TestThePublisherFollowsTheLoginWhateverItsName(t *testing.T) {
	w := newWorld(t)
	alt := w.loginNamed(t, tenantA, "trust_rw__ten_aa_alt")
	if err := w.publishOn(t, alt, "fin.pol", "3.1"); err != nil {
		t.Fatal(err)
	}
	if got := rowPublisher(t, w, "3.1"); got != "trust_rw__ten_aa_alt" {
		t.Fatalf("row published_by %q, want trust_rw__ten_aa_alt", got)
	}
	if got := eventActors(t, w); len(got) != 1 || got[0] != "admin:trust_rw__ten_aa_alt" {
		t.Fatalf("event actors %v, want [admin:trust_rw__ten_aa_alt]", got)
	}
	if err := w.publish(t, w.trustA, tenantA, "fin.pol", "3.2"); err != nil {
		t.Fatal(err)
	}
	if got := rowPublisher(t, w, "3.2"); got != sessionUser(t, w.trustA) {
		t.Fatalf("second row published_by %q, want %q", got, sessionUser(t, w.trustA))
	}
}

// INV-12: SET ROLE changes current_user and not the login that authenticated, and a connection can SET ROLE to the group role it belongs to. The publisher is the login, not the role it
// is running as.
func TestASetRoleDoesNotChangeThePublisher(t *testing.T) {
	w := newWorld(t)
	tx, err := w.trustA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx(t), `SET LOCAL ROLE trust_rw`); err != nil {
		t.Fatal(err)
	}
	var cur string
	if err := tx.QueryRow(ctx(t), `SELECT current_user::text`).Scan(&cur); err != nil || cur != "trust_rw" {
		t.Fatalf("current_user %q (%v): the test must run as a role that is not the login", cur, err)
	}
	if err := w.store.Publish(ctx(t), tx, policystore.Request{TenantID: tenantA, CorrelationID: "corr_pub0001", PolicyID: "fin.pol", Version: "3.1", DocumentHash: testDocHash}); err != nil {
		_ = tx.Rollback(ctx(t))
		t.Fatal(err)
	}
	if err := tx.Commit(ctx(t)); err != nil {
		t.Fatal(err)
	}
	login := sessionUser(t, w.trustA)
	if got := rowPublisher(t, w, "3.1"); got != login {
		t.Fatalf("row published_by %q, want the login %q and not the role it ran as", got, login)
	}
	if got := eventActors(t, w); len(got) != 1 || got[0] != "admin:"+login {
		t.Fatalf("event actors %v, want [admin:%s]", got, login)
	}
}

// AGENT-13, fail closed: a login whose name is no valid actor form cannot publish. The Record refuses the event, so no row and no event is kept, and the error text names neither the
// login nor the driver.
func TestALoginWhoseNameIsNoActorFormPublishesNothing(t *testing.T) {
	w := newWorld(t)
	odd := w.loginNamed(t, tenantA, "trust_rw__ten_aa odd name")
	err := w.publishOn(t, odd, "fin.pol", "3.1")
	if err == nil {
		t.Fatal("a login with a name that is no actor form published")
	}
	if strings.Contains(err.Error(), "odd") {
		t.Errorf("the error names the login: %q", err)
	}
	if n := w.countRows(t, `SELECT count(*) FROM policy_publication`); n != 0 {
		t.Errorf("%d publications kept, want none", n)
	}
	if n := w.countRows(t, `SELECT count(*) FROM record_event WHERE event_type = 'POLICY_PUBLISHED'`); n != 0 {
		t.Errorf("%d POLICY_PUBLISHED events kept, want none", n)
	}
}

func (w *world) countRows(t *testing.T, q string) int {
	t.Helper()
	var n int
	if err := w.admin.QueryRow(ctx(t), q).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// loginNamed makes a Trust login of the tenant under a name the test chooses, and returns a connection as it. The role is dropped when the test ends.
func (w *world) loginNamed(t *testing.T, tenant, name string) *pgx.Conn {
	t.Helper()
	quoted := pgx.Identifier{name}.Sanitize()
	if _, err := w.admin.Exec(ctx(t), `CREATE ROLE `+quoted+` LOGIN IN ROLE trust_rw`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = w.admin.Exec(ctx(t), `DROP ROLE IF EXISTS `+quoted) })
	if _, err := w.admin.Exec(ctx(t), `INSERT INTO tenant_login (login_role, power, tenant_id) VALUES ($1, 'trust', $2)`, name, tenant); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(w.dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.User = url.User(name)
	conn, err := pgx.Connect(ctx(t), u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close(ctx(t)) })
	return conn
}

// publishOn publishes one version of the policy for tenant A on a connection and commits.
func (w *world) publishOn(t *testing.T, conn *pgx.Conn, policyID, version string) error {
	t.Helper()
	tx, err := conn.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := w.store.Publish(ctx(t), tx, policystore.Request{TenantID: tenantA, CorrelationID: "corr_pub0001", PolicyID: policyID, Version: version, DocumentHash: testDocHash}); err != nil {
		_ = tx.Rollback(ctx(t))
		return err
	}
	return tx.Commit(ctx(t))
}
