# ui/src/contract_types

Generated TypeScript types and parsers for the contracts the UI reads (REQ-UI-001, REQ-API-001, AGENT-15). **Nothing here is edited by hand**: `python3 -I tools/codegen/generate.py --write` regenerates it from `contracts/schemas`, and `--check` (run by CI) fails when a file differs or was added by hand.

- Which contracts: `tools/codegen/ts.json` (the roots the UI reads); the contracts they refer to are generated with them.
- `parseX(value: unknown): X` for every type: it refuses what the schema refuses at the shape and at the bounds a parser can hold (JSON types, required and unknown members, enums, constants, whole-string patterns, string lengths in code points, number bounds, list lengths, `oneOf`). Conditional rules stay with the JSON Schema validator, as in the Go and Pydantic output. A `ContractError` names the place and the rule, never the value.
- The UI parses every response and every SSE event before it shows anything, so it shows only what a contract admits and never anything outside it (PERD §40.4).
- Tests: `ui/test/contracts.test.mjs` runs every valid and invalid fixture of these contracts through the parsers. Run: `cd ui && npm test`.
- Entry points (A258): import from `customer.ts` in a customer-facing page (the four public contracts and `errors.v3`) and from `operator.ts` only in the operator and auditor UI (adds `reconstruction.v3` and `record-event.v3`, which carry `tenant_id`, the full events and their hashes). There is no `index.ts`.
