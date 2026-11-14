# evidence/portread

The Evidence Layer's call site for the read ports of `adapters` (ENV-04 wiring slice W1; REQ-CTX-001, REQ-CTX-002, REQ-ENV-001, REQ-AUD-001; INV-12, INV-19, INV-23, AGENT-25). Updated 11 Nov 2026.

`Reader.ReadTransaction(ctx, scope, correlationID, transactionRef)` reads the ledger, the switch and the destination answers for one transaction and records one evidence item for each through `layer.Builder`. It is the only place that turns a port's answer into a `layer.Observation`; the status is still assigned by `layer.AssignStatus` and nowhere else.

| Rule | How |
|---|---|
| Identity (INV-12) | The `Scope` (tenant and principal) is an argument the caller takes from the authenticated session. It is passed to the ports unchanged and written to the item. The package reads no identity from anywhere else and mints no assertion (the client does). |
| Pack-driven | The caller passes the evidence-source descriptors it took from the pack (`Source`: id, fact, authority, freshness, sensitivity, resource kind). No industry vocabulary is in the code. The package reads no pack file. |
| A failed read is an item | An adapter error becomes `layer.ClassifyError`'s class and, from `layer.BodyOf`, the hash of the bytes received or an explicit no-bytes. A source that says `Unavailable` is UNAVAILABLE, never an empty answer. An answer the reader cannot use (provenance without a label, a reference for another transaction, a time that does not parse) is MALFORMED, so UNKNOWN, with no payload. The other ports are still read. |
| Determinate | An OK read counts only when its value is final: a ledger with at least one entry and none pending; a switch status that is terminal and not UNKNOWN; a destination whose credit state is neither UNKNOWN nor PENDING and whose settlement state is terminal (`REVERSAL_PENDING` is not, A31). Otherwise UNKNOWN. These four defaults are accepted by ruling A148 (Architect, 12 Nov 2026; `docs/decisions/DEC-2026-11-12-architect-rulings-a148-a160`, merged in docs#157), answering the task card on noema#441 and noema#443. A148 reads: a read is final when the ledger, switch and destination reads all return KNOWN status; no settlement item in the slice; scope strings and payload shape as written in noema#443. Each port's item is KNOWN only when its own answer is final, and the package computes no combined flag: the caller derives it from the three items. |
| Record first | A refusal by the Recorder stops the call: nothing further is read (EC-E16). The items recorded before it are returned with the error. |
| Labels (AGENT-25) | A successful read carries the adapter's own `DataSource` and `Synthetic`; a failed read carries the reader's configured label. |
| Deadlines (AGENT-16) | The caller's context needs a deadline; each port call gets `ReadTimeout` from configuration (zero refuses every read). |

Not here: reading a pack, the candidates search (`Transactions`), the `fin.src.settlement` source (the destination answer is recorded under the destination source only), `cmd/core` wiring and the Compose bench profile (Integration Lead; they wait for EDG-01 slice 3b, the session in `cmd/core`).

Tests: `go test -race ./evidence/portread/` (no database; fake ports and a fake recorder).
