# Session 2026-11-19 14:10 WAT · UND-05 · strict schema for slot mentions: no keyword beside a `$ref` (Core Engineer 2)

## Session summary
- What changed (local branch `fix/UND-05-strict-schema-no-ref-siblings` from main `cb77c746`; no PR yet):
  - `services/model/noema_model/understanding/pipeline/catalogue.py`: one Mention definition per slot type (`QuantityMention`, `PartyMention`, `TimeRefMention`), each carrying that type's help text in its own description, so a slot's property is a bare `$ref`. The Pydantic model, its config, the gate and the pipeline are unchanged.
  - `services/model/noema_model/harness/strict_schema.py` (new): `strict_schema_problems(schema)`, the one checker. `harness/core.py` `_schema_for` now uses it in place of the old `_strict_shape` (which it includes: all properties required, `additionalProperties` false), so a schema with a keyword beside a `$ref` is refused locally as `OUTPUT_TYPE_UNSAFE` and nothing is sent.
  - `services/model/tests/test_strict_schema.py` (36 tests) and `tests/fixtures/strict_schema/rec-0003-slot-mentions-format.json` (SYNTHETIC: the schema of recording rec-0003, copied as sent).
- Why: compile run 44521380842 (gpt-6-luna): all four `pipeline_slot_mentions` calls got HTTP 400 (body not recorded); the three slot properties were `$ref` plus a sibling `description`; the 13 `harness_decide` calls that were sent got 200 (finding A). HYPOTHESIS, not proven: the cause is the `$ref` siblings. The tests keep the shape out of every schema whatever the cause.
- Risks, and how they are contained: the text the model reads is the same two texts (type help, then the Mention text) in the definition instead of beside the `$ref`; a test asserts every fixture description is still present and that each slot reaches only its own type's help. The harness check is stricter than before (a `$ref` sibling, a bare `type: object`, a root that is not an object are now refused); every schema the service sends passes, and the full model suite (2,814 tests) and `mypy --strict` pass.
- Tests added (REQ-INT-002, INV-02): checker rules (each can fail), every schema the pipeline and every pack node send, the named schemas are exactly `harness_decide` and `pipeline_slot_mentions`, the rec-0003 fixture is flagged and the rebuilt schema is not and keeps its names and texts, the harness refusal.
- Regression cases added: `test_the_schema_of_recording_rec_0003_is_flagged_by_the_checker` and its rebuilt-schema twin.
- Debt entries: none.
- Conflicts raised: none.

## Silences (A332)
- The cause is a hypothesis: the 400 body was not recorded (a separate PR adds the error type, code and param). If the next run still gets a 400 on slot mentions, the cause is elsewhere.
- Choice: moved the description into a per-type referenced definition rather than inlining. Smallest change; the Pydantic model and the gate are untouched. The `Mention` class is kept as the base.
- Rules NOT enforced because not verified offline: unsupported per-type keywords (`pattern`, `format`, `minimum`, `maxLength` and the like), nesting-depth and property-count limits, `anyOf` and `allOf` placement, `$defs` beside a root `$ref`. The `$ref`-alone rule is enforced strictly (any sibling, `$defs` included).
- The adapters (`openai_chat`, `openai_responses`, `anthropic_messages`, `gemini_generate`) pass `ProviderRequest.output_schema` through; the tests read it at the provider seam. The pass-through of the Responses adapter is covered by `test_openai_responses.py`; the other adapters were not re-checked here.
- `status/README.md` is not updated (outside this PR's allowlist).
- Not run: any call to the provider (no network, no key).

## Mutants (26, run against `test_strict_schema.py`, `test_harness_call.py`, `test_pipeline.py`; each restored)
Killed (26 of 26 after 6 survivors were closed with new cases; the survivors were: `$ref` check skipped at the root, `type: "object"` misread, list `type` branch dropped, `"properties" in node` dropped, non-mapping `properties` accepted):
1. `$ref` sibling test needs more than 2 keys. 2. `$ref` test never fires. 3. `$ref` test skipped at the root. 4. `"object"` misspelt. 5. list-typed object not read. 6. `properties` without `type` not read as object. 7. required may be a superset. 8. `additionalProperties` true passes. 9. non-mapping `properties` accepted. 10. root rule disabled. 11. non-mapping root raises. 12. `properties` not read as a name table. 13. `$defs` not read as a name table. 14. `definitions` not read as a name table. 15. lists not walked. 16. wrong list index in the path. 17. wrong path for named members. 18. additionalProperties message dropped. 19. definition text without the type help. 20. definition text without the Mention text. 21. slots use the plain Mention. 22. every type gets the Quantity help. 23. definition not built on Mention. 24. description put back beside the `$ref`. 25. harness check disabled. 26. harness check inverted.

## Pre-work for the next agent
- Exact next step: after this lands, re-run the compile acceptance and confirm the slot-mentions calls no longer get 400; if they still do, read the (newly recorded) error type, code and param.
- Open questions (Architect): none; unverified strict-mode keyword limits are listed above.
- Files to read first: `services/model/noema_model/harness/strict_schema.py`, `services/model/noema_model/understanding/pipeline/catalogue.py`, `services/model/tests/test_strict_schema.py`.
- Do not touch: `contracts/`, `packs/`, the benchmark runner.
