# noema_model/understanding

**What it does.** Turns canonical input into a schema-valid Intent Object proposal: the input gateway, the intent compiler and entity resolution (SoT §13, §14; PERD §19 to §22). May use models, only through providers.

**REQs.** REQ-INT-001, REQ-INT-002, REQ-INT-003, REQ-AMB-001.

**Status.** `pipeline/` (T5) and `resolution/` (T6.3) are built. `gateway/` and `compiler/` are still empty (UND-01 to UND-08). See each sub-package's README. The import rules and prohibitions are in `__init__.py`.

**Tests.** `pytest` from `services/model/`: `tests/test_pipeline.py`, `tests/test_ranking.py`, `tests/test_matcher.py`.
