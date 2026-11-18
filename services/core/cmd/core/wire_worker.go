package main

import (
	"context"
	"errors"
)

// workerPorts are the real ports of the workflows' activities.
type workerPorts struct {
	Executor interface{}
	Reader   interface{}
	Issuer   interface{}
}

type workerHandle struct{}

func (h *workerHandle) Stop()                {}
func (h *workerHandle) Readiness() readiness { return func() (bool, string) { return true, "" } }

func buildWorker(context.Context, Env, workerPorts, func(context.Context, *WorkerConfig) (Connection, error)) (*workerHandle, error) {
	return nil, errors.New("not built")
}
