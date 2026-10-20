"""Provider-neutral text-model adapters (GPT, Gemini, Claude) and decision-model adapters, with failover.

Implements: REQ-INT-002, REQ-BEN-005.

May import: noema_model.platform.

Must never: be imported by anything but the harness; depend on a single provider (BD-16); hold a credential for the core database, Temporal, adapters or grant keys (INV-29).

Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with the engine tracks, and nothing is built here yet.
"""
