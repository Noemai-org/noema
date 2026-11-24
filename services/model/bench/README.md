# Baseline-run settings (VOI-16)

Date: 2026-11-20 · Owner: Voice Engineer · Status: bench values for the Intron baseline run only.

`baseline-settings.json` is the settings file `python -m noema_model.voice.baseline_run --settings=...` reads. Every key is required and none is defaulted in code (AGENT-16); the loader refuses a missing, extra or mistyped key.

| Keys | Where the value comes from |
|---|---|
| `stt_final_s`, `response_s`, `tts_first_s`, `session_s`, `read_s`, `close_s`, `hold_s`, `concurrency`, `min_pause_s`, `budget_minutes` | A380 (recorded in `DEC-2026-11-20-architect-rulings-a358-a453`, section 1, in the `docs` repository): 20 s, 1 s, 10 s, 40 s, 2 s, 1 s, 5 s, one session at a time, 2 s, 20 audio-minutes. Bench values for the baseline only. |
| `connections_per_minute` | The vendor's published limit on new connections per minute, with its source page in `docs/voice/intron-docs-notes-DRAFT.md` (noema#625). The pause between sessions is the larger of `min_pause_s` and 60 times `connections_per_session` over this number. |
| `connections_per_session` | Two: one speech-to-text connection and one text-to-speech connection a session. |
| `utterance_min_s`, `utterance_max_s` | The Founder's measurement condition: utterances of 5 to 15 seconds (A241). |
| `allowed_hosts` | **A556:** the host names the base address may name, supplied by the Founder in the settings file on the bench host: required, a non-empty list of plain lowercase host names (no scheme, port, path, wildcard or number-only address), no default. The runner (and `--check-key`) refuses before any connection unless `--base-url` is exactly `wss://<one of these hosts>`, with no user information, port or path. **The repository names no vendor host:** the shipped file holds the placeholder `set-by-the-founder.invalid`, so a run from the shipped file is refused until the Founder sets the real host in the bench copy. |
| `open_timeout_s`, `ping_interval_s`, `ping_timeout_s`, `max_message_bytes`, `tts_poll_s` | **Proposed by Voice under A332 (the author takes the safer reading and lists it in the pull request); not in A380.** 10 s to open a connection; no keep-alive pings (a session is far shorter than the vendor's idle gap); 4 MiB for the largest message (a text-to-speech answer chunk at 48 kHz, base64 encoded, is well under it, and websockets' own default of 1 MiB is not); 0.2 s between polls of a text-to-speech chunk. Change them by editing this file; the Architect may overrule any of them. |

No vendor address, no key and no voice choice is in this directory: the address, the key file and the voices file are arguments of the runner.

## Proposed files (not adopted)

`manifest.proposed.json` and `voices.proposed.json` are **proposals** the runner reads through `--manifest` and `--voices`; the Founder and the Architect change or adopt them (A523: they stay marked proposed). Nothing in them is a threshold.

- **Manifest:** 24 synthetic scripts (six in Nigerian English, six in Pidgin, four each in Yoruba, Hausa and Igbo mixed with English), each a short banking-style request with synthetic names and amounts, all labelled `SYNTHETIC`, none naming a recording of a person. `audio_s` is the estimate the budget is planned from (about 2.6 words a second); the real audio is whatever Intron text-to-speech generates, and an item whose generated audio falls outside the 5 to 15 second condition is skipped, counted in the report as `skipped_out_of_bounds`, and never sent. The Yoruba, Hausa and Igbo phrases were written by the Voice Engineer, not a native speaker: a native speaker should read them before they are relied on (REQ-VOI-007).
- **Voices:** Pidgin is language `pcm` with accent `pidgin`; Nigerian English is language `en` with accent `yoruba` because the documentation lists no Nigerian English entry (the accents it lists for English are Afrikaans, Hausa, Igbo, Luganda, Sepedi, Swahili, Setswana, Xhosa, Yoruba and Zulu); Yoruba, Hausa and Igbo use their own language codes and the accent of the same name, as their rows in the documentation's table of text-to-speech languages read. The gender (`female` throughout) is a placeholder: the documentation names no voices. The Intron Product session settles voice names.

## The report and the budget (A568)

- **Budget:** 20 audio-minutes, charged with what was actually generated and sent, never what the manifest declared: the audio Intron text-to-speech generates for a fixture is charged when it arrives, the answer to speak and the audio to send to speech-to-text must both fit before a session starts, and a run that cannot fit them ends with `ended: budget_exhausted` and nothing sent. The manifest's `audio_s` and `answer_s` are only the plan the manifest is trimmed from.
- **Report:** the report file is created before any connection (an existing file refuses the run, so no credit is spent for a report that cannot be kept; a file left empty by a run that never started must be deleted by the Founder before a rerun), owner-only, and rewritten whole after every session. A failure part-way (a store read, an unexpected error) therefore never discards what was measured. `ended` says why the run stopped: `complete`, `budget_exhausted`, `rate_limited`, `two_failures_in_a_row`, `generation_or_store_failed` or `store_read_failed`; a report still being written says `in_progress`.

## Chunk-by-chunk charging and what is written (A608, A612)

- The text-to-speech audio is charged **chunk by chunk as it streams**, before the next chunk is asked for: the generation stops at the first chunk that would not fit the budget (`ended: budget_exhausted`), the stream is closed, and the audio so far is not kept. Only accepted chunks are counted in `generation_s`. The chunk that would not fit has already been generated by the vendor, so it is the one amount of vendor credit the report does not count (one chunk, never more).
- **Only the audio of an accepted item is written to the store**, and only after its bounds and its whole session (the audio to send and the answer to speak) are known to fit: a budget stop, an out-of-bounds item or a session that does not fit leaves no file and no speaker folder behind.
