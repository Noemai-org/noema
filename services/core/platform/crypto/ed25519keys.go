package crypto

import (
	"crypto/ed25519"
	"encoding/hex"
	"math/big"
)

// p is 2^255 - 19, the prime of the Ed25519 field.
var fieldPrime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

// smallOrderY are the y coordinates (little-endian, sign bit cleared) of the eight points of small order: y = 1 (the identity), y = p - 1
// (order 2), y = 0 (order 4) and the two y values of the order 8 points, each with both signs of x. Checked with the RFC 8032 reference
// arithmetic: eight times each of these points is the identity.
var smallOrderY = func() [][32]byte {
	var out [][32]byte
	for _, h := range []string{
		"0100000000000000000000000000000000000000000000000000000000000000",
		"ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
		"0000000000000000000000000000000000000000000000000000000000000000",
		"26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05",
		"c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a",
	} {
		var y [32]byte
		b, err := hex.DecodeString(h)
		if err != nil || len(b) != 32 {
			panic("crypto: bad small-order constant") // a constant of this file, checked by its test
		}
		copy(y[:], b)
		out = append(out, y)
	}
	return out
}()

// UsableEd25519PublicKey reports whether k is a 32-byte Ed25519 public key that a verifier may trust: not of small order and not a non-canonical encoding. A
// signature under a small-order key can verify for any message in Go's and OpenSSL's verifiers (libsodium refuses it), so such a key in the
// key source would let anyone forge a receipt. This is defence in depth: the key source is a table the deployment controls (REC-08/W4).
func UsableEd25519PublicKey(k ed25519.PublicKey) bool {
	if len(k) != ed25519.PublicKeySize {
		return false
	}
	var y [32]byte
	copy(y[:], k)
	y[31] &= 0x7f // clear the sign bit of x
	le := make([]byte, 32)
	for i := range le { // big.Int wants big-endian
		le[31-i] = y[i]
	}
	if new(big.Int).SetBytes(le).Cmp(fieldPrime) >= 0 {
		return false // y at or above p is not a canonical encoding
	}
	for _, bad := range smallOrderY {
		if y == bad {
			return false
		}
	}
	return true
}
