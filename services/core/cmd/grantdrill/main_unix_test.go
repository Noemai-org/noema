//go:build unix

package main

// SEC-09 (SR), REQ-SEC-005: the command end to end on a real directory: key files written 0400, loaded by the core's loader, the drill run, the files removed, only counts printed. SYNTHETIC.

import (
	"bytes"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestTheCommandPassesLoadsRealKeyFilesAndLeavesNothingBehind(t *testing.T) {
	work := t.TempDir()
	var out, errw bytes.Buffer
	if code := runCommand([]string{"--work", work}, &out, &errw); code != exitPass {
		t.Fatalf("exit %d\n%s\n%s", code, out.String(), errw.String())
	}
	text := out.String()
	for _, want := range []string{"PASS: 0 valid grants rejected, 0 forged grants accepted", "valid grants rejected 0;", "forgeries accepted 0", "(must be above 0)"} {
		if !strings.Contains(text, want) {
			t.Errorf("the output lacks %q:\n%s", want, text)
		}
	}
	if regexp.MustCompile(`[0-9a-fA-F]{32}`).MatchString(text + errw.String()) {
		t.Error("the output holds a run of 32 hex characters (a key or a signature?)")
	}
	left, _ := os.ReadDir(work)
	if len(left) != 0 {
		t.Errorf("%d entries left in the work directory", len(left))
	}
}

func TestTheCommandRefusesAWorkDirectoryItCannotUseExitingTwo(t *testing.T) {
	full := t.TempDir()
	if err := os.WriteFile(filepath.Join(full, "x"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	for label, args := range map[string][]string{
		"no argument":       nil,
		"a missing path":    {"--work", filepath.Join(t.TempDir(), "absent")},
		"a non-empty path":  {"--work", full},
		"an extra argument": {"--work", t.TempDir(), "extra"},
		"an unknown flag":   {"--nope"},
	} {
		var out, errw bytes.Buffer
		if code := runCommand(args, &out, &errw); code != exitCannot {
			t.Errorf("%s: exit %d", label, code)
		}
		if _, err := os.Stat(filepath.Join(full, "x")); err != nil {
			t.Fatalf("%s: a file of the non-empty directory is gone", label)
		}
	}
}
