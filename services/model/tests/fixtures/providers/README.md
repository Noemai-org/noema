# tests/fixtures/providers

**These are synthetic fixtures, not recordings.** No vendor API key was available to session D, so each file is written by
hand from the vendor's published response shape (message content, finish reason, token usage, model version) and then
extended with the awkward cases the adapters must survive: a reasoning block that must be dropped, a refusal, a cut-off
reply, a non-object body. Nothing here came from a live call.

Status of the contract tests that read them: **not run against a live endpoint: no access.** When a key is available,
record a real response per adapter, replace the matching file here, keep the extended cases, and run a live smoke test
(REQ-INT-002, Tests). Until then a vendor wire-format change is not detected by this suite.

Every identifier in these files is invented, and the credential in `../credential.txt` is a placeholder.

## `openai_decisions_*.json` (a different provenance)

The OpenAI Decisions fixtures come from OpenAI's own documentation, read on 22 Oct 2026 once the founder allowed the host:
the endpoint page (`/api/reference/resources/decisions/methods/create`), the guide (`/api/docs/guides/decisions.md`), the model
page (`/api/docs/models/gpt-6-luna.md`), the pricing, error-code, rate-limit and data-controls pages. They are documentation
examples, not recordings of a live call, and no live call has been made: not run: no access.

- `openai_decisions_request.json` and `openai_decisions_ok.json`: the predicate example from the endpoint page, as first pasted
  by the founder and then confirmed on the page.
- `openai_decisions_choice_request.json`, `openai_decisions_score_request.json`: the guide's request examples, verbatim.
- `openai_decisions_choice_ok.json`, `openai_decisions_score_ok.json`: the guide's response excerpts, verbatim. The guide shows
  only the `answers` array, so the `model` and `usage` objects around them are written here in the endpoint page's shape
  (the token numbers are invented).

Every other Decisions case the tests use (malformed answers, wrong counts, refusals) is invented, and says so in the test.

## `openai_responses_*.json`

Synthetic, written from the Responses endpoint reference (`/api/reference/resources/responses/methods/create.md`, read 2026-10-27): a completed response with a reasoning item and one message, an `incomplete` one (`max_output_tokens`), and a refusal. The ids and token numbers are invented. No live call was made: not run: no access.

