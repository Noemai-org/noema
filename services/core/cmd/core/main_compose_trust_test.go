package main

// STM-01 final composition (the Founder's order of 20 Nov), slice 1: main builds Trust through buildTrust, once, from settings with no defaults (A330, AGENT-16), and the grant
// keys come only from NOEMA_GRANT_KEY_DIR through keyfiles.Load (INV-29; buildTrust already does that, so main passes the real loader and never loads a key itself). A missing
// setting is a startup error that names the setting; a missing adapter is one that names the adapter; with no Trust setting at all /readyz names "trust". REQ-TRU-009, REQ-SEC-003,
// INV-03, INV-12, INV-29, AGENT-16. Every value is SYNTHETIC.

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
)

type mcCalls struct{ adapters int }

func mcDeps(l *loaded, c *mcCalls) composeDeps {
	return composeDeps{
		LoadKeys: l.load,
		TrustAdapters: func(Env, reconstruct.TenantDBs, clock.Clock) (TrustAdapters, error) {
			c.adapters++
			return fullAdapters(), nil
		},
	}
}

// mcEnv is a Record reader plus the four Trust settings; drop removes settings by name.
func mcEnv(t *testing.T, drop ...string) envMap {
	t.Helper()
	rd, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	e := trustEnv(t)
	e[EnvRecordReadDSNDir] = rd
	for _, d := range drop {
		delete(e, d)
	}
	return e
}

func mcBuild(t *testing.T, e envMap, d composeDeps) (Components, TrustParts, func(), error) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC))
	return buildComponentsWith(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, clk, ids.New(clk, nil), discardLog(), d)
}

// INV-03, INV-29: main builds Trust once; the orchestrator gets the Trust adapter, the write path's Bindings are the same adapter, the keys are loaded once from the directory the
// setting names, and /readyz stops naming "trust".
func TestMainBuildsTrustOnceAndPlugsItsPortsAndReadinessStopsNamingIt(t *testing.T) {
	l, calls := &loaded{}, &mcCalls{}
	c, parts, closer, err := mcBuild(t, mcEnv(t), mcDeps(l, calls))
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if _, ok := c.Ports.Trust.(trustport.Port); !ok {
		t.Fatalf("Ports.Trust is %T, want the Trust adapter", c.Ports.Trust)
	}
	if _, ok := parts.Bindings.(trustport.Port); !ok || parts.Keys == nil || parts.Service == nil || parts.Dispatches == nil {
		t.Fatalf("parts %+v: want Bindings, Keys, Service and Dispatches built", parts)
	}
	if calls.adapters != 1 || l.calls != 1 || l.dir != "/synthetic/grant-keys" || l.maxActive != 2 {
		t.Fatalf("adapters asked %d times, keys loaded %d times from %q (max %d); want once, once, the setting's directory, 2", calls.adapters, l.calls, l.dir, l.maxActive)
	}
	if _, reason := composeReadiness(c, nil)(); strings.Contains(reason, "trust") {
		t.Fatalf("Trust is built but /readyz says %q", reason)
	}
}

// A330: with no Trust setting there is no Trust, no error, and the loader and the adapters are never asked; /readyz names "trust".
func TestWithNoTrustSettingNothingIsBuiltAndReadinessNamesTrust(t *testing.T) {
	l, calls := &loaded{}, &mcCalls{}
	c, parts, closer, err := mcBuild(t, mcEnv(t, EnvTrustDSNDir, EnvTrustDBTimeout, EnvGrantKeyDir, EnvGrantKeyMaxActive), mcDeps(l, calls))
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Ports.Trust != nil || parts.Keys != nil || calls.adapters != 0 || l.calls != 0 {
		t.Fatalf("trust %v, keys %v, adapters asked %d, keys loaded %d; want nothing", c.Ports.Trust != nil, parts.Keys != nil, calls.adapters, l.calls)
	}
	if _, reason := composeReadiness(c, nil)(); !strings.Contains(reason, "trust") {
		t.Fatalf("/readyz says %q; want it to name trust", reason)
	}
}

// AGENT-16: once any Trust setting is given, each of the others is required and the error names the one that is missing, never a DSN, a directory or a key.
func TestAnyTrustSettingMakesEveryOtherRequiredAndTheErrorNamesTheMissingOne(t *testing.T) {
	for _, missing := range []string{EnvTrustDBTimeout, EnvGrantKeyDir, EnvGrantKeyMaxActive, EnvTrustDSNDir} {
		l, calls := &loaded{}, &mcCalls{}
		e := mcEnv(t, missing)
		_, _, closer, err := mcBuild(t, e, mcDeps(l, calls))
		if err == nil || !strings.Contains(err.Error(), missing) || closer == nil {
			t.Errorf("%s missing: error %v, closer %v; want an error that names it", missing, err, closer != nil)
			continue
		}
		closer()
		for _, secret := range []string{synthPassword, e[EnvTrustDSNDir], e[EnvRecordReadDSNDir], "postgres://"} {
			if secret != "" && strings.Contains(err.Error(), secret) {
				t.Errorf("%s missing: the error carries %q", missing, secret)
			}
		}
	}
}

// NOEMA_TRUST_DSN_DIR alone is the payload reader's setting too: it does not by itself ask for Trust.
func TestTheTrustDSNDirAloneDoesNotStartTrust(t *testing.T) {
	l, calls := &loaded{}, &mcCalls{}
	c, _, closer, err := mcBuild(t, mcEnv(t, EnvTrustDBTimeout, EnvGrantKeyDir, EnvGrantKeyMaxActive), mcDeps(l, calls))
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Ports.Trust != nil || calls.adapters != 0 {
		t.Fatalf("Trust built from the DSN directory alone")
	}
}

// INV-03: Trust settings without the Record reader are refused by name (the dispatch count reads the Record); the production build has no adapter for most intake ports yet, and the
// error names the first one: no quiet nil.
func TestTrustSettingsWithoutTheRecordReaderOrWithoutAdaptersAreStartupErrors(t *testing.T) {
	l, calls := &loaded{}, &mcCalls{}
	if _, _, closer, err := mcBuild(t, mcEnv(t, EnvRecordReadDSNDir), mcDeps(l, calls)); err == nil || !strings.Contains(err.Error(), EnvRecordReadDSNDir) {
		t.Errorf("no Record reader: error %v; want it to name %s", err, EnvRecordReadDSNDir)
	} else {
		closer()
	}
	d := productionDeps()
	d.LoadKeys = l.load
	if d.TrustAdapters == nil {
		t.Fatal("the production build has no adapter source")
	}
	if _, _, closer, err := mcBuild(t, mcEnv(t), d); err == nil || !strings.Contains(err.Error(), "Intents") {
		t.Errorf("production adapters: error %v; want it to name the first missing adapter, Intents", err)
	} else {
		closer()
	}
	d.TrustAdapters = func(Env, reconstruct.TenantDBs, clock.Clock) (TrustAdapters, error) {
		return TrustAdapters{}, errors.New("synthetic adapter failure")
	}
	if _, _, closer, err := mcBuild(t, mcEnv(t), d); err == nil || !strings.Contains(err.Error(), "synthetic adapter failure") {
		t.Errorf("adapter source failure: error %v; want it returned", err)
	} else {
		closer()
	}
}

// INV-29: the production loader is keyfiles.Load: a directory with no key files is refused, never an empty ring.
func TestTheProductionKeyLoaderRefusesADirectoryWithNoKeys(t *testing.T) {
	d := productionDeps()
	if d.LoadKeys == nil {
		t.Fatal("no production key loader")
	}
	if keys, err := d.LoadKeys(t.TempDir(), 1); err == nil || keys != nil {
		t.Fatalf("an empty key directory gave keys %v, error %v", keys != nil, err)
	}
}

// A330: each Trust-only setting by itself asks for Trust, so a partial configuration is an error and never a silent absence of Trust.
func TestEachTrustOnlySettingAloneAsksForTrust(t *testing.T) {
	for _, only := range []string{EnvTrustDBTimeout, EnvGrantKeyDir, EnvGrantKeyMaxActive} {
		e := mcEnv(t, EnvTrustDSNDir, EnvTrustDBTimeout, EnvGrantKeyDir, EnvGrantKeyMaxActive)
		e[only] = trustEnv(t)[only]
		l, calls := &loaded{}, &mcCalls{}
		_, _, closer, err := mcBuild(t, e, mcDeps(l, calls))
		if err == nil || calls.adapters != 1 {
			t.Errorf("%s alone: error %v, adapters asked %d; want an error from buildTrust", only, err, calls.adapters)
			continue
		}
		closer()
	}
}
