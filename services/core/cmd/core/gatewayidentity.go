package main

import (
	"net/http"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
)

// gatewayidentity.go: the one place the Input Gateway's Identity is made (EDG-01 slice 3c; INV-12, AGENT-10). Every member comes from the verified session in the request
// context, which only the edge's session middleware sets; nothing is read from the query, the headers or the body (the body guard refuses a body that names one). It holds no
// product logic and it does not mount the route: what POST /v1/conversations answers is open (noema#601).

// rolesCustomer is who may hold a conversation: a customer. An ops, audit or approver session gets 404 from the role gate and the handler never runs.
var rolesCustomer = []session.Role{session.RoleCustomer}

// gatewayIdentity is the identity of the authenticated session of r, or false when r did not pass the session middleware. The session reference is the session's AuthnRef
// (`ses_` and 32 hex characters of a hash of the token's key id, subject and issue time), never the token itself.
func gatewayIdentity(r *http.Request) (inputgateway.Identity, bool) {
	p, ok := session.FromContext(r.Context())
	if !ok {
		return inputgateway.Identity{}, false
	}
	return inputgateway.Identity{TenantID: p.Tenant, PrincipalID: p.ID, PrincipalKind: p.PrincipalType, SessionID: p.AuthnRef}, true
}
