# Status · CON-01 (Session A) · contracts

Updated 2026-10-27.

## State
- Merged: schemas C-1 to C-6 with valid and invalid fixtures, the validator and `contracts/INVENTORY.md`; the candidates read contract (#42); the reconstruction response contract (#50, REQ-AUD-003); `PROVIDER_FAILOVER` carries its reason (#52).
- `INPUT_INVALID` and `TENANT_SCOPE_MISMATCH` are in `contracts/schemas/errors.v1.json`; `SCOPE_VIOLATION` is not. PERD v1.15 §39.3 now names `TENANT_SCOPE_MISMATCH`.

## Open
- Record event v2 (this pull request, branch `sa/record-event-v2`): `record-event.v2.json`, `reconstruction.v2.json`, nine grant-void reasons, `tools/record_chain_ref.py`; needs both founders (CODEOWNERS) and the docs decision record DEC-2026-10-27-record-event-v2 (a companion docs pull request, merged first or together).
- Also in this pull request (founder instruction of 27 Oct 2026, answer 3; message in `status/ENV-03.md`): `errors.v2.json` adds the code `DUPLICATE_CREATE` (non-retryable); the case adapter's transport class of the same name is classified `REJECTED_BY_TARGET` (PERD §27.1 E10, "4xx validation/business rejection"). No execution state is added, so the SoT and the state registry are unchanged. Decision record: D6 in `DEC-2026-10-27-record-event-v2`.
- Stopped, PERD silent: the format of `actor` and the `payload_hash` of an event that refers to no object. The PERD §32.1 says only "`payload_hash` (the hash of the referenced object's canonical JSON)" and "`actor` (the power or service identity, plus the principal where relevant)". D4 of the decision record keeps the two as defaults to confirm; nothing is changed until the founders choose a form.
- Record event v3 (27 Oct 2026, branch `sa/record-event-v3`): `record-event.v3.json`, `reconstruction.v3.json`, the emitter and principal of PERD v1.17 §32.1, `payload_hash` as SHA-256 of the RFC 8785 form (integers only); writer, verifier, ledger, runner reader, fixtures, golden vectors (the `payload_hash_vectors` from an independent Node implementation) and tests together. v2 untouched. Decisions: `DEC-2026-10-27-actor-and-payload-hash`. Needs both founders (CODEOWNERS).
- Open overlap: `contracts/INVENTORY.md` is changed by both #57 (Session B) and #59 (Session A).
- Non-author founder review on #59.

## Messages

### 2026-10-21 17:04Z sent · Session A → Session C
- Purpose: answers to "Needs session A" items 1 and 5 on noema#49 (reconstruction response format).
- Receiver used it for: not recorded.
- Evidence: noema#50.

### 2026-10-21 17:07Z sent · Session A → Session C
- Purpose: reply to three gaps on noema#50 (ACTION items require `facts.grant_state`).
- Receiver used it for: not recorded.
- Evidence: noema#50.

### 2026-10-27 (time not recorded) accepted · Founder → Session A
- Purpose: three things in order, nothing merged by me. (1) PR 1, contract: add a new versioned record-event schema beside v1, aligned with the PERD §32.1 event (`schema_version`, `tenant_id`, `seq`, `payload_hash`, `actor`, tenant-scoped genesis hash), update writers, readers, verifiers, models, fixtures, golden vectors and tests together, add six grant-void reasons (SIGNATURE_INVALID, EXPIRED, BINDING_MISMATCH, WRITES_DISABLED, REVALIDATION_UNAVAILABLE, REJECTED_STALE_VERSION) with a fixture and a test each and a test that an unknown reason is rejected, document it in the contract README and the decision record, no migration tooling; (2) PR 2, create-if-absent in mock-case, after PR 1 is open; (3) merge main into #77 after PR 1 merges and keep it open.
- Used it for: PR 1 (this pull request and the docs decision record). PR 2 follows. Step 3 cannot be done as written: #77 was already merged on 27 Oct 2026 13:14 UTC (by noemai-systems) when this instruction arrived; reported to the Founder.
- Evidence: this pull request; Noemai-org/docs decision record DEC-2026-10-27-record-event-v2; noema#77 (merged).
