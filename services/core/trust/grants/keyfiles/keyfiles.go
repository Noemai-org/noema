// Package keyfiles loads the Decision Grant HMAC key ring from the secrets directory the deploy step mounts (SEC-09; REQ-SEC-005, REQ-SEC-001; INV-03, INV-29).
//
// A key is one file, `grant.hmac.<version>.key`, holding the 32-byte secret as 64 lower-case hex characters (a final newline is allowed). The directory holds one key, or two during a rotation:
// the highest version signs, either verifies, and a retired key is simply not in the directory, so a grant signed under it is refused. This is the way the orchestrator's signing keys are
// loaded (execution/signing.LoadRing), so one mounting convention serves both: a regular file, not a link, owned by the process's user, mode 0400.
//
// May import: the standard library and trust/grants.
//
// Must never: print, log or return a secret or any part of one (an error names the file, never its content); read a path outside the directory it is given; follow a symbolic link or block on a
// special file; fall back to a default key, to an environment variable or to a weaker file mode; run on a system where the owner and the mode of a file cannot be checked.
package keyfiles

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Noemai-org/noema/services/core/trust/grants"
)

// IDPrefix begins the identifier of every grant key: `grant.hmac.<version>`. The identifier is what a grant's signature names.
const IDPrefix = "grant.hmac."

// fileBytes is the most a key file may hold: the secret in hex and a newline.
const fileBytes = sha256.Size + sha256.Size + 1

var (
	// ErrKeyFiles marks a key directory that cannot be used: a bad file, no key, too many keys, or a system that cannot check a file.
	ErrKeyFiles = errors.New("keyfiles: the grant key directory is not usable")
	// ErrUnsupported marks a system on which the owner and mode of a key file cannot be checked. Nothing is loaded there.
	ErrUnsupported = errors.New("keyfiles: key files cannot be checked on this system")

	nameRe   = regexp.MustCompile(`^grant\.hmac\.([1-9][0-9]{0,5})\.key$`)
	secretRe = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Options says how many keys may be present at once. There is no default: the rotation overlap is a decision (REQ-SEC-005), and a ring that is larger than grants.Keys allows is refused there.
type Options struct{ MaxActive int }

// Parsed is one key file read and checked: its version, and the key (identifier and secret).
type Parsed struct {
	Version int
	Key     grants.Key
}

// Format keeps the secret out of every fmt verb (%v, %+v, %#v, %s): a Parsed prints as its identifier and a redaction mark. It does not cover the grants.Key a caller copies out of it.
func (p Parsed) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "keyfiles.Parsed{%s, key material redacted}", p.Key.ID)
}

// Parse checks one key file's name and content. name is the file's base name. The error never holds the content.
func Parse(name string, raw []byte) (Parsed, error) {
	m := nameRe.FindStringSubmatch(name)
	if m == nil {
		return Parsed{}, fmt.Errorf("%w: %q is not named grant.hmac.<version>.key", ErrKeyFiles, name)
	}
	if len(raw) > fileBytes {
		return Parsed{}, fmt.Errorf("%w: %q is too large to be a key file", ErrKeyFiles, name)
	}
	text := strings.TrimSuffix(string(raw), "\n")
	if !secretRe.MatchString(text) {
		return Parsed{}, fmt.Errorf("%w: %q does not hold one secret of 64 lower-case hex characters", ErrKeyFiles, name)
	}
	secret, err := hex.DecodeString(text)
	if err != nil {
		return Parsed{}, fmt.Errorf("%w: %q does not hold a hex secret", ErrKeyFiles, name)
	}
	version, err := strconv.Atoi(m[1])
	if err != nil {
		return Parsed{}, fmt.Errorf("%w: %q has no version", ErrKeyFiles, name)
	}
	return Parsed{Version: version, Key: grants.Key{ID: IDPrefix + m[1], Secret: secret}}, nil
}

// Ring makes the key ring of the parsed files: the highest version first, which signs. It refuses no key, more keys than MaxActive, a MaxActive below one, and whatever grants.NewKeys
// refuses (a repeated identifier, a short secret, two keys with the same secret).
func Ring(files []Parsed, opt Options) (*grants.Keys, error) {
	switch {
	case opt.MaxActive < 1:
		return nil, fmt.Errorf("%w: the rotation overlap must be at least one key", ErrKeyFiles)
	case len(files) == 0:
		return nil, fmt.Errorf("%w: no grant key: nothing may sign without one", ErrKeyFiles)
	case len(files) > opt.MaxActive:
		return nil, fmt.Errorf("%w: %d keys; at most %d may be active", ErrKeyFiles, len(files), opt.MaxActive)
	}
	sorted := append([]Parsed(nil), files...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version > sorted[j].Version })
	keys := make([]grants.Key, 0, len(sorted))
	for _, p := range sorted {
		keys = append(keys, p.Key)
	}
	ring, err := grants.NewKeys(keys)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrKeyFiles, err)
	}
	return ring, nil
}

// Load reads every `*.key` file of dir (any other file is ignored; a `*.key` file that is not exactly a grant key file is an error) and returns the ring. Each file is opened without
// following a link, must be a regular file owned by the process's user with mode 0400, and is read at most one byte past its limit.
func Load(dir string, opt Options) (*grants.Keys, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%w: the directory cannot be read: %w", ErrKeyFiles, err)
	}
	var files []Parsed
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".key") {
			continue
		}
		raw, err := readKeyFile(filepath.Join(dir, name))
		if err != nil {
			return nil, fmt.Errorf("%w: %q: %w", ErrKeyFiles, name, err)
		}
		p, err := Parse(name, raw)
		if err != nil {
			return nil, err
		}
		files = append(files, p)
	}
	return Ring(files, opt)
}

// ReadFile reads one secret file under the same checks as Load: opened without following a link or blocking on a special file; a regular file owned by the process's user with mode 0400;
// at most maxBytes long. The error never holds the file's content. It is for a secret that is a file but not a grant key (the Input Gateway's input-hash key).
func ReadFile(path string, maxBytes int) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("not a readable regular file")
	}
	return os.ReadFile(path)
}
