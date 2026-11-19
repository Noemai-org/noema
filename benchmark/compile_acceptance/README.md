# benchmark/compile_acceptance

The compile acceptance run (UND-16; noema#546; ruling A198). It runs the five golden examples G-A to G-E of the Intent Compiler (PERD, golden examples) once each through the model service's compile service, with the real provider adapter, and records every provider call for the replay provider (UND-15, A261). Synthetic inputs only; not the bake-off; not a G1 result. It reads no oracle and no held-out case (INV-22).

**Implements:** REQ-INT-001, REQ-INT-002, REQ-BEN-002 (the recordings). **Never:** a key in a file, a log line or an argument (INV-29: only the model service's provider adapter reads `OPENAI_API_KEY`); a host or a header in a recording; a provider call that would pass the stop.

| File | What it holds |
|---|---|
| `golden.py` | the five examples as synthetic canonical inputs, and `check`: what each proposal must hold (the node, `question_type`, the amount in minor units, the counterparty, the time phrase, the prohibited remedy of G-E, and what must be absent). A problem names a slot, never a number or a piece of the utterance |
| `recording.py` | `Recorder`: one `recordings/rec-NNNN.json` per call (provider, model pin, run id, date, SHA-256 of the exact request bytes, both bodies parsed, cost) and `manifest.json` | Only a 2xx reply body is written: the body of any other response is not recorded (SR's condition on noema#654, A320); its status, its failure label, its failure class and three validated fields of its error object are (A359, below).
| `runner.py` | `run_cases`: each example once, in order; the stop stays; `RunResult.exit_code`; `run.json` |
| `build.py` | the live side: `RecordingTransport` (the budget guard decides before a request is sent), `DryTransport` (prices the first request of each example, sends nothing), `build_backend` (the real harness for one frozen harness configuration, the real pack, lexicon and masking limits, a synthetic candidate source) |
| `replay.py` | UND-15, the loader, the labels and the lookup of the recordings (below); no provider class |
| `fixtures.py` | UND-15, the reduction of a run's recordings to committed fixtures and the strict reader of them (below) |
| `__main__.py` | the command, below |
| `leakscan.allow` | the two line patterns of a real output the leak scan would otherwise take for a number (epoch times and cost counts) |

## Diagnostics of a failed call (A359)

For a reply that is not 2xx the recording also holds `error`: `{"type", "code", "param"}` read from the provider's error object `{"error": {...}}`, each only when it is a short plain identifier, else `null`. A bad `response_format` shows in `param` (for example `text.format.schema`). The `message`, any other member and the body are never read or written. A reply with no usable error object gives three `null`s; a call with no status, or a 2xx, has `error: null`. A value is kept only if it matches `[A-Za-z][A-Za-z0-9_.\[\]]{0,63}` (no space, quote, newline, hyphen or slash), holds at most four digits (the leak rule, REQ-SEC-003), has no run of letters and digits longer than 32 and has no key prefix (`sk_`, `AKIA`, `ghp_`, `xox`, `eyJ` and the like). The body is read only up to 1 MiB.

`failure_class` is written for every call, in the recording and in the manifest row (the manifest also counts them in `failure_classes`). It is one of: `OK` (2xx, a JSON object), `MALFORMED` (2xx that is not a JSON object, or the transport said so), `REJECTED` (any non-2xx below 500 other than 408 and 429, as `failure_for_status`), `RATE_LIMITED` (429), `TIMEOUT` (408, or the transport said so), `UNAVAILABLE` (5xx, or the transport said so: no status), `NO_STATUS` (no status and no failure label the transport named). The label the transport names (the model service's `ProviderFailure`) wins; otherwise the status decides. The status stays in `status`.

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
| 3 | NOT RUN: no key, or no provider call was made. Never a pass. `--dry-run` calls no provider, so it always exits 3 (A285) |

The cost counted for a call is what the reply's own `usage` gives at the frozen rate card, or the worst-case projection when a reply reports none (`benchmark/bakeoff/budget.py`; a failed call is settled at its projection). The hard stop is whole dollars (1 to 5) counted with `--prior-spent-micros`.

## Replay: the loader, the labels, the lookup (UND-15; A261)

`load_recordings(dir)` reads one `recordings/` directory and refuses it unless it is exactly what the recorder wrote: the manifest and the files in a closed shape, `calls` equal to the rows and the files, no missing, extra, duplicated or reordered recording, every shared field of a file equal to its manifest row, no link, no `..`, no file over 1 MiB, strict JSON. `file_sha256=` pins the exact bytes of each file when the fixtures are committed. `label_recording` gives each recording `usable_as_fixture` (HTTP 200, no failure label, a completed reply holding one JSON message that meets the request's own schema and answers exactly the questions asked) or `evidence_only`, with a closed reason, the step (`harness_decide`, `pipeline_slot_mentions`), the question ids and the case id; it reads fields, never the meaning of a reply or an utterance. `ReplayStore.lookup(request_sha256)` returns a copy of the recorded reply of a fixture and raises `ReplayRefused` (`UNKNOWN_REQUEST`, `EVIDENCE_ONLY`, `BAD_KEY`) for anything else: it never guesses and sends nothing. Evidence of a run on these recordings carries the label "Understanding: recorded replay of live output" (`REPLAY_LABEL`). The loader also keeps the recorder's `failure_class` and `error` (type, code, param; never a message) of each recording and the manifest's `failure_classes` (A359, noema#779): all or none, a class is a name (`[A-Z][A-Z_]{1,39}`) so a new class such as POOL_REFUSED loads, the counts must equal the rows', and an error belongs only to a call that was not 2xx.

### Committed fixtures (SR's conditions on noema#778, relayed)
The recordings of the live compile run of 19 Nov 2026 (workflow run 44521380842, dispatched on head `ee9156936ca1561d67040cd1764c9865471da014`) are committed, reduced, at `services/model/tests/fixtures/replay/compile-gh44521380842/` (a manifest and `rec-0001.json` to `rec-0025.json`; the loader refuses any other entry, so this text lives here, not there). Made by `python -m benchmark.compile_acceptance.fixtures reduce <recordings/> <dir> --head-sha <sha>`; `... fixtures check <dir>` reads a directory back. Reduced means: a **request** is kept exactly as recorded and stays keyed by `request_sha256`; a **reply** keeps the one message, the status, the model and the three token counts, and loses the provider's echo of the request, its tools and its opaque reasoning (`encrypted_content`), so the reduced reply parses to the same object (the message text is the same bytes; `fixture.reply_message_sha256` is its hash, and `fixture.labels` the labels computed on the original); a **failed call** has no reply and stays as it was, with its `fixture` member: a negative fixture, evidence and never served (A320). Each file carries `fixture`: the A261 label, `synthetic: true`, the run id, the head SHA, the SHA-256 of the original file and of the canonical request. `load_fixtures(dir)` refuses a directory whose request, message, labels or provenance is not what the reduction wrote. The tests read only these committed files (no environment variable, no download) and run the leak scan on them with `leakscan.allow` (its lines name the epoch timestamps and the token and cost counts; an account number planted in a copy is still found).
What the 25 hold: 17 answers, all of the decide step; 8 negative fixtures (4 `pipeline_slot_mentions` calls refused with HTTP 400, 4 injection-screen calls refused locally with no status). No slot-mention call has an answer, so **the compile cannot be replayed end to end from these**; the G1 replay fixtures come from the next passing run, reduced the same way. The request bytes the harness sent are not in a recording (it holds the parsed request, keys sorted): a replay answers a request only when the replaying harness produces the same bytes, which fails closed if it does not.

## What it does not show

A live reply has not been parsed by this code: the first live run is that evidence, and it is the Founder's to dispatch (noema#338, A198). The candidate source is synthetic and in memory (the core is not involved). `build.py` repeats two small pieces of `noema_model.understanding.compiler.wiring` (the pack view and the minter), which is noema#635 and not on `main`; they become an import when it is (debt in `status/UND-16.md`).

## Tests

`python -m unittest discover -s benchmark -t . -p 'test_*.py'` (the benchmark job) and `cd services/model && pytest tests/test_compile_acceptance_build.py` (the model job).
