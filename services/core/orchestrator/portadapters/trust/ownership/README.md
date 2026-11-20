# orchestrator/portadapters/trust/ownership

The Trust intake's Ownership (T8), package `ownershipport` (TRU-07; rulings A329 and A344). Updated 19 Nov 2026. All data is SYNTHETIC.

**Implements:** REQ-TRU-002, REQ-TRU-003; INV-11, INV-12, INV-19; AGENT-13, AGENT-16; A347. **Never:** a write, a model call, a tenant or principal from anything but the session (INV-12), a read for a tenant the adapter was not built for (INV-11), "not owned" for a read that did not finish (INV-19), `intake.ErrNotFound` (T8 turns it into "not owned"), a default for a setting (AGENT-16), a row, an ID, a tenant or a cause's text in an error (AGENT-13).

`Ownership.OwnsTarget(ctx, tenantID, principalID, intent)` implements `intake.Ownership`. Ownership is a principal-scoped read through the enterprise read port of the candidates read (`candidates-request.v1` in, `candidates-response.v1` out; A344). `ScopedReader` is the same shape as the Candidates adapter's reader (noema#741); the composition connects both to one read port. This package does not import that adapter.

| Step | Rule | Answer |
|---|---|---|
| Wiring | reader set, tenant of the shape `ten_...`, at least one node, each node named and with a `<vertical>.<kind>` | `ErrConfig` (also the zero value) |
| Context | the caller's context has a deadline; the adapter adds none and has no setting | `ErrNoDeadline` |
| Session | tenant `ten_...` and principal `usr_...` of the contract's shape | `ErrSession` |
| Tenant | the tenant is the adapter's | `ErrTenant` |
| Intent | the intent names the session's tenant and principal, has an ID, a version of at least 1 and a correlation | `ErrIntent` |
| Slot | the pack gives the intent node no ownership slot (not in `Config.Nodes`) | `true`, no read |
| Target | the target is `<node kind>:<id>` with an id of the contract's shape; empty, malformed and wrong-kind targets are not references | `false`, no error, no read |
| Read | one `Read` as the session's tenant and principal, the node's resource kind, the whole history (the earliest to the latest instant of `Timestamp`), no other filter. The target is never sent | `ErrRead` (cause kept; never false) |
| Answer | version `1.x`, this tenant and correlation, at most 20 rows, each row of the contract's shape and of the node's kind, no candidate and no reference twice. One bad row refuses the whole answer | `ErrBadAnswer` |
| Found | a row's reference is exactly the target | `true` |
| Not found | the answer is complete (fewer than 20 rows) | `false`, no error |
| Cut off | the answer is 20 rows (the contract's most) and lacks the target | `ErrInconclusive` (never false) |

**One answer (REQ-TRU-003).** Another principal's resource, another tenant's resource, an absent resource and a malformed reference all return `(false, nil)`. The reader's request carries no part of the target, so its reply cannot differ by it. Errors are about the call or the read, never about the target.

**Errors (A347, AGENT-13).** Fixed text with the prefix `ownershipport: `. The cause is kept for `errors.Is` and `errors.As` and is not in `Error()`. No tenant, principal, reference, row or driver text.

**Open: the contract cannot name one resource.** `candidates-request.v1` has no filter by resource reference, so the adapter reads the principal's rows of the kind and looks for the target. A principal with 20 or more resources of that kind, whose target is not among the newest 20, gets `ErrInconclusive` (T8 ends UNKNOWN), never a wrong "not owned". A reference filter on the read (a contract change, AGENT-15, not made here) removes the limit. See `status/TRU-07-ownership.md`.

**Pack data (for the composition).** `Config.Nodes` maps each intent node whose capability has `principal_must_own` to the resource kind that is owned. A node that is not in it is answered `true` without a read, so the composition must list every such node from the pack.

**Setting.** None. The deadline is the caller's context.

**Tests.** From `services/core/`: `go test -race ./orchestrator/portadapters/trust/ownership/`. No database. The store in the tests is a real reader of the contract (it scopes by the request's tenant and principal, filters by kind and window, orders and caps at 20); the faults are real (a failing reader, an expired context, answers outside the contract). Written first; the mutants that were killed are in `status/TRU-07-ownership.md`.
