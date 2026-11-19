package crypto

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"math/big"
)

// p is 2^255 - 19, the prime of the Ed25519 field.
var fieldPrime = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 255), big.NewInt(19))

// smallOrderY are the y coordinates (little-endian, sign bit cleared) of the eight points of small order: y = 1 (the identity), y = p - 1
// (order 2), y = 0 (order 4) and the two y values of the order 8 points, each with both signs of x. Checked with the RFC 8032 reference
// arithmetic: eight times each of these points is the identity.
var smallOrderHex = []string{
	"0100000000000000000000000000000000000000000000000000000000000000",
	"ecffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff7f",
	"0000000000000000000000000000000000000000000000000000000000000000",
	"26e8958fc2b227b045c3f489f2ef98f0d5dfac05d3c63339b13802886d53fc05",
	"c7176a703d4dd84fba3c0b760d10670f2a2053fa2c39ccc64ec7fd7792ac037a",
}

// errSmallOrderTable is returned when the small-order table does not load. The package then refuses every key (fail closed) instead of panicking at start-up.
var errSmallOrderTable = errors.New("crypto: the small-order key table did not load")

// loadSmallOrderY decodes the table. A constant that is not 32 bytes of hex refuses the whole table.
func loadSmallOrderY(hexes []string) ([][32]byte, error) {
	out := make([][32]byte, 0, len(hexes))
	for _, h := range hexes {
		b, err := hex.DecodeString(h)
		if err != nil || len(b) != 32 {
			return nil, errSmallOrderTable
		}
		var y [32]byte
		copy(y[:], b)
		out = append(out, y)
	}
	return out, nil
}

var smallOrderY, smallOrderErr = loadSmallOrderY(smallOrderHex)

// UsableEd25519PublicKey reports whether k is a 32-byte Ed25519 public key that a verifier may trust: not of small order and not a non-canonical encoding. A
// signature under a small-order key can verify for any message in Go's and OpenSSL's verifiers (libsodium refuses it), so such a key in the
// key source would let anyone forge a receipt. This is defence in depth: the key source is a table the deployment controls (REC-08/W4).
func UsableEd25519PublicKey(k ed25519.PublicKey) bool {
	if smallOrderErr != nil || len(k) != ed25519.PublicKeySize {
		return false // a table that did not load refuses every key
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
