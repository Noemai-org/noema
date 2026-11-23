package main

import (
	"context"
	"log/slog"
	"time"
)

// httpServer is the part of *http.Server that serve uses.
type httpServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

// serve runs srv until ctx ends or a fatal error arrives. Not built yet.
func serve(context.Context, httpServer, <-chan error, time.Duration, *slog.Logger) int { return 0 }

// mergeFatal delivers the first error of any of the channels. Not built yet.
func mergeFatal(context.Context, ...<-chan error) <-chan error { return nil }
