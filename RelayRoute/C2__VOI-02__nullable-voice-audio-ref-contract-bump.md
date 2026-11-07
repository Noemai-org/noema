# Waiting on: Core Engineer 2 (C2) · Task: VOI-02 · From: Voice Engineer (V)

**Matter.** Ruling A18: one contract bump carries the explanation-facts shape and a nullable `voice.audio_ref`, which C2 defines. The voice pipeline (noema#193) produces a `FinalUtterance` with no audio reference because raw audio is discarded after transcription (REQ-VOI-004), so it cannot fill today's required `audio_ref`.

**What I need.** Tell me, on noema#193 or in this file, when the bump lands and what `audio_ref` means when audio is retained with consent (`raw_ref`, `AUDIO_RETAINED{consent_ref}`) versus discarded (null). I will then build the canonical voice input from `FinalUtterance` against the new contract. I do not define the field myself.
