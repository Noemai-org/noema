package main

// STM-04 (card status/STM-04.md; REQ-EXE-006, REQ-OPS-003; INV-30, AGENT-07): a Temporal worker that dies on its own must be seen by the process. The SDK reports it through
// worker.Options.OnFatalError; the connection hands it out on Fatal() and readiness turns false. No server is needed: the callback the SDK would call is the one the test calls.

import (
	"errors"
	"runtime"
	"sync"
	"testing"
)

var errSyntheticFatal = errors.New("SYNTHETIC fatal worker error")

func TestAFatalErrorFromTheSDKCallbackReachesFatal(t *testing.T) {
	f := newFatalState()
	if !f.Ready() {
		t.Fatal("a worker that has not died must be ready")
	}
	opts := workerOptions(f)
	if opts.OnFatalError == nil {
		t.Fatal("worker options must set OnFatalError")
	}
	opts.OnFatalError(errSyntheticFatal)
	select {
	case err := <-f.Fatal():
		if !errors.Is(err, errSyntheticFatal) {
			t.Errorf("got %v", err)
		}
	default:
		t.Fatal("the fatal error was not delivered")
	}
	if f.Ready() {
		t.Error("readiness must be false after a fatal error")
	}
}

func TestTheFatalErrorIsDeliveredOnceWhateverTheSDKDoes(t *testing.T) {
	f := newFatalState()
	before := runtime.NumGoroutine()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); f.report(errSyntheticFatal) }()
	}
	wg.Wait()
	if got := runtime.NumGoroutine(); got > before {
		t.Errorf("goroutines: %d before, %d after", before, got)
	}
	n := 0
	for {
		select {
		case <-f.Fatal():
			n++
			continue
		default:
		}
		break
	}
	if n != 1 {
		t.Errorf("delivered %d times, want 1", n)
	}
	if f.Ready() {
		t.Error("readiness must be false")
	}
}

func TestReportingNeverBlocksWhenNobodyReads(t *testing.T) {
	f := newFatalState()
	done := make(chan struct{})
	go func() { f.report(errSyntheticFatal); f.report(errSyntheticFatal); close(done) }()
	<-done
}

func TestANilFatalErrorIsNotAFatalError(t *testing.T) {
	f := newFatalState()
	f.report(nil)
	if !f.Ready() {
		t.Error("nil must not turn readiness false")
	}
	select {
	case <-f.Fatal():
		t.Error("nil must not be delivered")
	default:
	}
}

func TestTheSDKConnectionExposesFatalAndReady(t *testing.T) {
	var c interface {
		Fatal() <-chan error
		Ready() bool
	} = sdkConnection{f: newFatalState()}
	c.(sdkConnection).f.report(errSyntheticFatal)
	if c.Ready() {
		t.Error("ready after fatal")
	}
	select {
	case err := <-c.Fatal():
		if !errors.Is(err, errSyntheticFatal) {
			t.Errorf("got %v", err)
		}
	default:
		t.Error("the fatal error was not delivered")
	}
}
