<!-- Copy of Noemai-org/docs docs/engineering/state-registry.md at docs commit 7d5b2fabdc4d81d09a5a12a49e186d4fdc8de635. The docs original wins on any difference. Re-point every copy with: python3 tools/vendor/repoint_docs.py --docs <docs checkout> -->

# State registry

> **Derived, with one normative exception.** This page puts every state, outcome and status value used in [`NOEMA_SOURCE_OF_TRUTH.md`](../NOEMA_SOURCE_OF_TRUTH.md) (the SoT) in one lookup table, with the section that defines it.
>
> - It adds no values. Every value is spelled exactly as in the SoT.
> - If a value set is shown in the SoT only as an example, the **Enumerated?** column says so. Such a set must not be completed by guesswork; the open entry is in [`open-decisions.md`](../open-decisions.md).
> - If this page and the SoT disagree, the SoT wins.
> - **This file is the single normative home of core value lists; CI compares `/contracts` against it (DEC-2026-10-16-money-and-value-lists §2.4).** That applies to the rows marked "Complete (normative)" in §2 and §6 and to the money row in §6. Every other row stays derived.

**The four powers own their own vocabulary.** No power writes another power's states:

- Understanding produces proposals.
- Trust produces decisions.
- Execution produces receipts and never writes `SUCCEEDED`.
- Outcome produces verdicts.

(SoT §13, §43.3, INV-04, AGENT-22.)

## 1. Status tags for claims in the SoT

Defined in SoT §0.2. Every claim in Noema documentation carries its tag exactly as given there.

| Tag | Meaning (SoT §0.2) |
|---|---|
| [CONVICTION] | Settled. Not reopened without new evidence and a version change. |
| [DECISION] | Chosen default. Can change through the open-decisions process in §46. |
| [OPEN] | Not decided. A recommended default is given with a deadline. |
| [HYPOTHESIS] | Something the work exists to test. Not something we already believe is true. |
| [TARGET] | A number we commit to reaching. Not a measured result. |
| [ESTIMATE] | Our own calculation. The method and assumptions are shown next to it. |
| [ILLUSTRATIVE] | An example to make a concept concrete. Not a plan to build it now. |

External facts carry a source marker such as [S4] (Appendix C). The non-canonical marker NON-CANONICAL used in this repository applies only to provisional migration items (OD-P-xx). It is not a SoT status tag.

## 2. Trust: the Decision Maker outcome

| Object · field | Values | Enumerated? | Source | Rules |
|---|---|---|---|---|
| Decision Record · `outcome` | `ALLOW`, `CLARIFY`, `CONFIRM`, `WAIT`, `ESCALATE`, `REJECT`, `UNKNOWN` | Complete (exactly one per decision) | §13.1, §14 (Decision Maker), §18.1, PLAT-FR-05, POC-FR-06 | A Decision Grant is issued **only** on `ALLOW` (§13.2, PLAT-FR-07). Fail-closed: a dependency failure yields `ESCALATE` or `UNKNOWN`, never `ALLOW` (INV-16, PLAT-NFR-03). An unresolved material ambiguity makes anything other than `CLARIFY` or `ESCALATE` unrepresentable (INV-06). |
| Decision Record · `authority` | three layers: `technical`, `policy`, `business`; each result ∈ `PASS`, `FAIL`, `NOT_EVALUATED` | Complete (normative) | §14 (Authority Engine), §43.2, App. A; value list: [DEC-2026-10-16-money-and-value-lists](../decisions/DEC-2026-10-16-money-and-value-lists.md) §2.2 | Closes [OD-P-08](../open-decisions.md#od-p-08). `OVERRIDDEN` was removed: nothing produces it, and nobody can override the business layer. An approver's ACCEPT triggers a fresh evaluation (§43.2a, INV-25). |
| Decision Record · `reason_codes` | **Core codes** (closed enum, 17): `PROHIBITED_CAPABILITY`, `AMBIGUOUS_COUNTERPARTY`, `DECLINED_BY_APPROVER`, `POLICY_VERSION_MISMATCH`, `INSUFFICIENT_EVIDENCE`, `STALE_EVIDENCE`, `INTENT_INVALID`, `UNAUTHENTICATED`, `WRITES_DISABLED`, `OUT_OF_SCOPE`, `NOT_AUTHORIZED`, `NOT_FOUND_OR_NOT_AUTHORIZED`, `PLAN_INVALID`, `NO_PERMITTING_RULE`, `RATE_LIMITED`, `INTERNAL_ERROR`, `MODE_NOT_ENABLED`. **Pack codes**: `<vertical>.reason.<name>` from the pack's reason catalogue | Complete (normative) | §43.2, §43.2a, US-10; value list: [DEC-2026-10-16-money-and-value-lists](../decisions/DEC-2026-10-16-money-and-value-lists.md) §2.3 | Closes [OD-P-08](../open-decisions.md#od-p-08). Producers are in PERD §23.3. The array may hold both tiers. `packcheck` rejects a pack code outside its namespace or one that reuses a core code. Not reason codes: `NONE` (an empty array means no reason), grant-void reasons, classifications and execution states, which keep their own fields. The last three core codes are [REC] until the founders confirm them. |
| Decision Grant · `single_use` | `true` | Fixed | §43.2 | A grant binds principal, target, intent hash, one capability, parameter hash, evidence-set hash, `plan_hash` and `plan_step`, `target_versions`, policy and pack versions, expiry and nonce. Any mismatch voids it (INV-03, INV-17). A new policy version voids every unspent grant governed by an earlier version (INV-26). |
| Decision Record · `evidence_freshness[].limit_set_by` | `SOURCE`, `TRUST_PROFILE`, `PROFILE_OVERRIDE` | Complete | §43.2 | The effective limit is the stricter of the source's `freshness_sla_seconds` and the trust profile's `max_evidence_age_seconds`, measured from `read_at` (§15.1, §43.5, INV-23). |
| Policy rule template · `evaluation_time` | `AT_DECISION` (default), `AT_ADMISSION` | Complete | §15.3 | Set by the policy author; Trust never infers it and never decides that one version is stricter than another (INV-09). |
| Confirmation request · `approver_requirement` | `SELF`, `ROLE:<role>`, `MAKER_CHECKER` | Complete | §43.2a | A `CONFIRM` never authorises anything by itself (INV-25). |
| Confirmation event response | `ACCEPT`, `DECLINE` | Complete | §43.2a | `ACCEPT` leads to a fresh Trust evaluation; a grant follows only if the binding hash still matches and the result is `ALLOW`. `DECLINE` closes the decision as `REJECT` with `DECLINED_BY_APPROVER`. |
| Confirmation request · terminal state on expiry | `EXPIRED` | Single value | §43.2a | No grant can be issued from an expired request. |

Legacy note: the READY / WAITING / BLOCKED / CLARIFICATION_REQUIRED trust state machine in the archive is **superseded** by this table.

## 3. Evidence: FactStatus

| Object · field | Values | Enumerated? | Source | Rules |
|---|---|---|---|---|
| Evidence item · `fact_status` | `KNOWN`, `INFERRED`, `AMBIGUOUS`, `UNAVAILABLE`, `STALE`, `CONTRADICTORY`, `UNKNOWN`, `UNAUTHORIZED` | Complete | §14 (State Reconciliation), §43.5, App. A | A missing record is `UNAVAILABLE` or `UNKNOWN`, never a negative fact (INV-19). User-facing text states a fact as certain only when it is `KNOWN` (INV-13, AGENT-17). Both `read_at` and `effective_at` are always kept (§43.5). |
| Evidence item · `authority_class` | e.g. `AUTHORITATIVE_FOR:case.status` | Example only | §43.5 | Declared per evidence source in the pack (§15.5 item 5). |

## 4. Execution: the Receipt

| Object · field | Values | Enumerated? | Source | Rules |
|---|---|---|---|---|
| Execution Receipt · `execution_state` | `DISPATCHED`, `ACKNOWLEDGED`, `OUTCOME_UNKNOWN`, `REJECTED_BY_TARGET`, `REJECTED_STALE_VERSION`, `FAILED_TERMINAL` | Complete | §43.3 | Execution never writes `SUCCEEDED`; that word belongs to Outcome (§43.3). `REJECTED_STALE_VERSION` voids the grant, returns the case to Trust and is never retried automatically (INV-27). Timeout or silence is `OUTCOME_UNKNOWN` (INV-07, AGENT-07). A write is retried only under a new grant (AGENT-08, INV-15). |
| Execution Receipt · `transport_result` | e.g. `TIMEOUT` | Example only | §43.3 | No value set is defined. Do not infer one. |
| Execution Receipt · `attempt` | integer | n/a | §35.5, §43.3 | A retry increments `attempt` and keeps the logical idempotency key. A new logical resolution epoch gives a new key (§35.5). |
| Capability descriptor · `retry_safety` | `NATIVE_IDEMPOTENT`, `QUERYABLE_BY_KEY`, `NONE` | Complete | §43.8 | `NONE` is never retried automatically; an uncertain result is escalated (INV-24). No duplicate effect is promised except where the target lets Noema prove it (§13.1). |
| Capability descriptor · `concurrency_control` | `CONDITIONAL_WRITE`, `NONE` | Complete | §43.8 | With `CONDITIONAL_WRITE`, every write passes the grant's version tokens; a mismatch aborts the write (INV-27). |
| Capability descriptor · `consequence_profile` dimensions (financial, customer, regulatory, access, operational) | `NONE`, `LOW`, `HIGH` | Complete | §43.8 | `risk_class` may never be lower than the risk implied by the profile. |
| Capability descriptor · `consequence_profile.irreversibility` | `REVERSIBLE`, `COMPENSABLE`, `IRREVERSIBLE` | Complete | §43.8 | A capability without a complete consequence profile cannot be registered (AGENT-06). |

## 5. Outcome: the Verdict

| Object · field | Values | Enumerated? | Source | Rules |
|---|---|---|---|---|
| Outcome Verdict · `verdict` | `ACHIEVED`, `NOT_ACHIEVED`, `PARTIALLY_ACHIEVED`, `PENDING_EXTERNAL`, `UNKNOWN`, `COMPENSATED` | Complete | §43.4, App. A | Only Outcome marks `ACHIEVED`, from evidence it read itself after the settle window (INV-04). Anything except `ACHIEVED` creates a `RecoveryRequest` to **Trust**, never straight back to Execution (§13.2, §43.4, INV-15). "Done" may be said to a person only on `ACHIEVED` (§18.4 rule 6). |
| Outcome Verdict · `watch` | `watch_id`, `next_check_at`, `deadline_at`, `triggers` | Fields | §43.4 | Present only on `PENDING_EXTERNAL`; stored durably. At the deadline the verdict becomes `UNKNOWN` and a RecoveryRequest goes to Trust (INV-28). Changes are reported by `outcome.updated` (§43.4a). |
| Outcome definition · `intermediate_states`, `terminal_states` | pack data, e.g. `CASE_SUBMITTED`; `ACHIEVED`, `NOT_ACHIEVED` | Pack-defined | §15.3 | Defined per outcome definition in the Outcome Taxonomy. Never in core. |

## 6. Understanding: the Intent Object

| Object · field | Values | Enumerated? | Source | Rules |
|---|---|---|---|---|
| Intent Object · `status` | `EXTRACTED`, `VALIDATED`, `REJECTED_AT_GATE` | Complete (normative) | §43.1; value list: [DEC-2026-10-16-money-and-value-lists](../decisions/DEC-2026-10-16-money-and-value-lists.md) §2.1 | Closes [OD-P-06](../open-decisions.md#od-p-06). Lifecycle: `EXTRACTED` → `VALIDATED` or `REJECTED_AT_GATE`, set once at the gate and never changed afterwards; only `VALIDATED` crosses into Trust. `SUPERSEDED` was removed: the Intent Object is immutable once admitted (§43.1), so supersession is derived from the `supersedes` link, and Trust T1 rejects a non-latest version with `INTENT_INVALID`. |
| Currency amount · any money field | an integer count of the currency's minor unit, `0 ≤ amount ≤ 9007199254740991`, with an ISO 4217 `currency`; exponent from `contracts/currencies.json` | Complete (normative) | §43.1; [DEC-2026-10-16-money-and-value-lists](../decisions/DEC-2026-10-16-money-and-value-lists.md) §1 (M1 to M4) | No `minor_units` flag. `Quantity` keeps `value` and `unit`; every other money field is `<name>_minor`. No floating point at any layer (JSON Schema `integer`, digit-only wire literals, Go `int64`, Python `StrictInt`, Postgres `BIGINT` with a range check). Direction is a separate field, never a sign. |
| Intent Object · slot `status` | e.g. `UNRESOLVED` | Example only | §43.1 | Not enumerated. |
| Intent Object · `ambiguity_flags` | e.g. `COUNTERPARTY_MULTIPLE_MATCH` | Example only | §43.1 | Every material ambiguity is flagged (PLAT-FR-02). |
| Intent Object · `source.mode` | e.g. `MODE_1` | Example only | §43.1 | The modes are Mode 1 and Mode 2 (§16). The POC builds Mode 1 only (POC-FR-16). |

## 7. Classifications the engine must never collapse (POC)

From SoT §35.5, [CONVICTION]. These are classifications of a situation, each with required behaviour. They are not the same thing as the field values above.

| Classification | When | Required behaviour |
|---|---|---|
| `EXECUTION_OUTCOME_UNKNOWN` | Ledger debit posted, switch timed out, destination unconfirmed | Verify through approved reads; never assume success or failure; never retry blindly. |
| `REVERSED_CONFIRMED` | Original debit and its reversal both confirmed | Explain the verified reversal; start no further action. |
| `CONTRADICTORY_STATE` | Ledger debit, switch says failed, destination shows a credit | Freeze consequential writes on the target (INV-20); gather evidence; escalate. |
| `OUTCOME_UNKNOWN` | A case write may have committed but the response was lost | Query by the stable idempotency key; never create a duplicate case. |
| `STALE_EVIDENCE` | Evidence older than its effective freshness limit (the stricter of the source's and the trust profile's) | Refresh or send to review; no consequential action on stale evidence. |
| `AMBIGUOUS_ENTITY` | Two or more transactions match | Targeted clarification; never guess. |
| `POLICY_STALE` | Policy changed after planning | Grant voided (INV-26); re-evaluate under the version in force, honouring AT_ADMISSION rules; the Record shows both versions (INV-09). |
| `POSTCONDITION_UNVERIFIED` | A write returned success but the postcondition is absent | Do not report success; recover or escalate. |
| `EVENT_ORDER_UNCERTAIN` | Late or out-of-order event, until reconciled | Keep event time and ingestion time; recompute deterministically. |

Do not confuse `OUTCOME_UNKNOWN` with `EXECUTION_OUTCOME_UNKNOWN`: see [OD-P-10](../open-decisions.md#od-p-10).

## 8. Recovery types

From SoT §35.5. "Compensation is not rollback." These five are kept distinct and never confused:

1. retrying a read;
2. verifying an uncertain write;
3. retrying the same idempotent logical write under a new grant;
4. compensating with a new, separately authorised action;
5. escalating because compensation is unsafe.

The POC implements no monetary compensation. All recovery goes back through Trust (§13.2).

## 9. Synthetic environment domain lifecycles (POC only, fintech)

From SoT §35.5, taken from the Tushe specification ([OD-P-03](../open-decisions.md#od-p-03)). These are **states of the synthetic enterprise's records**. They are not Noema core states. They belong to the fintech pack and the synthetic adapters, never to `/core` (INV-14, AGENT-04).

| Lifecycle | States |
|---|---|
| Transaction | CREATED, INITIATED, SUBMITTED, PROCESSING, SUCCESSFUL, FAILED, REJECTED, TIMED_OUT, EXECUTION_OUTCOME_UNKNOWN, DUPLICATE, PARTIAL |
| Settlement | NOT_REQUIRED, PENDING, SETTLED, FAILED, REVERSED, UNKNOWN |
| Beneficiary credit | NOT_APPLICABLE, PENDING, CREDITED, NOT_CREDITED, UNKNOWN |
| Case | NOT_OPEN, OPEN, INVESTIGATING, AWAITING_EXTERNAL_RESPONSE, ACTION_REQUIRED, ESCALATED, RESOLVED, CLOSED |
| Dispute | NONE, OPEN, UNDER_REVIEW, UPHELD, REJECTED, RESOLVED |

Every transition carries provenance, timestamp, correlation ID, actor or source, policy version where relevant, and supporting evidence (§35.5). The Tushe specification (§5, STM-01 to STM-03, in [`../poc/tushe/`](../poc/tushe/README.md)) requires Tushe to document the legal transitions and invariants; that documentation is a Tushe deliverable and is not yet in this repository.

## 10. Classes and levels

| Set | Values | Enumerated? | Source | Notes |
|---|---|---|---|---|
| Capability risk class (POC registry) | `R0` read, `R0` internal, `R1` append-only write, `R2` non-monetary write, `R3` regulatory write, `PROHIBITED` | Used in the POC registry. No canonical ladder. | §34.3 | `R3` is used for `LOG_COMPLAINT_REFERENCE` since v1.2 and in an [ILLUSTRATIVE] telecom row (§15.4). Ownership: [OD-P-09](../open-decisions.md#od-p-09). `PROHIBITED` capabilities always reject and have no adapter (§34.2, INV-05). |
| Finding severity | `S0`, `S1`, `S2`, `S3` | Complete | §38.3 | Zero S0 anywhere; zero S1 in the release suite. |
| Data sensitivity | e.g. `S3_financial_pii`, data class `S4` | Examples only | §15.3, §15.4 | Not the severity classes: [OD-P-10](../open-decisions.md#od-p-10). |
| Deployment mode | Mode 1, Mode 2 | Complete | §16 | Mode 2 is built in Phase 4 (§16, §41). |
| Benchmark families | B1–B8 | Complete | §20.2 | |
