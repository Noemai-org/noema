# Status · ENV-14 · G0-N, the Noema half of G0 (Integration Lead)

Updated 2026-11-14. Plan task ENV-14 (owner IL, EL, B; REQ-ENV-007, REQ-ENV-002). This file is the evidence index of the Noema-side checks of G0-N (gate record `docs/poc/execution/gates/G0-N.md`, internal: TEN details stay out of the Tushe folder). It records what is shown and what is not; the gate record is the Repo Steward's and the Founders' to fill at the gate meeting. A claim counts only with a check-run link at a merged head (AGENTS section 9).

## State: not passed. Five of the items need Tushe or a Founder.

The gate record lists four things that must be true: TEN-01 to TEN-08 pass; `/contracts` v1 is tagged; at least 50 fixtures are reproducible; the isolation tests pass.

| Item | State at 14 Nov | Evidence | What is open | Owner |
|---|---|---|---|---|
| TEN-01 no runtime container reaches the oracle | **Shown** | `TEN_RESULT id=TEN-01 seed=none pass=true` in the Compose job on `main`, run 43597307427 (14 Nov); first shown in noema#116. The "oracle" is the stack's stand-ins; Tushe's own oracle is not claimed. | nothing | IL |
| TEN-02 a replay gives identical decisions and verdicts | **Shown, both halves** | Trust decisions: `services/core/trust/decide/ten02_test.go` (noema#119, seed 20261103, 64 replays). Outcome verdicts: `services/core/outcome/verifier/ten02_test.go` (noema#206, six scenarios, 64 replays each, seed 20261103). | its two `TEN_RESULT` lines (`part=trust-decisions`, `part=outcome-verdicts`, seed 20261103, 64 replays; CI run 43602090979 on this branch). The trust half's line still ends "(verdict half: not built, OUT-01)": a stale note in a log message, the verdict half is built (noema#206) | IL |
| TEN-03 every case read has a version token; a stale write changes nothing | **Shown** | `TestTEN03EveryCaseReadCarriesATokenAndAStaleWriteChangesNothing` (noema#103): `TEN_RESULT id=TEN-03 seed=none pass=true` (it uses no fault plan, so it has no seed; CI run 43602091005 on this branch) | shown by this change | IL |
| TEN-04 a case committed with the response dropped is found by key | **Shown** | `TestTEN04ACommittedCaseWithALostResponseIsFoundByKey` (noema#103): `TEN_RESULT id=TEN-04 seed=42 pass=true` (CI run 43602091005), also exercised end to end by the US-06 tests | shown by this change | IL |
| TEN-05 Tushe time per object; Noema's simulated clock only in tests, advancing with Temporal test time | **Not shown** | Noema's clocks exist (the stand-in's simulated clock, noema#244; mock-case's clock). | the part "advances together with Temporal test time" needs a Temporal worker and a workflow (`workflows` is not built); Tushe's part is G0-T | EL, C1; Tushe part: Founders |
| TEN-06 every hostility fault can be produced on demand | **Shown** | `TestTEN06EveryHostilityFaultCanBeProducedOnDemand` (noema#115): `TEN_RESULT id=TEN-06 seed=42 families=5 pass=true` (CI run 43602091067 on this branch) | nothing | IL |
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
