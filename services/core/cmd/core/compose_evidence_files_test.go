package main

// STM-01 compose, review of #1018 (SR at 93af22ec, findings 3 and 4; A722, A536, A594, A664). REQ-STM-001, REQ-CTX-007, REQ-SEC-009; INV-16, INV-29; AGENT-13, AGENT-16. The evidence login's
// key file and DSN directory are checked through the shared readers (keyfiles.ReadFile, the DSN directory listing of loginPools): a present setting that names an unsafe file stops the
// process, and an error names the setting and never the path or the content. The overlap has the upper bound of the Trust key ring. A screen built with no Recorder names the gap. Every
// value is SYNTHETIC.

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/intake"
)

// evRulingReal is evRuling over files that exist and are safe: a 0400 DSN file for one tenant in a directory, and a 0400 key file. The content is a placeholder, not a key.
func evRulingReal(t *testing.T) envMap {
	t.Helper()
	dir, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("evidence_aa")})
	key := filepath.Join(t.TempDir(), "core-evidence.key")
	if err := os.WriteFile(key, []byte("SYNTHETIC placeholder, not a key\n"), 0o400); err != nil {
		t.Fatal(err)
	}
	e := evRuling()
	e[EnvEvidenceDSNDir], e[EnvEvidenceKeyFile] = dir, key
	return e
}

func evScreenEnv(t *testing.T, e envMap) envMap {
	t.Helper()
	all := twProductionEnv(t)
	for k, v := range injRuling(t) {
		all[k] = v
	}
	for k, v := range e {
		all[k] = v
	}
	return all
}

// REQ-STM-001, A722 (bench 2), A739: the overlap has the upper bound of every key ring, 999; a value above it stops the process and the error never echoes the value.
func TestTheEvidenceKeyOverlapHasTheUpperBoundOfEveryKeyRing(t *testing.T) {
	ok := evRuling()
	ok[EnvEvidenceKeyMaxActive] = "999"
	if set, _, err := evidenceSettingsFrom(Env{Getenv: ok.get}); err != nil || set.KeyMaxActive != 999 {
		t.Errorf("the largest value of the bound: settings %+v, err %v", set, err)
	}
	for _, bad := range []string{"1000", "1000000000", "4294967296"} {
		e := evRuling()
		e[EnvEvidenceKeyMaxActive] = bad
		_, _, err := evidenceSettingsFrom(Env{Getenv: e.get})
		if err == nil || !strings.Contains(err.Error(), EnvEvidenceKeyMaxActive) || strings.Contains(err.Error(), bad) {
			t.Errorf("%s=%q: err %v; want a refusal that names the setting and not the value", EnvEvidenceKeyMaxActive, bad, err)
		}
	}
}

// AGENT-13, INV-29, REQ-SEC-009: the key file and every DSN file are read by the shared checked reader. Each unsafe case stops the process; the error names the setting and carries
// neither the path nor the content, and no screen is built.
func TestAnUnsafeEvidenceKeyFileOrDSNDirectoryStopsTheScreen(t *testing.T) {
	l, _ := injLog()
	keyOf := func(e envMap) string { return e[EnvEvidenceKeyFile] }
	dirOf := func(e envMap) string { return e[EnvEvidenceDSNDir] }
	cases := map[string]struct {
		setting string
		break_  func(t *testing.T, e envMap)
	}{
		"key mode 0600": {EnvEvidenceKeyFile, func(t *testing.T, e envMap) { mustChmod(t, keyOf(e), 0o600) }},
		"key mode 0440": {EnvEvidenceKeyFile, func(t *testing.T, e envMap) { mustChmod(t, keyOf(e), 0o440) }},
		"key symlink": {EnvEvidenceKeyFile, func(t *testing.T, e envMap) {
			real := keyOf(e) + ".real"
			mustNil(t, os.Rename(keyOf(e), real))
			mustNil(t, os.Symlink(real, keyOf(e)))
		}},
		"key oversize": {EnvEvidenceKeyFile, func(t *testing.T, e envMap) {
			mustChmod(t, keyOf(e), 0o600)
			mustNil(t, os.WriteFile(keyOf(e), []byte(strings.Repeat("x", privateKeyMaxBytes+1)), 0o600))
			mustChmod(t, keyOf(e), 0o400)
		}},
		"key absent file": {EnvEvidenceKeyFile, func(t *testing.T, e envMap) { mustNil(t, os.Remove(keyOf(e))) }},
		"dsn mode 0644":   {EnvEvidenceDSNDir, func(t *testing.T, e envMap) { mustChmod(t, filepath.Join(dirOf(e), "ten_aa"), 0o644) }},
		"dsn symlink": {EnvEvidenceDSNDir, func(t *testing.T, e envMap) {
			mustNil(t, os.Rename(filepath.Join(dirOf(e), "ten_aa"), filepath.Join(t.TempDir(), "elsewhere")))
			mustNil(t, os.Symlink(keyOf(e), filepath.Join(dirOf(e), "ten_aa")))
		}},
		"dsn oversize": {EnvEvidenceDSNDir, func(t *testing.T, e envMap) {
			p := filepath.Join(dirOf(e), "ten_aa")
			mustChmod(t, p, 0o600)
			mustNil(t, os.WriteFile(p, []byte(strings.Repeat("x", dsnFileMaxBytes+1)), 0o600))
			mustChmod(t, p, 0o400)
		}},
		"dsn entry not a tenant name": {EnvEvidenceDSNDir, func(t *testing.T, e envMap) {
			mustNil(t, os.WriteFile(filepath.Join(dirOf(e), "not-a-tenant"), []byte("x"), 0o400))
		}},
		"dsn directory empty": {EnvEvidenceDSNDir, func(t *testing.T, e envMap) { mustNil(t, os.Remove(filepath.Join(dirOf(e), "ten_aa"))) }},
		"dsn directory absent": {EnvEvidenceDSNDir, func(t *testing.T, e envMap) {
			mustNil(t, os.Remove(filepath.Join(dirOf(e), "ten_aa")))
			mustNil(t, os.Remove(dirOf(e)))
		}},
	}
	for name, c := range cases {
		e := evScreenEnv(t, evRulingReal(t))
		c.break_(t, e)
		s, err := buildInjectionScreen(mcEnvOf(e), loadVerified, l, nil)
		if err == nil {
			t.Errorf("%s: the screen was built over an unsafe evidence input", name)
			continue
		}
		msg := err.Error()
		if !strings.Contains(msg, c.setting) || strings.Contains(msg, e[EnvEvidenceKeyFile]) || strings.Contains(msg, e[EnvEvidenceDSNDir]) || strings.Contains(msg, "SYNTHETIC") || strings.Contains(msg, "postgres://") {
			t.Errorf("%s: error %q must name %s and carry no path or content", name, msg, c.setting)
		}
		if screenGiven(s.Deps) {
			t.Errorf("%s: a screen came with the error", name)
		}
	}
}

// A safe key file and DSN directory build the screen: the check refuses nothing it should accept.
func TestASafeEvidenceKeyFileAndDSNDirectoryBuildTheScreen(t *testing.T) {
	l, _ := injLog()
	s, err := buildInjectionScreen(mcEnvOf(evScreenEnv(t, evRulingReal(t))), loadVerified, l, nil)
	if err != nil || !screenGiven(s.Deps) {
		t.Fatalf("screen %+v, err %v; want a screen over safe files", s, err)
	}
}

// A669-style order: the files are checked once the settings are complete. An absent setting (a named gap) comes first and the files are not touched; an unsafe file does not turn a gap
// into a stop.
func TestTheEvidenceFilesAreCheckedOnlyOnceEverySettingIsGiven(t *testing.T) {
	l, _ := injLog()
	e := evScreenEnv(t, evRulingReal(t))
	mustChmod(t, e[EnvEvidenceKeyFile], 0o644) // unsafe
	delete(e, EnvInjectionMaxDepth)            // and a limit is absent
	s, err := buildInjectionScreen(mcEnvOf(e), loadVerified, l, nil)
	if err != nil || screenGiven(s.Deps) || !slices.Equal(s.Missing, []string{EnvInjectionMaxDepth}) {
		t.Errorf("screen %+v, err %v; want the absent limit named and no stop", s, err)
	}
}

// The composition: an unsafe evidence key file stops the process and names the setting.
func TestTheCompositionStopsOnAnUnsafeEvidenceKeyFile(t *testing.T) {
	e := evScreenEnv(t, evRulingReal(t))
	mustChmod(t, e[EnvEvidenceKeyFile], 0o644)
	var calls twCalls
	_, _, closer, err := twBuild(t, e, twDeps(&loaded{}), &calls)
	closer()
	if err == nil || !strings.Contains(err.Error(), EnvEvidenceKeyFile) || strings.Contains(err.Error(), e[EnvEvidenceKeyFile]) {
		t.Fatalf("error %v; want a stop that names %s and not the path", err, EnvEvidenceKeyFile)
	}
}

type builtEvidence struct{}

func (builtEvidence) SnapshotFreshness(context.Context, string, string) (intake.SnapshotEvidence, error) {
	return intake.SnapshotEvidence{}, nil
}

// SR finding 3, A664, A704: a screen built with no Recorder is built for the Alert side only. The adapters that read it (Evidence, Policies, Clocks) are named with the fixed gap text of
// the injection recorder, built or not; the text carries no value and says the counter has no exporter, so a zero is not read as healthy.
func TestAScreenWithNoRecorderNamesTheInjectionRecorderGapOnTheAdaptersThatReadIt(t *testing.T) {
	s, _ := injBuilt(t, nil)
	for name, w := range map[string]trustWired{
		"unbuilt adapters": {Reasons: map[string][]string{}},
		"built Evidence":   {Inputs: trustInputs{Evidence: builtEvidence{}}},
	} {
		s.annotate(&w)
		for _, a := range []string{"Evidence", "Policies", "Clocks"} {
			if !slices.Contains(w.Reasons[a], needInjectionRecorder) {
				t.Errorf("%s: %s reasons %v; want %q", name, a, w.Reasons[a], needInjectionRecorder)
			}
		}
		for _, a := range []string{"Intents", "Reconciler", "Recorder", "Writes"} {
			if slices.Contains(w.Reasons[a], needInjectionRecorder) {
				t.Errorf("%s: %s does not read the screen", name, a)
			}
		}
	}
	if !strings.Contains(needInjectionRecorder, "injection recorder") || !strings.Contains(needInjectionRecorder, "exporter") {
		t.Errorf("gap text %q must name the injection recorder and say the counter has no exporter", needInjectionRecorder)
	}
	if strings.ContainsAny(needInjectionRecorder, "0123456789/") {
		t.Errorf("gap text %q carries a value", needInjectionRecorder)
	}
}

// With a Recorder the gap is not named; a screen that is not built names its missing settings and not the recorder.
func TestTheInjectionRecorderGapIsNamedOnlyForABuiltScreenWithNoRecorder(t *testing.T) {
	built, _ := injBuilt(t, &injRecorder{})
	w := trustWired{Reasons: map[string][]string{}}
	built.annotate(&w)
	if len(w.Reasons) != 0 {
		t.Errorf("a screen with a Recorder named gaps: %v", w.Reasons)
	}
	w2 := trustWired{Reasons: map[string][]string{}}
	injectionScreen{Missing: []string{EnvInjectionMaxDepth}}.annotate(&w2)
	for a, rs := range w2.Reasons {
		if slices.Contains(rs, needInjectionRecorder) {
			t.Errorf("%s: an unbuilt screen named the recorder gap: %v", a, rs)
		}
	}
}

// The composition: the production screen has no Recorder yet, so the wiring is told the gap on the three adapters.
func TestTheCompositionNamesTheInjectionRecorderGap(t *testing.T) {
	e := evScreenEnv(t, evRulingReal(t))
	var calls twCalls
	d := twDeps(&loaded{})
	inner := d.WireTrust
	var wired trustWired
	d.WireTrust = func(in trustWiringIn) (trustWired, error) {
		w, err := inner(in)
		wired = w
		return w, err
	}
	_, _, closer, err := twBuild(t, e, d, &calls)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	for _, a := range []string{"Evidence", "Policies", "Clocks"} {
		if !slices.Contains(wired.Reasons[a], needInjectionRecorder) {
			t.Errorf("%s reasons %v; want the injection recorder gap", a, wired.Reasons[a])
		}
	}
}

func mustChmod(t *testing.T, p string, m os.FileMode) {
	t.Helper()
	mustNil(t, os.Chmod(p, m))
}

func mustNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
