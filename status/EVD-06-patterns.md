# Status · EVD-06 patterns (Core Engineer 2) · the injection patterns pinned and extended

Updated 2026-11-24.

## State
- A645 (interim, committed in `DEC-2026-11-24-architect-rulings-a644-a656`) gives the injection-screen patterns to C2.
- Built (A664 round: version-gated): the patterns file is a manifest kind (`injection_patterns`, `expected.injection_patterns_id`), required from manifest version 4 and optional below it (the A579 pattern), hash-pinned and returned verified by `platform/packload` as `Pack.InjectionPatterns`; packcheck and the manifest schema carry it; four patterns added (`fin.inj.model_marker`, `fin.inj.mode_switch`, `fin.inj.command_line`, `fin.inj.encoded_blob`). Existing ids and regexes are unchanged (they are in the Record).

## Open
- The shipped manifest stays at version 2 until noema#910 (limits, version 3) merges; landing order #910, #964, #965, #963 (A664). After #910 lands: merge main, set the shipped manifest to version 4 and recompute its pins; nothing else changes. Until then the gate is exercised by tests on synthetic manifests of version 3 and 4.
- Without pattern data (a manifest below version 4 that lists none) the core builds no screen from the pack and the payload reader refuses a read whose screen is missing. There is no built-in minimum in the code; if the Architect wants one, say where it lives (it cannot be Go code with patterns without a decision: INV-14, AGENT-16).
- `fin.inj.encoded_blob` flags any 40-character base64-looking run, so a long identifier or hash in a field is a false positive. Advisory only (SoT §29); recall and false-positive rate are reported, not gated.
- The call site of the screener and the home of the 256-character cap are still open (see `status/EVD-06.md`).

## Messages
- 2026-11-24 · Founder → C2: "you own the Candidates read (EVD-04) and the injection-screen patterns" (A645).

# Session 2026-11-24 · EVD-06 patterns

## Session summary
- What changed (PR links): the patterns file pinned in the manifest, loaded through packload, extended (this PR).
- Why: A645.
- Risks, and how they are contained: a manifest of version 4 or later that lists no patterns is refused by the schema, packcheck and the loader; an older one loads with no pattern data and no screen can be built from it (no empty Detector); a regex that is not RE2 or can match the empty string is refused by the existing parser; the screen reads at most the cap, so a hostile value costs a bounded amount (allocation test, no stopwatch).
- Tests added (REQ-CTX-007, REQ-SEC-007, INV-14, AGENT-09): the four new patterns against injected and benign strings; packload refuses a changed, missing, unlisted or wrong-id patterns file; packcheck and schema tests.
- Regression cases added: none (no bug fixed).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: the composition passes `Pack.InjectionPatterns` to `injection.Parse` with the expected id; wire the screener into the evidence read path.
- Files to read first: `services/core/evidence/injection/README.md`, `services/core/platform/packload/README.md`.
- Do not touch: existing pattern ids (they are in the Record).
