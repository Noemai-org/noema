# G1 critical path: US-01 and US-06 end to end through the public API

**Live view of `main` at `1d761926`, 11 Nov 2026 (Engineering Lead).** Replaces the 4 Nov snapshot, which was stale. It ages with every push: the live source for heads and checks is each PR. One line per node: `node · PR · owner · blocker`. "On main" means code is merged and tested; it does not mean the node is wired into the process.

**The headline.** `cmd/core` serves only `/healthz` and `/readyz` (`newMux`). No route of the public API exists on `main`, nothing composes the nodes into a conversation, and `TestTheCoreProcessDoesNotImportTheReconstructionHandler` keeps the reconstruction handler out of the process on purpose until the role check and tenant logins land.

## The vertical slice
1. `POST /v1/conversations` · none · C1 (EDG-01 slice 3b) · not started; needs 2.
2. Session middleware wired in `cmd/core` · #156 (verifier, IdP; open), #198 (A1 identity contract; Codex READY on its own range, re-reads asked) · C1 and EL · #156 merges only once #198 has every READY but the parent's (A125); then slice 3b wires `Middleware`, `RequireRole`, `CheckTenant`. `edge/session` is not on `main` yet.
3. Input gateway (plan UND-02) · none; no code in `core` or `model` · C2 · not started; **no PR and no owner confirmed**. Smallest unit: validate the body, build `canonical-input.v2` (#409 is the voice field), hand to 4.
4. Intent via the model service · none · C2 · the harness, ranking and matcher exist as libraries but the model service serves only `/healthz` and `/readyz`: **no intent endpoint and no core client**. #199 and #215 (UND-13 qualification gate; READY with reviews pending) must merge first so only a qualified model is used.
5. Trust gate · on `main`: decide, policy, grants, confirm. Open: #197 (sign the Decision Record; CI green, Codex READY, Founder approval only), then #237 (the writer; Codex READY at `717b8cf7`, C1 re-read asked), #259 (TRU-08 policy publication; conflicts with `main` in the README) · EL · #197 then #237 merge order (A113).
6. Execution controller · on `main`: controller, ledger, bridge, planner, registry · C1 · no caller; the workflow that runs plan, spend and dispatch for a conversation is not written (`workflows` holds only the escalation). Open: #243, #250, #234, #233, #261, #263 (audit gaps, Record hardening, signing).
7. Stand-in adapter · #205 merged (`StandinClient`, read ports) · IL and EL · merged; **not enabled in the bench profile**: nothing calls `NewStandinClient`, Compose gives the core no stand-in settings (noema#441). #221 (recorded replays) follows.
8. Outcome verify by key · on `main`: verifier, store, predicate · C2 · no caller; waits for 6 (it verifies what 6 dispatched).
9. Record · on `main`: chain, read, reconstruct · C1 · works; open hardening only: #263 signed events (stacked on #229, #214), #377 export (draft).
10. SSE events · none; no code, no PR, no task row · unowned · **needs a task and an owner**.
11. `GET /v1/reconstructions/{correlation_id}` · handler on `main`, not wired · C1 · needs 2 (role check) and the guard test removed in the same PR.
12. Composition: conversation state machine to calls · `orchestrator` (the table) and `workflows` on `main`, not connected to 3 to 8 · EL with C1 and C2 · **the largest gap: no task row**; needs a card before anyone codes it.

## What I do now (EL)
Nodes 5 and 12's design: #197, #237, #259 first; then the Trust-side call that 12 makes (decide, write the record and grants in one transaction), and the card for 12. Everything else goes to the owners named above; I do not wait on their reviews.

## Claims the G1 evidence may not make
- "Writer identity enforced": not until #181's emitter logins are used by the edge writer (slice 3b).
- "Signatures verified": not until #263 and #229 merge.
