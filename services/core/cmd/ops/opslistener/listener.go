package opslistener

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controlplane"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// The settings of the ops listener. Every one is required and none has a default (AGENT-16, A330). The composition that builds the listener reads them and passes the file
// names here.
const (
	// EnvServerCertFile and EnvServerKeyFile are the listener's own certificate and private key (PEM, file-mounted).
	EnvServerCertFile = "NOEMA_OPS_TLS_CERT_FILE"
	EnvServerKeyFile  = "NOEMA_OPS_TLS_KEY_FILE"
	// EnvClientCAFile is the operator CA (PEM, file-mounted): the only root a client certificate is verified against. The system's roots are never used.
	EnvClientCAFile = "NOEMA_OPS_CLIENT_CA_FILE"
)

// Switcher is the merged kill switch path. *controlplane.Switch implements it.
type Switcher interface {
	Set(ctx context.Context, tenantID, correlationID string, c controlplane.Caller) error
	Clear(ctx context.Context, tenantID, correlationID string, c controlplane.Caller) error
}

// Config is the listener's whole configuration. Every field is required.
type Config struct {
	ServerCertFile string
	ServerKeyFile  string
	ClientCAFile   string
	// ReadFile reads a public file (the certificate and the CA). ReadSecret reads the private key and is the checked secret reader (SEC-09, REQ-SEC-009).
	ReadFile   func(path string) ([]byte, error)
	ReadSecret func(path string) ([]byte, error)
	// Clock is the only source of time, for certificate validity (INV-21).
	Clock clock.Clock
	// Switch is the kill switch path.
	Switch Switcher
	// CorrelationIDs gives the correlation ID of one request (a corr_ ID). Without one, nothing is called: there would be no Record trail.
	CorrelationIDs func() (string, error)
	// HeaderTimeout bounds reading a request's headers and the graceful stop of Serve.
	HeaderTimeout time.Duration
	// ReadTimeout bounds reading a whole request, body included. IdleTimeout bounds a kept-alive connection between requests. MaxHeaderBytes bounds the request headers.
	ReadTimeout    time.Duration
	IdleTimeout    time.Duration
	MaxHeaderBytes int
}

var (
	// principalRe is the grammar of the Record actor's name part (contracts record-event v3): a certificate whose name does not match is not a usable identity.
	principalRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}\z`)
	// tenantRe is the tenant ID of common.v1.
	tenantRe = regexp.MustCompile(`^ten_[0-9A-Za-z]{2,32}\z`)
)

// Listener is the ops listener.
type Listener struct {
	cfg   Config
	tls   *tls.Config
	roots *x509.CertPool
}

// New reads the certificates and builds the listener. Its errors name the setting or the field and never a path's content or a key.
func New(cfg Config) (*Listener, error) {
	for _, s := range []struct {
		name string
		ok   bool
	}{
		{EnvServerCertFile, cfg.ServerCertFile != ""}, {EnvServerKeyFile, cfg.ServerKeyFile != ""}, {EnvClientCAFile, cfg.ClientCAFile != ""},
		{"ReadFile", cfg.ReadFile != nil}, {"ReadSecret", cfg.ReadSecret != nil}, {"Clock", cfg.Clock != nil}, {"Switch", cfg.Switch != nil},
		{"CorrelationIDs", cfg.CorrelationIDs != nil}, {"HeaderTimeout", cfg.HeaderTimeout > 0},
	} {
		if !s.ok {
			return nil, fmt.Errorf("opslistener: %s is required and has no default", s.name)
		}
	}
	certPEM, err := cfg.ReadFile(cfg.ServerCertFile)
	if err != nil {
		return nil, fmt.Errorf("opslistener: %s cannot be read", EnvServerCertFile)
	}
	keyPEM, err := cfg.ReadSecret(cfg.ServerKeyFile)
	if err != nil {
		return nil, fmt.Errorf("opslistener: %s cannot be read as a secret", EnvServerKeyFile)
	}
	caPEM, err := cfg.ReadFile(cfg.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("opslistener: %s cannot be read", EnvClientCAFile)
	}
	pair, err := tls.X509KeyPair(certPEM, keyPEM) // its error can name a PEM block type, never the key: it is dropped all the same
	if err != nil {
		return nil, fmt.Errorf("opslistener: %s and %s are not a certificate and its private key", EnvServerCertFile, EnvServerKeyFile)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("opslistener: %s holds no PEM certificate", EnvClientCAFile)
	}
	return &Listener{cfg: cfg, roots: roots, tls: &tls.Config{
		MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Time: cfg.Clock.Now,
	}}, nil
}

// TLSConfig is the listener's TLS configuration: client certificates required and verified against the operator CA only.
func (l *Listener) TLSConfig() *tls.Config { return l.tls.Clone() }

// Handler serves the two routes. It checks the client certificate itself, whatever the TLS layer did.
func (l *Listener) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/ops/tenants/{tenant}/kill-switch:set", l.serve(true))
	mux.HandleFunc("POST /v1/ops/tenants/{tenant}/kill-switch:clear", l.serve(false))
	return mux
}

// Serve answers on ln over mutual TLS until ctx ends, then stops gracefully and returns nil.
func (l *Listener) Serve(ctx context.Context, ln net.Listener) error {
	srv := &http.Server{Handler: l.Handler(), TLSConfig: l.TLSConfig(), ReadHeaderTimeout: l.cfg.HeaderTimeout}
	done := make(chan struct{})
	go func() {
		defer close(done)
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), l.cfg.HeaderTimeout)
		defer cancel()
		if err := srv.Shutdown(sctx); err != nil {
			_ = srv.Close() // the grace period ended: stop hard; the caller learns of it from Serve's return only if serving failed
		}
	}()
	err := srv.ServeTLS(ln, "", "")
	if errors.Is(err, http.ErrServerClosed) {
		<-done
		return nil
	}
	return err
}

// callerOf maps the verified client certificate of a request to the admin Caller (INV-12). A request that did not come over TLS, carries no certificate, was not verified by the
// handshake, or whose certificate does not verify again here against the operator CA, now, for client authentication, has no identity.
func (l *Listener) callerOf(r *http.Request) (controlplane.Caller, bool) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 || len(r.TLS.VerifiedChains) == 0 { // the handshake must have verified a chain, and it is verified again here
		return controlplane.Caller{}, false
	}
	leaf := r.TLS.PeerCertificates[0]
	inter := x509.NewCertPool()
	for _, c := range r.TLS.PeerCertificates[1:] {
		inter.AddCert(c)
	}
	if _, err := leaf.Verify(x509.VerifyOptions{Roots: l.roots, Intermediates: inter, CurrentTime: l.cfg.Clock.Now(), KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return controlplane.Caller{}, false
	}
	if !principalRe.MatchString(leaf.Subject.CommonName) {
		return controlplane.Caller{}, false
	}
	return controlplane.Caller{Role: controlplane.RoleAdmin, PrincipalID: leaf.Subject.CommonName}, true
}

// serve returns the handler of one action. The answers are fixed text and name no tenant, person or cause (AGENT-13).
func (l *Listener) serve(on bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		caller, ok := l.callerOf(r)
		if !ok {
			reply(w, http.StatusUnauthorized, `{"error":"not_authenticated"}`)
			return
		}
		tenantID := r.PathValue("tenant")
		if !tenantRe.MatchString(tenantID) || hasBody(r) {
			reply(w, http.StatusBadRequest, `{"error":"bad_request"}`)
			return
		}
		corr, err := l.cfg.CorrelationIDs()
		if err != nil || corr == "" {
			reply(w, http.StatusInternalServerError, `{"error":"internal"}`)
			return
		}
		if on {
			err = l.cfg.Switch.Set(r.Context(), tenantID, corr, caller)
		} else {
			err = l.cfg.Switch.Clear(r.Context(), tenantID, corr, caller)
		}
		switch {
		case err == nil && on:
			reply(w, http.StatusOK, `{"kill_switch":true}`)
		case err == nil:
			reply(w, http.StatusOK, `{"kill_switch":false}`)
		case errors.Is(err, controlplane.ErrForbidden):
			reply(w, http.StatusForbidden, `{"error":"forbidden"}`)
		case errors.Is(err, controlplane.ErrCaller):
			reply(w, http.StatusUnauthorized, `{"error":"not_authenticated"}`)
		default:
			reply(w, http.StatusInternalServerError, `{"error":"internal"}`)
		}
	}
}

// hasBody is true when the request carries any body: neither route takes one, so a body that names a person or a role is refused, never read for meaning.
func hasBody(r *http.Request) bool {
	if r.ContentLength > 0 {
		return true
	}
	if r.Body == nil || r.Body == http.NoBody || r.ContentLength == 0 {
		return false
	}
	var one [1]byte
	n, _ := r.Body.Read(one[:])
	return n > 0
}

func reply(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_, _ = w.Write([]byte(body))
}
