// Package config reads the relay's settings. The relay holds one credential, the webhook signing secret, read from a
// file named by WEBHOOK_RELAY_SIGNING_SECRET_FILE. It refuses to start if the environment carries any other
// credential: a relay that is reachable from outside must not be able to leak what it was never given
// (DEC-2026-10-16 E6, INV-29 in spirit; AGENTS.md §10).
package config

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/verify"
)

// Config is the validated configuration.
type Config struct {
	Listen string
	Store  string
	Key    []byte
}

const secretEnv = "WEBHOOK_RELAY_SIGNING_SECRET_FILE"

// forbidden names, as substrings of an upper-cased variable name, that mark a credential or a path to one.
var forbidden = []string{"TOKEN", "PASSWORD", "PASSWD", "SECRET", "API_KEY", "APIKEY", "PRIVATE_KEY", "CREDENTIAL", "DATABASE_URL", "DSN", "PGPASS", "TEMPORAL", "AWS_", "ANTHROPIC", "OPENAI", "GEMINI", "TLS_KEY", "SSL_KEY"}

// allowed variables that match a forbidden word but are the relay's own.
var allowed = map[string]bool{secretEnv: true}

// Load builds a Config. environ is the list of KEY=VALUE strings (os.Environ()); getenv and readFile are injected.
func Load(environ []string, getenv func(string) string, readFile func(string) ([]byte, error)) (*Config, error) {
	var bad []string
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		up := strings.ToUpper(name)
		if allowed[up] {
			continue
		}
		for _, w := range forbidden {
			if strings.Contains(up, w) {
				bad = append(bad, name)
				break
			}
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		return nil, fmt.Errorf("the relay holds the webhook signing secret and nothing else; remove %s from its environment", strings.Join(bad, ", "))
	}
	path := getenv(secretEnv)
	if path == "" {
		return nil, fmt.Errorf("%s is required (a path to a file; the value is never read from the environment)", secretEnv)
	}
	raw, err := readFile(path)
	if err != nil {
		return nil, fmt.Errorf("read signing secret: %w", err)
	}
	key, err := verify.ParseSecret(string(raw))
	if err != nil {
		return nil, err
	}
	c := &Config{Listen: getenv("WEBHOOK_RELAY_LISTEN"), Store: getenv("WEBHOOK_RELAY_STORE_FILE"), Key: key}
	if c.Listen == "" {
		c.Listen = ":8090"
	}
	if c.Store == "" {
		return nil, errors.New("WEBHOOK_RELAY_STORE_FILE is required")
	}
	return c, nil
}

// OS loads from the process environment and the file system.
func OS() (*Config, error) { return Load(os.Environ(), os.Getenv, os.ReadFile) }
