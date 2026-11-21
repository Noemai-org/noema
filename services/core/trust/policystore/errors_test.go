package policystore_test

// A347 (fixed public text of an error that crosses a package boundary; AGENT-13): an error of policystore names neither the tenant, nor the policy ID or the version, nor the
// database's message (SQLSTATE text, server and login details), nor a DSN or a path. The cause stays behind Unwrap, so errors.Is and errors.As still reach it; the sentinels
// (ErrRequest, ErrNotNewer) stay reachable with errors.Is. Every case provokes the failure on a real PostgreSQL. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/internal/recordsig"
	"github.com/Noemai-org/noema/services/core/record/chain"
	"github.com/Noemai-org/noema/services/core/trust/policystore"
)

// leakyText is the kind of text a dependency's error can carry: a tenant, a login, a server, a path.
const leakyText = "boom for ten_aa at host=db.internal password=hunter2 /etc/noema/keys/trust.pem postgres://trust@db.internal/noema"

var errLeaky = errors.New(leakyText)

// commonLeaks is what no error text of the package may hold. The lower-cased text is searched.
var commonLeaks = []string{"ten_aa", "fin.pol", "@3.1", "sqlstate", "error:", "fatal", "pgx", "pq", "postgres://", "host=", "password", "/etc/", "/tmp", ".pem", "127.0.0.1", "localhost",
	"policy_publication", "core-trust", "permission denied", "terminating", "closed pool", "duplicate key", "unique", "hunter2", "db.internal", "adm_synth01"}

func assertFixedText(t *testing.T, err error, extra ...string) {
	t.Helper()
	text := strings.ToLower(err.Error())
	for _, leak := range append(append([]string{}, commonLeaks...), extra...) {
		if strings.Contains(text, leak) {
			t.Errorf("the error text carries %q: %q", leak, err.Error())
		}
	}
	if !strings.HasPrefix(err.Error(), "policystore: ") {
		t.Errorf("the error text %q does not start with the package's prefix", err.Error())
	}
	if cause := errors.Unwrap(err); cause != nil && strings.Contains(err.Error(), cause.Error()) {
		t.Errorf("the error text %q holds its cause's text %q", err.Error(), cause.Error())
	}
}

func pgCause(t *testing.T, err error, code string) {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) || (code != "" && pe.Code != code) {
		t.Errorf("the driver's PgError %s is not reachable: %v", code, err)
	}
}

func request() policystore.Request {
	return policystore.Request{TenantID: tenantA, CorrelationID: "corr_pub0001", PolicyID: "fin.pol", Version: "3.1", DocumentHash: testDocHash}
}

type leakySigner struct{}

func (leakySigner) SignRecordEvent(map[string]any) (string, string, []byte, error) {
	return "", "", nil, errLeaky
}

// A347: every error return site of Publish that carries a cause or an identifier.
func TestEveryErrorOfPublishHasAFixedPublicTextAndKeepsItsCause(t *testing.T) {
	cases := []struct {
		name  string
		extra []string
		run   func(t *testing.T, w *world) error
		check func(t *testing.T, err error)
	}{
		{name: "a store that is not configured is still ErrRequest",
			run: func(t *testing.T, w *world) error {
				return (&policystore.Store{}).Publish(ctx(t), nil, request())
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, policystore.ErrRequest) {
					t.Errorf("errors.Is(err, ErrRequest) is false: %v", err)
				}
			}},
		{name: "a version that is not higher: ErrNotNewer, with no policy and no version in the text",
			run: func(t *testing.T, w *world) error {
				if err := w.publish(t, w.trustA, tenantA, "fin.pol", "3.1"); err != nil {
					t.Fatal(err)
				}
				return w.publish(t, w.trustA, tenantA, "fin.pol", "3.1")
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, policystore.ErrNotNewer) {
					t.Errorf("errors.Is(err, ErrNotNewer) is false: %v", err)
				}
				pgCause(t, err, "")
				if err.Error() != policystore.ErrNotNewer.Error() {
					t.Errorf("the text is %q, want the sentinel's own text %q", err.Error(), policystore.ErrNotNewer.Error())
				}
			}},
		{name: "insert: the login may not publish",
			run: func(t *testing.T, w *world) error { return w.publish(t, w.execA, tenantA, "fin.pol", "3.1") },
			check: func(t *testing.T, err error) {
				pgCause(t, err, "42501")
			}},
		{name: "insert: the session is terminated",
			run: func(t *testing.T, w *world) error {
				tx, err := w.trustA.Begin(ctx(t))
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = tx.Rollback(context.Background()) }()
				var pid int
				if err := tx.QueryRow(ctx(t), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
					t.Fatal(err)
				}
				if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend($1)`, pid); err != nil {
					t.Fatal(err)
				}
				return w.store.Publish(ctx(t), tx, request())
			},
			check: func(t *testing.T, err error) {
				if errors.Unwrap(err) == nil {
					t.Errorf("the cause is not kept: %v", err)
				}
			}},
		{name: "append: there is no signer for core-trust",
			run: func(t *testing.T, w *world) error {
				return w.publishWith(t, &policystore.Store{Appender: &chain.Appender{Clock: w.store.Appender.Clock, IDs: w.store.Appender.IDs, Signers: map[string]chain.Signer{}}})
			},
			check: func(t *testing.T, err error) {
				if errors.Unwrap(err) == nil {
					t.Errorf("the cause is not kept: %v", err)
				}
			}},
		{name: "append: the signer fails with a leaky text",
			extra: []string{"boom"},
			run: func(t *testing.T, w *world) error {
				return w.publishWith(t, &policystore.Store{Appender: &chain.Appender{Clock: w.store.Appender.Clock, IDs: w.store.Appender.IDs, Signers: map[string]chain.Signer{"core-trust": leakySigner{}}}})
			},
			check: func(t *testing.T, err error) {
				if !errors.Is(err, errLeaky) {
					t.Errorf("the cause is not reachable: %v", err)
				}
			}},
		{name: "append: the database refuses the signature",
			run: func(t *testing.T, w *world) error {
				return w.publishWith(t, &policystore.Store{Appender: &chain.Appender{Clock: w.store.Appender.Clock, IDs: w.store.Appender.IDs, Signers: recordsig.Local("another world", nil, "core-trust")}})
			},
			check: func(t *testing.T, err error) {
				pgCause(t, err, "")
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			err := c.run(t, w)
			if err == nil {
				t.Fatal("no error, want one")
			}
			assertFixedText(t, err, c.extra...)
			c.check(t, err)
		})
	}
}

// publishWith publishes the request with another store, in a transaction of the tenant's Trust login that is rolled back.
func (w *world) publishWith(t *testing.T, s *policystore.Store) error {
	t.Helper()
	tx, err := w.trustA.Begin(ctx(t))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(context.Background()) }()
	return s.Publish(ctx(t), tx, request())
}

// A347: the two reads. Active and Published, on a closed pool, on a login without privilege, on a session that is terminated, and on a result that misbehaves.
func TestEveryErrorOfTheReadsHasAFixedPublicTextAndKeepsItsCause(t *testing.T) {
	reads := map[string]func(q any) error{
		"Active": func(q any) error {
			_, err := policystore.Active(ctx(t), q.(policystore.Queryer), tenantA, "fin.pol")
			return err
		},
		"Published": func(q any) error {
			_, err := policystore.Published(ctx(t), q.(policystore.Querier), tenantA, "fin.pol")
			return err
		},
	}
	for name, read := range reads {
		t.Run(name+": the pool is closed", func(t *testing.T) {
			w := newWorld(t)
			w.trustA.Close()
			err := read(w.trustA)
			if err == nil {
				t.Fatal("no error")
			}
			assertFixedText(t, err)
			if errors.Unwrap(err) == nil {
				t.Errorf("the cause is not kept: %v", err)
			}
		})
		t.Run(name+": the login has no privilege on the table", func(t *testing.T) {
			w := newWorld(t)
			err := read(w.outcomeA)
			if err == nil {
				t.Fatal("no error")
			}
			assertFixedText(t, err)
			pgCause(t, err, "42501")
		})
		t.Run(name+": the session is terminated", func(t *testing.T) {
			w := newWorld(t)
			tx, err := w.trustA.Begin(ctx(t))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = tx.Rollback(context.Background()) }()
			var pid int
			if err := tx.QueryRow(ctx(t), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
				t.Fatal(err)
			}
			if _, err := w.admin.Exec(ctx(t), `SELECT pg_terminate_backend($1)`, pid); err != nil {
				t.Fatal(err)
			}
			err = read(tx)
			if err == nil {
				t.Fatal("no error")
			}
			assertFixedText(t, err)
			if errors.Unwrap(err) == nil {
				t.Errorf("the cause is not kept: %v", err)
			}
		})
	}
	// The remaining sites cannot be reached by closing, refusing or terminating a connection: the query is accepted and the failure is in the result. They are provoked on a real
	// connection all the same: a pool of the tenant's real Trust login whose search_path puts a test-owned view named policy_publication before the table, so the unchanged query
	// runs on the real server and the real driver and the failure is a value the destination cannot take, or an error the server raises while the rows are read.
	const nullVersion = `SELECT 'ten_aa'::text AS tenant_id, 'fin.pol'::text AS policy_id, NULL::text AS version, ARRAY[1] AS version_key, now() AS published_at, NULL::text AS document_hash`
	const divides = `SELECT 'ten_aa'::text AS tenant_id, 'fin.pol'::text AS policy_id, (CASE WHEN n = 2 THEN 1 / (n - 2) ELSE n END)::text AS version, ARRAY[n] AS version_key, now() AS published_at, NULL::text AS document_hash FROM generate_series(1, 3) AS n`
	t.Run("Active: a row cannot be scanned: the server's version is NULL", func(t *testing.T) {
		w := newWorld(t)
		err := reads["Active"](w.shadowedPublications(t, nullVersion))
		if err == nil {
			t.Fatal("no error")
		}
		assertFixedText(t, err, "null")
		if !strings.HasSuffix(err.Error(), "the active version could not be read") {
			t.Errorf("the text %q is not the read's fixed text", err.Error())
		}
		if cause := errors.Unwrap(err); cause == nil || !strings.Contains(strings.ToLower(cause.Error()), "null") {
			t.Errorf("the cause is %v, want the driver's own scan error", cause)
		}
	})
	t.Run("Active: the server raises an error while the row is read", func(t *testing.T) {
		w := newWorld(t)
		err := reads["Active"](w.shadowedPublications(t, divides))
		if err == nil {
			t.Fatal("no error")
		}
		assertFixedText(t, err, "division")
		pgCause(t, err, "22012")
	})
	t.Run("Published: the rows end with an error: the server raises while the rows are read", func(t *testing.T) {
		w := newWorld(t)
		err := reads["Published"](w.shadowedPublications(t, divides))
		if err == nil {
			t.Fatal("no error")
		}
		assertFixedText(t, err, "division")
		pgCause(t, err, "22012")
		if !strings.HasSuffix(err.Error(), "the publications could not be read") {
			t.Errorf("the text %q is not the rows' fixed text", err.Error())
		}
	})
	t.Run("Published: a row cannot be scanned: the server's version is NULL", func(t *testing.T) {
		w := newWorld(t)
		err := reads["Published"](w.shadowedPublications(t, nullVersion))
		if err == nil {
			t.Fatal("no error")
		}
		assertFixedText(t, err, "null")
		if !strings.HasSuffix(err.Error(), "a publication could not be read") {
			t.Errorf("the text %q is not the scan's fixed text", err.Error())
		}
		if cause := errors.Unwrap(err); cause == nil || !strings.Contains(strings.ToLower(cause.Error()), "null") {
			t.Errorf("the cause is %v, want the driver's own scan error", cause)
		}
	})
}

var shadowSeq atomic.Int64

// shadowedPublications returns a pool of the tenant's real Trust login whose search_path puts a test-owned schema before public, where a view named policy_publication answers the
// queries of the reads with the given SELECT.
func (w *world) shadowedPublications(t *testing.T, selectSQL string) *pgxpool.Pool {
	t.Helper()
	schema := fmt.Sprintf("shadow_pub_%d", shadowSeq.Add(1))
	for _, stmt := range []string{
		`CREATE SCHEMA ` + schema,
		`CREATE VIEW ` + schema + `.policy_publication AS ` + selectSQL,
		`GRANT USAGE ON SCHEMA ` + schema + ` TO trust_rw`,
		`GRANT SELECT ON ` + schema + `.policy_publication TO trust_rw`,
	} {
		if _, err := w.admin.Exec(ctx(t), stmt); err != nil {
			t.Fatalf("%s: %v", stmt, err)
		}
	}
	cfg := w.trustA.Config().Copy()
	cfg.ConnConfig.RuntimeParams["search_path"] = schema + ", public"
	pool, err := pgxpool.NewWithConfig(ctx(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}
