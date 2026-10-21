# benchmark/generator

**Implements:** REQ-BEN-001 (T4 item 3 of DEC-2026-10-20). **Owning track:** T4 (benchmark owner, B).
**Updated:** 2026-10-21.

`noema-casegen`: the versioned, seeded case generator for layer A of the benchmark (PERD §48.1). The same generator version and seed give byte-identical cases, oracle documents, fixtures, fault plans and manifest. SYNTHETIC (AGENT-25). It never mounts, reads or infers anything from `benchmark-heldout` (INV-22, REQ-AGT-004).

## Use

```
python -m benchmark.generator generate    [--seed N] [--out DIR]   # writes the suite; exits 1, writing nothing, on a coverage gap
python -m benchmark.generator coverage    [--seed N]               # the coverage report only
python -m benchmark.generator assumptions                          # the INFERRED expectations, for the benchmark owner
```

The default seed is `20261020`; the default output root is `benchmark/out/generated` (git-ignored). The suite root holds `cases/`, `oracle/`, `fixtures/`, `fault_plans/` and `manifest.json`. A case's `known_facts_fixture` and `oracle_ref` are paths relative to that root.

## What it makes

320 cases in the ten PERD §48.1 categories, in the table's quotas. Per category, every archetype gets a base case (`BEN-<code>-0001` and up), the remaining quota is filled with further variants of the archetypes (different names, amounts, ids and wording), and up to 30% is mutated cases (`BEN-<code>-1001` and up; PERD §48.4). Wording cycles Pidgin, English, code-switched, Yoruba, Hausa and Igbo, so more than 30% of cases are Pidgin or code-switched.

| File | Role |
|---|---|
| `catalogue.py` | the US, PUS, EC and fault lists and the category quotas, vendored from PERD v1.11 (§11.2, §12, §37.1, §48.1) |
| `worlds.py`, `rng.py` | scenario worlds from a declarative spec; deterministic choices from a SHA-256 counter, never `random` |
| `archetype.py` | `Arc` (one scenario and its expectation) and `instantiate` |
| `lib_*.py`, `library.py` | the 140 archetypes, by category; each names the PERD rows it covers and says whether its expectation is `PERD` or `INFERRED` |
| `mutations.py` | 13 mutations, each a transform with a declared effect on the expectation |
| `generate.py` | the plan, the documents, the manifest |
| `coverage.py` | the coverage report |

## Coverage rule

The report covers every user story, supporting story, edge case, fault effect (and `FAULT:NONE`), and every capability in the pack, allowed and prohibited (read from `packs/fintech_v1.json` at generation time), plus families B1 to B7, the category quotas, the Pidgin share, the four mutation kinds and the four attack families. A tag no list knows is a gap too. B8 (business metrics) is computed across all cases, so no case belongs to it alone. **Any gap fails the build** (`generate` and `coverage` exit 1).

## Where the expectations come from

Each archetype's expectation is the PERD §11.2 / §12 row, column by column. Where the PERD allows two outcomes both are listed (disposition plus acceptable alternatives); no expectation loosens a PERD "must never". Where the PERD does not settle it, the archetype is marked `INFERRED`, the safer reading is taken, and `assumptions` lists it for the benchmark owner to confirm (AGENT-02). Expected writes are exact: an escalation on a resolved target is a case, its evidence packet and its routing (graph G2), counted by capability, target and number.

## Mutations

Implemented: `ADD_NEAR_DUPLICATE_CANDIDATE`, `PERTURB_AMOUNT` (ambiguity); `FLIP_SOURCE_UNAVAILABLE`, `FLIP_SOURCE_STALE`, `FLIP_SOURCE_CONTRADICTORY`, `ADD_LATE_REVERSAL` (state); `POLICY_PUBLISH_AT_STEP` (temporal); `TIMEOUT_AFTER_COMMIT`, `HTTP_5XX` (recovery); and the four attack families `NAME_COLLISION`, `INJECTION_IN_NAME`, `CANDIDATE_FLOODING`, `CROSS_PRINCIPAL_PROBE`.

Named by PERD §48.4 and **not implemented**, because no PERD row states what the expectation becomes: `ALIAS_NAME`, `SHIFT_TIME_WINDOW`, `ADD_DUPLICATE_DEBIT`, `CLOCK_SKEW`, `WEBHOOK_DELAYED_PAST_DEADLINE`, `SESSION_EXPIRY_MID_FLOW`, `HTTP_429_BEYOND_GRANT_EXPIRY`, `VERSION_CONFLICT`, `RESTART_AT_STEP`. Each is a one-function addition once the benchmark owner states the effect.

A mutated case keeps its base archetype's key, so it shares the base case's names, ids and wording; only the declared edit differs. Its manifest entry names the base case and the mutation.

## Fault plans

A case with faults names `fp_<case id>`; the plan file under `fault_plans/` is in the PERD §37.1 shape and is **provisional**: the fault-plan schema belongs to the environment track and replaces this file's format when it lands.

## Tests

```
make bench-test       # or: python -m unittest discover -s benchmark/generator -t . -p 'test_*.py'
```

`test_generate.py` proves determinism (byte-identical, a golden manifest digest), the quotas, case-id numbering, the Pidgin share, schema validity of every output, and oracle, fixture and fault-plan consistency. `test_coverage.py` proves the report passes on the suite and fails when a story, a capability, a category or the Pidgin share is removed. A deliberate change to the suite bumps `GENERATOR_VERSION` and the pinned digest in one commit.
