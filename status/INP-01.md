# Status · INP-01 · conversation continuity (design note)

Updated 2026-11-23 (refreshed by the Repo Steward; see the session section at the end) · Author: Core Engineer 2 (C2) · State: DESIGN NOTE ONLY. No code, no test, no contract change in this change. It decides nothing itself; the five open points of 17 Nov were answered by the Architect's rulings A320 and A404 (below).

## Task card

- **Task:** INP-01. **Owner:** C2 + AG. **Week/date in `tasks.csv`:** 5 / 2026-11-18. **Needs:** UND-04 (the Interpreter and its port adapter: #661 and the core Interpreter are merged).
- **Done when (plan):** the PUS-08 case passes; only the latest version is used by the decision.
- **PERD REQ:** REQ-INP-004 (conversation continuity), REQ-INT-004 (versioning and supersession); dependency REQ-AMB-006. **SoT:** the Intent Object versioning in SoT §43.1 (as REQ-INT-004 cites it), INV-03, INV-17 (no stale grant).
- **Tests the PERD names:** US-11, EC-C03, EC-C01, EC-C02, PUS-08, and a property test for REQ-INT-004.
- **Allowlist (proposed):** `services/core/orchestrator/` (the turn routing for a follow-up), `services/core/understanding/` (version N+1 and `supersedes`), `services/core/understanding/intentstore` or the admitted-intent store (the optimistic lock), the edge's conversation route (`cmd/core/conversations.go`), their READMEs and this file. Nothing in `contracts/`, trust, execution or outcome internals; where those must change, the change is a separate task.

## What the requirements say

- **REQ-INP-004.** A follow-up (a clarification answer, a contradiction or a new request) is bound to the open investigation by `conversation_ref`, and is accepted only if the authenticated principal equals the investigation's principal. Understanding compiles the turn **in the context of the open Intent Object** and emits version N+1 with `supersedes`. A principal mismatch is treated as not found and raises a security event. An investigation in a terminal state starts a new one that links the old one `[REC]`. Audit event: `INVESTIGATION_RESUMED`. Acceptance: a resumed decision uses only evidence read after the resume (US-11).
- **REQ-INT-004.** Every change of meaning produces a new version with `supersedes`. Admitted versions are immutable. Only the latest admitted version may be evaluated. Version N+1 voids any decision, grant or confirmation tied to N (`INTENT_SUPERSEDED`). A concurrent supersession of the same version is resolved by optimistic locking on `(intent_id, version)`; the loser is recompiled. Audit: `INTENT_ADMITTED` (N+1) and `GRANT_VOIDED` (reason `INTENT_SUPERSEDED`). Acceptance: no grant is spent whose `intent_hash` is not the latest admitted version's hash; the Record links every version.
- **PUS-08.** "It was 50k, not 20k": the flag `CONTRADICTS_PRIOR_TURN` is raised, a new version supersedes the old, candidates are recomputed, and the decision uses only the latest version.

## What is on `main` today (re-read 23 Nov 2026 at `cca1ff55`; first read at `af7b6fde`, 17 Nov)

| Piece | State |
|---|---|
| Version storage | `migrations/0026_admitted_intent.sql`: primary key `(tenant_id, intent_id, version)`, a check that version 1 has no `supersedes` and a later version has one, and a foreign key from `prev_version` to the same intent. So the store already **refuses a gap and a second version N+1 with the same key**: the optimistic lock of REQ-INT-004 exists as a constraint. |
| Contract | `intent-object` carries `supersedes: <intent_id>@<version>` (`CommonV1IntentRef`). The enum comment says SUPERSEDED status was removed: supersession is the link, not a status. |
| Canonical input | `conversation_ref` is an optional field typed `inv_` + 2..32 letters/digits (`InvestigationId`). The Interpreter port (#661, merged) builds the canonical input and does not set `conversation_ref` (checked in `portadapters/interpreter/port.go`): today every turn is a first turn. |
| `conversation_ref` value | The same value as `investigation_id`: `inv_` + the ULID of the first turn's correlation id (A234, `cmd/core/conversations.go`). A follow-up carries the first turn's id in `conversation_ref` but gets its **own** correlation id. |
| Record vocabulary | `INVESTIGATION_RESUMED` is in `record-event` v1 and v2. `INTENT_SUPERSEDED` exists as a void reason in `execution/controller` and in the grant-void reason enum. `INVESTIGATION_RESUMED` is only in the vocabulary: no code writes it yet. |
| Model service | The pack (`packs/fintech_v1.json`) already lists `CONTRADICTS_PRIOR_TURN` as a proposable flag with its yes/no question, and the compiler's tests use it. **The compile request has no field that carries the prior intent** (no `prior_intent` anywhere in the Go, Python or contract sources), so the model cannot see what was said before. Per A404 it stays that way. |
| Not built | The edge route for a follow-up turn on an existing conversation; the lookup of the open investigation by `conversation_ref` and principal; the compile call "in the context of the open Intent Object"; the write of N+1 with `supersedes`; the void of N's decision, grant and confirmation; the `INVESTIGATION_RESUMED` event. |

## Proposed design

**1. Binding (edge).** A follow-up arrives as a turn with `conversation_ref` set. The edge resolves it, in one transaction, to the open investigation and its latest admitted intent **under the verified session's tenant and principal**. No row, a row of another principal, or a row of another tenant gives the same answer, "not found", and a security event (REQ-INP-004; no oracle for which). The principal is taken only from the session (INV-12), never from the turn.

**2. Terminal state.** If the investigation is in a terminal state, the turn starts a new investigation whose first Record event links the old one `[REC]`. **Ruled (A320, A404):** a terminal-state follow-up is refused and the customer is asked for a new request, until a decision record says otherwise.

**3. Compile (no contract change).** **Ruled (A320, A404): no `prior_intent` contract change.** The compile request keeps its present shape. The first version of this note proposed one optional member, `prior_intent`; that proposal is withdrawn. How Understanding compiles "in the context of the open Intent Object" (REQ-INP-004) without such a member is not decided by the rulings. The note's earlier alternative (the core compiles the new turn alone and the gate compares N and N+1) is the only path found that needs no contract change; it is a design for the Architect to confirm, not a decision.

**4. Supersession.** The compiler's proposal for the turn becomes version N+1 with `supersedes = <intent_id>@N`, written through the existing admitted-intent store. A concurrent supersession of the same version is resolved by optimistic locking on `(intent_id, version)`, and the loser is recompiled (REQ-INT-004, exactly as the PERD states it). Admitted versions are never updated. The first version of this note added a rule that the loser recompiles once and that a second loss is a refusal with a retryable code; **that rule is removed: the PERD does not state it and no ruling gives it** (nothing here invents a retry count or a code).

**5. Contradiction flag.** **Ruled (A320, A404): the contradiction rule is deterministic in the gate, and the model's flag is advisory.** `CONTRADICTS_PRIOR_TURN` is decided by the gate: it is accepted only if the pack lists it and the slot values of N+1 differ from N in a slot N had resolved (INV-02). The model's yes/no, where the model can see anything of the prior turn, adds a signal and decides nothing.

**6. Voiding.** **Ruled (A320, A404): no eager void (Founders' Block D, D8: lazy refusal at spend and execute).** Admitting N+1 does not rewrite or void N's decision, grant or confirmation. A grant minted under N is refused when it is spent or executed, because its `intent_hash` is not the latest admitted version's (REQ-INT-004 acceptance; PERD §28.2 E4: execution voids). The first version of this note voided N's grants in the admit transaction; that step is removed. The refusal at spend is to be confirmed by a test against the real controller, not assumed.

**7. Evidence after the resume.** US-11 requires that a resumed decision uses only evidence read after the resume. The Evidence port already reads per investigation; the design needs the snapshot to carry the intent version it was read for, so a snapshot read under N cannot be reused for N+1. Whether the existing `evidence_snapshot_id` and `intent_hash` pair in the decision key (`migrations/0027_decision_input_key.sql`) already enforces this is **not verified**; the first test of the task checks it.

## Tests first (each names its ID)

1. A follow-up with the right principal, right tenant and an open investigation compiles with the prior intent and stores N+1 with `supersedes` (REQ-INP-004, REQ-INT-004, US-11).
2. A follow-up with another principal, another tenant and an unknown `conversation_ref` give byte-identical "not found" bodies and a security event for the first two (REQ-INP-004, EC-C03).
3. PUS-08 end to end over the real store: 20k then "50k, not 20k" gives N+1, the flag, recomputed candidates, and the decision input names N+1 only.
4. Two concurrent follow-ups on N: exactly one N+1 is stored, and the loser is recompiled (REQ-INT-004; real Postgres, EC-C01/EC-C02).
5. A grant minted under N is refused at spend and execute once N+1 is admitted (lazy void, D8; reason `INTENT_SUPERSEDED`), and nothing is rewritten at admit time (REQ-INT-004, INV-03, INV-17; Record events asserted: `INTENT_ADMITTED`, `INVESTIGATION_RESUMED` and the void the controller writes at execution).
6. Property (REQ-INT-004): for any sequence of follow-ups, the stored versions of an intent are 1..K without gaps, each with `supersedes` to its predecessor, and exactly one is the latest.
7. A follow-up on a terminal investigation is refused and the customer is asked for a new request (A320, A404).
8. Evidence read before the resume is not used by the decision of N+1 (US-11).

Faults: the store insert failing after the compile (nothing admitted, nothing voided), and the model service down (the follow-up fails closed with the open intent unchanged). Both come from the real store and a stopped service, not mocks.

## Open points (all five of 17 Nov are answered; one new point is added)

1. **Terminal-state follow-up. Answered (A320, A404):** refuse and ask for a new request, until a decision record says otherwise.
2. **A contract change for `prior_intent`. Answered (A320, A404):** no `prior_intent` contract change.
3. **Is the deterministic contradiction rule enough for the POC? Answered (A320, A404):** the rule is deterministic in the gate and the model's flag is advisory.
4. **Who voids. Answered (A320, A404, D8):** no eager void; lazy refusal at spend and execute.
5. **Rate and size of a conversation. Answered (A404):** no cap.
6. **New: how the compile call gets the prior turn, with no contract change.** REQ-INP-004 says Understanding compiles the turn in the context of the open Intent Object, and A404 rules out `prior_intent`. Is "compile the new turn alone; the gate compares N and N+1" the intended design, or is there another path? **Owner: Architect.**

## Pre-work for the next agent

- Exact next step: points 1 to 5 are answered, so no contract task is needed. Point 6 is open (owner: Architect). Start with tests 2, 4, 5 and 6, which need no contract change, as one red PR, read the whole red run, then implement.
- Files to read first: `migrations/0026_admitted_intent.sql`, `migrations/0027_decision_input_key.sql`, `cmd/core/conversations.go`, `understanding/interpreter/interpreter.go`, `orchestrator/portadapters/interpreter/port.go` (#661), `execution/controller/controller.go` (lines 70 to 110), PERD REQ-INP-004, REQ-INT-004, PUS-08.
- Do not touch: `contracts/` (without the AGENT-15 steps), trust and outcome internals, the frozen `benchmark/decisions/`.

## Session 2026-11-17 · C2

- What changed: this file only (docs, `status/`). Debt entries: none. Conflicts raised: none.
- Why: the Founder's order for an INP-01 design note. I did not verify the decision-key claim in point 7 or the controller's spend check; both are named as the first things the tests check.

## Session 2026-11-23 · Repo Steward (refresh)

- What changed: this file only (docs, `status/`). The five open points are marked answered with their source (A320 and A404, `DEC-2026-11-18-architect-rulings-a317-a328` and `DEC-2026-11-20-architect-rulings-a358-a453`; D8 of Block D). The design is corrected to match: the `prior_intent` member is withdrawn (design 3); the invented retry rule is removed (design 4: the loser is recompiled, exactly as REQ-INT-004 says); the void at admit time is removed in favour of the lazy refusal at spend and execute (design 6, test 5); the terminal-state refusal is a ruling (design 2, test 7). The facts table is re-read at `cca1ff55` (#661 merged; no `prior_intent` anywhere). A new open point 6 asks how the compile call gets the prior turn.
- Not changed: C2's other claims, which were not re-verified (the decision-key claim in design 7 stays "not verified").
- Debt entries: none. Conflicts raised: none.
