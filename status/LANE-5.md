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

---

# Session 2026-11-24 · LANE-5 slice 3 (C2)

## Session summary
- What changed (PR links): noema#950, `services/core/outcome/request` (new): `request.Build(Inputs) (verifier.Request, error)`, the Outcome request builder (A547, A620, A635; all interim). Red `f5c0cad2e` (tests only); green follows the red CI read.
- Why: lane 5 slice 3. The write path's Verifier port (`orchestrator/portadapters/verifier`) asks a `Requests` source for the verification request of a grant. Nothing built it. This is the pure part of that source; the Record reader that supplies its inputs is slice 4.
- What it does: from the receipt row's facts, the stored plan's facts, what the caller expects (tenant, intent, intent hash, plan hash), the pack's outcome definition and the capability's retry safety, it returns the request or a typed refusal. It reads nothing, writes nothing, owns no clock and decides no verdict.
- Refusals (each a fixed text naming no value): receipt unsigned (no document, key id or signature); signature not found VALID by the caller's check; state no Execution produces; ACKNOWLEDGED with no external acknowledgement (an HTTP 200 alone is not an outcome); tenant, intent, intent hash or plan hash differing between the receipt, the plan and the expected values; no plan step, or two, of the executed capability; a binding the inputs cannot supply (an empty value is never bound); a missing identifier or definition.
- INV-07: a receipt that is not ACKNOWLEDGED binds nothing from the acknowledgement, whether or not the row carries one, so the verifier cannot reach ACHIEVED on it. `QueryableByKey` is true only for the retry safety QUERYABLE_BY_KEY.
- Not built, with owners: the pack's declaration of which plan param feeds which predicate variable (the pack has none today, so the caller passes `Bindings`; Architect/C2 pack follow-up); a pending condition (`Definition.Pending` is nil: the outcome-definition contract has no member for it); the signature check itself (the caller runs `receiptsig` and passes the result: the verifier checks again); the mapping of the Execution receipt row and the plan row into `Inputs` (EL's composition, with slice 4's reader).
- Risks, and how they are contained: a binding mapping wrong in the composition gives a verifier verdict of UNKNOWN (an unbound predicate), never a success, because the verifier reads the target itself.
- Tests added: `request_test.go`: valid build, retry-safety mapping, determinism over 1,000 seeded shuffles, unsigned and unverified receipts, the success claim with no acknowledgement, tenant/intent/hash mismatches, states, unknown receipts binding nothing from the acknowledgement, unsupplied bindings, the plan step, incomplete input, and an import guard (no clock, no I/O, no other power).
- Regression cases added: the same file.
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: slice 4, the Record reader for the receipt, the plan and the SLA (tenant-bound, a missing row is not found); then EL maps its rows to `request.Inputs` and implements `verifierport.Requests`.
- Open questions (owner): where the pack declares the predicate-variable sources (Architect); whether a pending condition belongs in the outcome definition (Architect).
- Files to read first: `services/core/outcome/request/request.go`, `services/core/outcome/verifier/types.go`, `services/core/orchestrator/portadapters/verifier/port.go`.
- Do not touch: `outcome/verifier` (OUT-01), `trust/`, `execution/`.
