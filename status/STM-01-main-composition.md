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
