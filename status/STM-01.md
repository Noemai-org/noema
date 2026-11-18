# Status · STM-01 (Core Engineer 1) · investigation state machine

Updated 2026-11-07. Plan task STM-01: owner C1+AG; REQ-STM-001; needs REC-01; done when "property test (random command sequences) finds no illegal state; each prohibited transition has a negative test".

## State: slice 1 of 2 in review (draft until CI is green at the final head and the reviewer line is given). STM-01 is not Done.

## Session 2026-11-07 17:00 WAT · STM-01

### Session summary
- What changed (PR links): the STM-01 pull request on `feat/STM-01-machine`. `orchestrator/table.go` reads the embedded `transitions.yaml` (restricted YAML, standard library only, fails closed on any line it does not understand); `orchestrator/machine.go` is the pure `Apply(investigation, command)`: version first (optimistic lock), then the table, then the guard; it returns the new investigation and the `Change` that `INVESTIGATION_STATE_CHANGED` will carry.
- Why: REQ-STM-001 (every transition explicit, SoT 45.1); the property and negative tests are its acceptance criteria.
- Risks, and how they are contained: a guard can refuse a listed transition and can never allow an unlisted one (the guard is not consulted for an unlisted arrow); an unknown state is never a source or a target; a table the reader does not fully understand is an error.
- Tests added (REQ-STM-001, SoT 45.1): `orchestrator/machine_test.go` (the Go table equals the file by an independent reader; every listed arrow applies with version + 1; every one of the unlisted pairs, 21 x 21 less the arrows, is refused with the typed error and changes nothing; stale versions; guards; a rapid property over random command sequences with right and wrong versions and junk states), `orchestrator/table_internal_test.go` (13 malformed tables).
- Regression cases added: none (no defect).
- Debt entries: none.
- Conflicts raised: none. `EXPLAINING_BLOCKED` has no exit in the table (OD-P-60); it is a listed target and, as the file says, the machine does not invent an exit.

## Pre-work for the next agent
- Exact next step (slice 2): the `investigation` projection table (migration, `tenant_id`, `state_version`, RLS and FORCE as in 0015), the store that does the compare-and-swap on `(investigation_id, state_version)` in the same transaction as the `INVESTIGATION_STATE_CHANGED` Record event (appended through `chain.Appender`, emitter `core-workflows`), and the alert plus S1 triage on `IllegalTransitionError`. Check PERD section 38 for the table before writing it; take the migration number from the noema#183 ledger (A34).
- Open questions (owner): the counted guards ("at most 2 times" on REJECTED_AT_GATE and BLOCKED_UNKNOWN) are policy numbers and come from `platform/config` when it has them, not from literals (Architect); OD-P-60 (EXPLAINING_BLOCKED exit).
- Files to read first: `services/core/orchestrator/README.md`, `transitions.yaml`, PERD v1.20 section 28.
- Do not touch: `transitions.yaml` without the PERD text and `test_transitions.py` (the vendored PERD text is the check).

# Session 2026-11-11 · STM-01 and EDG-01 slice 3c (the G1 slice composition root and the US-01 read runner)

## Session summary
- What changed: `orchestrator/ports.go` and `conversation.go` (the ports the slice needs and `Runner.RunRead`, the read-only path of US-01 through the PERD 28.2 table); `cmd/core/compose.go` (the composition root: `Components`, `compose`, `composeReadiness`). The ports live in `orchestrator` because `internal/contracts` holds only generated types; this is a small deviation from the package comment, which now says so.
- Why: the Architect's order (A146/A147): the Engineering Lead owns the G1 slice composition. Task IDs EDG-01 slice 3c and STM-01; allowlist `cmd/core/compose.go`, `orchestrator/**`.
- Risks, and how they are contained: nothing is wired into `main.go` yet (`/readyz` still answers 503 until it is), and no real component implements a port yet. The Record-first guarantee is tested against a recorder double until the Record-backed recorder exists (STM-01 slice 2); that is a shortcut for the Steward to register as debt.
- Tests added (REQ-STM-001, INV-12, INV-03; PERD 32.4): `TestAReadWalksTheTableAndRecordsEveryMoveBeforeTheNextStep`, `TestEveryPortSeesTheSessionAndAnIncompleteSessionIsRefusedFirst`, `TestATurnWithoutItsIdentifiersIsRefusedBeforeAnythingRuns`, `TestARecordFailureStopsTheRunAtThatMoveAndWithholdsTheAnswer`, `TestInterpretationOutcomesTakeTheTableArrows`, `TestEveryDecisionOutcomeTakesItsTableArrowAndOnlyAnAllowedReadExplains`, `TestABlockedExplanationEndsInExplainingBlockedAndNeverInCompleted`, `TestAPortFailureStopsTheRunInItsStateAndMakesNoMove`, `TestARunnerThatIsNotFullyWiredDoesNotBuild`, `TestAFullyWiredSliceComposesAndIsReady`, `TestAnyMissingComponentMeansNoCompositionAndNamesWhatIsMissing`. Six mutants of the runner (swallowed append error, advance before recording, an action explained, no session check, an unknown outcome allowed, a blocked explanation completing) are each killed by a named test.
- Regression cases added: the same tests.
- Debt entries: the recorder double (see Risks): for the Steward to register.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: switch `main.go` to `compose` and `composeReadiness` (EL, same task) once the first real components exist; then US-06 (the write path: PLANNED, EXECUTING, AWAITING_VERIFICATION) in `RunWrite`.
- Open questions (owner): constructor names and signatures from C1 (the edge chain, noema#445), IL (the stand-in read port, noema#441), C2 (interpreter and evidence); the interface of Trust's decide call (EL, with noema#237).
- Files to read first: `orchestrator/conversation.go`, `cmd/core/compose.go`.
- Do not touch: `cmd/core/main.go` and `compose.go` (the composition root is the Engineering Lead's alone).

## Review notes from C2 on the composition PR (2026-11-11, EL)
1. **Session fields (REQ-INP-002).** `orchestrator.Session{TenantID, PrincipalID}` is smaller than the four identity fields the input gate takes (`principal_id`, `principal_type`, `authn_ref`, `asserted_by`). Neither the edge's `session.Principal` nor the token carries the other three. This is not decided here: it needs an owner (EDG-01 for the token and principal, UND-04 for the gate). Until then the Interpreter adapter cannot be built from this `Session`. Open on noema#454 (owner: Architect or Steward).
2. **Creation of the investigation.** INPUT_RECEIVED is written by the input gateway (A161); the orchestrator refuses a turn without its reference. (Closed 12 Nov: `RunRead` now fails closed on a missing or malformed `InputReceivedRef`.)
3. **A second `RunRead` with the same `InvestigationID`** restarts at version 0. The Record-backed recorder (slice 2) must refuse a stale expected version (unique key on investigation and version) so the same moves cannot be appended twice.
4. **A port failure or `ErrNotARead`** leaves the investigation in its last state with no failure move, by design. The persistence slice needs a way to resume or close such a run.
5. **Typed nil.** `missing` compares interface values to nil, so a typed nil pointer in an interface passes. Check when the C1, C2 and IL constructors are plugged in.
6. **Intent store.** No merged code or task row builds the store of an admitted intent with its `intent_hash` (`Interpretation.IntentRef`, #448's `IntentSource`). Open on noema#454.

## Session 2026-11-12 · slice 2: the investigation store (EL)

## Session summary
- What changed: migration `0025_investigation.sql` (number proposed to the Steward, next free after 0024) and `orchestrator/investigations` (`Store.Start`, idempotent per recorded input; `Store.AppendChange`, the Recorder: event first, then the compare-and-swap, one transaction).
- Why: noema#477 (an input whose turn failed to start must not be run twice or stranded) and SR's note 2 on noema#264 (the expected version is part of the compare-and-swap and comes from what the caller read, never from a default). Gives the runner a durable, tenant-bound state.
- Tests added (REQ-STM-001, REQ-AUD-001, REQ-SEC-004, EC-E16): `table_test.go` (SQL only: the states equal the machine's, RECEIVED at 0, one investigation per recorded input, one version at a time, identity columns immutable by privilege, no delete or truncate, tenant isolation, only the orchestrator role) and `store_test.go` (idempotent Start, eight concurrent starts make one, refusals, event and row together, a stale change writes nothing, a failed append moves nothing, four racing changes have one winner, another tenant's login). Written first (`841b9d52`, `51b518e4`). Seven mutants on the migration killed (no FORCE, no version guard, update on all columns, no unique key, delete allowed, no insert guard, a second role admitted); the column-privilege mutant survived until the tests bumped the version.
- Not run locally: the Record-signing extension is not in this machine's PostgreSQL, so `store_test.go`'s tests that append events could not run here; CI is the first run for them. The Start tests, the refusals and all of `table_test.go` ran and pass.
- Not done, stated: the runner does not call `Start` yet (it follows noema#465, same files); resume of a stranded run (the table has no failure state; re-deciding would record a second decision); the Admissions read.
- Debt: none new. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after noema#465 merges, add `Investigations` to `orchestrator.Ports`, have `RunRead` call `Start` and refuse to run an investigation whose version is above 0 (`ErrAlreadyStarted`), and plug the store into `compose.go` as `Recorder` and `Investigations`.

### Update 2026-11-12 · reviewer P1 and P2 on noema#490
- P1 (an investigation for an input the Record does not confirm): `Store.Inputs` (required) is asked before the insert; `ConfirmWith(Lookuper)` compares the recorded event with the session and the request (type INPUT_RECEIVED, tenant, principal, correlation); a lookup failure is a refusal. P2 (reuse with another correlation): `Start` reads the stored correlation on the conflict path and refuses a mismatch. Tests first; seven mutants killed (confirmation skipped, the correlation compare, each of the four comparisons in `ConfirmWith`, a lookup failure passing).
- Pre-work: the composition root adapts `record/admission.Reader` (noema#497, `Found`) to `Lookuper` and passes `ConfirmWith(...)` as `Inputs`.

## Session 2026-11-12 · INPUT_RECEIVED reference (A161, relayed, to be committed)
## Session summary
- What changed: `Turn.InputReceivedRef`, `ErrNoInputReceived`, and the check in `RunRead` (a Record event ID, `\Arecev_[0-9A-Za-z]{2,32}\z`); the check runs before anything is recorded or run. INPUT_RECEIVED is written by the input gateway (A161); the orchestrator refuses a turn without its reference.
- Why: without a recorded INPUT_RECEIVED a reconstruction would begin at a move from a state with no event (PERD 28.2 entry of RECEIVED; PERD 32.4).
- Tests added (REQ-STM-001, PERD 32.4): `TestATurnWithoutTheRecordedInputReceivedIsRefusedBeforeAnythingIsRecordedOrRun` (ten bad references, one boundary). Written first; it did not compile at `0d39b076`. Seven mutants killed: no check, empty allowed, upper bound 40, any prefix, check after the first record, start unanchored (this one survived until the "text before the prefix" case was added). `$` instead of `\z` is equivalent in Go (no multiline flag), so the trailing-newline case guards the form, not that mutant.
- Reviewer's P1 on noema#465 (the shape proves nothing): `RunRead` now asks the `Admissions` port for the event and compares type, tenant, principal and correlation with the session and the turn, before it records or runs anything. Mutants: no type check, no tenant, principal or correlation compare, the lookup error ignored and the lookup skipped are each killed; the empty-field clauses were equivalent (the session and the turn are non-empty by then) and were removed. Debt: the Record-backed `Admissions` implementation (a read of the event as the tenant's login) is STM-01 slice 2; `compose.go` refuses to start without it.
## Pre-work for the next agent
- Exact next step: the input gateway (C2, UND-02) writes INPUT_RECEIVED and passes its event ID as `Turn.InputReceivedRef`.

### Update 2026-11-12 · reviewer P2 on noema#465 (the admission must bind this turn, not only the actor and conversation)
- `Turn.RequestID` (a lower-case UUID, as canonical-input.v2 types `request_id`; the first version used common.v1's `req_` form, which is a different kind of ID, found when reading the input gateway's payload against it and fixed at the next push, tests first; checked as part of the turn) and `Admission.RequestID`: `RunRead` runs only if the confirmed INPUT_RECEIVED carries this turn's request ID, so an older input of the same actor and conversation cannot be paired with different text. Tests first at `08e47583`. Mutants killed: the request compare removed, the request pattern removed, the pattern unanchored (it survived until a "text before the id" case was added).
- Not done: the text itself. The INPUT_RECEIVED payload the reconstruction reads has `channel`, `locale` and `request_id`; no text digest is defined, so there is nothing to compare the turn's text with. If the input gateway (C2) puts a digest of the canonical input in that event, `Admission` gains it and the runner compares it; that is the gateway's payload decision, noted on noema#454.

## Session 2026-11-12 · the Admissions read (EL)

## Session summary
- What changed: `record/admission` (`Reader.Lookup`, `Found`): finds one event by ID in the tenant's chain as the tenant's `record_read` login, verifies the chain, returns the facts the orchestrator compares (A161, A169). Tests first (`8f3b7bed`, the red head).
- Why: the `Admissions` port of noema#465 had no implementation; `compose.go` cannot be wired without one.
- Tests added (REQ-AUD-001, REQ-SEC-004, INV-12): the event of the tenant with its identity; another tenant's reader and an unknown ID are the same `ErrNotFound`; another event type and missing facts come back as they are (the reader decides nothing); refusals (no pool, bad IDs, no deadline, unconfigured). The refusals ran here; the tests that append signed events need the signing extension, so CI is their first run.
- Not done, stated: the adapter to `orchestrator.Admission` (follows noema#465); a keyed Record read (the whole chain is read per lookup, bounded by the tenant's chain).
- Debt: whole-chain read per lookup (the same as the input gateway's index); owner EL with C2; fix: a keyed read function, a migration.
- Conflicts raised: none. Found on the way: noema#465 required common.v1's `req_` form for the request ID where the gateway records a UUID (canonical-input.v2); fixed in noema#465 at `186ae004`.


# Session 2026-11-14 · the write path of the runner (EL)

## Session summary
- What changed (PR link: noema#576): `orchestrator/runwrite.go` (`WriteRunner`, `RunWrite`, `ResumeWrite`), the write ports and `Decision.GrantIDs` and `IntentRef` in `ports.go`, `orchestrator/runwrite_test.go`, the README.
- Why: the Founder's order of 14 Nov and ruling A204 (relayed, not yet committed): the Record is the truth; EXECUTING is recorded after `Execute` returns; GRANT_SPENT and DISPATCH_ATTEMPTED are read from the Record; a resume continues from the Record.
- Risks, and how they are contained: a second dispatch (a lost reply is verified by key; a spent grant is never executed again; an Executor error stops the run); an unbounded loop (the recovery budget counts failed verdicts and voided grants and has no default); a stale or absent Record (an unreadable Journal executes nothing). The resume does not know how many recoveries the crashed run already used: the caller passes what is left (stated debt).
- Tests added (REQ-STM-001, INV-03, INV-07, INV-17, AGENT-07, AGENT-08, AGENT-16, EC-E16): the eight tests of the card, plus the missing-port, voided-again, grant-count and executor-error cases. Red first at `70f8e28e` (does not compile). Nine mutants killed: no budget check, resume ignoring the Journal, resume ignoring the dispatch flag, a verdict other than ACHIEVED taken as achieved, a voided grant not counted, a recovery not counted, an unknown receipt skipping AWAITING_VERIFICATION, a Journal error ignored, the grant count relaxed, an Executor error swallowed. Four of them survived the first eight tests and are killed by the four added cases.
- Regression cases added: the same tests.
- Debt entries (for the Steward): `RunWrite` repeats the turn checks of `RunRead` (a refactor PR); the ports are doubles, the real-stack run is the end-to-end task (C1's extracted fixture is not on `main`); the budget across a crash.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Executor adapter over `execution/controller` (receipt classes: APPLIED, VOIDED, UNKNOWN for a timeout, a 5xx or silence), the Verifier adapter over `outcome/verifier`, the Record-backed Journal, then plug them into `compose.go`. Then the end-to-end test against mock-case.
- Open questions (owner): where a resume finds the stored decision and its `GrantIDs` (Trust's decision store: C1); whether the budget is kept in the Record (Architect).
- Files to read first: `orchestrator/runwrite.go`, `orchestrator/runwrite_test.go`.
- Do not touch: `transitions.yaml`.

### Update 2026-11-14 · Security Reviewer's NOT READY on noema#576 (EL)
- **Blocking 1 (UNKNOWN led to a second dispatch).** Fixed: an UNKNOWN verdict is recorded as RECOVERING and the run stops with `ErrOutcomeUnknown`; nothing is executed again and Trust is not asked again. The first-round test had required a second grant after UNKNOWN; it now covers NOT_ACHIEVED, PARTIALLY_ACHIEVED and COMPENSATED, and `TestAnUnknownVerdictStopsInRecoveringAndNeverDispatchesAgain` holds the rule. A NOT_ACHIEVED verdict still recovers under a new grant (the first write is known not to have happened). Open for the Architect: whether PARTIALLY_ACHIEVED (part of the write happened) may also take a new grant; it still does here.
- **Blocking 2 (ResumeWrite trusted the caller).** Fixed: it runs the same admission check as `RunWrite` (the turn's INPUT_RECEIVED must be this tenant's, principal's, correlation's and request's, from the Record), refuses an investigation that is not the turn's, a grant that is not one plain reference and a decision with no `IntentRef`. Not done: loading the decision itself from the Record (the Trust adapter's work: the decision store is C1's); until then the caller still supplies the decision.
- **Ruling needed (the recovery budget), not decided here.** `maxRecoveries` is still given per call, so a crash-and-resume loop that passes a fresh budget gets fresh recoveries, and when the budget runs out the run stops in EVALUATING with a grant issued and never spent or voided, and no escalation state. The safe fix needs the Architect: where the count lives (the Record or a projection column), and which arrow ends an exhausted run (the table has only EVALUATING to ESCALATING by a Trust ESCALATE decision).
- Notes taken: (a) a receipt for another grant is refused (`ErrBadPortAnswer`); (e) tests added for `ErrNotResumable`, the resume input checks, COMPENSATED, an unknown receipt class and verdict, and the session reaching the Executor, Verifier and Journal. Note (b) (a crash in EVALUATING or RECOVERING is `ErrNotResumable` and stays stuck) and (c) (two concurrent resumes can both reach `Execute`; only the database's unique spend stops the second, which the e2e crash test exercises once) and (d) (RECOVERY_REQUESTED on the Record for RECOVERING) are open.
- Mutants killed (seven, this round): UNKNOWN as a recovery; the stop move dropped; ResumeWrite without the admission check; no `IntentRef` check; no plain-grant check; the receipt's grant not compared; no investigation check.

### Update 2026-11-14 · Security Reviewer's READY at `bc3c6ba2` on noema#576, items 1 to 4 (EL)
- **Item 4 (two concurrent resumes).** `TestTwoResumesAtOnceDispatchTheGrantOnce` (real PostgreSQL and mock-case): two `ResumeWrite` calls start together on the same planned grant; the target receives one create, the Record holds one GRANT_SPENT, at least one resume completes and the other stops with an error. Runs in CI only.
- **Items 1 to 3** are registered as debt in the orchestrator README (the closing test of each is named there): the recovery budget is not persisted and an exhausted run has no escalation end state (**the Architect's ruling is asked on noema#576**); a crash in EVALUATING or RECOVERING is `ErrNotResumable`; RECOVERY_REQUESTED is not written. For the Steward to register in `debt.md`.

### Update 2026-11-14 · A216: the recovery count from the Record (EL)
- **Ruling A216 (relayed, Founder-approved interim, not yet committed): derive the recovery count from the Record; the limit from the PERD default; an exhausted run ends via the existing arrow; PARTIALLY_ACHIEVED stays as built, with the test; the adapters stand (A217).** Done: `RunWrite(ctx, s, t)` and `ResumeWrite(ctx, s, t, inv, d)` no longer take a budget. `NewWriteRunner` takes `WriteConfig{MaxDispatchesPerKey}` (at least 1; the composition passes `orchestrator.PERDMaxDispatchesPerKey` = 2, **PERD v1.23 §30.2 and REQ-RCV-003, [ADOPTED DEC-2026-10-17], OD-P-32**; AGENT-16: no agent changes it). `Journal.Progress` returns `KeyDispatches`, the GRANT_SPENT events of the grant's idempotency key; the Record-backed Journal counts them (chain verified first). Before Trust is asked again the runner derives the count from the last grant: a resume cannot reset it.
- **The arrow.** An exhausted run needs no new arrow and no change request: Trust is still asked, REQ-RCV-003 makes ESCALATE the result, and the existing arrows take it (EVALUATING to ESCALATING on DECISION_MADE ESCALATE; ESCALATING to ESCALATED on the handoff; the public stage for it is `unavailable`). The runner never decides it. If Trust nevertheless allows another write, the runner refuses it (`ErrRecoveryBudgetSpent`) and executes nothing. **The Trust adapter (C1) must itself decide ESCALATE once the key's dispatches reach the limit.**
- **PARTIALLY_ACHIEVED** is kept as built (a new grant); `TestAVerdictOtherThanAchievedRecoversUnderAFreshSnapshotAndANewGrant` covers it.
- Tests: `TestAnExhaustedRecoveryBudgetNeverExecutesAgainAndEscalatesWhenTrustDoes`, `TestAResumeCannotResetTheDispatchCountOfTheKey`, `TestAJournalThatCannotCountStopsTheRecovery`, `TestAWriteRunnerWithoutADispatchLimitDoesNotBuild`; the real-stack US-01 test asserts the Journal counts one dispatch from the real Record. Mutants killed (eight): exhaustion ignored; off by one; voids ignored or not counted; a Journal error ignored; the last grant not set on verify; the count ignored; the limit not validated.
- Debt left: voided grants are counted in the run, not from the Record; a crash in EVALUATING or RECOVERING is `ErrNotResumable`; RECOVERY_REQUESTED is not written; `ResumeWrite` takes the decision from the caller.

### Update 2026-11-14 · Security Reviewer's READY at `f9c14d7b` on noema#576, notes 1 and 2 (EL)
- **Note 1 (the count is only as good as the key).** PERD §27.2: the idempotency key is a function of tenant, principal, transaction, logical action, reason code, policy scope and the logical resolution epoch, and the epoch changes only when the prior case is verified closed and a new intent is admitted: never on a recovery. So a recovery grant for the same write carries the same key. New real-stack test `TestRunWriteRecoversAfterAWriteThatNeverCommittedUnderTheSameKey` (the target drops the first create before it commits; the run verifies by key, recovers under a second grant that commits; the Journal counts 2 dispatches of the key under either grant; the target received 2 creates and committed 1). Runs in CI only.
- **Note 2.** The README debt line now says a void loop across crashes is unbounded (a void dispatches nothing).
- **Note 3** (the end state at the limit is Trust's ESCALATE) stays asked of the Architect on the PR.

### Update 2026-11-15 · CI fix for the recovery test, and a finding for the payload guard (EL)
- `TestRunWriteRecoversAfterAWriteThatNeverCommittedUnderTheSameKey` ran both dispatches in CI (states INTERPRETING, EVALUATING, PLANNED, EXECUTING, AWAITING_VERIFICATION, RECOVERING, EVALUATING, PLANNED, EXECUTING, AWAITING_VERIFICATION) and failed on the second verification: the Record's payload guard refused `/verification_id` ("a run of 6 digits"). The test built the verification ID as `"vrf_" + receipt ID`, and a receipt's ULID can hold a run of digits by chance. The test stand-in now makes the verification ID of letters only (the production ID is the outbox event's).
- **Correction (I first wrote this as a finding against the payload guard, and it is not one).** The guard (`record/payloadguard`, noema#536, merged) exempts a core-minted `<prefix>_<ULID>` ID of any prefix, so a real verification ID (the outbox event's, `obx_<ULID>`) is not refused. What was refused was my stand-in: `"vrf_" + receipt ID` is a prefix over a prefix (`vrf_rcp_<ULID>`), which is not an ID shape the guard knows, so a ULID digit run in it counted as text. The earlier tests passed with that stand-in only because their receipt IDs held no run of five digits. The stand-in is now letters only, as above. No ruling is needed.

### Update 2026-11-14 · the orchestrator's Recorder constructor (the Founder's order of 14 Nov; rulings A202 and A204, relayed, not yet committed)
- `cmd/core/recorder.go`: `buildRecorder` reads `NOEMA_ORCHESTRATOR_DSN_DIR` (one file per tenant: the tenant's `core-workflows` login), `NOEMA_ORCHESTRATOR_KEY_DIR` (the identity's signing keys, `signing.LoadRing`) and `NOEMA_ORCHESTRATOR_KEY_MAX_ACTIVE` (the rotation overlap, no default) and builds one `investigations.Store` per tenant over its own lazily connecting pool, signing as `core-workflows` only, confirming an input through the Record reader. `orchestratorRecorder` implements `orchestrator.Recorder`; a tenant with no store is an error. `recordpools.go`: the directory loader of noema#535 is extracted as `loginPools` and both use it (no behaviour change; its tests ran unchanged).
- Mutants run locally: no Record reader accepted (killed), a fallback to another tenant's store (killed), a second signer identity (killed). Two survived and are equivalent: dropping the "all three settings or none" check (the missing setting is then named by the next error anyway) and dropping the `< 1` check on the overlap (`signing.LoadRing` refuses an overlap below one).
- Not done: the plug-in in `main.go` (the composition PR), the Compose secrets and settings (IL), a first-use probe of each login (one follow-up for both the Record reader's pools and these).

### Update 2026-11-14 · the session carries the caller's role (ruling A205, Q5; relayed, not yet committed)
- `orchestrator.Session` gains `Role string`. The orchestrator interprets no field of it: it hands the session to every port as it received it (tested: `TestTheSessionsRoleReachesEveryPortUnchanged`, `TestASessionWithoutARoleIsPassedOnAsItIs`). An empty role is refused by Trust's technical-authority step (T6, C1), not by the runner. The edge's `Principal.Role` fills it when the conversation route is built (the handler that builds the Session does not exist yet). Red first, then the field.

### Update 2026-11-14 · Security Reviewer's note 1 on noema#560 (EL)
- `TestTheSessionsRoleReachesEveryPortUnchanged` now records the session each port was called with, by port (admissions, interpreter, evidence, trust, explainer, recorder), and asserts each equals the session given, role included. The first version checked only the shared fake's last-call session, so a Trust port given a stripped session could still pass. Test only; no behaviour change. Mutant killed: the runner passing Trust a session without the role (the test names the trust port).
- Notes 2 and 3 are not this PR's: where `Role` comes from (the edge's authenticated `Principal.Role`, A195) is read when the conversation handler exists; an empty role is refused by Trust T6, not by the runner.

## Session 2026-11-13 21:15 WAT · STM-01 · the Admissions port adapter (EL)

## Session summary
- What changed (local branch `feat/STM-01-admissions-adapter`, no PR yet: the work-in-progress limit of AGENTS §3 is exceeded by my open PRs, so it waits for a slot): `orchestrator/portadapters/admissions` (package `admissionsport`): `Port.Lookup` implements `orchestrator.Admissions` over `record/admission.Reader`: it asks for the session's tenant and the turn's reference only, returns the reader's error as it is, and refuses an answer about another tenant (`ErrWrongTenant`).
- Why: the Admissions port had a reader (noema#497) and no implementation; `cmd/core` cannot plug it in without one. Package place decided on noema#436 (`orchestrator/portadapters/<port>`, so nothing under `evidence/` or `trust/` imports `orchestrator`).
- Tests (REQ-AUD-001, REQ-SEC-004, INV-12): four tests; five mutants killed (no tenant check, wrong tenant asked, usable checks dropped, error rewritten, request id dropped).
- Not done, stated: the per-tenant `record_read` pools it needs in `cmd/core` are noema#535; plugging it into `compose.go` is the next PR.

## Pre-work for the next agent
- Exact next step: open the PR when a slot is free (tests first, red CI), then plug `Port{Reader: &admission.Reader{DBs: records}}` into `Components.Ports.Admissions` once noema#535 is on `main`.

### Update 2026-11-15 · A224: COMPENSATED stops the run like UNKNOWN (EL)
- **Ruling A224 (relayed, Founder-approved interim, not yet committed): COMPENSATED stops the run like UNKNOWN.** A COMPENSATED verdict (the write happened and was then undone) is recorded as RECOVERING and the run stops with the new `ErrCompensated`; nothing is executed again and Trust is not asked again, because a new grant could apply the write a second time (INV-07, AGENT-08). It used to take the recovery path with NOT_ACHIEVED and PARTIALLY_ACHIEVED; those two still recover under a new grant (PARTIALLY_ACHIEVED as built, with its test, A216).
- Tests: `TestAnUnknownOrACompensatedVerdictStopsInRecoveringAndNeverDispatchesAgain` (both verdicts; RECOVERING recorded after AWAITING_VERIFICATION; one execute, one decision, one evidence read); the recovery test no longer lists COMPENSATED. Red locally first (COMPENSATED recovered: 2 executes and ErrRecoveryBudgetSpent). Two mutants killed: COMPENSATED back on the recovery path; ErrOutcomeUnknown returned for it.

### Update 2026-11-15 · Codex P1: ResumeWrite proves its decision, grant and intent (EL)
- **Finding (Codex, `changes_requested` at `0034f0ff`):** `ResumeWrite` took the decision from its caller and checked only the investigation ID, that the grant ID is plain and that the intent reference is non-empty. A grant of another decision of the same tenant could reach `Execute` (from PLANNED), and a substituted decision could reach Verify and a new Trust evaluation. Accepted.
- **Fix:** a new write port `Bindings.Bound(ctx, session, Binding{CorrelationID, DecisionID, GrantID, IntentRef}) error`, required by `NewWriteRunner` and asked first in `ResumeWrite`. `ErrNotBound` is the refusal; any other error stops the resume. Nothing is read from the Journal, moved, executed or verified before it answers. The real implementation is the Trust adapter's (it reads Trust's decision and grant records); the real-stack tests use the stand-in's decision storage.
- **Tests (INV-03, INV-12, AGENT-08):** red at `6a4f02c6` (does not compile): an unbound decision does nothing in PLANNED, EXECUTING and AWAITING_VERIFICATION (no Execute, Verify, Journal read or move); Bindings is asked with exactly the turn's correlation, the decision, the grant and the intent, as the session; a Bindings that cannot answer stops the resume; a missing Bindings does not build; real stack: an unspent grant offered with another decision is refused and never spent. Four mutants killed (the call skipped; the correlation blanked; the intent blanked; the error swallowed).
- **Not done, stated:** the stored projection's state and version are still the caller's (the persistence slice, STM-01 slice 2); the binding proves the decision, grant and intent belong to this turn, not that the state is the latest. Debt for the Steward.

# Session 2026-11-15 · the Input Gateway's identity from the verified session (EL, noema#602)

## Session summary
- What changed (noema#602): `cmd/core/gatewayidentity.go` (`gatewayIdentity`, `rolesCustomer`) and its tests. The gateway's `Identity` is made in one place from the verified session in the request context: the token's tenant, its subject, the customer kind and the session's `AuthnRef`. No session, no identity.
- Why: the composition root must give `inputgateway` an identity that comes from the session and nothing else (INV-12, AGENT-10). The route is **not** mounted: what `POST /v1/conversations` answers conflicts between PERD 39.2 (`{investigation_id, conversation_ref, state, stream_url}`) and the gateway (`{correlation_id, input_event_id, input_seq, deduplicated}`); CONFLICT DETECTED on noema#601, and the safer reading applies meanwhile (not mounted, `/readyz` names it).
- Risks, and how they are contained: a wrong identity member would put another tenant's or principal's name on a Record event. Contained by tests through the real edge chain (a signed token): the identity is the session's; a second tenant's token gives that tenant; a body naming a tenant is refused; headers naming one are ignored; only a customer reaches a customer-gated route. Five mutants killed (session reference blanked; an empty identity accepted for no session; ops added to the customer roles; the kind hard-coded; the tenant hard-coded).
- Tests added (REQ-API-004, REQ-INP-001, INV-12, AGENT-10): five. Red first at `c2d7f632` (does not compile). One test case (`TestNoIdentityIsTakenFromTheBodyOrTheHeaders`, second request) lacked a `Content-Type` header and was refused by the body guard for that reason; fixed in the fix push, with the test file change disclosed in the PR.
- Regression cases added: the same. Debt entries: none. Conflicts raised: noema#601.

## Pre-work for the next agent
- Exact next step: the Architect answers noema#601; then mount `POST /v1/conversations` with `gateway.Handler(gatewayIdentity)` behind `rolesCustomer`, with the answer shape decided there. The Gateway's own configuration (hash key file, window, `RecordRecorder` over the core-edge login pool, `RecordIndex` over the record_read pool, the `Start` seam) is a separate PR.
- Open questions (owner): the answer shape and the minting of `investigation_id` and `conversation_ref` (Architect, noema#601).
- Files to read first: `inputgateway/README.md` ("For EL"), `cmd/core/compose.go`, `cmd/core/gatewayidentity.go`.
- Do not touch: `inputgateway/` (not on this allowlist).

# Session 2026-11-15 · the PayloadReader over the Trust login (EL)

## Session summary
- What changed (noema#599): `cmd/core/payloadreader.go` (`buildPayloadReader`, `trustPayloads.Read`, `EnvTrustDSNDir`, `ErrNoPayloadPool`, `ErrPayloadNotFound`, `ErrPayloadUnusable`) and its tests. It reads `evidence_payload.json` by `(tenant_id, payload_ref)` as the tenant's own `trust_rw` login, over the same directory loader as the Record reader and the Recorder (A170, A202).
- Why: ruling A228 (relayed, Founder-approved interim, not yet committed) and noema#596: C2's Reconciler classifies evidence items from their recorded payloads and takes a `PayloadReader` port; the composition implements it. `layer.Item` already carries `PayloadRef`.
- Risks, and how they are contained: a cross-tenant read (the database's row-level security through `evidence_login_tenant` decides, and the code never hands out another tenant's pool: a crossed pool and another tenant's reference both read as absent, tested against real PostgreSQL); an unbounded or injected reference (a plain-reference shape and a length bound before the database is asked; the query is parameterised); a DSN in a message (none: the driver error is replaced by a fixed one).
- Tests added (REQ-CTX-001, REQ-SEC-004, INV-12, AGENT-13): none set; one pool per tenant file and a missing tenant has none; a bad directory refuses to start and never echoes a DSN; a payload read by its reference; another tenant's payload absent, also through a crossed pool; an unknown reference absent; ten unusable calls refused first. Red first at `735c75e2` (does not compile). Mutants killed: a fallback to any pool, no deadline check, the tenant shape unchecked, the reference length unchecked, the reference shape unchecked, "not found" returned as another error. **One equivalent survivor, documented:** dropping `tenant_id` from the query's WHERE changes nothing (row-level security already binds it); the parameter is kept as defence in depth.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: C2's Reconciler takes a `PayloadReader` port with this signature (noema#596); the composition passes `*trustPayloads`; IL adds the `NOEMA_TRUST_DSN_DIR` setting and the per-tenant `trust_rw` DSN files to Compose.
- Open questions (owner): where the DSN files are provisioned (IL, `deploy/compose` is protected; OQ-09).
- Files to read first: `cmd/core/payloadreader.go`, `cmd/core/recordpools.go` (`loginPools`).
- Do not touch: `loginPools` without the three callers' tests.

# Session 2026-11-16 · POST /v1/conversations answers the PERD shape plus the correlation ID (EL, noema#617)

## Session summary
- What changed (noema#617): `cmd/core/conversations.go` (`startConversation`, `investigationIDOf`) and the mount in `compose.go` (`Components.Gateway`; `POST /v1/conversations` behind the verified session and the customer role gate, only with a gateway). On 200 and 202 the gateway's answer becomes `{investigation_id, conversation_ref, state, stream_url, correlation_id}`; every other response is the gateway's, unchanged.
- Why: ruling A234 (interim, docs#187, relayed, not yet committed), which closes noema#601: `investigation_id` is `inv_` and the ULID of the core-minted correlation ID (a retry gets the original correlation ID, A168, so the same value); `state` is `understanding`.
- **The contract prefix of `conversation_ref` (checked, as A234 asks):** PERD 32.3 has no `conv` prefix, and `canonical-input.v2` types `conversation_ref` as `common.v1`'s `InvestigationId` (`^inv_`). So `conversation_ref` equals `investigation_id`. If the Architect wants a distinct prefix it is a contract change (AGENT-15), not a route change.
- Risks, and how they are contained: a wrong or guessed identifier on the public API. Contained by deriving from the correlation ID alone and refusing (500, internal envelope, no detail) a correlation ID that is not `corr_` and a ULID; by the exact member set (nothing internal leaves); by passing every error through unchanged.
- **Found while testing:** the edge's own test tenant (`tnt_a`) is not a tenant in the Record's form (`ten_...`), so the gateway refuses its identity (`SESSION_INVALID`, fail closed). Not a product defect (a deployment's tenants are `ten_...`), but a deployment that lists a tenant in `NOEMA_SESSION_TENANTS` that is not in that form would answer 401 to everyone; the test uses `ten_aa`. The red commit had used the edge's test tenant and failed for that reason as well as for the missing code; fixed in the fix push.
- Tests added (REQ-API-001, REQ-INP-001 to REQ-INP-003, INV-12): seven, red first at `7c89bb5c` (does not compile). Seven mutants killed (the ref changed; errors rewritten; the state changed; ops added to the roles; the ULID rule loosened; an internal member added; the derivation guessed instead of refused).
- Regression cases added: the same. Debt entries: none. Conflicts raised: none new (noema#601 closed by A234).

## Pre-work for the next agent
- Exact next step: the Gateway's production construction (hash key file, deduplication window, the core-edge and record-read pools, the `Start` seam over the composed runner, late-bound so the gateway answers 503 until the runner is composed); then the investigation route and the events route (the read model from the Record, with A237's answer builder).
- Files to read first: `cmd/core/conversations.go`, `inputgateway/README.md` ("For EL").
- Do not touch: `inputgateway/`.

# Session 2026-11-16 13:30 WAT · STM-01 (SR NOT READY on noema#617)

## Session summary
- What changed (PR links): noema#617. The route's capture now starts from a clone of the response's headers, so an error body keeps the request's correlation ID (SR blocking 1; red first: `TestAnErrorBodyCarriesTheRequestsCorrelationID` failed with `corr_NONE` against a real header). `POST /v1/conversations` is a row of the authorization matrix (`edge/authz_matrix_test.go`, test file only: `mxRow` gains a body; customer only; SR blocking 2). `TestAnIdentityInTheBodyIsRefusedAndNothingStarts`: a tenant, role or principal in the body gives 400 and records and starts nothing (it passed at once: the refusal already existed in `session.RefuseBodyIdentity` and the gateway's member list; the test pins it).
- Why: SR review of 2461118f.
- Risks, and how they are contained: none new; one header map copied per request.
- Tests added (REQ, INV, US or EC IDs): REQ-API-001, INV-12.
- Regression cases added: the three above.
- Debt entries: none.
- Conflicts raised: SR note 1 (ULID digit runs in IDs the core mints, against A154) is a question for the Architect, not decided here: the correlation ID was already sent before this PR; I make no change.

## Pre-work for the next agent
- Exact next step: the investigation route and the events feed (GET /v1/investigations/{id}, SSE), reading the Record.
- Open questions (owner): A154 and core-minted ULIDs (Architect); errors.v2 against v3 codes in the gateway (SR note 2, Architect).
- Files to read first: `cmd/core/conversations.go`, `inputgateway/handler.go`.
- Do not touch: `inputgateway` (merged).

# Session 2026-11-16 · main builds its Components in one place (EL, noema#614)

## Session summary
- What changed (noema#614): `buildComponents` in `cmd/core/compose.go` builds the Record reader, the Admissions port over it (`admissionsport.Port{Reader: &admission.Reader{DBs: records}}`) and the orchestrator's Recorder, and returns them with one closer; `main.go` calls it instead of building the Record reader alone.
- Why: the Founder's order of 16 Nov (relayed, not yet committed): compose.go, route mounting, the Trust adapter. Of these only what is merged can be plugged: the Admissions adapter (noema#558) and the Recorder (noema#562). The rest is blocked, below.
- Risks, and how they are contained: a partly built composition. Contained by the unchanged `missing()` check: the Interpreter, Evidence, Trust and Explainer ports stay nil, so `/readyz` names the first and `compose` refuses; nothing is exposed that was not before. A bad setting is an error that names it, with no component built and every opened pool closed.
- Tests added (REQ-STM-001, REQ-AUD-003, REQ-SEC-004, INV-12, AGENT-16): four. Red first at `fc0e4a5a` (does not compile). Three mutants killed (the Admissions plug removed; the Recorder plug removed; Admissions built when there is no Record reader).
- Regression cases added: the same. Debt entries: none. Conflicts raised: none new.

## What is blocked, stated (the rest of the Founder's order of 16 Nov)
- **Route mounting:** `POST /v1/conversations` waits on the Architect's answer on noema#601 (PERD 39.2 and the gateway answer differently). The identity function and the role list are merged (noema#602). The events route needs a `Feed` that nothing builds.
- **The Trust adapter (`Trust.Decide`):** T13 to T15 are not on `main` (noema#572 is open: its blocking finding 1, the confirmation bound to the plan, waits on the single-use spend of noema#603 and the answers to Q-A to Q-C); without T15 there is no recorded decision or grant to return. It also implements `Bindings` for the write path (noema#576).
- **The Interpreter adapter** (C2, noema#565), **the Reconciler** (C2, noema#596; the payload reader noema#599 and its size bound noema#606), **the facts source** (EL, after noema#586 and noema#595 are approved).

## Pre-work for the next agent
- Exact next step: when noema#572 and noema#603 merge, build the Trust adapter tests first (the adapter takes the session, the intent reference and the evidence reference to `intake.Service.Evaluate`, maps its result to `orchestrator.Decision`, and implements `Bindings`); plug each merged adapter into `buildComponents` as it lands.
- Files to read first: `cmd/core/compose.go`, `trust/intake/intake.go`, `orchestrator/ports.go`.
- Do not touch: `inputgateway/`, the Trust packages (C1's).

# Session 2026-11-15 · the payload read has a byte bound (EL, noema#606)

## Session summary
- What changed (noema#606): `NOEMA_PAYLOAD_MAX_BYTES` (required whenever `NOEMA_TRUST_DSN_DIR` is set; a positive whole number; no default) and `trustPayloads.Read` bounded in SQL: the query returns the JSON text's `octet_length` and the document only when it is within the bound; over it the answer is `ErrPayloadTooLarge`, which names no content. A payload at exactly the bound is read.
- Why: the Security Reviewer's READY on noema#599 carried one condition: bound the read before the Reconciler (noema#596) wires it in.
- Risks, and how they are contained: a reader with no bound would pull an arbitrarily large row into memory. Contained by the SQL bound (the row never leaves the database) and by the required, default-free setting. **The number is not set here**: no approved value exists (AGENT-16); the Architect or a Founder gives it and IL sets it in Compose.
- Tests added (REQ-CTX-001, REQ-SEC-004, AGENT-16): the bound is configuration with no default (six bad values refuse, one good value builds); an oversize payload is refused and one at the bound is read (real PostgreSQL); a missing reference is not reported as too large. Red first at `663b46fc` (does not compile). Three mutants killed (the size check removed, the SQL bound disabled with the size check made `>=`, a zero accepted). The Security Reviewer's smaller notes on noema#599 are carried: the comment says "as the database returns it"; each tenant's own pool is asserted; the bad-directory test checks the error names the setting or a tenant; the test that depends on row-level security says so in its name.
- Regression cases added: the same. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the Architect or a Founder gives the bound and its source; IL adds `NOEMA_PAYLOAD_MAX_BYTES` to the core service in Compose (a protected path). C2's Reconciler (noema#596) then wires the reader in; the tenant it passes must come from the session, and the reference must be bound to the evidence item and its hash (the Security Reviewer's notes 1 and 3 on noema#599).
- Open questions (owner): the number (Architect or a Founder).
- Files to read first: `cmd/core/payloadreader.go`.
- Do not touch: `loginPools` without its three callers' tests.

# Session 2026-11-17 · STM-01 · C1's read of noema#576 (four tests that could not fail)
- What changed (tests; no behaviour): `TestAResumeWithTwoGrantsAsksNobodyAndDoesNothing` (`len(d.GrantIDs) != 1`), `TestAnAllowedReadGoesToExplainingAndNeverExecutes` (the `d.Read` branch of RunWrite), `TestADecisionWithNoIntentKeepsTheIntentOfTheEvaluation` (the `IntentRef` default; the evidence fake now records the intent it is asked for), and the journal port's first test file (`port_internal_test.go`: a port that cannot be used, a context with no deadline, a begin that fails, the reading of the idempotency key). Each of the first three was mutated against its line: all three mutants now fail a test. `ErrNotBound` moved off the comment of `ErrNotWired` (C1's cosmetic note).
- Not done, stated: the journal port's two branches that need a real chain (a Record that does not verify; a GRANT_SPENT that names no key) are tests of Record rows and need the pgsodium database; they cannot be run locally and are not claimed here. Debt: they are the next commit when CI can show them. C1's note on `Journal.Progress` needing a deadline that `RunWrite` and `ResumeWrite` do not set stays on the debt list (fails closed).
- One push for this round (the tests pass locally; the merge of `main` was clean).

# Session 2026-11-17 · STM-01 the spent-budget receipt class (noema#678; A216, A217, A224)
- What changed: `orchestrator.ReceiptBudgetSpent`; `portadapters/executor` returns it for `Outcome.BudgetSpent` (it was an error); `RunWrite` stops with `ErrRecoveryBudgetSpent` on it without verifying and without a new decision, leaving the investigation at PLANNED.
- Tests (REQ-RCV-003; INV-03, AGENT-08): `TestASpentDispatchBudgetIsItsOwnReceiptClass` (adapter), `TestASpentDispatchBudgetIsItsOwnReceiptClassThatStopsTheRunWithoutVerifying` (runner); red first (two failed locally, then in CI).
- For EL / the Architect: there is no arrow from PLANNED to a hand-over (HANDED_TO_TEAM) in `transitions.yaml`; A216 says EL files a change request and nothing is invented here.
