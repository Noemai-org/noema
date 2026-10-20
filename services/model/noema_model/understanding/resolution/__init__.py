"""Rank candidate targets from principal-scoped evidence by deterministic code: score = sum of weight times feature, with the composed recipient matcher (PERD v1.11).

Implements: REQ-AMB-002, REQ-AMB-003, REQ-AMB-007.

May import: noema_model.platform and the core Evidence API client.

Must never: call a model to rank; break a tie; pick a winner below the margin or the floor; search outside the principal's scope.

Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with the engine tracks, and nothing is built here yet.
"""
