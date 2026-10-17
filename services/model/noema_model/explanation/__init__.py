"""Renders plain-language explanations from facts Outcome has verified, with a claim validator and deterministic fallback templates (SoT §31; PERD §31).

May import: noema_model.platform and noema_model.providers.

Must never: create facts, change state, influence a verdict, write to the Record except its own output event, or be imported by decision, grant or verdict code; the core never imports it (INV-13).

An empty boundary package created by PLT-01; nothing is built here yet.
"""
