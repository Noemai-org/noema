# noema_model/platform

**What it does.** Shared plumbing of the model service: configuration, telemetry, the mandatory redactor and mutual TLS (PERD §64.2). The folder is named platform on purpose; the service package noema_model keeps it from shadowing the standard library's platform module.

**REQs.** REQ-OBS-001, REQ-SEC-003, REQ-OPS-003, REQ-TST-010.

**Status.** `blocking` (`BoundedBlockingPool`: the bounded worker threads a transport runs its blocking HTTP call on; a full pool refuses at once, UND-12), `clock` (the one place time is read), `config` (strict readers; nothing defaulted), `redaction` (`Redactor`: account numbers to the last four digits, secrets by key name; the runs it masks are the ones `redaction/digit_runs.py` finds, the model service's one definition of a digit run; a string that is wholly an amount such as `NGN 12,345.00` is left alone, `redaction/money.py`, A279/A270 interim) and `telemetry` (the `Telemetry` interface with null, in-memory and OpenTelemetry-span implementations) are built for the harness (T5). `mtls` builds the client side of the link to core (TLS 1.2 or later, server verified, client certificate presented). The Prometheus exporter and log shipping remain for REC-02 and PLT-05. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/` (`tests/test_harness_fields.py`, `tests/test_redactor_separators.py`, `tests/test_redactor_digit_runs.py` and `tests/test_digit_runs.py` cover the redactor; `tests/test_digit_run_vectors.py` gives the verdict of every vector of the shared file `contracts/fixtures/digit_run_vectors.json`, the same file the Record guard and the UI scan are tested against, A259 interim; the assembler's `_surface` is tested against the same file, A308 interim).
