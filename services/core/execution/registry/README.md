# execution/registry

The Capability Registry of the core. Created by T1 of DEC-2026-10-20; built by EXE-01 (2026-11-03).

**Implements:** REQ-CAP-001 (complete descriptors only), REQ-CAP-002 (prohibited: registered, rejecting, unreachable). SoT IDs: INV-05, INV-18, BD-08, AGENT-06.

## What it does

- `Load(r)` decodes a domain pack from `r` (the pack is data, SoT §51) through the generated `contracts.PackV1`, then calls `New`.
- `New(descriptors)` builds the registry, or refuses the whole set (no partial registry) when:
  - the set is empty, or two descriptors share an ID;
  - a descriptor lacks any field (`SCHEMA_INVALID`);
  - a descriptor whose risk class is monetary write (`R4…`) is not marked prohibited (`INTERNAL_SAFETY_BLOCK`).
- `Callable(id)` is the only way to get a `Callable` handle. A prohibited ID gives `CAPABILITY_PROHIBITED`, an unknown ID gives `CAPABILITY_NOT_REGISTERED`, and the handle is then the zero value. IDs match exactly (no case folding, no trimming).
- `IsProhibited`, `Registered`, `IDs` and `Prohibited` are for Trust (reject at T4) and the Planner.

Errors are `*registry.Error` with a `Code` from `errors.v2.json`; callers use `errors.As`.

## What it does not do

- It does not decide *which* capabilities are prohibited. That is pack data. `packcheck` (PCK-02) checks that the eight of SoT §34.3 exist and are marked; the registry refuses only a monetary-write descriptor that is not marked. The core carries no industry term (INV-14).
- It holds no limits, timeouts or breakers (CAP-01, REQ-CAP-003) and no compensation rules (CAP-02).
- It imports no other power.

## Tests

```
cd services/core && go test -race -count=1 ./execution/registry
```

| Test | Proves |
|---|---|
| `registry_test.go` | REQ-CAP-002: the 8 prohibited are registered and refused; INV-18: unknown IDs refused; REQ-CAP-001: 11 kinds of incomplete descriptor fail the load, for an allowed and a prohibited capability; INV-05: a monetary write not marked prohibited fails |
| `property_test.go` (`rapid`) | REQ-CAP-002: for any ID drawn (prohibited, registered, made up, with case, padding or path disguises) a prohibited ID never gives a handle |
| `scan_test.go` | REQ-CAP-002: no identifier or string literal in any non-test, non-generated Go file of `services/core` names a prohibited operation (the names are read from the pack, so a ninth prohibited capability is covered); planted code of five shapes is found |

Use `-rapid.checks=N` for a deeper run and `-rapid.nofailfile` when running mutations, so no failure file is left in `testdata/`.
