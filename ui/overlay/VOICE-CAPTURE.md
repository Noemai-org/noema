# Push-to-talk host interface

The host renders `PushToTalk` with both `onPcm(frame: Uint8Array)` and `maxRecordingMs`. The duration is required: without a valid host value the button is disabled. A515 sets the synthetic benchmark host value to 20,000 ms; the component supplies no default.

The callback receives signed 16-bit little-endian mono PCM at 16 kHz, in frames of at most 32,000 bytes, split at sample boundaries. It must consume the frame without logging or persisting audio. Capture stops on release, cancellation, failure, track loss or the host's duration limit. A visible recording status is present during capture.

The current browser test supplies a synthetic in-memory sink and Chromium's synthetic microphone. No voice route, provider transport or real-device acceptance is claimed by this capture slice. `ScriptProcessorNode` is a tracked replacement debt, owned by FE with Voice review.
