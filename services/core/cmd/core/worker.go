package main

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.temporal.io/sdk/workflow"

	"github.com/Noemai-org/noema/services/core/workflows"
)

// worker.go: the core's Temporal worker (STM-04; card status/STM-04.md; REQ-EXE-006, REQ-RCV-002, REQ-EXE-005, REQ-OPS-003; INV-30, AGENT-16). This slice holds the settings and the
// registration; the dial and the poll loop are the next commits of the task. Temporal is never authoritative and the worker holds no credential for the core database, the adapters or
// the grant keys: the activities it runs get their ports from the composition root.

// The settings. None has a default (AGENT-16).
const (
	EnvTemporalAddress         = "NOEMA_TEMPORAL_ADDRESS"
	EnvTemporalNamespace       = "NOEMA_TEMPORAL_NAMESPACE"
	EnvTemporalTaskQueue       = "NOEMA_TEMPORAL_TASK_QUEUE"
	EnvTemporalActivityTimeout = "NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS"
	EnvTemporalTLSCert         = "NOEMA_TEMPORAL_TLS_CERT_FILE"
	EnvTemporalTLSKey          = "NOEMA_TEMPORAL_TLS_KEY_FILE"
	EnvTemporalTLSCA           = "NOEMA_TEMPORAL_TLS_CA_FILE"
)

// WorkerTLS names the CA file that verifies the Temporal server and, optionally, the client certificate and key (both or neither).
type WorkerTLS struct{ CertFile, KeyFile, CAFile string }

// WorkerConfig is what the worker is built from.
type WorkerConfig struct {
	Address         string // host:port
	Namespace       string
	TaskQueue       string
	ActivityTimeout time.Duration
	TLS             WorkerTLS // required: a configured worker uses TLS and verifies the server (Codex P2 on noema#676); the client pair is optional (A310, DEBT-46)
}

var temporalName = regexp.MustCompile(`\A[A-Za-z0-9][A-Za-z0-9._-]{0,127}\z`)

// LoadWorkerConfig reads the settings. With none of them it returns nil and no error (there is no worker); with some, or any wrong, it returns an error that names the setting and the
// process does not start. The CA file is required (a configured worker never runs without TLS and server verification); the client certificate and key are an optional pair (A310: the bench Temporal does not authenticate clients for now, DEBT-46).
func LoadWorkerConfig(e Env) (*WorkerConfig, error) {
	names := []string{EnvTemporalAddress, EnvTemporalNamespace, EnvTemporalTaskQueue, EnvTemporalActivityTimeout, EnvTemporalTLSCert, EnvTemporalTLSKey, EnvTemporalTLSCA}
	anySet := false
	for _, n := range names {
		if e.Getenv(n) != "" {
			anySet = true
		}
	}
	if !anySet {
		return nil, nil
	}
	cfg := &WorkerConfig{Address: e.Getenv(EnvTemporalAddress), Namespace: e.Getenv(EnvTemporalNamespace), TaskQueue: e.Getenv(EnvTemporalTaskQueue)}
	if err := checkAddress(cfg.Address); err != nil {
		return nil, fmt.Errorf("%s: %w", EnvTemporalAddress, err)
	}
	if !temporalName.MatchString(cfg.Namespace) {
		return nil, fmt.Errorf("%s: a name of letters, digits, dot, underscore or hyphen is required", EnvTemporalNamespace)
	}
	if !temporalName.MatchString(cfg.TaskQueue) {
		return nil, fmt.Errorf("%s: a name of letters, digits, dot, underscore or hyphen is required", EnvTemporalTaskQueue)
	}
	ms, err := strconv.ParseInt(e.Getenv(EnvTemporalActivityTimeout), 10, 64)
	if err != nil || ms <= 0 || ms > int64(1<<31) {
		return nil, fmt.Errorf("%s: a positive whole number of milliseconds is required", EnvTemporalActivityTimeout)
	}
	cfg.ActivityTimeout = time.Duration(ms) * time.Millisecond
	cert, key, ca := e.Getenv(EnvTemporalTLSCert), e.Getenv(EnvTemporalTLSKey), e.Getenv(EnvTemporalTLSCA)
	switch {
	case ca == "":
		return nil, fmt.Errorf("%s: is required (the CA that signs the Temporal server)", EnvTemporalTLSCA)
	case cert != "" && key == "":
		return nil, fmt.Errorf("%s: is required when %s is set (a client certificate needs its key)", EnvTemporalTLSKey, EnvTemporalTLSCert)
	case cert == "" && key != "":
		return nil, fmt.Errorf("%s: is required when %s is set (a client key needs its certificate)", EnvTemporalTLSCert, EnvTemporalTLSKey)
	}
	cfg.TLS = WorkerTLS{CertFile: cert, KeyFile: key, CAFile: ca}
	return cfg, nil
}

func checkAddress(a string) error {
	if a == "" {
		return errors.New("is required (host:port)")
	}
	host, port, err := net.SplitHostPort(a)
	if err != nil || host == "" || strings.ContainsAny(a, "/ \t") {
		return errors.New("must be host:port")
	}
	if p, perr := strconv.Atoi(port); perr != nil || p < 1 || p > 65535 {
		return errors.New("the port must be 1 to 65535")
	}
	return nil
}

// registry is the part of a Temporal worker the registration uses (worker.Registry has these methods), so a test can record what is registered.
type registry interface {
	RegisterWorkflowWithOptions(w any, options workflow.RegisterOptions)
	RegisterActivity(a any)
}

// registerWorker registers exactly the workflows and activities `workflows` declares: the EscalateCase workflow and the one activities value whose methods the Activities table gives a
// retry policy to. A write activity runs once and a read is bounded (D8); the options come from workflows/options.go, never from here.
func registerWorker(r registry, a *workflows.EscalateActivities) {
	r.RegisterWorkflowWithOptions(workflows.EscalateCase, workflow.RegisterOptions{Name: workflows.EscalateWorkflowName})
	r.RegisterActivity(a)
}
