//go:build unix

package keyfiles_test

// SEC-09 (SR), REQ-SEC-005, INV-29: Load on real files. The directory and the files are made by the test, owned by the test's user; the keys are SYNTHETIC.

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
)

func put(t *testing.T, dir, name, content string, mode os.FileMode) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoadReadsOneOrTwoKeysAndIgnoresOtherFiles(t *testing.T) {
	dir := t.TempDir()
	put(t, dir, "grant.hmac.1.key", hexOf("1")+"\n", 0o400)
	put(t, dir, "README", "not a key", 0o644)
	ring, err := keyfiles.Load(dir, keyfiles.Options{MaxActive: 2})
	if err != nil {
		t.Fatal(err)
	}
	if g := issue(t, ring); g.Sig.KeyId != "grant.hmac.1" {
		t.Fatalf("signed under %q", g.Sig.KeyId)
	}
	put(t, dir, "grant.hmac.2.key", hexOf("2"), 0o400)
	ring, err = keyfiles.Load(dir, keyfiles.Options{MaxActive: 2})
	if err != nil {
		t.Fatal(err)
	}
	if g := issue(t, ring); g.Sig.KeyId != "grant.hmac.2" {
		t.Fatalf("signed under %q, want the newest", g.Sig.KeyId)
	}
}

func TestLoadRefusesWhatIsNotExactlyAKeyFile(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"mode 0600":        func(t *testing.T, dir string) { put(t, dir, "grant.hmac.1.key", hexOf("1"), 0o600) },
		"mode 0440":        func(t *testing.T, dir string) { put(t, dir, "grant.hmac.1.key", hexOf("1"), 0o440) },
		"mode 0444":        func(t *testing.T, dir string) { put(t, dir, "grant.hmac.1.key", hexOf("1"), 0o444) },
		"mode 0000":        func(t *testing.T, dir string) { put(t, dir, "grant.hmac.1.key", hexOf("1"), 0) },
		"a wrong name":     func(t *testing.T, dir string) { put(t, dir, "grant.hmac.01.key", hexOf("1"), 0o400) },
		"bad content":      func(t *testing.T, dir string) { put(t, dir, "grant.hmac.1.key", "nope", 0o400) },
		"a file too large": func(t *testing.T, dir string) { put(t, dir, "grant.hmac.1.key", strings.Repeat("a", 4096), 0o400) },
		"a directory":      func(t *testing.T, dir string) { _ = os.Mkdir(filepath.Join(dir, "grant.hmac.1.key"), 0o700) },
		"a symbolic link": func(t *testing.T, dir string) {
			target := put(t, t.TempDir(), "elsewhere", hexOf("1"), 0o400)
			if err := os.Symlink(target, filepath.Join(dir, "grant.hmac.1.key")); err != nil {
				t.Fatal(err)
			}
		},
		"a named pipe (must not block)": func(t *testing.T, dir string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "grant.hmac.1.key"), 0o400); err != nil {
				t.Fatal(err)
			}
		},
		"one good key and one bad": func(t *testing.T, dir string) {
			put(t, dir, "grant.hmac.1.key", hexOf("1"), 0o400)
			put(t, dir, "grant.hmac.2.key", hexOf("2"), 0o640)
		},
		"three keys": func(t *testing.T, dir string) {
			for i, c := range []string{"1", "2", "3"} {
				put(t, dir, "grant.hmac."+string(rune('1'+i))+".key", hexOf(c), 0o400)
			}
		},
		"two keys with one secret": func(t *testing.T, dir string) {
			put(t, dir, "grant.hmac.1.key", hexOf("1"), 0o400)
			put(t, dir, "grant.hmac.2.key", hexOf("1"), 0o400)
		},
		"no key at all": func(t *testing.T, dir string) { put(t, dir, "README", "x", 0o644) },
	}
	for label, build := range cases {
		dir := t.TempDir()
		build(t, dir)
		if _, err := keyfiles.Load(dir, keyfiles.Options{MaxActive: 2}); err == nil {
			t.Errorf("%s: accepted", label)
		}
	}
}

func TestLoadRefusesAMissingDirectoryAndNeverEchoesAFileContent(t *testing.T) {
	if _, err := keyfiles.Load(filepath.Join(t.TempDir(), "absent"), keyfiles.Options{MaxActive: 2}); err == nil {
		t.Error("a missing directory was accepted")
	}
	dir := t.TempDir()
	put(t, dir, "grant.hmac.1.key", strings.Repeat("c0de", 16)+"z", 0o400)
	_, err := keyfiles.Load(dir, keyfiles.Options{MaxActive: 2})
	if err == nil || strings.Contains(err.Error(), "c0de") {
		t.Fatalf("error %v", err)
	}
}
