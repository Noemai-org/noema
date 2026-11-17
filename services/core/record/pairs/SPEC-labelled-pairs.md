# MEM-03: labelled pairs by design (spec, for C1 and C2 to implement)

**Status:** a spec, 17 Nov 2026. No code. Task MEM-03 (owners C1, C2; due 1 Dec 2026), REQ-MEM-003 (P1), dependencies REQ-MEM-001, REQ-AUD-001; REC-03 is Done. Written by the Benchmark Owner agent for review by C1 and C2; nothing here is decided until they accept it.

## 1. What is asked

"An extraction query SHALL produce the three fields with provenance from a benchmark run, without reading anything outside the Record and memory" (REQ-MEM-003). The three fields are (request, resolved target, verified outcome), with tenant scope and provenance. The use is future training data for Understanding and Entity Resolution, **never** for Trust, Execution or the verdict (INV-01). **No extraction for training before GOV-25** (data rights). GOV-25 is Backlog, so this task builds the extraction and its proof, and leaves the training use switched off.

## 2. Proposal: a read-only view over the reconstruction, not a new store

Nothing new is stored. A pair is derived from the reconstruction of one correlation (`reconstruction.v3`, REC-03), which already reads the tenant's chain through `record_read` after verifying it, and which already projects only listed facts. This keeps the extraction inside "the Record and memory" by construction, adds no table and no write path, and inherits the integrity check (a chain that does not verify yields no pair).

| Field | Source section of the reconstruction | Notes |
|---|---|---|
| request | `REQUEST` | the typed facts the section already serves, not raw text (REQ-MEM-001: free text is not stored) |
| resolved target | `RESOLUTION` | the entity the resolver selected; absent if the case ended in clarification or refusal |
| verified outcome | `VERIFICATION` (and `DOWNSTREAM_RESULT` for the system's answer) | only a verdict from the Outcome power counts; an HTTP 200 never does (AGENT-21) |
| provenance | `chain.head_event_id`, `chain.head_hash`, the event ids of the three sections | so a pair traces to the exact Record bytes |
| scope | `tenant_id`, `correlation_id` | one tenant per call, taken from the authenticated session, never from input (INV-12) |

**A pair is emitted only when all three fields exist and the verification is a verified verdict.** A correlation with no resolved target, an `OUTCOME_UNKNOWN`, or a refused request yields no pair. It is counted, not silently dropped: the extraction reports how many correlations it skipped and why, by enum reason.

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
- **No pair without all three** (resolution absent, outcome unknown, refused): a table test now; a `rapid` property test once AGENT-14 approves the library.
- **Tamper:** break one Record event; the tenant yields no pairs and an integrity error (INV-11).
- **Tenant isolation:** a correlation of another tenant yields nothing, indistinguishable from "unknown".
- **Purpose gate:** `TRAINING` is refused; no code path writes the three fields anywhere outside the result.
- **Boundary lint:** no decision-path package imports `record/pairs` (extend the REQ-MEM-001 check).
- **Mutants to try:** emit a pair on `OUTCOME_UNKNOWN`; read the tenant from the argument instead of the session; allow `TRAINING`; drop provenance.

## 6. Questions for C1 and C2

1. Is the reconstruction the right source, or should the extraction read the chain through its own `record_read` view? This spec prefers the reconstruction for the verification it already does; the cost is one reconstruction per correlation.
2. Does `RESOLUTION` carry the selected entity as a typed fact today, or must the projection table (`project.go`) list a new key? A new key is a contract-adjacent change and needs its own PR.
3. Should a pair carry the case's `AMBIGUITY` facts (whether clarification was asked)? Useful for Entity Resolution labels; it widens the contract.
4. `rapid` is `[REC]` and needs AGENT-14 approval: is a table test enough for now?

## 7. Out of scope

Training itself; any use of a pair at decision time; storing pairs; an endpoint; the held-out set.
