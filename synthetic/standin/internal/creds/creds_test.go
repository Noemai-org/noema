package creds_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/synthetic/standin/internal/creds"
)

// The one-shot step that creates the stand-in's three credentials in a volume (never in the repository). REQ-ENV-002: they must differ.

func TestEveryMissingFileIsCreatedWithADifferentRandomValue(t *testing.T) {
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "bench", "token"), filepath.Join(dir, "runtime", "token"), filepath.Join(dir, "principal", "key")}
	if err := creds.Init(paths); err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		v := strings.TrimSpace(string(b))
		if len(v) < 64 || seen[v] {
			t.Fatalf("%s: the value must be long and different from the others (len %d, repeated %t)", p, len(v), seen[v])
		}
		seen[v] = true
		if st, _ := os.Stat(p); st.Mode().Perm() != 0o444 {
			t.Fatalf("%s: mode %v, want read-only for all (the volume is local; the service and its callers run as different users)", p, st.Mode().Perm())
		}
	}
}

func TestAnExistingValueIsNeverReplaced(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "token")
	if err := os.WriteFile(p, []byte("keep-me\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := creds.Init([]string{p}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "keep-me\n" {
		t.Fatalf("an existing credential was replaced: %q", b)
	}
}

func TestNoFilesIsAnError(t *testing.T) {
	if err := creds.Init(nil); err == nil {
		t.Fatal("init with no file must be refused")
	}
}

func TestTheSameFileTwiceIsRefused(t *testing.T) { // two credentials that share a file would be one credential
	p := filepath.Join(t.TempDir(), "token")
	if err := creds.Init([]string{p, p}); err == nil {
		t.Fatal("a repeated path must be refused")
	}
}
