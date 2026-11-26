package opslistener

import (
	"context"
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controlplane"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// The settings of the ops listener. Every one is required and none has a default (AGENT-16, A330).
const (
	EnvServerCertFile = "NOEMA_OPS_TLS_CERT_FILE"
	EnvServerKeyFile  = "NOEMA_OPS_TLS_KEY_FILE"
	EnvClientCAFile   = "NOEMA_OPS_CLIENT_CA_FILE"
)

// Switcher is the merged kill switch path (controlplane.Switch).
type Switcher interface {
	Set(ctx context.Context, tenantID, correlationID string, c controlplane.Caller) error
	Clear(ctx context.Context, tenantID, correlationID string, c controlplane.Caller) error
}

// Config is the listener's whole configuration.
type Config struct {
	ServerCertFile string
	ServerKeyFile  string
	ClientCAFile   string
	ReadFile       func(path string) ([]byte, error)
	ReadSecret     func(path string) ([]byte, error)
	Clock          clock.Clock
	Switch         Switcher
	CorrelationIDs func() (string, error)
	HeaderTimeout  time.Duration
}

// Listener is the ops listener.
type Listener struct{}

// New is not built yet.
func New(Config) (*Listener, error) { return nil, errors.New("opslistener: not built") }

// TLSConfig is not built yet.
func (*Listener) TLSConfig() *tls.Config { return nil }

// Handler is not built yet.
func (*Listener) Handler() http.Handler { return http.NotFoundHandler() }

// Serve is not built yet.
func (*Listener) Serve(context.Context, net.Listener) error {
	return errors.New("opslistener: not built")
}
