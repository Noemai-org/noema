package signing

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

const errPrefix = "signing: "

// Tag is the fixed domain tag that precedes the canonical bytes of a signed object (D6). It stops a signature over one type of
// object from verifying as another.
type Tag string

// The tags core-execution signs under. Outcome's verdict tag is Outcome's own and is not here.
const (
	ReceiptTag     Tag = "noema/receipt/v1\n"
	RecordEventTag Tag = "noema/record-event/v1\n"
)

func (t Tag) known() bool { return t == ReceiptTag || t == RecordEventTag }

var (
	identityRe = regexp.MustCompile(`^[a-z][a-z-]*$`)
	keyIDRe    = regexp.MustCompile(`^([a-z][a-z-]*)\.ed25519\.([1-9][0-9]{0,5})$`)
	seedRe     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	sigRe      = regexp.MustCompile(`^[0-9a-f]{128}$`)
)

// Options are the policy numbers of the keyring. They are not constants of this package: the rotation overlap is a policy
// (REQ-SEC-005, D5) and comes from configuration (REQ-OPS-003), supplied by the caller.
type Options struct {
	// MaxActive is how many keys of one identity may be mounted at once: the rotation overlap. At least one.
	MaxActive int
}

// A key file is the seed as lower-case hex and a line feed, readable by its owner alone (D3a).
var (
	maxKeyFileBytes = hex.EncodedLen(ed25519.SeedSize) + 1
	keyFileMode     = os.FileMode(syscall.S_IRUSR)
)

// The identity is the first group of keyIDRe and the version is the last.
const groupIdentity = 1

// Key is a private signing key. Nothing prints it: see Format, LogValue and the marshallers.
type Key struct {
	identity string
	version  int
	priv     ed25519.PrivateKey
}

// ID is `<identity>.ed25519.<version>` (D4).
func (k Key) ID() string { return k.identity + ".ed25519." + strconv.Itoa(k.version) }

func (k Key) redacted() string { return "signing.Key{" + k.ID() + ", key material redacted}" }

// Format makes every fmt verb print the redacted form.
func (k Key) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, k.redacted()) }

// String, GoString and LogValue keep the key out of fmt, %#v and slog.
func (k Key) String() string       { return k.redacted() }
func (k Key) GoString() string     { return k.redacted() }
func (k Key) LogValue() slog.Value { return slog.StringValue(k.redacted()) }

// MarshalText and MarshalJSON keep the key out of encoders.
func (k Key) MarshalText() ([]byte, error) { return []byte(k.redacted()), nil }
func (k Key) MarshalJSON() ([]byte, error) { return json.Marshal(k.redacted()) }

// Ring holds the active keys of one identity, oldest first. The signer uses the newest (D5).
type Ring struct {
	identity string
	keys     []Key
}

func (r Ring) redacted() string {
	ids := make([]string, len(r.keys))
	for i, k := range r.keys {
		ids[i] = k.ID()
	}
	return "signing.Ring{" + strings.Join(ids, ", ") + "; key material redacted}"
}

// Format, String, GoString, LogValue and the marshallers print the redacted form, like Key.
func (r Ring) Format(f fmt.State, _ rune)   { _, _ = io.WriteString(f, r.redacted()) }
func (r Ring) String() string               { return r.redacted() }
func (r Ring) GoString() string             { return r.redacted() }
func (r Ring) LogValue() slog.Value         { return slog.StringValue(r.redacted()) }
func (r Ring) MarshalText() ([]byte, error) { return []byte(r.redacted()), nil }
func (r Ring) MarshalJSON() ([]byte, error) { return json.Marshal(r.redacted()) }

// Newest is the key the signer uses.
func (r *Ring) Newest() Key { return r.keys[len(r.keys)-1] }

// PublicKeys are the public halves by key ID. The deploy step registers them (D3a); a verifier needs nothing else.
func (r *Ring) PublicKeys() map[string]ed25519.PublicKey {
	out := make(map[string]ed25519.PublicKey, len(r.keys))
	for _, k := range r.keys {
		out[k.ID()] = k.priv.Public().(ed25519.PublicKey)
	}
	return out
}

// LoadRing reads the keys of one identity from dir, the secrets directory the deploy step mounts (D3, D3a). Every `*.key` file must
// be a regular file (not a link) with mode 0400, named `<identity>.ed25519.<version>.key`, holding the seed as 64 lower-case hex
// characters. At most opt.MaxActive keys may be present (the rotation overlap). Any other file is ignored; any key file that is not exactly this is an error, and
// an error never repeats a file's contents. With no key there is no signer.
func LoadRing(identity, dir string, opt Options) (*Ring, error) {
	if opt.MaxActive < 1 {
		return nil, errors.New(errPrefix + "the rotation overlap must be at least one key")
	}
	if !identityRe.MatchString(identity) {
		return nil, errors.New(errPrefix + "the identity is not a service identity")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("%scannot read the key directory: %w", errPrefix, err)
	}
	var keys []Key
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".key") {
			continue
		}
		k, err := loadKey(identity, filepath.Join(dir, name), strings.TrimSuffix(name, ".key"))
		if err != nil {
			return nil, err
		}
		keys = append(keys, k)
	}
	switch {
	case len(keys) == 0:
		return nil, errors.New(errPrefix + "no signing key: nothing may sign without one")
	case len(keys) > opt.MaxActive:
		return nil, fmt.Errorf("%s%d keys for %s; at most %d may be active", errPrefix, len(keys), identity, opt.MaxActive)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].version < keys[j].version })
	return &Ring{identity: identity, keys: keys}, nil
}

func loadKey(identity, path, id string) (Key, error) {
	m := keyIDRe.FindStringSubmatch(id)
	if m == nil {
		return Key{}, fmt.Errorf("%s%q is not named <identity>.ed25519.<version>", errPrefix, filepath.Base(path))
	}
	if m[groupIdentity] != identity {
		return Key{}, fmt.Errorf("%s%q is a key of another identity", errPrefix, filepath.Base(path))
	}
	version, _ := strconv.Atoi(m[len(m)-1])
	// One descriptor serves every check and the read, so a swap of the path after the open cannot change what is read.
	// O_NOFOLLOW refuses a symbolic link; O_NONBLOCK stops a FIFO from holding the open.
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return Key{}, fmt.Errorf("%scannot open %q (a symbolic link is refused): %w", errPrefix, filepath.Base(path), err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return Key{}, fmt.Errorf("%scannot stat %q: %w", errPrefix, filepath.Base(path), err)
	}
	if !info.Mode().IsRegular() {
		return Key{}, fmt.Errorf("%s%q is not a regular file", errPrefix, filepath.Base(path))
	}
	if err := checkOwner(info); err != nil {
		return Key{}, fmt.Errorf("%s%q: %w", errPrefix, filepath.Base(path), err)
	}
	if info.Mode().Perm() != keyFileMode {
		return Key{}, fmt.Errorf("%s%q has mode %v; a key file is 0400", errPrefix, filepath.Base(path), info.Mode().Perm())
	}
	if info.Size() > int64(maxKeyFileBytes) {
		return Key{}, fmt.Errorf("%s%q is too large to be a key file", errPrefix, filepath.Base(path))
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(maxKeyFileBytes)+1))
	if err != nil {
		return Key{}, fmt.Errorf("%scannot read %q: %w", errPrefix, filepath.Base(path), err)
	}
	if len(raw) > maxKeyFileBytes {
		return Key{}, fmt.Errorf("%s%q is too large to be a key file", errPrefix, filepath.Base(path))
	}
	text := strings.TrimSuffix(string(raw), "\n")
	if !seedRe.MatchString(text) {
		return Key{}, fmt.Errorf("%s%q does not hold one seed of 64 lower-case hex characters", errPrefix, filepath.Base(path))
	}
	seed, err := hex.DecodeString(text)
	if err != nil {
		return Key{}, fmt.Errorf("%s%q does not hold a hex seed", errPrefix, filepath.Base(path))
	}
	return Key{identity: identity, version: version, priv: ed25519.NewKeyFromSeed(seed)}, nil
}

// checkOwner refuses a key file that does not belong to the effective user of the process. It reads the owner from the
// Stat of the open descriptor, so it describes the file whose bytes are then read.
func checkOwner(info os.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st == nil {
		return errors.New("the owner of the file cannot be read")
	}
	if st.Uid != uint32(os.Geteuid()) {
		return errors.New("the file is owned by another user")
	}
	return nil
}

// Signed is a signature and the exact bytes that were signed. The ledger stores both (D8).
type Signed struct {
	KeyID     string // `<identity>.ed25519.<version>`
	Value     string // 128 lower-case hex characters
	SignedJCS []byte // the document without sig, in RFC 8785 form, without the tag
}

// Sign signs a document under the newest key. The document must not carry sig. The message is the tag followed by the RFC 8785
// bytes of the document (D6).
func (r *Ring) Sign(tag Tag, doc map[string]any) (Signed, error) {
	if _, has := doc["sig"]; has {
		return Signed{}, errors.New(errPrefix + "the document already carries sig")
	}
	canon, err := crypto.CanonicalJSON(doc)
	if err != nil {
		return Signed{}, fmt.Errorf("%scanonical document: %w", errPrefix, err)
	}
	return r.sign(tag, canon)
}

// SignJCS signs bytes that are already a document, and only if they are exactly canonical: an RFC 8785 object with no repeated
// member name at any depth, no float, no sig and nothing after it. The database stores these bytes and compares them with the row,
// so what is signed is what is stored (D8).
func (r *Ring) SignJCS(tag Tag, jcs []byte) (Signed, error) {
	if err := checkCanonical(jcs); err != nil {
		return Signed{}, err
	}
	return r.sign(tag, jcs)
}

func (r *Ring) sign(tag Tag, canon []byte) (Signed, error) {
	if !tag.known() {
		return Signed{}, errors.New(errPrefix + "unknown domain tag")
	}
	k := r.Newest()
	sig := ed25519.Sign(k.priv, message(tag, canon))
	return Signed{KeyID: k.ID(), Value: hex.EncodeToString(sig), SignedJCS: append([]byte(nil), canon...)}, nil
}

// Verify reports whether sigHex is a signature by pub over the tag followed by jcs. It accepts only 128 lower-case hex characters.
func Verify(pub ed25519.PublicKey, tag Tag, jcs []byte, sigHex string) bool {
	if !tag.known() || len(pub) != ed25519.PublicKeySize || !sigRe.MatchString(sigHex) {
		return false
	}
	sig, err := hex.DecodeString(sigHex)
	if err != nil {
		return false
	}
	return ed25519.Verify(pub, message(tag, jcs), sig)
}

func message(tag Tag, canon []byte) []byte {
	out := make([]byte, 0, len(tag)+len(canon))
	return append(append(out, tag...), canon...)
}

// checkCanonical refuses a document that is not canonical, that repeats a member name, or that carries sig.
func checkCanonical(doc []byte) error {
	dec := json.NewDecoder(bytes.NewReader(doc))
	dec.UseNumber()
	if err := noDuplicateMembers(dec); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New(errPrefix + "data after the document")
	}
	var v map[string]any
	d := json.NewDecoder(bytes.NewReader(doc))
	d.UseNumber()
	if err := d.Decode(&v); err != nil || v == nil {
		return errors.New(errPrefix + "the document is not a JSON object")
	}
	if _, has := v["sig"]; has {
		return errors.New(errPrefix + "the document already carries sig")
	}
	canon, err := crypto.CanonicalJSON(v)
	if err != nil {
		return fmt.Errorf("%sthe document cannot be canonical: %w", errPrefix, err)
	}
	if !bytes.Equal(canon, doc) {
		return errors.New(errPrefix + "the document is not in RFC 8785 form")
	}
	return nil
}

// noDuplicateMembers reads one value and refuses a repeated member name at any depth. Go's own decoder would keep the last, and
// PostgreSQL's jsonb the last too, so the signed bytes and the stored document could differ.
func noDuplicateMembers(dec *json.Decoder) error {
	tok, err := dec.Token()
	if err != nil {
		return fmt.Errorf("%sthe document is not JSON: %w", errPrefix, err)
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return nil
	}
	switch d {
	case '{':
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return fmt.Errorf("%sthe document is not JSON: %w", errPrefix, err)
			}
			name, _ := k.(string)
			if seen[name] {
				return fmt.Errorf("%sthe member %q appears more than once", errPrefix, name)
			}
			seen[name] = true
			if err := noDuplicateMembers(dec); err != nil {
				return err
			}
		}
	case '[':
		for dec.More() {
			if err := noDuplicateMembers(dec); err != nil {
				return err
			}
		}
	}
	_, err = dec.Token()
	return err
}
