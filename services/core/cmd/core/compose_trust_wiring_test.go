package main

// STM-01 final composition, the part that does not need the events route (A643, A550, A551, A612, A512, interim, relayed, not yet committed): compose builds the Trust inputs of lane 2
// (buildTrustInputs) and the decisions of lane 3 (buildDecisions) over the ONE manifest-verified pack and the ONE set of Trust logins, with the real adapters; a part whose input is
// absent is a named gap with fixed text and no value, a present invalid one stops the process (A469, A486, A330). REQ-STM-001, REQ-TRU-009, REQ-INT-001; INV-12, INV-14, INV-16;
// AGENT-13, AGENT-16. Tests of the database-backed adapters themselves are in the lanes' own files; nothing here dials a database (pools connect lazily). Every value is SYNTHETIC.

import (
	"context"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/trust/grants"
)

// regexpDigitRun finds a run of five digits: a fixed reason carries none (AGENT-13).
var regexpDigitRun = regexp.MustCompile(`[0-9]{5,}`)

func twKeys(t *testing.T) *grants.Keys {
	t.Helper()
	k, err := (&loaded{}).load("", 1)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

// twIn is a complete input over the real pack: every part that can be given is given.
func twIn(t *testing.T) trustWiringIn {
	t.Helper()
	v := tiVerified(t)
	pack, err := trustpack.LoadVerified(v.Pack, v.ActionGraphs)
	if err != nil {
		t.Fatal(err)
	}
	rs, m := tiParts(t, v)
	e := envMap{EnvTrustDBTimeout: "750", EnvPayloadMaxBytes: "1048576"}
	return trustWiringIn{
		Env: Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, Clock: clock.NewManual(time.Unix(1_800_000_000, 0)), Pack: pack,
		Tenants: []string{"ten_aa", "ten_bb"}, Pools: tiPools(t, "ten_aa", "ten_bb"), Keys: twKeys(t),
		Understanding:   UnderstandingParts{RuleSet: rs, Mapping: m, PackRef: pack.Ref()},
		OwnershipReader: &tiCandidates{}, Screen: tiScreen(), ListEntities: tiLists(),
	}
}

// twProduction is what production gives today: no enterprise read, no injection screen, no list convention, no Record write login, no sealed-facts source.
func twProduction(t *testing.T) trustWiringIn {
	in := twIn(t)
	in.OwnershipReader, in.Screen, in.ListEntities = nil, ScreenDeps{}, nil
	return in
}

func twEnv(in *trustWiringIn, set map[string]string) {
	e := envMap{}
	for _, n := range []string{EnvTrustDBTimeout, EnvPayloadMaxBytes} {
		e[n] = in.Env.Getenv(n)
	}
	for k, v := range set {
		if v == "" {
			delete(e, k)
		} else {
			e[k] = v
		}
	}
	in.Env = Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
}

// Everything that can be built is built, from the one verified pack and the shared logins, and what cannot is named in the order of TrustAdapters: Intents, Ownership, Evidence and Policies
// are built when their inputs exist; Writes (noema#935), Candidates (no candidates read), Reconciler, Clocks, Confirmations, Planner and the Recorder (no Record write login, no sealed
// facts) are named gaps. REQ-STM-001, REQ-TRU-003; INV-12, INV-16.
func TestWiringBuildsWhatItCanFromTheVerifiedPackAndNamesTheRest(t *testing.T) {
	w, err := wireTrustInputs(twIn(t))
	if err != nil {
		t.Fatal(err)
	}
	a := w.fill(TrustAdapters{})
	if _, ok := a.Intents.(*trustport.StoredIntents); !ok {
		t.Errorf("Intents is %T, want the admitted-intent store adapter", a.Intents)
	}
	if a.Ownership == nil || a.Evidence == nil || a.Policies == nil {
		t.Errorf("Ownership %v, Evidence %v, Policies %v: each must be built when its inputs are given", a.Ownership != nil, a.Evidence != nil, a.Policies != nil)
	}
	want := []string{"Candidates", "Reconciler", "Clocks", "Confirmations", "Planner", "Recorder"}
	if got := w.Unbuilt(); !reflect.DeepEqual(got, want) {
		t.Errorf("unbuilt = %v, want %v", got, want)
	}
}

// What production gives today: Intents and Writes (the pack's write rule and the tenants' kill switch, A684 step 1); the rest is named, with the reason as fixed text.
func TestWiringWithTheProductionInputsBuildsIntentsAndNamesTheRest(t *testing.T) {
	w, err := wireTrustInputs(twProduction(t))
	if err != nil {
		t.Fatal(err)
	}
	a := w.fill(TrustAdapters{})
	if a.Intents == nil || a.Writes == nil || a.Ownership != nil || a.Evidence != nil || a.Policies != nil || a.Recorder != nil || a.Candidates != nil {
		t.Errorf("built: Intents %v, Ownership %v, Evidence %v, Policies %v, Recorder %v, Candidates %v, Writes %v", a.Intents != nil, a.Ownership != nil, a.Evidence != nil, a.Policies != nil, a.Recorder != nil, a.Candidates != nil, a.Writes != nil)
	}
	want := []string{"Candidates", "Ownership", "Evidence", "Reconciler", "Policies", "Clocks", "Confirmations", "Planner", "Recorder"}
	if got := w.Unbuilt(); !reflect.DeepEqual(got, want) {
		t.Fatalf("unbuilt = %v, want %v", got, want)
	}
	for _, name := range want {
		reasons := w.Reasons[name]
		if len(reasons) == 0 {
			t.Errorf("%s is a gap with no reason", name)
		}
		for _, r := range reasons {
			if r == "" || strings.ContainsAny(r, "\n\x00") || regexpDigitRun.MatchString(r) {
				t.Errorf("%s: the reason %q is not fixed plain text", name, r)
			}
		}
	}
}

// The grant keys given to the wiring are the ones the Recorder is told it has: with them the Recorder no longer lacks them; without them it does. The Record write login and the sealed
// facts are what is left (no adapter on main fills either).
func TestTheGrantKeysReachTheRecorder(t *testing.T) {
	with, err := wireTrustInputs(twIn(t))
	if err != nil {
		t.Fatal(err)
	}
	in := twIn(t)
	in.Keys = nil
	without, err := wireTrustInputs(in)
	if err != nil {
		t.Fatal(err)
	}
	has := func(w trustWired, text string) bool {
		for _, r := range w.Reasons["Recorder"] {
			if r == text {
				return true
			}
		}
		return false
	}
	if has(with, needGrantKeys) || !has(without, needGrantKeys) {
		t.Errorf("the Recorder lacks the grant keys: with them %v, without them %v; want false, true", has(with, needGrantKeys), has(without, needGrantKeys))
	}
	if !has(with, needRecordLogin) || !has(with, needSealedFacts) {
		t.Errorf("the Recorder's reasons are %v; want the Record write login and the sealed facts named", with.Reasons["Recorder"])
	}
}

// A486: an absent input is a named gap, never an error and never a quiet adapter.
func TestAnAbsentInputIsANamedGapAndNotAnError(t *testing.T) {
	cases := map[string]struct {
		change func(*trustWiringIn)
		gap    string
		reason string
	}{
		"no pack":             {func(in *trustWiringIn) { in.Pack = nil }, "Intents", needPack},
		"no trust logins":     {func(in *trustWiringIn) { in.Pools = nil }, "Intents", needTrustLogins},
		"no read timeout":     {func(in *trustWiringIn) { twEnv(in, map[string]string{EnvTrustDBTimeout: ""}) }, "Intents", EnvTrustDBTimeout},
		"no payload bound":    {func(in *trustWiringIn) { twEnv(in, map[string]string{EnvPayloadMaxBytes: ""}) }, "Evidence", EnvPayloadMaxBytes},
		"no mapping":          {func(in *trustWiringIn) { in.Understanding.Mapping = nil }, "Evidence", ""},
		"no injection screen": {func(in *trustWiringIn) { in.Screen = ScreenDeps{} }, "Evidence", ""},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			in := twIn(t)
			tc.change(&in)
			w, err := wireTrustInputs(in)
			if err != nil {
				t.Fatalf("a gap is not an error: %v", err)
			}
			unbuilt := w.Unbuilt()
			found := false
			for _, n := range unbuilt {
				found = found || n == tc.gap
			}
			if !found {
				t.Fatalf("unbuilt = %v; want %s named", unbuilt, tc.gap)
			}
			if tc.reason != "" {
				ok := false
				for _, r := range w.Reasons[tc.gap] {
					ok = ok || strings.Contains(r, tc.reason)
				}
				if !ok {
					t.Errorf("%s reasons %v; want one that names %q", tc.gap, w.Reasons[tc.gap], tc.reason)
				}
			}
		})
	}
}

// A469: a present value that is invalid stops the process, and the error names the setting, never the value.
func TestAPresentInvalidInputIsAnErrorThatNamesTheSettingAndNotTheValue(t *testing.T) {
	for name, tc := range map[string]struct {
		set     map[string]string
		setting string
		value   string
	}{
		"read timeout":  {map[string]string{EnvTrustDBTimeout: "0"}, EnvTrustDBTimeout, "0"},
		"payload bound": {map[string]string{EnvPayloadMaxBytes: "lots-of-bytes"}, EnvPayloadMaxBytes, "lots-of-bytes"},
	} {
		t.Run(name, func(t *testing.T) {
			in := twIn(t)
			twEnv(&in, tc.set)
			_, err := wireTrustInputs(in)
			if err == nil || !strings.Contains(err.Error(), tc.setting) || (len(tc.value) > 1 && strings.Contains(err.Error(), tc.value)) {
				t.Fatalf("error %v; want one that names %s and not its value", err, tc.setting)
			}
		})
	}
	t.Run("a served tenant with no Trust login", func(t *testing.T) {
		in := twIn(t)
		in.Tenants = append(in.Tenants, "ten_zz")
		_, err := wireTrustInputs(in)
		if err == nil || strings.Contains(err.Error(), "ten_zz") {
			t.Fatalf("error %v; want one that does not name the tenant", err)
		}
	})
}

// A612: the limits file has no loader on main (noema#910 is open). Until it merges, Clocks is a named gap; when the loader lands, a refused result must stop the start or leave Clocks a
// gap that FAILS CLOSED, never a default. The comment that says so is pinned by this test, and the gap is pinned whatever else is given.
func TestTheLimitsFileIsANamedGapThatFailsClosedAndNeverADefault(t *testing.T) {
	w, err := wireTrustInputs(twIn(t))
	if err != nil {
		t.Fatal(err)
	}
	if w.fill(TrustAdapters{}).Clocks != nil {
		t.Error("Clocks is built with no limits loader: a default was invented")
	}
	if len(w.Reasons["Clocks"]) == 0 || !strings.Contains(strings.Join(w.Reasons["Clocks"], " "), needTrustLimits) {
		t.Errorf("Clocks reasons = %v; want the limits file named", w.Reasons["Clocks"])
	}
	src, err := os.ReadFile("compose_trust_wiring.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, must := range []string{"noema#910", "FAILS CLOSED", "never a default"} {
		if !strings.Contains(string(src), must) {
			t.Errorf("compose_trust_wiring.go no longer says %q about the limits file", must)
		}
	}
}

// A548, INV-16: the pack is loaded and verified once for the composition; a second call gets the first answer, a failure included.
func TestThePackIsVerifiedOnceForTheWholeComposition(t *testing.T) {
	var calls int32
	want := twIn(t).Pack
	src := memoPackSource(func(Env) (*trustpack.Pack, error) { atomic.AddInt32(&calls, 1); return want, nil })
	for i := 0; i < 3; i++ {
		if got, err := src(Env{}); err != nil || got != want {
			t.Fatalf("call %d: %v, %v", i, got, err)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 1 {
		t.Errorf("the pack source ran %d times, want once", n)
	}
	fail := memoPackSource(func(Env) (*trustpack.Pack, error) { atomic.AddInt32(&calls, 1); return nil, errPackNotConfigured })
	for i := 0; i < 2; i++ {
		if _, err := fail(Env{}); err != errPackNotConfigured {
			t.Fatalf("a failure is not remembered: %v", err)
		}
	}
	if n := atomic.LoadInt32(&calls); n != 2 {
		t.Errorf("a failing source ran %d times in all, want twice (once each)", n)
	}
	if productionDeps().Pack == nil || productionDeps().WireTrust == nil {
		t.Error("production gives the composition no pack source or no wiring")
	}
}

type twCalls struct {
	dsnReads []string
	wired    []trustWiringIn
}

func twBuild(t *testing.T, e envMap, d composeDeps, c *twCalls) (Components, TrustParts, func(), error) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 24, 9, 0, 0, 0, time.UTC))
	env := Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: func(n string) ([]os.DirEntry, error) {
		if n == e[EnvTrustDSNDir] {
			c.dsnReads = append(c.dsnReads, n)
		}
		return os.ReadDir(n)
	}}
	if d.WireTrust != nil {
		inner := d.WireTrust
		d.WireTrust = func(in trustWiringIn) (trustWired, error) { c.wired = append(c.wired, in); return inner(in) }
	}
	return buildComponentsWith(t.Context(), env, clk, ids.New(clk, nil), discardLog(), d)
}

func twProductionEnv(t *testing.T) envMap {
	t.Helper()
	e := mcEnv(t)
	for k, v := range psrcEnv(psrcPacks()) {
		e[k] = v
	}
	e[edge.EnvTenants] = "ten_aa"
	return e
}

func twDeps(l *loaded) composeDeps {
	d := productionDeps()
	d.LoadKeys = l.load
	return d
}

// The production composition with a pack and every Trust setting: Intents is built over the verified pack, and /readyz names what is not, in the order of TrustAdapters. Capabilities,
// Scope, Authority, the clock and the pack version stay built, as before.
func TestProductionComposeBuildsIntentsOverTheVerifiedPackAndNamesTheRest(t *testing.T) {
	var calls twCalls
	c, parts, closer, err := twBuild(t, twProductionEnv(t), twDeps(&loaded{}), &calls)
	if err != nil {
		t.Fatalf("the process must start: %v", err)
	}
	defer closer()
	want := []string{"Candidates", "Ownership", "Evidence", "Reconciler", "Policies", "Clocks", "Confirmations", "Planner", "Recorder"}
	if !reflect.DeepEqual(c.TrustUnbuilt, want) {
		t.Errorf("TrustUnbuilt = %v, want %v", c.TrustUnbuilt, want)
	}
	if c.Ports.Trust != nil || parts.Service != nil {
		t.Error("Trust is plugged while adapters are still named as missing")
	}
	if len(calls.wired) != 1 {
		t.Fatalf("the wiring ran %d times, want once", len(calls.wired))
	}
	in := calls.wired[0]
	if in.Pack == nil || in.Pack.Ref() == "" || in.Keys == nil || len(in.Pools) != 1 || in.Pools["ten_aa"] == nil || !reflect.DeepEqual(in.Tenants, []string{"ten_aa"}) {
		t.Errorf("the wiring got pack %v, keys %v, pools %d, tenants %v; want the one pack, the loaded keys, one login of the served tenant", in.Pack != nil, in.Keys != nil, len(in.Pools), in.Tenants)
	}
	if _, reason := composeReadiness(c, nil)(); strings.Contains(reason, "trust.Intents") {
		t.Errorf("/readyz still names Intents: %q", reason)
	}
}

// One set of Trust logins for the wiring and for Trust: the directory is read once, whichever path builds Trust (the shared pool set; no second lazy set).
func TestTheTrustLoginsAreOpenedOnceWhicheverPathBuildsTrust(t *testing.T) {
	var unbuilt twCalls
	_, _, closeA, err := twBuild(t, twProductionEnv(t), twDeps(&loaded{}), &unbuilt)
	if err != nil {
		t.Fatal(err)
	}
	closeA()
	if len(unbuilt.dsnReads) != 1 {
		t.Errorf("with adapters still missing the Trust login directory was read %d times, want once", len(unbuilt.dsnReads))
	}
	var built twCalls
	d := mcDeps(&loaded{}, &mcCalls{})
	_, _, closeB, err := twBuild(t, mcEnv(t), d, &built)
	if err != nil {
		t.Fatal(err)
	}
	closeB()
	if len(built.dsnReads) != 1 {
		t.Errorf("with every adapter built the Trust login directory was read %d times, want once", len(built.dsnReads))
	}
}

// With no pack settings the pack is a named gap, the wiring still runs and names Intents with the rest; nothing is invented.
func TestWithNoPackTheWiringNamesIntentsAndStillStarts(t *testing.T) {
	e := mcEnv(t)
	e[EnvTrustEscalationGraph] = syntheticEscalationGraph
	var calls twCalls
	c, _, closer, err := twBuild(t, e, twDeps(&loaded{}), &calls)
	if err != nil {
		t.Fatalf("the process must start: %v", err)
	}
	defer closer()
	joined := strings.Join(c.TrustUnbuilt, ",")
	for _, n := range []string{"Pack", "Intents", "Recorder"} {
		if !strings.Contains(joined, n) {
			t.Errorf("TrustUnbuilt = %v; want %s named", c.TrustUnbuilt, n)
		}
	}
	if len(calls.dsnReads) != 1 {
		t.Errorf("the Trust login directory was read %d times, want once", len(calls.dsnReads))
	}
}

// A551: a pack that fails verification is a startup error, and nothing is built from it: the wiring is never called, so no accessor, no adapter and no gap list is made from bytes nobody
// checked.
func TestAPackThatFailsVerificationBuildsNothing(t *testing.T) {
	dir := psrcCopy(t)
	path := dir + "/fintech/graph/g1_investigate.json"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(raw, ' '), 0o644); err != nil { // one byte changed, the manifest not resealed
		t.Fatal(err)
	}
	e := twProductionEnv(t)
	e[EnvPackDir] = dir
	var calls twCalls
	c, _, closer, err := twBuild(t, e, twDeps(&loaded{}), &calls)
	if closer != nil {
		defer closer()
	}
	if err == nil {
		t.Fatal("a pack that does not verify must stop the process")
	}
	if len(calls.wired) != 0 || len(c.TrustUnbuilt) != 0 || c.Ports.Trust != nil {
		t.Errorf("wiring calls %d, gaps %v, trust %v; want nothing built from an unverified pack", len(calls.wired), c.TrustUnbuilt, c.Ports.Trust != nil)
	}
	if strings.Contains(err.Error(), dir) {
		t.Errorf("the error names the pack directory: %v", err)
	}
}

// Written after the hand mutation check: the one pack source of production answers once (two calls, one Pack), and the Trust logins the composition opened for the wiring are closed
// with the rest (a pool left open would outlive the process's own close).
func TestProductionsPackSourceAnswersOnceAndTheSharedLoginsAreClosedWithTheComposition(t *testing.T) {
	e := twProductionEnv(t)
	env := Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	d := productionDeps()
	first, err := d.Pack(env)
	if err != nil || first == nil {
		t.Fatalf("pack %v, %v", first, err)
	}
	if second, err := d.Pack(env); err != nil || second != first {
		t.Fatalf("a second call gave another Pack (%p, %p, %v): the pack was loaded twice", first, second, err)
	}
	var calls twCalls
	_, _, closer, err := twBuild(t, e, twDeps(&loaded{}), &calls)
	if err != nil || len(calls.wired) != 1 {
		t.Fatalf("build: %v, wiring calls %d", err, len(calls.wired))
	}
	pool := calls.wired[0].Pools["ten_aa"]
	closer()
	closer() // safe to call twice
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := pool.Ping(ctx); err == nil || !strings.Contains(err.Error(), "closed pool") {
		t.Errorf("a shared Trust login is still open after the composition closed: %v", err)
	}
}
