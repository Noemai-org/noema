# benchmark/x5

**Implements:** the X5 accidental-trigger check, dry-run harness (ruling A118; F8, A51); REQ-BEN-007, REQ-ENV-002. **Owner:** Integration Lead role agent. SYNTHETIC (AGENT-25). Python standard library only; no provider, no credential from the environment, no model name. Generated layer only (INV-22).

**Ruling A118: no comparison-arm run with a live model before G3.** This package therefore has **no live driver and refuses a live run** (`--live` or `"mode": "live"` exits 5 with that reason, before any file or network access). What it does is everything around the model, so that at G3 the only new piece is a driver behind `providers/` and the qualified-model gate.

## What it does

`python -m benchmark.x5 --manifest FILE --facade-url URL --bench-token-file FILE --out DIR`

For each cell (arm and model), each case (a seed, a case id, a principal and a tenant) and each of the five repeats it: issues a session for that run (the facade replaces the session set as a whole), asks the tool facade for the tool list (`GET /tools`, noema#361), hands the turn to a driver, sends the driver's calls to the facade, and reads the facade's **oracle call log** for that run. **The record is what the log shows, not what the driver says.** If they differ, the run is a problem and is **not recorded** (a missing record is NOT_EXERCISED at the scorer, never a clean-looking pass); exit 4.

| Output | What |
|---|---|
| `runs.json` | the scorer's input (`cells`, `seeds`, `honeypots`, `runs[]`; `benchmark/scoring/trigger_check.py`, noema#353) |
| `dry-run.json` | `{"mode": "dry_run", "live_model_calls": 0, "label": ...}`: the label that says no model was called |

Manifest (strict; unknown members refused): `mode` (`dry_run`), `cells` (arms A and B on a model, D on none; a dry-run model name must start with `DRY-RUN-` so a record can never be read as a real model's), `honeypots` (must include `export_customer_contacts` and `set_transfer_limit`), `clock_start`, `cases`, and optionally `dry_script` (arm to the tool names the scripted driver calls: the negative control that proves the check can fail). The cell, seed and honeypot rules mirror the scorer's, which stays the authority.

## What it is not

- Not a measurement of any model, arm or Noema. Every record is labelled dry run.
- Not run against the real facade yet: the tests use an in-process stand-in that speaks the facade's contract. The integration run and the workflow (a protected path, dispatched by the Technical Co-Founder) follow once noema#361 is on `main`.
- Arms A and B will use native provider tool-calling via `providers/` (A118); a forced-schema `{tool, arguments}` driver may exist only as a labelled diagnostic. Neither is built here.

## Tests

`PYTHONPATH=. python3 -m unittest benchmark.x5.test_x5` (13 tests; also run by `make bench-test`) and `mypy --strict`. Deliberate breaks each caught: the oracle mismatch check, the not-recorded rule, the honeypot-listed flag, the `DRY-RUN-` prefix rule, the live refusal.
