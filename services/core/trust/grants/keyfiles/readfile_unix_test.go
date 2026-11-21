//go:build unix

package keyfiles_test

// SEC-09 follow-up (noema#810), REQ-SEC-009, INV-29: ReadFile applies the key-file checks to any secret file. The files are made by the test; the content is SYNTHETIC.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
)

const readLimit = 64

func TestReadFileReturnsARegularOwnedMode0400File(t *testing.T) {
	path := put(t, t.TempDir(), "secret", strings.Repeat("s", 40)+"\n", 0o400)
	raw, err := keyfiles.ReadFile(path, readLimit)
	if err != nil || string(raw) != strings.Repeat("s", 40)+"\n" {
		t.Fatalf("got %q, %v", raw, err)
	}
}

func TestReadFileRefusesWhatLoadRefusesAndNeverEchoesTheContent(t *testing.T) {
	secret := strings.Repeat("c0de", 8)
	cases := map[string]func(t *testing.T, dir string) string{
		"mode 0600": func(t *testing.T, dir string) string { return put(t, dir, "f", secret, 0o600) },
		"mode 0440": func(t *testing.T, dir string) string { return put(t, dir, "f", secret, 0o440) },
		"mode 0444": func(t *testing.T, dir string) string { return put(t, dir, "f", secret, 0o444) },
		"mode 0000": func(t *testing.T, dir string) string { return put(t, dir, "f", secret, 0) },
		"mode 0500": func(t *testing.T, dir string) string { return put(t, dir, "f", secret, 0o500) },
		"too large": func(t *testing.T, dir string) string { return put(t, dir, "f", strings.Repeat("c0de", 64), 0o400) },
		"a directory": func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "f")
			if err := os.Mkdir(p, 0o700); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"a symbolic link": func(t *testing.T, dir string) string {
			target := put(t, t.TempDir(), "elsewhere", secret, 0o400)
			p := filepath.Join(dir, "f")
			if err := os.Symlink(target, p); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"a named pipe (must not block)": func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "f")
			if err := syscall.Mkfifo(p, 0o400); err != nil {
				t.Fatal(err)
			}
			return p
		},
		"absent": func(t *testing.T, dir string) string { return filepath.Join(dir, "absent") },
	}
	for label, build := range cases {
		raw, err := keyfiles.ReadFile(build(t, t.TempDir()), readLimit)
		if err == nil || raw != nil {
			t.Errorf("%s: accepted (%q, %v)", label, raw, err)
			continue
		}
		if strings.Contains(err.Error(), "c0de") {
			t.Errorf("%s: the error carries the content: %v", label, err)
		}
	}
}

func TestReadFileRefusesAFileOwnedByAnotherUser(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the owner of a file can be changed only by root; the owner check is exercised when the tests run as root (CI containers do)")
	}
	path := put(t, t.TempDir(), "f", strings.Repeat("c0de", 8), 0o400)
	if err := os.Chown(path, 65534, -1); err != nil {
		t.Skipf("chown is not allowed here: %v", err)
	}
	if raw, err := keyfiles.ReadFile(path, readLimit); err == nil || raw != nil {
		t.Fatalf("a file owned by another user was accepted (%v)", err)
	}
}

func TestReadFileRefusesANonPositiveLimit(t *testing.T) {
	path := put(t, t.TempDir(), "f", "x", 0o400)
	for _, n := range []int{0, -1} {
		if _, err := keyfiles.ReadFile(path, n); err == nil {
			t.Errorf("limit %d accepted", n)
		}
	}
}
