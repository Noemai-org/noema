package proxy

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Noemai-org/noema/benchmark/hostility_proxy/internal/jsonx"
)

// Fixture is an impossible-data response the proxy serves in place of the real one (IMPOSSIBLE_STATE). Tushe
// refuses to fake impossible state, so the proxy holds it (DEC-2026-10-12 D4). A fixture file must say
// `"synthetic": true`: it is test data, labelled as such (REQ-ENV-006, AGENT-25). The label is for the file; it is
// not sent to the system under test, which must not be able to tell the response is a fixture.
type Fixture struct {
	Status  int
	Headers map[string]string
	Body    any
}

type fixtureFile struct {
	Synthetic bool              `json:"synthetic"`
	Status    int               `json:"status"`
	Headers   map[string]string `json:"headers"`
	Body      json.RawMessage   `json:"body"`
}

var fixtureName = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// LoadFixtures reads every <name>.json in dir. An empty dir name means none.
func LoadFixtures(dir string) (map[string]Fixture, error) {
	out := map[string]Fixture{}
	if dir == "" {
		return out, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".json")
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if !fixtureName.MatchString(name) {
			return nil, fmt.Errorf("fixture file %q: the name must match %s", e.Name(), fixtureName)
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		fx, err := ParseFixture(raw)
		if err != nil {
			return nil, fmt.Errorf("fixture %s: %w", name, err)
		}
		out[name] = fx
	}
	return out, nil
}

// ParseFixture reads one fixture file.
func ParseFixture(raw []byte) (Fixture, error) {
	var f fixtureFile
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Fixture{}, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Fixture{}, errors.New("trailing data")
	}
	if !f.Synthetic {
		return Fixture{}, errors.New(`must say "synthetic": true (REQ-ENV-006)`)
	}
	if f.Status < 200 || f.Status > 599 {
		return Fixture{}, errors.New("status must be from 200 to 599")
	}
	if len(f.Body) == 0 {
		return Fixture{}, errors.New("body is required")
	}
	body, err := jsonx.Decode(f.Body)
	if err != nil {
		return Fixture{}, fmt.Errorf("body: %w", err)
	}
	return Fixture{Status: f.Status, Headers: f.Headers, Body: body}, nil
}
