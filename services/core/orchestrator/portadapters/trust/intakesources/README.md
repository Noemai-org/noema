# orchestrator/portadapters/trust/intakesources

Date: 2026-11-20 · Status: current. The rulings it applies are committed: A345 in `DEC-2026-11-19-architect-rulings-a341-a357`, A363 in `DEC-2026-11-19-architect-rulings-a362-a376`, A416, A419, A438 and A447 in `DEC-2026-11-20-architect-rulings-a358-a453` (all interim, counting on merge by a non-author Founder). All data is SYNTHETIC.

The production readers behind four consumer-side ports of `trust/intakeadapters` (noema#747), package `intakesources`. Each reads as the tenant's own Trust login (`trust_rw`); the database binds the tenant (row-level security, migration 0026) and the queries also name it. None writes.

**Implements:** REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007, REQ-SEC-004, REQ-CTX-004; A306; A345 S1 and S3, A438, A461 (interim); INV-09, INV-11, INV-12, INV-14, INV-16, INV-17, INV-19; AGENT-13, AGENT-16. **Never:** a write, a login that is not the tenant's (no fallback), a fact it did not read (an absence is never a negative fact), a setting or a default for a limit, a row, an ID, a tenant or a driver's text in an error, a clock.

| Part | Port | State |
|---|---|---|
| `Facts` (`NewFacts`) | `intakeadapters.FactSource` | Built for the facts the stored intent carries (`<slot>`, `<slot>.status`, `logical_resolution_epoch`) and, with `WithLists`, the list entities of the sealed snapshot (the `case` and `complaint_ref` rows). Refuses by name the facts with no source. |
| `Admissions` (`NewAdmissions`) | `intakeadapters.Admissions` | Built. |
| `AtRiskFromLimits` | the `AtRisk` of `intakeadapters.ClocksConfig` | Built. The pack's `clock_at_risk_remaining_bp`, read through `platform/trustlimits` (A447, A416). |
| `Anchors` (`NewAnchors`) | `intakeadapters.Anchors` | Built: the clock anchor from the sealed snapshot's complaint list (A345 S3, A438). |
| `DBSnapshotItems` (`NewSnapshotItems`), `PayloadStore` (`NewPayloadStore`) | the consumer-side `SnapshotItems` of this package, and `payloadread.PayloadReader` | Built: the sealed snapshot's items and a stored payload, each as the tenant's own login. |

## Facts

`PolicyFacts(ctx, q, need)` reads the stored intent version of the query (`admitted_intent.json`, the exact version, not the latest) and gives, for each variable of the need:

- `<slot>.status`: the slot's `status`, as an Enum value. The status must be one of the contract's four (`RESOLVED`, `UNRESOLVED`, `AMBIGUOUS`, `ABSENT`).
- `<slot>`: the slot's `resolved_ref`, as a Reference, for a `Resource` slot that is `RESOLVED`. For a slot that is not, the reference is **refused** (`ErrIntent`), not given as null: the rules that read it cannot be evaluated over a subject nobody resolved (the safer reading).

A variable is sourced from the slot **of the same name**, so the package names no slot and no industry term. A Resource slot is `RESOLVED` exactly when it has a resolved reference (intent-slot.v2); anything else is `ErrIntent`.

| Step | Rule | Refusal |
|---|---|---|
| Query | tenant (`ten_...`), intent ID and version in the one form of an intent reference (`contracts.ParseIntentRef`), an intent node; checked before any login is asked | `ErrQuery` |
| Entities | any entity the lists do not serve (all, without `WithLists`) has no source; nothing is read | `ErrNoSource` naming the entity |
| Snapshot ID | an entity is asked: the snapshot ID is the form of a sealed one (`evs_...`); checked before any login is asked | `ErrQuery` |
| Variables | a name that is not `logical_resolution_epoch` and cannot name a slot has no source; nothing is read | `ErrNoSource` naming it |
| Read | one statement as the tenant's login, bounded by the configured timeout; the caller's context needs a deadline | `ErrNotFound` (absent and another tenant's: same answer, same text), `ErrStore` (cause behind `errors.Is`, text fixed), `ErrNoDeadline` |
| Intent node | the document's node is the query's | `ErrIntent` |
| Slot | the slot exists (else `ErrNoSource` naming the variable); it is as the contract says (else `ErrIntent`) | |

There is never a half answer: on any error the set is empty. A need of nothing (a read capability) reads nothing and gives empty maps.

**The epoch.** `logical_resolution_epoch` is the `logical_resolution_epoch` column of the same stored intent row (migration 0033; 1 at creation, A345 S1), given as an Integer. It is read with the row but checked only when it is asked: a value below one is `ErrBadAnswer` and no value (the column's check pins it to 1 today; the reader does not assume that). Nothing defaults it.

**Has no source, and is refused by name:** every entity the lists do not serve (without `WithLists`, every entity; with it, any but the ones named in `EvidenceLists.Entities`, so `transaction`, `beneficiary`, `account`, `switch_event`, `settlement_record` and `attachment` for the shipped pack); `case_id`, a fact produced by an earlier plan step: T11 never evaluates a rule over a produced fact (A363, A419), so it is never asked for at the first decision, and it is refused by name if it is (INV-19; a later evaluation takes the real `case_id` from the case-system evidence); and any other variable no slot carries. With the shipped pack and `WithLists`, `fin.cap.create_escalation_case` (`case`, `subject_resource`, `logical_resolution_epoch`) and every capability that reads only slots can be evaluated; `fin.cap.route_to_internal_owner` (`case_id`) cannot yet. See the status file.

## Lists from the sealed snapshot

`NewFacts(logins, timeout, WithLists(lists))` and `NewAnchors(lists, events)` take one `EvidenceLists` (a copy is kept): the `Mapping` (a `ListMapper`; the real one is `*mapping.Mapping` of `fin.map.v1@2`, built with `mapping.WithScope(...)` from the pack's evidence descriptors, as `orchestrator/portadapters/trust/mappingscope_test.go` does), `Items` (a `SnapshotItems`; `NewSnapshotItems` is the real one), `Payloads` (a `payloadread.PayloadReader`; `NewPayloadStore` is a real one, `cmd/core`'s `trustPayloads` is another) and `Entities`, the list entities served, each a `ListSpec{Source, ScopePrefix}`: the item that holds the list is the one of that source whose `resource_scope` starts with the prefix. The package names no source, entity, prefix or pack term (INV-14): the composition passes them. Two entities of one source whose prefixes overlap are `ErrConfig`. **It also takes the injection screen of the payload reader (A372, noema#780): `Screen`, `Correlation` and `Alert`, all required with no default (`ErrConfig` when one is missing).** A list payload is screened like any evidence payload: a refused screen fails the list closed (`ErrListPayload` with `payloadread.ErrScreenRefused` behind it, so the case is UNKNOWN); a flagged list is still returned (a flag is advisory) and the alert is raised when the Record refused the flag's event.

For each list the query's sealed snapshot (`q.SnapshotID`) is read **once** as the tenant, then, fail closed (INV-19), `rows` gives rows only when:

| Step | Rule | Refusal |
|---|---|---|
| Snapshot | the snapshot is sealed (a `context_snapshot` row), the tenant's, and exactly what was sealed: every ID names one item of its own correlation, none missing or repeated, the set hash verifies (INV-17) | `ErrNoSnapshot` (absent, unsealed and another tenant's: same value, same text), `ErrSnapshotIntegrity` |
| Item | exactly one item of the list's source and scope prefix | `ErrListItem` (none, or more than one) |
| Status | the item's stored fact status is `KNOWN` | `ErrListItem` |
| Payload | the stored payload is the sealed one: `payloadread.Reader` decodes it and checks its canonical hash against the item's | `ErrListPayload` (cause behind `errors.Is`) |
| List | `Mapping.ListRows(source, entity, payload)`: the list is marked complete, and is a list of objects | `ErrList` (cause `mapping.ErrIncomplete`, `ErrListShape` or `ErrNoSuchList` behind `errors.Is`) |

Only a `KNOWN` item with a payload marked complete and an empty list gives an empty entity ("no such case"): the entity is then present, with no row. Anything else is an error and the set is empty. The texts are fixed; the entity is named, never a row, an ID or a tenant.

**The item convention is an inference.** Nothing committed says which `resource_scope` the evidence read gives the case list and the complaint list when both are items of `fin.src.case_system` (A461: EL's portread slice builds them). The `ScopePrefix` is how the composition tells them apart; EL must agree on it (status file, flags).

## Anchors

`AnchorTime(ctx, tenant, snapshotID, anchorEvent)` is for the events it was given (`map[event]AnchorEvent{Entity, Field}`; the shipped pack has one, `complaint_logged`, read from the `complaint_ref` list's `effective_at`, A438). It gives the **earliest** `effective_at` over the rows of the complete list and `happened=true`; a complete empty list is `happened=false` (NOT_STARTED, A345 S3). Anything that cannot be told is an error and never `happened=false`: the snapshot, the item, the payload or the list (as above), and a row with no usable `effective_at` or one before second one (`ErrBadAnswer`). Any other event is `ErrNoSource` naming it, with nothing read.

## Admissions

`AdmittedAt(ctx, tenant, intent, version)` is `admitted_intent.created_at` of that version, in whole Unix seconds, dropped and never rounded up, as `policystore.Published` drops a publication time, so both are compared on one scale. A time before second 1 is `ErrBadAnswer`.

**The choice.** The intent contract has no `admitted_at` (PERD 23.4 names one; PERD's data table lists `intent_object.admitted_at`, which the built table `admitted_intent` does not have). The row and the Record event `INTENT_ADMITTED` are written in one transaction, the event first (`understanding/intentstore`, EC-E16): the row's time is the time of that admission. The Record is not readable by a Trust login. `created_at` is the database clock (the transaction's start), the same clock as `policy_publication.published_at`.

## At-risk fraction

`AtRiskFromLimits(limits)` gives `clocks.Ratio{Num: bps, Den: 10000}` for `ClocksConfig.AtRisk`, where `bps` is `clock_at_risk_remaining_bp` of the pack's trust-limits document (`packs/fintech/limits/trust_limits.json`, SYNTHETIC, 2500) read through `trustlimits.Limits.ClockAtRiskRemainingBP()`. That is the **one** source of the value (A447 corrects A345 S4; A416): there is no setting (`NOEMA_TRUST_AT_RISK_BPS` is withdrawn) and no default. The bounds (1000 to 5000, A233) are held and checked by `platform/trustlimits` when it loads the document: this package checks nothing again, so there is no copy of them to drift. A `Limits` that was not loaded has no value and is `ErrConfig` with no fraction (a zero fraction would mean a clock is never AT_RISK); the cause (`trustlimits.ErrNotLoaded`) stays behind `errors.Is`, the text is fixed.

## What `cmd/core` must pass (EL's lane, `wire_trust.go`; not touched here)

- `intakesources.Logins`: `For(tenantID)` returns that tenant's own `trust_rw` pool (a `*pgxpool.Pool`), the same login files as the Record reader; an unknown tenant is an error, never a default login.
- `NewAdmissions(logins, timeout)` into `intakeadapters.PoliciesConfig.Admissions`, with the timeout of `NOEMA_TRUST_DB_TIMEOUT_MS`.
- The lists: `NewSnapshotItems(logins, timeout)`; a payload reader (`NewPayloadStore(logins, timeout, maxBytes)` with `NOEMA_PAYLOAD_MAX_BYTES`, or the `trustPayloads` already in `cmd/core`); the evidence mapping parsed with `mapping.WithScope` from the pack's evidence descriptors and `fin.src.case_system` among its sources; `Entities` `{"case": {Source: "fin.src.case_system", ScopePrefix: <EL's>}, "complaint_ref": {Source: "fin.src.case_system", ScopePrefix: <EL's>}}`, all read from the pack and the evidence read's convention, not from literals in code.
- `NewFacts(logins, timeout, WithLists(lists))` into `PoliciesConfig.Facts`; `NewAnchors(lists, map[string]AnchorEvent{"complaint_logged": {Entity: "complaint_ref", Field: "effective_at"}})` into `ClocksConfig.Anchors`.
- `trustlimits.Load` of the pack's `limits/trust_limits.json`, then `AtRiskFromLimits(limits)` to `intakeadapters.ClocksConfig.AtRisk`. No environment variable for it. `Routes` still has no source.

## Tests

`cd services/core && go test -count=1 -p 2 ./orchestrator/portadapters/trust/intakesources/` (a real PostgreSQL through `internal/testpg`, one `trust_rw` and one `evidence_rw` login for each of two tenants; a sealed snapshot written the way the sealer writes it; the real evidence mapping of the shipped pack; the real Policies and Clocks adapters, the shipped pack and the real engine in `e2e_test.go` and `e2e_lists_test.go`). Written first; the mutants are in `status/TRU-02-factsource.md`.
