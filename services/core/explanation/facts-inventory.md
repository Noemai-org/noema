# The FactsSource slot inventory (EXP-01; ruling A209, relayed, not yet committed)

Dated 14 Nov 2026. All data is SYNTHETIC. **This is a proposal for the Founder's A155 approval and for EL's real facts source (STM-01). It changes no pack, no code and no contract.** It lists which `{fact:<kind>}` slots the pack's fallback templates would hold, and where in the Record (PERD §32.2) each fact kind comes from. A row says what its basis is: **stated** (a PERD or user-story sentence names it) or **[REC]** (my reading, for EL to confirm). A fact kind the explanation contract does not name is never invented here: a row with no stated basis is marked, and the Founder may strike it.

## What is already decided (A155; `explanation.go`, `slots.go`)

- The slot syntax is `{fact:<kind>}`; `<kind>` is a fact kind of `explanation-facts.v2` (`^[a-z][a-z0-9_.]*$`). A slot is filled with the `value_masked` of the **one KNOWN fact** of that kind. Zero facts, two or more, one that is not KNOWN (INFERRED included), an empty value, or a value with a brace blocks the explanation (`ErrSlot`; INV-13).
- `value_masked` is at most 200 characters and holds no digit run over four (`explanation-facts.v2`, PERD §31.2 check 4).
- Facts come from KNOWN and INFERRED evidence and from verdicts; anything else is an unknown (`explanation-facts.v2`).
- The real source reads the Record and the Evidence Layer; the explanation package reads neither (`FactsSource`, A155).
- The pack's templates today are fixed sentences with **no slot** (35 entries: 5 outcomes × 7 classifications, `packs/fintech/templates/en-NG.json`). Adding a slot is the pack edit that needs one Founder approval (A155).

## The fact kinds

| Fact kind (`kind`) | Slot | Value shown (masked) | Basis | Record source | Used by |
|---|---|---|---|---|---|
| `payment.amount` | `{fact:payment.amount}` | amount and currency, grouped with commas (`NGN 20,000`), never a digit run over four | **stated:** the confirmation card shows "the masked target (amount, recipient, last four digits, time)" (PERD §13) | the sealed snapshot of the ledger read for the resolved transaction: `EVIDENCE_READ` and `CONTEXT_SNAPSHOT_SEALED` (the Evidence adapter seals it, noema#548); fact status is the snapshot item's status | `status_explained` |
| `account` | `{fact:account}` | the account's last four digits (`ending 4321`) | **stated:** US-01 "explanation cites the masked account and a KNOWN state" (PERD §11.2) | the same sealed ledger snapshot (`account_last4`); no other source | `status_explained` |
| `payment.time` | `{fact:payment.time}` | the transfer's time in local words (`10:42 on 12 Nov`) | **stated:** the same confirmation-card sentence (PERD §13) | the same sealed ledger snapshot (`occurred_at`) | `status_explained` |
| `payment.counterparty` | `{fact:payment.counterparty}` | the recipient's display name as the bank shows it, or its initial and last four | **stated:** the same sentence ("recipient"); **[REC]** the name of the kind | the same sealed ledger snapshot (counterparty display) | `status_explained` |
| `case.ref` | `{fact:case.ref}` | the case reference, masked as `actions[].case_ref_masked` is | **stated:** "a case reference, if one exists" (PERD §13 answer shape, item 6) and "Case creation verified: the case reference" (§13 table) | the `RECEIPT` of the create-case call, and the `VERDICT` whose result is ACHIEVED (US-06: "verdict ACHIEVED from a read by idempotency key"); a case with no ACHIEVED verdict yields **no** `case.ref` fact (the word "opened" needs the verdict, PERD §13 "Words reserved for verified states") | `case_opened`, `case_routed`, `complaint_logged`, `evidence_recorded` |
| `case.team` | `{fact:case.team}` | the owning team's display name | **stated:** "the case reference, the owning team and the clock (as configured)" (PERD §13 table); **[REC]** the kind name | the routing decision: `DECISION_MADE` and `PLAN_CREATED` hold the route; the display name is pack data (routing allowlist), never a model value | `case_routed` |
| `case.clock` | `{fact:case.clock}` | the committed time, from the configured SLA clock, in words | **stated:** "the clock (as configured)" (PERD §13); a time commitment is template-bound (§31.2) | `SLA_EVALUATED` (the clock the pack configures); never a literal in a template | `case_routed`, `case_opened` |

**What is not a slot.** The money state (debited, credited, reversed, pending, unknown) and the "do not resend" advice are **template-bound statements**, chosen by `outcome_id` and `classification` (templates are keyed by them), not filled from a fact (PERD §31.2; US-04's `fin.expl.do_not_resend.v1` is a template id, **[REC]**). The words "done", "resolved", "reversed" and "failed" stay with their verified fact or verdict (PERD §13, "Words reserved for verified states"); this inventory adds no slot for them.

## Which template groups would hold which slots

| Outcome (`outcome_id`) | Classifications (all 7) | Slots the sentence may hold |
|---|---|---|
| `fin.outcome.exception.status_explained` | every one | `payment.amount`, `account`, `payment.time`; `payment.counterparty` only where the pack text names the recipient |
| `fin.outcome.exception.case_opened` | every one | `case.ref`; `case.clock` |
| `fin.outcome.exception.case_routed` | every one | `case.ref`, `case.team`, `case.clock` |
| `fin.outcome.exception.complaint_logged` | every one | `case.ref` |
| `fin.outcome.exception.evidence_recorded` | every one | `case.ref` |

A template that holds a slot is blocked, never guessed, when the fact is missing: the deterministic fallback then has no slot-free sentence to fall back to, so each template that gains a slot needs a **slot-free sentence beside it** in the pack, or the explanation for that class stays blocked (`ErrSlot`, a blocked explanation, never a looser prompt: PERD §31.2). That choice is the Founder's (A155 approval) and the Architect's; I did not make it.

## Open points for EL (the real source) and the Founder

1. **Confirm or correct every Record source above.** The sources marked **[REC]** are my reading of PERD §32.2; I found no sentence that names the event for `payment.counterparty`, `case.team` or `case.clock`. EL builds the real source (STM-01) and knows what `portread` and the sealed snapshot actually carry.
2. **Which fact statuses count.** A slot takes the one KNOWN fact; an INFERRED fact (it names a rule) blocks it today (A155). If any slot above should take an INFERRED fact, that is a ruling.
3. **Masking.** `account` and `payment.counterparty` values must pass the masking definition of `record/payloadguard` (limit 4, A194) before they are put in a fact; the source masks, the template never does.
4. **The pack edit** (adding slots to the 35 `en-NG` templates, then `pcm`, `yo`, `ha`, `ig`) is not in this PR. It needs the Founder's A155 approval and a native-speaker review of every changed sentence (REQ-EXP-002 dependency).

Sources read: PERD v1.23 §13, §31.2, §32.2, §11.2 (US-01, US-04, US-06); `contracts/schemas/explanation-facts.v2.json`; `services/core/explanation/{explainer.go,slots.go}`; `packs/fintech/templates/en-NG.json`.
