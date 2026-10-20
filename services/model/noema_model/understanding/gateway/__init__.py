"""Normalise text or a final transcript into the canonical input.

Implements: REQ-INP-001, REQ-INP-002, REQ-INP-003, REQ-INP-004, REQ-VOI-001.

May import: noema_model.platform, noema_model.harness.

Must never: hold business logic; decide; pass a partial transcript as final; assert identity (INV-12).

Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with the engine tracks, and nothing is built here yet.
"""
