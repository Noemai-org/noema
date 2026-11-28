// Package opslistener is the ops listener of the tenant kill switch control plane (EXE-06; REQ-EXE-008, REQ-OPS-002, REQ-SEC-006; INV-03, INV-11, INV-12; rulings A666 and A699,
// A710; ruling A71, interim, relayed, not yet committed). It is the one door through which a person sets or clears a tenant's switch.
//
// The door is mutual TLS. A client must present a certificate that chains to the operator CA file the listener is given (a required setting, no default), is valid now by the
// injected clock, and is for client authentication. The only identity the listener ever has is that verified certificate: its common name becomes the principal of a
// controlplane.Caller with the admin role, the same way for every request. Headers, the query string and a body never name a person or a role (INV-12, AGENT-10); a request with
// a body is refused. The verification role (RoleVerify) is not produced here at all.
//
// The two routes, POST only:
//
//	/v1/ops/tenants/{tenant_id}/kill-switch:set
//	/v1/ops/tenants/{tenant_id}/kill-switch:clear
//
// Each calls the merged controlplane.Switch, which changes the switch and appends its Record events in one transaction on the tenant's own admin emitter login (migration 0042).
// This package holds no database login: the Switch it is given holds it, and only the process that builds this listener may mount it (A699).
package opslistener
