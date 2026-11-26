package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/Noemai-org/noema/services/core/evidence/injection"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/platform/telemetry"
)

// compose_injection.go builds the injection screen of the payload reader for the composition (STM-01; A704, A330, A664 round 2, A372, interim, relayed, not yet committed; REQ-STM-001,
// REQ-CTX-007, REQ-AUD-001; INV-14, INV-16, INV-19; AGENT-09, AGENT-13, AGENT-16). The five bounds of the detector (injection.Limits) are settings with no default in code: their numbers are
// the Architect's (A704) and are set in the bench Compose file only. The patterns are the bytes the manifest pinned (packload.Pack.InjectionPatterns). The Alert writes one line through the
// redactor and counts noema_injection_record_failures_total. The correlation comes from the request's chain. This file wires no Record port: the screen is built with the Recorder it is
// given, and the composition gives none yet, so a finding is then a Record failure that raises the Alert (never a silent pass). It wires nothing but the screen.

// The five settings. MaxPatternBytes bounds one regular expression in bytes (injection.Limits.MaxPatternLen); FieldCap is the cap on one enterprise string, in characters.
const (
	EnvInjectionMaxPatterns      = "NOEMA_INJECTION_MAX_PATTERNS"
	EnvInjectionMaxPatternBytes  = "NOEMA_INJECTION_MAX_PATTERN_BYTES"
	EnvInjectionMaxDocumentBytes = "NOEMA_INJECTION_MAX_DOCUMENT_BYTES"
	EnvInjectionMaxDepth         = "NOEMA_INJECTION_MAX_DEPTH"
	EnvInjectionFieldCap         = "NOEMA_INJECTION_FIELD_CAP"
)

// MetricInjectionRecordFailures is the name of the counter of flagged payloads whose Record event failed (PERD 44.1: noema_<name>_total, no label).
const MetricInjectionRecordFailures = "noema_injection_record_failures_total"

// needInjectionPatterns is the fixed text of the gap of a pack that gives no pattern data (A664): there is no empty screen.
const needInjectionPatterns = "the pack's injection patterns"

// recordFailureCounter counts noema_injection_record_failures_total. The core has no metric registry or exporter yet (platform/telemetry: "metrics: not here yet"); this is the value a
// registry reads when one exists.
type recordFailureCounter struct{ n atomic.Uint64 }

// Inc counts one failure.
func (c *recordFailureCounter) Inc() { c.n.Add(1) }

// Value is the count so far.
func (c *recordFailureCounter) Value() uint64 { return c.n.Load() }

// Name is the metric's name.
func (c *recordFailureCounter) Name() string { return MetricInjectionRecordFailures }

// injectionScreen is what the composition gets: the screen's three parts and the failure counter, or, when something is missing, none of them and the names of what is missing (fixed text,
// no value).
type injectionScreen struct {
	Deps     ScreenDeps
	Failures *recordFailureCounter
	Missing  []string
}

// injectionLimitsFrom reads the five settings. An absent one is named in missing (in the order of the constants); a present one that is not a whole positive number is an error that
// names the setting and never the value, whatever else is missing (A469).
func injectionLimitsFrom(env Env) (lim injection.Limits, missing []string, err error) {
	read := func(name string, into *int) error {
		if env.Getenv == nil || env.Getenv(name) == "" {
			missing = append(missing, name)
			return nil
		}
		n, err := wholeNumber(env, name, "")
		if err != nil {
			return err
		}
		if int64(int(n)) != n {
			return fmt.Errorf("cmd/core: %s must be a whole positive number", name)
		}
		*into = int(n)
		return nil
	}
	for _, s := range []struct {
		name string
		into *int
	}{
		{EnvInjectionMaxPatterns, &lim.MaxPatterns}, {EnvInjectionMaxPatternBytes, &lim.MaxPatternLen}, {EnvInjectionMaxDocumentBytes, &lim.MaxBytes},
		{EnvInjectionMaxDepth, &lim.MaxDepth}, {EnvInjectionFieldCap, &lim.MaxFieldRunes},
	} {
		if err := read(s.name, s.into); err != nil {
			return injection.Limits{}, nil, err
		}
	}
	if len(missing) > 0 {
		return injection.Limits{}, missing, nil
	}
	return lim, nil, nil
}

// correlationOfContext is the screen's correlation source: the correlation ID the edge minted, from the request's chain. Without one it is empty, and the reader refuses the read.
func correlationOfContext(ctx context.Context) string { return telemetry.CorrelationFrom(ctx) }

// injectionAlert is the operational alert for a flagged payload whose Record event failed: one line through the redactor, then the count. The line carries the tenant, the evidence id and
// the names of the fields and patterns, never a value (AGENT-13).
type injectionAlert struct {
	log      *slog.Logger
	failures *recordFailureCounter
}

func (a injectionAlert) ScreenFlagged(ctx context.Context, f payloadread.Flag) {
	a.failures.Inc()
	fields, patterns := make([]string, 0, len(f.Findings)), make([]string, 0, len(f.Findings))
	for _, x := range f.Findings {
		fields = append(fields, x.Field)
		patterns = append(patterns, x.PatternID)
	}
	telemetry.Logger(ctx, a.log).LogAttrs(ctx, slog.LevelError, "injection screen: a flagged payload could not be recorded",
		slog.String("tenant_id", f.TenantID), slog.String("evidence_id", f.EvidenceID),
		slog.String("fields", strings.Join(fields, ",")), slog.String("patterns", strings.Join(patterns, ",")))
}

// newInjectionScreen builds the screen over the verified pack's pattern bytes and the limits. A pack with no pattern data is a named gap, never an empty screen (A664). A file the limits
// refuse is an error whose text carries nothing of the pack. rec may be nil: a finding is then a Record failure and raises the Alert. log is the redacting process logger.
func newInjectionScreen(v packload.Pack, lim injection.Limits, log *slog.Logger, rec injection.Recorder) (injectionScreen, error) {
	if len(v.InjectionPatterns) == 0 || v.InjectionPatternsID == "" {
		return injectionScreen{Missing: []string{needInjectionPatterns}}, nil
	}
	if log == nil {
		return injectionScreen{}, errors.New("cmd/core: the injection alert has no logger: a flagged payload whose Record event fails would be unseen")
	}
	det, err := injection.Parse(v.InjectionPatterns, v.InjectionPatternsID, lim)
	if err != nil {
		return injectionScreen{}, inputsFail("the injection patterns cannot be loaded under the configured limits", err)
	}
	if isNilValue(rec) {
		rec = nil
	}
	counter := &recordFailureCounter{}
	return injectionScreen{
		Deps:     ScreenDeps{Screen: &injection.Screener{Detector: det, Recorder: rec}, Correlation: correlationOfContext, Alert: injectionAlert{log: log, failures: counter}},
		Failures: counter,
	}, nil
}

// buildInjectionScreen builds the screen from the environment: the five limits, then the pack's pattern bytes through the manifest (the same settings and the same loader as the rest of the
// composition: every file is pinned by its SHA-256). An absent limit, or no complete pack setting, is a named gap; an invalid limit, a pack that does not load or patterns the limits
// refuse stop the process (A704, A469). It dials nothing.
func buildInjectionScreen(env Env, _ VerifiedSource, log *slog.Logger, rec injection.Recorder) (injectionScreen, error) {
	lim, missing, err := injectionLimitsFrom(env)
	if err != nil {
		return injectionScreen{}, err
	}
	if len(missing) > 0 {
		return injectionScreen{Missing: missing}, nil
	}
	dir, manifest, bound := env.Getenv(EnvPackDir), env.Getenv(EnvPackManifest), env.Getenv(EnvPackMaxBytes)
	if dir == "" || manifest == "" || bound == "" {
		return injectionScreen{Missing: []string{needInjectionPatterns}}, nil
	}
	maxBytes, err := packBound(env, bound)
	if err != nil {
		return injectionScreen{}, err
	}
	v, err := packload.Load(dir, manifest, maxBytes)
	if err != nil {
		return injectionScreen{}, fmt.Errorf("cmd/core: %s: the pack cannot be loaded: %w", EnvPackDir, err)
	}
	return newInjectionScreen(v, lim, log, rec)
}

// annotate names what the screen lacks on the adapters that read it (the Evidence read, the Policies read and Clocks), when they are still unbuilt. A built screen adds nothing.
func (s injectionScreen) annotate(w *trustWired) {
	if len(s.Missing) == 0 {
		return
	}
	if w.Reasons == nil {
		w.Reasons = map[string][]string{}
	}
	unbuilt := w.Unbuilt()
	for _, a := range []string{"Evidence", "Policies", "Clocks"} {
		if slices.Contains(unbuilt, a) {
			w.Reasons[a] = append(w.Reasons[a], s.Missing...)
		}
	}
}

// The three evidence login settings (A722). Stub: nothing reads them yet.
const (
	EnvEvidenceDSNDir       = "NOEMA_EVIDENCE_DSN_DIR"
	EnvEvidenceKeyFile      = "NOEMA_EVIDENCE_KEY_FILE"
	EnvEvidenceKeyMaxActive = "NOEMA_EVIDENCE_KEY_MAX_ACTIVE"
)

type evidenceSettings struct {
	DSNDir       string
	KeyFile      string
	KeyMaxActive int
}

func evidenceSettingsFrom(env Env) (evidenceSettings, []string, error) {
	return evidenceSettings{}, nil, nil
}
