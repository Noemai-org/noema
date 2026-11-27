# Status · EVD-04 (Core Engineer 2) · the principal-scoped candidates read

Updated 2026-11-24.

## State
- A645 (interim) gives the Candidates read (REQ-CTX-006) to C2 as EVD-04. **A676 (interim, HIGH, privacy) round, 2026-11-25:** counterparty names are not returned at all. The read is now `candidates-response.v2` (new major, v1 not edited, AGENT-15): an opaque `counterparty_ref` (`cp_1`, `cp_2`, ... numbered in the answer's own order by first appearance of each normalised name; no key, no hash, no source id), the amount as integer minor units plus currency, the time, the last four, the direction and an exact `recipient_match` computed in core through a matcher port from the request's `counterparty_surface`. The Record and the logs carry no name (the reader has neither; a test walks every error in the chain and captures the default loggers).
- Built in this PR: `contracts/schemas/candidates-response.v2.json` (+ fixtures, regenerated Go and Python models, registry, shape refusals, migration note, INVENTORY row), the lint's allowed names (v2's two names added; `counterpartydisplay` stays for v1 only), `evidence/candidates_api` (Reader v2, `RecipientMatcher` port, `Match`), tests first.
- **Not in this PR (stated, not hidden):** (1) the production recipient matcher in Go (see Open); (2) the consumers of v1: the Trust adapter and ownership (EL), the model service ranker, evidence client and pipeline screen (C1 and C2), the pack templates that name `counterparty_display` and packcheck's candidate-field list, the benchmark builders; (3) the endpoint and the composition (EL). A676 says C1 and EL consume the new shape afterward.

- **A689 round, 2026-11-25:** (1) an amount range of min 0 and max 0 is answered empty without calling the source; (2) a candidate's currency and amount are checked with the generated `money.v1` type (USD refused; the amount bound 2^53-1 is a constant pinned to the schema by a test, because the generated Go amount type carries no bound). (3) **A695 approves `golang.org/x/text` and one manifest kind for the Go recipient matcher, and both Founders approve at the #960 merge; the matcher is still not built** (the Founder did not ask for it in this round).

- **A689 round 2 (Codex P2-1, P2-2; SR):** the request's amount range is bounded at the contract's maximum and its currency checked through the generated money.v1 enum, before the source is called (the counting double sees no call on any refusal).

## Open
- **OPEN entry requested of the Steward:** the counterparty ordinal groups counterparties **within one candidate set** only; a reference that holds across sets needs a Tushe beneficiary id or a key decision.
- **The production matcher.** The pack's `recipient_matcher.json` is matched in Python today (exact fractions, Unicode folding NFC/casefold/NFKD, a voice-path phonetic mode). Core needs the same with `math/big` and a Unicode normalisation that the Go standard library lacks: `golang.org/x/text` as a direct dependency (AGENT-14, an [OPEN] entry and approval) and a manifest kind for the matcher file (the manifest, `packload`, `packcheck` and the schema, on lines #964 also edits). The request also has no `phonetic` member (a contract change). The reader takes the matcher through a port, so the composition cannot build it until the matcher exists.
- **The share is exact, not a bucket.** A coarse bucket or basis points would round and change ranking (AGENT-16, the shared vector file `contracts/fixtures/candidate_ranking_vectors.json`, which is name-free and unchanged here). The Python ranker takes `Fraction(numerator, denominator)` as the recipient feature, which is what its matcher returns today.
- Whether the response should carry a completeness marker: today more than 20 matches is `ErrTooMany` and an unmarked page is `ErrIncomplete`. Architect.
- No Record event: no evidence-source descriptor exists for the candidate search (SR's question on noema#741 stays open).

## Messages
- 2026-11-24 · Founder to C2: "you own the Candidates read (EVD-04) and the injection-screen patterns" (A645).
- 2026-11-25 · Founder to C2: A676, names are not returned; the per-response ordinal; `candidates-response.v2`; the recipient match in core; both Founders approve the contract at merge.

# Session 2026-11-24 · EVD-04

## Session summary
- What changed (PR links): the candidates read (this PR).
- Why: A645.
- Risks, and how they are contained: a leak across principals (the scope is the request's; the test world scopes rows by it), a leak of a name (none is returned: a test over hostile names, a name equal to a reference, spellings that normalise alike, digits only, an empty name; fixed error text; the matcher's text is dropped), a cut list (refused whole).
- Tests added (REQ, INV, US or EC IDs): REQ-CTX-006, REQ-AMB-002, REQ-SEC-003; INV-01, INV-12, INV-21; US-07 (scope).
- Regression cases added: none (no bug fixed).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL composes the endpoint and passes the real `adapters.Transactions`, the masking limits and the pack's resource kind to `candidates_api.New`; the Ranker (noema#918) and the Labeller (EL) complete the trust adapter's parts.
- Files to read first: `evidence/candidates_api/README.md`, `orchestrator/portadapters/trust/candidates.go`.
- Do not touch: the red tests' meaning.

# Session 2026-11-26 · EVD-04 (the Go recipient matcher, A710)

## Session summary
- What changed (PR links): the PR of branch `feat/EVD-04-go-recipient-matcher`: `evidence/recipientmatch` (the matcher, its loader, README), the manifest kind `recipient_matcher` (schema, packcheck, `platform/packload`), the shipped manifest at version 5 with the matcher's data pinned, `golang.org/x/text` as a direct dependency of core (A695, OD-P-117), and 644 shared parity vectors (`contracts/fixtures/recipient_match_vectors.json`) with the Python test that loads them.
- Why: A710 orders the production `RecipientMatcher` that the Candidates reader (merged, #960) takes through its port, with the same results as the Python matcher.
- Risks, and how they are contained: a Go result that differs from the Python reference (the shared vectors, on the shipped data and on a dataset with five distinct tier scores; mutants over each tier and folding step); a matcher that fails or a share past an int64 (`ErrShare`, fixed text, the reader refuses the whole answer, A676); a gated manifest without the matcher (refused in the schema, packcheck and the loader, no opt-in).
- Tests added (REQ, INV, US or EC IDs): REQ-AMB-003, REQ-CTX-006; INV-14, INV-21; AGENT-13, AGENT-16.
- Regression cases added: none (new code).
- Debt entries: none.
- Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: EL builds Ownership and Candidates after this matcher: load the pack, build `recipientmatch.Load(pack.RecipientMatcher)` and pass it as `candidates_api.Config.Matcher`.
- Open questions (owner): the phonetic tier is not reachable through the port because the request has no `phonetic` member: add an optional member to `candidates-request` (Architect).
- Files to read first: `services/core/evidence/recipientmatch/README.md`, `contracts/fixtures/recipient_match_vectors.json`, `services/core/platform/packload/README.md`.
- Do not touch: `contracts/fixtures/candidate_ranking_vectors.json` and any threshold, margin or default (AGENT-16).
