package main

import (
	"context"
	"log/slog"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
	"github.com/Noemai-org/noema/services/core/orchestrator/recovery"
)

// recoverySessions is the recovery's SessionReader over the orchestrator's per-tenant stores. Not built yet.
type recoverySessions struct{ rec *orchestratorRecorder }

var _ recovery.SessionReader = recoverySessions{}

func (recoverySessions) SessionOf(context.Context, orchestrator.Session, string) (investigations.RecordedSession, error) {
	return investigations.RecordedSession{}, nil
}

// recoveryWritePorts is the write ports of the recovery runner. Not built yet.
func recoveryWritePorts(wp orchestrator.WritePorts) orchestrator.WritePorts { return wp }

// recoverySurface is the recovery activity and its single startup pass. Not built yet.
type recoverySurface struct{}

// buildRecoverySurface builds recovery. Not built yet.
func buildRecoverySurface(Env, Components, SurfaceSeams, *orchestratorRecorder, orchestrator.WritePorts, []string, *slog.Logger) (*recoverySurface, []string, error) {
	return nil, nil, nil
}

// Start runs the startup pass in the background.
func (*recoverySurface) Start(context.Context) {}

// Done is closed when the startup pass has ended.
func (*recoverySurface) Done() <-chan struct{} {
	c := make(chan struct{})
	close(c)
	return c
}

// Ready is true once the startup pass has completed.
func (*recoverySurface) Ready() (bool, string) { return false, "" }

// Fatal delivers the error of a startup pass that could not read its list.
func (*recoverySurface) Fatal() <-chan error { return nil }
