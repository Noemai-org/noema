package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// compose_surfaces_serve.go: the process serves until a signal or a fatal error (STM-04 #794, STM-01 surfaces; REQ-OPS-003, REQ-EXE-006; INV-30, AGENT-07; A442, interim, relayed, not yet
// committed). A worker or a recovery pass that dies on its own stops the server and the exit code is non-zero, so that Compose restarts the process; there is no degraded mode.

// httpServer is the part of *http.Server that serve uses.
type httpServer interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

// serve runs srv until ctx ends or a fatal error arrives, shuts it down under shutdownTimeout and returns the exit code: 0 for a signal, 1 for a fatal error or a server that could not
// listen. A fatal error is logged by its class and never its text, which can hold an address.
func serve(ctx context.Context, srv httpServer, fatal <-chan error, shutdownTimeout time.Duration, logger *slog.Logger) int {
	code := make(chan int, 1)
	listenDone := make(chan struct{})
	go func() {
		defer close(listenDone)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("serve", "error", err.Error())
			code <- 1
		}
	}()
	exit := 0
	select {
	case <-ctx.Done():
	case err := <-fatal:
		logger.Error("fatal", "error_class", fmt.Sprintf("%T", err))
		exit = 1
	case exit = <-code:
	}
	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		logger.Error("shutdown", "error", err.Error())
	}
	<-listenDone
	select {
	case c := <-code:
		exit = max(exit, c)
	default:
	}
	return exit
}

// mergeFatal delivers the first error of any of the channels, ignoring nil ones. It starts one goroutine per channel, each of which ends when its channel fires or ctx ends.
func mergeFatal(ctx context.Context, chans ...<-chan error) <-chan error {
	out := make(chan error, len(chans))
	for _, c := range chans {
		if c == nil {
			continue
		}
		go func() {
			select {
			case err := <-c:
				out <- err
			case <-ctx.Done():
			}
		}()
	}
	return out
}

// fatalChans are the channels of the parts that can die on their own: the worker and the recovery pass.
func (p *Process) fatalChans() []<-chan error {
	var chans []<-chan error
	if p.Worker != nil {
		if c := p.Worker.Fatal(); c != nil {
			chans = append(chans, c)
		}
	}
	if p.Surfaces != nil && p.Surfaces.Recovery != nil {
		chans = append(chans, p.Surfaces.Recovery.Fatal())
	}
	return chans
}
