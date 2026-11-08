# record/export

Date: 2026-11-08 · Task: REC-09 (ruling A109, plan v1.13) · Owner: C1 · Reviewer: EL, and the Security Reviewer on what it exposes · Status: in review (Draft, stacked on noema#249)

## What it does

`export.AuditRecordOf(ctx, db, Session, correlationID)` turns one correlation of one tenant's Record into the **neutral `audit_record`** of `benchmark-arm-result.v1`: per step the time, the step name, who acted, the evidence it rests on, a digest of the inputs and the outputs, the decision and the outcome, in order, and a digest of the whole. The Noema arm runner calls it after a case; the scorer reads only the result, never the Record (INV-22).

It is a **library function, not an HTTP route** (ruling A109, condition 1). If a route is ever wanted it first gets a row in the control-plane matrix (docs#134).

Implements: REQ-AUD-003, REQ-BEN-001, REQ-BEN-009. SoT: INV-11 (a chain that does not verify is refused), INV-12 (the tenant is the session's), INV-22 (nothing from the oracle or the held-out set).

## How

1. The session's tenant must be set (`ErrNoSession`). The tenant is a field of `Session`, never a separate argument.
2. One read-only REPEATABLE READ transaction reads the tenant's whole chain through `chain.ReadChain` (`record_read_chain_t`, the one read path into the Record: the database takes the tenant from the login).
3. The first event's `tenant_id` must be the session's (`ErrTenantMismatch`). A handle logged in as another tenant is refused here, not mistaken for a broken chain.
4. The chain is verified from genesis (`chain.VerifyRows`). A break anywhere in the tenant's chain, in this case or another, is `ErrIntegrity` with the `chain.BreakError`; nothing is exported.
5. The entries are built from those same rows: what is exported is exactly what was verified.

## The form (from the scoring side, noema#355)

- One entry per Record event of the case, in chain order. `seq` is renumbered 1..n per case. The 13 system-level event types (`table.go`, `leftOut`) are not entries. An event type with no row is `ErrUnmapped`; a test fails when the contract adds a type.
- `step` is neutral and many-to-one (`table.go`). `actor` is a pure function of the event type, the emitter and the principal kind (`neutralActor`).
- `outputs_digest` is the event's own `payload_hash`. `inputs_digest` is the digest of `[payload_hash of the previous exported entry]`, and of `[]` for the first, so the export is a linked chain.
- `evidence_ids` are `tc_1`, `tc_2` ... by `EVIDENCE_READ` events in chain order, listed by the `READ_EVIDENCE`, `DECIDE` and `EXPLAIN` steps (every read so far); other steps list `[]`.
- `decision` only on `DECISION_MADE` (`payload.decision_outcome`, one of the seven dispositions); `outcome` only on `VERDICT` (the verdict enum, nested under `verdict` as Outcome writes it, or flat). Anything else is `ErrUnprojectable`.
- The digest of the whole is the digest of the canonical JSON (RFC 8785) of the `entries` array alone. The worked example of noema#355 reproduces: `sha256:baae831e...7cb9`.
- No payload text and nothing the redactor masks leaves: only digests, ids, step names, labels and times.

## Constraints

- Imports only the standard library, pgx, `platform/crypto` and `record/chain` (a test fails on any other import).
- Writes nothing; no Record event is appended. The read is the only effect.
- A reconstruction from this export is reported as "consistent, not authenticated" until REC-08 is closed (ruling A40).

## Open

- Four points for the Benchmark Owner on noema#355 (`approver` as `operator`; whether `VERIFICATION_READ` is a `tc_n`; the fail-closed evidence case; the label keys).
- The Security Reviewer's exposure check before the PR leaves Draft (condition 3).

## Tests

```bash
cd services/core && go test ./record/export/
```

The tests need a PostgreSQL server (a started one via `NOEMA_TEST_DATABASE_URL`, or the binaries on the machine).
