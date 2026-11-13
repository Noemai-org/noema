# Status · ENV-13 · session notes

Moved from `docs/sessions/` on 26 Oct 2026 (DEC-2026-10-26-session-notes-in-status). One section per session, newest last; the text of each section is unchanged (headings one level lower). Files merged: `2026-10-21-1700-ENV-13.md`, `2026-10-24-1510-ENV-13b.md`.

## Session 2026-10-21 17:00 WAT · ENV-13

### Session summary
- What changed (PR links): the webhook relay, `synthetic/webhook_relay` (PR: see the branch `sB/t4-webhook-relay`).
- Why: DEC-2026-10-16 E6 and REQ-API-008: Tushe delivers to a relay, the core pulls; the core exposes no receiver.
- Risks, and how they are contained: the relay is reachable from outside. It holds only the signing secret, refuses to
  start with any other credential in its environment, holds no TLS key, stores nothing it has not verified, and is
  never an authority (INV-30). Refusals give one answer, so a probe learns nothing.
- Tests added (REQ, INV, US or EC IDs): REQ-API-008 (forged, tampered, unsigned, stale, future; replay; id conflict;
  persistence; pull auth; paging), E6 (single credential).
- Regression cases added: none (new component).
- Debt entries: none.
- Conflicts raised: none.

### Pre-work for the next agent
- Exact next step: the core's pull client (session A) using `GET /v1/deliveries?after=<next_after>`; `.env.example`
  entries; run `deploy/compose/netcheck/matrix.py` where a Docker daemon exists (not run in this session).
- Open questions (owner): whether Tushe's signing secret is given to us or we generate it (session A / founders).
- Files to read first: `synthetic/webhook_relay/README.md`, `internal/verify/verify.go`.
- Do not touch: the stored file format without a migration note.

## Session 2026-10-24 15:10 WAT · ENV-13 (follow-up: relay identity rows, #58)

### Session summary
- What changed (PR links): PR #58 adds `check_webhook_relay()` (matrix section 8) to `deploy/compose/netcheck/matrix.py`.
- Why: the founder asked for an identity check on the relay in the running stack (an unauthenticated pull is refused).
- Risks, and how they are contained: additive rows only; the merge with `main` kept both the relay check and main's `HOSTILITY_PROXY_SERVICES` clean-up.
- Tests added (REQ, INV, US or EC IDs): REQ-API-008 identity rows (pull without or with a wrong token: 401, unsigned delivery: 400, `/healthz`: 200).
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

### Instructions received (logged as the founder asked)
- 2026-10-24, founder, via the session: "main is green. #58 conflicts with main in deploy/compose/netcheck/matrix.py. Merge origin/main into your branch (no rebase, no force-push). Keep both sides: the webhook-relay check from your branch and the HOSTILITY_PROXY_SERVICES addition from main. Keep the check_webhook_relay() call and main's failures block. Run the matrix test locally, push, then do one fresh run on the final head. Report the head SHA and each check's result in one table, with run links. Log this message in your status file. Do not merge; the founder merges after approval."
- Earlier the same day: wait for the founder to say main is green; merge `origin/main` only if it conflicts; one fresh run on the final head; the founder merges in the order #51, #57, #58, #59, #60.

### Pre-work for the next agent
- Exact next step: the founder reviews and merges #58.
- Open questions (owner): none.
- Files to read first: `deploy/compose/netcheck/matrix.py` (section 8).
- Do not touch: other components' matrix rows.

# Session 2026-11-11 · ENV-13

## Session summary
- What changed (PR links): this file only; no code. The relay host was hardened by the Founder with the reviewed `deploy/relay-host/setup.sh` at `a3a08ff` (merged in noema#189 at 12:54 UTC, 11 Nov). The acceptance evidence is the comment on noema#189 (11 Nov 2026), hostname redacted as `<RELAY_HOSTNAME>`.
- Why: SR's findings F2 to F4 on #189 (Caddy admin API, public routes, container hardening) and ruling A139.
- Risks, and how they are contained: Caddy runs as root inside the container under A139, with every capability dropped but `NET_BIND_SERVICE`, `no-new-privileges` and a read-only filesystem (a non-root process cannot gain the port-binding capability under no-new-privileges on the host network). The admin API is off and only `/webhooks` and `/healthz` are proxied.
- Tests added: none in this change. The observations below were made on the host by the Founder.
- Regression cases added: none.
- Debt entries: none.
- Conflicts raised: none.

| Check | Result |
|---|---|
| Image pinned by digest (`caddy:2.8.4@sha256:226d1f05…`) | PASS |
| Read-only filesystem | PASS |
| Capabilities: CapDrop ALL, CapAdd NET_BIND_SERVICE only | PASS |
| no-new-privileges | PASS |
| Network | host, as designed |
| Port 2019 | no listener, PASS |
| Lightsail firewall: 80 and 443 open, 22 restricted to the authorised address, no rule for 2019 | PASS |
| `/healthz` | HTTP 200, PASS |
| Unknown route | HTTP 404, PASS |
| HTTP to HTTPS | HTTP 308, PASS |
| Unsigned webhook | HTTP 400 (reached the relay, refused as unsigned), PASS |

Scope: host-level hardening only. **Not claimed:** end-to-end webhook integration, the Tushe signing secret (not issued yet; Tushe generates it, A94), or the G1 gate.

## Pre-work for the next agent
- Exact next step: when Tushe issues the signing secret, write it to the relay's volume file only (see `deploy/relay-host/README.md`) and test one signed delivery and one replay of the same `webhook-id`.
- Open questions (owner): the signing secret's format and rotation announcement (Tushe, written reply to item 1 of the address message).
- Files to read first: `deploy/relay-host/README.md`, `deploy/relay-host/setup.sh`, the evidence comment on noema#189.
- Do not touch: the real hostname, the server address or any token in any file or issue (DEC-2026-11-05 D5).
