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
- **Projection** (`project.go`). A table maps each event type to a section, a basis (`EVIDENCE`, `RULE` or `PRINCIPAL_ACTION`) and the payload keys it may contribute as facts. A key that is not listed is never served. A fact outside its contract type or pattern makes the build fail with `ErrUnprojectable` (500 `INTERNAL_SAFETY_BLOCK`), never a partial body. Some event types are in the timeline only, because they carry no fact a section needs or because they are model output: `INTENT_PROPOSED`, and the lifecycle, control-plane, security and provider events. REC-04 fixes the payload schemas this table reads. Until then, this table is the one place that names them. A `DISPATCH_ATTEMPTED` with no `RECEIPT` of the same grant and attempt is an ACTION item in the state `DISPATCHED`; with a receipt, the receipt is the item. The capability of an action is the one the dispatch recorded (`capability_id`), not `logical_action`.
- **ACTION** comes from `RECEIPT`: one receipt per dispatch attempt, as in the contract's `full_write_case` fixture and the runner's reader. `capability_id` is the capability of the dispatch: the `capability` key of the `DISPATCH_ATTEMPTED` event of that grant and attempt (ruling A117; the execution-limits budget key, so the event carries one name for it), with the receipt's `logical_action` only as the fallback for a Record written before the dispatch carried it. `grant_state` is derived from the Record: `VALID` only when the grant was issued in this correlation, spent once for this attempt, before `expires_at`, for this capability, and not voided first. Otherwise it is `MISSING`, `SPENT`, `EXPIRED` or `UNMATCHED`.
- **Handler.** The tenant comes from the authenticated caller, never from the path (INV-12). The default authoriser `DenyAll` refuses everything, so nothing reaches the Record until the composition root mounts it with `SessionAuthorizer` (slice 2, below) behind the session check and the role gate. `cmd/core` mounts the route (`compose.go`, `apiRoutes`) for ops and audit with `SessionAuthorizer` and the access logger, only when a per-tenant Record reader is configured (`Components.Records`; none is set in `main` yet, OD-P-82), so the route stays unreachable until then; a Record reader without an access logger is a startup error (REQ-AUD-003). The `ops` and `audit` roles are served (REQ-AUD-003; Architect decision of 1 Nov 2026), and only to a staff principal: `operator`, `auditor`, `regulator` or `admin`. A customer or an approver is refused even if the session carries one of the roles, and so is any principal kind not on that list. **Masking:** every run of five or more digits in a served free-text or scalar fact keeps only its last four (`0123456789` is served as `******6789`). References, IDs and enum values are served as written, because their contract patterns constrain them and the reader matches on them. The `events` array is the hashed Record and is not altered, so its `actor` (`<kind>:<id>`, the session's pseudonymous `sub`) is served as stored. Error answers:

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

**Slice 2, the session Authorizer (11 Nov 2026).** `SessionAuthorizer` builds the `Caller` from the principal that `session.Middleware` put in the request context and from nothing else: the tenant is the token's, the role is the token's one role, and the principal is `<kind>:<sub>` with the kind taken from the role (`ops` is `operator`, `audit` is `auditor`; any other role keeps its own name, which is not a staff kind, so the handler answers 404). A request with no session principal is `ErrUnauthenticated`. Mount it as `session.Middleware`, then `session.RequireRole(ops, audit)`, then the handler; the handler also refuses customers and approvers if the gate is missing. Tenant binding is also the database's: a pool for another tenant than the token's reads a chain whose genesis commits to that other tenant, so verification fails and the handler answers 409 `RECORD_INTEGRITY_FAILURE` with no events (tested; an on-call reader should know this 409 on the reconstruction route can mean a wired pool of the wrong tenant, not tampering: check the pool's login mapping first; `record_login_tenant` is not callable by `record_read`, so `session.CheckTenant` cannot be asked of the database from here).

**Subject length.** The edge accepts a `sub` of up to 128 bytes (ruling A1, item 6) and the handler's principal pattern allows an id of 64 characters (PERD 32.1), so a verified ops or audit session with a longer subject is answered 404. That is fail closed; neither limit is changed here (AGENT-16).

**Not built here.** The route in `cmd/core` (the composition root, EL); decision replay (REQ-EXP-004); the payload schemas (REC-04). Reading the whole tenant chain per request is simple and measured inside the budget: P95 38.5 ms for a 200-event case in a 400-event chain, on PostgreSQL 17.11. A long-lived tenant chain will need a verified-head checkpoint (REC-05).

**Tests.** `go test ./record/reconstruct/...` from `services/core/`. They need PostgreSQL server binaries or `NOEMA_TEST_DATABASE_URL`.
- US-01 and US-06 are appended through the real appender as the tenant's `execution_rw` login and reconstructed as its `record_read` login.
- The output is reproducible and matches the committed golden files in `testdata/`.
- Seven kinds of tampering are each refused, including a break in another correlation of the tenant.
- Tenant isolation; only `record_read` can reconstruct.
- A receipt whose capability is not a pack ID is refused.
- The grant-state derivation; facts outside the contract are refused; the enum sets equal `enums.v1.json`.
- The handler's refusals and codes; the P95 budget.

`tools/contracts/test_reconstruction_golden.py` validates the golden files against `reconstruction.v3` and recomputes every event hash in Python. Regenerate the golden files with `go test ./record/reconstruct/ -run US0 -update`, and only when a change to the body is intended.

**What a reconstruction proves (ruling A40).** It is *consistent, not authenticated*: the chain verifies from genesis (tamper-evident), but no signature on a Record event is checked yet, so it shows consistency, not who wrote each event. That changes only with DB-side verification of receipts (EXE-08 slice B) and of Record events (REC-08). `chain.verified: true` in the response means the hash chain verified. It means the chain is consistent, not that its writers are authenticated.

**What `grant_state` and `execution_state` do and do not say.** `grant_state` is derived from Record events alone. A dispatch with no receipt is projected as `execution_state: DISPATCHED` with `grant_state: VALID`: that says a grant was issued and spent in this correlation, not that the write happened or is fine. With no receipt the outcome is unknown (INV-07) and only a later receipt and an Outcome verdict settle it; read `VALID` as "the grant was in order", never as success. Until DB-side verification of Record events (REC-08, noema#263) is merged and the chain verifier checks signatures, a `GRANT_ISSUED`, `GRANT_SPENT` or `RECEIPT` event appended under a leaked runtime credential would be projected as genuine (DEBT-03); the A40 wording above is the reader's guard.
