# evidence/readseal

The adapter for the orchestrator's Evidence port (ENV-04 wiring slice W2; REQ-CTX-001, REQ-CTX-004, REQ-AUD-001; INV-11, INV-12, INV-14, INV-17, INV-19). Updated 13 Nov 2026. All data is SYNTHETIC.

`Adapter.Read(ctx, Session, intentRef)` implements `orchestrator.Evidence`. For `intent_id@version` of the adapter's own tenant it reads the stored intent, takes the transaction from the intent's resolved transaction slot, reads it through `portread` as the session's scope, has the items classified, seals the snapshot and returns `EvidenceRef{SnapshotID, Unavailable}`.

| Step | Rule | Refusal |
|---|---|---|
| Wiring | every collaborator, the deadline of the seal transaction, the pack's slot name and resource kind are set; none has a default (AGENT-16) | `ErrNotWired` |
| Context | the caller's context has a deadline | `ErrNoDeadline` |
| Session | tenant and principal set (INV-12); the tenant is the adapter's (INV-11; a wiring mistake, not an external answer) | `orchestrator.ErrNoSession`, `ErrTenant` |
| Reference | `int_<id>@<version>`, version 1 to 999,999,999 | `ErrIntentRef` |
| Intent | the stored intent is of the adapter's tenant **and names exactly the session's principal** (the database binds the tenant, nothing binds the principal, so it is checked here, INV-12); an intent of another tenant or principal, or one that names no provable principal, answers exactly as a missing intent | `intentstore.ErrNotFound` (wrapped) |
| Transaction | the configured slot is RESOLVED and its `resolved_ref` is `<configured kind>:<id>` with an ID the read call site accepts; a surface form or a candidate is never used | `ErrNoTransaction` |
| Read | the Record comes first: a failed record stops the run; no item at all is never sealed (INV-17) | the read's error, `snapshot.ErrEmptySet` |
| Classify | the `Reconciler` answers with a reconciliation ID (`rec_` and 2 to 32 letters or digits) and a classification (upper case, digits, underscore): the shapes migration 0020 checks; the database does not check the closed set of classifications, which is the Reconciler's contract (PERD 22.3) | the Reconciler's error, `ErrIncomplete` |
| Seal | one transaction of the tenant's Trust login: the snapshot row and its `CONTEXT_SNAPSHOT_SEALED` event commit together or not at all | the seal's or the commit's error, rolled back |

**Unavailable.** True when any item is UNAVAILABLE, UNKNOWN or UNAUTHORIZED (INV-19). The snapshot is still sealed over it. The adapter never decides what that means; Trust does.

**For the caller (the orchestrator's composition).** Map `intentstore.ErrNotFound`, `ErrTenant`, `ErrNoTransaction` and every other error to one external answer, and never return `err.Error()` to a client: wrapped errors carry the underlying port and database errors. `Timeout` bounds the seal transaction only; the read and `Classify` run under the caller's deadline (the context must have one), and the item count is the read call site's (one item per port). The tenant binding in the database and a cross-tenant and cross-principal test over Postgres belong to the first real wiring; the tests here use fakes for the collaborators and assert the database binding by this comment only.

**No industry terms (INV-14).** The slot name and the resource kind are configuration taken from the pack.

**Not here.** The real `Reconciler`: it needs the pack's evidence-source descriptors (entity and fields per source), which the pack does not carry yet, and the rule set `packs/fintech/recon_rules/rules.v1.json` over typed fields. Until it exists the adapter cannot be wired into `cmd/core` (EL, EDG-01 slice 3c). Design points: noema#547.

**Tests.** `go test ./evidence/readseal/` from `services/core/`. They use fakes for the adapter's collaborators and need no database. The real sealer and the Record are tested in `evidence/snapshot` and `evidence/layer`.
