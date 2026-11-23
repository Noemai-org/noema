# Task card · LANE-5 · compose the Execution Controller and the Outcome request builder (A547, A554)

Written by C1, 2026-11-23, from `DEC-2026-11-23-architect-rulings-a542-a553` (A547, A550) and `DEC-2026-11-23-architect-rulings-a554-a560` (A554), both interim. Template: `docs/poc/perd/templates/task-card.md`.

## Goal
Nobody composes the Execution Controller (ledger, dispatcher, receipt signer, Verifier, Bindings) or the Outcome request builder (A547). Lane 5 is C1's, EL integrates the ports, and it is on the G1 critical path. After it, `cmd/core` can build the write path's Execution side from file-mounted settings, and `/readyz` names what is still missing.

## REQ and SoT
REQ-EXE-001 to REQ-EXE-008 (spend and dispatch, E1 to E11), REQ-OUT-001 (the Outcome verdict), REQ-SEC-004 and REQ-SEC-005 (a login for each tenant, key rotation), REQ-AUD-001. SoT: INV-03, INV-04, INV-07, INV-12, INV-17, INV-29, AGENT-13, AGENT-16.

## Settings (A554; mirror the orchestrator and edge pattern; all four or none; absence is a named gap)
- `NOEMA_EXECUTION_DSN_DIR`: one file for each tenant, named by the tenant ID, holding that tenant's `execution_rw__<tenant>` login.
- `NOEMA_OUTCOME_DSN_DIR`: the same, `outcome_rw__<tenant>`.
- `NOEMA_EXECUTION_KEY_DIR` and `NOEMA_EXECUTION_KEY_MAX_ACTIVE`: the receipt signer's Ed25519 keys, identity `core-execution` (migration key announcements: purpose `receipt`, identity `core-execution`), `core-execution.ed25519.<n>.key`, mode 0400, owner 65532; the number of keys that may be mounted at once, a positive integer, no default.
- Files are read by the shared checked reader (A509: one open descriptor, regular, owned by the effective user, no group or other bits, no link). IL adds the mounts. If the code differs from A554, C1 raises a CONFLICT block.

## Inventory (what exists, from the code at `03c79640`)
- `execution/controller.Controller` has nine ports: `Clock`, `Keys` (KeyVerifier), `Receipts` (ReceiptSigner), `Grants` (GrantSource), `Bind` (Binder), `Reval` (Revalidator), `Ledger`, `Out` (Dispatcher), `IDs`.
- Implementations in the tree: `execution/ledger` (`Ledger`, `StoreBinder`), `execution/signing` (`Ring`, `LoadRing`), `execution/bridge` (`CaseDispatcher`, `CaseRevalidator`, `GrantExecutor`, `CaseReadBack`), `orchestrator/portadapters/verifier` (`Port`), `orchestrator/portadapters/executor`.
- Missing: the composition in `cmd/core` (`compose.go` builds Trust only), the four settings, the Outcome request builder, and the Record reader for the receipt, the plan and the SLA.

## Slices (each its own red-first PR, one branch, one allowlist)
1. **Settings and pools (this PR).** `cmd/core/execution.go`: `buildExecution` reads the four settings (all or none), loads the receipt ring for `core-execution`, makes one pool per tenant for the execution login and one for the outcome login, refuses a login of another power or another tenant, a tenant that has only one of the two logins, a bad key directory (never echoing a key) and an overlap that is not a positive integer. Nothing is dialled. Allowlist: `cmd/core/execution.go` and its test, `status/LANE-5.md`.
2. **The Execution side.** The Controller per tenant over the ledger, the dispatcher (`bridge`), the receipt signer, the Verifier and the Bindings, with `/readyz` naming each part that is still missing. Allowlist: `cmd/core` composition, `execution/*` only for a missing constructor.
3. **The Outcome request builder** from a receipt and the stored plan to the Outcome request, a pure function with its contract tests.
4. **The Record reader for the receipt, the plan and the SLA** (a read-only reader over the Record login), tenant-bound, a missing row is not found.

## Acceptance criteria (slice 1, each tied to a test)
- No setting: no parts, no error, a closer that does nothing. Some but not all: an error naming the missing setting.
- Each tenant file is one execution pool and one outcome pool; a tenant with only one is refused, naming the setting and the tenant.
- A login whose user is not `execution_rw__<tenant>` (or `outcome_rw__<tenant>`) is refused; no message holds a DSN, a login or key material.
- The key directory is checked by `signing.LoadRing`; an error never repeats a key. The overlap is a positive integer with no default.
- The closer closes every pool and is safe to call twice.

## Stop conditions
A mount, a login name or a key identity that differs from A554 is a CONFLICT block, not a guess. No new dependency, no migration, no contract change.

## Out of scope
The mounts (IL), the ports' integration in the composition root (EL), `EnvTrust*` settings, the Outcome verdict logic.
