# trust

**What it does.** The Trust Engine: reconciliation at decision time, authority, policy, clocks, decisions, grants, confirmations.

**REQs.** REQ-TRU-001, REQ-TRU-002, REQ-TRU-008, REQ-TRU-009, REQ-NFR-002.

**Status.** Empty boundary package (PLT-01). PLT-01 creates the boundary; filled by TRU-01 to TRU-12. The import rules and prohibitions are in `doc.go`.

**Tests.** `go test -race ./trust/...` from `services/core/`.

**Gate order (TRU-02).** `decide.Evaluate` runs the caller's step functions T1 to T15 in the fixed order of PERD §23.3 and returns one decision plus an `evaluation_trace` of step IDs and results (`PASSED`, `TERMINAL`, `FAILED`, `PANICKED`, `DEADLINE`, `INVALID_RESULT`). A step may stop only with the outcomes of its §23.3 row, or UNKNOWN; ALLOW only at T15. A panic, an error, a done context, a missing step, a refused outcome, a decision for another intent version and a T15 that does not decide all end in UNKNOWN `INTERNAL_ERROR` (REQ-TRU-002, INV-16). Evaluate reads nothing itself: the steps carry their ports, so a REJECT at T4 to T6 happens before any read. The steps T7 to T15 themselves arrive with TRU-03 to TRU-11; the Trust intake that builds them is not built yet.

**Proposer swap (decision half).** `decide/proposer_swap_test.go` runs the real `Admit` and `FailAtGate` on two proposers' Intent Objects and keeps `synthetic/proposer_swap/decisions.json` current (`status/ENV-08.md`). Test code only; its `intake` is a stand-in for the unbuilt Trust intake. It validates the decision path only.

**Decision determinism (TRU-09).** `decide.InputHash` is the decision input hash recorded with a decision (REQ-TRU-007, INV-21): `sha256:` of the length-prefixed intent hash, sealed-snapshot hash, pack version, clock reading and distinct policy versions (sorted, so their order never matters), under the domain tag `noema/decision-input/v1`. Golden vectors come from an independent Python implementation. A decision is a pure function of those inputs: 10,000 concurrent replays of the same inputs give identical decisions and traces. The Decision Record that stores the hash is TRU-05.

**Decision basis (TRU-01).** A `decide.Decision` can carry a `Basis`: the pack version, the policy version of each rule that decided (INV-09), the sealed evidence snapshot ID, the freshness of the evidence, and the evaluation time (REQ-TRU-001). `WithBasis` attaches it once; an ALLOW or CONFIRM is refused without the policy rules, snapshot and freshness (`ErrBasisIncomplete`); a refusal before any evidence is read needs only the pack version and the time. The Decision Record that signs and stores it is TRU-05.

**One decision per input (A172, A186; migration 0027).** `recorder.Writer.Write` writes a Decision Record once for its full input tuple (tenant, intent ID, intent hash, sealed context snapshot, policy version; the unique index `decision_record_input_key`). Asking again for the same tuple returns the stored record and its grants and writes nothing; a stored decision that the new evaluation contradicts is `ErrDecisionConflict`. A new snapshot or another policy version is a new decision.
