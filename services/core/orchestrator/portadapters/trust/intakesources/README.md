# orchestrator/portadapters/trust/intakesources

Date: 2026-11-19 · Status: current (rulings A341 to A357 are relayed and not committed; nothing here cites one as authority). All data is SYNTHETIC.

The production readers behind three consumer-side ports of `trust/intakeadapters` (noema#747), package `intakesources`. Each reads as the tenant's own Trust login (`trust_rw`); the database binds the tenant (row-level security, migration 0026) and the queries also name it. None writes.

**Implements:** REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007, REQ-SEC-004; A306; INV-09, INV-11, INV-12, INV-14, INV-16, INV-19; AGENT-13, AGENT-16. **Never:** a write, a login that is not the tenant's (no fallback), a fact it did not read (an absence is never a negative fact), a default for a setting, a row, an ID, a tenant or a driver's text in an error, a clock.

| Part | Port | State |
|---|---|---|
| `Facts` (`NewFacts`) | `intakeadapters.FactSource` | Built for the facts the stored intent carries: `<slot>` and `<slot>.status`. Refuses by name the facts with no source. |
| `Admissions` (`NewAdmissions`) | `intakeadapters.Admissions` | Built. |
| `AtRiskFromEnv` | the `AtRisk` of `intakeadapters.ClocksConfig` | Built. |
| Anchors | `intakeadapters.Anchors` | **Not built: the data source does not exist.** See `status/TRU-02-factsource.md`, S3. |

## Facts

`PolicyFacts(ctx, q, need)` reads the stored intent version of the query (`admitted_intent.json`, the exact version, not the latest) and gives, for each variable of the need:

- `<slot>.status`: the slot's `status`, as an Enum value. The status must be one of the contract's four (`RESOLVED`, `UNRESOLVED`, `AMBIGUOUS`, `ABSENT`).
- `<slot>`: the slot's `resolved_ref`, as a Reference, for a `Resource` slot that is `RESOLVED`. For a slot that is not, the reference is **refused** (`ErrIntent`), not given as null: the rules that read it cannot be evaluated over a subject nobody resolved (the safer reading).

A variable is sourced from the slot **of the same name**, so the package names no slot and no industry term. A Resource slot is `RESOLVED` exactly when it has a resolved reference (intent-slot.v2); anything else is `ErrIntent`.

| Step | Rule | Refusal |
|---|---|---|
| Query | tenant (`ten_...`), intent ID and version in the one form of an intent reference (`contracts.ParseIntentRef`), an intent node; checked before any login is asked | `ErrQuery` |
| Entities | any entity in the need has no source; nothing is read | `ErrNoSource` naming the entity |
| Variables | a name that cannot name a slot has no source; nothing is read | `ErrNoSource` naming it |
| Read | one statement as the tenant's login, bounded by the configured timeout; the caller's context needs a deadline | `ErrNotFound` (absent and another tenant's: same answer, same text), `ErrStore` (cause behind `errors.Is`, text fixed), `ErrNoDeadline` |
| Intent node | the document's node is the query's | `ErrIntent` |
| Slot | the slot exists (else `ErrNoSource` naming the variable); it is as the contract says (else `ErrIntent`) | |

There is never a half answer: on any error the set is empty. A need of nothing (a read capability) reads nothing and gives empty maps.

**Has no source, and is refused by name:** the `case` entity (no mapping in the pack, no case-system item in the sealed snapshot); `logical_resolution_epoch` (not stored anywhere); `case_id` (minted by the case system when a case is created, assigned by no planner); and any other variable no slot carries. With the shipped pack this means `fin.cap.create_escalation_case` and `fin.cap.route_to_internal_owner` cannot be evaluated yet (UNKNOWN at the intake), while `fin.cap.record_investigation_evidence` (reads `subject_resource.status` only) and every read capability can. See the status file, S1.

## Admissions

`AdmittedAt(ctx, tenant, intent, version)` is `admitted_intent.created_at` of that version, in whole Unix seconds, dropped and never rounded up, as `policystore.Published` drops a publication time, so both are compared on one scale. A time before second 1 is `ErrBadAnswer`.

**The choice.** The intent contract has no `admitted_at` (PERD 23.4 names one; PERD's data table lists `intent_object.admitted_at`, which the built table `admitted_intent` does not have). The row and the Record event `INTENT_ADMITTED` are written in one transaction, the event first (`understanding/intentstore`, EC-E16): the row's time is the time of that admission. The Record is not readable by a Trust login. `created_at` is the database clock (the transaction's start), the same clock as `policy_publication.published_at`.

## At-risk setting

`AtRiskFromEnv(getenv)` reads `NOEMA_TRUST_AT_RISK_BPS`: whole basis points, ASCII digits only, no sign, no space, no leading zero, no overflow, **no default**; the bounds are those of ruling A233 held by `platform/ceilings` (1000 to 5000). It returns `clocks.Ratio{Num: bps, Den: 10000}` for `ClocksConfig.AtRisk`. The error (`ErrSetting`) names the setting and the bounds and never echoes the value. The package reads no environment: the composition passes its lookup.

## What `cmd/core` must pass (EL's lane, `wire_trust.go`; not touched here)

- `intakesources.Logins`: `For(tenantID)` returns that tenant's own `trust_rw` pool (a `*pgxpool.Pool`), the same login files as the Record reader; an unknown tenant is an error, never a default login.
- `NewFacts(logins, timeout)` and `NewAdmissions(logins, timeout)` with the timeout of `NOEMA_TRUST_DB_TIMEOUT_MS`; give them to `intakeadapters.PoliciesConfig{Facts, Admissions}`.
- `AtRiskFromEnv(env.Getenv)` to `intakeadapters.ClocksConfig.AtRisk`. `Anchors` and `Routes` still have no source.

## Tests

`cd services/core && go test -count=1 -p 2 ./orchestrator/portadapters/trust/intakesources/` (a real PostgreSQL through `internal/testpg`, one `trust_rw` login for each of two tenants; the real Policies adapter, the shipped pack and the real engine in `e2e_test.go`). Written first; the mutants are in `status/TRU-02-factsource.md`.
