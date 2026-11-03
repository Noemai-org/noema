# outcome/callbacks/egress

Date: 2026-11-03. Task: SEC-05 (SR+AG). Requirements: REQ-SEC-009, REQ-API-006 (the signed event it protects). Threat: TM-25. Status: built; used by OUT-05 (C1), not yet wired.

The callback safety guard. It decides which URLs a signed `outcome.updated` callback may go to, and it is the only thing the delivery code uses to open a connection. Standard library only.

## Use (for OUT-05)

```go
g := egress.NewGuard(nil)                         // nil: the system resolver
err := g.Register(ctx, "https://cb.partner.example/hook") // admin API: PUT /v1/admin/tenants/{id}/callback
client := g.Client(15 * time.Second)              // the only client that delivers; never follows a redirect
addr, err := g.Resolve(ctx, url)                  // before every delivery: registered, re-resolved, all answers public
```

A refusal is `egress.ErrEgressDenied` (`errors.Is`). The caller records `CALLBACK_FAILED{reason: EGRESS_DENIED}` and raises an ops alert; it never retries over another path. A redirect comes back as a 3xx response and is a failed delivery.

## What it enforces

HTTPS on port 443; no credentials, fragment, control character or non-ASCII; a DNS name of at least two labels with an alphabetic top-level label, so no IP literal or numeric spelling (`2130706433`, `0x7f.1`, `127.1`) gets through; at registration every DNS answer public; at every delivery the host registered and re-resolved with every answer public; the connection made to the address that was checked, and the remote address checked again in the dialer; IPv4-mapped, NAT64, 6to4, Teredo, unique-local, link-local, documentation, CGNAT, loopback, multicast and unspecified addresses all refused; no environment proxy; TLS 1.2 or later with verification; a new connection per delivery.

## Tests

`go test ./outcome/callbacks/egress/` (add `-race`). No test uses the network: the resolver and the dial function are injected. The suite was checked by mutation: fifteen deliberate weakenings of the code (a dropped range, a skipped check, a followed redirect, an environment proxy, dialling the name, accepting `http`, ...) are each caught by a test.

## Not covered

An egress **proxy** at the network layer (REQ-SEC-009 also asks for one): this package is the in-process half; the network half is the Compose policy (`deploy/compose/netcheck`, SEC-11). A real delivery over TLS to a public host is not tested here (CI has no public host); OUT-05's delivery test against the reference listener (`benchmark/partner_listener`) covers it. HMAC signing and the 5-minute replay window are REQ-API-006 and belong to OUT-05.
