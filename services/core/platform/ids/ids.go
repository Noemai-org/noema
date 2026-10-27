// Package ids makes the prefixed ULID identifiers of PERD 32.3 (`<prefix>_<ULID>`). The time comes from the
// clock interface (INV-21) and the entropy from an injected reader, so a simulated run is reproducible.
package ids

import (
	"crypto/rand"
	"errors"
	"io"
	"regexp"
	"sync"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	// alphabet is Crockford's base32, the ULID alphabet.
	alphabet   = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	timeChars  = 10
	randChars  = 16
	randBytes  = 10
	bitsInChar = 5
)

var prefixRe = regexp.MustCompile(`^[a-z]{2,6}$`)

// ErrBadPrefix is returned for a prefix that PERD 32.3 would not allow.
var ErrBadPrefix = errors.New("ids: a prefix is two to six lower-case letters")

// Generator makes IDs. It is safe for concurrent use.
type Generator struct {
	clock clock.Clock
	mu    sync.Mutex
	rand  io.Reader
}

// New returns a Generator. A nil entropy reader means crypto/rand.
func New(c clock.Clock, entropy io.Reader) *Generator {
	if entropy == nil {
		entropy = rand.Reader
	}
	return &Generator{clock: c, rand: entropy}
}

// ID returns `<prefix>_<ULID>`: 48 bits of milliseconds since the Unix epoch, then 80 bits of entropy, in 26
// Crockford base32 characters. IDs made in different milliseconds sort by time.
func (g *Generator) ID(prefix string) (string, error) {
	if !prefixRe.MatchString(prefix) {
		return "", ErrBadPrefix
	}
	ms := uint64(g.clock.Now().UnixMilli())
	var buf [randBytes]byte
	g.mu.Lock()
	_, err := io.ReadFull(g.rand, buf[:])
	g.mu.Unlock()
	if err != nil {
		return "", err
	}
	out := make([]byte, 0, len(prefix)+1+timeChars+randChars)
	out = append(out, prefix...)
	out = append(out, '_')
	for i := timeChars - 1; i >= 0; i-- {
		out = append(out, alphabet[(ms>>(uint(i)*bitsInChar))&(1<<bitsInChar-1)])
	}
	var acc uint64
	var bits uint
	for _, b := range buf {
		acc = acc<<8 | uint64(b)
		bits += 8
		for bits >= bitsInChar {
			bits -= bitsInChar
			out = append(out, alphabet[(acc>>bits)&(1<<bitsInChar-1)])
		}
	}
	return string(out), nil
}

// Must is ID for a constant prefix: it panics on a programming error (a bad prefix or a failed entropy source).
func (g *Generator) Must(prefix string) string {
	id, err := g.ID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}

// ReceiptID satisfies execution/controller.IDs.
func (g *Generator) ReceiptID() string { return g.Must("rcp") }
