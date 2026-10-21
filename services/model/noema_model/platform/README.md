# noema_model/platform

**What it does.** Shared plumbing of the model service: configuration, telemetry, the mandatory redactor and mutual TLS (PERD §64.2). The folder is named platform on purpose; the service package noema_model keeps it from shadowing the standard library's platform module.

**REQs.** REQ-OBS-001, REQ-SEC-003, REQ-OPS-003.

**Status.** `clock` (the one place time is read), `config` (strict readers; nothing defaulted), `redaction` (`Redactor`: account numbers to the last four digits, secrets by key name) and `telemetry` (the `Telemetry` interface with null, in-memory and OpenTelemetry-span implementations) are built for the harness (T5). `mtls` builds the client side of the link to core (TLS 1.2 or later, server verified, client certificate presented). The Prometheus exporter and log shipping remain for REC-02 and PLT-05. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/` (`tests/test_harness_fields.py` covers the redactor).
