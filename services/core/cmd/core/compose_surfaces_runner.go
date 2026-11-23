package main

import (
	"log/slog"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// buildSurfaceRunner builds the write runner of the Input Gateway (STM-01 surfaces; REQ-STM-001, REQ-EXE-001; INV-03, INV-30; A204). Not built yet.
func buildSurfaceRunner(Components, orchestrator.WritePorts) (*orchestrator.WriteRunner, error) {
	return nil, nil
}

// surfaceRunFailed is the failure report of the investigation workers: the investigation id and the class of the error, never its text. Not built yet.
func surfaceRunFailed(*slog.Logger) func(string, error) { return func(string, error) {} }
