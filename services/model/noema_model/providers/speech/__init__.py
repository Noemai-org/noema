"""Provider-neutral streaming speech adapters (Aethex AI, Indigenius, N-ATLAS).

Implements: REQ-VOI-002, REQ-VOI-003.

May import: noema_model.platform.

Must never: be imported by anything but the harness; depend on a single vendor (BD-16); pass a partial transcript as final.

Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with the engine tracks, and nothing is built here yet.
"""
