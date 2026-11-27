package main

// STM-01 compose, follow-up of A722 (interim; DEC-2026-11-26-architect-rulings-a721-a737). REQ-STM-001, REQ-CTX-007, REQ-AUD-001; INV-16, INV-29; AGENT-13, AGENT-16. The evidence login's three
// settings (NOEMA_EVIDENCE_DSN_DIR, NOEMA_EVIDENCE_KEY_FILE, NOEMA_EVIDENCE_KEY_MAX_ACTIVE) are required with no default in code: absent is a named gap, an invalid one stops the process. The
// injection screen carries the one verified pack: it never loads the pack itself. Every value is SYNTHETIC.

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/packload"
)

var evNames = []string{"NOEMA_EVIDENCE_DSN_DIR", "NOEMA_EVIDENCE_KEY_FILE", "NOEMA_EVIDENCE_KEY_MAX_ACTIVE"}

// evRuling is a SYNTHETIC set: the paths are not real, and 2 is the bench overlap (A594).
func evRuling() envMap {
	return envMap{evNames[0]: "/synthetic/evidence-dsn", evNames[1]: "/synthetic/evidence/key", evNames[2]: "2"}
}

// REQ-STM-001, AGENT-16, A722: the names are NOEMA_EVIDENCE_*; with none given every one is a named gap, in this order, and no default is invented.
func TestTheEvidenceLoginSettingsAreRequiredWithNoDefault(t *testing.T) {
	got := []string{EnvEvidenceDSNDir, EnvEvidenceKeyFile, EnvEvidenceKeyMaxActive}
	for i := range evNames {
		if got[i] != evNames[i] {
			t.Errorf("setting %d is %q, want %q", i, got[i], evNames[i])
		}
	}
	for name, env := range map[string]Env{"empty": {Getenv: envMap{}.get}, "no getenv": {}} {
		set, missing, err := evidenceSettingsFrom(env)
		if err != nil || set != (evidenceSettings{}) {
			t.Errorf("%s: settings %+v, err %v; a default was invented", name, set, err)
		}
		if strings.Join(missing, ",") != strings.Join(evNames, ",") {
			t.Errorf("%s: missing %v, want every setting in order %v", name, missing, evNames)
		}
	}
}

func TestAPartlyGivenSetOfEvidenceSettingsNamesOnlyTheMissingOnes(t *testing.T) {
	for _, name := range evNames {
		e := evRuling()
		delete(e, name)
		_, missing, err := evidenceSettingsFrom(Env{Getenv: e.get})
		if err != nil || len(missing) != 1 || missing[0] != name {
			t.Errorf("without %s: missing %v, err %v", name, missing, err)
		}
	}
}

func TestEachEvidenceSettingReachesItsOwnField(t *testing.T) {
	e := envMap{evNames[0]: "/synthetic/a", evNames[1]: "/synthetic/b", evNames[2]: "7"}
	set, missing, err := evidenceSettingsFrom(Env{Getenv: e.get})
	if err != nil || len(missing) != 0 || set != (evidenceSettings{DSNDir: "/synthetic/a", KeyFile: "/synthetic/b", KeyMaxActive: 7}) {
		t.Fatalf("settings %+v, missing %v, err %v", set, missing, err)
	}
}

// AGENT-13, A469: a present overlap that is not a whole positive number stops the process whatever else is missing, and the error names the setting and never the value.
func TestAnInvalidEvidenceOverlapIsFatalAndNeverEchoesItsValue(t *testing.T) {
	for _, bad := range []string{"0", "-1", "abc", "1.5", " 7", "007", "1e3", "99999999999999999999999"} {
		e := envMap{EnvEvidenceKeyMaxActive: bad} // the other two absent
		_, _, err := evidenceSettingsFrom(Env{Getenv: e.get})
		if err == nil {
			t.Errorf("%s=%q was accepted", EnvEvidenceKeyMaxActive, bad)
			continue
		}
		if !strings.Contains(err.Error(), EnvEvidenceKeyMaxActive) || (strings.TrimSpace(bad) != "" && strings.Contains(err.Error(), bad)) {
			t.Errorf("%s=%q: error %q must name the setting and never the value", EnvEvidenceKeyMaxActive, bad, err)
		}
	}
}

// The screen is not built without the evidence login's settings (a finding is recorded by that login, A664, A704): the screen names each missing one, after the limits, and builds none.
func TestTheScreenNamesAMissingEvidenceSettingAndIsNotBuilt(t *testing.T) {
	l, _ := injLog()
	for _, name := range evNames {
		e := twProductionEnv(t)
		for k, v := range injRuling(t) {
			e[k] = v
		}
		delete(e, name)
		s, err := buildInjectionScreen(mcEnvOf(e), loadVerified, l, nil)
		if err != nil || screenGiven(s.Deps) || len(s.Missing) != 1 || s.Missing[0] != name {
			t.Errorf("without %s: screen %+v, err %v; want it named and no screen", name, s, err)
		}
	}
	e := twProductionEnv(t)
	for k, v := range injRuling(t) {
		e[k] = v
	}
	e[EnvEvidenceKeyMaxActive] = "zero"
	if _, err := buildInjectionScreen(mcEnvOf(e), loadVerified, l, nil); err == nil || !strings.Contains(err.Error(), EnvEvidenceKeyMaxActive) || strings.Contains(err.Error(), "zero") {
		t.Errorf("an invalid overlap: err %v; want fatal, naming the setting", err)
	}
}

// The composition: a missing evidence setting is named on the adapters that read the screen; an invalid one stops the process.
func TestTheCompositionNamesAMissingEvidenceSettingAndStopsOnAnInvalidOne(t *testing.T) {
	e := twProductionEnv(t)
	for k, v := range injRuling(t) {
		e[k] = v
	}
	delete(e, EnvEvidenceKeyFile)
	var calls twCalls
	d := twDeps(&loaded{})
	inner := d.WireTrust
	var wired trustWired
	d.WireTrust = func(in trustWiringIn) (trustWired, error) {
		w, err := inner(in)
		wired = w
		return w, err
	}
	c, _, closer, err := twBuild(t, e, d, &calls)
	if err != nil {
		t.Fatalf("a missing setting is a named gap, not a stop: %v", err)
	}
	defer closer()
	if c.InjectionFailures != nil {
		t.Error("a counter was built without the evidence login's settings")
	}
	for _, a := range []string{"Evidence", "Policies"} {
		if !slices.Contains(wired.Reasons[a], EnvEvidenceKeyFile) {
			t.Errorf("%s reasons %v; want %s named", a, wired.Reasons[a], EnvEvidenceKeyFile)
		}
	}
	_, _, closeAll, err := mcBuild(t, envMap{EnvEvidenceKeyMaxActive: "abc"}, composeDeps{})
	closeAll()
	if err == nil || !strings.Contains(err.Error(), EnvEvidenceKeyMaxActive) || strings.Contains(err.Error(), "abc") {
		t.Fatalf("error %v; want one that names the setting and not the value", err)
	}
}

// A722, INV-16: the screen is built from the verified pack the composition's pack source already loaded; with a source that gives it, the screen is built even though the environment names no
// pack, which a second load by the screen could not do.
func TestTheScreenIsBuiltFromTheVerifiedPackItIsGivenAndNeverLoadsOne(t *testing.T) {
	v := tiVerified(t)
	var calls int
	src := func(Env) (packload.Pack, error) { calls++; return v, nil }
	l, _ := injLog()
	s, err := buildInjectionScreen(mcEnvOf(injRuling(t)), src, l, nil) // no pack setting in the environment
	if err != nil || !screenGiven(s.Deps) || len(s.Missing) != 0 || s.Failures == nil {
		t.Fatalf("screen %+v, err %v; want a screen built from the given verified pack", s, err)
	}
	if calls != 1 {
		t.Errorf("the verified source was asked %d times, want once", calls)
	}
	var none int
	gone := func(Env) (packload.Pack, error) { none++; return packload.Pack{}, errPackNotConfigured }
	g, err := buildInjectionScreen(mcEnvOf(injRuling(t)), gone, l, nil)
	if err != nil || screenGiven(g.Deps) || len(g.Missing) != 1 || g.Missing[0] != needInjectionPatterns {
		t.Errorf("a pack that is not configured: screen %+v, err %v; want the patterns named", g, err)
	}
	if g, err := buildInjectionScreen(mcEnvOf(injRuling(t)), nil, l, nil); err != nil || screenGiven(g.Deps) || len(g.Missing) != 1 {
		t.Errorf("no source: screen %+v, err %v; want the patterns named", g, err)
	}
	broken := func(Env) (packload.Pack, error) { return packload.Pack{}, errors.New("synthetic load failure") }
	if _, err := buildInjectionScreen(mcEnvOf(injRuling(t)), broken, l, nil); err == nil {
		t.Error("a pack that failed to load started the screen")
	}
}

// A722: the composition hands the screen the pack source's one load. The deps' Verified is asked once for the composition, and the screen is built from it with no pack setting at all.
func TestTheCompositionBuildsTheScreenFromTheDepsVerifiedPack(t *testing.T) {
	v := tiVerified(t)
	var calls int
	c, _, closer, err := mcBuild(t, injRuling(t), composeDeps{Verified: func(Env) (packload.Pack, error) { calls++; return v, nil }})
	if err != nil {
		t.Fatalf("the process must start: %v", err)
	}
	defer closer()
	if c.InjectionFailures == nil || calls != 1 {
		t.Errorf("counter %v, verified source asked %d times; want a built screen and one ask", c.InjectionFailures, calls)
	}
}

// A722, A548: in production the Trust pack and the screen's pack are the one load. After the pack source answered, the pack directory may vanish: the verified source still answers, from the
// same load, with the patterns.
func TestProductionGivesThePackSourceAndTheScreenOneLoad(t *testing.T) {
	dir := psrcCopy(t)
	e := psrcEnv(dir)
	env := Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}
	d := productionDeps()
	if d.Verified == nil {
		t.Fatal("productionDeps has no verified source: the screen would load the pack a second time")
	}
	if _, err := d.Pack(env); err != nil {
		t.Fatalf("the pack source: %v", err)
	}
	if err := os.RemoveAll(filepath.Clean(dir)); err != nil {
		t.Fatal(err)
	}
	v, err := d.Verified(env)
	if err != nil || len(v.InjectionPatterns) == 0 {
		t.Fatalf("the verified source after the pack source: patterns %d, err %v; want the same one load", len(v.InjectionPatterns), err)
	}
}

// The screen's file loads no pack of its own (A722): a source-level guard, as for the INV-14 guard, so a second load cannot come back by a refactor.
func TestTheInjectionScreenFileNeverLoadsThePack(t *testing.T) {
	raw, err := os.ReadFile("compose_injection.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"packload.Load(", "EnvPackDir", "EnvPackManifest", "EnvPackMaxBytes", "packBound("} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("compose_injection.go contains %q: the screen must carry the verified pack, not load one", banned)
		}
	}
}

// A722, A594, AGENT-16: the bench values sit in the bench Compose file only: the directory follows the existing DSN-directory pattern and the overlap is 2. The key file's path is not named by
// any ruling, so no value is set for it here (a named gap until IL provisions it).
func TestTheBenchComposeSetsTheEvidenceDirectoryAndOverlapAndInventsNoKeyPath(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "deploy", "compose", "bench-stack.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, want := range []string{"NOEMA_EVIDENCE_DSN_DIR: /run/evidence-dsn", `NOEMA_EVIDENCE_KEY_MAX_ACTIVE: "2"`} {
		if !strings.Contains(text, want) {
			t.Errorf("bench-stack.yml lacks %q", want)
		}
	}
	if strings.Contains(text, "NOEMA_EVIDENCE_KEY_FILE:") {
		t.Error("bench-stack.yml sets NOEMA_EVIDENCE_KEY_FILE: no ruling names the path (AGENT-16)")
	}
}
