// Command grantdrill is the SEC-09 key-rotation drill for the Decision Grant HMAC key (REQ-SEC-005, INV-03, INV-25), on simulated time.
//
//	grantdrill --work DIR
//
// DIR is an existing, empty directory (a tmpfs in the Compose job). The command makes three synthetic keys for the run, writes the three rings an operator goes through as key files (one key; the
// new key and the old; the new key alone), loads each with the same loader the core uses (trust/grants/keyfiles), runs the drill and removes the files. Nothing but counts is printed: never a key,
// a file name's content or a grant. It also runs the unsafe rule once (the old key retired the moment the new one joins) and requires the drill to see valid grants rejected: a drill that cannot fail
// proves nothing.
//
// Exit codes: 0 the drill passed (0 valid grants rejected, 0 forged grants accepted, the control detected); 1 it did not; 2 it could not run (a bad argument, a system that cannot check key files).
// All keys and grants are SYNTHETIC; the clock is simulated.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/Noemai-org/noema/services/core/trust/grants"
	"github.com/Noemai-org/noema/services/core/trust/grants/keyfiles"
)

const (
	exitPass   = 0
	exitFailed = 1
	exitCannot = 2

	secretBytes = 32
)

func main() { os.Exit(runCommand(os.Args[1:], os.Stdout, os.Stderr)) }

// mount writes the rings as key files and loads them with the core's loader. A ring is loaded once per directory.
type mount struct {
	dirs  map[string]string
	rings map[string]*grants.Keys
}

func writeKey(dir string, version string, secret []byte) error {
	return os.WriteFile(filepath.Join(dir, "grant.hmac."+version+".key"), []byte(hex.EncodeToString(secret)+"\n"), 0o400)
}

// newMount writes the three key directories in a fixed order. On an error it still returns the mount with every directory made so far (Codex P2 on noema#708), so the
// caller's cleanup removes a partial write too; the error says only that it failed.
func newMount(work string, oldSecret, newSecret []byte) (*mount, error) {
	m := &mount{dirs: map[string]string{}, rings: map[string]*grants.Keys{}}
	layout := []struct {
		name string
		keys map[string][]byte
	}{{"one", map[string][]byte{"1": oldSecret}}, {"both", map[string][]byte{"1": oldSecret, "2": newSecret}}, {"new", map[string][]byte{"2": newSecret}}}
	for _, d := range layout {
		dir := filepath.Join(work, d.name)
		if err := os.Mkdir(dir, 0o700); err != nil {
			return m, err
		}
		m.dirs[d.name] = dir
		for version, secret := range d.keys {
			if err := writeKey(dir, version, secret); err != nil {
				return m, err
			}
		}
	}
	return m, nil
}

// cleanup removes every directory the mount made, whether or not the mount is complete. A nil mount has none.
func (m *mount) cleanup() {
	if m == nil {
		return
	}
	for _, dir := range m.dirs {
		_ = os.RemoveAll(dir)
	}
}

func (m *mount) load(name string) (*grants.Keys, error) {
	if k, ok := m.rings[name]; ok {
		return k, nil
	}
	k, err := keyfiles.Load(m.dirs[name], keyfiles.Options{MaxActive: 2})
	if err != nil {
		return nil, err
	}
	m.rings[name] = k
	return k, nil
}

// ring is the operator's order: the old key alone, then both, then (from retireAt) the new key alone.
func (m *mount) ring(retireAt time.Duration) ringAt {
	return func(at time.Duration) (*grants.Keys, error) {
		switch {
		case at < overlap:
			return m.load("one")
		case at < retireAt:
			return m.load("both")
		default:
			return m.load("new")
		}
	}
}

func secret() ([]byte, error) {
	b := make([]byte, secretBytes)
	_, err := rand.Read(b)
	return b, err
}

func runCommand(args []string, out, errw io.Writer) int {
	fs := flag.NewFlagSet("grantdrill", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	work := fs.String("work", "", "an existing, empty directory for the synthetic key files")
	if err := fs.Parse(args); err != nil || *work == "" || fs.NArg() != 0 {
		_, _ = fmt.Fprintln(errw, "usage: grantdrill --work DIR (an existing, empty directory)")
		return exitCannot
	}
	entries, err := os.ReadDir(*work)
	if err != nil || len(entries) != 0 {
		_, _ = fmt.Fprintln(errw, "grantdrill: --work must be an existing, empty directory")
		return exitCannot
	}
	oldSecret, e1 := secret()
	newSecret, e2 := secret()
	attacker, e3 := secret()
	if err := errors.Join(e1, e2, e3); err != nil {
		_, _ = fmt.Fprintln(errw, "grantdrill: no random source")
		return exitCannot
	}
	m, err := newMount(*work, oldSecret, newSecret)
	defer m.cleanup()
	if err != nil {
		_, _ = fmt.Fprintln(errw, "grantdrill: cannot write the key files")
		return exitCannot
	}
	if _, err := m.load("one"); err != nil {
		if errors.Is(err, keyfiles.ErrUnsupported) {
			_, _ = fmt.Fprintln(errw, "grantdrill: key files cannot be checked on this system")
		} else {
			_, _ = fmt.Fprintln(errw, "grantdrill: the key files cannot be loaded")
		}
		return exitCannot
	}
	safeRetire := overlap + validity // the last grant signed by the old key was issued before the overlap, so it has expired by now
	safe, err := run(plan{ring: m.ring(safeRetire), retireAt: safeRetire, oldSecret: oldSecret, attacker: attacker})
	if err != nil {
		_, _ = fmt.Fprintln(errw, "grantdrill: the safe run could not complete")
		return exitCannot
	}
	control, err := run(plan{ring: m.ring(overlap), retireAt: overlap, oldSecret: oldSecret, attacker: attacker})
	if err != nil {
		_, _ = fmt.Fprintln(errw, "grantdrill: the control run could not complete")
		return exitCannot
	}
	_, _ = fmt.Fprintln(out, "grantdrill: SYNTHETIC keys and simulated time")
	_, _ = fmt.Fprintf(out, "safe rule (old key retired after its last grant expired): grants %d (old key %d, new key %d); checks %d; valid grants rejected %d; forgeries tried %d; forgeries accepted %d\n",
		safe.issued, safe.signedOld, safe.signedNew, safe.checks, safe.rejectedValid, safe.tried, safe.forgedAccepted)
	_, _ = fmt.Fprintf(out, "control (old key retired the moment the new one joined): valid grants rejected %d (must be above 0)\n", control.rejectedValid)
	problems := safe.failures()
	if control.rejectedValid == 0 {
		problems = append(problems, "the control rejected nothing: the drill cannot detect the operator mistake")
	}
	for _, p := range problems {
		_, _ = fmt.Fprintln(out, "FAIL:", p)
	}
	if len(problems) != 0 {
		return exitFailed
	}
	_, _ = fmt.Fprintln(out, "PASS: 0 valid grants rejected, 0 forged grants accepted")
	return exitPass
}
