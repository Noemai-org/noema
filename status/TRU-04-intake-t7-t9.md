# Status · TRU-04 intake T7 to T9 (Core Engineer 1, helper session) · target resolution, ownership, evidence

Updated 2026-11-14. The next piece of the Trust assembly after TRU-02 (noema#567; the G1 card of noema#436, rulings A205 and A188). `trust/intake` now evaluates T7 target resolution, T8 business ownership and T9 evidence. T10 to T15 still return `ErrNotBuilt`, so the evaluation ends in UNKNOWN INTERNAL_ERROR at T10 and nothing can be allowed yet. This is unreviewed helper work for C1; C1 reviews, mutates and pushes it.

## State: built in a local worktree, not pushed (no PR)

## Session 2026-11-14 · TRU-04 intake T7 to T9

### Session summary
- What changed (PR links): none yet. Branch `feat/TRU-04-intake-t7-t9`: `trust/intake/{ports_t7_t9.go,gates_t7_t9.go}` (new), `ports.go` (Request and Ports fields), `gates.go` (steps and `evaluation.basis`), `intake.go` (configuration check, basis builder), `doc.go`, `README.md`, tests.
- Why: T7 to T9 are the only steps that may read about a target, and the intake had none of them.
- Risks, and how they are contained: it adds no allow path and no write path. Every port error, panic, unusable answer and an unresolved intent after T7 ends in UNKNOWN INTERNAL_ERROR. T7 reads only scoped candidates, T8 only the ownership answer, T9 only the snapshot's freshness; none of the three ports is called by T1 to T6 (a spy test). A not-owned target and a not-found one give the same decision, trace and port calls. No number is in the code: floor, maximum, digit run and attribute order arrive from the Candidates port as pack data (ruling A205 Q1 values are PROVISIONAL pack data); scores are integers, so no float decides anything.
- Tests added (REQ-TRU-002, REQ-TRU-003, REQ-AMB-001 to REQ-AMB-004, REQ-CTX-003, REQ-NFR-002, INV-06, INV-16, INV-19, INV-21, INV-23): `t7_t9_test.go` (step by step: each stop outcome with its fixed reason code, the floor boundary, the option maximum over every maximum and candidate count, order independence, unusable port answers, identical not-owned and not-found, unavailable beats stale, RecordEvidence once with the snapshot and items the port reported, errors and panics at each port method), `target_ports_test.go` (T1 to T6 never call a target port; the target ports run only after T6; a missing port is a configuration error), `gates_test.go` (spy extended; a full evaluation now ends at T10; the fault test checks the faulty port was reached). Red first at `939024f1` (22 of 33 tests failing on behaviour).
- Regression cases added: the above.
- Debt entries: none.
- Conflicts raised: none. Decisions no rule covers, for C1 (see the pre-work list).

### Pre-work for the next agent
- Exact next step: T10 to T12 (PR 2 of the assembly), then T13 to T15 and the Decision Record; adapters for `Candidates`, `Ownership` and `Evidence` (EL and the evidence tasks).
- Open questions (owner: C1, to confirm or rule):
  1. The auto-resolve margin is not in `Disambiguation`: Trust never auto-resolves (Understanding resolves a slot and writes version N+1), so the field would be dead. Add it if a use is wanted.
  2. T8 REJECT carries authority technical PASS, policy NOT_EVALUATED, business FAIL; T9 ESCALATE carries technical PASS, policy and business NOT_EVALUATED (ownership passed, the business layer is not finished). PERD 23.2 does not fix these values for T8 and T9.
  3. One candidate at or above the floor on an unresolved intent is ESCALATE `AMBIGUOUS_COUNTERPARTY` (`BuildClarification` refuses a single candidate, PERD REQ-AMB-005 calls a single left candidate still a question). Labels that cannot tell options apart escalate too (REQ-AMB-004 [REC]).
  4. No snapshot ID and no `EvidenceUnavailable` is UNKNOWN INTERNAL_ERROR (a defect), not INSUFFICIENT_EVIDENCE.
  5. T9 has no refresh port: `SnapshotEvidence` is the state after the Evidence Layer's refresh attempt, so any item past its limit is STALE_EVIDENCE. Facts UNKNOWN, UNAUTHORIZED, AMBIGUOUS and CONTRADICTORY pass T9 to T10 and T11.
  6. The pack's unit for scores (fixed-point integers against the PERD's 0.35 and 0.70) is for the adapter and the pack loader to define; Trust compares in whatever unit the port uses for both.
  7. The clarification's candidate-set ID, version and expiry (REQ-AMB-004) are not built here.
- Files to read first: `trust/intake/ports_t7_t9.go`, `trust/intake/gates_t7_t9.go`, `trust/decide/{clarify.go,basis_builder.go}`.
- Do not touch: `trust/decide`.

### Mutation check (20 mutants of the non-test code)
18 caught. Two survive and are equivalent: removing only the explicit maximum check (`decide.BuildClarification` enforces the same maximum and still escalates), and a second `RecordEvidence` whose error is ignored (the builder refuses it and its state is unchanged). Removing both maximum guards, or a second call whose error is checked, is caught.
