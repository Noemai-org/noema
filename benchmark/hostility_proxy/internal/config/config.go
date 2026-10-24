// Package config reads the hostility proxy's settings from the environment. Secrets are read from files named by
// *_FILE variables, never from a value in the environment (AGENTS.md §10).
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
)

// Profiles the proxy may run in (PERD §65.1: it is absent from every other profile).
var allowedProfiles = map[string]bool{"bench": true, "ci": true, "test": true}

// MinSecretBytes is the shortest key or token accepted.
const MinSecretBytes = 32

// Config is the validated configuration.
type Config struct {
	Profile           string
	DataListen        string
	ControlListen     string // bound literally, or ...
	ControlBindVia    string // ... bound to the local address in the subnet of this host (bench_net only)
	ControlPort       string
	RoutesFile        string
	FixturesDir       string
	NumberKey         []byte
	BenchToken        string
	upstreamEnvPrefix string
}

// Load builds a Config. getenv and readFile are injected so tests need no real environment.
func Load(getenv func(string) string, readFile func(string) ([]byte, error)) (*Config, error) {
	def := func(name, fallback string) string {
		if v := getenv(name); v != "" {
			return v
		}
		return fallback
	}
	c := &Config{
		Profile:           getenv("HOSTILITY_PROXY_PROFILE"),
		DataListen:        def("HOSTILITY_PROXY_DATA_LISTEN", ":8080"),
		ControlListen:     getenv("HOSTILITY_PROXY_CONTROL_LISTEN"),
		ControlBindVia:    getenv("HOSTILITY_PROXY_CONTROL_BIND_VIA"),
		ControlPort:       def("HOSTILITY_PROXY_CONTROL_PORT", "8081"),
		RoutesFile:        getenv("HOSTILITY_PROXY_ROUTES_FILE"),
		FixturesDir:       getenv("HOSTILITY_PROXY_FIXTURES_DIR"),
		upstreamEnvPrefix: "HOSTILITY_PROXY_UPSTREAM_",
	}
	if !allowedProfiles[c.Profile] {
		return nil, fmt.Errorf("HOSTILITY_PROXY_PROFILE is %q: the hostility proxy runs only in the bench, ci and test profiles", c.Profile)
	}
	if (c.ControlListen == "") == (c.ControlBindVia == "") {
		return nil, errors.New("set exactly one of HOSTILITY_PROXY_CONTROL_LISTEN and HOSTILITY_PROXY_CONTROL_BIND_VIA: the control listener must never default to every interface")
	}
	if c.RoutesFile == "" {
		return nil, errors.New("HOSTILITY_PROXY_ROUTES_FILE is required")
	}
	key, err := readSecret(getenv, readFile, "HOSTILITY_PROXY_NUMBER_KEY_FILE")
	if err != nil {
		return nil, err
	}
	c.NumberKey = []byte(key)
	if c.BenchToken, err = readSecret(getenv, readFile, "HOSTILITY_PROXY_BENCH_TOKEN_FILE"); err != nil {
		return nil, err
	}
	if c.BenchToken == key {
		return nil, errors.New("the bench token and the number key must differ")
	}
	return c, nil
}

func readSecret(getenv func(string) string, readFile func(string) ([]byte, error), env string) (string, error) {
	path := getenv(env)
	if path == "" {
		return "", fmt.Errorf("%s is required (a path to a file; the value is never read from the environment)", env)
	}
	b, err := readFile(path)
	if err != nil {
		return "", fmt.Errorf("%s: %w", env, err)
	}
	v := strings.TrimSpace(string(b))
	if len(v) < MinSecretBytes {
		return "", fmt.Errorf("%s: the secret must be at least %d bytes", env, MinSecretBytes)
	}
	return v, nil
}

// Upstreams resolves HOSTILITY_PROXY_UPSTREAM_<NAME> for each name the route table uses. Only these upstreams
// can ever be reached, and each must be an http or https URL with a host and nothing else.
func (c *Config) Upstreams(names []string, getenv func(string) string) (map[string]*url.URL, error) {
	out := map[string]*url.URL{}
	for _, n := range names {
		env := c.upstreamEnvPrefix + strings.ToUpper(n)
		raw := getenv(env)
		if raw == "" {
			return nil, fmt.Errorf("%s is required: the route table uses upstream %q", env, n)
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("%s must be an http(s) URL with a host and no credentials, query or fragment", env)
		}
		out[n] = u
	}
	return out, nil
}

// OSEnv and OSReadFile bind Load to the process.
func OSEnv(name string) string { return os.Getenv(name) }

// OSReadFile reads a file from disk.
func OSReadFile(path string) ([]byte, error) { return os.ReadFile(path) }
