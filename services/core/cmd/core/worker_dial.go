package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

// worker_dial.go: the real connection to Temporal (STM-04 slice 2). Always TLS with the server verified; the client certificate is sent only when configured (A310, DEBT-46: the bench
// Temporal does not authenticate clients for now). There is no plain connection to fall back to.

type sdkConnection struct{ c client.Client }

func (s sdkConnection) NewWorker(taskQueue string) temporalWorker {
	return worker.New(s.c, taskQueue, worker.Options{})
}

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
	return sdkConnection{c: c}, nil
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
