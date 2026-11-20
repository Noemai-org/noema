# STM-01 · final composition of `cmd/core` (main.go, compose.go)

State: draft PR, red tests pushed first; the green commit follows the red. Built by an agent for the Engineering Lead (EL). Dated 2026-11-20 WAT.

Rulings used (all "interim, relayed, not yet committed"): the Founder's order of 20 Nov (open this PR now as a draft, add commits as dependencies merge); A330 (settings: `NOEMA_<AREA>_<WHAT>[_MS|_BYTES]`, no default in code); A333 and the 20 Nov rulings (PackVersion = the manifest-verified pack; CorrelationOf = the snapshot's own, required equal to the evaluation's); A442 (worker death: exit non-zero, Compose restarts, /readyz not ready while down, no silent degraded mode); A377 item 14 (cmd imports only `portadapters/caseverify`).

# Session 2026-11-20 WAT · STM-01 (main composition, slice 1: what main has today)

## Session summary
- What changed: `composeDeps` (the seams), `buildComponentsWith` (calls `buildTrust` exactly once when a Trust setting is given), `buildWorker` (`LoadWorkerConfig` + `startWorker`), `newProcessLogger` (the redactor on the logger), `Process` and `newProcess`; `main` builds through `newProcess`.
- Why: SEC-03 (log redactor and trace filter, README limit 4 says the composition root installs them), the order of 20 Nov, A442.
- Safer readings taken:
  1. Trust is asked for only when one of `NOEMA_TRUST_DB_TIMEOUT_MS`, `NOEMA_GRANT_KEY_DIR`, `NOEMA_GRANT_KEY_MAX_ACTIVE` is set. `NOEMA_TRUST_DSN_DIR` alone is the payload reader's setting too and does not start Trust. With any of the three set, `buildTrust` makes the others required and names the missing one. With none, /readyz names `trust` (the state of today).
  2. The production adapter source returns only the clock: every other intake adapter is on an open PR, so with Trust settings the process refuses to start and names `Intents`. No quiet nil.
  3. The production worker has no activities until #812 (worker ports): a configured worker then stops the process with `ErrWorkerNotStarted` (A442, no degraded mode). With no Temporal setting there is no worker and /readyz names `worker`; once it runs, it is not named. Liveness is #794's.
  4. The worker start deadline is a structural constant (`workerStartTimeout`), not a setting and not a safety number; EL to confirm or turn it into `NOEMA_TEMPORAL_START_TIMEOUT_MS`.
  5. There is no tracer in the core (no OpenTelemetry dependency on main; adding one needs AGENT-14 approval). The redactor's trace filter is exposed as `Process.TraceFilter` for the tracer's commit; it is tested.
  6. No new setting is added by this slice.
- Hand mutation check: 18 mutants of compose.go, 17 killed. The survivor removes the append of `closeTrust` to the closers: the pools connect lazily, so a missed close is not observable without a database (CI only).
- Three tests (`TestEachTrustOnlySettingAloneAsksForTrust`, `TestAFullCompositionIsNotReadyWithoutAWorker`, `TestTheProductionSeamsHoldWhatIsMergedAndInventNothing`) were added in the green commit after the mutation check; they were not in the red head.
- NOT built (not on main): `PackVersion` from the manifest-verified pack and `CorrelationOf` (A333): #773 owns the pack load; `TrustAdapters.PackVersion` stays empty in production, which `buildTrust` already refuses. `RunningWorker.Ready/Fatal`: #794.
- Tests added (REQ-SEC-003, REQ-SEC-005, REQ-VER-004, REQ-EXE-006, REQ-OBS-002; INV-03, INV-12, INV-29, AGENT-13, AGENT-16): `main_compose_trust_test.go`, `main_compose_worker_test.go`, `main_compose_logger_test.go`.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Commits still to add, in order (each: the test first, seen red; one commit per dependency; merge origin/main first, never rebase)
1. #794 (Fatal, Ready). Red: `TestMainExitsNonZeroWhenTheWorkerDiesOnItsOwn` (a fake worker's `Fatal()` channel fires: `run` shuts the server down and returns exit code 1, not 0; needs `main` split into `run(ctx, ...) int`), `TestReadyzIsNotReadyWhileTheWorkerIsDown` (`check = withWorker(check, rw)`: rw not ready names `worker`, and ready again does not), `TestTheWorkerIsStoppedOnEveryExit` (`defer rw.Stop()`). Code: `select { case err := <-rw.Fatal(): log, shutdown, os.Exit(1) }`. A442.
2. #812 (worker ports). Red: `TestTheProductionWorkerHasItsActivities` (`productionDeps().WorkerActivities` returns activities with Executor, Reader, Issuer all non-nil when their settings are present; none nil is a startup error naming it). Replace the nil source.
3. #788 (PackProfiles, Evidence adapter's SourceProfiles). Red: `TestTheEvidenceAdapterIsBuiltFromThePackProfiles`: `TrustAdapters.Evidence` is the #788 adapter, not a fake, and `buildTrust` no longer names `Evidence`.
4. #762 (PlanGraphs), #756 (confirmation store, migration 0032), #755 then #759 (#755 owns the Declined to REJECT mapping, A435; #759 rebases on it), and the Recorder, Intents, Writes, Capabilities, Scope, Authority, Candidates, Ownership, Reconciler, Policies, Clocks, Confirmations adapters of TRU-12 and TRU-07 as each merges. Red for each: with all Trust settings, `buildTrust` stops naming that adapter (`TestProductionTrustAdaptersNameTheNextMissingOne` walks the field order; the last green flips it to "builds with no fake"). Each adapter is built in `productionDeps().TrustAdapters` from its package, never from a fake.
5. #773 (manifest-verified pack load, UND-04). Red: `TestPackVersionIsTheManifestVerifiedPacksOwn` (`TrustAdapters.PackVersion` equals the verified manifest's version; a pack that fails verification is a startup error; no `NOEMA_PACK_VERSION` setting exists) and `TestCorrelationOfIsTheSnapshotsOwnAndEqualToTheEvaluations` (A333: a snapshot whose correlation differs from the evaluation's is refused). Blocked until #773 is on main; do not invent the load.
6. #722 (Input Gateway). Red: `TestMainBuildsTheGatewayOnceAndReadinessStopsNamingIt` (`Components.Gateway` set, `input gateway` leaves /readyz; its settings name themselves when missing).
7. #726 (events, wire_events). Red: the events route is mounted only with its component; /readyz names it while missing (add the one line to `missing()` and the readiness test tables in the same PR, as the field test requires).
8. #789 (STM-05 RecoverInvestigations, session fields AuthnRef, PrincipalType, AssertedBy). Red: `TestTheRecoveryWorkerIsStartedAtStartupAndIsSingleFlight` (the startup pass runs once before /readyz turns green, A377 item 15) and `TestNoRequestDerivedValueReachesTheSession` (the one constructor builds the recovery session from the stored row). `caseverify` read-back constructor (A377 item 14) is the only case-system import in cmd.
9. Last: `compose(...)` with the full `Components` and `TestTheProductionBuildWithAllSettingsIsReady` (the production `/readyz` turns 200 only when every line above is built, with synthetic settings); delete the "named while missing" assertions that no longer apply, never weakened: each is replaced by its built-state twin.

## Pre-work for the next agent
- Exact next step: item 1 once #794 merges.
- Open questions (EL): the worker start deadline (reading 4); whether `worker` should be a `/readyz` line before #794 (reading 3).
- Files to read first: `services/core/cmd/core/compose.go`, `wire_trust.go`, `worker_run.go`.
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`.

# Session 2026-11-20 WAT · STM-01 (main composition, slice 2: the adapters that are on main)

## Session summary
- What changed: `wire_trust_adapters.go` (`PackSource`, `productionPack`, `trustAdaptersSource`, `UnbuiltAdaptersError`, `unsetAdapters`); `checkTrustAdapters` now uses `unsetAdapters`; `Components.Confirmations` and its route mount (A341, noema#754) with the process logger; `/readyz` names `confirmation route`; `buildAPI` refuses the route with no logger.
- Merged and verified with `gh api`: #759 #762 #750 #752 #754 #763 #782 #784 #787 #765 #745 #746 #747 #741 #766 #769 #771 #801 #817 #818 #820. Open: #773 #788 #722 #812 #794 #726 #789 #755 #756.
- Wired now: Capabilities, Scope, Authority (trustpack, over the pack the `PackSource` gives, escalation graph from `NOEMA_TRUST_ESCALATION_GRAPH`); the confirmation route when `Components.Confirmations` is given.
- Not wired, and why (each is named in the startup error of a process asked for Trust, in this order: Pack, Intents, Writes, Capabilities, Scope, Authority, Candidates, Ownership, Evidence, Reconciler, Policies, Clocks, Confirmations, Planner, Recorder, PackVersion):
  - Pack, PackVersion: the manifest-verified pack load is noema#773 (open). Production has no pack source; loading `trustpack.LoadPack` over an unverified pack was refused (A333: PackVersion = the manifest-verified pack). EVERY other adapter reads the pack (Intents: pack bytes; Writes: KillSwitch packRef and the write rule; Candidates, Ownership: pack nodes; Reconciler: Pack; Policies and Clocks: the pack's books; Recorder: PolicyID and escalation set), so none is buildable before it.
  - Evidence: needs `SourceProfiles` (#788, open); `SealedSnapshots` is on main and joins it then.
  - Confirmations, route store: the store and migration 0032 are #756 (open); `ConfirmationPath` (#750) is on main and needs it. The route's Requests and Answers are the same store, so `Components.Confirmations` stays nil in production and `/readyz` names it.
  - Planner: three gaps, no decision taken. (1) `plangraphs.Pack` over the pack: action-graph.v2 params have `slot`, `const`, `map`, `fact`, `policy_param`, `evidence_packet` and NO slot kind (A440 asks `{"slot","kind"}`), and the graph is selected by `applies_when`, not by intent node; the A454 Slots would come from the intent node's slot declarations. (2) `plangraphs.Intents` (stored-intent slots) has no implementation on main. (3) `plangraphs.PlanContexts` has none either. Registry: `trustpack.Pack` has no accessor for it.
  - Recorder: needs the pack's PolicyID and escalation set (`EscalationSetFromPack`), a `StepFactSource`, grant and record lifetimes and `TxTimeout` (Architect's values, no setting yet).
  - Candidates, Ownership: built per tenant, need the pack nodes plus a reader, ranker and labeller.
  - Reconciler: `NOEMA_TRUST_RECONCILE_TIMEOUT_MS` is named in its README as the composition's setting; not added, since the adapter needs the pack and snapshots first.
- Settings added: `NOEMA_TRUST_ESCALATION_GRAPH` (no code default; required once a pack is given). No other.
- Safer readings taken: (1) the confirmation route logs through the same redacted logger as `AccessLog` (no second logger field), and a route with no logger is a startup error; (2) no unverified pack is ever loaded; (3) `/readyz` is not served when the process refuses to start, so each missing adapter is named in the startup error, and with no Trust setting `/readyz` names `trust` and `confirmation route`; (4) a pack source that returns no pack and no error is no pack.
- Hand mutation check: 14 mutants, 11 killed. Survivors: `graph == ""` (equivalent: `NewCapabilities` refuses an empty graph and the message names the setting) and the `productionPack` replacement (equivalent: a nil pack takes the same path). Two tests (`TestAPackSourceThatReturnsNothingIsNoPack`, `TestCheckTrustAdaptersNamesTheFirstMissingInStructOrder`) were added after it and are not in the red head.
- Tests (REQ-TRU-002, REQ-TRU-004, REQ-TRU-005, REQ-TRU-009, REQ-SEC-003; INV-03, INV-12, INV-16, AGENT-13, AGENT-16): `main_compose_adapters_test.go`; readiness tables updated for `confirmation route`.
- Process note: a build binary (`services/core/cmd/core/core`, 37 MB) was committed by mistake in the first red commit 60cb3d5e on `feat/STM-01-main-composition` and pushed. A429: close and reopen from a fresh branch. `feat/STM-01-main-composition-v2` is the same work with no binary, not pushed.

## Pre-work for the next agent
- Exact next step: when #773 merges, replace `productionPack` and name `PackVersion` from the verified manifest; then the pack-reading adapters one by one (checklist above).
- Open questions (EL): where the pack comes from before #773; the Planner gaps; the grant and record lifetimes and `TxTimeout`.
- Files to read first: `wire_trust_adapters.go`, `compose.go`.
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`.

# Session 2026-11-20 WAT · STM-01 (main composition, review fix: a Trust whose adapters are not yet built does not stop the process)

## Session summary
- What changed: `buildComponentsWith` no longer returns an error when the Trust adapter source answers with an `UnbuiltAdaptersError` (the named, expected gaps: the pack not on main, an adapter not built). The process starts, Trust is not plugged (`Ports.Trust` stays nil, the orchestrator keeps its not-built Trust), `Components.TrustUnbuilt` holds the names, and `/readyz` answers 503 naming `trust` and each `trust.<Adapter>` (fixed text, no value). `trustSettings` (split out of `buildTrust`) still checks every Trust setting and loads the grant keys in that case: an invalid or missing setting stays a startup error. A pack that fails to load or verify, and any other adapter-source failure, stay startup errors (`trustAdaptersSource` returns the pack's own error, not a named gap).
- Why: the CI `compose` job failed at the previous head, the core container exited 1 at start-up. A445 (interim, relayed, recorded in docs#212, not yet merged): stack-check reports `/readyz` and netcheck, and the G1 bench stack needs the process up so `/readyz` can name what is missing. EL's decision (interim, relayed, not yet committed).
- Safer readings taken: (1) a setting that is present but invalid, a pack that fails verification, an unknown adapter-source error and a route with no logger stay fatal; (2) the worker rules are unchanged: a configured worker with no activities (noema#812) still stops the process (A442), and a partial Temporal setting still names the setting; (3) /readyz names adapters by their Go field names, prefixed `trust.`, no values.
- Verified by reading: `deploy/compose/docker-compose.yml` at this head sets `NOEMA_TEMPORAL_ADDRESS` only for the core (no namespace, task queue or activity timeout) and does not set `NOEMA_GRANT_KEY_DIR` (the grant keys are mounted at `/run/grant-keys`). So `LoadWorkerConfig` refuses the partial Temporal settings and the process still exits 1 under Compose until `deploy/` sets the whole worker group or the worker is also allowed to wait for its activities. `deploy/` is outside this PR's allowlist: EL to decide.
- Tests (REQ-TRU-009, REQ-API-004, REQ-SEC-003; INV-03, INV-29, AGENT-13, AGENT-16): `main_compose_unbuilt_test.go`; `TestAPackThatCannotBeLoadedIsAStartupErrorThatIsNotQuiet` changed (the pack's own error, no longer a list); the two tests that expected the production Trust settings to stop the process were removed (`TestAProcessAskedForTrustWithoutAPackRefusesAndNamesEachAdapter`, and the production case of `TestTrustSettingsWithoutTheRecordReaderOrWithoutAdaptersAreStartupErrors`); each is replaced by a test in the new file. `TestReadyzOfTheProductionBuildFailsForAServedTenantWithNoRecordLogin` needs pgsodium and runs in CI only.
- Hand mutation check: 11 mutants of compose.go, wire_trust.go and wire_trust_adapters.go, 11 killed.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: as in the earlier session (checklist above); when all adapters and the pack are built the `UnbuiltAdaptersError` path is no longer taken and `TrustUnbuilt` stays empty.
- Open questions (EL): the compose worker settings (above).
- Files to read first: `compose.go` (`buildComponentsWith`, `missing`), `wire_trust.go` (`trustSettings`).
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`, `deploy/`.

# Session 2026-11-20 WAT · STM-01 (main composition, review fix: the worker is a gap until configured; the start deadline is a setting)

## Session summary
- What changed: `buildWorker` (compose.go). A worker is configured only when a worker setting OTHER than `NOEMA_TEMPORAL_ADDRESS` is present (`workerConfigured`); otherwise, or when the activities are not built (noema#812), there is no worker, the process starts and `/readyz` names `worker` (A469, A475). Once configured, a missing or invalid companion (`LoadWorkerConfig`) or start deadline stays fatal, naming the setting and never the value. The 30 s constant `workerStartTimeout` is gone: the deadline is the required setting `NOEMA_INVESTIGATION_START_TIMEOUT_MS` (`readStartTimeout`; no default, positive whole milliseconds; A472, A330). A worker that was started and then dies still exits the process non-zero (A442): that is noema#794's `Fatal` path, unchanged and not built here; no existing test was weakened.
- Why: the base Compose sets `NOEMA_TEMPORAL_ADDRESS` only, which made `LoadWorkerConfig` refuse a partial setting and the core exit 1 (CI `compose` red). Rulings A469, A472, A475 (interim, relayed, recorded in docs#216, not yet merged).
- Safer readings taken: (1) the address alone is not a worker configuration; (2) the start deadline setting alone does not configure a worker and is not asked for without one; (3) a valid configured worker with no activities is a gap, not an error (the old `ErrWorkerNotStarted` for it is replaced; a start that fails is still `ErrWorkerNotStarted`).
- Tests (REQ-VER-004, REQ-EXE-006; INV-30, AGENT-16): `main_compose_worker_gap_test.go` (7 tests); `main_compose_worker_test.go`: the no-activities expectations moved to the gap tests, `mcWorkerEnv` gains the new setting.
- Hand mutation check: 6 mutants of `buildWorker` and `workerConfigured`, 6 killed. `TestReadyzOfTheProductionBuildFailsForAServedTenantWithNoRecordLogin` needs pgsodium and runs in CI only.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: noema#794 (Fatal, Ready) then #812 (activities); then Compose must set `NOEMA_INVESTIGATION_START_TIMEOUT_MS` (bench value 30000) with the other worker settings (EL, `deploy/`).
- Files to read first: `compose.go` (`workerConfigured`, `readStartTimeout`, `buildWorker`).
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`, `deploy/`.

# Session 2026-11-20 WAT · STM-01 (main composition, review fix: a partial worker setting is a gap; activity timeout above run timeout)

## Session summary
- What changed: `compose.go`. A worker setting that is missing is a gap, not fatal (A486): any subset of the worker settings that is incomplete (the address alone, or with some companions; `workerMissing`) starts the process with no worker, nothing dialled, and `/readyz` answers 503 naming each missing setting as `worker: <NAME> missing` (fixed text, no value). A present value that is invalid stays fatal and names the setting only (A469; `checkWorkerSettings`, which runs the loader's own checks over an environment where only the absent settings are filled with placeholders). A complete valid set with the activities not built (noema#812) stays the plain `worker` gap. `NOEMA_INVESTIGATION_START_TIMEOUT_MS` (A472) stays required for a complete worker. New `NOEMA_INVESTIGATION_RUN_TIMEOUT_MS` is read only to compare: when both it and `NOEMA_TEMPORAL_ACTIVITY_TIMEOUT_MS` are present, an activity timeout not above the run timeout is refused, fatal, naming both settings and no value (A477). The run setting is not read by anything else here and has no default.
- Tests that pinned the OLD reading and were changed to A486 (not weakened otherwise): in `main_compose_worker_gap_test.go`, `TestAConfiguredWorkerWithAMissingOrInvalidSettingIsFatalAndNamesIt` became `TestAMissingWorkerSettingIsAGapNamedByReadyzAndNotFatal` (the invalid half moved to `TestAPresentButInvalidWorkerSettingIsFatalEvenWithAMissingOne`), and `TestEachWorkerSettingBeyondTheAddressAloneAsksForAWorker` (fatal) became `TestEachWorkerSettingBeyondTheAddressAloneIsAnIncompleteWorkerGap`. New: `TestEveryMissingWorkerSettingIsNamed`, `TestAnActivityTimeoutNotAboveTheRunTimeoutIsRefused`.
- Safer readings (A332): the run timeout, when invalid, is fatal even with no worker configured (a present invalid value, A469); the activity-versus-run check also applies to an otherwise incomplete worker, since both values are known; with the run timeout absent there is no check.
- Hand mutation check: 8 mutants of compose.go, 8 killed. `TestReadyzOfTheProductionBuildFailsForAServedTenantWithNoRecordLogin` needs pgsodium and runs in CI only; the full `./cmd/core` suite was not run locally.
- Rulings: A486, A469, A477 (interim, relayed, recorded in docs#216, not yet merged).

## Pre-work for the next agent
- Exact next step: noema#794, noema#812; Compose to set the whole worker group and both timeouts (EL, `deploy/`).
- Do not touch: `trust/**`, `contracts/`, `migrations/`, `.github/`, `deploy/`.
