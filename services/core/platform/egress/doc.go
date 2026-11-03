// Package egress is the callback safety guard: it decides which URLs a signed outcome.updated callback may be sent to, and it is the
// only way the core's callback delivery opens a connection.
//
// Implements: REQ-SEC-009 (egress control and callback safety), REQ-API-006 (the signed outbound event it protects); TM-25 (callback
// server-side request forgery). PERD 39.4 "Callback registration safety".
//
// It lives in platform/ because it is a network-safety capability, not decision code; outcome/callbacks (OUT-05) imports it.
//
// Rules, each with a test in egress_test.go:
//
//   - A callback URL is HTTPS on port 443, has no credentials, no fragment and no control characters, and its host is a DNS name with
//     at least two labels and an alphabetic top-level label. An IP literal, in any spelling, is refused, and so is a single-label name
//     such as localhost.
//   - At registration the host must resolve, and EVERY answer must be a public unicast address. One private answer among public ones
//     refuses the host.
//   - At every delivery the host must be registered, is resolved again, and every answer is checked again (DNS rebinding). The
//     connection is made to the address that was checked, never to a second lookup of the name, and the dial itself re-checks the
//     remote address.
//   - A redirect is never followed, the environment's proxy settings are ignored, and certificates are verified with TLS 1.2 or later.
//
// Every refusal is ErrEgressDenied, which the delivery code records as CALLBACK_FAILED{reason: EGRESS_DENIED} and raises as an ops
// alert (REQ-SEC-009). Nothing falls back to a direct connection.
//
// Must never: connect to a host that is not registered; connect to a non-public address; follow a redirect; use a proxy from the
// environment; log a resolved internal address.
package egress
