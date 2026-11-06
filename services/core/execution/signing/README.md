# execution/signing

**What it does.** Ed25519 signing for `core-execution`: the signing side of DEBT-01 Option B (W3, task EXE-07). Updated 3 Nov 2026.

**REQs.** REQ-AUD-006, REQ-SEC-005, from DEC-2026-11-01-receipt-and-record-signing (**Proposed**: Noemai-org/docs#80, head `a3196a6`). Nothing here is in force until the founders approve that record (GOV-28).

**Status.** Slice 1 of EXE-07: the signer alone. Not yet wired into the controller or the ledger, and it changes no existing behaviour.

**How it works.**
- `LoadRing(identity, dir, Options{MaxActive})` reads the keys of one identity from the secrets directory the deploy step mounts. A key file is a regular file (a link is refused) with mode `0400`, named `<identity>.ed25519.<version>.key`, holding the seed as 64 lower-case hex characters and a line feed. Any key file that is not exactly this is an error, and an error never repeats a file's contents. With no key there is no signer. At most `MaxActive` keys may be present: the rotation overlap, a policy number the caller reads from configuration (REQ-OPS-003; the lint keeps such numbers out of decision code).
- `Sign(tag, doc)` and `SignJCS(tag, bytes)` sign under the **newest** key. The message is the domain tag followed by the RFC 8785 bytes of the document without `sig` (D6). The result is the key ID (`core-execution.ed25519.3`), the 128-character hex signature and the exact bytes that were signed, which the ledger stores (D8). `SignJCS` accepts only bytes that are exactly canonical, with no repeated member at any depth, no float and no `sig`.
- `Verify(pub, tag, bytes, sig)` needs only a public key. `Ring.PublicKeys()` gives the public halves for the deploy step to register (D3a).
- A key or a ring prints as `signing.Key{core-execution.ed25519.3, key material redacted}` through every `fmt` verb, `slog`, and the JSON and text marshallers (D3).

**Tests.** `go test -race ./execution/signing/...` from `services/core/`. Every key is SYNTHETIC (AGENT-25): 32 copies of one byte, generated for the tests, never mounted and never registered. They prove: a signature verifies over exactly tag plus canonical bytes; a signature does not verify as another type, under another key, over any changed byte, or with any changed hex digit or a malformed value; the newest key signs; a third key, a key of another identity, a leading-zero or zero version, a wrong mode, a link, a short or non-hex or upper-case seed are refused; no print path shows a seed, the private key or the directory; non-canonical and repeated-member documents are refused; signing is deterministic. Each mutation (no mode check, no domain tag, the oldest key, no duplicate check, a key that prints its seed) is caught.

**Not here yet.** Controller and ledger wiring; the `signed_jcs` column and the database verification (W4, EXE-08); the golden-vector family `ed25519_signing_vectors`; the lint rule that `execution` and `outcome` cannot import each other's signer (`tools/lint`, a protected path); the key-material scan over migrations and fixtures (W1 test 19).
