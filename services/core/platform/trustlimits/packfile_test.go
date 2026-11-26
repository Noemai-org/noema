package trustlimits_test

// A612 (interim; AGENT-15, AGENT-16, INV-14): the loader refuses a malformed or v1 limits file at START-UP, not only packcheck. LoadPackFile is the start-up validation EL's
// composition calls with the verified Pack.Limits map (the bytes the manifest pinned) and the path the composition names; the core names no file. All data is SYNTHETIC.

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/trustlimits"
)

const shippedPath = "../../../../packs/fintech/limits/trust_limits.json"

func shipped(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile(shippedPath)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestAStartupLoadOfTheShippedFileSucceeds(t *testing.T) {
	files := map[string][]byte{"fintech/limits/trust_limits.json": shipped(t), "fintech/limits/other_limits.json": []byte(`{}`)}
	l, err := trustlimits.LoadPackFile(files, "fintech/limits/trust_limits.json", "fin")
	if err != nil || !l.Loaded() {
		t.Fatalf("the shipped file was refused at start-up: %v", err)
	}
	if got, cerr := l.ConfirmationTTLCeilingMS(); cerr != nil || got != 600000 {
		t.Errorf("ceiling %d, %v", got, cerr)
	}
}

func TestAStartupLoadRefusesAMalformedV1OrIncompleteFileAndAnAbsentOne(t *testing.T) {
	good := string(shipped(t))
	cases := map[string]string{
		"not json":            `{`,
		"empty":               ``,
		"v1 shaped":           strings.Replace(strings.Replace(good, `"schema_version": "2.0"`, `"schema_version": "1.0"`, 1), `"confirmation_ttl_ceiling_ms": 600000`, `"label": "SYNTHETIC"`, 1),
		"v1 with the ceiling": strings.Replace(good, `"schema_version": "2.0"`, `"schema_version": "1.0"`, 1),
		"no ceiling":          strings.Replace(good, `"confirmation_ttl_ceiling_ms": 600000`, `"label": "SYNTHETIC"`, 1),
		"unknown member":      strings.Replace(good, `"pack_id": "fin"`, `"pack_id": "fin", "an_unknown_member": 1`, 1),
		"another pack":        strings.Replace(good, `"pack_id": "fin"`, `"pack_id": "other"`, 1),
	}
	for name, text := range cases {
		l, err := trustlimits.LoadPackFile(map[string][]byte{"p/trust_limits.json": []byte(text)}, "p/trust_limits.json", "fin")
		if err == nil || l.Loaded() {
			t.Errorf("%s: a malformed file was accepted at start-up", name)
			continue
		}
		var te *trustlimits.Error
		if !errors.As(err, &te) {
			t.Errorf("%s: the refusal is not a *trustlimits.Error: %T", name, err)
		}
	}
	for name, files := range map[string]map[string][]byte{"path not pinned": {"p/other.json": []byte(good)}, "no files": nil, "empty map": {}} {
		if l, err := trustlimits.LoadPackFile(files, "p/trust_limits.json", "fin"); err == nil || l.Loaded() {
			t.Errorf("%s: an absent limits file was accepted at start-up", name)
		}
	}
	if _, err := trustlimits.LoadPackFile(map[string][]byte{"p/trust_limits.json": []byte(good)}, "", "fin"); err == nil {
		t.Error("an empty path was accepted")
	}
}

func TestAStartupRefusalNamesNoPathAndNoContent(t *testing.T) {
	_, err := trustlimits.LoadPackFile(map[string][]byte{"secret/path/trust_limits.json": []byte(`{"schema_version":"1.0","pack_id":"fin"}`)}, "secret/path/trust_limits.json", "fin")
	if err == nil {
		t.Fatal("accepted")
	}
	if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "1.0") {
		t.Errorf("the refusal quotes the path or the document: %v", err)
	}
}
