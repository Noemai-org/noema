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


### Update 2026-11-14 · the orchestrator's Recorder constructor (the Founder's order of 14 Nov; rulings A202 and A204, relayed, not yet committed)
- `cmd/core/recorder.go`: `buildRecorder` reads `NOEMA_ORCHESTRATOR_DSN_DIR` (one file per tenant: the tenant's `core-workflows` login), `NOEMA_ORCHESTRATOR_KEY_DIR` (the identity's signing keys, `signing.LoadRing`) and `NOEMA_ORCHESTRATOR_KEY_MAX_ACTIVE` (the rotation overlap, no default) and builds one `investigations.Store` per tenant over its own lazily connecting pool, signing as `core-workflows` only, confirming an input through the Record reader. `orchestratorRecorder` implements `orchestrator.Recorder`; a tenant with no store is an error. `recordpools.go`: the directory loader of noema#535 is extracted as `loginPools` and both use it (no behaviour change; its tests ran unchanged).
- Mutants run locally: no Record reader accepted (killed), a fallback to another tenant's store (killed), a second signer identity (killed). Two survived and are equivalent: dropping the "all three settings or none" check (the missing setting is then named by the next error anyway) and dropping the `< 1` check on the overlap (`signing.LoadRing` refuses an overlap below one).
- Not done: the plug-in in `main.go` (the composition PR), the Compose secrets and settings (IL), a first-use probe of each login (one follow-up for both the Record reader's pools and these).
