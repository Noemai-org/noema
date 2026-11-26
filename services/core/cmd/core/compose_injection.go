package main

import (
	"context"
	"log/slog"
	"sync/atomic"

	"github.com/Noemai-org/noema/services/core/evidence/injection"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/platform/packload"
)

// compose_injection.go (stub: the red commit of STM-01 compose, injection screen inputs; A704).

const (
	EnvInjectionMaxPatterns      = ""
	EnvInjectionMaxPatternBytes  = ""
	EnvInjectionMaxDocumentBytes = ""
	EnvInjectionMaxDepth         = ""
	EnvInjectionFieldCap         = ""
)

// MetricInjectionRecordFailures is the counter's name.
const MetricInjectionRecordFailures = ""

type recordFailureCounter struct{ n atomic.Uint64 }

func (c *recordFailureCounter) Inc()          {}
func (c *recordFailureCounter) Value() uint64 { return 0 }
func (c *recordFailureCounter) Name() string  { return "" }

type injectionScreen struct {
	Deps     ScreenDeps
	Failures *recordFailureCounter
	Missing  []string
}

func injectionLimitsFrom(Env) (injection.Limits, []string, error) {
	return injection.Limits{}, nil, nil
}

func correlationOfContext(context.Context) string { return "" }

type injectionAlert struct{}

func (injectionAlert) ScreenFlagged(context.Context, payloadread.Flag) {}

func buildInjectionScreen(Env, *slog.Logger, injection.Recorder) (injectionScreen, error) {
	return injectionScreen{}, nil
}

func newInjectionScreen(packload.Pack, injection.Limits, *slog.Logger, injection.Recorder) (injectionScreen, error) {
	return injectionScreen{}, nil
}

func (s injectionScreen) annotate(w *trustWired) {}
