package edge

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// The session configuration of the core process, read through Load by the composition root (REQ-API-004; AGENT-16: nothing here has a default,
// and no key or number is in code). The public key and the limits are files; the rest are plain values. All six are given, or none: a core with some of them does not start.
const (
	EnvKeyFile    = "NOEMA_SESSION_PUBLIC_KEY_FILE" // the synthetic IdP's ES256 public key, PKIX PEM
	EnvKid        = "NOEMA_SESSION_KID"
	EnvAudience   = "NOEMA_SESSION_AUDIENCE"
	EnvTenants    = "NOEMA_SESSION_TENANTS"     // the tenants this deployment serves, comma separated
	EnvLimitsFile = "NOEMA_SESSION_LIMITS_FILE" // a session-limits.v1 document (ruling A21: the clock skew)
	EnvMaxBody    = "NOEMA_MAX_BODY_BYTES"      // the largest request body read, in bytes
)

var sessionEnv = []string{EnvKeyFile, EnvKid, EnvAudience, EnvTenants, EnvLimitsFile, EnvMaxBody}

// Load builds the protected /v1 handler, serving routes, from the session configuration. With none of it set it returns no handler and no error, and the
// core serves health only. With some of it, or with any value that is wrong, it returns an error and no handler. Errors name the variable and
// never repeat a file's content.
func Load(getenv func(string) string, readFile func(string) ([]byte, error), clk clock.Clock, ids session.IDs, routes []Route) (http.Handler, error) {
	var missing []string
	for _, name := range sessionEnv {
		if getenv(name) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) == len(sessionEnv) {
		return nil, nil
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("session configuration is incomplete, missing: %s", strings.Join(missing, ", "))
	}
	keyBytes, rerr := readFile(getenv(EnvKeyFile))
	if rerr != nil {
		return nil, fmt.Errorf("%s: cannot read the file: %w", EnvKeyFile, rerr)
	}
	pub, err := session.ParsePublicKeyPEM(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvKeyFile, err)
	}
	limitsBytes, rerr := readFile(getenv(EnvLimitsFile))
	if rerr != nil {
		return nil, fmt.Errorf("%s: cannot read the file: %w", EnvLimitsFile, rerr)
	}
	limits, err := session.LoadLimits(strings.NewReader(string(limitsBytes)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvLimitsFile, err)
	}
	maxBody, err := strconv.ParseInt(getenv(EnvMaxBody), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: not a whole number of bytes", EnvMaxBody)
	}
	tenants := strings.Split(getenv(EnvTenants), ",")
	for _, t := range tenants {
		if t != strings.TrimSpace(t) {
			return nil, fmt.Errorf("%s: a tenant has surrounding whitespace", EnvTenants)
		}
	}
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{getenv(EnvKid): pub}, Audience: getenv(EnvAudience),
		Tenants: tenants, MaxLifetime: session.CeilingLifetime, Skew: limits.ClockSkew, Clock: clk})
	if err != nil {
		return nil, fmt.Errorf("session configuration: %w", err)
	}
	h, err := Protected(Options{Verifier: v, IDs: ids, MaxBody: maxBody, Routes: routes})
	if errors.Is(err, session.ErrBodyLimit) {
		return nil, fmt.Errorf("%s: %w", EnvMaxBody, err)
	}
	if err != nil {
		return nil, err
	}
	return h, nil
}
