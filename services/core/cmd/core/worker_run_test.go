package main

// STM-04 slice 2 (card status/STM-04.md; A296 item 6): starting and stopping the worker. The connection to Temporal is a seam (a Connection that makes a worker and closes), so the order of
// events is asserted without a server: dial, one worker on the configured task queue, the registration of exactly what `workflows` declares, start; and on stop the worker stops and the
// connection closes, once, however many times Stop is called. A dial that fails builds nothing; a worker that does not start closes the connection it opened and reports an error that
// carries no address or file path. REQ-EXE-006, INV-30, AGENT-16. Every value is SYNTHETIC.

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/workflows"
)

type fakeWorker struct {
	log      *[]string
	startErr error
}

func (w *fakeWorker) RegisterWorkflowWithOptions(_ any, o workflow.RegisterOptions) {
	*w.log = append(*w.log, "workflow:"+o.Name)
}
func (w *fakeWorker) RegisterActivity(any) { *w.log = append(*w.log, "activities") }
func (w *fakeWorker) Start() error {
	*w.log = append(*w.log, "start")
	return w.startErr
}
func (w *fakeWorker) Stop() { *w.log = append(*w.log, "stop") }

type fakeConn struct {
	log      *[]string
	startErr error
}

func (c *fakeConn) NewWorker(queue string) temporalWorker {
	*c.log = append(*c.log, "worker:"+queue)
	return &fakeWorker{log: c.log, startErr: c.startErr}
}
func (c *fakeConn) Close() { *c.log = append(*c.log, "close") }

func cfgFor() *WorkerConfig {
	return &WorkerConfig{Address: "secret-host:7233", Namespace: "noema", TaskQueue: "noema-core", ActivityTimeout: 30 * time.Second}
}

func deadlineCtx() context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	_ = cancel
	return ctx
}

func TestTheWorkerIsStartedInOrderAndStoppedOnce(t *testing.T) {
	var log []string
	dial := func(_ context.Context, c *WorkerConfig) (Connection, error) {
		log = append(log, "dial:"+c.Namespace)
		return &fakeConn{log: &log}, nil
	}
	rw, err := startWorker(deadlineCtx(), cfgFor(), dial, &workflows.EscalateActivities{})
	if err != nil || rw == nil {
		t.Fatalf("%v, %v", rw, err)
	}
	want := []string{"dial:noema", "worker:noema-core", "workflow:" + workflows.EscalateWorkflowName, "activities", "start"}
	if !reflect.DeepEqual(log, want) {
		t.Fatalf("events %v, want %v", log, want)
	}
	rw.Stop()
	rw.Stop()
	rw.Stop()
	if got := log[len(want):]; !reflect.DeepEqual(got, []string{"stop", "close"}) {
		t.Errorf("after three Stops: %v, want one stop and one close", got)
	}
}

func TestADialThatFailsBuildsNoWorker(t *testing.T) {
	var log []string
	dial := func(context.Context, *WorkerConfig) (Connection, error) {
		return nil, errors.New("dial tcp secret-host:7233: connection refused")
	}
	rw, err := startWorker(deadlineCtx(), cfgFor(), dial, &workflows.EscalateActivities{})
	if rw != nil || err == nil || len(log) != 0 {
		t.Fatalf("%v, %v, %v", rw, err, log)
	}
	if strings.Contains(err.Error(), "secret-host") || strings.Contains(err.Error(), "refused") {
		t.Errorf("the error carries the address or the cause's text: %v", err)
	}
	if !errors.Is(err, ErrWorkerNotStarted) {
		t.Errorf("%v is not ErrWorkerNotStarted", err)
	}
}

// A worker that cannot start closes the connection it opened (nothing leaks) and the error carries no address.
func TestAWorkerThatDoesNotStartClosesItsConnection(t *testing.T) {
	var log []string
	dial := func(context.Context, *WorkerConfig) (Connection, error) {
		return &fakeConn{log: &log, startErr: errors.New("poller failed for secret-host:7233")}, nil
	}
	rw, err := startWorker(deadlineCtx(), cfgFor(), dial, &workflows.EscalateActivities{})
	if rw != nil || err == nil || !errors.Is(err, ErrWorkerNotStarted) || strings.Contains(err.Error(), "secret-host") {
		t.Fatalf("%v, %v", rw, err)
	}
	if log[len(log)-1] != "close" || strings.Count(strings.Join(log, ","), "close") != 1 {
		t.Errorf("events %v: the connection was not closed exactly once", log)
	}
}

// No configuration, no activities, no dialer and no deadline are refused before anything is dialled.
func TestStartWorkerRefusesWhatItCannotRun(t *testing.T) {
	dialled := 0
	dial := func(context.Context, *WorkerConfig) (Connection, error) { dialled++; return nil, errors.New("x") }
	acts := &workflows.EscalateActivities{}
	for name, call := range map[string]func() (*RunningWorker, error){
		"no config":     func() (*RunningWorker, error) { return startWorker(deadlineCtx(), nil, dial, acts) },
		"no activities": func() (*RunningWorker, error) { return startWorker(deadlineCtx(), cfgFor(), dial, nil) },
		"no dialer":     func() (*RunningWorker, error) { return startWorker(deadlineCtx(), cfgFor(), nil, acts) },
		"no deadline":   func() (*RunningWorker, error) { return startWorker(context.Background(), cfgFor(), dial, acts) },
	} {
		if rw, err := call(); rw != nil || !errors.Is(err, ErrWorkerNotStarted) {
			t.Errorf("%s: %v, %v", name, rw, err)
		}
	}
	if dialled != 0 {
		t.Errorf("dialled %d times for refused calls", dialled)
	}
}
