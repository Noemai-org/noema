"""The model service as the bench stack runs it: the same app, with the cost-file telemetry handed in from outside (Architect's ruling A55 of 8 Nov 2026).

Loaded only by `deploy/compose/bench-model.yml`, which mounts this directory read-only into the model container as the package `model_cost` and starts
`model_cost.model_app:app`. It needs BENCH_OUT (the bind mount of the bench output directory) and refuses to start without it, so a bench run cannot silently run
without cost files. The service's own code never sees BENCH_OUT. This file is excluded from the benchmark's type check because it imports the service's package, which the
benchmark environment does not install; the service's tests load it (services/model/tests/test_main_telemetry.py)."""

import os
from pathlib import Path

from noema_model.main import build_telemetry, create_app

from .cost_files import CostFileTelemetry

app = create_app(telemetry=CostFileTelemetry(build_telemetry(), Path(os.environ["BENCH_OUT"])))
