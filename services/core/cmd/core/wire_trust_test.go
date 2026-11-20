package main

// STM-01 follow-up (wire_trust.go): buildTrust makes the orchestrator's Trust and Bindings from the Trust adapter of noema#710, over the Postgres readers of trustpg, and loads the
// grant keys from files only. Every missing item is a startup error that names it, and no message carries a DSN or key material. No database is dialled here (pools connect lazily);
// the readers' queries are tested against PostgreSQL in orchestrator/portadapters/trustpg. All values are SYNTHETIC. REQ-TRU-009, REQ-RCV-003; INV-03, INV-12, INV-25, INV-29, AGENT-16.

import (
	"context"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// The fakes below are SYNTHETIC: each implements one intake port and is never called by buildTrust (nothing is evaluated at startup).
type fakeIntents struct{ intake.Intents }
type fakeWrites struct{ intake.Writes }
type fakeCapabilities struct{ intake.Capabilities }
type fakeScope struct{ intake.Scope }
type fakeAuthority struct{ intake.Authority }
type fakeCandidates struct{ intake.Candidates }
type fakeOwnership struct{ intake.Ownership }
type fakeEvidence struct{ intake.Evidence }
type fakeReconciler struct{ intake.Reconciler }
type fakePolicies struct{ intake.Policies }
type fakeClocks struct{ intake.Clocks }
type fakeConfirmations struct{ intake.Confirmations }
type fakePlanner struct{ intake.Planner }
type fakeRecorder struct{ intake.Recorder }

// fullAdapters is every adapter buildTrust is given: all the intake ports but Dispatches (which buildTrust makes itself), the service clock and the pack version.
func fullAdapters() TrustAdapters {
	return TrustAdapters{
		Intents: fakeIntents{}, Writes: fakeWrites{}, Capabilities: fakeCapabilities{}, Scope: fakeScope{}, Authority: fakeAuthority{},
		Candidates: fakeCandidates{}, Ownership: fakeOwnership{}, Evidence: fakeEvidence{}, Reconciler: fakeReconciler{}, Policies: fakePolicies{},
		Clocks: fakeClocks{}, Confirmations: fakeConfirmations{}, Planner: fakePlanner{}, Recorder: fakeRecorder{},
		Clock: clock.NewManual(time.Unix(1_800_000_000, 0)), PackVersion: "synthetic.pack.v1",
	}
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
	return envMap{EnvTrustDSNDir: dsn, EnvTrustDBTimeout: "5000", EnvGrantKeyDir: "/synthetic/grant-keys", EnvGrantKeyMaxActive: "2"}
}

func buildT(t *testing.T, e envMap, rec reconstruct.TenantDBs, ad TrustAdapters, l *loaded) (TrustParts, func(), error) {
	t.Helper()
	return buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, rec, ad, l.load)
}

func TestWithEverythingGivenBuildTrustReturnsTheRealTrustAndBindings(t *testing.T) {
	l := &loaded{}
	p, closer, err := buildT(t, trustEnv(t), emptyRecord{}, fullAdapters(), l)
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
	if p.Dispatches == nil || p.Keys == nil || p.Service == nil {
		t.Fatalf("dispatches %v, keys %v, service %v; want all", p.Dispatches != nil, p.Keys != nil, p.Service != nil)
	}
	// INV-29, REQ-SEC-005: the keys come from the directory the setting names, with the rotation overlap the setting gives, once.
	if l.dir != "/synthetic/grant-keys" || l.maxActive != 2 || l.calls != 1 {
		t.Fatalf("loader called %d times with %q and %d", l.calls, l.dir, l.maxActive)
	}
}

// REQ-RCV-003, A306: the dispatch count's limit is the adopted budget, for every capability, and the used count comes from the Record reader.
func TestTheDispatchLimitIsTheAdoptedBudget(t *testing.T) {
	p, closer, err := buildT(t, trustEnv(t), emptyRecord{}, fullAdapters(), &loaded{})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	c, err := p.Dispatches.Count(ctx, intake.DispatchKey{TenantID: "ten_aa", CorrelationID: "corr_ABC123", CapabilityID: "fin.cap.x", TargetRef: "fin.t:1"})
	if err != nil {
		t.Fatal(err)
	}
	if c.Used != 0 || c.Limit != orchestrator.PERDMaxDispatchesPerKey {
		t.Fatalf("count %+v, want used 0 and limit %d", c, orchestrator.PERDMaxDispatchesPerKey)
	}
}

// INV-12: the Bindings refuse a session without a tenant and a principal before any read.
func TestTheBindingsAreTheAdapterAndRefuseAnEmptySession(t *testing.T) {
	p, closer, err := buildT(t, trustEnv(t), emptyRecord{}, fullAdapters(), &loaded{})
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
		noKey bool
		want  string
	}{
		{name: "no trust DSN directory", mut: func(e envMap) { delete(e, EnvTrustDSNDir) }, want: EnvTrustDSNDir},
		{name: "no timeout", mut: func(e envMap) { delete(e, EnvTrustDBTimeout) }, want: EnvTrustDBTimeout},
		{name: "a timeout that is not a duration", mut: func(e envMap) { e[EnvTrustDBTimeout] = "soon" }, want: EnvTrustDBTimeout},
		{name: "a Go duration is not milliseconds", mut: func(e envMap) { e[EnvTrustDBTimeout] = "5s" }, want: EnvTrustDBTimeout},
		{name: "a negative timeout", mut: func(e envMap) { e[EnvTrustDBTimeout] = "-5" }, want: EnvTrustDBTimeout},
		{name: "a timeout of zero", mut: func(e envMap) { e[EnvTrustDBTimeout] = "0" }, want: EnvTrustDBTimeout},
		{name: "no grant key directory", mut: func(e envMap) { delete(e, EnvGrantKeyDir) }, want: EnvGrantKeyDir},
		{name: "no rotation overlap", mut: func(e envMap) { delete(e, EnvGrantKeyMaxActive) }, want: EnvGrantKeyMaxActive},
		{name: "a rotation overlap of zero", mut: func(e envMap) { e[EnvGrantKeyMaxActive] = "0" }, want: EnvGrantKeyMaxActive},
		{name: "no Record reader", mut: func(envMap) {}, noRec: true, want: EnvRecordReadDSNDir},
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
			ad := fullAdapters()
			load := (&loaded{}).load
			if c.noKey {
				load = nil
			}
			p, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, rec, ad, load)
			if err == nil {
				closer()
				t.Fatal("no error")
			}
			if !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %q does not name %q", err, c.want)
			}
			if p.Trust != nil || p.Bindings != nil || p.Dispatches != nil || p.Keys != nil || p.Service != nil {
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
	p, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, emptyRecord{}, fullAdapters(),
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
	_, closer, err := buildT(t, e, emptyRecord{}, fullAdapters(), l)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if l.dir != "/synthetic/grant-keys" {
		t.Fatalf("loader got %q", l.dir)
	}
}

// A loader that returns neither keys nor an error is a startup error, never a Trust without keys (INV-29).
func TestALoaderThatReturnsNoKeysStopsTheStartup(t *testing.T) {
	e := trustEnv(t)
	p, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, emptyRecord{}, fullAdapters(),
		func(string, int) (*grants.Keys, error) { return nil, nil })
	if err == nil || !strings.Contains(err.Error(), EnvGrantKeyDir) || p.Trust != nil {
		t.Fatalf("error %v, trust %v; want an error that names %s and no Trust", err, p.Trust, EnvGrantKeyDir)
	}
	closer()
}

// REQ-TRU-002, INV-03: the Trust is the real intake service, and every one of its 15 ports is set. The ports are enumerated by reflection over intake.Ports, so a port added to
// the intake later cannot be forgotten here: this test fails until TrustAdapters (or buildTrust) supplies it.
func TestEveryPortOfTheIntakeServiceIsSet(t *testing.T) {
	p, closer, err := buildT(t, trustEnv(t), emptyRecord{}, fullAdapters(), &loaded{})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if p.Service == nil {
		t.Fatal("no intake service")
	}
	if tr, ok := p.Trust.(trustport.Port); !ok || tr.Evaluator != trustport.Evaluator(p.Service) {
		t.Fatalf("Trust %T does not evaluate through the built intake service", p.Trust)
	}
	v := reflect.ValueOf(p.Service.Ports)
	if v.NumField() != 15 {
		t.Fatalf("intake.Ports has %d fields; the lane wires 15: update TrustAdapters and this count together", v.NumField())
	}
	for i := 0; i < v.NumField(); i++ {
		if v.Field(i).IsNil() {
			t.Errorf("intake port %s is not set", v.Type().Field(i).Name)
		}
	}
	if p.Service.Clock == nil || p.Service.PackVersion != "synthetic.pack.v1" {
		t.Fatalf("service clock %v, pack version %q", p.Service.Clock, p.Service.PackVersion)
	}
}

// Every port of intake.Ports except Dispatches is a field of TrustAdapters of the same name and type: a new port forces a new field.
func TestTrustAdaptersHasAFieldForEveryPortButDispatches(t *testing.T) {
	ad := reflect.TypeOf(TrustAdapters{})
	ports := reflect.TypeOf(intake.Ports{})
	for i := 0; i < ports.NumField(); i++ {
		f := ports.Field(i)
		if f.Name == "Dispatches" {
			if _, ok := ad.FieldByName("Dispatches"); ok {
				t.Error("Dispatches is built by buildTrust from the Record reader; it must not be a TrustAdapters field")
			}
			continue
		}
		g, ok := ad.FieldByName(f.Name)
		if !ok || g.Type != f.Type {
			t.Errorf("TrustAdapters has no field %s of type %s", f.Name, f.Type)
		}
	}
}

// Fail closed: a missing adapter is a startup error that names the port; nothing is built and the closer is safe.
func TestEachMissingAdapterIsAStartupErrorThatNamesThePort(t *testing.T) {
	ports := reflect.TypeOf(TrustAdapters{})
	for i := 0; i < ports.NumField(); i++ {
		name := ports.Field(i).Name
		t.Run(name, func(t *testing.T) {
			ad := fullAdapters()
			f := reflect.ValueOf(&ad).Elem().Field(i)
			f.Set(reflect.Zero(f.Type()))
			l := &loaded{}
			e := trustEnv(t)
			p, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, emptyRecord{}, ad, l.load)
			defer closer()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error %v, want one that names %s", err, name)
			}
			if p.Trust != nil || p.Service != nil || p.Keys != nil || l.calls != 0 {
				t.Fatalf("built despite the error (keys loaded %d times)", l.calls)
			}
		})
	}
}

// A nil adapter that sits inside a non-nil interface (a typed nil pointer) is also refused, by name.
func TestATypedNilAdapterIsRefused(t *testing.T) {
	ad := fullAdapters()
	var rec *typedNilRecorder
	ad.Recorder = rec
	e := trustEnv(t)
	_, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, emptyRecord{}, ad, (&loaded{}).load)
	defer closer()
	if err == nil || !strings.Contains(err.Error(), "Recorder") {
		t.Fatalf("error %v, want one that names Recorder", err)
	}
}

type typedNilRecorder struct{}

func (*typedNilRecorder) Record(context.Context, intake.RecordRequest) (intake.RecordResult, error) {
	return intake.RecordResult{}, errors.New("not called")
}

// INV-29, SEC-09: the production key loader is keyfiles.Load: a directory that is not there is an error that names the setting and holds no key material.
func TestTheGrantKeyLoaderIsKeyfilesLoad(t *testing.T) {
	_, err := loadGrantKeys(t.TempDir()+"/absent", 2)
	if err == nil {
		t.Fatal("no error for a directory that is not there")
	}
	if _, err := loadGrantKeys(t.TempDir(), 0); err == nil {
		t.Fatal("no error for a rotation overlap of zero")
	}
}

// The service clock and the pack version are required.
func TestAMissingClockOrPackVersionIsAStartupError(t *testing.T) {
	for name, mut := range map[string]func(*TrustAdapters){
		"Clock":       func(a *TrustAdapters) { a.Clock = nil },
		"PackVersion": func(a *TrustAdapters) { a.PackVersion = "" },
	} {
		t.Run(name, func(t *testing.T) {
			ad := fullAdapters()
			mut(&ad)
			e := trustEnv(t)
			_, closer, err := buildTrust(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, emptyRecord{}, ad, (&loaded{}).load)
			defer closer()
			if err == nil || !strings.Contains(err.Error(), name) {
				t.Fatalf("error %v, want one that names %s", err, name)
			}
		})
	}
}

// SEC-09, REQ-SEC-005: loadGrantKeys passes the rotation overlap to keyfiles.Load: two SYNTHETIC key files load with an overlap of 2 and are refused with 1.
func TestTheRotationOverlapReachesTheKeyLoader(t *testing.T) {
	dir := t.TempDir()
	for _, v := range []string{"1", "2"} {
		if err := os.WriteFile(dir+"/grant.hmac."+v+".key", []byte(strings.Repeat(v+"a", 32)+"\n"), 0o400); err != nil {
			t.Fatal(err)
		}
	}
	if k, err := loadGrantKeys(dir, 2); err != nil || k == nil {
		t.Fatalf("overlap 2: keys %v, error %v", k != nil, err)
	}
	if _, err := loadGrantKeys(dir, 1); err == nil {
		t.Fatal("overlap 1 accepted two keys")
	}
}
