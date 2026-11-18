# MEM-03: labelled pairs by design (spec, for C1 and C2 to implement)

**Status:** the final spec, 18 Nov 2026, updated to the Architect's ruling A322 (interim; recorded in docs#205, so it counts once a Founder merges that pull request). No code. Task MEM-03 (owners C1, C2; due 1 Dec 2026), REQ-MEM-003 (P1), dependencies REQ-MEM-001, REQ-AUD-001; REC-03 is Done. Written by the Benchmark Owner agent for C1 and C2 to implement. The four open questions of the 17 Nov draft are answered in section 6.

## 1. What is asked

"An extraction query SHALL produce the three fields with provenance from a benchmark run, without reading anything outside the Record and memory" (REQ-MEM-003). The three fields are (request, resolved target, verified outcome), with tenant scope and provenance. The use is future training data for Understanding and Entity Resolution, **never** for Trust, Execution or the verdict (INV-01). **No extraction for training before GOV-25** (data rights). GOV-25 is Backlog, so this task builds the extraction and its proof, and leaves the training use switched off.

## 2. Proposal: a read-only view over the reconstruction, not a new store

Nothing new is stored. A pair is derived from the reconstruction of one correlation (`reconstruction.v3`, REC-03), which already reads the tenant's chain through `record_read` after verifying it, and which already projects only listed facts. This keeps the extraction inside "the Record and memory" by construction, adds no table and no write path, and inherits the integrity check (a chain that does not verify yields no pair).

| Field | Source section of the reconstruction | Notes |
|---|---|---|
| request | `REQUEST` | the typed facts the section already serves, not raw text (REQ-MEM-001: free text is not stored) |
| resolved target | `RESOLUTION` | **a typed fact** (a `RESOLUTION` entity reference of the contract's own type, never a free string); absent if the case ended in clarification or refusal (A322) |
| verified outcome | `VERIFICATION` (and `DOWNSTREAM_RESULT` for the system's answer) | only a verdict from the Outcome power counts; an HTTP 200 never does (AGENT-21) |
| provenance | `chain.head_event_id`, `chain.head_hash`, the event ids of the three sections | so a pair traces to the exact Record bytes |
| scope | `tenant_id`, `correlation_id` | one tenant per call, taken from the authenticated session, never from input (INV-12) |

**A pair carries exactly the fields above and no `AMBIGUITY` facts in v1 (A322).** **A pair is emitted only when all three fields exist and the verification is a verified verdict.** A correlation with no resolved target, an `OUTCOME_UNKNOWN`, or a refused request yields no pair. It is counted, not silently dropped: the extraction reports how many correlations it skipped and why, by enum reason.

## 3. Interface (smallest viable)

- Go, package `services/core/record/pairs`: `Extract(ctx, db, tenant, correlations []string, purpose Purpose) (Result, error)`; `Result{Pairs []Pair, Skipped []Skip}`. It calls `reconstruct.Build` per correlation and opens no connection of its own.
- Contract: `contracts/schemas/labelled-pair.v1.json` (closed, `additionalProperties: false`, typed facts only, `tenant_id`, `provenance`), via codegen (AGENT-15). No free-text field.
- Surface: a function and a CLI under the benchmark profile for the acceptance test. **No HTTP endpoint** (the PERD section 39 defines none; adding one is a separate request).

## 4. Constraints (what must not happen)

1. **Never on the decision path.** The import-boundary check that REQ-MEM-001 requires also covers `record/pairs`: no `trust`, `execution`, `outcome` or `understanding` package imports it, and it imports none of them (it imports `reconstruct` only).
2. **No extraction for training before GOV-25.** The function refuses unless the purpose is `BENCHMARK_EVIDENCE`; a `TRAINING` purpose returns the typed refusal `PURPOSE_NOT_PERMITTED` until GOV-25 is Done and a decision record says otherwise.
3. **Tenant scope.** One tenant per call, from the session; a correlation of another tenant is "not found" (the reconstruction's existing single code).
4. **Synthetic only.** The acceptance run uses a benchmark run's generated cases; no partner data (PERD R-18).
5. **No oracle or held-out reads** (INV-22). The extraction reads the Record, which holds what the system did, not what the benchmark expected.

## 5. Tests (written first, seen failing)

- **Extraction on a benchmark run (the task's "done when"):** run a small generated suite through the stack, then `Extract` over its correlations. Every completed, verified, resolved case yields exactly one pair with all three fields and a provenance that matches the reconstruction's `chain.head_hash`; skipped cases are counted with a reason.
- **No pair without all three** (resolution absent, outcome unknown, refused): a **table test now** (A322): one row per skip reason and per missing field, each asserting no pair and the counted reason. No property-test library is needed for v1.
- **Tamper:** break one Record event; the tenant yields no pairs and an integrity error (INV-11).
- **Tenant isolation:** a correlation of another tenant yields nothing, indistinguishable from "unknown".
- **Purpose gate:** `TRAINING` is refused; no code path writes the three fields anywhere outside the result.
- **Boundary lint:** no decision-path package imports `record/pairs` (extend the REQ-MEM-001 check).
- **Typed target:** a `RESOLUTION` whose target is not of the contract's type yields no pair and a counted skip reason, never a coerced string.
- **No `AMBIGUITY` in v1:** the contract has no member for it and is closed (`additionalProperties: false`); a fixture with an `AMBIGUITY` section yields a pair without it.
- **Mutants to try:** emit a pair on `OUTCOME_UNKNOWN`; read the tenant from the argument instead of the session; allow `TRAINING`; drop provenance; copy `AMBIGUITY` facts into the pair; accept an untyped target.

## 6. Decided (ruling A322) and what is left to C1 and C2

| Question of the 17 Nov draft | Decision (A322) |
|---|---|
| Is the reconstruction the source, or the chain through `record_read`? | **The reconstruction is the source.** One reconstruction per correlation is the accepted cost. |
| Does `RESOLUTION` carry a typed selected entity? | **The target is a typed `RESOLUTION`.** If `project.go` does not list the key today, adding it is a contract-adjacent change in its own pull request (AGENT-15) before this one. C1 or C2 checks which. |
| Should a pair carry `AMBIGUITY` facts? | **No: `AMBIGUITY` stays out of v1.** Adding it later is a new minor version of `labelled-pair`. |
| Is a table test enough, or `rapid`? | **A table test now.** |

Left to C1 and C2, as implementation checks rather than open decisions: whether the typed-target key exists in the projection today (above), and the CLI's exact flags under the benchmark profile.

## 7. Out of scope

Training itself; any use of a pair at decision time; storing pairs; an endpoint; the held-out set.
