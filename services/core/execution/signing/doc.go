// Package signing: Ed25519 signing for core-execution, the signing side of DEBT-01 Option B (W3, task EXE-07).
//
// Implements: REQ-AUD-006, REQ-SEC-005 (DEC-2026-11-01-receipt-and-record-signing, Proposed: D3, D4, D5, D6, D8).
//
// May import: platform/crypto and the standard library.
//
// Must never: hold a private key in PostgreSQL, a log, a trace, a fixture or the model service; read a key that is not a 0400 regular
// file named <identity>.ed25519.<version>.key; sign a document that is not canonical, repeats a member name or already carries sig;
// import Outcome's signer (a lint rule, W1 test 12).
//
// This slice is the signer alone. Wiring it into the controller and the ledger (receipt signatures, signed_jcs), the database
// verification (W4, EXE-08) and the golden vectors follow.
package signing
