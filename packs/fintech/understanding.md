# packs/fintech: the `understanding` block and the matcher reference

**Implements:** REQ-AMB-002, REQ-AMB-003, REQ-INT-003, REQ-INT-005 (PERD §20.3); DEC-2026-10-20 D3 and D5.
**Asked for by:** Session D (noema#43, "Needs session A"). **Read by:** the model service's ranking and pipeline (`noema_model.understanding`).

The block is `understanding` in `packs/fintech_v1.json` (schema: `pack.v1.json`, `Understanding`). Every value is one the PERD already gives; nothing is tuned here (AGENT-16). Numbers are strings (`"0.35"`), never JSON numbers (INV-21). The margin (0.35) and the limit of four stay in the intent node's `disambiguation`, where the SoT shape puts them.

| Field | Value | Source and label |
|---|---|---|
| `ranking.weights` | amount 0.35, recipient 0.25, temporal 0.20, relationship 0.10, type_direction 0.10 | PERD REQ-AMB-003 table, [ADOPTED DEC-2026-10-17]. Sum exactly 1 (packcheck) |
| `ranking.absolute_floor` | 0.70 | REQ-AMB-003, [ADOPTED DEC-2026-10-17] |
| `ranking.max_raw_candidates` | 20 | REQ-AMB-002 and REQ-CTX-006 ("at most 20") |
| `ranking.amount_tolerance_ratio` | 0.01 | REQ-AMB-002: "± 1% when the amount is uncertain", **[REC]**. The PERD gives it for the search range; the ranking's amount feature uses the same ratio, which is a reading of mine |
| `ranking.time_tolerance_seconds` | 7200 | REQ-AMB-002: "± 2 h", **[REC]** |
| `proposable_flags` | MULTIPLE_INTENTS, CONTRADICTS_PRIOR_TURN, each with a yes-or-no question | Flag names: PERD §20.3. **The question wording is mine** (an agent default [REC]); it is a prompt to a model, not customer copy |
| `locales` | en-NG, pcm, yo, ha, ig | The five locales of `explanation-facts.v1.json` (PERD §31.2). Labels are mine |
| `material_flags` | the eight flags §20.3 marks "Yes" | PERD §20.3. AMOUNT_MISSING and TIME_VAGUE ("only if needed to separate candidates") are conditional, so they are not here; OUT_OF_SCOPE_REQUEST ("n/a") is `out_of_scope_flag` |
| `intent_unclear_flag`, `out_of_scope_flag` | INTENT_UNCLEAR, OUT_OF_SCOPE_REQUEST | PERD §20.3 |
| `state_flags` | NO_CANDIDATES to NO_TRANSACTION_MATCH; AMBIGUOUS and TOO_MANY_CANDIDATES to MULTIPLE_TRANSACTIONS_MATCH | PERD §20.3 and the handling table. COUNTERPARTY_MULTIPLE_MATCH needs a counterparty-level ranking that the engine does not do; no state maps to it (a gap) |

**Matcher reference.** The node `fin.payments.exception.investigate` has `disambiguation.matcher_ref: "fintech/lexicons/recipient_matcher.json"`, relative to the pack file's folder like `action_graph_files`. packcheck refuses a ranking node without one, a reference to a file that does not exist, and a reference that leaves the folder. The model image's build context is `services/model` only, so the file reaches the service by a mounted pack directory (compose), not by the image; mounting it is a separate step.

**Gaps.** The three conformance packs also rank candidates; they carry no `understanding` block or matcher (packcheck skips the presence rules for conformance packs only). `fin.lex.*` lexicons (amounts, numerals, relative times) are still UND-05's.
