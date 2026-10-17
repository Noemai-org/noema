# contracts/schemas

JSON Schema 2020-12, one file per object, `<name>.v1.json`. `$id` is `https://contracts.noema.invalid/<name>.v1.json` (a reserved, never-resolvable host), and `$ref` uses the same base. The row for each file is in [`../INVENTORY.md`](../INVENTORY.md).

Conventions:

- Every object type states `additionalProperties`; it is `false` wherever the source says unknown fields are rejected.
- IDs are `<prefix>_<body>` (PERD §32.3). The wire pattern accepts 3 to 32 alphanumerics so the SoT examples and ULIDs both validate.
- Money is an integer in minor units, bounded 0 to 9007199254740991, never `number` (DEC-2026-10-16 money-and-value-lists M1 to M4). Ratios and confidences are `number`, and are not money.
- A value list is its own named type in `enums.v1.json`. Never reuse a list for a field it does not belong to (OD-P-10).
- Hand-written Go or Python contract types are not allowed. Codegen arrives with CON-03.

Run: `make contracts-validate`. A document written with a decimal point or exponent is never an `integer`, so `2000000.0` and `2e6` are rejected for a money field.
