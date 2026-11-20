# STM-01-writes-adapter · the production `Writes` adapter of the Trust intake (T3)

State: local branch `prep/r1-writes` (no push, no PR), on top of the open branch `feat/STM-01-trust-intents-killswitch` (noema#746), merged in. Built by Core Engineer 1's helper on 19 Nov 2026 (item 1 of the A341 to A357 batch; the rulings are relayed by the user in the session and are NOT committed, AGENTS section 2.3). Tier 1 (Trust): unreviewed helper work. Half built, on purpose: see "Blocked".

# Session 2026-11-19 12:30 WAT · STM-01-writes-adapter

## Session summary
- What changed (PR links): none (local commits only). Package `orchestrator/portadapters/trust` (`trustport`): new `writes.go` and `writes_test.go`; one doc comment of `killswitch.go`; a section in the package README; one line in `status/README.md`.
- Why: `intake.Writes` has two questions (T3): `Enabled` (the tenant's kill switch) and `NeedsWrite` (does the intent lead to a write; it decides the Planner's `WriteRequired`, and a plan with no steps is refused when `WriteRequired` is true, `gates_t13_t15.go`). #746 delivered the switch only. `cmd/core` cannot compose Trust without both.

### Built
- `trustport.Writes` (`NewWrites(sw WritesSwitch, rule WriteRule)`) implements `intake.Writes` (compile-time assertion).
  - `Enabled`: the switch's answer. Any error from the switch is `false` with that error, whatever the switch returned with it (never true on an error). `*KillSwitch` is the production switch.
  - `NeedsWrite`: `WriteRule.MayWrite(intent node)`. No node on the intent is `ErrStoredIntent`; a node the rule has no answer for is `ErrNoWriteAnswer` (the rule returns it); a failing rule and a done context are errors. In every error case the answer is `false`, and a rule's answer that comes with an error is not used. Core holds no node, industry or question word (INV-14): the answer is the rule's.
  - `WriteRule` and `WritesSwitch` are consumer-side interfaces. `ErrNoWriteAnswer` is the error a rule wraps for an unknown node, an unloaded pack or a node the pack does not classify (fail closed: never "no write", never "write").
  - A nil part, or a nil adapter, is `ErrConfig`.

## Blocked, and why (do not guess; Tier 1)
**The pack rule is not built.** The field `may_write` (ruling A342, relayed) does not exist anywhere: not in `contracts/schemas/intent-node.v1.json` (the node schema has `slots`, `trust_profile`, `outcome_definitions`, no write flag), not in `packs/fintech_v1.json`, `packs/README.md`, `tools/packcheck`, and not in the docs repository (searched `origin/main`, which holds the A329 to A340 record; A341 to A357 are not committed). Adding it touches `/contracts` and the pack (P0 paths, AGENT-15: version bump, migration note, regenerated types, packcheck), so I did not. Without it, `cmd/core` has no `WriteRule` to pass and `NewWrites` cannot be built there.

What the contract and pack change needs (for the Architect and the owner of `/contracts` and `packs/`):
- Name and type: a field on the intent node, relayed as `may_write` (boolean), required on every node so that a missing field fails closed (the reader refuses a node without it); `intent-node.v1` changes shape, so a new version per AGENT-15.
- The fintech pack has two nodes. `fin.meta.out_of_scope`: no slots, no trust profile, no outcomes: `false`. `fin.payments.exception.investigate`: see the doubt below.
- The pack-backed `WriteRule` (reads the pack bytes with the same strict generated contract types `StoredIntents` uses, not `readPack`'s local struct; an unknown node, nil pack or a node without the field is `ErrNoWriteAnswer`) is a small follow-up in this package once the field exists; the tests of this PR (table-backed rule, the scan test, the mutants) are written so it can reuse them with the real pack file.

**Doubt to raise with the Architect (the safer reading is to stop, not to pick).** `fin.payments.exception.investigate` is the one real node, and read versus write depends on the slot `question_type` (vocabulary `STATUS, WHY, RESEND_ADVICE, REVERSAL_STATUS, ESCALATION_REQUEST, CASE_STATUS, RECONSTRUCT`) and `requested_remedy`: DEC-2026-10-16 clarification C2 says question types are slots, never nodes. A per-node boolean cannot say it:
- `may_write = false` for the node: an escalation request (the create-case, complaint, evidence and routing steps; plan `WriteRequired`) is classified a read and T3 skips the kill switch for it. Unsafe.
- `may_write = true` for the node: every status question is a write: T3 refuses it when the switch is on (`WRITES_DISABLED`, over-blocking reads), and `WriteRequired` is true, so the plan for a read-only status question must have at least one step (`needsWrite == (len(p.Steps) == 0)` is refused). That breaks the read path.
- `AdmittedIntent` carries no slot values (the view has the slot statuses only), so a slot-value rule cannot be answered from it today either; the answer would need the stored document's `question_type` value, or the node split the C2 clarification forbids.
This is the same open point as `status/TRU-adapters-intents-writes-ownership.md` item 1, and `status/TRU-07-a306-per-step-policy.md` Q3. If A342 means a per-node boolean, it needs the Architect to say how the investigate node's read and write questions are told apart; I applied nothing.

## What `cmd/core` (EL) must pass
- `sw`: the `*KillSwitch` of `NewKillSwitch(pools, packRef, timeout)` (the same one `trust_rw` login per tenant as `NewStoredIntents`, `<pack id>@<version>` of the loaded pack, a timeout from configuration, AGENT-16).
- `rule`: a `WriteRule` over the loaded pack: not available until the field exists. Plug `Writes` as `intake.Ports.Writes`. A process whose pack failed to load has no `Writes`, so no write can be enabled and the intake does not run.

## Tests added (REQ, INV, US or EC IDs)
REQ-TRU-002 and REQ-NFR-002 (T3: an error is never enabled, never a read or a write), INV-01 (no model; the answer is data), INV-14 (the scan test: no string literal of `writes.go` names an industry, intent, status word or `may_write`; it parses the file, so a comment cannot hide or fake a hit). `Enabled` runs on real PostgreSQL (`internal/testpg`, the tenant's own login, a revoked table for a store failure); `NeedsWrite` on a table-backed `WriteRule` double: a data double of a consumer-side port, not a failure mode (AGENT-12 concerns injected faults, and the faults here are errors the real KillSwitch and the real pack reader produce or will). Seven tests: `TestWritesEnabledIsTheTenantsOwnKillSwitch`, `TestWritesEnabledIsNeverTrueOnAnError`, `TestNeedsWriteIsThePacksAnswerPerNode`, `TestNeedsWriteAsksTheRuleForTheIntentsNodeAndNothingElse`, `TestNeedsWriteFailsClosedOnNoAnswer`, `TestTheWritesAdapterIsNotBuiltWithoutASwitchOrARule`, `TestTheWritesAdapterSourceNamesNoIndustryOrIntent`. RED commit first (a stub answering `false, nil`; 6 of 7 tests failed on assertions), then GREEN.
- Regression cases added: none (no bug found in existing code).
- Debt entries: none filed (the Repo Steward files them); the pack rule is the open point above.
- Conflicts raised: none written as a CONFLICT block; the doubt above is a candidate against the C2 clarification if A342 is per node.

## Mutants (each applied, tested, restored with `git checkout`; `git diff` empty after each)
16 over `writes.go`, all killed: inverted answer (M1), rule error swallowed (M2), default true on a rule error (M3), wrong node asked (M4), rule ignored with false (M5) and with true (M6), empty node accepted (M7), switch error swallowed (M8), true on a switch error (M9), `Enabled` inverted (M10), always true (M11, first applied as a build break, so redone), tenant replaced by a literal (M12), no context check (M13), no nil check (M14), constructor accepts nil (M15), a node literal in the answer (M16, killed by the scan test).

## Pre-work for the next agent
- Exact next step: (1) Architect: commit A342 and rule on the investigate node (above). (2) Owner of `/contracts` and `packs/`: the field, the version bump and the regenerated types. (3) Here: the pack-backed `WriteRule` and its test over `packs/fintech_v1.json`. (4) EL: compose `Writes` in `wire_trust.go`.
- Open questions (owner): how read and write questions of one node are told apart (Architect); whether `NeedsWrite` may read a slot value of the stored intent (Architect).
- Files to read first: `orchestrator/portadapters/trust/writes.go`, `killswitch.go`, `trust/intake/gates.go` (t3), `gates_t13_t15.go`.
- Do not touch: `cmd/core` (EL), `trust/intake`, `/contracts` and `packs/` until the field is ruled.
