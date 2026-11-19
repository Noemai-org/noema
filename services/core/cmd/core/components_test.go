package main

// EDG-01 slice 3c: main builds its Components in one place, buildComponents, from the environment (A188: EL owns the composition). Each adapter that is merged is plugged in here and
// nothing else is: with none of the settings there is nothing and no error; the Record reader gives the Admissions port; the Record reader and the orchestrator's three settings give
// the Recorder; an orchestrator setting without the Record reader is an error that names it. The missing ports stay missing, so /readyz still names them. Keys and DSNs are SYNTHETIC.
// REQ-STM-001, REQ-AUD-003, REQ-SEC-004, INV-12, AGENT-16.

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

func buildAll(t *testing.T, e envMap) (Components, func(), error) {
	t.Helper()
	clk := clock.NewManual(time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC))
	return buildComponents(t.Context(), Env{Getenv: e.get, ReadFile: os.ReadFile, ReadDir: os.ReadDir}, clk, ids.New(clk, nil), discardLog())
}

// With none of the settings there is nothing to compose and no error: the core serves health only and /readyz names what is missing (the state of today).
func TestWithNoSettingsThereAreNoComponentsAndNoError(t *testing.T) {
	c, closer, err := buildAll(t, envMap{})
	if err != nil || closer == nil {
		t.Fatalf("error %v, closer %v; want no error and a callable closer", err, closer != nil)
	}
	defer closer()
	if c.Records != nil || c.Ports.Admissions != nil || c.Ports.Recorder != nil || c.Ports.Interpreter != nil || c.Ports.Evidence != nil || c.Ports.Trust != nil || c.Ports.Explainer != nil {
		t.Fatalf("components with no settings: %+v", c)
	}
}

// The Record reader brings the Admissions port with it (the same pools, as the session's tenant), and the access logger its reads are logged to (REQ-AUD-003).
func TestTheRecordReaderGivesTheAdmissionsPortAndTheAccessLogger(t *testing.T) {
	dir, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	c, closer, err := buildAll(t, envMap{EnvRecordReadDSNDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Records == nil || c.Ports.Admissions == nil || c.AccessLog == nil {
		t.Fatalf("records %v, admissions %v, access log %v; want all three", c.Records != nil, c.Ports.Admissions != nil, c.AccessLog != nil)
	}
	if c.Ports.Recorder != nil {
		t.Error("a Recorder with none of its settings")
	}
}

// The Recorder needs the Record reader and its three settings; with both, it is plugged in.
func TestTheRecordReaderAndTheOrchestratorSettingsGiveTheRecorder(t *testing.T) {
	rd, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	o := newOrchDirs(t, "ten_aa")
	c, closer, err := buildAll(t, envMap{EnvRecordReadDSNDir: rd, EnvOrchestratorDSNDir: o.dsn, EnvOrchestratorKeyDir: o.keys, EnvOrchestratorKeyMaxActive: "2", EnvReadTimeoutMS: "2000"})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	if c.Ports.Recorder == nil || c.Ports.Admissions == nil {
		t.Fatalf("recorder %v, admissions %v; want both", c.Ports.Recorder != nil, c.Ports.Admissions != nil)
	}
}

// Fail closed: an orchestrator setting without the Record reader, a partial set of the orchestrator's settings and a bad Record reader directory are all errors that name the setting,
// with no components and a closer that is safe to call (every pool opened so far is closed).
func TestAnUnusableSettingRefusesToStartAndNamesIt(t *testing.T) {
	rd, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	o := newOrchDirs(t, "ten_aa")
	empty := t.TempDir()
	for name, c := range map[string]struct {
		env  envMap
		want string
	}{
		"the Recorder without the Record reader": {envMap{EnvOrchestratorDSNDir: o.dsn, EnvOrchestratorKeyDir: o.keys, EnvOrchestratorKeyMaxActive: "2"}, EnvRecordReadDSNDir},
		"a partial set of the Recorder's":        {envMap{EnvRecordReadDSNDir: rd, EnvOrchestratorDSNDir: o.dsn}, EnvOrchestratorKeyDir},
		"an empty Record reader directory":       {envMap{EnvRecordReadDSNDir: empty}, EnvRecordReadDSNDir},
	} {
		t.Run(name, func(t *testing.T) {
			got, closer, err := buildAll(t, c.env)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v; want one naming %s", err, c.want)
			}
			if got.Records != nil || got.Ports.Recorder != nil || got.Ports.Admissions != nil {
				t.Errorf("components built with an error: %+v", got)
			}
			if closer == nil {
				t.Fatal("no closer: a caller would panic")
			}
			closer()
		})
	}
}
