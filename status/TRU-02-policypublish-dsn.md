# Status · TRU-02 · `cmd/policypublish`, the DSN file through one descriptor (A509)

Follow-up of noema#846 (`cmd/migrate`, Codex's P2) and noema#850 (A474). Ruling A509 (interim, `DEC-2026-11-21-architect-rulings-a507-a517`): "DSN files get the same key-file checks as key files: regular, owned by the effective user, no group or other bits, no link, read through one open descriptor."

## Session 2026-11-22 · TRU-02 (C2, fork)

### Session summary
- What changed (PR links): noema#878. `connect` reads the DSN file with `keyfiles.ReadFile` (the shared reader of `trust/grants/keyfiles`, on main): one open descriptor, `O_NOFOLLOW|O_NONBLOCK`, a regular file owned by the effective user, mode 0400, bounded read. `os.Lstat` and `os.ReadFile` are gone from `main.go`. The refusal is the command's existing one (exit 2, the same fixed sentence).
- Why: a path check followed by a read by path leaves a gap, a followed link and a world-readable credential file; A509.
- Risks, and how they are contained: a DSN file of any mode but 0400 is now refused (0600 included): IL writes 0400 (A509), and the test helper now does. This follows the key-file rule exactly; if the Architect meant 0600 to pass, it is one constant. Off unix the shared reader refuses.
- Tests added (REQ-SEC-001; INV-12, AGENT-13): `dsnfile_test.go`: link, pipe (no block), 11 modes, oversized file, foreign owner (root only, otherwise skipped with its reason), the refusal equals a missing file's, no DSN/path/login in any output, nothing written, and a source test that `main.go` calls no `os.Lstat`, `os.Stat` or `os.ReadFile`.
- Regression cases added: the tests above.
- Debt entries: none. Conflicts raised: none.
- Not done: `cmd/migrate` has its own copy of the same checks and allows 0600; switching it to the shared reader is a one-line follow-up. EL's noema#810 (the shared reader for DSN files) was not readable from this session (404); if it adds another reader, `policypublish` should switch to it.

## Pre-work for the next agent
- Exact next step: Codex and SR read; the Architect confirms mode 0400 exactly (not 0600).
- Files to read first: `services/core/cmd/policypublish/main.go` (`connect`), `services/core/trust/grants/keyfiles/read_unix.go`.
- Do not touch: the publisher identity (A474), `trust/grants/keyfiles`.
