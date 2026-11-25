package main

import (
	"log/slog"

	"github.com/Noemai-org/noema/services/core/execution/controller"
	"github.com/Noemai-org/noema/services/core/execution/ledger"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// RED stub (LANE-5 slice 2): the build answers nothing yet.

const (
	EnvExecutionRollbackTimeout = "NOEMA_EXECUTION_ROLLBACK_TIMEOUT_MS"
	EnvExecutionExecuteTimeout  = "NOEMA_EXECUTION_EXECUTE_TIMEOUT_MS"
)

type executionIDs interface {
	ID(prefix string) (string, error)
	ReceiptID() (string, error)
}

type executionSeams struct {
	Clock       clock.Clock
	IDs         executionIDs
	Keys        controller.KeyVerifier
	Dispatcher  controller.Dispatcher
	Revalidator controller.Revalidator
	Intents     ledger.IntentSource
	Plans       ledger.PlanSource
	Verifier    orchestrator.Verifier
	Bindings    orchestrator.Bindings
	Log         *slog.Logger
}

type executionSide struct {
	Controllers map[string]*controller.Controller
	Executor    orchestrator.Executor
	Verifier    orchestrator.Verifier
	Bindings    orchestrator.Bindings
	Gaps        []string
}

func buildExecutionSide(env Env, parts *executionParts, s executionSeams) (*executionSide, error) {
	return &executionSide{}, nil
}

func (e *executionSide) WritePorts(journal orchestrator.Journal) (orchestrator.WritePorts, []string) {
	return orchestrator.WritePorts{}, nil
}
