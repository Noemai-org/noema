# Status · OUT-02 (Core Engineer 2) · predicate language v1 over every pack outcome

Updated 2026-11-06. Plan task OUT-02 (owner C2+AG; REQ-VER-002; Needs OUT-01, merged). Done when: "Every pack outcome definition type-checks and evaluates".

## Session 2026-11-06 · OUT-02 slice 1

### Session summary
- What changed: `services/core/outcome/predicate/outcomes_eval_test.go` only. For each of the five fintech outcomes (case exists by key and count equals, owner queue equals, attachment hash equals, count equals, status set) a table of true and false cases on the data the predicate names, plus a row-order test. A guard fails when the pack gains an outcome that has no evaluation cases.
- Why: the language, the pack scope and the load-time type check already existed (OUT-01 `LoadPack` compiles every outcome); what was missing was proof that each one evaluates to the right answer.
- Tests added: REQ-VER-002. They passed when written (the evaluator was already complete), so they are guards, not seen failing first. Mutants of the pack's predicates, each run and reverted, each make a test fail: owner-queue `==` to `!=`, attachment-hash `==` to `!=`, count `== 1` to `>= 1`, an extra confirmed class.
- Disclosed: my first version asserted that a missing variable makes the predicate an error. The language compares a null with `==` as false by design; an unbound variable is UNKNOWN in the verifier, which checks bindings before it evaluates (OUT-01). I removed that assertion rather than bend the language.
- Not done: reading attachments, complaint references and settlement or switch rows from the target (the verifier's `CaseReader` port knows the `case` entity only); that belongs to the outcome slices that need those readers (OUT-03/OUT-04, ENV-05).
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: a reader per entity for the verifier, when the first non-case outcome is on a G1/G2 path.
