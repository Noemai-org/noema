# Status · ENV-14 · G0-N, the Noema half of G0 (Integration Lead)

Updated 2026-11-14. Plan task ENV-14 (owner IL, EL, B; REQ-ENV-007, REQ-ENV-002). This file is the evidence index of the Noema-side checks of G0-N (gate record `docs/poc/execution/gates/G0-N.md`, internal: TEN details stay out of the Tushe folder). It records what is shown and what is not; the gate record is the Repo Steward's and the Founders' to fill at the gate meeting. A claim counts only with a check-run link at a merged head (AGENTS section 9).

## State: not passed. Five of the items need Tushe or a Founder.

The gate record lists four things that must be true: TEN-01 to TEN-08 pass; `/contracts` v1 is tagged; at least 50 fixtures are reproducible; the isolation tests pass.

| Item | State at 14 Nov | Evidence | What is open | Owner |
|---|---|---|---|---|
| TEN-01 no runtime container reaches the oracle | **Shown** | `TEN_RESULT id=TEN-01 seed=none pass=true` in the Compose job on `main` at the merge commit `605d9294` of noema#578, run 43623373569; first shown in noema#116. The "oracle" is the stack's stand-ins; Tushe's own oracle is not claimed. | nothing | IL |
| TEN-02 a replay gives identical decisions and verdicts | **Shown, both halves** | Trust decisions: `services/core/trust/decide/ten02_test.go` (noema#119, seed 20261103, 64 replays). Outcome verdicts: `services/core/outcome/verifier/ten02_test.go` (noema#206, six scenarios, 64 replays each, seed 20261103). | its two `TEN_RESULT` lines (`part=trust-decisions`, `part=outcome-verdicts`, seed 20261103, 64 replays; core run 43623373609 on `main` at `605d9294`). The trust half's line used to end "(verdict half: not built, OUT-01)", a stale note; it is corrected in the follow-up fix/ENV-14-ten-evidence-notes | IL |
| TEN-03 every case read has a version token; a stale write changes nothing | **Shown** | `TestTEN03EveryCaseReadCarriesATokenAndAStaleWriteChangesNothing` (noema#103): `TEN_RESULT id=TEN-03 seed=none pass=true` (it uses no fault plan, so it has no seed; mock-case run 43623373654 on `main` at `605d9294`) | shown by this change | IL |
| TEN-04 a case committed with the response dropped is found by key | **Shown** | `TestTEN04ACommittedCaseWithALostResponseIsFoundByKey` (noema#103): `TEN_RESULT id=TEN-04 seed=42 pass=true` (mock-case run 43623373654), also exercised end to end by the US-06 tests | shown by this change | IL |
| TEN-05 Tushe time per object; Noema's simulated clock only in tests, advancing with Temporal test time | **Not shown** | Noema's clocks exist (the stand-in's simulated clock, noema#244; mock-case's clock). | the part "advances together with Temporal test time" needs a Temporal worker and a workflow (`workflows` is not built); Tushe's part is G0-T | EL, C1; Tushe part: Founders |
| TEN-06 every hostility fault can be produced on demand | **Shown** | `TestTEN06EveryHostilityFaultCanBeProducedOnDemand` (noema#115): `TEN_RESULT id=TEN-06 seed=42 families=5 pass=true` (hostility-proxy run 43623373664 on `main` at `605d9294`) | nothing | IL |
| TEN-07 every evidence response carries the section 21.1 provenance fields | **Noema side only** | the stand-in client carries `Provenance` and `RawHash` on reads (noema#205, #401) | Tushe's responses: not shown | Founders with Tushe (G0-T) |
| TEN-08 the Noema integration credential cannot perform a prohibited operation in Tushe | **Not shown** | none | Tushe's demonstration (item 9 of the 11 Nov Tushe items, noema#258) | Founders with Tushe |
| `/contracts` v1 tagged | **Not done** | no tag exists in the repository | a Founder tags the commit they choose | Founders |
| at least 50 fixtures, reproducible (two runs, identical output) | **Not verified by IL** | the generator and its determinism tests are BEN-04 | the run log of two runs with identical digests | BO |
| isolation tests, including runtime refusal on the mock-case truth endpoint and on Tushe `/partner/...` | **Truth endpoint shown; Tushe `/partner/...` not** | `TestBenchApiRefusesEveryRuntimeIdentity`, `TestCaseApiOpensOnlyForTheRuntimeCredential`, `TestBenchCredentialOpensTheBenchApi` (`synthetic/mock_case/internal/api/auth_test.go`), `TestForbiddenOperationsHaveNoRouteAndChangeNothing` (`absent_test.go`) | Tushe `/partner/...` refusal needs live Tushe | Founders with Tushe |

## Session 2026-11-14 · ENV-14

### Session summary
- What changed: the mock-case, hostility-proxy and core (`static` job) workflows print the `TEN_RESULT` lines of TEN-03, TEN-04, TEN-06 and TEN-02 (a second, verbose run of those tests only); this file.
- Why: the gate's evidence is "the Noema-side TEN results with the run seed"; CI did not print them (no `-v`), so only TEN-01 had a quotable line on `main`.
- Risks, and how they are contained: three extra short test runs in three existing jobs (the TEN-02 tests need no database); no change to any test.
- Tests added: none (the TEN tests exist).
- Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: after this merges, quote the `TEN_RESULT` lines of TEN-01, TEN-02 (two parts), TEN-03, TEN-04 and TEN-06 with their run links at the merged head in the gate record (the Steward fills it at the gate meeting).
- Open questions (owner): TEN-05's Temporal half (EL, C1); TEN-07, TEN-08 and the `/partner/...` refusal (Founders with Tushe); the `/contracts` v1 tag (Founders); the two fixture runs (BO).
- Do not touch: `docs/poc/execution/gates/G0-N.md` (the Steward fills it at the gate meeting, from evidence on the screen).

# Session 2026-11-15 11:30 WAT · ENV-14 · the Security Reviewer's notes on noema#578 (IL)

## Session summary
- What changed (follow-up to merged noema#578): the run links above now point at runs on `main` at the merge commit `605d9294` (the Security Reviewer found the earlier ones belonged to an older head; I read each of the four runs' logs and the `TEN_RESULT` lines are in them); the stale "verdict half: not built" note is removed from the TEN-02 trust line; the TEN-04 and TEN-06 seeds printed are the constants the fault plans are now built from (the plan and the printed seed cannot differ); the three workflows now fail if the expected `TEN_RESULT ... pass=true` line is missing (a renamed test no longer passes silently).
- Why: the Security Reviewer's notes 1, 2, 3 and the evidence fix on noema#578 (note 4 on the isolation row stands as written: the row says the Tushe part is not done and does not claim database isolation).
- Risks, and how they are contained: `.github/workflows` change (core, mock-case, hostility-proxy): Security Reviewer and a Founder. The mock-case tests need PostgreSQL, so I could not run them here; CI runs them.
- Tests added: the line checks in three workflows. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: none for IL; G0-N still needs TEN-05 (Temporal half), TEN-07, TEN-08, `/partner`, the v1 tag and BO's fixtures.
- Files to read first: this file, noema#578.

# Session 2026-11-17 09:00 WAT · ENV-14 · the Noema-covered acceptance exceptions and the /partner refusal probe (IL)

## Session summary
- What changed (this PR): `benchmark/runner/tushe_partner_probe.py` and its tests: the refusal probe of ruling A263 (the **runtime integration credential** is refused on Tushe's `/partner...` routes). It is built and tested; it has **not been run against Tushe**: no agent holds a Tushe credential.
- **AT-04, AT-05, AT-07 and AT-14 (Noema covers): recorded seeded runs are on `main`.** Each is a test that runs its scenario twice from a real FaultPlan, requires the two runs to be identical and requires the committed record to equal them: AT-04, AT-05 and AT-07 in `benchmark/hostility_proxy/internal/proxy/at_test.go` (`TestAT04...`, `TestAT05...`, `TestAT07RecordedSeededRun`; records `synthetic/at_runs/AT-04.json`, `AT-05.json`, `AT-07.json`); AT-14 in `synthetic/mock_case/internal/api/at14_test.go` (`TestAT14RecordedSeededRun`; `AT-14.json`, seed 42). CI on `main`: hostility-proxy run 43891129850 (head `34dbcfe2`) and mock-case run 44022779934 (head `abc899f2`), both success. They show the proxy's and mock-case's own seeded faults, **not Tushe's behaviour**.
- **AT-17 (Noema covers, export of a completed run): NOT SHOWN.** It needs the export assembled from Tushe API reads with the read-only key; that reader is BO's (noema#627) and waits for the key (noema#620). No claim is made.
- **The `/partner` refusal probe: NOT SHOWN.** The tool exists; the result does not. How it works: a CONTROL read that the runtime credential is allowed to make must be served (2xx) first, because a credential presented the wrong way is refused everywhere and a refusal alone proves nothing; then each administration route (GET only) must answer 401 or 403; a 2xx is NOT MET; any other answer (404, 3xx, 5xx, a timeout) is NOT RUN. The credential is read from a file and never printed.
- What the probe needs, none of it guessable (Tushe's specification and deltas do not state it): the base URL (from the Environment, never in the repository); **how the runtime credential is presented** (bearer or `X-API-Key`; the tool takes it as `--scheme` and has no default); **one read the runtime credential is allowed to make** (`--control-path`); and the routes to probe (delta v1.3 names `GET /partners/api-keys/scopes`; delta v1.2 names `GET /partner/events`). A263's two extra probes, that the read-only key cannot write (TEN-08a and TEN-08b), need a write route that is harmless if wrongly accepted; none is named, so they are not built.
- Why: the Founders' order of 17 Nov (AT-04, 05, 07, 14 recorded; AT-17 not shown; `/partner` probe with the runtime credential, A263).
- Risks, and how they are contained: a probe against a hosted system with a real credential: GET only, https only, no redirects, bodies discarded, credential never printed; it runs only when a Founder dispatches it with the Environment's values. Tests: 9, each failing under a mutation (no control gate; a 404 or redirect read as a refusal).
- Tests added (TEN-08; AGENT-11, AGENT-12): `benchmark/runner/test_tushe_partner_probe.py`. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: a Founder gives the three missing facts (presentation, a control read, the base URL in the Environment) and dispatches the probe; IL then adds a dispatch-only workflow that reads the credential from the Environment into a 0600 file.
- Open questions (owner): which Environment holds the runtime integration credential (`core-ci` has `TUSHE_INTEGRATION_*` in `.env.example`; a `tushe-runtime` Environment also exists) (Founders).
- Files to read first: `benchmark/runner/tushe_partner_probe.py`, `docs/poc/tushe/tushe-spec-delta-v1.3.md`.
- Do not touch: the runtime credential's value; never put a base URL or a key in a file.
