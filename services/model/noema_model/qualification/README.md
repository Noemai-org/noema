# noema_model/qualification

**What it does.** Two things for REQ-INT-006 (model-drift canary; D25, AGENT-28): `gate.py` refuses to start the service on a model ID with no QUALIFIED record or an unpinned version; `canary.py` sends the qualification subset through the service's own provider adapters and emits the run record (`qualification-run.v1`) with PASS, DRIFT (with the differing slots and the version that answered) or PROVIDER_UNAVAILABLE.

**Formats and rules** belong to the benchmark's `benchmark/qualification/` module (its README). The tests here run SYNTHETIC records and run records that module produced (`tests/fixtures/qualification/*_golden.json`) and require identical results. If the benchmark module changes a format, regenerate the fixtures; the tests fail until the two agree.

**Limits.** No held-out or partner case reaches this package (INV-22): a subset outside the GENERATED layer is refused before any call. The canary never decides that a drift is acceptable or that a version is qualified (that is the qualification record's decision record). No timeout is chosen here (AGENT-16): `run_canary` takes `timeout_s` from configuration. The prompt and schema for a case belong to the pipeline: `ProviderAsker` takes a `build` function.

**Wiring (not in this slice).** Calling `require_qualified` at service start with the configured providers, and running the canary on a version or provider change, are the service's start-up and operations wiring; they need the harness configuration to name a records directory and a subset, which is a configuration decision (OD below in the PR).

**Tests.** `pytest tests/test_qualification_gate.py tests/test_qualification_canary.py` from `services/model/`.
