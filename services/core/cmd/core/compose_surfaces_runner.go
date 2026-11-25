package main

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// buildSurfaceRunner builds the write runner of the Input Gateway (STM-01 surfaces; REQ-STM-001, REQ-EXE-001; INV-03, INV-30; A204): the real transition table, the ports of the
// composition and the write ports, with the adopted recovery budget of PERD v1.23 (PERDMaxDispatchesPerKey; no number of this file's own, AGENT-16). A port that is missing is a gap,
// not an error: the runner is nil and nothing is half built. Only a table that cannot load is an error.
func buildSurfaceRunner(c Components, wp orchestrator.WritePorts) (*orchestrator.WriteRunner, error) {
	tab, err := orchestrator.LoadTable()
	if err != nil {
		return nil, fmt.Errorf("cmd/core: the transition table: %w", err)
	}
	r, err := orchestrator.NewWriteRunner(&orchestrator.Machine{Table: tab}, c.Ports, wp, orchestrator.WriteConfig{MaxDispatchesPerKey: orchestrator.PERDMaxDispatchesPerKey})
	if errors.Is(err, orchestrator.ErrNotWired) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("cmd/core: the write runner: %w", err)
	}
	return r, nil
}

// surfaceRunFailed is the failure report of the investigation workers: the investigation id and the class of the error, never its text, which can hold request data (AGENT-13).
func surfaceRunFailed(l *slog.Logger) func(string, error) {
	return func(investigationID string, err error) {
		if l == nil {
			return
		}
		l.Error("investigation run failed", "investigation_id", investigationID, "error_class", fmt.Sprintf("%T", err))
	}
}
