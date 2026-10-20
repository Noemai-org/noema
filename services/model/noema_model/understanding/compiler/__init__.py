"""Compile canonical input into an Intent Object proposal using the pack's Intent Taxonomy; every proposal crosses the schema gate.

Implements: REQ-INT-001, REQ-INT-002, REQ-INT-003, REQ-INT-004, REQ-INT-005.

May import: noema_model.platform, noema_model.harness.

Must never: act; hold credentials; see policy rules or thresholds; set identity; compile to a node the pack does not define.

Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with the engine tracks, and nothing is built here yet.
"""
