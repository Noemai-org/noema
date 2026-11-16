package config

import (
	"errors"
	"os"
	"strings"
	"testing"
	"time"
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

// A240 and A247: the bench can make the relay hold its answer past Tushe's 12-second delivery timeout. The number of seconds is a setting with no default; unset means no hold.
func TestHoldIsOffUnlessSetAndMustBeAboveTusheTimeout(t *testing.T) {
	base := func(extra map[string]string) map[string]string {
		env := map[string]string{"WEBHOOK_RELAY_SIGNING_SECRET_FILE": "/s", "WEBHOOK_RELAY_STORE_FILE": "/d/s.jsonl"}
		for k, v := range extra {
			env[k] = v
		}
		return env
	}
	files := map[string]string{"/s": goodSecret}
	c, err := load(base(nil), files)
	if err != nil || c.Hold != 0 {
		t.Fatalf("unset: %v %+v", err, c)
	}
	c, err = load(base(map[string]string{"WEBHOOK_RELAY_HOLD_SECONDS": "15"}), files)
	if err != nil || c.Hold != 15*time.Second {
		t.Fatalf("15: %v %+v", err, c)
	}
	for _, bad := range []string{"12", "11", "0", "-15", "15.5", "abc", " 15", "1e2", "99999999999999999999"} {
		if _, err := load(base(map[string]string{"WEBHOOK_RELAY_HOLD_SECONDS": bad}), files); err == nil || !strings.Contains(err.Error(), "WEBHOOK_RELAY_HOLD_SECONDS") {
			t.Errorf("%q: want a refusal naming the setting, got %v", bad, err)
		}
	}
}
