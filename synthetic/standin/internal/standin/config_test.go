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
)

func env(over map[string]string) (func(string) string, func(string) ([]byte, error)) {
	vars := map[string]string{"STANDIN_DATA_LISTEN": "10.20.0.5:8093", "STANDIN_CONTROL_LISTEN": "10.30.0.5:8094",
		"STANDIN_BENCH_TOKEN_FILE": "/run/secrets/bench", "STANDIN_RUNTIME_TOKEN_FILE": "/run/secrets/runtime"}
	for k, v := range over {
		vars[k] = v
	}
	files := map[string]string{"/run/secrets/bench": good1 + "\n", "/run/secrets/runtime": good2 + "\n"}
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
	if err != nil || c.Options.BenchToken != good1 || c.Options.RuntimeToken != good2 || c.DataListen != "10.20.0.5:8093" {
		t.Fatalf("config wrong: %+v %v", c, err)
	}
	g, r = env(map[string]string{"STANDIN_BENCH_TOKEN_FILE": ""})
	if _, err := standin.LoadConfig(g, r); err == nil || !strings.Contains(err.Error(), "STANDIN_BENCH_TOKEN_FILE") {
		t.Fatalf("a missing credential file must be named: %v", err)
	}
}

func TestConfigRefusesAnEmptyOrEveryInterfaceListenAddress(t *testing.T) { // the bench side must never default to every interface
	for _, k := range []string{"STANDIN_DATA_LISTEN", "STANDIN_CONTROL_LISTEN"} {
		for _, bad := range []string{"", ":8093", "0.0.0.0:8093", "[::]:8093", "8093"} {
			g, r := env(map[string]string{k: bad})
			if _, err := standin.LoadConfig(g, r); err == nil {
				t.Fatalf("%s=%q must be refused", k, bad)
			}
		}
	}
}

func TestConfigRefusesTheSameAddressForBothSides(t *testing.T) {
	g, r := env(map[string]string{"STANDIN_CONTROL_LISTEN": "10.20.0.5:8093"})
	if _, err := standin.LoadConfig(g, r); err == nil {
		t.Fatal("one listener for both credentials must be refused")
	}
}
