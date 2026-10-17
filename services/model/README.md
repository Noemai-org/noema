# services/model

The Python model service (the Go core is the other deployable). It hosts Understanding, Explanation and Voice, and is the only place with a model or speech provider SDK or credential (INV-29; SoT §35.6, PERD §64.2).

**Status (PLT-01).** Boundary packages exist, empty, under the one importable package `noema_model`: `understanding`, `explanation`, `voice`, `providers` and `platform`. The only running code is `noema_model/main.py`, which serves `/healthz` (200) and `/readyz` (503 until dependencies exist). There is no database driver, Temporal client or provider SDK.

- **Python:** 3.13, pinned by `requires-python` in `pyproject.toml`. Dependencies are pinned with `==`, and `requirements.lock` / `requirements-dev.lock` carry every file hash (`pip install --require-hashes`). Regenerate with `make lock-model`.
- **Build context:** this folder only (DEC-2026-10-13 R2): `docker build services/model`.
- **Tests and types:** `make test-model` (pytest) and `mypy --strict` through `make lint`.
- **Why `noema_model/`.** The PERD §64.2 tree shows `understanding/` and `platform/` directly under `services/model/`. A top-level package named `platform` would shadow the standard library's `platform` module for anything that runs with this folder on `sys.path`, so the same packages sit one level down. See the PR for the recorded decision.
