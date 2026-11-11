package main

import (
	"crypto/ecdsa"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/Noemai-org/noema/services/core/edge"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// The session configuration (REQ-API-004, AGENT-16: nothing here has a default, and no key or number is in code). The public key and the
// limits are files; the rest are plain values. All six are given, or none: a core with some of them does not start.
const (
	envKeyFile    = "NOEMA_SESSION_PUBLIC_KEY_FILE" // the synthetic IdP's ES256 public key, PKIX PEM
	envKid        = "NOEMA_SESSION_KID"
	envAudience   = "NOEMA_SESSION_AUDIENCE"
	envTenants    = "NOEMA_SESSION_TENANTS"     // the tenants this deployment serves, comma separated
	envLimitsFile = "NOEMA_SESSION_LIMITS_FILE" // a session-limits.v1 document (ruling A21: the clock skew)
	envMaxBody    = "NOEMA_MAX_BODY_BYTES"      // the largest request body read, in bytes
)

var sessionEnv = []string{envKeyFile, envKid, envAudience, envTenants, envLimitsFile, envMaxBody}

// coreRoutes are the endpoints of the public API. None is registered yet: each arrives with the task that builds its handler, together with its
// role list, and until then every /v1 request that passes the session check is 404.
func coreRoutes() []edge.Route { return nil }

// loadEdge builds the protected /v1 handler, serving routes, from the session configuration. With none of it set it returns no handler and no error, and the
// core serves health only. With some of it, or with any value that is wrong, it returns an error and no handler. Errors name the variable and
// never repeat a file's content.
func loadEdge(getenv func(string) string, readFile func(string) ([]byte, error), clk clock.Clock, ids session.IDs, routes []edge.Route) (http.Handler, error) {
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
	keyBytes, rerr := readFile(getenv(envKeyFile))
	if rerr != nil {
		return nil, fmt.Errorf("%s: cannot read the file: %w", envKeyFile, rerr)
	}
	pub, err := session.ParsePublicKeyPEM(keyBytes)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envKeyFile, err)
	}
	limitsBytes, rerr := readFile(getenv(envLimitsFile))
	if rerr != nil {
		return nil, fmt.Errorf("%s: cannot read the file: %w", envLimitsFile, rerr)
	}
	limits, err := session.LoadLimits(strings.NewReader(string(limitsBytes)))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envLimitsFile, err)
	}
	maxBody, err := strconv.ParseInt(getenv(envMaxBody), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("%s: not a whole number of bytes", envMaxBody)
	}
	v, err := session.NewVerifier(session.Config{Keys: map[string]*ecdsa.PublicKey{getenv(envKid): pub}, Audience: getenv(envAudience),
		Tenants: strings.Split(getenv(envTenants), ","), MaxLifetime: session.CeilingLifetime, Skew: limits.ClockSkew, Clock: clk})
	if err != nil {
		return nil, fmt.Errorf("session configuration: %w", err)
	}
	h, err := edge.Protected(edge.Options{Verifier: v, IDs: ids, MaxBody: maxBody, Routes: routes})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", envMaxBody, err)
	}
	return h, nil
}
