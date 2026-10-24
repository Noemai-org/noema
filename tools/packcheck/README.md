# tools/packcheck

Checks a pack before it is loaded (PCK-01, PCK-02; SoT 15.4, REQ-CAP-001, REQ-AG-001, REQ-AG-002).

## What is built

- `python tools/packcheck validate [--conformance] <pack.json>`: exit 1 on any problem. Checks: schema, references, capabilities, predicates (parse and type-check against the pack's evidence descriptors and predicate variables), graphs, routing, reason-code namespaces, and the conformance caps.
- `checks.py` holds every check (one code path for the command line, the tests and CI); `predicate.py` parses and type-checks predicates.
- Tests: `test_packcheck.py`, `test_predicate.py`; each crafted bad pack in `packs/fixtures/bad_packs/` names the check that must fail.

## Constraints

- Packs are data, never code (SoT §51). The grammar and corpus are in `contracts/predicates/`.
- Conformance packs skip only the presence rules for the `understanding` block.

## Open

- #59 adds the `understanding` and `matcher_ref` checks; they are not on `main` until it merges.
- The Go predicate evaluator (T6.1) must pass the same corpus.
