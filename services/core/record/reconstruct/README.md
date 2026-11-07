# record/reconstruct

**What it does.** Builds the reconstruction of one correlation from the Record alone and serves it at `GET /v1/reconstructions/{correlation_id}` (PERD §39.4). Task REC-03. Updated 2 Nov 2026.

**REQs.** REQ-AUD-003 (SoT US-12, POC-FR-12, INV-10, INV-11).

**How it works.**
- `Build(ctx, db, tenant, correlation)` opens one read-only `REPEATABLE READ` transaction as `record_read`. It reads the tenant's whole chain once through `record_read_chain_t` (migrations `0003` and `0004`), logged in as that tenant's `record_read` login, verifies it from genesis with `chain.VerifyRows`, then builds the body from those same rows. What is served is exactly what was verified.
- A chain that does not verify gives `ErrIntegrity`, and the handler answers 409 `RECORD_INTEGRITY_FAILURE` with no events (INV-11). A break anywhere in the tenant's chain refuses every correlation of that tenant. That is the safe reading of "the chain verification for that span".
- The body follows `contracts/schemas/reconstruction.v3.json`:
  - twelve sections, in the REQ-AUD-003 order;
  - every Record event of the correlation, in chain order;
  - one timeline line per event.

  `chain.head_event_id` and `chain.head_hash` name the correlation's last event. Its hash commits to everything before it, and it does not move when other correlations append, so the same Record always gives the same bytes. The body is canonical JSON (RFC 8785).
- **Projection** (`project.go`). A table maps each event type to a section, a basis (`EVIDENCE`, `RULE` or `PRINCIPAL_ACTION`) and the payload keys it may contribute as facts. A key that is not listed is never served. A fact outside its contract type or pattern makes the build fail with `ErrUnprojectable` (500 `INTERNAL_SAFETY_BLOCK`), never a partial body. Some event types are in the timeline only, because they carry no fact a section needs or because they are model output: `INTENT_PROPOSED`, and the lifecycle, control-plane, security and provider events. REC-04 fixes the payload schemas this table reads. Until then, this table is the one place that names them. A `DISPATCH_ATTEMPTED` with no `RECEIPT` of the same grant and attempt is an ACTION item in the state `DISPATCHED`, with the `capability_id` the dispatch recorded; with a receipt, the receipt is the item (its capability is its `logical_action`, a PackId).
- **ACTION** comes from `RECEIPT`: one receipt per dispatch attempt, as in the contract's `full_write_case` fixture and the runner's reader. `capability_id` is the receipt's `logical_action`. `grant_state` is derived from the Record: `VALID` only when the grant was issued in this correlation, spent once for this attempt, before `expires_at`, for this capability, and not voided first. Otherwise it is `MISSING`, `SPENT`, `EXPIRED` or `UNMATCHED`.
- **Handler.** The tenant comes from the authenticated caller, never from the path (INV-12). The default authoriser `DenyAll` refuses everything, so nothing reaches the Record until EDG-01 supplies the session check and wires the route into `cmd/core` (not done here). The `ops` and `audit` roles are served (REQ-AUD-003; Architect decision of 1 Nov 2026), and only to a staff principal: `operator`, `auditor`, `regulator` or `admin`. A customer or an approver is refused even if the session carries one of the roles, and so is any principal kind not on that list. **Masking:** every run of five or more digits in a served free-text or scalar fact keeps only its last four (`0123456789` is served as `******6789`). References, IDs and enum values are served as written, because their contract patterns constrain them and the reader matches on them. The `events` array is the hashed Record and is not altered, so its `actor` (`<kind>:<id>`, the session's pseudonymous `sub`) is served as stored. Error answers:

  | Status | Code | When |
  |---|---|---|
  | 401 | `UNAUTHENTICATED` | no authenticated session |
  | 404 | `NOT_FOUND_OR_NOT_AUTHORIZED` | the caller lacks the role, the correlation is unknown, or it belongs to another tenant (one code, so existence does not leak) |
  | 400 | `INPUT_INVALID` | the correlation ID is malformed |
  | 409 | `RECORD_INTEGRITY_FAILURE` | the chain did not verify |
  | 500 | `INTERNAL_SAFETY_BLOCK` | a verified object breaks the contract |
  | 503 | `DEPENDENCY_UNAVAILABLE` | the database is unavailable (retryable) |

  Every read writes a `RECORD_ACCESSED` line to the access log: IDs and the outcome only, never Record content.

**Tenant isolation: DEBT-04 option A (slice 2).** The database, not the handler, decides which tenant's chain a connection can read. `record_read_chain_t()` has no tenant argument; it finds the tenant with `session_user`, the login the connection authenticated as (a `tenant_login` row maps each login to one tenant). The handler gets a pool per tenant from `TenantDBs.For(tenant)` and never falls back to another tenant's pool. `TestRecordReadChainDeniesAnotherTenantAtTheDatabase` shows two logins each read only their own chain; the old function that took a tenant is dropped (migration `0005`). What remains before the endpoint is wired into `cmd/core`: tenant provisioning (the logins and their mapping rows, slice 3) and EDG-01's ops and audit role check. A test in `cmd/core` fails if the process imports this package before then.

**Not built here.** The route in `cmd/core` (EDG-01); decision replay (REQ-EXP-004); the payload schemas (REC-04). Reading the whole tenant chain per request is simple and measured inside the budget: P95 38.5 ms for a 200-event case in a 400-event chain, on PostgreSQL 17.11. A long-lived tenant chain will need a verified-head checkpoint (REC-05).

**Tests.** `go test ./record/reconstruct/...` from `services/core/`. They need PostgreSQL server binaries or `NOEMA_TEST_DATABASE_URL`.
- US-01 and US-06 are appended through the real appender as the tenant's `execution_rw` login and reconstructed as its `record_read` login.
- The output is reproducible and matches the committed golden files in `testdata/`.
- Seven kinds of tampering are each refused, including a break in another correlation of the tenant.
- Tenant isolation; only `record_read` can reconstruct.
- A receipt whose capability is not a pack ID is refused.
- The grant-state derivation; facts outside the contract are refused; the enum sets equal `enums.v1.json`.
- The handler's refusals and codes; the P95 budget.

`tools/contracts/test_reconstruction_golden.py` validates the golden files against `reconstruction.v3` and recomputes every event hash in Python. Regenerate the golden files with `go test ./record/reconstruct/ -run US0 -update`, and only when a change to the body is intended.
