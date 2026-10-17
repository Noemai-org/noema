// Package crypto holds HMAC-SHA-256 signing and verification for grants and confirmations, and RFC 8785 JCS canonicalisation (SoT §43; PERD §23 and §27).
//
// May import: the standard library.
//
// Must never: hold a key outside the core, log a key, or use a float type for money.
//
// This is an empty boundary package created by PLT-01. Its contents arrive with the tasks named in
// the README of the PERD §64.2 tree; nothing is built here yet.
package crypto
