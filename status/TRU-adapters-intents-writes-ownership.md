# TRU-adapters-intents-writes-ownership · the production adapters of three `trust/intake` ports

State: local branch `prep/adapters-intents-writes-ownership` (no push, no PR). Built by Core Engineer 1 on the order of 18 Nov 2026 (rulings A329 to A340 relayed, not committed; A313 and A318/A319 committed in docs). Tier 1 (Trust): unreviewed helper work. Two of the three ports are only partly built, on purpose: see "Not built and why".

# Session 2026-11-18 · TRU-adapters-intents-writes-ownership (C1)

## Session summary
- What changed (PR links): none (local commits only). Package `orchestrator/portadapters/trust` (`trustport`, EL's, noema#710), new files `intents.go`, `killswitch.go` (and tests `world_test.go`, `intents_test.go`, `killswitch_test.go`); `understanding/intentstore/verify.go` exports `Verify`.
- Why: `Intents`, `Writes` and `Ownership` of `trust/intake` have only fakes (`ports.go`, `ports_t7_t9.go`); `cmd/core` cannot compose Trust without them (STM-01 pre-work: "the pgx implementations of `Intents`, ..."). Placement follows #710: consumer-defined interfaces, no new dependency, no edit of `cmd/core` (A313: `wire_trust.go` is EL's).

### Built
- `trustport.StoredIntents` (`NewStoredIntents(pools, pack, timeout)`) implements `intake.Intents` and also this package's `Intents`:
  - `Get`: reads `admitted_intent` as the session's tenant (`WHERE tenant_id = $1 ...`, on the tenant's own `trust_rw` login) and builds the `AdmittedIntent` from the stored document and the loaded pack: `PrincipalID` is the document's (the gate wrote the session's, INV-12); `View` is the version, the ambiguity flags and the status of every required slot of the node; `TargetRef` is the one `resolved_ref` of the node's `principal_must_own` slots.
  - `LatestVersion`: newest stored version of the tenant's intent.
  - `Verify`: re-reads the row, makes the canonical form of the jsonb text, runs `intentstore.Verify` (the check `Save` makes: canonical form, sha-256 equals the stored hash, row identity, version, supersedes, correlation, node and `status = VALIDATED` equal the document's), decodes the document as `intent-object.v3` (schema), and holds the carried `AdmittedIntent` to the stored one (hash, node, correlation, principal, target, view). Not the same: `ErrInvalid`. No such row: `ErrInvalid`.
  - Answers: another tenant's intent and an absent one are the same `intake.ErrNotFound`; a store failure is `ErrStore` (never `ErrNotFound`/`ErrInvalid`); an expired or cancelled context is that error, wrapped with `ErrStore`; a tenant with no login is `ErrNoPool`, never another tenant's login; a stored intent the loaded pack does not describe (another pack version, unknown node, two owned targets, a resolved owned slot with no reference) is `ErrStoredIntent` (T1 then ends UNKNOWN, no guessed view).
- `trustport.KillSwitch` (`NewKillSwitch(pools, packRef, timeout)`): `Enabled(ctx, tenantID)` reads `SELECT kill_switch FROM tenant WHERE tenant_id = $1` as the tenant's `trust_rw` login (the same read `trust/recorder` makes). True only for a visible row with the switch off. Not readable or not visible: `false` with `ErrStore` or `ErrNoTenant`. "The pack loaded" is a condition of the constructor (a well-formed `<id>@<x.y.z>`).
- `trustport.Querier`, `trustport.TenantPools`, `trustport.PoolsOf(map[string]*pgxpool.Pool)`: the consumer-defined seam to the per-tenant login directory of `cmd/core` (`loginPools`, `EnvTrustDSNDir`, A228). `*pgxpool.Pool` satisfies `Querier`.
- `intentstore.Verify(Admitted) error`: the existing `verify`, exported (one line plus doc). No second copy of the hash or binding rules exists.

### What `cmd/core` (EL) must pass
- `pools`: `trustport.PoolsOf(m)` over the same one-`trust_rw`-login-per-tenant map `buildPayloadReader` builds (a tenant file per login). Never an administrator's or an orchestrator's login.
- `pack`: the bytes of the pack file the process loaded (the same bytes `understanding.NewPackView` and `interpreterport.RequiredFromPack` get).
- `packRef`: `<pack id>@<version>` of that pack. `timeout`: from configuration (AGENT-16); it applies on top of the caller's context.
- Plug `StoredIntents` as `intake.Ports.Intents`. `KillSwitch` is half of `intake.Writes` only (below).

## Not built and why
1. **`Writes.NeedsWrite`: no rule exists; stopped (Tier 1, do not guess).** T3 uses it to decide read or write, and `late.needsWrite` also decides the Planner's `WriteRequired` and the plan's shape (`wellFormed`: a write has at least one step, a read none). Nothing in the pack says which intents lead to a write: `fin.payments.exception.investigate` is one node whose `question_type` (STATUS, ..., ESCALATION_REQUEST) decides read or write, and no pack field classifies those values. The candidates all break something: the node's trust profile risk class (`R2_non_monetary_write`) would make every status question a write whose plan needs a step; the `requested_remedy` slot (`Capabilities.Requested`) would make an escalation request a read; the graph `applies_when` (`g2_escalate`: `subject_resource.status == 'RESOLVED'`) is true for every resolved intent. Core may not name `ESCALATION_REQUEST` (INV-14). Needed: a pack field (or an Architect ruling) mapping intent node and slot values to "leads to a write", read by this adapter as data. Until then `KillSwitch` is delivered alone; EL composes `intake.Writes` from `KillSwitch.Enabled` and the rule once it exists. Related open point in `status/TRU-07-a306-per-step-policy.md` Q3 (`needsWrite` versus "names a capability" in T12).
2. **`Ownership` (T8): not built; there is no data source for ownership on main.** `OwnsTarget(tenant, principal, AdmittedIntent)` must answer whether the principal owns the target (`AdmittedIntent.TargetRef`, a `fin.transaction:<id>` reference). What exists, and why none is a source:
   - The pack names the fact: `fin.src.customer` is authoritative for `customer_identity_and_ownership`, and the `account` descriptor has `owner` (`packs/README.md`, PCK-04, ruling D6). `adapters/README.md`: "Interfaces for `mock-customer` and `mock-policy` ... are not built", their payloads "are fixed when the Tushe OpenAPI is frozen"; no `adapters.Customer` port exists. The signed Tushe worksheet is not there.
   - The pack gives account ownership (`account.owner == principal_id`); the target here is a transaction. No port, table or pack field maps a transaction to its account or its owner.
   - The only behaviour that implies ownership is the stand-in's scoped per-reference reads (`synthetic/standin`: "not owned looks like not found"; `adapters.Ledger`/`Switch`/`Destination` take a `Scope`). That is one observed response of a stand-in, not a Tushe semantic (AGENTS section 2 item 4: nobody infers endpoint semantics from one observed response), and the read would also be an oracle (a read about a target before T8 decides, TRU-04 note (a)).
   - No table in the migrations holds principal-to-resource ownership (the `owner` hits in the migrations are database ownership).
   Needed: the `mock-customer` (ownership) read port and its Tushe semantics, or a ruling that the stand-in's scoped read is the POC source, plus the transaction-to-owner mapping. Open since TRU-04: what `OwnsTarget` answers for an intent with no `principal_must_own` slot or no target (the port says true; that lets the business layer PASS with nothing checked). `StoredIntents` makes a resolved owned slot with no reference an error, so that case cannot arise from a stored intent, but the adapter PR must still decide it.

## Interpretations to confirm (Architect), defaults applied (the safer reading, AGENT-20)
- Q1 Every ambiguity flag the document carries is material to `View.MaterialFlags`. The pack's `understanding.material_flags` ("always material") is not used to narrow it: the conditional flags (AMOUNT_MISSING, TIME_VAGUE) cannot be judged by Trust, and the Interpreter port already holds any flag for clarification. Narrowing would be a one-line change.
- Q2 Required slots are the pack node's `required: true` slots; a missing one is ABSENT; a status that is not one of the four words, or no status on a slot that is not a Reference, is UNRESOLVED (the Interpreter port's own reading, noema#661).
- Q3 `TargetRef` is the `resolved_ref` of the node's `principal_must_own` slots (trust profile `authority`). Two different targets make the intent unusable (the port has one `TargetRef`, T14 lets a step act on one target). A node with no trust profile has none.
- Q4 `Verify` has no signature to check: the stored document carries the `unsigned` marker (A166); the hash covers the whole document including it.
- Q5 `intentstore.Verify` was exported (C2's package, one line). Needed to reuse the check, not copy it.
- Q6 Migration 0030 (the `tenant` row-level security of `wt-0030`) is not on main. Today `trust_rw` can read every tenant's `tenant` row, so `KillSwitch` relies on its own `WHERE tenant_id = $1` (tested with an administrator's pool). When 0030 lands, add the test that a login of another tenant sees no row (`ErrNoTenant`); not added now because it would fail on main.

## Tests added (REQ, INV, US or EC IDs)
REQ-TRU-003 (same answer for another tenant's and an absent intent), REQ-AMB-001 / INV-06 (view, unresolved intents), REQ-SEC-004 / INV-11 (the tenant bound without row-level security, no fallback pool), INV-12 (principal from the stored document; tenant only from the argument), INV-17 / REQ-AUD-001 (Verify), REQ-NFR-002 (a store failure, an expired context and a missing tenant are errors, never a refusal or "enabled"). Real PostgreSQL (internal/testpg, per-tenant logins), the real gate and the real store make the fixtures. 28 tests in `trustport` (21 in `intents_test.go`, 7 in `killswitch_test.go`, shared world in `world_test.go`) plus `TestVerifyAcceptsABoundDocumentAndRefusesAnotherHash` in `intentstore`.
- Regression cases added: none (no bug found in existing code).
- Debt entries: none filed (the Repo Steward files them); the two "not built" items above are the open points.
- Conflicts raised: none.

## Mutants (each applied, tested, reverted with `git checkout -- <file>`; `git diff` empty before every commit)
31 mutants over `intents.go`, `killswitch.go` and `intentstore.Verify`; 30 killed, 1 shadowed by design. Three were first found surviving and each got a test before it was killed again: M13 (a slot with no status read as resolved), M21 (an empty principal accepted), M22 (an owned slot with no reference skipped); the two own-timeout tests (M20, M28) were added before the mutants ran, after a first reading showed nothing would catch them.
- Tenant bound: M01 (SQL bound of `row` dropped) survives alone, because the returned row's own tenant check (M05) hides it, as the `intentstore` README says of its two bounds; M05 alone is killed (a store that answers with another row), and both removed together (M31) is killed. M02 (`LatestVersion` bound dropped) and M23, M24 (kill switch: a fixed tenant, no bound) are killed.
- Errors swallowed: M03, M04, M08 (a store error becomes not found or passes), M25 (a store error becomes "enabled"), M26 (a missing row is "enabled"), M19 (the context error dropped), all killed.
- Verify: M06 (store check skipped), M07 (comparison with the carried intent skipped), M09 (an absent intent verifies), M10 (no canonical form), M18 (target not compared), M30 (`intentstore.Verify` always nil), all killed.
- View, target, pack: M11 (flags dropped), M12 (a missing slot resolved), M13, M14 (an unknown status word resolved), M15 (two targets, first wins), M16 (pack identity unchecked), M17 (fallback pool), M21, M22, all killed.
- Kill switch: M27 (inverted), M28 (no own timeout), M29 (no pack needed), M20 (no own timeout in `Get`), all killed.

## Pre-work for the next agent
- Exact next step: (1) Architect: the `NeedsWrite` rule as pack data, and the Ownership source (above). (2) EL: compose `StoredIntents` and `KillSwitch` in `wire_trust.go`. (3) Ownership adapter and `NeedsWrite` once decided, same package, same style.
- Open questions (owner): Q1 to Q6 (Architect); the answer of `OwnsTarget` with no target (Architect).
- Files to read first: `orchestrator/portadapters/trust/intents.go`, `killswitch.go`, `trust/intake/ports.go`, `ports_t7_t9.go`.
- Do not touch: `cmd/core` (EL), `trust/intake` (C1's, merged), `migrations/0026`.
