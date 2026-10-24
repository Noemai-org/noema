# Status · CON-01 (Session A) · contracts

Updated 2026-10-24.

## State
- Merged: schemas C-1 to C-6 with valid and invalid fixtures, the validator and `contracts/INVENTORY.md`; the candidates read contract (#42); the reconstruction response contract (#50, REQ-AUD-003); `PROVIDER_FAILOVER` carries its reason (#52).
- `INPUT_INVALID` and `TENANT_SCOPE_MISMATCH` are in `contracts/schemas/errors.v1.json`; `SCOPE_VIOLATION` is not. PERD v1.15 §39.3 now names `TENANT_SCOPE_MISMATCH`.

## Open
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
