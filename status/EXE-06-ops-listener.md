# Status · EXE-06 ops listener (Engineering Lead, agent session)

Updated 2026-11-26. Plan task EXE-06 (REQ-EXE-008, REQ-OPS-002, REQ-SEC-006). Branch `feat/EXE-06-ops-listener`. Instruction: A710 and A699, A666 (docs `DEC-2026-11-26-architect-rulings-a704-a720`, `...-a690-a700`, `...-a657-a667`; interim until merged there).

## State: built locally (red then green), not pushed, no pull request.

## Session 2026-11-26 17:40 WAT · EXE-06 ops listener

### Session summary
- What changed (PR links): none yet. New `services/core/cmd/ops/opslistener` (listener, doc, tests), `services/core/cmd/ops/README.md`, one CODEOWNERS line.
- Why: A666 and A699: only a listener behind operator mTLS certificates may set or clear a tenant's switch; SR reads it as a keys and identity path (A541).
- Risks, and how they are contained: (1) any certificate the operator CA signs is an operator: there is no allowlist of names (not defined in any ruling); the CA file is the control. (2) The tenant is named in the path by the operator; `controlplane.Logins.For` reaches only that tenant's own login. (3) The principal is the certificate's common name in the Record actor grammar; a name outside it is refused.
- Tests added (REQ-EXE-008, REQ-OPS-002, REQ-SEC-006, REQ-SEC-009, REQ-AUD-001, INV-03, INV-11, INV-12, AGENT-13, AGENT-16): real TLS handshakes with in-memory test PKI; no database needed.
- Regression cases added: none. Debt entries: none. Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the ops `main` (reads the three settings, builds the per-tenant admin emitter login pools from a directory of DSN files, builds `controlplane.Switch`, calls `Serve`) and the netcheck assertion that only that process mounts the admin emitter DSN (A699).
- Open questions (owner): see the PR body, "Safer readings taken".
- Files to read first: `services/core/cmd/ops/opslistener/doc.go`, `listener.go`.
- Do not touch: `execution/controlplane/switch.go`, migration 0042.
