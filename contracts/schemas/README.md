# contracts/schemas

JSON Schema 2020-12, one file per object, `<name>.v1.json`. `$id` is `https://contracts.noema.invalid/<name>.v1.json` (a reserved, never-resolvable host), and `$ref` uses the same base. The row for each file is in [`../INVENTORY.md`](../INVENTORY.md).

Conventions:

- Every object type states `additionalProperties`; it is `false` wherever the source says unknown fields are rejected.
- IDs are `<prefix>_<body>` (PERD §32.3). The wire pattern accepts 2 to 32 alphanumerics so the SoT examples and ULIDs both validate.
- Money is an integer in minor units, bounded 0 to 9007199254740991, never `number` (DEC-2026-10-16 money-and-value-lists M1 to M4). Ratios and confidences are `number`, and are not money.
- A value list is its own named type in `enums.v1.json`. Never reuse a list for a field it does not belong to (OD-P-10).
- Hand-written Go or Python contract types are not allowed. Codegen arrives with CON-03.

Run: `make contracts-validate`. A document written with a decimal point or exponent is never an `integer`, so `2000000.0` and `2e6` are rejected for a money field.

## Pack schema coverage of SoT 15.1 (T2)

SoT 15.1 lists the minimum properties each taxonomy must be able to express, and says a vertical may add fields but never remove these. The table below maps each one to the schema field that carries it. `covered` rows name a path that `packs/tests/test_taxonomy_coverage.py` resolves in the schemas; `gap` rows have none and are open decisions (AGENT-02), not silently dropped. Several properties live in runtime objects (Intent Object, Decision Record, Outcome Verdict) rather than in the pack, because they hold per-request values the pack cannot.

Result: Intent 9 of 11 covered, Trust 16 of 18, Outcome 10 of 11. Five gaps: urgency, model and prompt version, fraud and risk signals, consent, acceptable uncertainty.

| Taxonomy | Minimum property | Status | Carried by | Note |
|---|---|---|---|---|
| Intent | Actor | covered | `intent-object#/principal` | The authenticated principal; never taken from text (INV-12) |
| Intent | Desired outcome | covered | `intent-object#/desired_outcome_refs`, `intent-node#/outcome_definitions` |  |
| Intent | Objects and entities involved | covered | `intent-node#/slots`, `intent-object#/slots` | Slots are typed Resource, Party, Quantity, TimeRef or Reference (SoT 15.2) |
| Intent | Constraints | covered | `intent-node#/slots` | Carried as typed, optionally required slots; there is no separate constraint field |
| Intent | Temporal meaning | covered | `intent-node#/slots` | A `TimeRef` slot, resolved by the deterministic lexicon |
| Intent | Evidence the requester supplied | covered | `evidence-item#/authority_class` | Recorded as `customer_assertion` evidence with authority NONE (PERD 21.4) |
| Intent | Urgency | gap | — | No field in the Intent Object or the intent node. Open decision |
| Intent | Authorisation context | covered | `intent-object#/principal` | `principal_type`, `authn_ref`, `asserted_by` |
| Intent | Ambiguity | covered | `intent-object#/ambiguity_flags`, `intent-node#/disambiguation` |  |
| Intent | Provenance: source and modality | covered | `intent-object#/source` | `mode`, `channel`, `locale_detected`, `asr_confidence`, `raw_ref` |
| Intent | Provenance: model and prompt version | gap | — | Not a field of the Intent Object; `canonical-input#/voice/model_version` is the speech model only. Open decision |
| Trust | Identity | covered | `intent-object#/principal` |  |
| Trust | Authentication state | covered | `decision-record#/authority` | `authority.technical` |
| Trust | Authority | covered | `trust-profile#/authority` |  |
| Trust | Resource ownership | covered | `trust-profile#/authority` | `principal_must_own` |
| Trust | Business role | covered | `trust-profile#/authority` | `roles_allowed` |
| Trust | Policy eligibility | covered | `policy-rule#/condition`, `decision-record#/authority` | `authority.policy` |
| Trust | Action risk | covered | `trust-profile#/risk_class`, `capability-descriptor#/consequence_profile` |  |
| Trust | Regulatory constraints | covered | `trust-profile#/clocks`, `policy-rule#/class` | Clocks as data; class `REGULATORY` |
| Trust | Required evidence | covered | `trust-profile#/evidence_required` |  |
| Trust | Required confirmation | covered | `trust-profile#/approvals` |  |
| Trust | Segregation-of-duties requirements | covered | `trust-profile#/approvals`, `confirmation-request#/approver_requirement` | `MAKER_CHECKER` |
| Trust | Fraud and risk signals | gap | — | SoT: 'where relevant'. No field; the POC pack uses none. Open decision |
| Trust | Data sensitivity | covered | `trust-profile#/data_sensitivity`, `evidence-source#/sensitivity` |  |
| Trust | Consent | gap | — | SoT: 'where the domain requires it'. No field; the POC pack needs none. Open decision |
| Trust | Policy version | covered | `decision-record#/policy_version` |  |
| Trust | Temporal validity | covered | `decision-grant#/expires_at`, `decision-grant#/not_before` | Grants carry the validity window |
| Trust | Tenant boundary | covered | `intent-object#/tenant_id`, `decision-record#/tenant_id` | `tenant_id` on every runtime object; a pack is tenant-neutral |
| Trust | Maximum acceptable evidence age | covered | `trust-profile#/max_evidence_age_seconds`, `evidence-source#/freshness_sla_seconds` | The stricter wins (INV-23) |
| Outcome | Expected outcome | covered | `capability-descriptor#/expected_outcome`, `outcome-definition#/outcome_id` |  |
| Outcome | Authoritative source | covered | `outcome-definition#/verification_sources`, `evidence-source#/authoritative_for` |  |
| Outcome | Observed state | covered | `outcome-verdict#/evidence_reads` |  |
| Outcome | Verification evidence | covered | `outcome-verdict#/evidence_reads` |  |
| Outcome | Verification timestamp | covered | `outcome-verdict#/verified_at` |  |
| Outcome | Terminal and non-terminal states | covered | `outcome-definition#/terminal_states`, `outcome-definition#/intermediate_states` |  |
| Outcome | Outcome confidence derived from evidence | covered | `outcome-verdict#/predicate_result`, `outcome-verdict#/verdict` | A predicate result and a verdict; no numeric confidence, by design |
| Outcome | Reconciliation state | covered | `context-snapshot#/reconciliation` |  |
| Outcome | Recovery and compensation state | covered | `outcome-definition#/compensation`, `capability-descriptor#/recovery`, `recovery-request#/reason` |  |
| Outcome | Acceptable uncertainty | gap | — | Only a settle window; no tolerance field. Open decision |
| Outcome | Escalation outcomes | covered | `action-graph#/nodes`, `recovery-request#/reason` | `escalation_ref` on a graph node |
