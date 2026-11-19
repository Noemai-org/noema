# Status · STM-01 · wire the investigation reader (noema#715)

Date: 2026-11-18 · Owner: Core Engineer 1 (agent session), at the Founder's order of 18 Nov (relayed; rulings A234, A242 and A243 are committed in `docs` as interim records: `DEC-2026-11-16-architect-rulings-a234-a240.md` and `...-a242-a252.md`). Branch `prep/wire-investigations`, from `origin/main` at `543f246a`. **Nothing is pushed; no PR is open.** The task is not Done: the card below is written for the PR that will carry it.

## Task card (PERD 61.4, short)

- Task: STM-01, the production adapter of the `InvestigationReader` port, and the route mounted from the production components.
- Requirement IDs: REQ-STM-001, REQ-API-001, REQ-SEC-004, REQ-AUD-003 (the reconstruction route check); SoT INV-12, INV-30, AGENT-10, AGENT-13, AGENT-16.
- Allowlist (touched): `services/core/cmd/core/wire_investigations.go` and its test (new); `services/core/cmd/core/compose.go` (the lines listed below; the Engineering Lead's file) and `compose_test.go` (the fixture and one case); `services/core/orchestrator/investigations/store.go` (one read-only method), its README and `read_test.go` (new; C1's module); this file.
- Not touched: `deploy/**`, any compose file, `.github`, `main.go`, migrations, `/contracts`, the package of noema#710 (`orchestrator/portadapters/trust`), other worktrees.

## What is wired

1. `Store.Read(ctx, session, id) (Snapshot{State, Principal}, error)` in `orchestrator/investigations/store.go`: a read-only transaction, `WHERE tenant_id = $1 AND investigation_id = $2`; a row of another tenant or none is `ErrNoInvestigation`; a malformed ID, an incomplete session, an unconfigured store and a context with no deadline are errors. It says what the row says and decides nothing about who may see it. `PrincipalOf` is unchanged.
2. `cmd/core/wire_investigations.go`: `buildInvestigationReader(env, stores)` makes the `InvestigationReader`. It reads through the **Recorder's own per-tenant stores** (`orchestratorRecorder.For`), which sit on each tenant's orchestrator login (`NOEMA_ORCHESTRATOR_DSN_DIR`, row-level security with FORCE, migration 0025). **No new login setting and no new file; one new setting, the read bound (below, A319).** It returns `found=false`, with no error, for: another principal's, another tenant's, an absent or malformed investigation, an empty caller principal (no query), and a legacy row whose `principal_ref` is empty (it belongs to no one, so it is served to no one). It returns an error (503 in the route, with no driver text) for: a tenant with no store, a store that cannot be reached or refuses, anything unexpected. It never reads a request: the tenant and the principal are the route's arguments from the verified session (INV-12).
3. `cmd/core/compose.go`, exactly these lines (all in `buildComponents` and `missing`; EL to review):
   - in `missing`, after the `admissions` case, two lines: `case c.Investigations == nil:` and `return "investigations"`. So `compose` refuses, and `/readyz` names `investigations is not configured`, when the reader is absent. Placed after `recorder` and `admissions`, so those are still named first when the Recorder is absent (the reader is built from it).
   - in `buildComponents`, inside the existing `if recorder != nil {` block, a call `reader, err := buildInvestigationReader(env, recorder)`; on an error `closeAll()` and return the error (no components, no pool left open); then `c.Investigations = reader`. Only inside that block, so the interface is never a typed nil, and the setting is asked for exactly when the orchestrator settings are given.
   - the doc comment of `buildComponents`: two clauses ("and, over the same per-tenant stores, the investigation reader (A243)" and "which needs NOEMA_READ_TIMEOUT_MS as well (A319)").
   - `compose_test.go` (EL's test file): `full()` gains `Investigations: &fakeReader{}` (a composition without it is no longer full) and the table of missing components gains the `investigations` case. No assertion was weakened.
4. Route, verified through the production build (`TestTheProductionBuildServesAnInvestigationToItsOwnerAndToNoOneElse` and the others in `wire_investigations_test.go`, real PostgreSQL, real session middleware and role gate, `buildComponents` then `buildAPI`):
   - `GET /v1/investigations/{id}`: customer only; owner gets `{"schema_version":"1.0","state":"working"}`; another principal, another tenant, absent, legacy row and "the request names the owner in the query and a header" all get the one not-found shape (byte-identical apart from the correlation ID); ops, audit and approver get 404 from the role gate, no session 401; a closed pool gives 503 `DEPENDENCY_UNAVAILABLE`.
   - The reconstruction route `GET /v1/reconstructions/{correlation_id}` is mounted in the production build whenever the Record reader is configured (`Components.Records`, from `NOEMA_RECORD_READ_DSN_DIR`), for **ops and audit only**; the test shows ops and audit reach the handler's own answer (a tenant with no Record login gets the handler's `INTERNAL_SAFETY_BLOCK`), and customer and approver get the role gate's 404. The route table of the production build is asserted: exactly two routes.
5. `/readyz`: `composeReadiness` is unchanged; `missing` names `investigations` as above. Readiness is **not** green: the interpreter, evidence, trust and explainer ports are not built, and it names the first of them.

6. **The read bound (A319, noema#715).** `NOEMA_READ_TIMEOUT_MS` is read through the cmd/core `Env` with no default in code. It must be a whole positive number of milliseconds: ASCII digits only, no sign, space, leading zero, unit, fraction or exponent, and not so large that it overflows a duration. A missing or invalid value builds no reader: `buildInvestigationReader` returns an error that names the setting (never the value), and `buildComponents` returns it, closes every pool and returns no components, the way the Recorder's own settings fail. The setting is asked for only when the Recorder is built; one given without the orchestrator settings is ignored. A read runs under the earlier of the request's deadline and the bound. Tests: `wire_investigations_timeout_test.go` (three tests); existing tests that give the orchestrator settings now also set it (`components_test.go` one line, `wire_investigations_test.go`).

## What is NOT wired, and why

- **`POST /v1/conversations` (`Components.Gateway`).** `inputgateway.New` needs `Recorder`, `Index`, `Start`, `IDs`, `MaxBody`, `HashKey`. Merged: `RecordRecorder`, `RecordIndex`, the ID source, `gatewayIdentity`, and the route wrapper `startConversation`. **Missing, so nothing was invented:**
  1. **The `Start` seam** (`func(ctx, inputgateway.Turn) error`): no adapter exists. It must (a) map the `Turn` and the session to `investigations.StartRequest` with the investigation ID of A234 (`investigationIDOf(correlation)`, the same value the route answers) and `InputReceivedRef = Turn.Input.EventID`, calling `Store.Start` (idempotent per input, as A168 needs), and (b) hand the run to the orchestrator. `Runner.RunRead` does not call `Start` yet (`orchestrator/investigations/README.md`, "Not here" 1) and needs `Interpreter`, `Evidence`, `Trust` and `Explainer`, none of which has a production adapter in `buildComponents`. Whether the run is synchronous, a goroutine or a Temporal workflow is undecided.
  2. **A per-tenant Recorder and Index.** `RecordRecorder` and `RecordIndex` each hold one `*pgxpool.Pool` (one tenant's `core-edge` emitter login, one tenant's `record_read` login). `Gateway` takes one `Recorder` and one `Index`. The composition root's pattern is one pool per tenant behind a `For(tenant)`; no dispatching adapter exists for these two, and no directory of `core-edge` logins is loaded (`loginPools` could load one).
  3. **Settings with no default (AGENT-16) that do not exist yet:** the `core-edge` login directory, the `core-edge` signing key directory (`signing.LoadRing("core-edge", ...)`), the HMAC hash key file (at least 32 bytes), the body bound (the edge's `NOEMA_EDGE_MAX_BODY` may or may not be the same value), the 24-hour window and the per-append timeout (`RecordIndex.Window`, `Timeout`, `RecordRecorder.Timeout`). Each is a new name or a decision, and mounting the secrets is `deploy/**`, a protected path.
  The production build therefore has `Gateway == nil` (asserted by a test) and the route is unmounted. `/readyz` does **not** name the gateway: `missing` has no case for it (see question 2).
- **`docs/architecture.md`** is the PLT-01 text and was not updated (it describes none of the slice).

## Mutants (each applied by hand to the green code, the named tests run, each reverted)

Read bound (A319), all killed: a leading zero allowed; the setting ignored (a fixed 5s); no bound when the request has none; a default when the setting is empty; the request's earlier deadline dropped; the overflow guard removed; `buildComponents` swallowing the error. Reader and store (earlier session):

All killed: the principal check dropped; the tenant bound dropped (the store of another tenant used; the session's tenant replaced); an empty principal accepted (the legacy row served); the store error swallowed as not-found; the no-store error swallowed as not-found; the early return for an empty principal or malformed ID removed; in `Store.Read` the tenant clause removed (killed only by the test that uses the administrator's pool, which row-level security does not bind) and an absent row read as an empty snapshot; `missing` without the `investigations` case; the reader not plugged into `buildComponents`. Not killable by a test, stated: the `ReadOnly` access mode of the transaction (the database would refuse a write by grant either way; the test asserts the row is unchanged).

# Session 2026-11-18 · STM-01

## Session summary
- What changed (PR links): none yet; local commits on `prep/wire-investigations` (red, then green). No push.
- Why: the Founder's order of 18 Nov; A243 asks for a read-only store method and a principal on the investigation (migration 0029, merged), and the route had a port but no production reader.
- Risks, and how they are contained: (1) the reader serves only the principal that started the investigation (compared in Go after a tenant-bound, RLS-bound read; four layers: the pool's login, the query's tenant clause, the principal comparison, the one not-found shape); (2) a legacy row with no principal is served to no one; (3) a failure reads as an error, never as not found; (4) `compose.go` is EL's: the change is three lines and a comment, listed above; (5) the read bound is a setting with no default (A319), so a deployment that gives the orchestrator settings without it does not start (listed for IL below).
- Tests added (REQ-STM-001, REQ-API-001, REQ-SEC-004, INV-12): `orchestrator/investigations/read_test.go` (6 tests) and `cmd/core/wire_investigations_test.go` (11 tests, real PostgreSQL); a case in `compose_test.go`.
- Regression cases added: the same tests (no defect was found).
- Debt entries: none. One test-infrastructure note: `cmd/core` had no `TestMain`, so each `go test` of it left a PostgreSQL server and its data directory running (`internal/testpg` stops the server only from `Main`); `wire_investigations_test.go` now has `func TestMain(m *testing.M) { testpg.Main(m) }`. If a concurrent change adds another `TestMain` to the package, the build fails loudly and one of them goes.
- Conflicts raised: none. Note for EL: the A243 text says the route "reads through a read-only method of the store"; the principal comparison is in the adapter, not in SQL, so the store method stays a pure read.

## Pre-work for the next agent
- Exact next step: EL reviews the compose lines; then the `Start` seam (see "What is NOT wired", 1) and the gateway settings (3), which need a decision on the run (question 3) and a deploy PR for the secrets (IL).
- Open questions (owner):
  1. CLOSED by A319: the read bound is `NOEMA_READ_TIMEOUT_MS`, no default in code. **For IL (deploy/compose, not touched here):** set `NOEMA_READ_TIMEOUT_MS=2000` on the core service wherever `NOEMA_ORCHESTRATOR_DSN_DIR` is set. Without it the core refuses to start with `cmd/core: NOEMA_READ_TIMEOUT_MS must be a whole positive number of milliseconds`.
  2. EL: should `missing()` also name `input gateway` when `Components.Gateway` is nil? Today `/readyz` could turn green with the conversation route unmounted once the four unbuilt ports land. One case plus the fixture (`newGateway` exists in `conversations_test.go`). Not done: it is outside the order and the gateway cannot be built yet.
  3. Architect or EL: the `Start` seam: who calls `Runner.RunRead`, synchronously, in a goroutine, or as a Temporal workflow (STM-04)?
  4. EL: `NOEMA_ORCHESTRATOR_DSN_DIR` now also carries the investigation reads. The login is the same `orchestrator_rw` login that writes; a read-only login for the route would be a separate decision (it is not needed for correctness: the reader's transaction is read-only).
  5. Steward: whether this work is a new task row or part of STM-01 (the file name `STM-01-wire-investigations.md` is the proposal).
- Files to read first: `services/core/cmd/core/wire_investigations.go`, `investigation.go`, `compose.go`, `orchestrator/investigations/store.go`, `inputgateway/README.md` ("For EL (composition root)").
- Do not touch: the package of noema#710 (`orchestrator/portadapters/trust`); `deploy/**`.

# Session 2026-11-18 (later) · STM-01

## Session summary
- What changed (PR links): noema#715, as two more commits on the branch (red, then green) after `origin/main` (`4e364be5`) was merged in with no conflict. The read bound is now the setting `NOEMA_READ_TIMEOUT_MS`, read through the cmd/core `Env` with no default in code, strict whole-positive-number parsing, required exactly when the orchestrator settings are given; the 5-second constant is gone.
- Why: ruling A319 (committed in docs, `DEC-2026-11-18-architect-rulings-a317-a328.md`, C1 row): "noema#715: mark ready. The reader read bound is `NOEMA_READ_TIMEOUT_MS=2000`." AGENT-16 allows no default for a bound.
- Risks, and how they are contained: a deployment that gives the orchestrator settings without the new setting now fails to start with an error that names it (the same behaviour as `NOEMA_ORCHESTRATOR_KEY_MAX_ACTIVE`); IL must set 2000 (listed above). Nothing in `deploy/` or the compose files was edited.
- Tests added (REQ-API-001, REQ-STM-001, AGENT-16): `wire_investigations_timeout_test.go`: strict parsing with no default (17 bad values, 3 good), the value used as the bound and never later than the request's deadline (observed on the store's pool seam, no failure mode mocked), and the requirement in `buildComponents` (missing, empty, zero, unit, leading zero, sign; valid; ignored without the orchestrator).
- Regression cases added: the same tests.
- Debt entries: none. Conflicts raised: none.

## Pre-work for the next agent
- Exact next step: IL sets `NOEMA_READ_TIMEOUT_MS=2000` for the core service; EL reviews the compose.go lines (listed above, now four lines and one comment).
- Open questions (owner): questions 2 to 5 above are unchanged.
- Files to read first: `cmd/core/wire_investigations.go`, `wire_investigations_timeout_test.go`.
- Do not touch: `deploy/**`; the package of noema#710.
