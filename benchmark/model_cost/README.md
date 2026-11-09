# benchmark/model_cost

Bench-only cost files for the model service (REQ-NFR-007). Added 8 Nov 2026 by the Architect's ruling A55 on noema#293: the writer lives in the benchmark tree and is handed to the service from outside; the service's runtime code reads no benchmark variable and holds no writer.

- `cost_files.py`: `CostFileTelemetry` wraps a telemetry object and also writes each `MODEL_CALL_RECORDED` record to `<out>/cost/<correlation_id>-<NNN>.json`. Standard library only. Exact names (an id outside `[A-Za-z0-9_-]` is refused, not rewritten), 999 calls per id, files created exclusively, and nothing written through a symbolic link at `<out>` or `<out>/cost` (INV-22).
- `model_app.py`: the entry point the bench compose override starts (`model_cost.model_app:app`). It needs `BENCH_OUT` and refuses to start without it. It imports the service's package, which the benchmark environment does not install, so `benchmark/pyproject.toml` has a mypy override ignoring the missing `noema_model` import; the service's tests load it.
- `test_cost_files.py`: the writer's tests (unittest, run by `make bench-test`). The service-side tests are in `services/model/tests/test_main_telemetry.py`: the service ignores `BENCH_OUT`, its runtime package holds no writer, the entry point writes the call record, and the compose override mounts only benchmark output and this directory (read-only), never held-out custody.

Run: `python -m unittest benchmark.model_cost.test_cost_files`.
