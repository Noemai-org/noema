package config

import (
	"errors"
	"strings"
	"testing"
)

const keyA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
const keyB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func env(extra map[string]string) func(string) string {
	base := map[string]string{"HOSTILITY_PROXY_PROFILE": "bench", "HOSTILITY_PROXY_CONTROL_BIND_VIA": "mock-case-db", "HOSTILITY_PROXY_ROUTES_FILE": "/etc/hostility-proxy/config/routes.json",
		"HOSTILITY_PROXY_NUMBER_KEY_FILE": "/key", "HOSTILITY_PROXY_BENCH_TOKEN_FILE": "/tok"}
	for k, v := range extra {
		if v == "-" {
			delete(base, k)
		} else {
			base[k] = v
		}
	}
	return func(k string) string { return base[k] }
}

func files(m map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if v, ok := m[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file " + p)
	}
}

var good = map[string]string{"/key": keyA + "\n", "/tok": keyB + "\n"}

func TestLoad(t *testing.T) {
	c, err := Load(env(nil), files(good))
	if err != nil || c.Profile != "bench" || string(c.NumberKey) != keyA || c.BenchToken != keyB || c.DataListen != ":8080" {
		t.Fatalf("%v %+v", err, c)
	}
}

// PERD §65.1, REQ-ENV-005: the proxy is absent from every profile but bench and ci.
func TestProfileGuard(t *testing.T) {
	for _, p := range []string{"", "dev", "demo", "prod", "production", "BENCH"} {
		if _, err := Load(env(map[string]string{"HOSTILITY_PROXY_PROFILE": p}), files(good)); err == nil || !strings.Contains(err.Error(), "bench, ci and test") {
			t.Errorf("profile %q: %v", p, err)
		}
	}
	for _, p := range []string{"bench", "ci", "test"} {
		if _, err := Load(env(map[string]string{"HOSTILITY_PROXY_PROFILE": p}), files(good)); err != nil {
			t.Errorf("profile %q: %v", p, err)
		}
	}
}

func TestRefusals(t *testing.T) {
	cases := map[string]struct {
		env   map[string]string
		files map[string]string
		want  string
	}{
		"control listener unset": {map[string]string{"HOSTILITY_PROXY_CONTROL_BIND_VIA": "-"}, good, "exactly one"},
		"control listener twice": {map[string]string{"HOSTILITY_PROXY_CONTROL_LISTEN": "127.0.0.1:1"}, good, "exactly one"},
		"no routes":              {map[string]string{"HOSTILITY_PROXY_ROUTES_FILE": "-"}, good, "ROUTES_FILE"},
		"no key path":            {map[string]string{"HOSTILITY_PROXY_NUMBER_KEY_FILE": "-"}, good, "NUMBER_KEY_FILE is required"},
		"short key":              {nil, map[string]string{"/key": "short", "/tok": keyB}, "at least"},
		"short token":            {nil, map[string]string{"/key": keyA, "/tok": "short"}, "at least"},
		"missing file":           {nil, map[string]string{"/key": keyA}, "no such file"},
		"token is the key":       {nil, map[string]string{"/key": keyA, "/tok": keyA}, "must differ"},
	}
	for name, tc := range cases {
		if _, err := Load(env(tc.env), files(tc.files)); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// Codex finding 1 on noema#207: the route table name used to be interpolated into a host bind-mount source by Compose, so a hostile value could mount a host file
// (../../../../.aws/credentials) into the proxy. Compose now mounts one fixed directory and only the container path varies; the proxy refuses a route table path that
// is not a clean path inside the config directory, so a name cannot walk out of it.
func TestARoutesPathThatWalksOutOfTheConfigDirectoryIsRefused(t *testing.T) {
	for _, bad := range []string{
		"/etc/hostility-proxy/config/../../../root/.aws/credentials", "/etc/hostility-proxy/config/..", "../../x", "/etc/hostility-proxy/config/./routes.json",
		"/etc/hostility-proxy/config//routes.json", "/etc/passwd", "/etc/hostility-proxy/config/sub/routes.json", "/etc/hostility-proxy/config/routes.json/..", "/etc/hostility-proxy/config/.hidden", "/etc/hostility-proxy/config/a b.json",
	} {
		if _, err := Load(env(map[string]string{"HOSTILITY_PROXY_ROUTES_FILE": bad}), files(good)); err == nil || !strings.Contains(err.Error(), "ROUTES_FILE") {
			t.Errorf("%q: %v", bad, err)
		}
	}
	for _, ok := range []string{"/etc/hostility-proxy/config/routes.json", "/etc/hostility-proxy/config/routes.standin.json"} {
		if _, err := Load(env(map[string]string{"HOSTILITY_PROXY_ROUTES_FILE": ok}), files(good)); err != nil {
			t.Errorf("%q: %v", ok, err)
		}
	}
}

func TestUpstreams(t *testing.T) {
	c, _ := Load(env(nil), files(good))
	get := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	ups, err := c.Upstreams([]string{"tushe", "relay"}, get(map[string]string{"HOSTILITY_PROXY_UPSTREAM_TUSHE": "https://sandbox.example/api", "HOSTILITY_PROXY_UPSTREAM_RELAY": "http://relay.example:8443"}))
	if err != nil || ups["tushe"].Host != "sandbox.example" || ups["relay"].Scheme != "http" {
		t.Fatalf("%v %v", err, ups)
	}
	for name, raw := range map[string]string{"missing": "", "ftp": "ftp://x", "no host": "https://", "userinfo": "https://a:b@x", "query": "https://x?q=1", "fragment": "https://x#f", "not a url": "::"} {
		if _, err := c.Upstreams([]string{"tushe"}, get(map[string]string{"HOSTILITY_PROXY_UPSTREAM_TUSHE": raw})); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}
