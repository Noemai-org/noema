# explanation/recorder

The Record-backed `Recorder` of the Explainer (EXP-01 slice 5, ruling A155; REQ-EXP-002, REQ-AUD-001, REQ-AUD-002, REQ-AUD-003; INV-11, INV-12, EC-E16). Updated 12 Nov 2026.

`Recorder.AppendExplanation(ctx, explanation.Subject, explanation.Event)` appends one event to the Record as `core-edge`, in its own transaction, so a failed append stores nothing and the Explainer returns no text (it records before it returns).

| Event | Payload |
|---|---|
| `EXPLANATION_RENDERED` | `claims`: one claim `{text, bound_fact_status, disclosed_refs, fact_ids?, fact_id?}`, the shape `record/reconstruct` reads (`text` 1 to 300 characters, `bound_fact_status` `KNOWN` when facts filled the template and an explicit `null` when none did, `fact_id` only when exactly one fact did, `disclosed_refs` empty: this slice discloses no resource reference); `validator_result` `PASSED` (PERD 28.2); `template_id`, `template_version` |
| `EXPLANATION_BLOCKED` | `rule` (the first code, PERD 28.2 `EXPLANATION_BLOCKED{rule}`) and `rules` (all codes, sorted); never text, a fact or a template |

Common: `written_by` and `actor` are `core-edge` (no authenticated person caused the event: a person asked, the system rendered the answer); `object_ref` is `exp_` and the correlation id's suffix, so it is deterministic (INV-21); the tenant is the login's, bound by the database (migration 0011), so a subject of another tenant is refused.

**Refused before it is written** (fail closed; a Record the reconstruction cannot serve is worse than a missing event): a text of more than 300 characters or none; an unmasked digit run of more than 4 in any script (ruling A154); a rendered event without a template id and version, or with rules; a repeated or malformed fact id; a blocked event without a rule, with a lower-case rule, or carrying any text, fact or template; a correlation id that is not `corr_…`.

**Who may call it.** `AppendExplanation` writes `validator_result: PASSED` for any text inside its own bounds: it does not know the facts or the forbidden claims, so it cannot tell a validated text from another. Only the Explainer, after the validator has passed the text, calls it; the composition root reaches the recorder through the Explainer's `Recorder` interface and nowhere else. `explanation/recorder_callers_test.go` fails if any other code in the core calls it (review of noema#493, Security Reviewer, note 1). `Subject.PrincipalID` is required but never written to the Record: the actor is `core-edge` (REQ-AUD-001 asks who caused the event; no person did). A rendered and a blocked event for one correlation share the object reference `exp_<corr suffix>`; the reconstruction does not need them to differ, and a reader that keys on the reference should not assume it is unique per event. The blocked event stores its rule codes sorted and without repeats, whatever order it was given.

**Not here.** The real `FactsSource` and the adapter to the orchestrator's `Explainer` port in `compose.go` (EL, A155); the pack templates with slots (a Founder approval, A155); a claim per sentence for a text of more than 300 characters (the pack's longest template is 146 characters; a longer text is refused, not split).

**Open, for the Security Reviewer and the Architect (not decided here).** (1) The digit check counts runs (`maxDigitRun = 4`), so `1234 5678 9012 3456` passes, here and in the Explainer's own check (`explanation.go`). Counting digits across separators would also refuse a formatted amount such as `20,000.50` (7 digits), so which rule is meant is a decision about ruling A154, not a fix: the single source is `platform/masking` in noema#468, and this recorder's second copy of the threshold moves to it when #468 merges. (2) `FactIDs` and `Rules` have no count cap; a bound is a limit to be chosen, not defaulted here. (3) The same call twice for one correlation appends two events (a re-explain, or a retry after a commit error that landed); how `reconstruct` should read two is REC-04's. 

**Open.** `bound_fact_status` of a template-only claim is `null` (bound to a template, not to a fact); REC-04 or the Architect may want a different marker. The `fact_ids` and `validator_result` members are in the Record and hashed but never served by the reconstruction.

**Tests.** `go test ./explanation/recorder/` from `services/core/`. The rule tests (`build_test.go`) need no database. The others (`recorder_test.go`) run against PostgreSQL with pgsodium, as the Record requires per-emitter signatures (REC-08): CI's `core` job, or a local server with the extension.
