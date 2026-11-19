# trust/grants/keyfiles

**What it does.** Loads the Decision Grant HMAC key ring from the secrets directory the deploy step mounts (SEC-09). Before this, nothing in the core built a `grants.Keys` from files.

**REQs.** REQ-SEC-005 (rotation with two active keys), INV-03, INV-29. Updated 18 Nov 2026.

**How it works.**
- One key is one file, `grant.hmac.<version>.key` (version 1 to 999999, no leading zero), holding the 32-byte secret as 64 lower-case hex characters and an optional final newline. The key's identifier, which a grant's signature names, is `grant.hmac.<version>`.
- `Load(dir, Options{MaxActive})` reads every `*.key` file of the directory (any other file is ignored; a `*.key` file that is not exactly a grant key file is an error). Each file is opened without following a link and without blocking on a special file, and must be a regular file owned by the process's user with mode 0400, the same convention as `execution/signing.LoadRing`.
- `Ring` puts the highest version first (it signs; either verifies) and refuses no key, more keys than `MaxActive`, `MaxActive` below one, and whatever `grants.NewKeys` refuses (a repeated ID, a short secret, two keys with one secret). `MaxActive` has no default: the rotation overlap is a decision.
- A retired key is simply absent from the directory, so a grant signed under it is refused.
- An error names the file and never its content; a `Parsed` prints as `key material redacted` through every `fmt` verb.
- On a system where the owner and mode of a file cannot be checked (not unix), `Load` returns `ErrUnsupported` and loads nothing.

**Tests.** `go test ./trust/grants/keyfiles/...` from `services/core/`. `keyfiles_test.go` runs everywhere; `read_unix_test.go` opens real files (mode, link, pipe, directory, size, count) and runs on unix, which is where CI runs. Every key is SYNTHETIC (AGENT-25).

**Not here yet.** Wiring into `cmd/core` (the composition root is EL's): which environment setting names the directory, and the Compose mount. The `KEY_ROTATED` Record event needs the Record writer (see `trust/grants/doc.go`).
