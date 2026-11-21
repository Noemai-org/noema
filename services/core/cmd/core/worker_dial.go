package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"
	"sync"
	"sync/atomic"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// worker_dial.go: the real connection to Temporal (STM-04 slice 2). Always TLS with the server verified; the client certificate is sent only when configured (A310, DEBT-46: the bench
// Temporal does not authenticate clients for now). There is no plain connection to fall back to.

type sdkConnection struct {
	c client.Client
	f *fatalState
}

func (s sdkConnection) NewWorker(taskQueue string) temporalWorker {
	return worker.New(s.c, taskQueue, workerOptions(s.f))
}

// Fatal delivers, once, the error that stopped the worker on its own.
func (s sdkConnection) Fatal() <-chan error { return s.f.Fatal() }

// Ready is false once the worker has died on its own.
func (s sdkConnection) Ready() bool { return s.f.Ready() }

// fatalState records the one fatal error of a worker (REQ-OPS-003, AGENT-07). The SDK calls OnFatalError from its own goroutines, so report never blocks and starts no goroutine: the
// first error goes into a channel of one, later ones are dropped, and readiness turns false. Nothing in the error is logged or rewritten here.
type fatalState struct {
	once sync.Once
	ch   chan error
	dead atomic.Bool
}

func newFatalState() *fatalState { return &fatalState{ch: make(chan error, 1)} }

func (f *fatalState) report(err error) {
	if err == nil {
		return
	}
	f.once.Do(func() {
		f.dead.Store(true)
		f.ch <- err
	})
}

// Fatal delivers the first fatal error once. It is never closed.
func (f *fatalState) Fatal() <-chan error { return f.ch }

// Ready is false once a fatal error was reported.
func (f *fatalState) Ready() bool { return !f.dead.Load() }

// workerOptions wires the SDK's fatal callback into f. Nothing else about the worker is set here (no option is a safety setting, AGENT-16).
func workerOptions(f *fatalState) worker.Options { return worker.Options{OnFatalError: f.report} }

func (s sdkConnection) Close() { s.c.Close() }

// dialTemporal connects as the configured namespace. The ConnectionOptions carry the TLS configuration when there is one.
func dialTemporal(_ context.Context, cfg *WorkerConfig) (Connection, error) {
	tc, err := temporalTLS(cfg)
	if err != nil {
		return nil, err
	}
	c, err := client.Dial(client.Options{HostPort: cfg.Address, Namespace: cfg.Namespace, ConnectionOptions: client.ConnectionOptions{TLS: tc}})
	if err != nil {
		return nil, err
	}
	return sdkConnection{c: c, f: newFatalState()}, nil
}

// temporalTLS builds the client's TLS configuration from the CA that signs the server and, when configured, the client certificate and key. TLS 1.3 at least, the server name is the host
// of the address, and a CA file with no certificate in it is refused. Errors name the setting and never a path or file content.
func temporalTLS(cfg *WorkerConfig) (*tls.Config, error) {
	var certs []tls.Certificate
	if cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("%s and %s: the client certificate and key cannot be loaded", EnvTemporalTLSCert, EnvTemporalTLSKey)
		}
		certs = []tls.Certificate{cert}
	}
	pem, err := os.ReadFile(cfg.TLS.CAFile)
	if err != nil {
		return nil, fmt.Errorf("%s: the CA file cannot be read", EnvTemporalTLSCA)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("%s: the file holds no certificate", EnvTemporalTLSCA)
	}
	host, _, err := net.SplitHostPort(cfg.Address)
	if err != nil || host == "" {
		return nil, errors.New(EnvTemporalAddress + ": must be host:port")
	}
	return &tls.Config{Certificates: certs, RootCAs: pool, ServerName: host, MinVersion: tls.VersionTLS13}, nil
}
