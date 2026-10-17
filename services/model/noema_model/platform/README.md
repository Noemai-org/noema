# noema_model/platform

**What it does.** Shared plumbing of the model service: configuration, telemetry, the mandatory redactor and mutual TLS (PERD §64.2). The folder is named platform on purpose; the service package noema_model keeps it from shadowing the standard library's platform module.

**REQs.** REQ-OBS-001, REQ-SEC-003, REQ-OPS-003.

**Status.** Empty boundary package (PLT-01). Filled by REC-02 and PLT-05. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/` (none for this package yet).
