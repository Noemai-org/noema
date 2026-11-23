# Baseline-run settings (VOI-16)

Date: 2026-11-20 · Owner: Voice Engineer · Status: bench values for the Intron baseline run only.

`baseline-settings.json` is the settings file `python -m noema_model.voice.baseline_run --settings=...` reads. Every key is required and none is defaulted in code (AGENT-16); the loader refuses a missing, extra or mistyped key.

| Keys | Where the value comes from |
|---|---|
| `stt_final_s`, `response_s`, `tts_first_s`, `session_s`, `read_s`, `close_s`, `hold_s`, `concurrency`, `min_pause_s`, `budget_minutes` | A380 (recorded in `DEC-2026-11-20-architect-rulings-a358-a453`, section 1, in the `docs` repository): 20 s, 1 s, 10 s, 40 s, 2 s, 1 s, 5 s, one session at a time, 2 s, 20 audio-minutes. Bench values for the baseline only. |
| `connections_per_minute` | The vendor's published limit on new connections per minute, with its source page in `docs/voice/intron-docs-notes-DRAFT.md` (noema#625). The pause between sessions is the larger of `min_pause_s` and 60 times `connections_per_session` over this number. |
| `connections_per_session` | Two: one speech-to-text connection and one text-to-speech connection a session. |
| `utterance_min_s`, `utterance_max_s` | The Founder's measurement condition: utterances of 5 to 15 seconds (A241). |
| `open_timeout_s`, `ping_interval_s`, `ping_timeout_s`, `max_message_bytes`, `tts_poll_s` | **Proposed by Voice under A332 (the author takes the safer reading and lists it in the pull request); not in A380.** 10 s to open a connection; no keep-alive pings (a session is far shorter than the vendor's idle gap); 4 MiB for the largest message (a text-to-speech answer chunk at 48 kHz, base64 encoded, is well under it, and websockets' own default of 1 MiB is not); 0.2 s between polls of a text-to-speech chunk. Change them by editing this file; the Architect may overrule any of them. |

No vendor address, no key and no voice choice is in this directory: the address, the key file and the voices file are arguments of the runner.

## Proposed files (not adopted)

`manifest.proposed.json` and `voices.proposed.json` are **proposals** the runner reads through `--manifest` and `--voices`; the Founder and the Architect change or adopt them (A523: they stay marked proposed). Nothing in them is a threshold.

- **Manifest:** 24 synthetic scripts (six in Nigerian English, six in Pidgin, four each in Yoruba, Hausa and Igbo mixed with English), each a short banking-style request with synthetic names and amounts, all labelled `SYNTHETIC`, none naming a recording of a person. `audio_s` is the estimate the budget is planned from (about 2.6 words a second); the real audio is whatever Intron text-to-speech generates, and an item whose generated audio falls outside the 5 to 15 second condition is skipped, counted in the report as `skipped_out_of_bounds`, and never sent. The Yoruba, Hausa and Igbo phrases were written by the Voice Engineer, not a native speaker: a native speaker should read them before they are relied on (REQ-VOI-007).
- **Voices:** Pidgin is language `pcm` with accent `pidgin`; Nigerian English is language `en` with accent `yoruba` because the documentation lists no Nigerian English entry (the accents it lists for English are Afrikaans, Hausa, Igbo, Luganda, Sepedi, Swahili, Setswana, Xhosa, Yoruba and Zulu); Yoruba, Hausa and Igbo use their own language codes and the accent of the same name, as their rows in the documentation's table of text-to-speech languages read. The gender (`female` throughout) is a placeholder: the documentation names no voices. The Intron Product session settles voice names.
