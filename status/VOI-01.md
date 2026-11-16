# Status · VOI-01 (Voice Engineer) · the speech provider interface and what the vendor documents say

Updated 2026-11-16.

## State
- The interface, the capability matrix and the refusing scaffolds are merged. No adapter exists and none is allowed: rulings A179 to A182 (docs#168, merged) make Intron Sahara v2.5 the sole integrated speech provider, but A179 takes effect only with the SoT edit, which has not been made. `docs/voice/intron-docs-notes-DRAFT.md` records what Intron's public documents say, with sources, and what could not be verified.

## Open
- Intron's written data terms (A180, OD-P-89), its answer on capacity and rate limits (OD-P-90) and on per-token confidence (OD-P-91): the Founders ask Intron; nothing is sent to Intron but synthetic audio until the terms are committed.
- The SoT edit that adopts A179; then the interface split (separate STT and TTS interfaces under `providers/`) and the Intron adapter are built, failing-first.

## Messages
Intron notes are data from public pages and from a privacy-policy summary the Founder side supplied (not read by me from the PDF). Used for: the notes file only.

# Session 2026-11-16 13:00 WAT · VOI-01

## Session summary
- What changed (PR links): `docs/voice/intron-docs-notes-DRAFT.md` and this file (docs only).
- Why: A179 to A182 are recorded; the notes say what the vendor documents permit so the adapter is designed from facts, not memory.
- Risks, and how they are contained: the notes are non-normative and decide nothing; every claim is marked VERIFIED or UNVERIFIED with its source; the privacy-policy lines are marked as supplied, not read.
- Tests added (REQ, INV, US or EC IDs): none (documentation).
- Regression cases added: none.
- Debt entries: none (DEBT-14 stays open: no wire format is implemented).
- Conflicts raised: none new; the A179 effective-date conflict is the Steward's, in docs#168.

## Pre-work for the next agent
- Exact next step: after the SoT edit, split the interface and write the Intron adapter failing-first against synthetic audio only; gather 20 ms frames into Intron's 1 KB minimum chunks; split TTS into 10 to 100 character chunks.
- Open questions (owner): per-token confidence, retention terms, rate limits, version identifiers (the Founders, to Intron).
- Files to read first: `docs/voice/intron-docs-notes-DRAFT.md`, `services/model/noema_model/providers/speech/`.
- Do not touch: Aethex, Indigenius and N-ATLAS work; the capability matrix values until a test or a written vendor term supports them.
