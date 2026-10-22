"""Shared plumbing of the model service: configuration, telemetry, the mandatory redactor and mutual TLS (PERD §64.2). The folder is named platform on purpose; the service package noema_model keeps it from shadowing the standard library's platform module.

May import: the standard library and the declared dependencies.

Must never: log a secret, a full account number, raw audio or an unredacted transcript (AGENT-13); read the wall clock outside its clock module; hold a core-database, Temporal or grant credential.

Boundary package created by PLT-01. Its sub-packages are filled by the engine tracks; see the README beside this file for what exists.
"""
