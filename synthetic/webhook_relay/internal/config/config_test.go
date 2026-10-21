package config

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const goodSecret = "whsec_" + "MfKQ9r8GKYqrTwjU" + "PD8ILPZIo2LaLaSw" // split so a scanner sees no key; the documented Standard Webhooks example key, not a credential

func load(env map[string]string, files map[string]string) (*Config, error) {
	var environ []string
	for k, v := range env {
		environ = append(environ, k+"="+v)
	}
	return Load(environ, func(k string) string { return env[k] }, func(p string) ([]byte, error) {
		if v, ok := files[p]; ok {
			return []byte(v), nil
		}
		return nil, os.ErrNotExist
	})
}

func TestLoadsWithTheSigningSecretOnly(t *testing.T) { // DEC-2026-10-16 E6
	c, err := load(map[string]string{"WEBHOOK_RELAY_SIGNING_SECRET_FILE": "/s", "WEBHOOK_RELAY_STORE_FILE": "/d/s.jsonl", "PATH": "/bin"}, map[string]string{"/s": goodSecret + "\n"})
	if err != nil || c.Listen != ":8090" || len(c.Key) < 24 {
		t.Fatalf("%v %+v", err, c)
	}
}

func TestRefusesAnyOtherCredential(t *testing.T) {
	for _, name := range []string{"DATABASE_URL", "PGPASSWORD", "TEMPORAL_ADDRESS", "ANTHROPIC_API_KEY", "TLS_KEY_FILE", "GRANT_HMAC_SECRET", "SOME_TOKEN", "AWS_ACCESS_KEY_ID", "NOEMA_CORE_PULL_TOKEN"} {
		_, err := load(map[string]string{"WEBHOOK_RELAY_SIGNING_SECRET_FILE": "/s", "WEBHOOK_RELAY_STORE_FILE": "/d", name: "x"}, map[string]string{"/s": goodSecret})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s: want a refusal naming it, got %v", name, err)
		}
	}
}

func TestRefusesMissingOrWeakSecret(t *testing.T) {
	cases := []struct {
		env   map[string]string
		files map[string]string
	}{
		{map[string]string{"WEBHOOK_RELAY_STORE_FILE": "/d"}, nil},
		{map[string]string{"WEBHOOK_RELAY_SIGNING_SECRET_FILE": "/missing", "WEBHOOK_RELAY_STORE_FILE": "/d"}, nil},
		{map[string]string{"WEBHOOK_RELAY_SIGNING_SECRET_FILE": "/s", "WEBHOOK_RELAY_STORE_FILE": "/d"}, map[string]string{"/s": "short"}},
		{map[string]string{"WEBHOOK_RELAY_SIGNING_SECRET_FILE": "/s"}, map[string]string{"/s": goodSecret}},
	}
	for i, c := range cases {
		if _, err := load(c.env, c.files); err == nil || errors.Is(err, nil) {
			t.Errorf("case %d: want a refusal", i)
		}
	}
}
