# Status · TRU-02 · the fact, admission and at-risk sources of the Policies (T11) and Clocks (T12) adapters (Core Engineer 1's helper, prepared locally)

Updated 2026-11-19. Item 2 of the A341 to A357 batch (ruling A345 as relayed by the user: "FactSource for Policies/Clocks per A345 (subject, status, epoch, case rows, planned references), admitted_at, anchors from the snapshot, NOEMA_TRUST_AT_RISK_BPS"). The rulings A341 to A357 are relayed and NOT committed (AGENTS section 2.3): everything here is interim, the text of A345 was not available, and where it was not given the safer reading is taken and written down. Tier 1 (Trust is a P0 path).

## State: commits on `prep/r2-factsource` (merge of noema#747's branch `feat/TRU-02-policies-clocks`, then this work); not pushed, no PR

New package `services/core/orchestrator/portadapters/trust/intakesources` (README inside). It sits beside the other Trust port adapters, not under `trust/`, because it reads the database (the lint `threshold-literal` and the "no reader in `trust/intakeadapters`" rule of its doc). No edit to `trust/intakeadapters`, `cmd/core`, migrations, contracts or the pack.

# Session 2026-11-19 · TRU-02 · fact, admission and at-risk sources

## Session summary
- What changed (PR links): none yet. `intakesources.Facts` (`intakeadapters.FactSource`), `intakesources.Admissions` (`intakeadapters.Admissions`), `intakesources.AtRiskFromEnv` and `EnvAtRiskBPS`; the shared reader (`Logins`, `Querier`).
- Why: the stop points S1, S2 and S4(a) of `status/TRU-adapters-policies-clocks.md`. S3 (anchors) and parts of S1 stay stopped: their data source does not exist (below).
- Risks, and how they are contained: it adds no allow path and no write. Every failure is an error with an empty answer, which the intake turns into UNKNOWN `INTERNAL_ERROR`. A fact is given only when it was read from the stored intent; an evidence entity, an epoch, a case reference and any variable no slot carries are refused by name (INV-19). No default for the setting. No threshold, TTL or number of its own: the bounds are `platform/ceilings` (A233), and `10000` is the unit of a basis point.
- Tests added (REQ-TRU-004, REQ-TRU-005, REQ-TRU-006, REQ-TRU-007, REQ-SEC-004; A306, A233; INV-09, INV-11, INV-16, INV-19; AGENT-13, AGENT-16): 39 tests: `admissions_test.go` (10), `facts_test.go` (18), `setting_test.go` (6), `e2e_test.go` (5: the real Policies adapter, the shipped `fintech_v1.json`, the real engine). Real PostgreSQL (`internal/testpg`), a `trust_rw` login for each of two tenants, tenant isolation tested through each tenant's own login and through a login that RLS does not bind (the query itself names the tenant).
- Regression cases added: the same tests. RED `3d54946e` (stubs; all 33 tests of that commit failed on assertions, seen), GREEN `7c46402e`, `502144b7` (tests added after the mutation pass). The RED tests read a wall clock in two timeout tests (lint `clock`); the GREEN commit compares deadlines instead.
- Debt entries: none opened. The anchor source and the epoch are decisions, not debt (below).
- Conflicts raised: none new. Two points for the Architect (Q-A, Q-B).

## What each source reads

| Source | Reads | Cmd/core passes |
|---|---|---|
| `Facts.PolicyFacts` | one statement over `admitted_intent.json` of (tenant, intent, version of the query) as the tenant's `trust_rw` login. Variable `<slot>.status` is the slot's `status` (Enum, one of the contract's four); variable `<slot>` is the `resolved_ref` of a `Resource` slot that is `RESOLVED`. A variable is sourced from the slot of the same name; the document's `intent_node` must be the query's. | `Logins`, timeout of `NOEMA_TRUST_DB_TIMEOUT_MS` |
| `Admissions.AdmittedAt` | `floor(extract(epoch FROM admitted_intent.created_at))` of that (tenant, intent, version) | same |
| `AtRiskFromEnv` | the setting `NOEMA_TRUST_AT_RISK_BPS` through the composition's lookup | `env.Getenv` |

`cmd/core` (EL, `wire_trust.go`): a `Logins` whose `For(tenantID)` returns that tenant's own `trust_rw` pool (an unknown tenant is an error, never a default login); `NewFacts` and `NewAdmissions` into `intakeadapters.PoliciesConfig{Facts, Admissions}`; `AtRiskFromEnv(env.Getenv)` into `ClocksConfig.AtRisk`. With the shipped pack, `Policies` can now be built, and `fin.cap.record_investigation_evidence` and every read capability evaluate; `fin.cap.create_escalation_case` and `fin.cap.route_to_internal_owner` end in an error (UNKNOWN) until S1 is answered.

## Blocked, and exactly what is missing

**S1a. The `case` entity rows** (`fin.cap.create_escalation_case`, the rule `fin.pol.workflow.case_dedupe`). Missing: (1) `packs/fintech/evidence_mapping/mapping.v1.json` has three entities (`debit`, `switch`, `credit`) and no `case` mapping; (2) the evidence read (`evidence/portread`) reads the ledger, the switch and the destination only, so no `fin.src.case_system` item is ever in a sealed snapshot (and `evidence_required` of the trust profile does not list it); (3) no Trust-readable case-system port exists. Needs: the pack owner's mapping of the case system's payload to `case` rows (subject, status, epoch), and either the evidence read of `fin.src.case_system` into the snapshot or a decision that Trust may read the case system itself. Owner: Architect, pack owner (ENV).

**S1b. `logical_resolution_epoch`.** Missing: it is stored nowhere. PERD 27.2 says it is "part of the investigation's persisted state" and changes only when the prior case is verified CLOSED and a new intent is admitted [REC]; the `investigation` table (migrations 0025, 0029) has no epoch column, and only `orchestrator_rw` can read that table (`trust_rw` holds no grant). Every test and the e2e stacks pass the literal `"1"`. Needs: a decision on where the epoch lives (a column plus its writer and the increment rule), a migration, and a Trust-readable path. Owner: Architect.

**S1c. Planned references (`case_id`).** Missing: nothing assigns it. The case ID is minted by the case system when a case is created (`adapters.CaseRecord.CaseID`); the plan's `case_id` param is read back by `execution/bridge` (`targetCase`) and must equal the grant's target; `trustport.PlannerAdapter` assigns no reference (it hashes params) and `PlanGraphs`, the port that would supply a graph, has no production implementation. For a first write there is no case at T11. So `fin.pol.default.permit_routing` (`case_id != null`) cannot be evaluated at T11 by any source that exists. Needs: a ruling on whether the routing rule is evaluated at T11 at all (the case exists only after the create step) or at a later point, and where the case reference comes from. Owner: Architect.

**S3. Clock anchors (`Anchors`): not built, no data source.** The pack's only anchor is `complaint_logged` (`fin.clock.ng.nip_dispute_72h`). Missing: (1) the pack maps no anchor event to a source, entity or field; (2) the complaint exists only in the case system (`adapters.ComplaintRecord.ClockStartedAt`, `complaint_ref` rows keyed by subject and epoch) and no snapshot holds a case-system item (S1a); (3) the Record is not readable by a Trust login (only `record_read`, which reads and verifies a whole chain); (4) the nearest Trust-readable data, `outcome_verdict` of `fin.outcome.exception.complaint_logged` by correlation, is scoped to one investigation, but the clock is per (subject, epoch): a complaint logged in an earlier investigation would read as "not happened", a NOT_STARTED the code would have assumed (PERD 23.5: never assumed). I did not map the event name to the outcome name by resemblance. Needs: the pack's mapping of `anchor_event` to a source and field, the snapshot or a port that carries it, and S1b (the epoch). Until then `Clocks` cannot be wired for a node whose profile has a clock (the shipped node has one).

**S4(b). Clock routing (`Routes`)** is unchanged from `status/TRU-adapters-policies-clocks.md`: no pack data, none invented.

## Safer readings taken (each to be confirmed)
1. `admitted_at` is `admitted_intent.created_at` of the version asked, in whole seconds dropped, never rounded up, like `policystore.Published`. Why: the intent contract has no `admitted_at` (PERD 23.4 and its data table name one; the built table `admitted_intent` has `created_at`), the row and the `INTENT_ADMITTED` event are written in one transaction, the event first, and a Trust login cannot read the Record. The same-second case (an admission and a publication in one second) treats the new version as in force at admission (both dropped to the same second): the database has only microsecond times and the port seconds. Per version (the evaluated one), not the first version's: the port already asks per version; PERD 23.4 says "the intent's `admitted_at`" and does not say which version.
2. A reference of a slot that is not `RESOLVED` is refused (`ErrIntent`), not given as null. The engine compares `null == null` as true, so a null subject could match a case row with no subject.
3. A variable is sourced from the slot of the same name, and `<slot>.status` from that slot's status. The pack does not say this; it is the only binding the pack's names support (`principal_must_own: ["subject_resource"]` is a slot name, `predicate_variables` has `subject_resource` and `subject_resource.status`). A name no slot carries is refused.
4. The facts read nothing of the sealed snapshot (`q.SnapshotID` unused): the subject and its status are the intent's. The snapshot does not name its intent (open item of `orchestrator/portadapters/trust/README.md`), so nothing here ties the two.
5. A Resource slot is `RESOLVED` exactly when it has a resolved reference; a slot that breaks the contract is `ErrIntent` and not repaired. A slot of another type may give its status when it has one.
6. The at-risk setting is configuration with no default; value in 1000 to 5000 (A233), digits only.

## Questions for the Architect
- Q-A. **Two sources for one value.** `platform/trustlimits` already loads `clock_at_risk_remaining_bp` (2500, label SYNTHETIC) from the pack's `limits/trust_limits.json` with the same A233 bounds (TRU-06). A345 as relayed puts it in `NOEMA_TRUST_AT_RISK_BPS`. If both are wired they can disagree. Which is the source, and should the composition refuse a mismatch? (Nothing here reads the pack's value.)
- Q-B. **The first and the evaluated version.** For an AT_ADMISSION rule, is `admitted_at` the evaluated version's admission, or the first version's of the chain (a later version is a correction of the same request)? The reader gives either: the port is per version.
- Q-C. The answers to S1a to S1c and S3 above.

## Verification (2026-11-19, `services/core`)
`go build ./... && go vet ./... && gofmt -l .` clean; `go test -count=1 -p 2 ./trust/... ./orchestrator/...` ok; `python3 tools/lint/run.py`: `lint OK`. `testdata/rapid/*.fail`: none staged.

## Mutants (by hand; each applied, run, source restored, `git diff` empty after each)
46 mutants over `logins.go`, `admissions.go`, `facts.go`, `setting.go`, `errors.go`: 45 killed, 1 equivalent. Examples: zero or missing timeout accepted; nil login source accepted; tenant shape and version not checked; deadline not required; nil login not refused; the configured timeout not applied; a missing row read as a store error; the cause dropped; the driver text shown; second 0 accepted as a time; the time rounded and not dropped; the version or the tenant not named in the query; an entity or a name that names no slot read; the intent node not compared; a status outside the contract accepted; an empty reference; a RESOLVED slot with no reference and the reverse; a reference of an unresolved or non-Resource slot; a half answer on error; the wrong key or kind; a loose form, a plus sign and a leading zero in the setting; both bounds and both boundaries; the unit; no lookup; the wrong setting name. Equivalent: `err != nil ||` before the range check in `AtRiskFromEnv` (an overflow returns the largest integer, which the range check refuses). Two survivors of the first run (the tenant named by the query, names that name no slot read by the database) were closed by tests, not by weakening any; one redundant clause (`s.Status == nil ||`) was removed from the code as equivalent.
- Review triggers (AGENTS section 7): none. No file over 400 lines (largest: `facts_test.go`, 290) and no function over 60.

## Pre-work for the next agent
- Exact next step: the Architect answers S1a to S1c, S3, Q-A and Q-B; then build the case rows, the epoch reader and the anchors reader behind the same ports, and wire `cmd/core` (EL). Push this branch after noema#747 merges.
- Open questions (owner): see above (Architect, pack owner, EL).
- Files to read first: `orchestrator/portadapters/trust/intakesources/README.md`, `facts.go`, this file, `status/TRU-adapters-policies-clocks.md`.
- Do not touch: `trust/policy`, `trust/clocks`, `trust/intake`, `cmd/core/wire_trust.go`, `platform/trustlimits`.

## Messages
- 2026-11-19 · the user (relayed order, item 2 of the A341 to A357 batch) → Core Engineer 1's helper (this session): build the readers behind the #747 ports; stop a piece whose data source does not exist. Receiver used it for: this branch. Rulings are relayed and not committed; nothing here cites one as authority.

# Session 2026-11-20 · TRU-02 · the at-risk source corrected (A447, A416) and produced_by withdrawn (A419, A363)

Updated 2026-11-20. Earlier sections above are not edited: where they say `AtRiskFromEnv`, `NOEMA_TRUST_AT_RISK_BPS`, Q-A or "relayed, not committed", this section governs. Tier 1 (Trust is a P0 path). Same branch `prep/r2-factsource`; not pushed, no PR. The work is meant to be pushed as commits onto noema#747's own branch (`feat/TRU-02-policies-clocks`), never as a PR based on another PR (AGENTS section 11).

## Session summary
- What changed (PR links): none yet. (1) The branch now contains the tip of `origin/feat/TRU-02-policies-clocks` (`5bb68e35`) and `origin/main` (one conflict, `status/README.md`: both entries kept). (2) `intakesources.AtRiskFromLimits(trustlimits.Limits) (clocks.Ratio, error)` replaces `AtRiskFromEnv`: the at-risk value is `clock_at_risk_remaining_bp` read through `platform/trustlimits` over 10000; `AtRiskFromEnv`, `EnvAtRiskBPS`, `ErrSetting` and `setting_test.go` are removed with the withdrawn setting. (3) README and `doc.go` of the package follow.
- Why: A447 (corrects A345 S4; the setting is withdrawn) and A416 (one source), both in `DEC-2026-11-20-architect-rulings-a358-a453`, which says the correction was first recorded in `DEC-2026-11-19-architect-corrections-a371-a345s4`. This answers Q-A above: the source is the pack, and there is nothing to compare because nothing else reads the value.
- Risks, and how they are contained: no allow path, no write. An unloaded `Limits` gives `ErrConfig` and no fraction, never a default (a zero fraction would mean a clock is never AT_RISK). The A233 bounds are checked once, by `platform/trustlimits` (the owner), and not again here, so there is no copy to drift; a test proves a document outside the bounds gives no fraction through that path. `platform/trustlimits` needed no new accessor (`ClockAtRiskRemainingBP() (int, error)` exists) and is not edited. The unit `10000` stays here as the unit of a basis point (not a bound).
- Tests added (REQ-TRU-006, AGENT-16, A447, A416, A233): six in `atrisk_test.go`: `TestAtRiskFromLimitsIsTheClockAtRiskRemainingBPOfTheShippedPack`, `...IsTheLoadedValueOverTenThousandAtEveryPointOfTheBounds`, `...HasNoDefaultAndRefusesALimitsThatWasNotLoaded`, `...ARefusedDocumentGivesNoFraction`, `...TheErrorIsFixedAndKeepsNoValue`, `...IsAFractionTheClocksAdapterAccepts`.
- Tests removed (they pinned the withdrawn setting; deleted together with it, none weakened): in `setting_test.go`: `TestAtRiskIsReadFromTheNamedSettingAsBasisPointsOverTenThousand`, `TestAtRiskRefusesEverythingBeyondTheBoundsOfA233AndEveryLooseForm`, `TestAtRiskTheErrorNeverEchoesTheValue`, `TestAtRiskHasNoDefaultAndNeedsALookup`, `TestAtRiskTheErrorNamesTheBoundsOfTheRuling`, `TestAtRiskIsAFractionTheClocksAdapterAccepts` (replaced by the `...FromLimits...` one).
- Regression cases added: the same tests. RED `a36894ab` (stub; six tests fail on assertions, seen), GREEN `8c000f17`.
- Debt entries: none opened.
- Conflicts raised: none.

## A419 (A363): `produced_by` and `planned:<node>`
Nothing on this branch handled a pack `produced_by` marker or a `planned:<node>` placeholder: the code, its tests and its docs never built the last bullet of A345 S1 (earlier section: "S1c", stopped for want of a source). Nothing to remove in code or tests. The docs are corrected: the README now says that `case_id`, a fact produced by an earlier plan step, is never asked for at T11 (A363: T11 never evaluates a rule over a produced fact; the first decision plans `create_escalation_case` alone), and that `Facts` still refuses it by name if it is asked (INV-19). The `produced_by` of the reason catalogue in the pack and `contracts` is a different member (the catalogue entry's producer) and is not touched. "S1c" of the earlier section is thereby closed on the Facts side; the planner side (runnable steps only, A363) is not this package's.

## What stands from A345 S1 to S3, and what is built
| Item | State |
|---|---|
| `subject_resource` and its status, from the stored intent (slot `resolved_ref` and `status`, mapped by identical name, any other status an error) | Built (earlier section). The reader reads the stored `admitted_intent` document, whose `subject_resource` slot is the intent's target; it does not read the `AdmittedIntent.TargetRef` of the Trust port (see "Flags", 1). |
| `admitted_at` = the evaluated version's `created_at` (S2) | Built; Q-B above is answered by A345 S2 as committed ("the evaluated version's"). |
| `logical_resolution_epoch` (S1) | Not built. The column is migration 0033 (C2, noema#769, not merged); no migration is touched here. Refused by name (`ErrNoSource`) until a port reads it. |
| Case rows from the sealed snapshot (S1, A438) | Not built. Needs the case mapping (`fin.map.v1@2`, `rows_from`, the `complete` marker: A438) and the case system in the profile's `evidence_required`, neither on this branch. Absence is never "no case": the entity is refused by name (INV-19). |
| Anchors from the snapshot's `complaint_ref` item, `effective_at` (S3, A438), absence NOT_STARTED | Not built. The `Anchors` port has no reader; same dependency as the case rows. |
| `Routes` (S4: empty accepted for G1, a pack `routes` field is debt before G2, DEBT-53) | Nothing built or needed here. |

## Mutants (by hand; each applied, run, source restored from a saved copy, `git diff` empty after each)
8 mutants of `atrisk.go`: wrong unit (1000), numerator plus one, numerator doubled, a default of 2500 for an unloaded `Limits`, an unloaded `Limits` giving no error, the error not `ErrConfig`, the cause dropped, the ambiguity floor read instead. 7 killed by the first run; the cause-dropped mutant survived and is closed by an added `errors.Is(err, trustlimits.ErrNotLoaded)` assertion (not by weakening any), then killed. Nothing of the removed code is left to mutate.

## Verification (2026-11-20, `services/core`)
`go build ./...`, `go vet ./trust/... ./orchestrator/... ./platform/...` and `gofmt -l .` clean; `go test -count=1 -p 2 ./trust/... ./orchestrator/portadapters/trust/... ./platform/trustlimits/... ./platform/db/...` ok; `python3 tools/lint/run.py`: `lint OK`. No `testdata/rapid/**/*.fail` staged.

## Flags
1. **TargetRef.** A345 S1 says `subject_resource` "comes from `AdmittedIntent.TargetRef`" (the Trust port's `TargetRef`, built by `orchestrator/portadapters/trust/intents.go` from the stored intent's owned slot). `Facts` reads the same stored document directly (slot named `subject_resource`), because the `FactSource` port carries no `AdmittedIntent`. The two agree for a well-formed intent; I did not add a cross-check (the safer reading would be to compare them, which needs a port change in `trust/intakeadapters`, outside this card). Architect or EL to say whether the direct read is acceptable.
2. The `status/TRU-adapters-policies-clocks.md` file (noema#747's) still says the at-risk fraction has no source; it is not mine and is not edited here.
3. The pack's trust-limits document is not loaded by anything in `cmd/core` yet (`trustlimits` has no caller): `AtRiskFromLimits` needs the composition (EL, `wire_trust.go`) to call `trustlimits.Load` first.

## Pre-work for the next agent
- Exact next step: push these commits onto noema#747's branch (C1 decides); EL wires `trustlimits.Load` then `AtRiskFromLimits` into `ClocksConfig.AtRisk`; when migration 0033 and the case mapping (A438) land, build the epoch reader, the case rows and the anchors behind the same ports.
- Open questions (owner): flag 1 above (Architect, EL).
- Files to read first: `orchestrator/portadapters/trust/intakesources/README.md`, `atrisk.go`, this file.
- Do not touch: `trust/policy`, `trust/clocks`, `trust/intake`, `cmd/core/wire_trust.go`, `platform/trustlimits`, migrations.

## Messages
- 2026-11-20 · C1 (order to this helper) → this session: apply A447 and A416 (the at-risk value from `platform/trustlimits`, the setting withdrawn) and A419 (`produced_by` removed); merge `origin/main` and the tip of noema#747's branch; add commits only. Receiver used it for: this section and its commits.

## Session 2026-11-20 · TRU-02 (C1, the real-database tests of `intakesources` after noema#771 landed on main)

- noema#771 (migration 0035) made `document_hash` required for a new publication. noema#820 (this package) merged after it with a `publish` helper in `e2e_test.go` that inserted a publication without a hash, so five tests failed on `main` (`a new publication needs the hash of its policy document`, SQLSTATE 23502). Seen failing on the merge ref of noema#827 (`tests (0)`).
- Fix (test only): `publish` gives the hash of the shipped pack's policy document (`policy.DocumentHash`), which is also what the loader will require once the drift check lands (noema#827). No production file touched.
- Verification: `go test ./orchestrator/portadapters/trust/intakesources/` ok; the same package fails five tests on `origin/main`.
