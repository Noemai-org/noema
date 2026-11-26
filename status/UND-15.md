# Status · UND-15 · the recorded-replay Understanding provider

Owner: C2 · Task: UND-15 (REQ-INT-002; Needs UND-14, UND-16; rulings A188, A198, A261).

State (2026-11-19): the loader, the labels and the lookup of the recordings are built (`benchmark/compile_acceptance/replay.py`). The provider class under `providers/`, its profile switch and the demo-profile refusal are not built (see Open).

# Session 2026-11-19 · UND-15 · loader, manifest and labels, on the 25 recordings of the first live compile run (A361)

## Session summary
- What changed (PR: not yet opened): `benchmark/compile_acceptance/replay.py` (`load_recordings`, `label_recording`, `ReplayStore`), `benchmark/compile_acceptance/test_replay.py`, one README section. No recording and no manifest of the real run is committed.
- Why: A188 (G1's Understanding is a recorded-replay provider on recordings of a real live compile run), A261 (recordings with a manifest and the label "Understanding: recorded replay of live output"), A320 (a failed call is evidence, not a fixture), and A361 as quoted by the Founder's session (start the loader, manifest and labels now on the 25 recordings so infrastructure work is not blocked). A361 is not in `docs` on `main` or on the `OPS-09-rulings-a341-a357` branch: it is cited from the instruction, not read.
- What it does: the loader refuses a directory that is not exactly the recorder's output (closed manifest, rows, pin and recording shapes; `calls` equal to rows and files; a missing, extra, duplicated or reordered recording; any field of a file that disagrees with its manifest row; a body on a non-2xx call; a link; `..`; over 1 MiB; non-strict JSON). The labels are `usable_as_fixture` (HTTP 200, no failure label, `completed`, one JSON message that meets the request's own schema and answers exactly the questions asked) or `evidence_only`, each with a closed reason, the step, the question ids and the case id, from the recording's fields only. The lookup keys on the request SHA-256 and returns a copy of the reply of a fixture or raises a typed `ReplayRefused`.
- On the 25 real recordings (`NOEMA_UND15_RECORDINGS`): 17 fixtures (all `harness_decide`, HTTP 200), 8 evidence only (4 `HTTP_4XX`, all `pipeline_slot_mentions`; 4 `NO_STATUS`, all `harness_decide`).
- Risks, and how they are contained: it only reads and serves; it imports no network or process module (a test reads the imports), and a lookup under a socket guard opens nothing. Labels never read reply or utterance meaning (a test puts "not usable" in an utterance and "fixture" in a refused one).
- Tests added (REQ-INT-002, REQ-TST-004, AGENT-12, AGENT-13, A261, A320): 60 in `test_replay.py` (5 on the real recordings, skipped with a message when the path is absent). Red first: against a stub whose functions raise `NotImplementedError`, 55 ran and 123 errors (subtests) were seen. Green: 60 pass with the real path; 55 pass and 5 skip without.
- Regression cases added: those tests.
- Mutants (51 hand mutants of `replay.py`, each run against the benchmark test module, each restored): 50 killed. M01 a missing status passes; M02 a non-200 passes; M03 and M04 the 4xx and 5xx ranges; M05 a failure label on a 200 ignored; M06 a not-completed reply passes; M07 and M08 and M09 the one-message rule (none, two content parts, a non-`output_text` part); M10 NaN accepted in a reply; M11 duplicate keys accepted; M12 and M13 a schema `$id` and a non-local `$ref` followed; M14 the reply not validated against the schema; M15 and M16 an answer to a question not asked, an answer given twice; M17 an unknown step is a fixture; M18 `..` allowed; M19 and M20 a symlinked directory, a symlinked entry; M21b a directory in the place of a recording; M22 an extra file; M23 a missing file; M24 the size cap; M25 `calls` not equal to rows; M26 and M27 a duplicated request, a duplicated id; M28 the numbering out of order; M29 an empty run loads; M30 a body on a non-2xx call; M31 `reported_model_id` not the reply's; M32 another run; M33 cost not compared; M34 the manifest's reported model ids; M35 and M36 the pinned file hashes (coverage, equality); M37 evidence served; M38 the reply shared, not copied; M39 a key with a trailing newline; M40 the key not the SHA-256 of the bytes; M41 `evidence_only` inverted; M42 the pin's model; M43 the manifest label; M44, M45, M47 and M50 unknown members allowed (manifest, recording, pin, row); M46 a bool as a count; M48 a store from a non-set; M49 `usable()` inverted. M10, M08 and M21b survived the first tests and each got a case. One mutant survives and is equivalent: M21, the entry scan not refusing a directory, because the read refuses a non-regular file too; M21b removes both and is killed.
- Debt entries: none filed (the silences below are the Architect's to rule on). Conflicts raised: none.

## Silences (A332)
1. What a committed fixture may hold. A261 says the recordings go in `services/model/tests/fixtures/replay/` with a manifest; that path is outside this PR's allowlist, so nothing is committed there. Safest reading, applied here: this PR commits no recording of the real run and no manifest of it (98 KB; each 200 carries the model's full reply, with the opaque reasoning block `encrypted_content`). The always-run tests use synthetic copies made by the recorder itself; the real recordings are read from a path at run time. The decision for the Architect: commit the 17 fixtures whole, or reduced to the request and the single message text (the reasoning block dropped); a reduced file is no longer the recorder's output and needs its own manifest shape.
2. The request hash cannot be recomputed. `request_sha256` is the hash of the bytes the transport sent; a recording holds the parsed request, with sorted keys, and none of six serialisations reproduces the hash. The loader checks the file against its manifest row and the hash's form, and serves by the recorded hash. A replay provider must hash exactly the bytes its adapter will send, and any byte that differs fails closed.
3. No file hash in the manifest. An altered reply that keeps every shared field is not detected without `file_sha256=` pins; the pins are the way to guard committed fixtures, and where they live is undecided.
4. Compile replay is not end to end. All four calls of the extraction step (`pipeline_slot_mentions`) were HTTP 400 in the run (one for each example G-A to G-D), so no recording answers that step; only `harness_decide` calls can be replayed. Whether the first compile step of UND-14 can be replayed at all is for the Architect.
5. The pin and the manifest are closed to the recorder as of noema#654 and noema#720; a recorder change that adds a member makes the loader refuse until it is updated with it.
6. A 2xx that is not 200 is `HTTP_OTHER`, never a fixture; the harness's choice of `selected` and the probabilities are not judged (the schema gate and the harness do that; INV-02).
7. Size: `replay.py` is 388 lines and `test_replay.py` 676; the review trigger of AGENTS section 7 applies.

## Pre-work for the next agent
- Exact next step: the provider class under `services/model/noema_model/providers/` that selects `ReplayStore` by configuration in bench and test profiles, refuses to start in the demo profile and fails closed for a request with no recording, with its tests (the row's "Done when"); it needs the Architect's answer on silences 1 and 4 and the allowlist for `services/`.
- Open questions (Architect): silence 1 (the committed fixture), silence 4 (no recording of the extraction step), and the text of A361.
- Files to read first: `benchmark/compile_acceptance/replay.py`, `recording.py`, `status/UND-16.md`.
- Do not touch: `benchmark/decisions/harness-*.json` (frozen); the recorder's output shape without updating `replay.py` with it.
- Also open: `status/README.md` lists the files of the folder; its index line for `UND-15.md` is outside this task's allowlist.

# Session 2026-11-19 17:30 UTC · UND-15 · the conversion to committed, reduced fixtures (SR's four conditions, relayed)

## Session summary
- What changed (PR: noema#778): `replay.py` takes #779's recorder shape (`failure_class`, `error`, `failure_classes`; a new class name such as POOL_REFUSED loads; an error holds exactly type, code and param and only on a non-2xx call; the older shape still loads); new `fixtures.py` (`reduce_reply`, `reduce_run`, `load_fixtures`, a small CLI); `test_replay.py` reads the committed fixtures and no environment variable; new `test_fixtures.py`; the 25 reduced fixtures of run 44521380842 under `services/model/tests/fixtures/replay/compile-gh44521380842/` (149 KB); README.
- Why: the Founder's order of 19 Nov (SR's conditions: reduce replies with a parse-equivalence test, keep requests as recorded and keyed by `request_sha256`, label SYNTHETIC with run id, head SHA and the A261 label, tests read only committed files, leak scan with allow lines, failed calls negative only). The conditions are relayed, not yet committed (docs#211 is open).
- Head SHA: the workflow run's own `head_sha`, `ee9156936ca1561d67040cd1764c9865471da014` (read from the run, not from the recordings, which do not hold it).
- Evidence: all 17 reduced replies equal the original's message text byte for byte and parse to the same object (checked on the originals at conversion); every request and `request_sha256` equal the original's; no `encrypted_content` or reasoning item in any committed file; the leak scan is clean with `leakscan.allow` and fails without it (the allow lines are needed) and finds an account number planted in a copy.
- Risks, and how they are contained: a reduced file is no longer the recorder's output; `load_fixtures` refuses a directory that is not exactly what `reduce_run` wrote (request, message hash, labels, provenance). The original files are not committed, so the parse-equivalence on the real data rests on the hashes `reduce_run` recorded (`original_file_sha256`, `reply_message_sha256`) and on the conversion check above.
- Tests added (REQ-INT-002, REQ-TST-004, AGENT-12, AGENT-13, A261, A320, A359): 9 in `test_replay.py` (`RecorderShape`) and 27 in `test_fixtures.py`; the 5 tests that were skipped in CI now run on the committed files.
- Regression cases added: those tests. Debt entries: none. Conflicts raised: none.

## Silences (A332)
1. The loader's strictness: a member the recorder adds later makes it refuse until it is updated (as for #779); `fixture` is the only optional member added for the committed form.
2. The request bytes are not recoverable from the recording (unchanged from the first session): the canonical hash of the parsed request guards the file; the key a replay uses is the recorded `request_sha256`.
3. The path `services/model/tests/fixtures/replay/` is outside the task's allowlist: stated exception, by the Founder's order and SR's condition (relayed).
4. Size: `replay.py` is over 400 lines and `test_replay.py` over 700 (review trigger of AGENTS section 7); `fixtures.py` 150.

## Pre-work for the next agent
- Exact next step: the provider class under `providers/` that serves `ReplayStore` (the row's "Done when"); the POOL_REFUSED class in the recorder (its own PR; the loader already takes it); reduce the next passing run with `fixtures reduce` for the G1 fixtures.
- Open questions (Architect): the text of SR's conditions to be committed (docs#211); whether `fixture.labels` should also pin the question ids.
- Files to read first: `benchmark/compile_acceptance/fixtures.py`, `replay.py`, the README section "Committed fixtures".
- Do not touch: the committed fixture files (regenerate from a run, never edit by hand).

# Session 2026-11-24 · UND-15 · the replay transport under providers/ (A619)

## Session summary
- What changed (PR: noema#938): `services/model/noema_model/providers/llm/replay.py` (`ReplayTransport`, `ReplayMiss`, `ReplayFixturesRefused`), `services/model/tests/test_replay_transport.py`, a README section. No new dependency, no change to the benchmark package, the live runner, a golden, an input, a threshold or a prompt.
- Why: A619 (interim, relayed): wait for a passing run, no hand-over of run 7, build the provider class under `providers/` only. The class is the replay provider that A604 needs to fix the A603 gate offline.
- Design: a Transport, not an adapter, so the real adapters and the harness are unchanged above it. The key is the SHA-256 of the request bytes (the recorded `request_sha256`; the parsed request cannot be turned back into those bytes, A261 silence 2). It does not import `benchmark/` (the model service never does): it carries its own small strict reader, and the strict loader of the benchmark package stays the validator of the committed set.
- Tests added (REQ-INT-002, REQ-TST-004, AGENT-12, AGENT-14, AGENT-25, INV-29): 28, including the real `OpenAIDecisions` adapter getting a choice answer (with its probabilities, abstain included) and a yes/no gate answer from a recording, a miss on a changed request, a socket guard, link/size/strict-JSON/duplicate/extra-entry refusals, and an import-boundary test. 13 hand mutants, 11 killed at first; two (NaN and a duplicate key inside a recording) were pinned by a test added before the green; one (the lstat size check) is equivalent to the bounded read.
- Debt entries: none. Conflicts raised: none.

## Silences
1. The gate has no recording yet. The committed run (44521380842) predates the A603 gate; no recording holds a gate question. A604 (fix the gate against BO's labelled set with the replay provider) needs recordings of the labelled phrasings, which come from a live call (a few cents) or from hand-written SYNTHETIC replies; hand-written replies test the threshold logic and not the model. Not decided here.
2. A symlink is checked with `lstat` before the open; the fixtures are committed repository data, not a secret, so the check-then-open gap is accepted.
3. The key function equals `ReplayStore.lookup_bytes` of the benchmark package (`"sha256:" + hexdigest`); no test imports the benchmark package from the model service.

## Pre-work for the next agent
- Exact next step: the profile switch (bench and test profiles select `ReplayTransport` by configuration; refuse to start in the demo profile) once the Architect answers the `services/` allowlist question; reduce the next PASSING run for the G1 fixtures.
- Files to read first: `providers/llm/replay.py`, `tests/test_replay_transport.py`.
- Do not touch: the committed fixture files.


# Session 2026-11-25 · UND-15 · the profile switch and the demo-profile refusal (A635, A619)

## Session summary
- What changed (PR: noema#972): `services/model/noema_model/providers/llm/profile.py` (new: `select_transport`, `ProfileRefused`, `Selection`, the closed profile set, `REPLAY_EVIDENCE_LABEL`), `services/model/noema_model/harness/assemble.py` (one call site), `services/model/tests/test_provider_profile.py`, a README section. No new dependency, nothing in the core, no change to a golden, an input, a threshold or the replay transport.
- Why: the row's Done-when asks for a test that the service refuses to start with the replay provider in the demo profile, and that a request with no recording fails closed (the second already holds in #938).
- Rule: `NOEMA_PROFILE` (closed set `dev, ci, bench, demo, test`); a replay only by configuration (`NOEMA_MODEL_REPLAY_FIXTURES_DIR`) and only in `bench` and `test`; every other profile refuses it; unknown profile refuses the start; unreadable fixtures refuse the start and never fall back to live; no replay setting is the live transport as before.
- Risks, and how they are contained: a deployment that never set the profile is unchanged (live). A replay can never be selected without an explicit bench or test profile. A refusal names a code only.
- Tests added (REQ-INT-002, REQ-TST-004, AGENT-12, AGENT-16, INV-29): 33, every profile and setting combination, the refusal text carries no value, a socket guard on the replay path, the service-level refusal (assembly and `/readyz`), a read of the module's imports.
- Hand mutants: 8 run, 8 killed (demo allowed, unknown profile accepted, no profile accepted, dev and ci allowed, fallback to live on unreadable fixtures, an empty setting read as live, a default to replay, the label dropped).
- Debt entries: none. Conflicts raised: none.

## Silences
1. The PERD defines no demo-profile refusal beyond the row ("refused in the demo profile"); this implements exactly that. It does not refuse a live credential, the evil provider or the hostility proxy in the demo profile (the evil provider and the hostility proxy already refuse every profile but bench, and ci and test for the proxy).
2. `NOEMA_MODEL_REPLAY_FIXTURES_DIR` is a new setting (A330; A635 approved the switch, not the name).
3. The evidence label is exposed as a constant and an event; carrying it into the Record's evidence belongs to the G1 wiring.

## Pre-work for the next agent
- Exact next step: the A604 gate fix against SYNTHETIC boundary replies once #932 has merged (recordings regenerated at the merged head); reduce the next passing run for the G1 fixtures.
- Files to read first: `providers/llm/profile.py`, `tests/test_provider_profile.py`.
- Do not touch: the committed fixture files.


# Session 2026-11-25 (second) · UND-15 · rework for A679 (the provider profile name and values)

## Session summary
- What changed (PR: noema#972; supersedes the section above where they differ): A679 (DEC-2026-11-25-architect-rulings-a673-a684) names the setting `NOEMA_MODEL_PROVIDER_PROFILE` with the values `live` and `replay`; absent refuses the start; the demo profile refuses `replay`. `providers/llm/profile.py` now requires it (absent, empty, padded, upper-case or any other value refuses), `live` builds the live transport, `replay` serves the recorded set. `NOEMA_PROFILE` stays the environment profile and identifies `demo`. `NOEMA_MODEL_REPLAY_FIXTURES_DIR` keeps its name (A679 does not rename it).
- Behaviour change: a harness configuration with no provider profile no longer starts (it did start live before). Four existing model tests that assemble a live harness now name `live`.
- Ambiguities, taken the safer way (not decided here): A679 is silent on `dev` and `ci` (replay stays refused there, as before); on `live` with a fixtures directory also set (refused as a contradiction); and on where "absent refuses" is checked (where the transport is chosen, so a service with no harness configuration is not ready, as before).
- Tests: 57 in `test_provider_profile.py` (REQ-INT-002, REQ-TST-004, AGENT-12, AGENT-16, INV-29); the whole model suite passes.
- Hand mutants on the new logic: 12 run, 12 killed (absent defaults to live, absent defaults to replay, unknown provider value accepted, case-insensitive match, demo allowed, dev and ci allowed, live with fixtures accepted, replay also builds a live transport, unknown environment profile accepted, replay with no environment profile accepted, unreadable fixtures fall back to live, the provider value in the refusal text).
- Debt entries: none. Conflicts raised: none.

## Silences
1. No compose, deploy or CI file sets `NOEMA_MODEL_HARNESS_CONFIG` today, so no deployed model reaches the transport choice; the day one does, `NOEMA_MODEL_PROVIDER_PROFILE` must be set with it. Those files are outside this card's allowlist and are listed in the PR.

## Pre-work for the next agent
- Exact next step: whoever mounts the harness configuration in compose adds `NOEMA_MODEL_PROVIDER_PROFILE` (`live`, or `replay` with `NOEMA_PROFILE: bench` and the fixtures directory in the bench model) in the same change; then the A604 gate fix once #932 has merged.
- Files to read first: `providers/llm/profile.py`, `tests/test_provider_profile.py`.
- Do not touch: the committed fixture files.
