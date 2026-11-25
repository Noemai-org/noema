# packs/fintech/routing

**Implements:** REQ-CAP-001, REQ-AG-001, REQ-AG-002, REQ-NFR-005.
**Owning task:** PCK-01 (the rewrite of the fintech pack) and, for `graph/`, T2 of DEC-2026-10-20; PCK-09 for `priority_routing.v1.json`.

Will hold the fintech pack's routing allowlist. Packs are data, loaded and never imported as code (SoT §51). The allowlist is still in `packs/fintech_v1.json` until PCK-01 replaces it.

**`priority_routing.v1.json` (PCK-09; ruling A644, interim, relayed, not yet committed).** The pack's priority routing member: for each SLA phase the pack names (`predicate_variables.sla_phase`), a `priority` (`HIGH`, `NORMAL` or `LOW`) and an `owner_queue` (a queue of `routing_allowlist`). Every phase must have an entry: packcheck fails a pack that misses one (A660, interim, relayed, not yet committed), and `Lookup` for an unlisted phase gives the zero entry and false, never a silent `NORMAL`. Schema: `packs/schemas/priority_routing.v1.json`. Pinned in the manifest with kind `priority_routing` (at most one). Checked by `tools/packcheck manifest`; read through `platform/priorityrouting` from the bytes `platform/packload` verified. All values are SYNTHETIC and are not a Founder-approved policy.

Created by T1 of DEC-2026-10-20.
