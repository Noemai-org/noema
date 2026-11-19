# orchestrator/portadapters/trust

Date: 2026-11-18 · Status: current. All data is SYNTHETIC. This README says what is built; the SoT, the PERD and `/contracts` define what is required.

The adapters between the orchestrator and Trust (package `trustport`). Each adapter of the Trust intake (T7 to T15) is its own pull request in this package (ruling A329, interim). Add a section for yours below; do not rewrite another's.

## Evidence (the intake's T9 read; A329, C2)

`Evidence` implements `intake.Evidence`. REQ-TRU-001, REQ-TRU-002 (T9), REQ-TRU-003, REQ-CTX-003, REQ-CTX-004, REQ-NFR-002; INV-11, INV-16, INV-17, INV-19, INV-23; AGENT-13, AGENT-16.

`SnapshotFreshness(ctx, tenantID, snapshotID)` reads the sealed snapshot the orchestrator named, as the tenant's own `trust_rw` login, and returns per item the freshness the Decision Record keeps (evidence id, effective_at, source, age, effective limit, who set it) and the stored fact status. It reads what was sealed and verified; it recomputes nothing from a payload and never reads one.

| Step | Rule | Refusal |
|---|---|---|
| Wiring | reader, profile source and clock set; the reader has a login source and a timeout from configuration (`NOEMA_TRUST_DB_TIMEOUT_MS`); no default (AGENT-16) | `ErrConfig` |
| Call | tenant ID of the shape `ten_...`; context with a deadline | `ErrSession`, `ErrEvidenceNoDeadline` |
| Snapshot ID | `evs_` and 2 to 32 letters or digits, checked before the database is asked | `ErrSnapshotNotFound` |
| Read | one read-only, repeatable-read transaction of the tenant's own login, ended by rollback: the snapshot (`context_snapshot`), its items (`evidence_item`) and the latest intent of its correlation (`admitted_intent`) are one view. The database binds the tenant (row-level security); the queries also name it | `ErrSnapshotNotFound` (absent and another tenant's are one answer, in value and in text), `ErrSnapshotStore` (cause kept for `errors.Is`, text hidden) |
| Intact | the ids are sorted and unique; each id has exactly one item, of the snapshot's own correlation; `snapshot.SetHash` over the (id, payload hash) pairs equals the stored hash; the answer is for the tenant and snapshot asked | `ErrSnapshotNotIntact` |
| Items | every item has one of the eight fact statuses, a source, a source limit of at least 1 second, a read time not after the evaluation (no negative age) and an effective time | `ErrSnapshotNotIntact`, answer empty |
| Intent | the snapshot does not name its intent (see Open). It is attributed to the correlation's latest intent only if that intent was admitted no later than every item was read | `ErrSnapshotIntent` |
| Rules | the profile's `max_evidence_age_seconds` (at least 1) and `evidence_required`, asked for the tenant and the intent's node through `SourceProfiles` | `ErrSourceRules` |

**Freshness.** Age is whole seconds from `read_at` to the injected clock's now, rounded up, so `age > limit` is exactly "older than the limit" (the status function's rule). The limit is `min(source limit, profile cap)` (INV-23): `SOURCE` on a tie, `TRUST_PROFILE` when the profile is strictly stricter. `PROFILE_OVERRIDE` is never produced: no data carries a per-source override.

**RequiredUnavailable** is true when a source the profile requires has no item at all in the snapshot. An item with status UNAVAILABLE is reported by its status; T9 handles both.

**Errors** carry no tenant, evidence identifier or row text (AGENT-13). On any error the answer is empty and T9 ends UNKNOWN.

**For the composition (EL, `wire_trust.go`).** Give `SealedSnapshots` a `SnapshotLogins` that returns each tenant's own `trust_rw` pool (`*pgxpool.Pool` is a `SnapshotTx`) and refuses an unknown tenant, with `NOEMA_TRUST_DB_TIMEOUT_MS`; give `NewEvidence` a `SourceProfiles` that reads the pack's trust profile by intent node (data, never code) and the system clock. There is no new setting.

**Open.**
- The sealed snapshot (`context_snapshot`, `CONTEXT_SNAPSHOT_SEALED`) records its correlation and not its intent. The intent rule above is the strictest one that needs no schema change; its residual is a version admitted between the intent read and the first evidence read, and it compares the database clock (`admitted_intent.created_at`) with the application clock (`read_at`). The fix is to record `intent_id@version` in the snapshot (a migration and a contract change, AGENT-15): proposed as a debt entry in `status/TRU-07-evidence.md`.
- The payload body is not re-hashed against `payload_hash` here; the nightly `evidence_verify` recompute covers items against snapshots.

**Tests.** `go test ./orchestrator/portadapters/trust/` from `services/core/`. `evidence_test.go` needs no database (a fake reader, a recording transaction). `evidence_db_test.go` needs PostgreSQL (`NOEMA_TEST_DATABASE_URL`, or a local server). Every helper of the Evidence tests starts with `evd` so that the other adapters' tests cannot collide with it.

## Intents: the capability an intent asks for (A370, TRU-02)

`StoredIntents.Get` sets `intake.AdmittedIntent.RequestedCapability`: the value of the node's capability slot, read from the stored document with the pack. The slot is the one the pack declares as a Reference with `references: capability` (the core names no slot, INV-14). None present, or one that says ABSENT, is empty; a slot that is present and cannot be read as one capability (not a Reference, an empty or non-string value, a status that is not RESOLVED or ABSENT) is `ErrStoredIntent`, never "none"; the same value in two slots is that value, two different values are `ErrStoredIntent`. Whether the capability is registered or prohibited is T4's question (`trustpack.Capabilities.Requested`). `Verify` compares the field with the stored one. Tests: `intents_capability_test.go` (real PostgreSQL).
