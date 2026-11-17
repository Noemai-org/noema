# Status · INP-01 · conversation continuity (design note)

Updated 2026-11-17 · Author: Core Engineer 2 (C2) · State: DESIGN NOTE ONLY. No code, no test, no contract change in this change. It decides nothing; every open point has an owner.

## Task card

- **Task:** INP-01. **Owner:** C2 + AG. **Week/date in `tasks.csv`:** 5 / 2026-11-18. **Needs:** UND-04 (the Interpreter and its port adapter: #661 open, the core Interpreter merged).
- **Done when (plan):** the PUS-08 case passes; only the latest version is used by the decision.
- **PERD REQ:** REQ-INP-004 (conversation continuity), REQ-INT-004 (versioning and supersession); dependency REQ-AMB-006. **SoT:** the Intent Object versioning in SoT §43.1 (as REQ-INT-004 cites it), INV-03, INV-17 (no stale grant).
- **Tests the PERD names:** US-11, EC-C03, EC-C01, EC-C02, PUS-08, and a property test for REQ-INT-004.
- **Allowlist (proposed):** `services/core/orchestrator/` (the turn routing for a follow-up), `services/core/understanding/` (version N+1 and `supersedes`), `services/core/understanding/intentstore` or the admitted-intent store (the optimistic lock), the edge's conversation route (`cmd/core/conversations.go`), their READMEs and this file. Nothing in `contracts/`, trust, execution or outcome internals; where those must change, the change is a separate task.

## What the requirements say

- **REQ-INP-004.** A follow-up (a clarification answer, a contradiction or a new request) is bound to the open investigation by `conversation_ref`, and is accepted only if the authenticated principal equals the investigation's principal. Understanding compiles the turn **in the context of the open Intent Object** and emits version N+1 with `supersedes`. A principal mismatch is treated as not found and raises a security event. An investigation in a terminal state starts a new one that links the old one `[REC]`. Audit event: `INVESTIGATION_RESUMED`. Acceptance: a resumed decision uses only evidence read after the resume (US-11).
- **REQ-INT-004.** Every change of meaning produces a new version with `supersedes`. Admitted versions are immutable. Only the latest admitted version may be evaluated. Version N+1 voids any decision, grant or confirmation tied to N (`INTENT_SUPERSEDED`). A concurrent supersession of the same version is resolved by optimistic locking on `(intent_id, version)`; the loser is recompiled. Audit: `INTENT_ADMITTED` (N+1) and `GRANT_VOIDED` (reason `INTENT_SUPERSEDED`). Acceptance: no grant is spent whose `intent_hash` is not the latest admitted version's hash; the Record links every version.
- **PUS-08.** "It was 50k, not 20k": the flag `CONTRADICTS_PRIOR_TURN` is raised, a new version supersedes the old, candidates are recomputed, and the decision uses only the latest version.

## What is on `main` today (read at `af7b6fde`, 17 Nov 2026)

| Piece | State |
|---|---|
| Version storage | `migrations/0026_admitted_intent.sql`: primary key `(tenant_id, intent_id, version)`, a check that version 1 has no `supersedes` and a later version has one, and a foreign key from `prev_version` to the same intent. So the store already **refuses a gap and a second version N+1 with the same key**: the optimistic lock of REQ-INT-004 exists as a constraint. |
| Contract | `intent-object` carries `supersedes: <intent_id>@<version>` (`CommonV1IntentRef`). The enum comment says SUPERSEDED status was removed: supersession is the link, not a status. |
| Canonical input | `conversation_ref` is an optional field typed `inv_` + 2..32 letters/digits (`InvestigationId`). The Interpreter port of #661 builds the canonical input and **does not set `conversation_ref`** (not in its tests either): today every turn is a first turn. |
| `conversation_ref` value | The same value as `investigation_id`: `inv_` + the ULID of the first turn's correlation id (A234, `cmd/core/conversations.go`). A follow-up carries the first turn's id in `conversation_ref` but gets its **own** correlation id. |
| Record vocabulary | `INVESTIGATION_RESUMED` is in `record-event` v1 and v2. `INTENT_SUPERSEDED` exists as a void reason in `execution/controller` and in the grant-void reason enum. |
| Model service | The pack (`packs/fintech_v1.json`) already lists `CONTRADICTS_PRIOR_TURN` as a proposable flag with its yes/no question, and the compiler's tests use it. **The compile request has no field that carries the prior intent**, so the model cannot see what was said before. |
| Not built | The edge route for a follow-up turn on an existing conversation; the lookup of the open investigation by `conversation_ref` and principal; the compile call "in the context of the open Intent Object"; the write of N+1 with `supersedes`; the void of N's decision, grant and confirmation; the `INVESTIGATION_RESUMED` event. |

## Proposed design

**1. Binding (edge).** A follow-up arrives as a turn with `conversation_ref` set. The edge resolves it, in one transaction, to the open investigation and its latest admitted intent **under the verified session's tenant and principal**. No row, a row of another principal, or a row of another tenant gives the same answer, "not found", and a security event (REQ-INP-004; no oracle for which). The principal is taken only from the session (INV-12), never from the turn.

**2. Terminal state.** If the investigation is in a terminal state, the turn starts a new investigation whose first Record event links the old one `[REC]`. Until the `[REC]` is adopted, the safe reading is to **refuse the follow-up with "start a new request"** (open point 1).

**3. Compile in context.** The compile call needs the open intent. The smallest addition is one optional member on the compile request, `prior_intent` (the latest admitted Intent Object, as the core stored it). It is data for the model, never an instruction (AGENT-09); the schema gate is unchanged. This is a **contract change** (AGENT-15: version bump, migration note, regenerated models, both Founders) and therefore its own task (open point 2).

**4. Supersession.** The compiler's proposal for the turn becomes version N+1 with `supersedes = <intent_id>@N`, written through the existing admitted-intent store. If the write loses the race (the key exists), the Interpreter recompiles once from the new latest version and does not retry blindly past that (a second loss is a refusal with a retryable code). Admitted versions are never updated.

**5. Contradiction flag.** `CONTRADICTS_PRIOR_TURN` is **proposed by the model and decided by the gate** like every ambiguity flag (INV-02): the model may propose it from the prior intent; the gate accepts it only if the pack lists it and the slot values of N+1 differ from N in a slot N had resolved. The deterministic half (a resolved slot changed value) does not need the model and is the part the tests prove; the model's yes/no is the extra signal. Open point 3 asks whether the deterministic rule alone is acceptable for the POC.

**6. Voiding.** On `INTENT_ADMITTED` of N+1 the core voids every decision, grant and confirmation tied to N (`GRANT_VOIDED`, reason `INTENT_SUPERSEDED`) in the same transaction as the admit, so there is no instant where N+1 is the latest and a grant of N is spendable. The grant spend path already refuses a hash that is not the latest admitted version's (REQ-INT-004 acceptance; to be confirmed by a test against the real controller, not assumed).

**7. Evidence after the resume.** US-11 requires that a resumed decision uses only evidence read after the resume. The Evidence port already reads per investigation; the design needs the snapshot to carry the intent version it was read for, so a snapshot read under N cannot be reused for N+1. Whether the existing `evidence_snapshot_id` and `intent_hash` pair in the decision key (`migrations/0027_decision_input_key.sql`) already enforces this is **not verified**; the first test of the task checks it.

## Tests first (each names its ID)

1. A follow-up with the right principal, right tenant and an open investigation compiles with the prior intent and stores N+1 with `supersedes` (REQ-INP-004, REQ-INT-004, US-11).
2. A follow-up with another principal, another tenant and an unknown `conversation_ref` give byte-identical "not found" bodies and a security event for the first two (REQ-INP-004, EC-C03).
3. PUS-08 end to end over the real store: 20k then "50k, not 20k" gives N+1, the flag, recomputed candidates, and the decision input names N+1 only.
4. Two concurrent follow-ups on N: exactly one N+1 is stored, the loser recompiles from N+1 (REQ-INT-004; real Postgres, EC-C01/EC-C02).
5. A grant minted under N is voided with `INTENT_SUPERSEDED` when N+1 is admitted, and the spend of it is refused (REQ-INT-004, INV-03, INV-17; Record events asserted: `INTENT_ADMITTED`, `GRANT_VOIDED`, `INVESTIGATION_RESUMED`).
6. Property (REQ-INT-004): for any sequence of follow-ups, the stored versions of an intent are 1..K without gaps, each with `supersedes` to its predecessor, and exactly one is the latest.
7. A follow-up on a terminal investigation is refused (or links, once `[REC]` is adopted).
8. Evidence read before the resume is not used by the decision of N+1 (US-11).

Faults: the store insert failing after the compile (nothing admitted, nothing voided), and the model service down (the follow-up fails closed with the open intent unchanged). Both come from the real store and a stopped service, not mocks.

## Open points (nobody has decided; none is answered by this file)

1. **Terminal-state follow-up.** The PERD marks "start a new investigation that links the old one" as `[REC]`. Adopt, or refuse? Proposed default: refuse until adopted. **Owner: Founder.**
2. **A contract change for `prior_intent`** on the compile request (and, if the model must see the prior turn's text, a second member). It needs both Founders (AGENT-15) and is not part of INP-01's allowlist. Is there an alternative that keeps the prior intent out of the compile request (for example the core compiles the new turn alone and the gate compares N and N+1)? That alternative is safer for the model boundary but cannot raise the model's `CONTRADICTS_PRIOR_TURN` question. **Owner: Architect.**
3. **Is the deterministic contradiction rule enough for the POC,** with the model's question as a later addition? **Owner: Architect.**
4. **Who voids.** The void of N's grants lives in execution (`controller`). INP-01 would call it through a port at admit time, or the controller would void lazily at spend time. The first is safer and crosses a module boundary through a contract; the second needs no new call but leaves grants of N in the table. **Owner: EL.**
5. **Rate and size of a conversation.** Is there a maximum number of versions per intent or turns per investigation? The PERD gives none that I found; I add none. **Owner: Founder if wanted.**

## Pre-work for the next agent

- Exact next step: when point 2 is answered, open the contract task (if needed); otherwise start with tests 2, 4, 5 and 6, which need no contract change, as one red PR, read the whole red run, then implement.
- Files to read first: `migrations/0026_admitted_intent.sql`, `migrations/0027_decision_input_key.sql`, `cmd/core/conversations.go`, `understanding/interpreter/interpreter.go`, `orchestrator/portadapters/interpreter/port.go` (#661), `execution/controller/controller.go` (lines 70 to 110), PERD REQ-INP-004, REQ-INT-004, PUS-08.
- Do not touch: `contracts/` (without the AGENT-15 steps), trust and outcome internals, the frozen `benchmark/decisions/`.

## Session 2026-11-17 · C2

- What changed: this file only (docs, `status/`). Debt entries: none. Conflicts raised: none.
- Why: the Founder's order for an INP-01 design note. I did not verify the decision-key claim in point 7 or the controller's spend check; both are named as the first things the tests check.
