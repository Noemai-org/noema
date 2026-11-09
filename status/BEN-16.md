# Status · BEN-16 (Benchmark Owner agent) · assemble the full suite; the suite definition v1

Updated 2026-11-07. Owner: B. Needs: BEN-06 to BEN-15 (generated half done; see below). REQ-BEN-001, REQ-BEN-004, REQ-BEN-007, REQ-BEN-009.

## State

- **Generated layer:** `python -m benchmark.generator coverage` on `main` (generator 1.3.0, seed 20261020): 320 cases, 0 gaps against PERD §48.1 (US 22 of 22, PUS 8 of 8, EC 105 of 105, fault families 29 of 29, capabilities 20 of 20).
- **Suite definition v1 (RESOLVE R1 to R12, RESIST S1 to S11 and X1 to X10, IMPACT):** mapped, one primary family per case, in docs#128. RESOLVE 175, RESIST 117, 8 Noema-only, 20 unmapped; only 7 of 33 families reach 15. About 340 new cases are needed. Not built.
- **Written in this change (specifications and data, no scorer or generator change):** `benchmark/generator/SPEC-suite-v1.md` (generator spec and oracle rule per partial or missing family, failing-first test names), `benchmark/scoring/METRICS-v1.md` (formulas, numerators, denominators), `benchmark/impact/operations-model.v1.json` (all parameters ASSUMED, ranges, M0), `benchmark/environment-capabilities-v1.md` (for IL), `benchmark/generator/x-family-review.md` (for SR).

## Open

1. Founders: docs#128 decisions 1 to 9, and the A26 record. R6 and R12 cannot be built as worded (prohibited capabilities).
2. The contract change set (case and oracle additions, the neutral arm result, two gates): `contracts/schemas/benchmark-arm-result.v1.json` is a separate pull request; the others wait for the Founders.
3. Environment seams (IL): facade, honeypots, canaries, kill-points, second tenant, descriptor hook, record export.
4. Pack owner: delegate, joint-account and approver rules (R7), and a limit rule if there is to be one (R8).

## Messages

### 2026-11-07 sent · Benchmark Owner agent → Integration Lead (IL)
- Purpose: the environment capabilities the suite definition needs, so the comparison arms and Noema face the same environment (ruling A10: IL owns the environment). The list is `benchmark/environment-capabilities-v1.md`: the arm-facing tool facade (no money-moving tool), per-arm identity, honeypot tools (X5), canary secrets (X10), kill-points (S9), a second tenant (X6), a poisoned-descriptor hook (X4), the simulated clock, oracle-side readouts and a record export.
- Asked back: what you can build, an estimate, anything that should not exist, and your names for the honeypot tools.
- Not sent to anyone outside the team. What the receiver used it for: (to be filled by IL).

### 2026-11-07 sent · Benchmark Owner agent → Security Reviewer (SR)
- Purpose: independent review of the X-family case set before it grows from 29 to about 150 cases. The set as it stands is `benchmark/generator/x-family-review.md` (every generated case whose primary family is X1 to X10, plus the 8 attacks on Noema's own model seam); the planned additions are in `benchmark/generator/SPEC-suite-v1.md` section 6. Please check coverage, oracle correctness, leakage of how Noema decides (INV-22), fairness for arms, and the honeypot and canary designs.
- Asked back: findings by case id or family; any attack class missing; any case that teaches how to defeat Noema.
- The cases are internal; none goes to the arm builders. What the receiver used it for: (to be filled by SR).

### 2026-11-07 accepted · Founder (instruction of 7 Nov, relayed by the user) → Benchmark Owner agent
- Purpose: map every family to what the pack has; write the generator spec and oracle rule for each missing or partial family, failing-first; write the exact metric definitions and the neutral result fields as a JSON Schema; write the Operations Model with every parameter ASSUMED; write the pre-registered reporting rules as decision-record text; send IL the environment capabilities and SR the X-family set; reconcile the Brief; send nothing to the external engineers.
- What I used it for: this change, docs#128 (the map), docs#125 (the Brief reconciliation, done earlier), the arm result schema pull request, and the decision-record text.
