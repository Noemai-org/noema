// Package numbers rewrites Tushe's test account numbers (9999SSMMMM) to stable synthetic numbers before any system
// under test sees them, and back again when the system sends one to Tushe (REQ-ENV-005, DEC-2026-10-12 D4).
// Otherwise a system could learn that 999904... means "will be reversed", and the environment would no longer force
// a real investigation.
//
// The mapping is built from a keyed, stateless bijection on the 10-digit numbers: an 8-round Feistel network over
// two 5-digit halves, with HMAC-SHA-256 as the round function. A test number maps by cycle-walking until the
// result no longer starts with 9999 (see Synthetic), so the same Tushe number always gives the same synthetic number for one key, across
// runs, and the proxy needs no table. A synthetic number is invertible only if it was issued: the inverse checks
// that the walk forward from the recovered test number lands on it, and leaves any other number untouched.
// One consequence of a stateless scheme: about 1 in 10,000 random 10-digit numbers is a synthetic number.
package numbers

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	half   = 100000      // 10^5: each Feistel half holds five digits
	space  = half * half // 10^10 numbers of ten digits
	rounds = 8
	// maxWalk bounds the cycle walk. A walk takes more than one step with probability 1e-4 per step.
	maxWalk = 4
	// MinKeyBytes is the shortest key accepted.
	MinKeyBytes = 32
)

// Mapper rewrites numbers under one key.
type Mapper struct{ key []byte }

// New returns a Mapper. The same key gives the same mapping in every process and every run.
func New(key []byte) (*Mapper, error) {
	if len(key) < MinKeyBytes {
		return nil, fmt.Errorf("the number key must be at least %d bytes", MinKeyBytes)
	}
	return &Mapper{key: append([]byte(nil), key...)}, nil
}

func (m *Mapper) round(i int, r uint64) uint64 {
	mac := hmac.New(sha256.New, m.key)
	var buf [9]byte
	buf[0] = byte(i)
	binary.BigEndian.PutUint64(buf[1:], r)
	mac.Write([]byte("hostility-proxy/numbers/v1\x00"))
	mac.Write(buf[:])
	return binary.BigEndian.Uint64(mac.Sum(nil)[:8]) % half
}

func (m *Mapper) enc(n uint64) uint64 {
	l, r := n/half, n%half
	for i := 0; i < rounds; i++ {
		l, r = r, (l+m.round(i, r))%half
	}
	return l*half + r
}

func (m *Mapper) dec(n uint64) uint64 {
	l, r := n/half, n%half
	for i := rounds - 1; i >= 0; i-- {
		l, r = (r+half-m.round(i, l))%half, l
	}
	return l*half + r
}

// IsTestNumber reports whether run is a Tushe test account number: exactly ten digits starting 9999.
func IsTestNumber(run string) bool { return len(run) == 10 && run[:4] == "9999" && allDigits(run) }

func allDigits(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return len(s) > 0
}

func parse(run string) uint64 {
	var n uint64
	for i := 0; i < len(run); i++ {
		n = n*10 + uint64(run[i]-'0')
	}
	return n
}

func isTest(n uint64) bool { return n/1000000 == 9999 }

// Synthetic maps a test number to its synthetic number.
//
// The six digits after 9999 are embedded as the small number 0000xxxxxx, enciphered over all ten digits, and the
// cipher is applied again for as long as the result is itself a test number (cycle-walking over the numbers that
// are not test numbers). Cycle-walking a permutation over a subset permutes that subset, so two test numbers never
// share a synthetic number, and the results are scattered over the whole ten-digit space instead of sitting in one
// block. ok is false only if the bounded walk finds no exit (probability about 1e-16): the caller must then not
// deliver the number.
func (m *Mapper) Synthetic(testNumber string) (string, bool) {
	if !IsTestNumber(testNumber) {
		return "", false
	}
	y := parse(testNumber[4:])
	for i := 0; i < maxWalk; i++ {
		y = m.enc(y)
		if !isTest(y) {
			return fmt.Sprintf("%010d", y), true
		}
	}
	return "", false
}

// Real maps a synthetic number back to the test number it was issued for. A number that was not issued is not
// rewritten (ok false).
func (m *Mapper) Real(synthetic string) (string, bool) {
	if len(synthetic) != 10 || !allDigits(synthetic) || IsTestNumber(synthetic) {
		return "", false
	}
	y := parse(synthetic)
	for i := 0; i < maxWalk; i++ {
		y = m.dec(y)
		if !isTest(y) {
			if y >= 1000000 {
				return "", false // outside the embedded range: not a number this proxy issued
			}
			real := fmt.Sprintf("9999%06d", y)
			if back, ok := m.Synthetic(real); ok && back == synthetic {
				return real, true
			}
			return "", false
		}
	}
	return "", false
}

// Direction says which way a rewrite runs.
type Direction int

const (
	// ToSynthetic is for responses: test numbers become synthetic ones.
	ToSynthetic Direction = iota
	// ToReal is for requests: synthetic numbers become the test numbers Tushe knows.
	ToReal
)

// Stats counts what a rewrite did.
type Stats struct {
	Rewritten  int // numbers replaced
	Unmappable int // test numbers with no synthetic number; replaced by ten zeros so none is delivered
}

// RewriteText rewrites every maximal run of ASCII digits that is exactly ten digits long. A longer or shorter run,
// such as an amount in minor units written as a string of 12 digits, is never touched.
func (m *Mapper) RewriteText(s string, dir Direction) (string, Stats) {
	var st Stats
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] < '0' || s[i] > '9' {
			out = append(out, s[i])
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		run := s[i:j]
		switch {
		case dir == ToSynthetic && IsTestNumber(run):
			if syn, ok := m.Synthetic(run); ok {
				run = syn
				st.Rewritten++
			} else {
				run = "0000000000"
				st.Unmappable++
			}
		case dir == ToReal && len(run) == 10 && !IsTestNumber(run):
			if real, ok := m.Real(run); ok {
				run = real
				st.Rewritten++
			}
		}
		out = append(out, run...)
		i = j
	}
	return string(out), st
}

// ContainsTestNumber reports whether any maximal digit run in s is a test number. The proxy checks every outgoing
// body with it after rewriting, so a number that slipped through is a refused response, not a leak.
func ContainsTestNumber(s string) bool {
	for i := 0; i < len(s); {
		if s[i] < '0' || s[i] > '9' {
			i++
			continue
		}
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if IsTestNumber(s[i:j]) {
			return true
		}
		i = j
	}
	return false
}

// ErrNoKey is returned when no key is configured.
var ErrNoKey = errors.New("no number key configured")
