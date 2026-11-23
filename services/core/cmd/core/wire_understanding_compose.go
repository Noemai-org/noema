package main

import (
	"fmt"
	"net/url"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// wire_understanding_compose.go is how Compose plugs the Understanding half in (STM-01, the pack-source follow-up, Founder instruction, interim, relayed, not yet committed: A506, A512;
// A469, A486 for the gaps; A377 for what the later wiring takes from the parts; REQ-STM-001, REQ-INT-001; INV-12, INV-29; AGENT-13, AGENT-16). It decides only between three outcomes: no
// setting of the half is given (the plain "interpreter" gap), some are missing (a gap that names each, as "interpreter: NAME missing", fixed text and never a value), or the set is
// complete and buildUnderstanding builds it. A value that is present and invalid is fatal in every case, whatever else is missing, and its error names the setting and never the value.
//
// The parts other than the Interpreter (RuleSet, Mapping, PackRef, SealTimeout) are kept in Components.Understanding for the Trust and Evidence wiring, which is not built here. The Trust
// intake's PackVersion (from the manifest-verified pack) and CorrelationOf (the snapshot's own correlation, required equal to the evaluation's) stay named gaps of that wiring ("trust.*",
// "evidence").

// understandingSettings is every setting of the half, in the order /readyz names a gap.
var understandingSettings = []string{
	EnvPackDir, EnvPackManifest, EnvPackMaxBytes, EnvPackMaxDepth, EnvStoreTimeoutMS, EnvCompileTimeoutMS, EnvSealTimeoutMS,
	EnvModelURL, EnvModelClientCertFile, EnvModelClientKeyFile, EnvModelCAFile, EnvModelMaxResponseBytes,
}

// understandingGiven says whether any setting of the half is given. None of them is read by anything else, so one alone asks for the Interpreter.
func understandingGiven(env Env) bool {
	for _, n := range understandingSettings {
		if env.Getenv(n) != "" {
			return true
		}
	}
	return false
}

// understandingMissing names what keeps the Interpreter from being built, in a fixed order: the Record's admissions and the orchestrator's Recorder it stands on, then each setting that
// is absent. It is empty when none of the settings is given (the plain gap, nothing to name) and when the half can be built.
func understandingMissing(env Env, hasRecorder, hasAdmissions bool) []string {
	if !understandingGiven(env) {
		return nil
	}
	var m []string
	if !hasAdmissions {
		m = append(m, EnvRecordReadDSNDir)
	}
	if !hasRecorder {
		m = append(m, EnvOrchestratorDSNDir)
	}
	for _, n := range understandingSettings {
		if env.Getenv(n) == "" {
			m = append(m, n)
		}
	}
	return m
}

// checkUnderstandingValues refuses a present value that cannot be used (A469), whatever else is missing. The files and the pack are judged when the half is built.
func checkUnderstandingValues(env Env) error {
	for _, n := range []string{EnvPackMaxBytes, EnvPackMaxDepth} {
		if env.Getenv(n) != "" {
			if _, err := wholeNumber(env, n, ""); err != nil {
				return err
			}
		}
	}
	for _, n := range []string{EnvStoreTimeoutMS, EnvCompileTimeoutMS, EnvSealTimeoutMS} {
		if env.Getenv(n) != "" {
			if _, err := wholeMillis(env, n); err != nil {
				return err
			}
		}
	}
	if env.Getenv(EnvModelMaxResponseBytes) != "" {
		if _, err := modelMaxBytes(env); err != nil {
			return err
		}
	}
	if v := env.Getenv(EnvModelURL); v != "" {
		if u, err := url.Parse(v); err != nil || u.Scheme != "https" || u.Host == "" {
			return fmt.Errorf("cmd/core: %s must be https://host[:port]: the model service is called over mutual TLS only", EnvModelURL)
		}
	}
	return nil
}

// composeUnderstanding returns the parts, or the gap (names, no parts, no error), or an error. It dials nothing.
func composeUnderstanding(env Env, clk clock.Clock, recorder *orchestratorRecorder, admissions orchestrator.Admissions) (UnderstandingParts, []string, error) {
	if err := checkUnderstandingValues(env); err != nil {
		return UnderstandingParts{}, nil, err
	}
	if gap := understandingMissing(env, recorder != nil, admissions != nil); len(gap) > 0 {
		return UnderstandingParts{}, gap, nil
	}
	if !understandingGiven(env) {
		return UnderstandingParts{}, nil, nil // none of the settings is given: the plain gap
	}
	parts, err := buildUnderstanding(env, clk, recorder, admissions)
	return parts, nil, err
}
