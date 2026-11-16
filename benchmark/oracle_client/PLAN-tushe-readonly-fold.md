# Plan: the harness reads Tushe's payout transitions through the read-only key (M-02 fold, AT-19 trace join)

**Task:** BEN-03 follow-up (oracle client; REQ-ENV-002, REQ-BEN-002, REQ-ENV-007). **Owner:** Benchmark Owner role agent. **Written:** 16 Nov 2026. **Status: a plan. Nothing here is built, and nothing here reads a credential.**

## 1. What is asked, and what it rests on

Tushe's addendum of 15 Nov 2026 (committed unchanged by docs#188; its text is not in this repository as text, only as a PDF, so I quote it as the Repo Steward summarised it in `DEC-2026-11-16-tushe-addendum-od-p-67`) says, in the Steward's words and not in mine:

- **M-01:** a read-only programmatic credential already exists as a partner API key with read-only scopes. The addendum lists eight: `events:read`, `audit:read`, `accounts:read`, `customers:read`, `cards:read`, `bills:read`, `compliance:read`, `group_savings:read`.
- **M-02:** the payout transition log exists. "State at a point in time" is a **client-side fold of the transitions up to a timestamp**. Correlation and causation are on the **event feed**, not on the transition row: a **two-call join**.

Ruling A240 (interim; recorded in docs#187, which counts on a non-author Founder's merge) says the Founder creates the read-only key `noema-harness-readonly` with the eight scopes, registers the webhook relay and rotates its secret, and that every value goes in the **bench Environment only**, never in the runtime or model environments (INV-22). So the harness (the Benchmark Owner's side, after a run) is the one reader.

AT-19 as the frozen specification words it: "Audit completeness. Inspect a material transition. **Actor / request / correlation / state / timestamp metadata all present.**" Noema covers it by reading and checking, not by trusting a report.

## 2. What may and may not be built now (AGENTS section 2, item 4, and section 14)

- **The addendum does not govern endpoint semantics.** DEC-2026-10-16 E10 lists what does (the specification, the 7 Oct response, DEC-2026-10-12, delta v1.2, the signed worksheet). The Steward's record says no adapter or test is built on the addendum's routes until a delta file adopts them. **So I build no reader, guess no route name and no field name.** Doing so would be inventing endpoint semantics.
- **What can be built before that: two pure functions on rows I describe as ASSUMED**, below, with synthetic data and the shape written as a parameter of the function, so adopting the real shape changes one mapping, not the logic. This is allowed because it asserts nothing about Tushe's behaviour; it asserts what Noema will require of whatever Tushe returns.

## 3. Design

### 3.1 Where it sits

`benchmark/oracle_client/` already has the one reader of ground truth for **mock-case** (the bench credential from a file, `GET` only, after the run; `README.md` says Tushe-held state is not read yet because the POC's writes are all on mock-case). This plan adds the Tushe half as separate modules, not a change to the mock-case ones:

| Module (planned) | What |
|---|---|
| `tushe_fold.py` | `fold_transitions(rows, at)`: pure. The M-02 fold |
| `tushe_trace.py` | `join_trace(transition, events)`: pure. The AT-19 two-call join and its completeness check |
| `tushe_readonly.py` | `TusheReadOnly`: the reader over the existing `JsonClient`, **GET only**, the key from a file, a base URL that must be https. **Built last, after the delta file names the routes and shapes** |

It is used by the scorer for scenarios whose truth is Tushe-held, in the **live-Tushe rerun**. It is **not on G1's path**: G1 runs on the stand-in and the replay provider (Founder, 14 Nov), where the truth is mock-case.

### 3.2 The fold (M-02), fail-closed

Input: the transition rows of one payout (ASSUMED fields: an id, an occurrence timestamp, a sequence number if the log gives one, `from_state`, `to_state`) and a timestamp `at`. Output: `{state, used: [transition ids], status}`.

- **`status` is one of** `OK`, `NOT_YET_EXISTING` (no transition at or before `at`: never "initial state"), `CHAIN_BROKEN` (a transition's `from_state` is not the previous `to_state`, or two transitions leave the same state: a gap or a branch), `ORDER_AMBIGUOUS` (two transitions share the same timestamp and the log gives no sequence to order them), `UNREADABLE` (a row lacks a field or a timestamp that does not parse). **Only `OK` yields a state.** Anything else yields no state, and the scorer treats it as "the oracle could not say", never as a pass (the same rule as `CounterUnavailable`).
- **Order** is by (timestamp, sequence). The function never relies on input order: the property tests permute the input.
- **Prefix property:** the fold at `at` equals the fold of exactly the prefix of transitions at or before `at`; folding at a later time never changes an earlier answer.
- **No clock is read inside it.** `at` is an argument, so the replay clock (SYNTHETIC) and the real one are the caller's business (TEN-05).

### 3.3 The trace join (AT-19), fail-closed

Input: one transition row and the events returned by the second call. The join key is **the trace identifier the two calls share** (the Founder's word is `traceId`; which member of which payload carries it is **not known**, section 5). Output: `{status, missing: [...]}`.

- **`COMPLETE`** only if exactly one event matches and the five AT-19 elements are all present and non-empty: **actor, request, correlation, state, timestamp** (state and timestamp from the transition row, actor, request and correlation from the event).
- **`NO_EVENT`** (the transition has no event in the feed: audit incomplete), **`AMBIGUOUS_JOIN`** (more than one event matches the key), **`INCOMPLETE`** (matched, but named elements missing: the list says which). None of these counts as a pass.
- An event that is a *duplicate delivery* of the same message (AT-06) must not make a join ambiguous: it is collapsed by its message identity first, and the collapse is reported, not hidden. Which member is the message identity is also unknown (section 5).

### 3.4 The reader (last)

`GET` only (a test fails if any other verb is possible), https only, no redirects (the existing `JsonClient`), the key read from a file with `read_secret` (never an argument, an environment value or a log line), paging followed to completion with a hard limit, every non-2xx or timeout is `TransportError` and **never** a result of zero rows. It reads **after** the run, from the bench Environment, and never from the runtime or model environments (INV-22). The value is never written to a log, a summary or an artifact; the leak scan's planted canary (noema#591) covers the artifact.

## 4. Tests, written first and seen failing (AGENT-11)

1. **Fold, unit and property** (Hypothesis, already in the stack): order independence (any permutation of the rows gives the same answer); the prefix property; monotone `used`; `NOT_YET_EXISTING` before the first transition; a gap, a branch, a same-timestamp pair without a sequence and an unparseable row each give their status and **no state**; a duplicated row is `CHAIN_BROKEN` or collapsed by id, said in the result.
2. **Join:** one matching event with all five elements is `COMPLETE`; none is `NO_EVENT`; two distinct events is `AMBIGUOUS_JOIN`; two copies of one message is `COMPLETE` with the collapse reported; each missing element is named.
3. **Reader (only after adoption):** against a local stub that serves only `GET`; a 4xx, a 5xx, a timeout and a truncated page are errors, never an empty result; a `POST` is impossible; the key does not appear in any exception text or log (a planted canary value is searched for); the base URL must be https.
4. **Mutation check** of every guard, as for the other benchmark modules.
5. **Evidence, not assertion:** the first live read is a Founder-dispatched workflow in the bench Environment, printing counts and statuses only (no row content), linked at one final head.

## 5. What is not known (I ask, I do not guess)

1. **Route names, response shapes, paging and rate limits** of the transition log and the event feed. The delta file (a v1.3 exists as a draft that does not mention them) must name them. Owner: Steward, Founders, with Tushe.
2. **The order field:** is there a per-payout sequence, or only timestamps? (decides `ORDER_AMBIGUOUS`.)
3. **The join key and the message identity:** which member is the `traceId` on the transition row and on the event, and which member identifies one delivered message.
4. **Whether the scope list is enough:** which of the eight scopes the two reads need (I expect `events:read` and `audit:read`; the others should not be asked for in the harness, least privilege).
5. **Time:** are the transition timestamps in the simulated clock or the real one, and do they survive a reset (AT-16)? (TEN-05.)
6. **The reply of 10 Nov** (not committed; the addendum corrects it): the Founder supplies it.

## 6. Order of work

1. The Founders or the Architect adopt the addendum's routes through a delta file (Steward writes it). **Blocks section 3.4 only.**
2. The Founder creates `noema-harness-readonly` with the scopes the two reads need and puts it in the bench Environment (A240). I never see the value.
3. I build `tushe_fold.py` and `tushe_trace.py` first (pure, tests first), with the row shape as a parameter and the section 5 questions in the PR, so a reviewer can see what is assumed.
4. After adoption: `tushe_readonly.py`, then the Founder-dispatched read, then the evidence.

## 7. What this does not do

It does not claim AT-19, M-01 or M-02 are met: Noema has seen none of this work (the Steward's note says the same). Acceptance is the joint session and the signed record. It changes no threshold and adds no dependency (standard library and the Hypothesis already in the stack).
