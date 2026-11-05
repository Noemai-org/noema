# Status · EXE-07 (Core Engineer 1) · W3, Ed25519 receipt signing in core-execution

Updated 2026-11-03. Plan task EXE-07: owner C1+AG; REQ-AUD-006, REQ-SEC-005; Needs GOV-28; done when "Signing and rotation tests pass; no key material in the database, logs or traces".

## State: slice 1 (the signer) in review as a draft; **must not merge before GOV-28 is approved**

Started against W1's current head (Noemai-org/docs#80, `a3196a6`, Proposed) on the Founder's order of 3 Nov 2026. If the design changes, this package changes with it.

## Session 2026-11-03 · EXE-07

### Session summary
- What changed (PR links): the EXE-07 slice 1 pull request on `feat/EXE-07-ed25519-signer`.
- Why: DEBT-01 closure criterion (4) needs signing and rotation tests; the critical path of G3 runs through W3 and W4.
- Risks, and how they are contained: it follows a Proposed design (it is a draft and not wired in); key handling is the sensitive part, so every refusal and every print path is tested and mutation-checked.
- Tests added (REQ-AUD-006, REQ-SEC-005): `execution/signing/signing_test.go`, eight tests.
- Regression cases added: none (no defect).
- Debt entries: none.
- Conflicts raised: none. One finding: a repeated member name can never equal its own canonical form, so the byte comparison already refuses it; the explicit duplicate check is defence in depth with a clearer error, and a test now observes it.

### Pre-work for the next agent
- Exact next step: slice 2, the controller and ledger wiring (receipt signature, `signed_jcs`), after GOV-28 is approved; then the golden vectors (a stdlib Python Ed25519 reference) and the key-material scan.
- Open questions (owner): the key file format (hex seed, one line) is my default for the deploy step; the Engineering Lead and Security Reviewer confirm it.
- Files to read first: `services/core/execution/signing/README.md`, Noemai-org/docs#80.
- Do not touch: noema#94, noema#90 (held, DEBT-01).
