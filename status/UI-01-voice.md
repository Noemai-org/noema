# UI-01 · push-to-talk capture

## Session 2026-11-21 · recording duration
- UI CI at `a5e6e831` observed the recording deadline regression. The local fix requires a host-provided maximum recording duration, closes the microphone on expiry, disables the control without a valid limit and shows a visible recording indicator. Node 22 tests and seven synthetic Chromium checks pass locally; no physical device or live voice sink is claimed. #860 asks Architect/Voice to identify the production host value; none is defaulted.

## Session 2026-11-20 · A191 PCM frame bound
- Voice's review identified that a maximum 16,384-sample audio callback produced 32,768 bytes, above the A191 32,000-byte model-service ceiling. Tests first at `33791f96`; Node 22 UI CI failed as expected on the missing limit.
- Encode all samples before emission, then split into frames of at most 32,000 bytes on 16-bit sample boundaries. Stop emission on abort. The full Node 22 suite, six Playwright tests (including synthetic Chromium microphone with a forced 16,384-sample callback), pin and diff checks pass locally. This does not establish physical Android or live voice-sink acceptance.

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

## Session 2026-11-20 · capture slice
UI CI at `e0bb435b` observed the missing-module failure. The capture slice requests a 16 kHz AudioContext and fails closed if unsupported; it encodes mono samples as signed little-endian PCM16 and stops tracks on release, unmount, failure or an ended-track notification. The button is disabled without a host-supplied sink. No voice endpoint or transmission is provided.
Node 22: the full Node suite and six Chromium Playwright checks pass locally, including synthetic-device capture with the actual context rate asserted, microphone permission denial and a simulated ended notification. The browser tests do not prove physical Android microphone behavior. A physical webview and an approved live voice path remain outstanding.

## A505 · voice security follow-up
- The push-to-talk component shows the visible `Recording` status while capture is active and removes it when capture stops. The host must supply a maximum recording duration; no production default is assumed (#860).
- Technical debt — owner: Frontend Engineer, with Voice Engineer review. `ScriptProcessorNode` is deprecated; replace it with `AudioWorklet` in a follow-up while preserving PCM16 mono framing, the recording deadline, cleanup and privacy tests. This POC does not claim physical-device or live voice-sink acceptance.

## 23 Nov · synthetic host and sink
- A515: a synthetic browser host passes 20,000 ms explicitly to the component; no value is defaulted by UI code. Chromium's synthetic microphone feeds a synthetic in-memory sink while the visible Recording indicator is asserted, then the test releases the control. The sink interface is documented in ui/overlay/VOICE-CAPTURE.md. Node 22 full suite passes with one real-core test skipped. No live voice route or Android-device result is claimed.
