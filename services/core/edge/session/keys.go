package session

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
)

// ParsePublicKeyPEM reads one PKIX public key from PEM and accepts only an ECDSA P-256 key, the only key ES256 uses. Anything else, more
// than one block, or trailing bytes is an error: a key file is read as a whole or not at all.
func ParsePublicKeyPEM(b []byte) (*ecdsa.PublicKey, error) {
	blk, rest := pem.Decode(b)
	if blk == nil || blk.Type != "PUBLIC KEY" {
		return nil, errors.New("session: not a PEM public key")
	}
	if len(rest) != 0 {
		return nil, errors.New("session: trailing data after the public key")
	}
	k, err := x509.ParsePKIXPublicKey(blk.Bytes)
	if err != nil {
		return nil, fmt.Errorf("session: public key: %w", err)
	}
	pub, ok := k.(*ecdsa.PublicKey)
	if !ok || pub.Curve != elliptic.P256() {
		return nil, errors.New("session: the key is not an ECDSA P-256 public key")
	}
	return pub, nil
}

// ParseRole returns the role a name denotes, or false for a name that is not one of the roles a session may carry.
func ParseRole(name string) (Role, bool) {
	r := Role(name)
	return r, knownRole(r)
}
