# contracts

Canonical, versioned contracts (JSON Schema 2020-12, `<name>.v<major>.json`). Owner series: CON-01.

- `schemas/`: the schemas. Empty until CON-01.
- `fixtures/valid/` and `fixtures/invalid/`: one of each per schema.
- `fixtures/golden_vectors.json`: existing evidence-set hash vectors; see `fixtures/README.md`.

Changes need both founders (CODEOWNERS). A contract change follows AGENT-15: version bump, migration note, regenerated models.
