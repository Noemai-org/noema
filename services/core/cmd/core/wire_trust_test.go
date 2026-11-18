package main

// STM-01 follow-up (wire_trust.go): buildTrust makes the orchestrator's Trust and Bindings from the Trust adapter of noema#710, over the Postgres readers of trustpg, and loads the
// grant keys from files only. Every missing item is a startup error that names it, and no message carries a DSN or key material. No database is dialled here (pools connect lazily);
// the readers' queries are tested against PostgreSQL in orchestrator/portadapters/trustpg. All values are SYNTHETIC. REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, INV-25, INV-29, AGENT-16.

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

type evalStub struct{}

func (evalStub) Evaluate(context.Context, intake.Request) (intake.Result, error) {
	return intake.Result{}, errors.New("not called in this test")
}

// emptyRecord is a Record reader whose chain is empty: BeginTx gives a transaction that returns no row.
type emptyRecord struct{}

type emptyTx struct{ pgx.Tx }
type noRows struct{ pgx.Rows }

func (emptyRecord) For(string) (reconstruct.TxBeginner, error) { return emptyRecord{}, nil }
func (emptyRecord) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return emptyTx{}, nil
}
func (emptyTx) Query(context.Context, string, ...any) (pgx.Rows, error) { return noRows{}, nil }
func (emptyTx) Rollback(context.Context) error                          { return nil }
func (noRows) Next() bool                                               { return false }
func (noRows) Err() error                                               { return nil }
func (noRows) Close()                                                   {}

var _ reconstruct.TenantDBs = emptyRecord{}

type loaded struct {
	dir       string
	maxActive int
	calls     int
}

func (l *loaded) load(dir string, maxActive int) (*grants.Keys, error) {
	l.dir, l.maxActive = dir, maxActive
	l.calls++
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 7)
	}
	return grants.NewKeys([]grants.Key{{ID: "grant.hmac.1", Secret: secret}})
}

func trustEnv(t *testing.T) envMap {
	t.Helper()
	dsn, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("trust_aa") + "\n"})
	return envMap{EnvTrustDSNDir: dsn, EnvTrustDBTimeout: "5s", EnvGrantKeyDir: "/synthetic/grant-keys", EnvGrantKeyMaxActive: "2"}
}

func buildT(t *testing.T, e envMap, rec reconstruct.TenantDBs, ev trustport.Evaluator, l *loaded) (TrustParts, func(), error) {
	t.Helper()
	return buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, rec, ev, l.load)
}

func TestWithEverythingGivenBuildTrustReturnsTheRealTrustAndBindings(t *testing.T) {
	l := &loaded{}
	p, closer, err := buildT(t, trustEnv(t), emptyRecord{}, evalStub{}, l)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	closer() // safe to call more than once
	if _, ok := p.Trust.(trustport.Port); !ok {
		t.Fatalf("Trust is %T, want the Trust adapter of noema#710", p.Trust)
	}
	if _, ok := p.Bindings.(trustport.Port); !ok {
		t.Fatalf("Bindings is %T, want the Trust adapter of noema#710", p.Bindings)
	}
	if p.Dispatches == nil || p.Keys == nil {
		t.Fatalf("dispatches %v, keys %v; want both", p.Dispatches != nil, p.Keys != nil)
	}
	// INV-29, REQ-SEC-005: the keys come from the directory the setting names, with the rotation overlap the setting gives, once.
	if l.dir != "/synthetic/grant-keys" || l.maxActive != 2 || l.calls != 1 {
		t.Fatalf("loader called %d times with %q and %d", l.calls, l.dir, l.maxActive)
	}
}

// REQ-RCV-003, A306: the dispatch count's limit is the adopted budget, for every capability, and the used count comes from the Record reader.
func TestTheDispatchLimitIsTheAdoptedBudget(t *testing.T) {
	p, closer, err := buildT(t, trustEnv(t), emptyRecord{}, evalStub{}, &loaded{})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	c, err := p.Dispatches.Count(t.Context(), intake.DispatchKey{TenantID: "ten_aa", CorrelationID: "corr_ABC123", CapabilityID: "fin.cap.x", TargetRef: "fin.t:1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Used != 0 || c.Limit != orchestrator.PERDMaxDispatchesPerKey {
		t.Fatalf("count %+v, want used 0 and limit %d", c, orchestrator.PERDMaxDispatchesPerKey)
	}
}

// INV-12: the Bindings refuse a session without a tenant and a principal before any read.
func TestTheBindingsAreTheAdapterAndRefuseAnEmptySession(t *testing.T) {
	p, closer, err := buildT(t, trustEnv(t), emptyRecord{}, evalStub{}, &loaded{})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if err := p.Bindings.Bound(t.Context(), orchestrator.Session{}, orchestrator.Binding{}); !errors.Is(err, trustport.ErrSession) {
		t.Fatalf("got %v, want ErrSession", err)
	}
}

// Fail closed: each missing item is an error that names it; nothing is built and no pool is left open.
func TestEachMissingItemIsAStartupErrorThatNamesIt(t *testing.T) {
	cases := []struct {
		name  string
		mut   func(envMap)
		noRec bool
		noEv  bool
		noKey bool
		want  string
	}{
		{name: "no trust DSN directory", mut: func(e envMap) { delete(e, EnvTrustDSNDir) }, want: EnvTrustDSNDir},
		{name: "no timeout", mut: func(e envMap) { delete(e, EnvTrustDBTimeout) }, want: EnvTrustDBTimeout},
		{name: "a timeout that is not a duration", mut: func(e envMap) { e[EnvTrustDBTimeout] = "soon" }, want: EnvTrustDBTimeout},
		{name: "a timeout of zero", mut: func(e envMap) { e[EnvTrustDBTimeout] = "0s" }, want: EnvTrustDBTimeout},
		{name: "no grant key directory", mut: func(e envMap) { delete(e, EnvGrantKeyDir) }, want: EnvGrantKeyDir},
		{name: "no rotation overlap", mut: func(e envMap) { delete(e, EnvGrantKeyMaxActive) }, want: EnvGrantKeyMaxActive},
		{name: "a rotation overlap of zero", mut: func(e envMap) { e[EnvGrantKeyMaxActive] = "0" }, want: EnvGrantKeyMaxActive},
		{name: "no Record reader", mut: func(envMap) {}, noRec: true, want: EnvRecordReadDSNDir},
		{name: "no intake", mut: func(envMap) {}, noEv: true, want: "intake"},
		{name: "no key loader", mut: func(envMap) {}, noKey: true, want: "key loader"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := trustEnv(t)
			c.mut(e)
			var rec reconstruct.TenantDBs = emptyRecord{}
			if c.noRec {
				rec = nil
			}
			var ev trustport.Evaluator = evalStub{}
			if c.noEv {
				ev = nil
			}
			load := (&loaded{}).load
			if c.noKey {
				load = nil
			}
			p, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, rec, ev, load)
			if err == nil {
				closer()
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not name %q", err, c.want)
			}
			if p.Trust != nil || p.Bindings != nil || p.Dispatches != nil || p.Keys != nil {
				t.Fatalf("parts built despite the error: %+v", p)
			}
			if closer == nil {
				t.Fatal("no closer")
			}
			closer()
		})
	}
}

// INV-29, AGENT-13: a loader that fails is a startup error that names the setting; the DSN's password is never in a message.
func TestAKeyLoaderThatFailsStopsTheStartup(t *testing.T) {
	e := trustEnv(t)
	p, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, emptyRecord{}, evalStub{},
		func(string, int) (*grants.Keys, error) { return nil, errors.New("no key") })
	if err == nil || !strings.Contains(err.Error(), EnvGrantKeyDir) {
		t.Fatalf("error %v, want one that names %s", err, EnvGrantKeyDir)
	}
	if strings.Contains(err.Error(), synthPassword) {
		t.Fatal("the error carries the DSN password")
	}
	if p.Keys != nil || p.Trust != nil {
		t.Fatal("parts built despite the error")
	}
	closer()
}

// INV-29: no key comes from the environment. A key given by value in any setting is not read: the loader still gets the directory.
func TestNoKeyIsReadFromTheEnvironment(t *testing.T) {
	e := trustEnv(t)
	e["NOEMA_GRANT_KEY"] = strings.Repeat("ab", 32)
	l := &loaded{}
	_, closer, err := buildT(t, e, emptyRecord{}, evalStub{}, l)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if l.dir != "/synthetic/grant-keys" {
		t.Fatalf("loader got %q", l.dir)
	}
}
