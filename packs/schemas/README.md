# packs/schemas

The parameter schemas of the fintech pack's tools, one per **permitted** capability (`packs/fintech_v1.json`, `capabilities[].input_schema`, a pack-relative path from `packs/`). The eight prohibited capabilities have none: they offer no tool (BD-08, REQ-CAP-002).

**Rule (noema#362, agreed with the Integration Lead on 10 Nov 2026):** a schema holds only the fields a caller supplies, with `additionalProperties: false`. It never holds the tenant, the principal, or anything Trust or Execution derives (epoch, `created_by_intent`, version tokens, idempotency keys, payload hashes). Identity comes from the session (INV-12). The keys follow the pack's evidence descriptors (`transaction.id`, `account.id`, `case.id`, and the `transaction` key of `switch_event`, `settlement_record` and `beneficiary`); the case writes follow the stand-in's request bodies (`synthetic/mock_case/internal/api/openapi.json`) minus the derived fields.

Pack data, loaded and never imported as code (SoT §51). A change is a pack change, reviewed by a Founder (AGENT-16). Tests: `packs/tests/test_input_schemas.py`.
