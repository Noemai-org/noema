//go:build unix

package main

// SEC-09 (SR), REQ-SEC-005: a key-file write that fails part way leaves nothing behind (Codex P2 on noema#708). Unix only: the files are real. SYNTHETIC keys.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAMountThatFailsPartWayStillReturnsWhatItMadeAndCleanupLeavesTheDirectoryEmpty(t *testing.T) {
	work := t.TempDir()
	// "new" is the last directory made; a plain file of that name makes its Mkdir fail after "one" and "both" were written.
	if err := os.WriteFile(filepath.Join(work, "new"), []byte("in the way"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := newMount(work, []byte(strings.Repeat("a", 32)), []byte(strings.Repeat("b", 32)))
	if err == nil {
		t.Fatal("the mount did not fail")
	}
	if m == nil || len(m.dirs) != 2 {
		t.Fatalf("the failed mount must report the two directories it made, got %v", m)
	}
	m.cleanup()
	for _, name := range []string{"one", "both"} {
		if _, err := os.Stat(filepath.Join(work, name)); !os.IsNotExist(err) {
			t.Errorf("%s was left behind", name)
		}
	}
	if err := os.Remove(filepath.Join(work, "new")); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(work)
	if len(entries) != 0 {
		t.Errorf("the work directory is not empty after cleanup: %d entries", len(entries))
	}
}

func TestAMountThatFailsOnItsFirstDirectoryReportsNothingAndNilCleanupIsSafe(t *testing.T) {
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "one"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := newMount(work, []byte(strings.Repeat("a", 32)), []byte(strings.Repeat("b", 32)))
	if err == nil || m == nil || len(m.dirs) != 0 {
		t.Fatalf("expected an empty mount and an error, got %v, %v", m, err)
	}
	m.cleanup()
	var none *mount
	none.cleanup()
}
