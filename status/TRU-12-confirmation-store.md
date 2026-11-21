# Status: TRU-12-confirmation-store

State: A368 red pushed (noema#756); A368 green committed locally, not pushed (A332, A340).

# Session 2026-11-19 14:00 WAT · TRU-12 (confirmation request and answer store)

## Session summary
- What changed (PR links): noema#756. New migration `0032_confirmation_request_answer.sql` (tables `confirmation_request`, `confirmation_answer`, `confirmation_expiry`), its README paragraph, `platform/db/confirmation_request_test.go`, and `orchestrator/portadapters/trustpg2/requests.go` and `requests_test.go` (`Requests`: `Latest`, `Open`, `Expire`, `Answer`).
- Why: the confirmation adapter (noema#750) has no store for a request (eight bound values, requirement, requester, expiry, supersession) or for the approver's answer. REQ-TRU-009, REQ-AUD-001, REQ-SEC-004; INV-11, INV-12, INV-17, INV-25.
- Risks, and how they are contained: the migration number is claimed, not merged (EL on noema#183). The DB tests need pgsodium and run in CI only. Safer readings are listed in the PR body.
- Tests added (REQ, INV, US or EC IDs): REQ-TRU-009, REQ-SEC-004, INV-11, INV-12 (see the two test files).
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: after noema#750 merges, swap the local types of `requests.go` (`NewRequest`, `RequestRecord`, `AnswerRecord`) for the `trustport` ones so `Requests` satisfies `trustport.ConfirmationStore`; remove the duplicate `TestMain`; write the Postgres `RequestEvents` appender over the Record and the route that calls `Answer`.
- Open questions (owner): confirm-request and confirm-event contracts do not exist on main (EL, C1); the Unix-second unit of `ExpiresAt` and `At` (EL).
- Files to read first: `services/core/migrations/0032_confirmation_request_answer.sql`, `trustpg2/requests.go`, `status/TRU-12-confirmations.md` on the adapter branch.
- Do not touch: migration 0028, `trust/confirm`, `trust/recorder`.

## Addendum 2026-11-19 (A341, interim, relayed, not yet committed)
- Migration 0032 is not merged, so it was edited IN PLACE: `UNIQUE (tenant_id, confirmation_request_id)` on `confirmation_answer` replaces the per-approver key (first answer wins). `Answer` returns typed `ErrAlreadyAnswered` for any second answer except the same approver's same response (a replay, returns the stored event). Safer reading: the same approver answering DIFFERENTLY is also `ErrAlreadyAnswered` (was `ErrRequestConflict`).

## Addendum 2026-11-19 (A368, interim, relayed, not yet committed)
- What changed: migration 0032 edited in place again (unmerged): `requested_at`, `answered_at`, `observed_at`, `recorded_at` are the database's `now()` (BEFORE INSERT triggers; column defaults too); `intent_version >= 1`; the answer trigger refuses an answer after the expiry (SQLSTATE 23E01) or after a successor exists (23S01, decided first), under a per-request advisory lock shared with the superseding insert; a request that already has an answer is left to the unique key. Store: `Requests.Clock` and `AnswerInput.At` REMOVED, `NewRequest.ExpiresAt` replaced by `TTL` (the database computes `now() + TTL`), `Version < 1` refused in Go before the insert, typed `ErrRequestExpired` and `ErrRequestSuperseded`, `Expire` decides on `expires_at <= now()` in SQL.
- Findings answered: Codex P1 and C2 P1 (one answer per request: already UNIQUE, kept and tested); SR requirement 1 (no ordering by a caller time: there is one answer, `Latest` needs no ORDER BY) and 2 (database clock); SR requirement 3 (version >= 1); C2 P2 (answer bound to a live, unsuperseded request); Voice P3 (expiry time is the database's) and Voice comment 1 (header no longer says "no foreign keys"); IL P2 (`ErrNotExpired` comment) and IL P3 (the store trusts the roles it is given; the route is the check).
- Safer readings: a superseded request is refused with 23S01 even when it is also expired; the chain still allows a request answered DECLINE and then superseded (C2's question: intended, the answer is history). The race test cannot make the not-yet-expired interleaving deterministic; the advisory lock is the mechanism and the test asserts the invariant (no answer on a superseded request).
- Adapter note for noema#750: build `NewRequest.TTL` from the policy's time-to-live and drop `At` and `Clock`; the Event.At of a read answer is the database's answered_at.
