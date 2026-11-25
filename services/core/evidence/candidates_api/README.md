# evidence/candidates_api

Date: 2026-11-24 · Owner: Core Engineer 2 (C2) · Task: EVD-04 · Ruling A645 (interim, `DEC-2026-11-24-architect-rulings-a644-a656`: "The Candidates read (REQ-CTX-006) is C2's, as EVD-04") · All data is SYNTHETIC.

**What it does.** `Reader.Read(ctx, candidates-request.v1) (candidates-response.v2, error)` is the principal-scoped candidates read. It asks the candidate search (`adapters.Transactions`) as the request's tenant and principal, which the caller took from the authenticated session and never from the model (INV-12), and answers the candidate-shaped rows. **No name of any kind leaves it (A676, interim, HIGH, privacy):** the answer carries an opaque counterparty reference, the amount as integer minor units plus currency, the time, the last four digits, the direction and an exact recipient match. It decides nothing and calls no model (INV-01). Its output is `candidates-response.v2`; the Trust adapter's `CandidateReader` port still reads v1 and moves afterwards (C1 and EL).

**REQs.** REQ-CTX-006, REQ-CTX-007, REQ-AMB-002, REQ-SEC-003. **SoT.** INV-01, INV-11, INV-12, INV-14, INV-21, AGENT-13, AGENT-15, AGENT-16.

## Rules, and what refuses

| Rule | Refusal |
|---|---|
| a source, a recipient matcher, a positive read timeout and the pack's resource kind; none has a default | `ErrConfig` (also for a zero `Reader`) |
| tenant, principal and correlation of the contract's shape; the resource kind is the pack's; a time window of two RFC 3339 UTC instants with `from` not after `to`; an amount range with `min` not above `max`; a counterparty surface of at most 64 characters | `ErrRequest`, before the source is touched (no window, no read: REQ-AMB-002) |
| the search fails or never answers (the reader's own deadline) | `ErrRead`, the cause reachable with `errors.Is` |
| the search found more than 20 (it is asked for 21) | `ErrTooMany` |
| the page is not marked complete, or has a cursor | `ErrIncomplete` |
| a row the reader cannot make safe: a duplicate reference, a direction, amount, currency or time off the contract, an account with no four digits to show, an empty name, a match that is not an exact share of one or a matcher that fails | `ErrMalformed`, the whole answer |

**An amount range of min 0 and max 0 (A689)** can match no candidate (a candidate's amount is above zero), so it is answered with the empty, complete answer and the source is not called; a range of 0 to 1, 1 to 1 or wider still calls it. **A candidate's currency and amount are checked with the generated `money.v1` type (A689):** the pair is decoded through `contracts.MoneyV1Money`, so the currency must be a value of the generated enum (the POC set, NGN today; USD is refused) and the amount an integer; the amount must also be within the contract's bound (2^53-1). The generated Go amount type is a bare `int64` with no bound, so the bound is a constant here and a test reads the maximum from `contracts/schemas/money.v1.json` and fails if they differ. A zero or negative amount is refused (the adapter's `Candidate.Validate`). A refusal is the whole answer with the fixed text below.

An error is never an empty list. A complete empty page is an empty answer (the intake turns it into ESCALATE). Error text is fixed and carries no row, reference, identifier or name. A row with an empty name (after trimming) is `ErrMalformed`, as before.

## What an answer holds

At most `MaxCandidates` (20, the contract's maxItems), newest first, a tie broken on the candidate id (INV-21). The candidate id is `cand_` plus 16 hex characters of SHA-256 of tenant, principal and reference: stable and not the reference. `resource_ref` is `<resource kind>:<reference>`. `account_last4` is the last four digits of the account. `beneficiary_alias_match` has no source here and stays unset.

- **`counterparty_ref`** is `cp_` and an ordinal (`cp_1`, `cp_2`, ...), numbered in the answer's own order (newest first, ties on the candidate id) by the first appearance of each **normalised name** (lower case, white space collapsed): the same counterparty gets the same one within the answer. It is not derived from the name, uses no key, no hash and no source id, and carries nothing of the name's letters, length or digits; two answers whose names differ but are equal in the same places are byte for byte the same (a test). It means nothing outside the answer: it groups counterparties **within one candidate set** only. A reference that holds across sets needs a Tushe beneficiary id or a key decision (an [OPEN] entry, requested of the Steward in the PR).
- **`recipient_match`** is `{numerator, denominator}`, an exact share in [0, 1] computed in core by the `RecipientMatcher` port from the request's `counterparty_surface` and the name; 0/1, with the matcher not asked, when the request has no surface. It is an exact share and not a bucket or basis points so that ranking does not change by rounding (AGENT-16). A matcher that fails, or a share outside [0, 1], refuses the whole answer with fixed text; the matcher's own text is dropped because it may hold the name.
- **A name** is used inside the read for two things: the match and the equality of two names. It is never returned, never in an error, never in a log or an event (tests walk the whole error chain and capture the default loggers).

## Not here

- **No Record event.** The pack has no evidence-source descriptor for the candidate search, so there is nothing to record as an `EVIDENCE_READ` item; whether a Trust re-read needs its own event is SR's open question on noema#741. Nothing is recorded until that is decided.
- **Another principal's rows are the source's to exclude** (the real source scopes by the principal's headers, US-07 fixture). The reader passes the session's scope and nothing else, and its test world scopes rows by it.
- **The endpoint** `POST /internal/v1/evidence/candidates` and its composition are EL's. **The production recipient matcher.** The pack's `recipient_matcher.json` (tiers, folding, nicknames, variants, phonetic rules) is matched today by the model service's `RecipientMatcher` in Python. Core needs the same algorithm with exact fractions (`math/big`) and Unicode folding (NFC, case folding, NFKD), which Go's standard library lacks: a `golang.org/x/text` dependency (AGENT-14) and a manifest kind for the matcher file. Neither is decided here; the reader takes the matcher through a port, and the composition cannot build the reader until the matcher exists. The request has no `phonetic` flag either (the Python matcher has a voice-path mode).

## Tests

`go test -race ./evidence/candidates_api/` from `services/core/`. No database; the faults are a context that really expires and pages that really are out of contract.
