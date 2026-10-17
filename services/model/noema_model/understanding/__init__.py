"""Turns canonical input into a schema-valid Intent Object proposal: the input gateway, the intent compiler and entity resolution (SoT §13, §14; PERD §19 to §22). May use models, only through providers.

May import: noema_model.platform and noema_model.providers (the only route to a model or speech provider).

Must never: act, hold credentials for the core database, Temporal, adapters or grant keys (INV-29), see policy rules or thresholds, assert identity (INV-12), or authorise anything; model output is untrusted data that crosses only the schema gate (INV-02).

An empty boundary package created by PLT-01; nothing is built here yet.
"""
