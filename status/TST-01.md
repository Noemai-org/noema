# Status · TST-01 (Core Engineer 1) · property tests of the Execution invariants

Updated 2026-11-03. Plan task TST-01: owner C1+AG; REQ-TST-002; Needs EXE-02; done when "All green on every merge".

## State: in review for INV-03, INV-07 and INV-08; INV-05 and INV-18 wait for EXE-01 and PLN-01 (they need the registry and the planner)

TST-01 is **not** Done until INV-05 and INV-18 are proved too. They ship with EXE-01 and PLN-01, which start when CON-03 (noema#120) merges.

## Session 2026-11-03 · TST-01

### Session summary
- What changed (PR links): the TST-01 pull request on `test/TST-01-execution-invariants`.
- Why: REQ-TST-002; G1 needs the property tests green.
- Risks, and how they are contained: a new dependency (`pgregory.net/rapid` v1.3.0, MPL-2.0), the same line as noema#122 and recorded in Noemai-org/docs#94; test-only. No production change.
- Tests added (INV-03, INV-07, INV-08): three property tests in `execution/controller/inv_property_test.go`. Mutation-checked four ways.
- Regression cases added: none (no defect found in the controller).
- Debt entries: none.
- Conflicts raised: none. One reading recorded: INV-03's "target … match the call" is enforced by the grant's signature, not by comparison with the plan, because a plan step has no target.

### Pre-work for the next agent
- Exact next step: EXE-01 (registry loader, static scan, INV-05) and PLN-01 (planner, plan hash, INV-18) once noema#120 is on `main`.
- Do not touch: noema#94, noema#90 (held, DEBT-01).

## Session 2026-11-04 · TST-01 · Codex review 5763774986
- **Finding:** deleting the five `rapid` replay files removes durable regression evidence. **Checked, and not accepted as stated:** the INV-03 file replays the defect "a target that does not match, validly signed", which my first draft had wrong (the controller correctly dispatches such a grant; I removed the defect, so the file's draw no longer means that case); the INV-07 and INV-08 files came from my own deliberate mutation runs (a 5xx classified as failed, a different key). None was a counterexample against the real code, and replaying them would mislead.
- **What I did with the substance:** the evidence should be durable and seed-free. Each property body is now a check function; `rapid` draws it, and three new tests run it exhaustively (`TestINV03EveryDefectUnderEveryFault`: 14 defects × 7 faults × 1 and 3 presentations; `TestINV07EveryFaultAndEveryCrashPoint`; `TestINV08EveryCrashPointUnderEveryFault`). The four mutations (signature check skipped, expiry skipped, key differs, 5xx as terminal failure) are each caught by the exhaustive tests alone (run and reverted).

## Session 2026-11-14 · TST-01: the G1 property tests re-checked against the current code (INV-03, INV-07, INV-08, the prohibited registry)
### Session summary
- What changed (PR links): the TST-01 follow-up pull request on `test/TST-01-g1-property-gaps`: four test-only files (no production code): `execution/controller/inv_property_gap_test.go` (INV-07 over every `TransportClass` declared in `types.go`, plus a class nobody listed, through `Execute`), `execution/controller/inv_property_gap2_test.go` (INV-03 fail-closed on a failed E3 to E7 read, plan flags, single use, and the dispatch budget of ruling A153), `execution/bridge/property_gap_test.go` (INV-05 at the dispatcher: every registered ID outside the mapped set, all eight prohibited ones included, gets `NO_ADAPTER_FOR_CAPABILITY`, makes no adapter call and has no revalidation; INV-08: the key reaches the adapter unchanged for attempts 1 to 9), `execution/registry/property_gap_test.go` (the eight SoT 34.3 IDs pinned, the trust profiles' prohibited lists agree with the registry, a property over flag combinations).
- Why: the seven named tests (`TestINV03...` x2, `TestINV07...` x2, `TestINV08...` x2, `TestProhibitedIDsAreNeverCallable`) pass on `main`, but they run against the deterministic simulation of D7 (`sim_world_test.go`), which re-implements the ledger and the dispatcher, and a mutation re-check against today's code showed what they do not see. G1 needs these invariants shown at the final head.
- What the seven named tests did not see (found by mutation, each re-run by C1 against the new tests): a revalidation error treated as a pass (E7) and the dispatch budget not enforced (no other non-database test caught either); DUPLICATE_CREATE read as an acknowledgement and NO_RESPONSE read as a rejection (`TestClassifyTable` elsewhere already caught both); a prohibited capability flipped to allowed in the pack data (only a count test caught it); a prohibited ID mapped in `bridge.CaseDispatcher` (only a lexical scan caught it). The new tests kill all six. Also: the old INV-07 test's `State == "SUCCEEDED"` and `"FAILED"` checks (`inv_property_test.go:167,178`) can never fire, because `ExecutionState` has no such values (`types.go:94-99`); the new test pins the real states. The old tests are not edited.
- Not covered, and not coverable by these tests: the real ledger's semantics (unspent, the nonce rule, the A153 budget, INV-08's database timestamps) are proved only by the Postgres-backed `execution/ledger/*_test.go` and `execution/e2e`; if G1 means database semantics, the evidence is that shard. No production code builds `PlanStep.Registered`, `Prohibited` or `AdapterExists` at this head (every plan source is a test stub; `cmd/core` does not wire the controller): a wiring item for the composition.
- CI visibility (a finding, not changed here): `core.yml` runs `go test -race` over the shard package lists with no `-v` and no `-run`, so the job log shows `ok <package>` and not these test names; a deleted or renamed test would pass unnoticed. The G1 evidence row 4 asks for each test by name from the `core` job log. The smallest change is one step that runs the seven named tests with `-v -run` and greps for each `--- PASS:`; it touches `.github/workflows` and needs the Security Reviewer and a Founder.
- Tests added (INV-03, INV-05, INV-07, INV-08; REQ-CAP-002, REQ-EXE-002, REQ-EXE-005, REQ-EXE-007, REQ-TST-002): the four files above.
- Regression cases added: none (no defect found in production code). Debt entries: none. Conflicts raised: none.
### Pre-work for the next agent
- Exact next step: the CI step that prints the named tests (Founder and Security Reviewer); then re-run the G1 property tests at the final head.
- Do not touch: `execution/controller/inv_property_test.go` and `sim_world_test.go` (the original TST-01 evidence).
