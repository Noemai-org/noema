package main

// STM-01 surfaces, the process exit (REQ-OPS-003, REQ-EXE-006; INV-30, AGENT-07; A442, interim, relayed, not yet committed): a worker or a recovery pass that dies on its own makes the process
// stop serving and exit non-zero, so Compose restarts it; a signal ends it with zero. The cause of a fatal error is never logged as text. Every value is SYNTHETIC.

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

type sfServer struct {
	mu        sync.Mutex
	stop      chan struct{}
	shutdowns int
	hadDL     bool
	listen    error
}

func newSfServer() *sfServer { return &sfServer{stop: make(chan struct{})} }

func (s *sfServer) ListenAndServe() error {
	if s.listen != nil {
		return s.listen
	}
	<-s.stop
	return http.ErrServerClosed
}

func (s *sfServer) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdowns++
	_, s.hadDL = ctx.Deadline()
	if s.shutdowns == 1 {
		close(s.stop)
	}
	return nil
}

func sfServe(t *testing.T, ctx context.Context, srv httpServer, fatal <-chan error) (int, string) {
	t.Helper()
	var buf bytes.Buffer
	code := make(chan int, 1)
	go func() { code <- serve(ctx, srv, fatal, time.Second, slog.New(slog.NewJSONHandler(&buf, nil))) }()
	select {
	case c := <-code:
		return c, buf.String()
	case <-time.After(10 * time.Second):
		t.Fatal("serve did not return")
		return 0, ""
	}
}

// A fatal error stops the server under a deadline and the exit code is 1; the log names the class and never the text (a worker's error can hold an address).
func TestAFatalErrorShutsTheServerDownAndExitsNonZero(t *testing.T) {
	srv, fatal := newSfServer(), make(chan error, 1)
	fatal <- errors.New("synthetic: dial temporal.synthetic:7233 refused")
	code, log := sfServe(t, t.Context(), srv, fatal)
	if code != 1 || srv.shutdowns != 1 || !srv.hadDL {
		t.Fatalf("code %d, shutdowns %d, deadline %v; want 1, 1, true", code, srv.shutdowns, srv.hadDL)
	}
	if !strings.Contains(log, "errors.errorString") || strings.Contains(log, "temporal.synthetic") {
		t.Errorf("log %q; want the class and no text", log)
	}
}

// A signal (the context ending) shuts the server down and the exit code is 0.
func TestASignalShutsTheServerDownAndExitsZero(t *testing.T) {
	srv := newSfServer()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	code, _ := sfServe(t, ctx, srv, nil)
	if code != 0 || srv.shutdowns != 1 {
		t.Fatalf("code %d, shutdowns %d; want 0 and 1", code, srv.shutdowns)
	}
}

// A server that cannot listen is a failure, and the exit code is 1.
func TestAServerThatCannotListenExitsNonZero(t *testing.T) {
	srv := newSfServer()
	srv.listen = errors.New("synthetic: address in use")
	if code, _ := sfServe(t, t.Context(), srv, nil); code != 1 {
		t.Fatalf("code %d; want 1", code)
	}
}

// mergeFatal delivers the first error of any channel, ignores a nil one, and delivers nothing while none has fired.
func TestMergeFatalDeliversTheFirstErrorOfAnyChannel(t *testing.T) {
	a, b := make(chan error, 1), make(chan error, 1)
	out := mergeFatal(t.Context(), nil, a, b)
	select {
	case e := <-out:
		t.Fatalf("delivered %v before any fired", e)
	case <-time.After(20 * time.Millisecond):
	}
	want := errors.New("synthetic")
	b <- want
	select {
	case e := <-out:
		if !errors.Is(e, want) {
			t.Fatalf("delivered %v; want %v", e, want)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("nothing delivered")
	}
}
