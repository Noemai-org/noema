# G1 critical path: US-01 and US-06 end to end through the public API

**A snapshot taken 4 Nov 2026 (refreshed 13:00 UTC), not a live view.** It ages with every push; the live source for heads, draft state and checks is each PR itself. Owner: Engineering Lead role agent. Heads and draft/ready state are from the live open-PR list at this time; task states are from `tasks.csv` on `main` of 3 Nov (the board owner moves status). **A check-run result is stated only where I looked at it; "unknown" otherwise.** G1: target Mon 9 Nov, evidence Wed 11 Nov, review Thu 12 Nov 10:00 (DEC-2026-11-03). Assertions: PERD §65 US-01 (ALLOW, zero grants, explanation cites masked account, one candidate with margin ≥ 0.35) and US-06 (receipt `OUTCOME_UNKNOWN`, verdict ACHIEVED by key, dispatch count 1).

One line per node: `node · task · PR (head) · state · blocker`.

## Input and evidence (US-01 and US-06)
- Input gateway · UND-02 · status file `UND-02.md`; PR not in the open list · In progress (C2) · unknown.
- Model service, intent compiler · UND-01 In progress, UND-03 In progress, UND-05 Backlog · noema#141 (`3b3cd1e`) is UND-12, not a node · UND-05 Needs UND-03 and PCK-01 · blocker: UND-05 not started (C2).
- Evidence layer · EVD-01 · no PR · **Backlog** (Needs PLT-06 In progress, ENV-03 In progress) · **blocker for TRU-04 and EVD-03**.
- Reconciliation · RES-01 · Done.
- Context snapshot and evidence_set_hash · EVD-03 · no PR · Backlog (Needs EVD-01).
- Candidates API · EVD-04 · In progress (C2) · Needs EVD-01.

## Record (US-01, US-06)
- Record append and hash chain · REC-01 · noema#122 (`39400d9`, **no longer draft**, property tests with rapid) · In progress · **blocker for TRU-05**; waits for Codex/founder merge; CI result unknown.
- Migrations · PLT-06 · noema#128 (`c0c5d7d`, bounded cleanup, draft) · In progress.
- Reconstruction · REC-03 · noema#145 (`6812cd2`, draft; "carries noema#107; merge this one alone") and noema#107 (`f4db5fe`, draft, migration 0003) · In progress.
- Chain verification · REC-05 slice 1 · noema#134 (`8daacfc`, draft); REC-06 In progress.
- Tenant from the login (DEBT-04 option A) · now in noema#145 together with REC-03 · touches the same tables as REC-01.
- Signing (W1/W3/W4) · EXE-07 slice 1 · noema#129 (`6e994b2`, draft) · W4 (EXE-08) not started · not needed for the US-01 and US-06 assertions as written, needed for DEBT-01.

## Trust (mine)
- Seven outcomes, basis · TRU-01 · noema#118 · **merged**.
- Gate order T1–T15 · TRU-02 · noema#109 · **merged**; follow-up noema#133 (`920079d`, Ready, Codex READY, 16 checks green).
- Policy evaluator · TRU-03 · noema#121 (`34b1d9b`, Ready, Codex READY at that head) · awaiting founder approval.
- Decision determinism · TRU-09 · noema#132 (`fdbb1c9`, draft; Codex READY at `b8e6c16`; main merged in) · not on the US-01/US-06 assertions; G2. Its `codegen-diff` is red because main is red since noema#115; the fix is noema#154 (open).
- Three-layer authority and ownership read · TRU-04 · no PR · **Backlog; Needs EVD-01** · on the path for T8 (not-owned looks like not-found).
- Decision Record, grants · TRU-05 · no PR · **Backlog; Needs REC-01**. Starts the moment REC-01 is Done.
- Confirmation · TRU-07 Needs TRU-05 · not on the US-01 and US-06 path (no CONFIRM).

## Pack and plan
- packcheck · PCK-02 · In progress · **blocker for EXE-01** (`Needs: PCK-02`). Decision of 3 Nov splits it: the coverage-check mechanism now; enforcement on the real pack moves to EXP-04. noema#149 (`d7c8fdc`, Ready, Codex READY) is the mechanism; it closes the blocker once merged.
- Action Graph shape · PCK-04 · noema#104 (`6d8a0e4`, held) · off the G1 path unless PLN-01 reads the graph. docs#88 is **merged**. Remaining: noema#148 (`ab35ac6`, draft, the beneficiary and account descriptors, E2) must merge, and the PCK-02 mechanism (noema#149).
- Registry, prohibited never callable · EXE-01 · noema#131 (`3845a22`, draft) · Needs PCK-02.
- Planner and canonical plan hash · PLN-01 · noema#142 (`4c64f5e`, slice 1, draft) · Needs EXE-01.

## Execution and Outcome
- Execution Controller · EXE-02 · **Done** (follow-up noema#137, `e1d7ca1`, non-draft, entropy failure is a typed error).
- Idempotency key · EXE-03 · In progress.
- Outbox · PLT-08 · In progress.
- Property tests INV-03/07/08 · TST-01 · noema#123 (`ff15b27`, non-draft) · In progress.
- Outcome Verifier, verify by key, US-06 end to end · OUT-01 · noema#136 (`fe8224e`, non-draft) · open. **DEBT-03 decision (3 Nov): it must authenticate the calling login before `outcome_record_verdict` runs (C2).**
- Recovery · RCV-01 Needs OUT-01 · Backlog.
- Template-only explanations · EXP-01 · no PR · Backlog (Needs OUT-01); US-01 requires the explanation to cite the masked account.
- Replay and run of both cases twice · BEN-05 · noema#102 (`a92f486`, draft) and BEN-29 suites noema#117/#138 · In review.

## The end-to-end test and its home
- Decision 8 of 3 Nov: repo-level `e2e/`, run by `make e2e` and a CI job. Not started; needs the public API (EDG-01 Backlog) and every node above. I write it with C1 and C2 once TRU-05 and OUT-01 have merged.

## Claims the G1 evidence may not make (Architect, 4 Nov 2026)
- **"Writer identity enforced"** is not true yet. A power's login can still write a Record event as a non-power emitter (`core-edge`, `core-record`, `admin`, `model-service`, `mock-*`). noema#145 binds the five power emitters to their own power's login; the rest waits for noema#145 to merge, then a migration (a login per such emitter, `core-edge` first; see `status/EDG-01.md` on noema#156). The evidence must say "power emitters bound to their logins (noema#145); non-power emitters not yet".
- **"Signatures verified"** is not true yet (DEBT-01 and DEBT-03 open; W3 and W4 not built).

## Updates since the snapshot above (4 Nov 13:00 UTC)
- Merged: noema#121 (TRU-03), #139, #130, #140, #149 (PCK-02 mechanism), #154, docs#87, docs#88. Ready with Codex READY: #133, #132, #150, #155, #157.
- Public API session · EDG-01 · noema#156, draft; slice 1 (the verifier) and slice 2 (the IdP issuer and the benchmark session provider) are in it; blocked on Codex (its claim-names finding is answered by OD-P-74 in docs#107, which the Architect accepts once Codex clears it).
- Writer binding for non-power emitters · after noema#145 merges.
- TRU-05 · still blocked on REC-01 (noema#122).

## noema#94 and noema#90
- **Not on the path.** They carry EXE-04 (migration 0002 and the void of a spent grant after a confirmed `REJECTED_STALE_VERSION`). Neither US-01 nor US-06 has a stale-version refusal. I did not touch them (instruction).
- What releases them: not known to me. They are Session A's (`sa/` branches); I only know the founders hold them. They do hold migration number 0002 while noema#107 uses 0003 and noema#136 uses 0004, so the ordering must be settled before either lands (db.Migrate applies by name, so the gap itself is safe).

## The shortest chain to G1, in order
1. REC-01 → TRU-05 (me, with C1 and C2). 2. PCK-02 mechanism → EXE-01 → PLN-01. 3. EVD-01 → TRU-04 and EVD-03. 4. OUT-01 with the DEBT-03 fix → EXP-01. 5. `e2e/` and BEN-05 runs. The longest unknown is EVD-01 (Backlog, C2, no PR).

## Refresh, 5 Nov 2026 (about 15:45 WAT)
**A snapshot, not a live view.** Heads and check results are on each PR. A check is stated only where I looked at it at the head named.

- **Merged today (from the work-queue order of 5 Nov, 15:30 WAT):** noema#145 (REC-03 and DEBT-04: tenant from the login, power emitters bound), #134 (REC-05 slice 1), #129 (EXE-07 slice 1), #136 (OUT-01, migration 0007), #160, #112, #165, #159, #135, #147. REC-01 (noema#122) merged on 4 Nov, so **TRU-05 is no longer blocked by REC-01**.
- **TRU-05 (mine), in three parts.**
  - noema#163 (`2e637b3`): grant issuer with HMAC over JCS and two-key rotation. Since 5 Nov it binds the exact `policy_id@version` (ruling of 5 Nov: versions compare only inside one policy_id), and Execution E4 voids a grant whose policy_id has another active version. CI at `2e637b3`: the first run had two jobs fail on a 504 while fetching CI tool binaries (`install_tools.sh`), re-run once; result read at the final head before Codex is asked.
  - noema#168 (`c9fe273`): `decision_record` (migration 0008) and the trigger that forbids a grant on a non-ALLOW decision. Main merged in; two tests that seed grants directly now record the ALLOW decision first. Same 504 on two `images` jobs, re-run once.
  - **Not built:** the Go side that writes the Decision Record, then the grants with their `GRANT_ISSUED` events in one transaction; `KEY_ROTATED`; key loading from a secret; TRU-08's void on a superseded basis version. It is stacked on both PRs above, so it starts after they merge.
  - **Migration number:** noema#167 (EVD-01) also uses 0008. The first to merge keeps it; the other renumbers to 0009 when it merges main.
- **EVD-01 slice 2a (noema#167):** Codex NOT READY at `5d18fec`: `evidence_rw` can insert rows for any tenant. This is DEBT-06 (Tier 1, every tenant table, close before G3); answered on the PR, decision requested.
- **PCK-04 (noema#104 and noema#148):** `contracts` is red at `da02a0b` only because `lookup_beneficiary` and `lookup_account` have no postcondition. The descriptors in #148 name `fin.src.core_ledger`, which the pack declares authoritative only for `debit_posted_or_reversed`; the Tushe worksheet defines no fields for either entity. Needs an Architect decision on source and fields. Not exempted.
- **EDG-01 (noema#156):** Codex NOT READY at `757331f`: OD-P-74 (docs#107, merged) is a NON-CANONICAL default and needs a committed adoption. Waiting on that.
- **Claims G1 evidence may not make** (unchanged): "writer identity enforced" for non-power emitters (DEBT-07 open); "signatures verified" (DEBT-01 and DEBT-03).

- **Update ~17:00 WAT:** noema#168 now carries migration **0009** (noema#167 keeps 0008). noema#104 is green locally with the ruling-D6 postconditions (beneficiary from `fin.src.beneficiary`, account ownership from `fin.src.customer`); account balance and state have no source and stay an open gap (Tushe request and Tier 1 debt row: the Steward files them). EDG-01 is Core Engineer 1's (handover on docs#107). The graph walk (PLN-01 slice 2, this PR, stacked on #104) is built: every emitted plan follows the declared edges.
