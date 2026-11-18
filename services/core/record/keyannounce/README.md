# record/keyannounce

**What it does.** Announces key registrations on the Record. The deploy step registers public keys through `key_register` and writes no Record event (it holds no signing key). At start-up the core calls `Announcer.Announce` once per tenant: for every `signing_key` version with no `KEY_ROTATED` written by `admin` on that tenant's chain, it appends one, written and signed by `admin`, with the payload `{"key_id": "<identity>.ed25519.<version>"}` and nothing else (DEC-2026-11-01-receipt-and-record-signing D2a, D3a, D4).

**REQs.** REQ-SEC-005 (audit: `KEY_ROTATED`), REQ-AUD-001, REQ-AUD-006. SoT: INV-11.

**How it behaves.**

- Exactly once per key and chain: it reads the registry and the tenant's chain, and only when something is missing takes a per-tenant advisory lock in the admin login's transaction, reads again under it, appends and commits. A second or a concurrent start-up appends nothing.
- Fail closed, Record first: any failure is returned and the transaction is rolled back, so nothing partial is kept. The caller refuses to start.
- One tenant per call. The keys are the deployment's and the Record is the tenant's, so every tenant's chain is announced on its own (an assumption the Founders confirm; see `status/SEC-KEY-ROTATED-startup.md`).
- It holds no key and registers, retires or revokes nothing. The admin signer is passed in through the `chain.Appender`; the package never reads a key file.

**Connections it needs, per tenant.** The tenant's admin emitter login (`record_emitter`, bound by `provision_emitter_login`), the tenant's `record_read` login, and one login with SELECT on `signing_key` (the admin emitter login has none).

**Tests.** `go test ./record/keyannounce/` from `services/core/`. They need PostgreSQL server binaries or `NOEMA_TEST_DATABASE_URL`. Every key and datum is SYNTHETIC.

**Not wired into `cmd/core`.** The composition root has no admin signer, admin emitter login or key-registry login yet; see the status file for the exact call.
