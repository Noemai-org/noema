package main

// STM-01 final composition, slice 3 (SEC-03; REQ-SEC-003, AGENT-13, POC-NFR-06, ruling A154): the logger every component of the process receives goes through the redactor, and
// the trace attribute filter is the redactor's. Nothing the core logs may carry a secret-shaped value or a digit run longer than four. platform/redaction README limit (4) says the
// composition root installs it. There is no tracer in the core yet: the filter is exposed on the Process for the tracer's commit. Every value is SYNTHETIC.

import (
	"errors"
	"strings"
	"testing"
)

const (
	mcCard   = "4111111111111111"
	mcSpaced = "4111 1111 1111 1111"
	mcBearer = "abcdefgh12345678"
	mcToken  = "synthetic-token-value-zzz"
	mcPass   = "hunter2hunter2"
)

func mcLeaks(t *testing.T, out string) {
	t.Helper()
	for _, leak := range []string{mcCard, mcSpaced, mcBearer, mcToken, mcPass, "4111111111", "1111 1111 1111"} {
		if strings.Contains(out, leak) {
			t.Errorf("the log writer received %q: %s", leak, out)
		}
	}
	if !strings.Contains(out, "1111") {
		t.Errorf("the masked card's last four digits are missing: %s", out)
	}
}

func mcNoise(l interface {
	Info(string, ...any)
	Error(string, ...any)
}) {
	l.Info("card "+mcCard+" authorization: Bearer "+mcBearer, "token", mcToken, "account", mcSpaced)
	l.Error("configuration", "error", errors.New("password="+mcPass+" for "+mcCard).Error())
}

func TestTheProcessLoggerNeverWritesASecretOrALongDigitRun(t *testing.T) {
	var sink strings.Builder
	l, red := newProcessLogger(&sink)
	if l == nil || red == nil {
		t.Fatal("no logger or no redactor")
	}
	mcNoise(l)
	mcLeaks(t, sink.String())
}

// The logger every wire_* receives is that one: the Record reader's access logger and the logger of the process are the redacting logger.
func TestTheProcessGivesEveryComponentTheRedactingLogger(t *testing.T) {
	var sink strings.Builder
	p, err := mcProcess(t, envMap{}, composeDeps{}, &sink)
	if err != nil || p.Logger == nil {
		t.Fatalf("process %v, error %v", p, err)
	}
	mcNoise(p.Logger)
	mcNoise(p.Components.AccessLog)
	mcLeaks(t, sink.String())
}

func TestTheTraceFilterIsTheRedactorsAndMasksSecretsAndLongDigitRuns(t *testing.T) {
	var sink strings.Builder
	p, err := mcProcess(t, envMap{}, composeDeps{}, &sink)
	if err != nil || p.TraceFilter == nil {
		t.Fatalf("process %v, error %v", p, err)
	}
	out := p.TraceFilter(map[string]string{"account": mcCard, "note": "card " + mcSpaced, "token": mcToken, "ok": "ten_01"})
	for k, v := range out {
		for _, leak := range []string{mcCard, mcSpaced, mcToken, "4111111111"} {
			if strings.Contains(v, leak) || strings.Contains(k, leak) {
				t.Errorf("%s=%q carries %q", k, v, leak)
			}
		}
	}
	if out["ok"] != "ten_01" || !strings.HasSuffix(out["account"], "1111") {
		t.Errorf("filter output %v: harmless values must stay and a card keeps its last four", out)
	}
}
