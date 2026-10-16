# Packs

> Pre-PCK-01 port. Authoritative today: the 20 capability descriptors and the Action Graph. Being rewritten to SoT §15.3 shapes (DEC C2, C3): intent nodes, trust profiles, outcome definitions, evidence-source descriptors and policy rules. Do not build against those sections yet.

Here, "DEC C2, C3" is [DEC-2026-10-16 (contract and pack clarifications)](https://github.com/Noemai-org/docs/blob/main/docs/decisions/DEC-2026-10-16-contract-and-pack-clarifications.md), items C2 and C3, once that record is merged in `Noemai-org/docs`; the rewrite is task PCK-01, with `packcheck` extended by PCK-02.

Packs are loaded as data, never imported as code (SoT §51). The files are `fintech_v1.json`, the three conformance packs in `conformance/`, and the eight crafted bad packs in `fixtures/bad_packs/`. Validate one with `python3 -I tools/packcheck.py validate <pack>`.
