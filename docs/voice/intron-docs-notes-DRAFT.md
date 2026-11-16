# Intron (Sahara) official documentation notes: DRAFT

Date: 13 Nov 2026 · Owner: Voice Engineer · Status: **notes only, derived and non-normative.** They decide nothing: no adapter, no interface change, no threshold. Rulings A179 to A182 (docs#168, merged 13 Nov) make Intron the sole integrated speech provider but take effect only with the SoT edit, which has not been made; until it is, none of this is built on. Read by a web fetch of Intron's public pages on 13 Nov 2026 (the privacy-policy lines were supplied to me and are marked so). No account, no key, no live call, no data sent (F4).

Status key: **VERIFIED** = on an official Intron page (URL given). **UNVERIFIED** = not found, not readable, or only a marketing claim. The docs site does not name "Sahara"; the Sahara v2.5 homepage links to it, which is the only tie between them.

## Sources read
| Page | Result |
|---|---|
| https://docs.voice.intron.io/docs/index/introduction (API docs; also /stt/supported-languages, /stt/streaming, /tts/supported-languages-and-accents, /tts/tts-streaming, /tts/tts-generate, /accounts/credits_balance) | read |
| https://intron.io/ (Sahara v2.5 homepage), https://intron.io/sahara (2025 launch page) | read; marketing |
| https://speech.intron.health/ | a contributor login page ("Intron Data Vault"), no API docs |
| https://docs.intron.health, https://intron.health/developers | does not resolve / 404 |
| Privacy policy PDF (https://www.intron.io/wp-content/uploads/2024/05/Revised-Introns-Privacy-Policy.pdf), terms PDF (https://www.intron.io/wp-content/uploads/2023/12/Intron-Transcribe-App.pdf) | fetched, text **not extractable**; nothing verified |
| Not read: file-upload, file-status, question-answering, tts-queue, tts-status, widget, voice-ai page, any pricing page (none linked), the dashboard (login) | |

## Findings
| Topic | Finding | Status |
|---|---|---|
| Locales, STT | Listed: "English (en)" only, plus code-switched pairs with English: Pidgin (pcm), Yoruba (yo), Hausa (ha), Igbo (ig), and others. **No labelled Nigerian English locale.** Our `Locale` set (EN_NG, PCM, YO, HA, IG) does not map one-to-one: EN_NG would be plain `en`, and Yoruba, Hausa and Igbo are Yoruba-English style code-switched models, not monolingual. | VERIFIED (/stt/supported-languages) |
| Locales, TTS | 13 languages; English with accent rows (yoruba, hausa, igbo, pidgin and others), male and female. The streaming example uses accent `english`, which is not in the accent table. | VERIFIED; docs inconsistent |
| Per-language quality | No per-language accuracy, WER or status in the docs. Homepage says only "Improved". | UNVERIFIED |
| Language counts | Homepage: 57 languages, 500+ accents, "63 total"; press gives other figures. Not reconciled. | UNVERIFIED |
| Streaming STT protocol | WebSocket only: `wss://infer.voice.intron.io/stt/v1/stream`, `Authorization: Bearer <key>`. No gRPC or HTTP streaming documented. | VERIFIED |
| Streaming STT audio | Base64 PCM16 little-endian in `audio_base_64`; params `sample_rate` (default 16000), `bit_rate` (16), `num_channels` (1); chunks 1 KB to 32 KB. A list of allowed rates is not given. This fits F7 (PCM16 16 kHz). | VERIFIED |
| Streaming STT events | In: `INPUT_AUDIO_CHUNK`, `COMMIT`. Out: `SESSION_CREATED`, `AUDIO_CHUNK_ACK`, `PARTIAL_TRANSCRIPT` (`transcript`, sent when text changes), `COMMITTED_TRANSCRIPT` (`transcript_id`, `transcript_text`, `audio_len`). A partial and a final are distinct events, as our contract needs. The client must send `COMMIT`; the provider does not decide the endpoint for us. | VERIFIED |
| Per-token confidence | **No per-token or per-word confidence field** in any response schema read. Our contract treats a missing confidence as 0.0, so every key slot would read as low and fall to confirmation or text. The homepage "98% confidence" is a UI mock-up, not an API field. | NOT FOUND |
| TTS | Streaming: `wss://infer.voice.intron.io/tts/v1/stream`, text chunks of 10 to 100 characters, then `FETCH_AUDIO_CHUNK` polled until READY, then `COMMIT`. Non-streaming: `POST /tts/v1/generate`, docs say up to 4096 characters, which conflicts with a 100-character error sample. Output: WAV or OPUS, default WAV; a sample shows 48000 Hz. Voice choice is accent plus gender only. | VERIFIED; docs inconsistent |
| Latency, SLA | No numbers. Homepage says "Real-time" and "Low-latency". | NOT FOUND |
| Limits | Streaming STT and TTS: 300 s session lifetime, 60 s idle audio gap, 3 connections per minute, 3 concurrent connections. TTS generate: 30 requests per minute (503 after 120 s). Balance endpoint: 100 per minute. File-upload limits not read. | VERIFIED |
| Data terms | **Supplied to me by the Founder side, not read by me** (the PDF text was not extractable here): Intron Privacy Policy, Version 01, June 2024, which names APIs and speech data in its scope. Retention: personal information is kept "as long as necessary" for the stated purposes or legal, accounting and reporting needs; **no number of hours, days or months for audio, and none for transcripts**. Deletion: on request, only if the data is no longer necessary and there is no other lawful basis to keep it. Anonymised data: may be kept and used **indefinitely**. Sharing: affiliates and service providers, including for support, database management, analytics, **research** and quality control. Nothing was supplied on training use of customer audio, residency or a DPA. The terms PDF still has not been read, and may cover the Transcribe App, not the API. | UNVERIFIED by me (policy supplied); retention numbers NOT STATED |
| Pricing, access | Key from the developers tab at voice.intron.io; usage is credit-based (`credit_balance` in `SESSION_CREATED`, `QUOTA_EXCEEDED`); balance in NGN or USD by account country. No price list found; "Talk to Our Team" form and `voice@intron.io` for support. | VERIFIED access, pricing UNVERIFIED |
| Marketing claims | "Beats OpenAI, Google, AWS, Azure", "300+ non-native accents", the 12-bilingual-model list: not verified and not used. | UNVERIFIED |


Also supplied (not read by me): the docs have file-upload and synchronous STT upload pages (the latter waits and returns the transcript); I read neither, so file size and length limits are still unknown. Whether the 3-connection, 3-per-minute and 300 s limits are defaults or can be raised per account is a question for Intron.

## What this means for our contract (read-only observations, no change proposed here)
1. **Confidence is the gap that matters.** Without a per-token confidence the voice clarification flow cannot tell a good key slot from a bad one; by our rules every key slot is treated as low. Whether Intron returns one outside these pages is a question for Intron, in writing.
2. The 3-concurrent-connection and 3-per-minute limits are far below any field-study load and need an enterprise answer before sizing.
3. The 300 s session cap fits an utterance but needs the pipeline to reconnect on long turns.
4. Data terms are the F4 blocker: no partner data until they are read and approved. The policy gives no retention period and lets anonymised data be kept and used indefinitely and shared for research, which does not match what our recording plan (F3) promises its speakers (deleted 90 days after analysis). Any use needs a written contract term, not the policy alone.
5. Keeping Intron behind the provider interface, apart from Trust and Execution, is what our architecture already requires (INV-29, A9); these notes change nothing about that.

## Questions to put to Intron (writing, `voice@intron.io`)
Whether the stated limits are defaults or can be raised; the retention period for audio and transcripts and a no-training, no-research commitment in a contract; per-token confidence in any response; the allowed sample rates; per-locale accuracy on Nigerian English and the three code-switched models; latency figures and any SLA; limits at enterprise tier; retention, training use, residency and a DPA for audio and transcripts; the effective date and scope of the terms.

## What the committed rulings say about these findings (16 Nov 2026)

Rulings A179 to A182 were recorded by docs#168 (merged 13 Nov). **A179 takes effect only with the SoT edit that adopts it (v1.12, not yet made); until then the committed SoT stands and no one builds against Intron.** These notes are read-only evidence for that day; they authorise nothing.

| Ruling | What it says | What these notes add |
|---|---|---|
| A180 | No real speaker's audio or transcript goes to Intron until the Founder commits written terms (retention, zero data retention, no training, sub-processors, hosting region, deletion timeline, incident notice). "Zero data retention" is unverified until it is in writing. | The published privacy policy (supplied to me; I did not read the PDF) states no retention period and lets anonymised data be kept indefinitely and shared for research. That is the opposite of a written term, so the claim stays unverified. Only synthetic audio goes to Intron meanwhile (OD-P-89). |
| A181 | Intron's documented 10 to 100 character TTS chunking is done by the adapter. A spoken "yes" is never a confirmation. | The streaming TTS docs say 10 to 100 characters per chunk; the synchronous endpoint's docs say up to 4,096 characters but one error sample says 100. The adapter must follow the stricter figure until Intron answers. |
| A191 | STT frames at most 32,000 bytes, 16 kHz mono PCM16 only; a missing per-token confidence counts as low, is measured, and triggers the second-provider decision (OD-P-88) if G5 fails. | Intron's streaming STT takes base64 PCM16 chunks of **1 KB to 32 KB**. The cap (32,000 bytes) fits under their maximum; their **minimum of 1 KB is larger than a 20 ms frame (640 bytes)**, so the adapter must gather several frames into one chunk. The cap is a ceiling for our frames, not Intron's chunk size. |
| A228 | The voice confidence for the low-ASR rule is the minimum over tokens, in basis points, a missing token counting as 0; voice stays refused in G1. | Intron's responses carry **no per-token confidence** in any documented field, so every key slot would count as low until Intron answers (OD-P-91). That is safe (the person confirms or types) and it is measured, not disqualifying. |
| A182 | Intron's STT and TTS model IDs are pinned and qualified (UND-13) before any gate run. | The docs give no model or version identifier in the STT or TTS responses I read; the pin pattern in `benchmark/qualification/pins.py` needs a version string, which Intron has to supply. |

Limits that bear on the field study (A179, OD-P-90): three concurrent streams and three new connections per minute, a 300 s session cap, 60 s idle gap; the plan's sessions and pacing must be built for those until Intron says whether they are defaults.
