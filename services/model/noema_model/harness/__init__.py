"""The only road to a model: versioned prompt templates, schema-forced output, masking and data-delimiting, per-call accounting, spans, timeouts, ordered failover, a circuit breaker per provider, and the generate and decide interfaces (D2, D9).

Implements: REQ-INT-002, REQ-CTX-007, REQ-NFR-007, REQ-OBS-002, REQ-BEN-005.

May import: noema_model.platform and noema_model.providers (it is the only importer of providers; the harness-only-providers lint).

Must never: repair a mismatching output (reject); turn a probability into a decision; log provider reasoning output; hold code that knows of the evil provider (endpoints are configuration); accept an unpinned model version.

Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with the engine tracks, and nothing is built here yet.
"""
