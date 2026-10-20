# contracts

Canonical, versioned contracts (JSON Schema 2020-12, `<name>.v<major>.json`). Owner series: CON-01.

- `schemas/`: the schemas. Empty until CON-01.
- `fixtures/valid/` and `fixtures/invalid/`: one of each per schema.
- `fixtures/golden_vectors.json`: existing evidence-set hash vectors; see `fixtures/README.md`.

Changes need both founders (CODEOWNERS). A contract change follows AGENT-15: version bump, migration note, regenerated models.

**Vendored copies of docs files.** These files are copies of originals in `Noemai-org/docs`; the original wins on any difference. Each starts with a comment naming the docs commit it was taken from: `AGENTS.md`, `CLAUDE.md`, `contracts/state-registry.md` and `contracts/perd-32-2-event-types.md` (an extract of PERD section 32.2). One command re-points all of them to a clean docs checkout: `python3 tools/vendor/repoint_docs.py --docs <docs checkout>` (add `--check` to only report differences). The current PERD file for the extract is read from the docs checkout's `docs/poc/perd/README.md` (the row marked "Current."), so a PERD version bump needs no change to the script.
