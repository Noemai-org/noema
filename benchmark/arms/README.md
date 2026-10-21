# benchmark/arms

**Implements:** REQ-BEN-007 (competent, fair arms built after the core passes).
**Owning track:** T4 (DEC-2026-10-20): this README and the arm-manifest schema only. **No arm is built here, and none may be built until Noema passes its own gates.**

Comparison arms A, B and D, and the A0 diagnostic floor (SoT §38.5, PERD §50). Generated layer only: nothing here mounts, reads or infers anything from `benchmark-heldout` (INV-22, REQ-AGT-004).

## Why nothing is built yet

Building arms early invites tuning Noema against them and makes "Noema beats X" unprovable (architecture review §8, item 1). The core must pass its own gates (Phase D, plan task GOV-12) first. The task sequence is BEN-20 (freeze the scenario pack and the scoring rules, publish the arm brief), ARM-01 to ARM-04 (the arm builder, who is outside the build), ARM-05 (the independent reviewer signs each arm off), then BEN-24 (the comparison run). An agent session never builds an arm.

| Arm | What it is | Folder | Builder |
|---|---|---|---|
| A | A competent model with tools | `a_model_tools/` | ARM, outside the build |
| B | An agent plus harness plus the best equivalents of Noema's primitives: a policy engine, an idempotency store, verification reads, schema validation, retries, logging, **and a decision model as its gate** | `b_agent_harness/` | ARM |
| D | Deterministic rules and scripts an operations team would write | `d_rules/` | ARM |
| A0 | A naive model, a diagnostic floor, labelled "not a competitor"; never in a "Noema beats" claim | `a0_naive_diag/` | ARM, only if time allows |
| C | Noema itself | (not here) | the build |
| E, F | An automation product; human operations | (not here) | Written up as "not tested" unless access or partner data exists |

## The fair-comparison rules (SoT §38.5, REQ-BEN-007)

Every arm gets the same fixtures, oracle isolation, instructions, documentation, adapters, auth context, capabilities, model access and time budget. No arm gets Noema-specific information, and no arm can reach the oracle. Nothing a competent engineer would add is removed from A or B. Hard cases are never removed selectively and results are reported whichever way they go. Where an arm uses a stronger or weaker model, a second run with matched models is made so that a better model is not credited to Noema's architecture (PERD §50.2 run discipline).

## What must be recorded for each arm: `arm-manifest.v1.json`

`contracts/schemas/arm-manifest.v1.json` (fixtures in `contracts/fixtures/`) fixes the record:

- the arm, its status (`BUILT` or `NOT_TESTED`, with the reason) and the date its configuration was frozen;
- its configuration: the model and its pinned provider version (or no model, for D), the prompt or configuration version, the code version (a commit SHA), the pack and policy versions, the scenario version, the seed and the environment version, and the explicit statement that it had the same inputs as Noema;
- its effort: engineering hours, setup hours, lines of code, lines of configuration and the integration burden;
- the independent reviewer's sign-off that the arm is competent (the reviewer is not the builder, OQ-02).

`A0` must carry the label `NAIVE_DIAGNOSTIC_NOT_A_COMPETITOR` and no other arm may. An arm that was not tested has no configuration, effort or review, and no claim is made about it.

## Metrics compared (same oracle)

False confidence, unsafe actions, ambiguity handling, wrong-transaction attribution, timeout handling, duplicate handling, policy mutation, record completeness, time to outcome, intervention rate, the code and configuration burden, and cost. They are computed by `benchmark/scoring` from the same recorded result files as Noema's.
