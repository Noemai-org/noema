package main

// STM-01 pack-source follow-up, the Understanding half (Founder instruction, interim, relayed, not yet committed: A506, A512; with A469, A486, A377, A330). Compose calls
// buildUnderstanding with the Record's admissions, plugs the Interpreter into Ports.Interpreter and keeps RuleSet, Mapping, PackRef and SealTimeout for the later Trust and Evidence
// wiring. A part that is not configured, or only partly, is a gap: the process starts and /readyz names each missing setting as "interpreter: NAME missing" in fixed text; a present
// value that is invalid stays fatal and its error names the setting and never the value. REQ-STM-001, REQ-INT-001; INV-12, INV-29; AGENT-13, AGENT-16. Every value is SYNTHETIC.

import (
	"net/http"
	"strings"
	"testing"
)

// ucSettingNames is every setting of the Understanding half, in the order /readyz names a gap.
var ucSettingNames = []string{EnvPackDir, EnvPackManifest, EnvPackMaxBytes, EnvPackMaxDepth, EnvStoreTimeoutMS, EnvCompileTimeoutMS, EnvSealTimeoutMS,
	EnvModelURL, EnvModelClientCertFile, EnvModelClientKeyFile, EnvModelCAFile, EnvModelMaxResponseBytes}

// ucEnv is a Record reader, the orchestrator's logins and keys, and every Understanding setting over a real pack and a model service stub.
func ucEnv(t *testing.T) envMap {
	t.Helper()
	rd, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	d := newOrchDirs(t, "ten_aa")
	e := envMap{EnvRecordReadDSNDir: rd, EnvOrchestratorDSNDir: d.dsn, EnvOrchestratorKeyDir: d.keys, EnvOrchestratorKeyMaxActive: "2", EnvReadTimeoutMS: "2000"}
	return merged(e, undSettings(copyPack(t), newModelStub(t)))
}

func ucBuild(t *testing.T, e envMap) (Components, func(), error) {
	t.Helper()
	c, _, closer, err := mcBuild(t, e, composeDeps{})
	return c, closer, err
}

func ucReason(c Components) string {
	_, reason := composeReadiness(c, nil)()
	return reason
}

// REQ-STM-001, REQ-INT-001, INV-12: a complete settings set builds the Interpreter through Compose, plugs it into the orchestrator's port and keeps the parts for the later wiring.
func TestACompleteUnderstandingSettingsSetBuildsTheInterpreterAndItsPartsThroughCompose(t *testing.T) {
	c, closer, err := ucBuild(t, ucEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	u := c.Understanding
	if c.Ports.Interpreter == nil || u.Interpreter == nil || c.Ports.Interpreter != u.Interpreter {
		t.Fatalf("Ports.Interpreter %v, parts %v; want the one Interpreter in both", c.Ports.Interpreter, u.Interpreter)
	}
	if u.RuleSet == nil || u.Mapping == nil || !strings.Contains(u.PackRef, "@") || u.SealTimeout.Milliseconds() != 1000 {
		t.Errorf("parts %+v; want the verified rule set, mapping, `<vertical>@<version>` and the seal bound of 1000 ms", u)
	}
	if len(c.UnderstandingUnbuilt) != 0 {
		t.Errorf("gaps %v; want none", c.UnderstandingUnbuilt)
	}
	if reason := ucReason(c); strings.Contains(reason, "interpreter") {
		t.Errorf("/readyz says %q; want it not to name the interpreter", reason)
	}
}

// A486: with none of the settings there is no Interpreter and no error; /readyz names the plain "interpreter".
func TestWithNoUnderstandingSettingThereIsNoInterpreterAndTheGapIsPlain(t *testing.T) {
	e := ucEnv(t)
	for _, n := range ucSettingNames {
		delete(e, n)
	}
	c, closer, err := ucBuild(t, e)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Ports.Interpreter != nil || c.Understanding.Interpreter != nil || len(c.UnderstandingUnbuilt) != 0 {
		t.Errorf("interpreter %v, gaps %v; want nothing", c.Ports.Interpreter, c.UnderstandingUnbuilt)
	}
	if reason := ucReason(c); !strings.Contains(reason, "interpreter") || strings.Contains(reason, "interpreter:") {
		t.Errorf("/readyz says %q; want the plain interpreter gap", reason)
	}
}

// A469, A486: a setting that is missing when others are given is a gap, not a crash: no Interpreter, no error, and /readyz names the setting and no value.
func TestAMissingUnderstandingSettingIsANamedGapAndNotFatal(t *testing.T) {
	for _, drop := range ucSettingNames {
		e := ucEnv(t)
		values := []string{}
		for _, n := range ucSettingNames {
			values = append(values, e[n])
		}
		delete(e, drop)
		c, closer, err := ucBuild(t, e)
		if err != nil {
			t.Fatalf("missing %s: error %v; want a gap", drop, err)
		}
		reason := ucReason(c)
		closer()
		if c.Ports.Interpreter != nil || c.Understanding.Interpreter != nil {
			t.Errorf("missing %s: an Interpreter was built", drop)
		}
		if want := "interpreter: " + drop + " missing"; !strings.Contains(reason, want) {
			t.Errorf("missing %s: /readyz says %q; want %q", drop, reason, want)
		}
		for i, v := range values {
			if ucSettingNames[i] != drop && strings.Contains(reason, v) {
				t.Errorf("missing %s: /readyz carries the value of %s", drop, ucSettingNames[i])
			}
		}
	}
}

// Every missing setting is named, not only the first, in the fixed order, and a setting that is given is not named.
func TestEveryMissingUnderstandingSettingIsNamedInOneOrder(t *testing.T) {
	e := ucEnv(t)
	for _, n := range []string{EnvPackManifest, EnvSealTimeoutMS, EnvModelCAFile} {
		delete(e, n)
	}
	c, closer, err := ucBuild(t, e)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	want := []string{EnvPackManifest, EnvSealTimeoutMS, EnvModelCAFile}
	if got := strings.Join(c.UnderstandingUnbuilt, ","); got != strings.Join(want, ",") {
		t.Errorf("gaps %q; want %q", got, strings.Join(want, ","))
	}
	if strings.Contains(ucReason(c), "interpreter: "+EnvPackDir) {
		t.Error("a setting that is given is named")
	}
}

// The Interpreter stores through the orchestrator's logins and confirms inputs through the Record: without them the gap names the setting that provides them.
func TestWithoutTheOrchestratorLoginsOrTheRecordReaderTheGapNamesThem(t *testing.T) {
	e := ucEnv(t)
	for _, n := range []string{EnvOrchestratorDSNDir, EnvOrchestratorKeyDir, EnvOrchestratorKeyMaxActive} {
		delete(e, n)
	}
	c, closer, err := ucBuild(t, e)
	if err != nil {
		t.Fatal(err)
	}
	if c.Ports.Interpreter != nil || !strings.Contains(ucReason(c), "interpreter: "+EnvOrchestratorDSNDir+" missing") {
		t.Errorf("interpreter %v, /readyz %q; want a gap naming %s", c.Ports.Interpreter, ucReason(c), EnvOrchestratorDSNDir)
	}
	closer()
	delete(e, EnvRecordReadDSNDir)
	c, closer, err = ucBuild(t, e)
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Ports.Interpreter != nil || !strings.Contains(ucReason(c), "interpreter: "+EnvRecordReadDSNDir+" missing") {
		t.Errorf("interpreter %v, /readyz %q; want a gap naming %s", c.Ports.Interpreter, ucReason(c), EnvRecordReadDSNDir)
	}
}

// A469 stands: a present value that cannot be used is fatal, even when another setting is missing, and the error names the setting and never the value.
func TestAPresentButInvalidUnderstandingSettingIsFatalEvenWithAMissingOne(t *testing.T) {
	cases := []struct{ name, value string }{
		{EnvPackMaxBytes, "12x34"}, {EnvPackMaxDepth, "0"}, {EnvStoreTimeoutMS, "-5"}, {EnvCompileTimeoutMS, "1.5"}, {EnvSealTimeoutMS, "abc-synthetic"},
		{EnvModelMaxResponseBytes, "zero-synthetic"}, {EnvModelURL, "http://model.synthetic:1"},
	}
	for _, c := range cases {
		for _, alsoMissing := range []string{"", EnvPackManifest} {
			e := ucEnv(t)
			e[c.name] = c.value
			if alsoMissing != "" {
				delete(e, alsoMissing)
			}
			got, closer, err := ucBuild(t, e)
			if err == nil || !strings.Contains(err.Error(), c.name) || strings.Contains(err.Error(), c.value) {
				t.Errorf("%s=%q (also missing %q): error %v; want a fatal error naming the setting without its value", c.name, c.value, alsoMissing, err)
			}
			if got.Ports.Interpreter != nil {
				t.Errorf("%s=%q: an Interpreter was built", c.name, c.value)
			}
			closer()
		}
	}
}

// The served process starts with a gap and answers 503 naming it; with the complete set /readyz no longer names the interpreter.
func TestTheProcessStartsWithAnUnderstandingGapAndReadyzNamesItWithoutAValue(t *testing.T) {
	e := ucEnv(t)
	delete(e, EnvPackMaxDepth)
	var sink strings.Builder
	var log []string
	p, err := mcProcess(t, e, mcWorkerDeps(&log, nil, true), &sink)
	if err != nil || p == nil || p.Handler == nil {
		t.Fatalf("process %v, error %v; want a process", p, err)
	}
	code, body := mcReadyz(p)
	p.Close()
	if want := "interpreter: " + EnvPackMaxDepth + " missing"; code != http.StatusServiceUnavailable || !strings.Contains(body, want) {
		t.Errorf("status %d, body %s; want 503 naming %q", code, body, want)
	}
	if strings.Contains(body, e[EnvPackDir]) || strings.Contains(body, e[EnvModelURL]) {
		t.Errorf("/readyz carries a value: %s", body)
	}
	full := ucEnv(t)
	p, err = mcProcess(t, full, mcWorkerDeps(&log, nil, true), &sink)
	if err != nil || p == nil {
		t.Fatalf("process %v, error %v", p, err)
	}
	defer p.Close()
	if p.Components.Ports.Interpreter == nil {
		t.Error("the process has no Interpreter with the complete settings")
	}
	if _, body := mcReadyz(p); strings.Contains(body, "interpreter") {
		t.Errorf("/readyz %s; want no interpreter gap", body)
	}
}
