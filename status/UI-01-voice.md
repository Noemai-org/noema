# UI-01 · push-to-talk capture

## Task card

Task: UI-01, push-to-talk capture. Priority P1. Requirements: REQ-UI-001, REQ-VOI-001; SoT §18.4; POC-FR-11; A19, A97 (16 kHz PCM16).
Allowlist: ui/** and status/UI-01-voice.md. No backend, contract or dependency change.
Inputs and outputs: a user-held microphone interaction; signed little-endian 16-bit mono PCM at 16 kHz to an explicitly supplied voice sink only. No transcript or response is invented.
Tests first: PCM byte ordering, clipping and numeric edge cases; permission denial and resource cleanup; no microphone or enabled button without a voice sink; browser behavior and privacy scanning.
Faults: actual unavailable/denied microphone or unsupported audio rate. No mock library. Record events: none; UI never writes Record events.
Out of scope: live voice endpoint/protocol, provider SDKs, recording persistence, replacing text, automatic activation or claiming real-device acceptance.
Done: checks and review green; real voice integration and Android device acceptance remain UI-01/VOI-15 dependencies.

## Session 2026-11-19 · failing-first
Node 22 tests specify PCM16 conversion and the disabled-without-sink button; they fail because the module does not exist. Microphone access remains disabled in the current shipped overlay. Live transmission is not part of this change.
