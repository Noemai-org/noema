package main

// STM-01 final composition, the compose failure of the 20 Nov run (interim, relayed, not yet committed: EL's decision, with A445). A Trust that is configured but whose adapters are
// NOT YET BUILT (the named, expected gaps: the pack and every adapter that reads it) does not stop the process: it starts, /healthz is 200, /readyz is 503 and names `trust` and each
// missing adapter (fixed text, no value), and Trust is not plugged, so the orchestrator keeps its fail-closed "not built" Trust. A setting that is present but invalid stays a startup
// error. REQ-TRU-009, REQ-API-004, REQ-SEC-003; INV-03, INV-29, AGENT-13, AGENT-16. Every value is SYNTHETIC.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/reconstruct"
	"github.com/Noemai-org/noema/services/core/trust/grants"
)

func unbuiltBuild(t *testing.T, e envMap, l *loaded) (Components, TrustParts, func(), error) {
	t.Helper()
	d := productionDeps()
	d.LoadKeys = l.load
	return mcBuild(t, e, d)
}

// With every Trust setting given and the production seams (no pack source until noema#773), the composition is built: no error, Trust not plugged, no Trust parts, and /readyz names it.
func TestTrustConfiguredButItsAdaptersNotBuiltStartsWithoutTrustAndReadyzNamesThem(t *testing.T) {
	l := &loaded{}
	c, parts, closer, err := unbuiltBuild(t, mcEnv(t), l)
	if err != nil {
		t.Fatalf("the process must start: %v", err)
	}
	defer closer()
	if c.Ports.Trust != nil || parts.Trust != nil || parts.Bindings != nil || parts.Service != nil {
		t.Errorf("Trust is plugged (%v) or its parts are built: the orchestrator's Trust must stay the not-built one", c.Ports.Trust != nil)
	}
	ok, reason := composeReadiness(c, nil)()
	if ok {
		t.Fatal("ready with Trust unbuilt")
	}
	for _, name := range []string{"trust", "trust.Pack", "trust.Intents", "trust.Writes", "trust.PackVersion"} {
		if !strings.Contains(reason, name) {
			t.Errorf("/readyz says %q; want it to name %s", reason, name)
		}
	}
}

// The served mux answers 200 on /healthz and 503 on /readyz with the names, and the body carries no setting's value, path or key (AGENT-13).
func TestTheServedMuxIsAliveAndNotReadyNamingTheUnbuiltTrust(t *testing.T) {
	e := mcEnv(t)
	c, _, closer, err := unbuiltBuild(t, e, &loaded{})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	mux := newMux(composeReadiness(c, nil), nil)
	h := httptest.NewRecorder()
	mux.ServeHTTP(h, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	r := httptest.NewRecorder()
	mux.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	body := r.Body.String()
	if h.Code != http.StatusOK || r.Code != http.StatusServiceUnavailable || !strings.Contains(body, "trust.Pack") {
		t.Fatalf("healthz %d, readyz %d %q; want 200, 503 naming trust.Pack", h.Code, r.Code, body)
	}
	for _, secret := range []string{e[EnvGrantKeyDir], e[EnvTrustDSNDir], e[EnvRecordReadDSNDir], "postgres://"} {
		if secret != "" && strings.Contains(body, secret) {
			t.Errorf("/readyz carries %q", secret)
		}
	}
}

// A setting that is present but invalid stays fatal, also while the adapters are unbuilt: a bad number, a grant key directory that cannot be loaded, a key loader that returns no keys.
// The error names the setting. AGENT-16, INV-29.
func TestAnInvalidTrustSettingIsStillAStartupErrorWhileTheAdaptersAreUnbuilt(t *testing.T) {
	cases := map[string]struct {
		mut  func(envMap)
		load func(string, int) (*grants.Keys, error)
		want string
	}{
		"timeout not a number": {func(e envMap) { e[EnvTrustDBTimeout] = "soon" }, nil, EnvTrustDBTimeout},
		"timeout zero":         {func(e envMap) { e[EnvTrustDBTimeout] = "0" }, nil, EnvTrustDBTimeout},
		"max active zero":      {func(e envMap) { e[EnvGrantKeyMaxActive] = "0" }, nil, EnvGrantKeyMaxActive},
		"keys unreadable":      {func(envMap) {}, func(string, int) (*grants.Keys, error) { return nil, errors.New("synthetic unreadable key directory") }, EnvGrantKeyDir},
		"keys nil":             {func(envMap) {}, func(string, int) (*grants.Keys, error) { return nil, nil }, EnvGrantKeyDir},
	}
	for name, c := range cases {
		l := &loaded{}
		e := mcEnv(t)
		c.mut(e)
		d := productionDeps()
		d.LoadKeys = l.load
		if c.load != nil {
			d.LoadKeys = c.load
		}
		_, _, closer, err := mcBuild(t, e, d)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: error %v; want one naming %s", name, err, c.want)
			continue
		}
		closer()
	}
}

// A Trust setting is still required once any is given, with the adapters unbuilt: the missing one is named (AGENT-16).
func TestAMissingTrustSettingIsStillAnErrorWhileTheAdaptersAreUnbuilt(t *testing.T) {
	for _, missing := range []string{EnvTrustDBTimeout, EnvGrantKeyDir, EnvGrantKeyMaxActive, EnvTrustDSNDir, EnvRecordReadDSNDir} {
		_, _, closer, err := unbuiltBuild(t, mcEnv(t, missing), &loaded{})
		if err == nil || !strings.Contains(err.Error(), missing) {
			t.Errorf("%s missing: error %v; want one naming it", missing, err)
			continue
		}
		closer()
	}
}

// An adapter source that fails for any reason but the named gaps (the pack not on main, an adapter not built) is a startup error: the process never starts on an unknown failure.
func TestAnAdapterSourceFailureThatIsNotANamedGapIsAStartupError(t *testing.T) {
	d := productionDeps()
	d.LoadKeys = (&loaded{}).load
	d.TrustAdapters = func(Env, reconstruct.TenantDBs, clock.Clock) (TrustAdapters, error) {
		return TrustAdapters{}, errors.New("synthetic adapter failure")
	}
	if _, _, closer, err := mcBuild(t, mcEnv(t), d); err == nil || !strings.Contains(err.Error(), "synthetic adapter failure") {
		t.Errorf("error %v; want the source's failure", err)
	} else {
		closer()
	}
}

// A built Trust names nothing: the unbuilt names go away with the gap (the process with complete adapters is unchanged).
func TestCompleteAdaptersPlugTrustAndReadyzNamesNoTrustLine(t *testing.T) {
	c, _, closer, err := mcBuild(t, mcEnv(t), mcDeps(&loaded{}, &mcCalls{}))
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if _, reason := composeReadiness(c, nil)(); strings.Contains(reason, "trust") {
		t.Errorf("/readyz says %q with Trust built", reason)
	}
}
