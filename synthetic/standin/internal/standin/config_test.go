package standin_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/synthetic/standin/internal/standin"
)

const (
	good1 = "0123456789abcdef0123456789abcdef-bench"
	good2 = "fedcba9876543210fedcba9876543210-runtime"
	good3 = "0f1e2d3c4b5a69788796a5b4c3d2e1f0-principal-key"
)

func env(over map[string]string) (func(string) string, func(string) ([]byte, error)) {
	vars := map[string]string{"STANDIN_DATA_LISTEN": "10.20.0.5:8093", "STANDIN_CONTROL_LISTEN": "10.30.0.5:8094",
		"STANDIN_BENCH_TOKEN_FILE": "/run/secrets/bench", "STANDIN_RUNTIME_TOKEN_FILE": "/run/secrets/runtime", "STANDIN_PRINCIPAL_KEY_FILE": "/run/secrets/principal"}
	for k, v := range over {
		vars[k] = v
	}
	files := map[string]string{"/run/secrets/bench": good1 + "\n", "/run/secrets/runtime": good2 + "\n", "/run/secrets/principal": good3 + "\n"}
	return func(k string) string { return vars[k] }, func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file")
	}
}

func TestConfigReadsTheTwoCredentialsFromFilesOnly(t *testing.T) { // AGENTS 10: file-mounted secrets, never an environment value
	g, r := env(nil)
	c, err := standin.LoadConfig(g, r)
	if err != nil || c.Options.BenchToken != good1 || c.Options.RuntimeToken != good2 || c.Options.PrincipalKey != good3 || c.DataListen != "10.20.0.5:8093" {
		t.Fatalf("config wrong: %+v %v", c, err)
	}
	for _, name := range []string{"STANDIN_BENCH_TOKEN_FILE", "STANDIN_RUNTIME_TOKEN_FILE", "STANDIN_PRINCIPAL_KEY_FILE"} {
		g, r = env(map[string]string{name: ""})
		if _, err := standin.LoadConfig(g, r); err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("a missing %s must be named: %v", name, err)
		}
	}
}

func TestConfigRefusesAnEmptyOrEveryInterfaceControlListenAddress(t *testing.T) { // the bench side must never default to every interface
	for _, bad := range []string{":8094", "0.0.0.0:8094", "[::]:8094", "8094"} {
		g, r := env(map[string]string{"STANDIN_CONTROL_LISTEN": bad})
		if _, err := standin.LoadConfig(g, r); err == nil {
			t.Fatalf("STANDIN_CONTROL_LISTEN=%q must be refused", bad)
		}
	}
}

func TestConfigNeedsExactlyOneWayToPlaceTheControlListener(t *testing.T) {
	g, r := env(map[string]string{"STANDIN_CONTROL_LISTEN": ""})
	if _, err := standin.LoadConfig(g, r); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("no control placement must be refused and say why: %v", err)
	}
	g, r = env(map[string]string{"STANDIN_CONTROL_BIND_VIA": "some-bench-host", "STANDIN_CONTROL_PORT": "8094"})
	if _, err := standin.LoadConfig(g, r); err == nil || !strings.Contains(err.Error(), "exactly one") {
		t.Fatalf("both placements must be refused: %v", err)
	}
	g, r = env(map[string]string{"STANDIN_CONTROL_LISTEN": "", "STANDIN_CONTROL_BIND_VIA": "some-bench-host", "STANDIN_CONTROL_PORT": "8094"})
	c, err := standin.LoadConfig(g, r)
	if err != nil || c.ControlBindVia != "some-bench-host" || c.ControlPort != "8094" || c.ControlListen != "" {
		t.Fatalf("bind-via placement wrong: %+v %v", c, err)
	}
	for _, bad := range []string{"", "0", "70000", "abc"} {
		g, r = env(map[string]string{"STANDIN_CONTROL_LISTEN": "", "STANDIN_CONTROL_BIND_VIA": "some-bench-host", "STANDIN_CONTROL_PORT": bad})
		if _, err := standin.LoadConfig(g, r); err == nil {
			t.Fatalf("STANDIN_CONTROL_PORT=%q must be refused", bad)
		}
	}
}

func TestTheDataListenerMayBeAPortAloneButNeverEmptyOrMalformed(t *testing.T) { // like mock-case's case API: the networks the container joins decide who reaches it
	g, r := env(map[string]string{"STANDIN_DATA_LISTEN": ":8093"})
	if c, err := standin.LoadConfig(g, r); err != nil || c.DataListen != ":8093" {
		t.Fatalf(":8093 must be accepted for the data side: %+v %v", c, err)
	}
	for _, bad := range []string{"", "8093", ":0", ":x"} {
		g, r = env(map[string]string{"STANDIN_DATA_LISTEN": bad})
		if _, err := standin.LoadConfig(g, r); err == nil {
			t.Fatalf("STANDIN_DATA_LISTEN=%q must be refused", bad)
		}
	}
}

func TestConfigRefusesTheSameAddressForBothSides(t *testing.T) {
	g, r := env(map[string]string{"STANDIN_CONTROL_LISTEN": "10.20.0.5:8093"})
	if _, err := standin.LoadConfig(g, r); err == nil {
		t.Fatal("one listener for both credentials must be refused")
	}
}
