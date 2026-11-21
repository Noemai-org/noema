# orchestrator/portadapters/trust

Date: 2026-11-18 · Status: current. All data is SYNTHETIC. This README says what is built; the SoT, the PERD and `/contracts` define what is required.

The adapters between the orchestrator and Trust (package `trustport`). Each adapter of the Trust intake (T7 to T15) is its own pull request in this package (ruling A329, interim). Add a section for yours below; do not rewrite another's.

## PackProfiles (the `SourceProfiles` of the Evidence adapter; TRU-07, C2)

`PackProfiles` implements `SourceProfiles` over the pack's data. REQ-TRU-001, REQ-CTX-003, REQ-NFR-002; INV-14, INV-16, INV-23; AGENT-13, AGENT-16.

`NewPackProfiles(nodes, profiles)` takes the pack's `intent_nodes` and `trust_profiles` (decoded through the generated contract types) and `SourceRules(ctx, tenantID, intentNode)` answers the governing profile's `evidence_required` and `max_evidence_age_seconds` as a copy. No node, profile or source ID and no number is written in the code; the pack is the same for every tenant and `tenantID` is only checked to be a tenant.

| Case | Answer |
|---|---|
| Node with a profile | its rules, as a copy |
| Unknown node, empty node, node with no `trust_profile` | `ErrProfileNotFound` (never an empty rule set) |
| Zero value | `ErrConfig` |
| Tenant ID not of the shape `ten_...` | `ErrSession` |
| Context done | its own error |
| Build: no node; a node or profile with no ID or repeated; a node naming a missing profile; age limit below 1; no required source, an empty or a repeated one | `ErrProfilesData` (the whole pack, at the build) |

Errors name the rule, never a node, a profile or a source (AGENT-13). **Open:** a profile that requires no source is refused (safer reading; the schema allows it); the pack is decoded once by `trustpack.LoadPack` and once for this adapter unless EL exposes the two slices from `trustpack.Pack`.

**For the composition (EL):** `trustport.NewPackProfiles(doc.IntentNodes, doc.TrustProfiles)` with `doc` a `contracts.PackV1`; pass it to `NewEvidence`.

**Tests.** `profiles_test.go` (no database; every helper starts with `spr`), including the real `packs/fintech_v1.json` against an independent reader of the file.

## Evidence (the intake's T9 read; A329, C2)

`Evidence` implements `intake.Evidence`. REQ-TRU-001, REQ-TRU-002 (T9), REQ-TRU-003, REQ-CTX-003, REQ-CTX-004, REQ-NFR-002; INV-11, INV-16, INV-17, INV-19, INV-23; AGENT-13, AGENT-16.

`SnapshotFreshness(ctx, tenantID, snapshotID)` reads the sealed snapshot the orchestrator named, as the tenant's own `trust_rw` login, and returns per item the freshness the Decision Record keeps (evidence id, effective_at, source, age, effective limit, who set it) and the stored fact status. It reads what was sealed and verified. The freshness and the status are the stored columns and need no payload; the binding value (below) needs the payloads, read by the facts source.

**The two evidence values of the answer (A365, refined; A435).** The answer's `EvidenceSetHash` is the snapshot's OWN set hash (over the evidence IDs and payload hashes), as stored and as `intact` verifies it: the Decision Record and the grant keep it. The answer's `ConfirmationBindingEvidenceHash` is the CONTENT digest of the items (`snapshot.ContentHash`: sorted source, source record ID, FACTS DIGEST and the item's status, no evidence ID), which only the confirmation binding uses (the contract field is still named `evidence_set_hash`). An item's facts digest (`snapshot.FactsDigest`) is the digest of its MAPPED, NORMALIZED facts: `ItemFacts.Facts` (the Reconciler, `evidence/reconciler`) reads the item's stored payload as the tenant, verifies it against the item's payload hash, and applies the evidence mapping the Reconciler classifies with; there is no second mapping. The raw payload hash is not bound: two reads of unchanged evidence can differ in a volatile or unmapped field. A re-read of unchanged facts under new evidence IDs gives the same value; a changed mapped fact (the status the Reconciler gives the row, the entity or a field) or a changed item status gives another. Only what the pack's mapping maps is bound (the evidence the decision rests on); a payload field the mapping does not look at is not. Fail closed: an item with no source record ID, whose payload cannot be read or is not the sealed one (also a tampered one), that the mapping cannot use, or whose facts cannot be digested makes the snapshot unusable as evidence (`ErrSnapshotNotIntact`, the cause behind the fixed text); no payload is read for a snapshot that did not verify. `NewEvidence` takes the facts source (a missing one is `ErrConfig`).

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

**For the composition (EL, `wire_trust.go`).** Give `SealedSnapshots` a `SnapshotLogins` that returns each tenant's own `trust_rw` pool (`*pgxpool.Pool` is a `SnapshotTx`) and refuses an unknown tenant, with `NOEMA_TRUST_DB_TIMEOUT_MS`; give `NewEvidence` the Reconciler as its `ItemFacts` (the same `reconciler.Reconciler` that `Reconciler` T10 uses, built over the Trust login's payload reader of `cmd/core`) and a `SourceProfiles` that reads the pack's trust profile by intent node (data, never code) and the system clock. There is no new setting.

**Open.**
- The sealed snapshot (`context_snapshot`, `CONTEXT_SNAPSHOT_SEALED`) records its correlation and not its intent. The intent rule above is the strictest one that needs no schema change; its residual is a version admitted between the intent read and the first evidence read, and it compares the database clock (`admitted_intent.created_at`) with the application clock (`read_at`). The fix is to record `intent_id@version` in the snapshot (a migration and a contract change, AGENT-15): proposed as a debt entry in `status/TRU-07-evidence.md`.
- The payload body is re-hashed against `payload_hash` by the facts source (`payloadread`) for the binding value; the nightly `evidence_verify` recompute covers items against snapshots.

**Tests.** `go test ./orchestrator/portadapters/trust/` from `services/core/`. `evidence_test.go` needs no database (a fake reader, a recording transaction). `evidence_db_test.go` needs PostgreSQL (`NOEMA_TEST_DATABASE_URL`, or a local server). Every helper of the Evidence tests starts with `evd` so that the other adapters' tests cannot collide with it. `evidence_intent_db_test.go` (DEBT-52) holds the intent read (`ORDER BY version DESC LIMIT 1` per correlation) to the database; `reconciler/readers_agree_db_test.go` asserts that this reader and the Reconciler's `Store` return the same sealed snapshot.

## Intents: the capability an intent asks for (A370, TRU-02)

`StoredIntents.Get` sets `intake.AdmittedIntent.RequestedCapability`: the value of the node's capability slot, read from the stored document with the pack. The slot is the one the pack declares as a Reference with `references: capability` (the core names no slot, INV-14). None present, or one that says ABSENT, is empty; a slot that is present and cannot be read as one capability (not a Reference, an empty or non-string value, a status that is not RESOLVED or ABSENT) is `ErrStoredIntent`, never "none"; the same value in two slots is that value, two different values are `ErrStoredIntent`. Whether the capability is registered or prohibited is T4's question (`trustpack.Capabilities.Requested`). `Verify` compares the field with the stored one. Tests: `intents_capability_test.go` (real PostgreSQL).

## Recorder (the intake's T15 write; TRU-12)

`RecorderAdapter` implements `intake.Recorder`: it writes the Decision Record, the grants and their events with `recorder.Writer` in one transaction it opens and commits. REQ-TRU-009, REQ-AUD-001; INV-03, INV-16, INV-17; EC-E16.

`SealedFacts` is the production `RecordFactSource`. It reads the evidence set hash from the SEALED SNAPSHOT of the evaluation (the snapshot named in the evaluation's own basis, read as the request's tenant through `SnapshotReader`), re-checks it is intact and of the evaluation's own correlation (equal, else `ErrSnapshotNotIntact`), takes the policy from the manifest-verified pack passed in (it must be a policy the evaluation used) and asks a `StepFactSource` for each step's target versions and freeze permission. A `RecordRequest` has no field that can set the evidence hash or a target version, and a test fails if one is added.

The confirmation spend is the writer's and the database's, not a second path in this adapter: the adapter only passes the confirmation event and plan hash on, and `recorder.Writer` and the database's single-use constraint spend it (INV-17).

**Open.** The production `StepFactSource` (target versions and the `reconcile.FreezeGate` permission per step) is not in this package: the sealed snapshot holds no version token, so it needs a source that is another lane's (T9 to T11).

## Writes (the intake's T3; STM-01 follow-up, ruling A342 relayed, not committed)

`Writes` implements `intake.Writes`. REQ-TRU-002, REQ-NFR-002; INV-01, INV-14. Built from two parts, each behind a consumer-side interface.

| Question | Source | Answer |
|---|---|---|
| `Enabled(tenant)` | `WritesSwitch`, the `*KillSwitch` of `NewKillSwitch` (the tenant's `tenant.kill_switch`, read as the tenant's own `trust_rw` login) | true only when the switch says true without an error; any error is false with that error |
| `NeedsWrite(intent)` | `WriteRule.MayWrite(intent node)`, the pack's answer as data (never a literal in core, INV-14) | the rule's answer; no node, no answer (`ErrNoWriteAnswer`), a failing rule and a done context are false with an error |

`NewWrites(sw, rule)` is `ErrConfig` without either part. **The pack rule is not built:** the pack and the `intent-node` contract carry no field that classifies a node as read or write (`status/STM-01-writes-adapter.md` names the change needed). Until it exists `cmd/core` cannot build `Writes`, and the intake is not composed.

**Tests.** `writes_test.go`; `Enabled` runs on real PostgreSQL, `NeedsWrite` on a table-backed `WriteRule` double (a data double of the consumer-side port, not a failure mode).
