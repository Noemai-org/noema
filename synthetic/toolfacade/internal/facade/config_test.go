package facade_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/synthetic/toolfacade/internal/facade"
)

// Configuration: the control side must never default to every interface (the arm must have no route to it), the credential and the pack come from files and
// never from the environment (AGENTS section 10). SYNTHETIC (AGENT-25).

const (
	goodBench = "facade-bench-token-facade-bench-token-0001"
	goodPack  = `{"capabilities":[{"capability_id":"fin.cap.a","risk_class":"R0_read","business_meaning":"x","prohibited":false}]}`
)

func cfgEnv(over map[string]string) (func(string) string, func(string) ([]byte, error)) {
	vars := map[string]string{"FACADE_DATA_LISTEN": ":8095", "FACADE_CONTROL_LISTEN": "10.30.0.5:8096",
		"FACADE_BENCH_TOKEN_FILE": "/run/secrets/bench", "FACADE_PACK_FILE": "/etc/facade/pack.json"}
	for k, v := range over {
		vars[k] = v
	}
	files := map[string]string{"/run/secrets/bench": goodBench + "\n", "/etc/facade/pack.json": goodPack}
	return func(k string) string { return vars[k] }, func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, errors.New("no such file")
	}
}

func TestConfigReadsTheCredentialAndThePackFromFiles(t *testing.T) {
	g, r := cfgEnv(nil)
	c, err := facade.LoadConfig(g, r)
	if err != nil {
		t.Fatal(err)
	}
	if c.Options.BenchToken != goodBench || string(c.Options.Pack) != goodPack || c.DataListen != ":8095" || c.ControlListen != "10.30.0.5:8096" {
		t.Fatalf("config: %+v", c)
	}
}

func TestConfigRefusesAControlListenerThatCouldBeEveryInterface(t *testing.T) {
	for _, bad := range []string{"", ":8096", "0.0.0.0:8096", "[::]:8096", "8096", "localhost", "10.30.0.5:99999", "10.30.0.5:0"} {
		g, r := cfgEnv(map[string]string{"FACADE_CONTROL_LISTEN": bad})
		if _, err := facade.LoadConfig(g, r); err == nil {
			t.Errorf("control listen %q must be refused", bad)
		}
	}
}

func TestConfigRefusesTheSameAddressForBothSides(t *testing.T) {
	g, r := cfgEnv(map[string]string{"FACADE_DATA_LISTEN": "10.30.0.5:8096"})
	if _, err := facade.LoadConfig(g, r); err == nil {
		t.Fatal("the two listeners must differ: the arm must not reach the control side")
	}
}

func TestConfigRefusesAMissingShortOrEmptyCredentialAndAMissingPack(t *testing.T) {
	for name, over := range map[string]map[string]string{
		"no token file variable": {"FACADE_BENCH_TOKEN_FILE": ""},
		"token file missing":     {"FACADE_BENCH_TOKEN_FILE": "/nope"},
		"no pack variable":       {"FACADE_PACK_FILE": ""},
		"pack file missing":      {"FACADE_PACK_FILE": "/nope"},
		"no data listen":         {"FACADE_DATA_LISTEN": ""},
	} {
		g, r := cfgEnv(over)
		if _, err := facade.LoadConfig(g, r); err == nil {
			t.Errorf("%s: want an error", name)
		}
	}
	g, _ := cfgEnv(nil)
	short := func(p string) ([]byte, error) { return []byte("short"), nil }
	if _, err := facade.LoadConfig(g, short); err == nil || !strings.Contains(err.Error(), "FACADE_") {
		t.Errorf("a short credential must be refused and name the variable: %v", err)
	}
}
