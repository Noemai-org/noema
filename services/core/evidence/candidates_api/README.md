# evidence/candidates_api

Date: 2026-11-24 · Owner: Core Engineer 2 (C2) · Task: EVD-04 · Ruling A645 (interim, `DEC-2026-11-24-architect-rulings-a644-a656`: "The Candidates read (REQ-CTX-006) is C2's, as EVD-04") · All data is SYNTHETIC.

**What it does.** `Reader.Read(ctx, candidates-request.v1) (candidates-response.v1, error)` is the principal-scoped candidates read. It asks the candidate search (`adapters.Transactions`) as the request's tenant and principal, which the caller took from the authenticated session and never from the model (INV-12), and answers the masked, candidate-shaped rows, newest first. It decides nothing and calls no model (INV-01). It is the real `CandidateReader` of `orchestrator/portadapters/trust` (noema#741).

**REQs.** REQ-CTX-006, REQ-CTX-007, REQ-AMB-002, REQ-SEC-003. **SoT.** INV-01, INV-11, INV-12, INV-14, INV-21, AGENT-13, AGENT-16.

## Rules, and what refuses

| Rule | Refusal |
|---|---|
| a source, loaded masking limits, a positive read timeout and the pack's resource kind; none has a default | `ErrConfig` (also for a zero `Reader`) |
| tenant, principal and correlation of the contract's shape; the resource kind is the pack's; a time window of two RFC 3339 UTC instants with `from` not after `to`; an amount range with `min` not above `max`; a counterparty surface of at most 64 characters | `ErrRequest`, before the source is touched (no window, no read: REQ-AMB-002) |
| the search fails or never answers (the reader's own deadline) | `ErrRead`, the cause reachable with `errors.Is` |
| the search found more than 20 (it is asked for 21) | `ErrTooMany` |
| the page is not marked complete, or has a cursor | `ErrIncomplete` |
| a row the reader cannot make safe: a duplicate reference, a direction, amount, currency or time off the contract, an account with no four digits to show, an empty name | `ErrMalformed`, the whole answer |

An error is never an empty list. A complete empty page is an empty answer (the intake turns it into ESCALATE). Error text is fixed and carries no row, reference or identifier.

## What an answer holds

At most `MaxCandidates` (20, the contract's maxItems), newest first, a tie broken on the candidate id (INV-21). The candidate id is `cand_` plus 16 hex characters of SHA-256 of tenant, principal and reference: stable and not the reference. `resource_ref` is `<resource kind>:<reference>`. `account_last4` is the last four digits of the account. `counterparty_display` is the name with every digit run of five or more masked down to its last four (`max_unmasked_digit_run`, from `platform/masking`), cut at 64 characters. `beneficiary_alias_match` has no source here and stays unset.

## Not here

- **No Record event.** The pack has no evidence-source descriptor for the candidate search, so there is nothing to record as an `EVIDENCE_READ` item; whether a Trust re-read needs its own event is SR's open question on noema#741. Nothing is recorded until that is decided.
- **Another principal's rows are the source's to exclude** (the real source scopes by the principal's headers, US-07 fixture). The reader passes the session's scope and nothing else, and its test world scopes rows by it.
- **The endpoint** `POST /internal/v1/evidence/candidates` and its composition are EL's. **The name-masking rule** (the contract says "never an unmasked name") has no source in the documents: only digit runs are masked here. OPEN entry for the Steward.

## Tests

`go test -race ./evidence/candidates_api/` from `services/core/`. No database; the faults are a context that really expires and pages that really are out of contract.
