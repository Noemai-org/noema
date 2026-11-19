# Status · SEC-KEY-ROTATED start-up announcement (Core Engineer 1) · KEY_ROTATED for every signing-key version

Updated 2026-11-18. Order of the Founders, A317 to A328 (relayed, interim, not yet committed by the Steward): "KEY_ROTATED at startup". Source: DEC-2026-11-01-receipt-and-record-signing D2a ("At start-up the core appends `KEY_ROTATED` (signed by `admin`) for every `signing_key` version that has none yet"), D3a, D4 invariant K1, test 20; PERD 32.2 (control plane), REQ-SEC-005 (audit: `KEY_ROTATED`).

## State: library built and tested locally, not pushed, not wired into `cmd/core`

- `services/core/record/keyannounce`: `Announcer.Announce(ctx, Tenant)` appends one `KEY_ROTATED` (written_by `admin`, actor `admin`, payload `{"key_id": ...}` only, signed by the admin signer of the appender) per `signing_key` version that has no admin `KEY_ROTATED` on the tenant's chain, in one transaction, and returns the key IDs. No migration, no new dependency, no change to a contract, to `chain`, to `payloadguard` or to `cmd/core`.
- Exactly once: a first read (registry and chain) finds nothing missing and ends without a write; otherwise the admin login's transaction takes `pg_advisory_xact_lock` for the tenant, reads again under it, appends and commits. Idempotency comes from the chain itself (the events), not from a table or a memory.
- Fail closed: any error (registry unreadable, chain unreadable, signer missing or failing, the database refusing the emitter or the signature, no deadline) is returned and rolled back; nothing partial is kept; a later run with the fault removed announces every key.

## Where `cmd/core` must call it (the composition root is EL's; I did not touch it)

`cmd/core/main.go` builds the components and then serves; no path there builds an `admin` signer, an admin emitter login or a key-registry login (the orchestrator's Recorder, `recorder.go`, is the pattern to copy). After `buildComponents` and before `ListenAndServe` (so `/readyz` and the API never open on a Record that does not announce its keys):

```go
adminRing, err := signing.LoadRing("admin", env.Getenv(EnvAdminKeyDir), signing.Options{MaxActive: n}) // new settings, no default (AGENT-16)
ann := &keyannounce.Announcer{
    Appender: &chain.Appender{Clock: clk, IDs: idg, Signers: map[string]chain.Signer{"admin": adminRing}},
    Keys:     keyannounce.PoolKeys{Q: keyRegistryPool}, // a login with SELECT on signing_key
}
for tenant := range tenants { // the tenants that have both logins below
    _, err := ann.Announce(startCtx, keyannounce.Tenant{ID: tenant, Admin: adminPools[tenant], Reader: readerPools[tenant]})
    if err != nil { logger.Error("key announcement", "tenant", tenant, "error", err.Error()); os.Exit(1) } // refuse to start
}
```

It needs three things the core does not have today: (1) a per-tenant pool for the admin emitter login (`provision_emitter_login(login, tenant, 'admin')`, migration 0011; `loginPools` builds such pools from a DSN directory), (2) the admin signing key file (the key must be registered with `key_register` before start; the first `KEY_ROTATED` is for `admin.ed25519.1` itself and is signed by it), (3) one login with SELECT on `signing_key` (granted today to `trust_rw`, `execution_rw`, `outcome_rw`, `record_append`, `record_verify`; the admin emitter and `record_read` logins have none). The tenant `record_read` pools already exist (`NOEMA_RECORD_READ_DSN_DIR`).

## Questions for the Founders and the Architect (none blocks the library; each is an assumption I made on the safer reading)

1. **Which Record?** `signing_key` has no tenant, the Record is per tenant, and D2a does not say whose chain a key's `KEY_ROTATED` goes on. I read it as: every tenant's chain gets one per key (the verifier of any tenant sees every key that signs its events), and `Announce` takes one tenant so the caller decides the set. A platform-level chain would need a decision and a tenant for it.
2. **`object_ref`.** Neither PERD 32.3 nor the decision names a reference for a key object. `key_<first 32 hex of SHA-256(key_id)>`: one reference per key, matching the contract pattern, and not the key ID spelled differently (the test scan of every test Record flags an `object_ref` derived from a payload string, `payloadguard.DerivedObjectRef`; I checked that `key_core_execution_ed25519_1` is flagged against the payload `core-execution.ed25519.1` and the hashed form is not). Alternative: a minted `key_<ULID>` (then the key is found only by payload).
3. **`actor`.** D2a says `admin:<id>` of the authenticated administrator for admin-API events. A start-up announcement has no administrator session, so `actor` is the emitter's own identity `admin`, which record-event.v3 allows for an event no person caused.
4. **Only `admin` counts.** The database does not restrict an event type to an emitter, so another emitter can write a `KEY_ROTATED`; I count only the admin's as the announcement (tested).
5. **Retire and revoke** are not announced: D2a covers registration ("for every `signing_key` version that has none"). `key_retire` and `key_revoke` write nothing to the Record today; whether they should (`KEY_ROTATED` again, or `SECURITY_EVENT`) is a decision.
6. **Cost.** Each start-up reads the tenant's whole chain (`record_read_chain_t` is the only read the `record_read` login has). Fine at POC size; a definer function returning the announced key IDs would remove it (a migration, number 0032 or later; not needed, so not written).
7. **A key registered while a start-up runs** (the deploy step is a separate step) is announced at the next start-up, not this one.
8. **D2a lint.** "The `admin` key file is read only by the admin-API package (architecture-lint rule with a canary)": no admin-API package exists yet. This package never reads a key; the composition root would, which the future rule has to allow or move.

## Session 2026-11-18 · SEC-KEY-ROTATED start-up announcement

### Session summary
- What changed (PR links): none (local commits only, not pushed; RED `cd5b70b5`, GREEN `79279718`, then this file and the READMEs).
- Why: DEC-2026-11-01 D2a: every key registration must reach the Record, and the deploy step cannot write it.
- Risks, and how they are contained: Tier 1 (Record, identity). No migration, contract, guard or `cmd/core` change; the append goes through the existing signed `record_insert_t` path, so the database still verifies the admin signature and the emitter binding. A failed announcement refuses start-up (the caller's job; the library returns the error and keeps nothing). Open questions above.
- Tests added (REQ-SEC-005, REQ-AUD-001, REQ-AUD-006, INV-11, DEC-2026-11-01 test 20): `record/keyannounce/keyannounce_test.go`, 11 tests on a real PostgreSQL: every key with no event gets one (admin, admin, `{"key_id"}` only, signed under `admin.ed25519.1`, chain and verification job accept); no key material in body, payload or signed bytes; a key that already has one gets none and a restarted process appends nothing; a second run moves no head; a new version appends one more; 8 concurrent runs append one per key; each tenant's chain on its own; no key is no chain touched; another emitter's `KEY_ROTATED` does not count; six failure cases (signer fails on the second append, admin key not the registered one, no admin signer, login is not the admin emitter's, registry unreadable, chain unreadable) each an error with nothing kept and a clean retry; a context with no deadline is refused.
- Regression cases added: the failure table above (it is the failing-append case of the order).
- Debt entries: none filed (question 6 is a limit, not a shortcut).
- Conflicts raised: none against an invariant or decision; questions 1 to 5 are gaps in D2a.

### Mutants (each run against the 11 tests, each killed, original restored)
No idempotency (never reads what is announced): 4 tests fail. Append for all keys each time: 4 fail. Wrong emitter (`core-record`): the database refuses, 10 fail. Key material in the payload: with a hex string the append guard refuses first, 10 fail; with letters only (`abcdef...`) the payload and no-key-material tests fail. Any emitter counts as announced: the other-emitter test fails. No re-read under the lock: the concurrency test fails 5 of 5 runs. No advisory lock (re-read kept): fails 5 of 5 runs.

### Pre-work for the next agent
- Exact next step: EL wires the call above in `cmd/core` after the three inputs exist (admin key setting, admin emitter login pools, a `signing_key` reader login), with a `cmd/core` test that a start-up with an unannounced key appends it and one with a failing append exits non-zero. Then settle questions 1 to 3 with the Architect.
- Open questions (owner): 1 to 8 above (Founders for 1, 3, 5; Architect for 2, 8).
- Files to read first: `services/core/record/keyannounce/keyannounce.go`, DEC-2026-11-01 D2a and test 20, `cmd/core/recorder.go`, migrations 0011, 0013, 0019.
- Do not touch: `payloadguard` (its `/key_id` exemption is what lets the payload through), `chain.Appender`, the signing_key migrations.

## Session 2026-11-19 · SEC-KEY-ROTATED fixed-text errors (A347)

### Session summary
- What changed (PR links): noema#738, local commits not pushed: RED `7b8ba703`, then the GREEN commit that follows it; the sites are listed in `services/core/record/keyannounce/README.md` (Errors).
- Why: A347 (relayed, interim): an error crossing a package boundary has a fixed public text; the tenant, key IDs, the driver's message and paths are not in `Error()`, and the cause stays behind `Unwrap`.
- Risks, and how they are contained: Tier 1 (Record). Behaviour is unchanged; only error text and wrapping change. A caller that matched on the old text (none in the repository) would break; `errors.Is` and `errors.As` still reach every cause.
- Tests added (AGENT-13, A347): `record/keyannounce/errors_test.go`: a table over 14 error sites of `Announce` and 5 of `PoolKeys.KeyIDs`, on a real PostgreSQL (closed pool, session terminated while the lock statement waits, expired deadline, a login without privilege, a refused signature, a refused emitter, a connection lost before the commit); each checks the text for the tenant, key ID, SQLSTATE, driver words, DSN and paths, and that the cause's own text is not in the error's text, and that `errors.Is` or `errors.As` reaches the cause.
- Regression cases added: the same table.
- Debt entries: none.
- Conflicts raised: none.

### Mutants (each run against `errors_test.go`, each killed, original restored)
Each of the 11 changed sites reverted to its old `fmt.Errorf` (or bare `err`) in turn: 11 of 11 fail. The commit site was first not caught (its driver text, `conn closed`, holds none of the listed words); the test now also fails any error whose text contains its cause's text, and that mutant is killed.

### Pre-work for the next agent
- Exact next step: none for this item; the wiring in `cmd/core` is unchanged.
- Open questions (owner): `PoolKeys.KeyIDs` scan and `rows.Err()` are provoked through a stub `chain.Queryer` (a one-column result cannot be made to fail mid-stream on a real connection); say if that is not acceptable under AGENT-12 (Architect).
- Files to read first: `record/keyannounce/keyannounce.go`, `errors_test.go`.
- Do not touch: the unchanged fixed-text errors.
