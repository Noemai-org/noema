# trust

**What it does.** The Trust Engine: reconciliation at decision time, authority, policy, clocks, decisions, grants, confirmations.

**REQs.** REQ-TRU-001, REQ-TRU-008, REQ-TRU-009.

**Status.** Empty boundary package (PLT-01). PLT-01 creates the boundary; filled by TRU-01 to TRU-12. The import rules and prohibitions are in `doc.go`.

**Tests.** `go test ./trust/...` from `services/core/` (none yet).

**Proposer swap (decision half).** `decide/proposer_swap_test.go` runs the real `Admit` and `FailAtGate` on two proposers' Intent Objects and keeps `synthetic/proposer_swap/decisions.json` current (`status/ENV-08.md`). Test code only; its `intake` is a stand-in for the unbuilt Trust intake. It validates the decision path only.

**Decision basis (TRU-01).** A `decide.Decision` can carry a `Basis`: the pack version, the policy version of each rule that decided (INV-09), the sealed evidence snapshot ID, the freshness of the evidence, and the evaluation time (REQ-TRU-001). `WithBasis` attaches it once; an ALLOW or CONFIRM is refused without the policy rules, snapshot and freshness (`ErrBasisIncomplete`); a refusal before any evidence is read needs only the pack version and the time. The Decision Record that signs and stores it is TRU-05.
