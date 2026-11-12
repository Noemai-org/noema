# Status · UND-04, model-service half (Core Engineer 2)

Updated 2026-11-12. Plan task UND-04 (owners C2 and C1; REQ-INT-001, REQ-INP-002). The core half is `status/UND-04.md` in noema#453; this file is separate so the two pull requests do not both add one file. PR noema#489.

## Session 2026-11-12 (C2)
- **What changed:** `services/model/noema_model/understanding/gate/` (`SchemaGate`, `Context`, `GateResult`, `Reason`, `PackView`, `Node`, `UNSIGNED`), `tests/test_intent_gate.py`, the READMEs of the package, of `understanding/` and of the service.
- **Failing first:** the tests-only commit `fc347984` (import error). The PR was opened on that head so its CI run is the red evidence.
- **Mutations by hand, each red:** 33 rules (status, schema version, both supersedes rules, the chain's intent id and previous version, created_at, tenant, principal, session, correlation, Mode 2, raw_ref, pack id, pack version, repeated flag, node, slot name, unknown slot, slot type, RESOLVED iff resolved_ref, window length, window instants, repeated outcome, outcome membership, model-set sig, empty body, size bound, repeated member, NaN, context, the VALIDATED flip, non-object). Five more after adding tests: recursion, strict UTF-8, the float guard. Three survived first: a lenient UTF-8 decode (a bad byte would be replaced silently), the float guard (`1e400`), and the unknown-member fuzz hitting `sig`, which is a contract member (a strategy error, fixed in the test).
- **A difference from the core gate, stated:** the generated Pydantic model bounds every Ratio, so a ratio outside [0, 1] is a `SHAPE` refusal here and a rule in Go (whose decode carries no bounds). A test pins it.
- **Known limit:** the generated model does not model `propertyNames` or the `supersedes` conditional; the gate carries those rules (`SLOT_NAME`, `SUPERSEDES`).
- **Conflicts raised:** none. **Debt for the Steward:** none beyond the open points in noema#484.

## Review P2 (12 Nov, noema#489)
- **Finding:** `_chain` read the digits after `@` in `supersedes` with `int()`; a version of more than 4,300 digits passes the contract's `IntentRef` pattern and makes Python raise `ValueError`, so the gate was not total.
- **Fix, test first:** `3 tests` at `ef4f02d0` (red: the reviewer's exact `ValueError`); then the gate's own pattern reads at most 16 digits (2^53 - 1 has 16), so a longer version is a refused chain (`SUPERSEDES`), never an exception. Mutation: restoring the unbounded pattern turns the new test red. The largest case is kept under the 64 KiB bound (a larger body is refused earlier as `SIZE`).

## Ruling A166 item 3 (12 Nov, docs#159)
- **What it requires:** the model's output carries no `principal_type` and no `asserted_by`; the core sets them; a model that sends them is refused as a shape error. My gate required and accepted both, which contradicted the ruling.
- **Fix, test first:** tests at `f219c096` (red: the new `UNSET` marker did not exist); then the gate refuses a proposal with either member (`SHAPE`) and fills both with `UNSET` for the closed decode. Mutations: removing the refusal, and removing the marker, each turn a named test red. The `Context` docstring and the README no longer call the two members "not compared".

## Pre-work for the next agent
- Exact next step: when noema#484 is answered, the compiler (prompt, provider call, assembly) and the route call `SchemaGate.check` and return the validated proposal; the core builds the v2 object, hashes it and signs it (ruling A166).
- Open questions (owner): noema#484 points 1 to 4 (Architect).
- Files to read first: `services/model/noema_model/understanding/gate/README.md`, `gate.py`.
- Do not touch: the generated models in `noema_model/contracts/`.

## Session 2026-11-12 (night) · UND-04 model half · C1's re-read at 3f7d818c (A166 items 1 and 5)
- **Findings:** the gate's docstrings, the README row for `sig`, the comment at `gate.py:172` and the comment on the `UNSIGNED` marker said the model service signs after the gate; A166 item 1 says the core hashes and signs after its gate and the model service never signs. And the README did not say which contract the model half validates against the object the core admits (v2).
- **Fix (words and comments only, no behaviour):** the module and package docstrings, the README row and paragraph, the marker's comment and the comment at the refusal of a model `sig` now say the core builds the object, hashes it and signs it; the `Context` docstring and the README's "Contract version" paragraph say the model half validates the v1-shaped proposal with the identity members marked `UNSET` and that the core builds the `intent-object.v2` object (choice (a) of C1's finding 2; a dedicated proposal contract would be an AGENT-15 change with C1, none proposed). The refusal of a model-supplied `sig` and its reason code are unchanged.
- **Checked:** the code of `gate.py` and `__init__.py` is identical with docstrings and comments removed (an AST comparison before and after); `pytest tests/test_intent_gate.py` 70 passed; `mypy --strict` clean. One push.
- Debt: none. Conflicts raised: none.
