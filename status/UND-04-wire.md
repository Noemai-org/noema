# Status · UND-04 · wire the Understanding half (wiring slice 1)

Date: 2026-11-19 · Owner: Core Engineer 2 (agent session), at the Architect's rulings A320, A329, A330, A332, A333, A347 and A356 (docs records `DEC-2026-11-18-architect-rulings-a317-a328`, `...-a329-a340` and the interim record of A341 to A357 on `docs/OPS-09-rulings-a341-a357`). Branch `feat/UND-04-wire-understanding-slice1`, from `origin/main` at `8e2f8e89`. **Nothing is committed or pushed; no PR is open.** (The branch name `feat/UND-04-wire-understanding` is held by an old, merged worktree of another session; the push can name it: `git push origin HEAD:feat/UND-04-wire-understanding`.) Do not append to `status/UND-04.md`: it is another session's file.

## Task card (PERD 61.4, short)

- Task: UND-04, `wire_understanding.go` slice 1: the Understanding half of the core's composition root.
- Requirement IDs: REQ-INT-001, REQ-INT-002, REQ-INP-002, REQ-AUD-001, REQ-SEC-004, REQ-OPS-003; SoT INV-02, INV-12, INV-14, INV-17, INV-29, AGENT-10, AGENT-13, AGENT-16.
- Allowlist (touched): `services/core/cmd/core/wire_understanding.go` and `wire_understanding_test.go` (new); `services/core/platform/packload/` (new: code, tests, README); this file; one line in `status/README.md`.
- Not touched: `contracts/`, `migrations/`, `packs/` (the manifest of noema#734 is read, not changed), `.github/`, `deploy/`, `CODEOWNERS`, `compose.go`, `recorder.go`, `main.go`, the SoT, the PERD, the plan. No new dependency.

## What is built

1. **`platform/packload`** (new, standard library only): `Load(dir, manifest, maxBytes)` reads the manifest (A333) and every file it lists through `os.Root`, and refuses the whole pack on: a SHA-256 that differs (a file changed after CI), a path that is not plain (absolute, `..`, `\`, a drive letter, a hidden or empty component, a control character), a symlink in any component (including one that points inside the directory), a file that is missing, not regular or over the bound, a file of a listed kind that is not listed, a manifest that is not exactly `manifest.v1`, an id the file does not carry. It names no vertical, folder or file (INV-14); where a kind lives is read from where the manifest put the listed file. README in the package.
2. **`cmd/core/wire_understanding.go`**: `buildUnderstanding(env, clk, recorder) (UnderstandingParts, error)`. From the verified pack bytes only (one read): the schema gate (`understanding.NewGate`, bound `NOEMA_MODEL_MAX_RESPONSE_BYTES`, A333), the required slots (`RequiredFromPack`), the rule set (`reconcile.ParseRuleSet`), the evidence mapping (`mapping.Parse`, expected id from the manifest, sources from the pack). One Interpreter per tenant, each over that tenant's orchestrator login: the pools and signing ring of the Recorder (no second set of pools). The compile call is bounded by `NOEMA_COMPILE_TIMEOUT_MS`; every save and every gate-refusal or override append by `NOEMA_STORE_TIMEOUT_MS`. An unknown tenant is `ErrNoInterpreterForTenant`, with no fallback and no tenant id in the text. The port is `interpreterport.Port` with an adapter-local session resolver.
3. **`authn_ref` (A149, A333).** `orchestrator.Session` is **not changed**. `edgeSessions.Resolve` takes the whole `understanding.Session` (principal, tenant, `AuthnRef`, principal type, issuer) and the actor kind from the verified edge session in the request's context (`session.FromContext`), and only when that session equals the orchestrator Session in tenant, principal and role. No session in the context, or a different one, is `ErrSessionUnavailable`.

## Settings (no default in code; every one is required when the half is built; a missing or invalid one is a fixed error that names it and never repeats the value)

| Setting | Meaning | Source |
|---|---|---|
| `NOEMA_PACK_DIR` | the one mounted directory the manifest's paths are relative to | A320, A356 |
| `NOEMA_STORE_TIMEOUT_MS` | bound of each save and each gate-refusal or override append | A320, A356 (1000) |
| `NOEMA_COMPILE_TIMEOUT_MS` | bound of the compile call | A320, A356 (3000) |
| `NOEMA_SEAL_TIMEOUT_MS` | bound of sealing an evidence snapshot; validated here, carried in `UnderstandingParts.SealTimeout`, used by the Evidence read-and-seal wiring | A320, A356 (1000) |
| `NOEMA_PACK_MANIFEST` | **new (A330)**: the manifest's path, relative to `NOEMA_PACK_DIR` | silence 2 |
| `NOEMA_PACK_MAX_BYTES` | **new (A330)**: bound of the manifest and of each listed file, and `mapping.Limits.MaxBytes` | silence 3 |
| `NOEMA_PACK_MAX_DEPTH` | **new (A330)**: `mapping.Limits.MaxDepth` | silence 3 |

Reused, unchanged: the five `NOEMA_MODEL_*` settings (mutual TLS, the response bound), `NOEMA_ORCHESTRATOR_DSN_DIR`, `NOEMA_ORCHESTRATOR_KEY_DIR`, `NOEMA_ORCHESTRATOR_KEY_MAX_ACTIVE` (through the Recorder). `NOEMA_TRUST_DSN_DIR` and `NOEMA_RECORD_READ_DSN_DIR` are not read by this half.

## Silences (A332: the author takes the safer reading; each stands unless Codex or SR objects)

1. **`orchestrator.Session` is not extended.** The safer reading is an adapter-local resolver. It works only while the Runner calls the port with a context that still carries the edge's verified session (the in-request path, and a goroutine started with `context.WithoutCancel`). If the Runner is ever run from a context that does not (a Temporal activity, STM-04), `orchestrator.Session` must carry `AuthnRef`, `PrincipalType` and `AssertedBy`, which is a contract change for EL and the Architect. Until then such a call fails closed with `ErrSessionUnavailable`.
2. `NOEMA_PACK_MANIFEST` is a setting, not a constant: a fixed `fintech/manifest.v1.json` in core would name a vertical (INV-14); A320's "fixed relative paths" is read as "fixed by the deployment".
3. `NOEMA_PACK_MAX_BYTES` and `NOEMA_PACK_MAX_DEPTH`: `mapping.Parse` and every read need bounds that the caller supplies (the parser's own comment: no literal in the package). The values are IL's, in Compose; the largest pack file today is 64069 bytes.
4. **The real FactsSource is not built.** On `main` there is only the interface (`explanation.FactsSource`) and the pure core `explanation/factsource`; no implementation reads the Record and the sealed snapshot. It needs its own task, and a zone, a currency table and an owner-queue table that are not settings yet.
5. `NOEMA_SEAL_TIMEOUT_MS` is required and validated, but nothing in this half consumes it; `UnderstandingParts.SealTimeout` carries it to the Evidence lane.
6. The unlisted-file rule (a file of a listed kind that is not listed) is read from the listed file's own location (folder owns its extension, the pack directory owns a `_v<N>` family), because the folders of `tools/packcheck` name a vertical. `packcheck` stays the CI check of where a kind belongs.
7. The actor kind is the session's principal type (`customer`, `operator`, `auditor`, `approver`); the resolver's role comparison is strict equality, so an orchestrator Session with an empty role is refused.
8. `buildUnderstanding` is not plugged into `buildComponents` (compose.go is EL's file and noema#722/#725/#721 are open); the plug-in is one call in the `recorder != nil` block and `c.Ports.Interpreter = parts.Interpreter`.
9. The redundant check `ruleSet.ID() == manifest id` was dropped: `packload` already compares the id the file carries, and the mapping parser compares its rule set.

## Tests (REQ-INT-001, REQ-INP-002, REQ-AUD-001, REQ-OPS-003; INV-12, INV-14; AGENT-13, AGENT-16)

- `platform/packload/packload_test.go`: a verified pack loads; a pack file changed after the manifest (each of the three, one byte) is `ErrHash`; every id other than the manifest's is `ErrID`; every spelling of an escaping path is `ErrPath` and is not echoed; a symlink in each position is `ErrLink`; a missing, directory or oversize file is `ErrFile`; an unlisted file of a listed kind is `ErrUnlisted`; the manifest's closed shape; any names load.
- `cmd/core/wire_understanding_test.go`: every setting required (twelve deleted one at a time); invalid bounds (fifteen spellings, five settings); the real pack through its real manifest; the A333 acceptance cases through the build; a real PostgreSQL with the real session middleware and a real mutual-TLS model service: a turn is compiled, admitted and saved as the session's tenant; a gate refusal is recorded; an identity override is recorded; no verified session and every mismatch call nothing; an unknown tenant is refused; a model service that never answers is abandoned at the compile bound; a really locked table makes the save and the refusal append give up at the store bound (EC-E16); a model service of another CA is never called.
- Red first: the tests were written against compile-only stubs. The first red run failed on the stub's `not implemented` for 16 tests in `platform/packload` and 17 in `cmd/core` (for example `wire_understanding_test.go:268: the complete settings do not build: cmd/core: not implemented`). The final test set against the same stubs fails 18 and 23 tests; the stub files are the first commit.

## Mutants (hand mutants on the non-test code; each shown killed, then restored)

1. `packload.go`: Load: a zero bound is accepted: killed
2. `packload.go`: pathRe: a leading dot (hidden, ..) is allowed: killed
3. `packload.go`: pathRe: no end anchor: killed
4. `packload.go`: readPlain: path length bound off by one: killed
5. `packload.go`: digestRe: 63 characters allowed: killed
6. `packload.go`: digestRe: upper case allowed: killed
7. `packload.go`: Load: digest comparison inverted: killed
8. `packload.go`: readPlain: no link check: killed
9. `packload.go`: readPlain: only the last component is examined: killed
10. `packload.go`: readPlain: a file exactly at the bound refused: killed
11. `packload.go`: readPlain: the bound cannot be exceeded: killed
12. `packload.go`: noUnlisted: any file of any family counts: killed
13. `packload.go`: Load: the manifest is not exempt: killed
14. `packload.go`: Load: listed files not recorded: killed
15. `packload.go`: noUnlisted: folders are not searched below the first level: killed
16. `packload.go`: noUnlisted: every file of the folder counts: killed
17. `packload.go`: noUnlisted: the pack directory itself is not the root case: killed
18. `packload.go`: checkIDs: the mapping's rule set is not compared: killed
19. `packload.go`: checkIDs: the mapping id is not compared: killed
20. `packload.go`: checkIDs: the rule set id is not compared: killed
21. `packload.go`: manifest: a zero version: killed
22. `packload.go`: manifest: the label is not checked: killed
23. `packload.go`: manifest: note bound off by one: killed
24. `packload.go`: manifest: an empty note: killed
25. `packload.go`: manifest: repeated members allowed: killed
26. `packload.go`: manifest: unknown members allowed: killed
27. `packload.go`: manifest: data after the document allowed: killed
28. `packload.go`: manifest: no pack file needed: killed
29. `packload.go`: manifest: two rule sets allowed: killed
30. `packload.go`: manifest: a path listed twice: killed
31. `packload.go`: manifest: unknown kinds allowed: killed
32. `packload.go`: manifest: expected id length unbounded: killed
33. `packload.go`: idRe: version 0 allowed: killed
34. `packload.go`: manifest: the manifest id is not checked: killed
35. `packload.go`: Load: every file is returned as the pack: killed
36. `packload.go`: Error: the kind is never named: killed
37. `packload.go`: manifest: kinds are not counted: killed
38. `wire_understanding.go`: a recorder with no tenant login accepted: killed
39. `wire_understanding.go`: no clock accepted: killed
40. `wire_understanding.go`: no manifest setting accepted: killed
41. `wire_understanding.go`: a zero or a leading zero accepted: killed
42. `wire_understanding.go`: an overflowing number accepted: killed
43. `wire_understanding.go`: a duration over the range accepted: killed
44. `wire_understanding.go`: pack bounds unbounded above: killed
45. `wire_understanding.go`: gate bound unbounded above: killed
46. `wire_understanding.go`: no model client accepted: killed
47. `wire_understanding.go`: pack reference without @: killed
48. `wire_understanding.go`: gate bound is 1: killed
49. `wire_understanding.go`: compile bound is the store bound: killed
50. `wire_understanding.go`: recorder bound is the compile bound: killed
51. `wire_understanding.go`: store bound is the compile bound: killed
52. `wire_understanding.go`: seal bound is the store bound: killed
53. `wire_understanding.go`: mapping depth bound is the byte bound: killed
54. `wire_understanding.go`: file bound is far larger: killed
55. `wire_understanding.go`: compile call has no bound: killed
56. `wire_understanding.go`: unknown tenant has no refusal: killed
57. `wire_understanding.go`: session absence not checked: killed
58. `wire_understanding.go`: tenant of the session not compared: killed
59. `wire_understanding.go`: principal of the session not compared: killed
60. `wire_understanding.go`: role of the session not compared: killed
61. `wire_understanding.go`: actor kind is always customer: killed
62. `wire_understanding.go`: authn_ref is a constant: killed
63. `wire_understanding.go`: asserted_by is a constant: survived: equivalent (the verifier accepts one issuer, so the constant equals the session's value)
64. `wire_understanding.go`: principal type is a constant: killed
65. `wire_understanding.go`: sources: empty and repeated ids accepted: killed
66. `wire_understanding.go`: sources: repeated ids accepted: killed
67. `wire_understanding.go`: sources: none accepted: killed
68. `wire_understanding.go`: sources: ids not remembered: killed
69. `wire_understanding.go`: store pool and appender not checked: killed
70. `wire_understanding.go`: bounds are in microseconds: killed

70 mutants: 69 killed, 1 equivalent.

## Local results and what could not run

- `gofmt -l`, `go vet ./cmd/... ./platform/...`, `python3 -I tools/lint/run.py` (lint OK), `go test -race -count=1 ./cmd/core/ ./platform/packload/`: green with a local stub of the signature verifier (below).
- **Could not run as written here:** the Record verifies each event's signature through `pgsodium`, which this machine does not have (the existing `intentstore` tests fail the same way). Four tests of `cmd/core` append signed Record events and fail locally with `schema "pgsodium" does not exist`: `TestATurnIsCompiledAdmittedAndSavedAsTheSessionsTenant`, `TestAProposalTheGateRefusesIsRecordedAndNotSaved`, `TestAProposalThatNamesAnotherPrincipalIsSavedAsTheSessions`, `TestTheSavedIdentityIsTheVerifiedSessionsInEveryMember`. To exercise the wiring I ran the whole package once with a **temporary, uncommitted** stub in `internal/testpg` (a `pgsodium.crypto_sign_verify_detached` that returns true), then reverted it (`git status` shows no change to `testpg`). That run proves the wiring and the bounds; **it does not prove the signature path**: the real run is CI's, where the extension is built in. The registered key is the one of the mounted ring (`core-workflows.ed25519.1`), as `recordsig` registers it.

## Risks, and how they are contained

- The resolver fails closed when the context carries no session (silence 1).
- Tests that append Record events were only run under the stub verifier here (above).
- No pool is opened and nothing is dialled in `buildUnderstanding`.

## Pre-work for the next agent

- Exact next step: EL plugs `parts.Interpreter` into `Ports.Interpreter` in `buildComponents` (inside `recorder != nil`), and the Trust and Evidence lanes take `RuleSet`, `Mapping` and `SealTimeout` from `UnderstandingParts`. Then the real FactsSource (silence 4) and `/readyz`.
- Open questions (owner): silence 1 (EL, the Architect); the values of `NOEMA_PACK_MANIFEST`, `NOEMA_PACK_MAX_BYTES`, `NOEMA_PACK_MAX_DEPTH` in Compose (IL).
- Files to read first: `services/core/platform/packload/README.md`, `services/core/cmd/core/wire_understanding.go`, `services/core/orchestrator/portadapters/interpreter/port.go`.
- Do not touch: `orchestrator.Session` without the Architect, `packs/fintech/manifest.v1.json` (a change needs its digests updated in the same pull request, A333).

## Messages

| Date (UTC) | From | To | Purpose | What the receiver used it for |
|---|---|---|---|---|
| 2026-11-19 | Engineering Lead (via the orchestrating session) | C2 | Build the Understanding half of the composition root, slice 1 | This change |
