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

// needInjectionRecorder is the fixed text of the gap of a screen built with no Recorder (SR on #1018): the screen is built for the Alert side only, so a finding is an alert and a count
// and nothing is recorded; and the count has no exporter yet, so a zero is not a healthy screen. It carries no value.
const needInjectionRecorder = "the injection recorder: not wired, so a flagged payload is an alert and a count and no Record event, and the count has no exporter, so a count of zero is not a healthy screen"

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

// buildInjectionScreen builds the screen from the environment and the composition's one verified pack (A722: the screen carries it and loads none of its own): the five limits, the evidence
// login's three settings, then the pack's pinned pattern bytes. An absent limit or evidence setting, or a pack that is not configured, is a named gap; an invalid setting, a pack that does
// not load or patterns the limits refuse stop the process (A704, A469). It dials nothing and asks the verified source only once the settings are complete.
func buildInjectionScreen(env Env, verified VerifiedSource, log *slog.Logger, rec injection.Recorder) (injectionScreen, error) {
	lim, missing, err := injectionLimitsFrom(env)
	if err != nil {
		return injectionScreen{}, err
	}
	_, evMissing, err := evidenceSettingsFrom(env)
	if err != nil {
		return injectionScreen{}, err
	}
	if missing = append(missing, evMissing...); len(missing) > 0 {
		return injectionScreen{Missing: missing}, nil
	}
	if verified == nil {
		return injectionScreen{Missing: []string{needInjectionPatterns}}, nil
	}
	v, err := verified(env)
	if errors.Is(err, errPackNotConfigured) {
		return injectionScreen{Missing: []string{needInjectionPatterns}}, nil
	}
	if err != nil {
		return injectionScreen{}, err
	}
	return newInjectionScreen(v, lim, log, rec)
}

// The evidence login's three settings (A722, A536, A594): the directory of the per-tenant DSN files, the key file of the core-evidence identity, and how many keys of it may be mounted at
// once. They have no default in code (AGENT-16); the bench numbers are in the bench Compose file only. Nothing here dials or reads them: the Record port that uses them is a later lane's.
const (
	EnvEvidenceDSNDir       = "NOEMA_EVIDENCE_DSN_DIR"
	EnvEvidenceKeyFile      = "NOEMA_EVIDENCE_KEY_FILE"
	EnvEvidenceKeyMaxActive = "NOEMA_EVIDENCE_KEY_MAX_ACTIVE"
)

// evidenceSettings are the evidence login's three settings once all are given.
type evidenceSettings struct {
	DSNDir       string
	KeyFile      string
	KeyMaxActive int
}

// evidenceSettingsFrom reads the three settings. An absent one is named in missing (in the order of the constants); a present overlap that is not a whole positive number is an error that
// names the setting and never the value, whatever else is missing (A469).
func evidenceSettingsFrom(env Env) (set evidenceSettings, missing []string, err error) {
	get := func(name string) string {
		if env.Getenv == nil {
			return ""
		}
		return env.Getenv(name)
	}
	var given [3]bool
	set.DSNDir, set.KeyFile = get(EnvEvidenceDSNDir), get(EnvEvidenceKeyFile)
	given[0], given[1] = set.DSNDir != "", set.KeyFile != ""
	if given[2] = get(EnvEvidenceKeyMaxActive) != ""; given[2] {
		n, err := wholeNumber(env, EnvEvidenceKeyMaxActive, "")
		if err != nil {
			return evidenceSettings{}, nil, err
		}
		if int64(int(n)) != n {
			return evidenceSettings{}, nil, fmt.Errorf("cmd/core: %s must be a whole positive number", EnvEvidenceKeyMaxActive)
		}
		set.KeyMaxActive = int(n)
	}
	for i, name := range []string{EnvEvidenceDSNDir, EnvEvidenceKeyFile, EnvEvidenceKeyMaxActive} {
		if !given[i] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return evidenceSettings{}, missing, nil
	}
	return set, nil, nil
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
