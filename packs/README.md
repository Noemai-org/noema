# Packs

The four packs are written in the SoT 15.3 shapes against the CON-01 pack schema (`contracts/schemas/pack.v1.json`): `fintech_v1.json` and the three conformance packs in `conformance/` (telecom, insurance, healthcare administration; SoT 15.4 cap: at most 3 intent nodes, 1 trust profile, 1 outcome definition, no adapters, no executed capabilities). PCK-01.

All content is SYNTHETIC. Values the SoT or PERD give are used as given. Where a predicate names a field, the field vocabulary is not yet defined: predicates are parsed by `internal/predicate` (T6.1) and type-checked by `packcheck` (PCK-02), and stay unconfirmed until then.

Packs are loaded as data, never imported as code (SoT §51). The files are `fintech_v1.json`, the three conformance packs in `conformance/`, and the eight crafted bad packs in `fixtures/bad_packs/`. Shape tests: `python -m unittest discover -s packs/tests` (run by `make contracts-validate`).

## Action Graph, packcheck and the predicate language (T2)

- The fintech Action Graph is pack data in `fintech/graph/` (G1 investigate, G2 escalate, G3 complaint; PERD section 24). The pack lists the files in `action_graph_files`.
- `python tools/packcheck validate [--conformance] <pack>` runs every check: schema, references, capabilities (REQ-CAP-001), predicates (parse and type-check against `evidence_descriptors` and `predicate_variables`), graphs (REQ-AG-001), routing (REQ-AG-002) and reason-code namespaces. `fixtures/bad_packs/` holds one patch per failing case; each names the check that must fail.
- The predicate grammar and the shared corpus are in `contracts/predicates/`. The Go evaluator (T6.1) must pass the same corpus.

## Status (Session A, 24 Oct 2026)

- **Built:** the four packs, the fintech Action Graph and the `packcheck` checks. #59 (open) adds the `understanding` block and `matcher_ref`.
- **Constraints:** packs are data, loaded never imported; both founders review changes; all content is synthetic.
- **Open:** non-author founder review on #59; OD-P-69 (the format of `counterparty_display`); OD-P-70 (the pack directory is mounted read-only in compose, a default waiting for the Founder's "yes").
