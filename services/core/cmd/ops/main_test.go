package main

import (
	"strings"
	"testing"
)

type envMap map[string]string

func (e envMap) get(k string) string { return e[k] }

func full() envMap {
	return envMap{
		EnvListenAddr: "127.0.0.1:9443", EnvDSNDir: "/run/ops-dsn", EnvKeyDir: "/run/ops-keys", EnvKeyMaxActive: "2",
		"NOEMA_OPS_TLS_CERT_FILE": "/run/ops-tls/server.crt", "NOEMA_OPS_TLS_KEY_FILE": "/run/ops-tls/server.key", "NOEMA_OPS_CLIENT_CA_FILE": "/run/ops-tls/operator-ca.crt",
		EnvSwitchTimeoutMS: "5000", EnvHeaderTimeoutMS: "5000", EnvReadTimeoutMS: "10000", EnvIdleTimeoutMS: "30000", EnvMaxHeaderBytes: "8192",
	}
}

// REQ-OPS-002, AGENT-16 (A775): every setting is required and none has a default; each missing one is named.
func TestEverySettingIsRequiredAndNamed(t *testing.T) {
	if _, err := loadSettings(Env{Getenv: full().get}); err != nil {
		t.Fatalf("complete settings: %v", err)
	}
	for name := range full() {
		e := full()
		delete(e, name)
		_, err := loadSettings(Env{Getenv: e.get})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("without %s the error is %v; want it named", name, err)
		}
	}
}

// An invalid setting stops the process and names the setting, never its value.
func TestAnInvalidSettingNamesTheSettingAndNotTheValue(t *testing.T) {
	for name, bad := range map[string]string{EnvListenAddr: "no-port-secret", EnvKeyMaxActive: "none", EnvSwitchTimeoutMS: "zero", EnvHeaderTimeoutMS: "-5", EnvReadTimeoutMS: "x", EnvIdleTimeoutMS: "99999999", EnvMaxHeaderBytes: "none"} {
		e := full()
		e[name] = bad
		_, err := loadSettings(Env{Getenv: e.get})
		if err == nil || !strings.Contains(err.Error(), name) || strings.Contains(err.Error(), bad) {
			t.Errorf("%s=%q: error %v; want it to name the setting and not the value", name, bad, err)
		}
	}
}
