# Opus audio decoding in the model service · AGENT-14 draft (document only)

Date: 2026-11-06 · Owner: the Founders decide · Drafted by: Voice Engineer role · Status: **DRAFT, nothing added, nothing built**

This is the AGENT-02 / AGENT-14 write-up for a possible dependency. It adds no dependency and changes no code. Ruling A19 (6 Nov 2026) is the current position: **the POC phone sends PCM16 at 16 kHz**, so the model service needs no audio codec. If this draft is ever approved it becomes an `[OPEN]` entry in `docs/open-decisions.md` in the `docs` repository, by its own pull request.

## 1. The missing decision

PERD §35.1 shows the phone sending "Opus/16 kHz" over a WebSocket. A19 overrides that for the POC. The question this draft keeps open is whether, and when, the model service should decode Opus.

## 2. Why it matters

- Uncompressed mono PCM16 at 16 kHz is 16,000 samples a second × 16 bits = 256 kbit/s. REQ-VOI-005 requires the gate to pass on a 3G connection. Whether 256 kbit/s survives Airtel 3G with loss and jitter is **not measured**; it is the thing the VOI-11 dry run measures. No claim is made here about 3G capacity.
- End-of-speech detection (`voice/endpoint.py`) and the simulation library (`voice/simulation/`) work on decoded PCM only. They refuse Opus.
- A speech provider may accept Opus directly, in which case the model service would pass it through and need no decoder at all. That is unverified for all three providers (matrix cells are UNVERIFIED).

## 3. Options

| Option | Effort | Risk | Maintenance |
|---|---|---|---|
| A. PCM16 16 kHz from the phone (A19, **current**) | None; already built | Bandwidth on 3G unmeasured; if it fails, G5 fails for a reason that is not the voice pipeline | None |
| B. Decode Opus in the model service | A native-code dependency in the Python image, an AGENT-14 approval, a pinned and scanned build, tests with real Opus fixtures | A new native dependency in the service that holds provider credentials (INV-29 surface), a larger image, decoder bugs on malformed input are a fuzzing target | Version pinning and security updates of the codec |
| C. Provider-side Opus pass-through | Depends on provider documents we do not have | Unknown until DEBT-14 is closed; endpoint detection would then need another source | None in our code |
| D. Do nothing further | None | Same as A | None |

## 4. Smallest safe default

Option A for the POC. Do not add a dependency.

## 5. What would invalidate the default

The VOI-11 dry run (10 sessions on the Tecno Spark over Airtel 3G) shows PCM16 lost-frame rates or latency that a lab check attributes to bandwidth rather than to the pipeline. Then Option B or C is reopened with those measurements.

## 6. Recommendation

Stay on Option A. Reopen only on the evidence in section 5. If reopened, prefer Option C if a provider documents Opus input, because it adds no dependency; otherwise Option B with the decoder isolated in `providers/speech/` and fuzzed against malformed frames.

## 7. Question for the Founders

Do you accept Option A for the POC, with the reopen condition in section 5?
