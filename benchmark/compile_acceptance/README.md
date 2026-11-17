# benchmark/compile_acceptance

The compile acceptance run (UND-16; noema#546; ruling A198). It runs the five golden examples G-A to G-E of the Intent Compiler (PERD, golden examples) once each through the model service's compile service, with the real provider adapter, and records every provider call for the replay provider (UND-15, A261). Synthetic inputs only; not the bake-off; not a G1 result. It reads no oracle and no held-out case (INV-22).

**Implements:** REQ-INT-001, REQ-INT-002, REQ-BEN-002 (the recordings). **Never:** a key in a file, a log line or an argument (INV-29: only the model service's provider adapter reads `OPENAI_API_KEY`); a host or a header in a recording; a provider call that would pass the stop.

| File | What it holds |
|---|---|
| `golden.py` | the five examples as synthetic canonical inputs, and `check`: what each proposal must hold (the node, `question_type`, the amount in minor units, the counterparty, the time phrase, the prohibited remedy of G-E, and what must be absent). A problem names a slot, never a number or a piece of the utterance |
| `recording.py` | `Recorder`: one `recordings/rec-NNNN.json` per call (provider, model pin, run id, date, SHA-256 of the exact request bytes, both bodies parsed, cost) and `manifest.json` |
| `runner.py` | `run_cases`: each example once, in order; the stop stays; `RunResult.exit_code`; `run.json` |
| `build.py` | the live side: `RecordingTransport` (the budget guard decides before a request is sent), `DryTransport` (prices the first request of each example, sends nothing), `build_backend` (the real harness for one frozen harness configuration, the real pack, lexicon and masking limits, a synthetic candidate source) |
| `__main__.py` | the command, below |
| `leakscan.allow` | the two line patterns of a real output the leak scan would otherwise take for a number (epoch times and cost counts) |

## Run

```
python -m benchmark.compile_acceptance [--dry-run] --pack-ref fin@1.0.0 --provider openai --hard-stop-usd N --prior-spent-micros M --out DIR --run-id ID
```

It is started only by `.github/workflows/bench-live-compile.yml` (noema#569), with `PYTHONPATH=services/model:.`. `--dry-run` needs no key and calls nothing: it prints the worst-case cost of the first request of each example and the ceiling. A run needs `OPENAI_API_KEY` in the environment.

| Exit | Meaning |
|---|---|
| 0 | every example met its check |
| 1 | a miss, an error (no usable reply, or the schema gate refused it) or the stop |
| 2 | a bad input, or something that names the held-out set |
| 3 | NOT RUN: no key, or no provider call was made. Never a pass |

The cost counted for a call is what the reply's own `usage` gives at the frozen rate card, or the worst-case projection when a reply reports none (`benchmark/bakeoff/budget.py`; a failed call is settled at its projection). The hard stop is whole dollars (1 to 5) counted with `--prior-spent-micros`.

## What it does not show

A live reply has not been parsed by this code: the first live run is that evidence, and it is the Founder's to dispatch (noema#338, A198). The candidate source is synthetic and in memory (the core is not involved). `build.py` repeats two small pieces of `noema_model.understanding.compiler.wiring` (the pack view and the minter), which is noema#635 and not on `main`; they become an import when it is (debt in `status/UND-16.md`).

## Tests

`python -m unittest discover -s benchmark -t . -p 'test_*.py'` (the benchmark job) and `cd services/model && pytest tests/test_compile_acceptance_build.py` (the model job).
