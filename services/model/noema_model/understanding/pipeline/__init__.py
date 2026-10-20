"""Stateless pure-function steps composed as a fixed DAG: normalise input, compile the node and slots, run the schema gate, request candidates, rank deterministically (D3).

Implements: REQ-INT-001, REQ-AMB-003, REQ-NFR-001.

May import: noema_model.understanding.{gateway,compiler,resolution}, noema_model.harness, and the core Evidence API client.

Must never: hold workflow state or module-level mutable state; import a database, Temporal, HTTP or stateful agent-framework client (the pipeline-purity lint); decide; write a clarification question; check policy.

Created by T1 of DEC-2026-10-20 (PERD v1.11 §64.2). An empty boundary package: its contents arrive with the engine tracks, and nothing is built here yet.
"""
