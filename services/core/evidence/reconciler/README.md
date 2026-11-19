# evidence/reconciler

The Reconciler that `readseal` calls (ENV-04 slice 2b; A208, A245, A260 on noema#579 and noema#596). `Classify(ctx, tenant, items)` returns a reconciliation id and a classification.

**Implements:** REQ-RES-001, REQ-CTX-001. **Never:** a negative fact from an absence (INV-19), a store read of its own, a payload, a store's error text or a connection string in an error (AGENT-13), a default for anything it is not given.

Per item, in id order: the item is checked (an id, a source, a read time, an effective time, a freshness limit above zero, a status that is one); a KNOWN item's payload is read through `payloadread` as the tenant of the call and verified against the item's hash; the pack's mapping turns it into rows of the rule set's vocabulary; each row becomes one `reconcile.Evidence` (id `<evidence id>/<entity>`, source, status, effective time, expiry = read time plus the effective freshness limit, **required**, fields). A row the payload gave no field for, a payload that is not the sealed one or not one object, and an item that is not KNOWN are UNKNOWN or their own status: evidence that cannot satisfy a rule. A store or context failure is not an absence: the whole call is refused (`ErrRead`, the cause reachable with `errors.Is`, never printed).

`reconcile.Reconcile` then classifies with the real rule set. The reconciliation id is `rec_` and 26 letters from the SHA-256 of everything the classification depends on: the rule set, the mapping, the clock reading it was computed at, and for each sorted item its id, source, payload hash, status, read time, effective time and freshness limit. The same inputs give the same id; a later clock reading, another expiry or another source gives another. It names what was classified (INV-21, INV-23) and holds no digit.

**Refused whole:** an item whose read time is after the clock's reading (it would push its expiry out past its limit; a read at the reading is used). The freshness limit has no upper bound here: its bound is the Evidence layer's (`layer` takes it from the source's profile), not this package's.

**What it does not surface:** a stored payload that is not the sealed one (`payloadread.ErrHashMismatch`), is not an object, or is not canonical becomes UNKNOWN silently, as A260 says. That is also a possible tamper signal. This package emits no Record event and no metric for it; where it is surfaced is not decided (open question in `status/ENV-04.md`, owners REC and SEC).

Not observable through the answer, and tested apart or stated: the expiry and the clock (they feed `ValidUntil`, which `readseal` does not take; the expiry is tested in `internal_test.go`; the clock's reading changes no class of the current rule set).

**Tests:** `cd services/core && go test ./evidence/reconciler/`. The real rule set and mapping of `packs/fintech` run; the payload store is a spy. Synthetic data.
