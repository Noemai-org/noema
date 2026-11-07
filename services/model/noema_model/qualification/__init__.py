"""Model qualification in the model service: the drift canary and the gate that refuses an unqualified model ID.

Implements: REQ-INT-006 (model-drift canary), DEC-2026-10-31 D25, AGENT-28; INV-22 (nothing held-out reaches this service).

May import: noema_model.platform, noema_model.providers.

Must never: read the oracle or a held-out or partner case; choose a threshold, a timeout or a version (AGENT-16); decide that a drift is acceptable or that a version is
qualified (that is a recorded founder decision, the qualification record's `decision_record`); hold a credential of its own.

The formats and the lookup rules belong to the benchmark's qualification module (benchmark/qualification/README.md). This package follows them and is tested against records and run
records that module produced.
"""
