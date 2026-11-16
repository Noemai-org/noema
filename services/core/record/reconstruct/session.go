package reconstruct

import (
	"net/http"

	"github.com/Noemai-org/noema/services/core/edge/session"
)

// kindOfRole is the principal kind (PERD 32.1) a session role stands for. Only ops and audit have one that may read a reconstruction; any other
// role is carried under its own name, which is not a staff kind, so mayRead refuses it.
var kindOfRole = map[session.Role]string{session.RoleOps: "operator", session.RoleAudit: "auditor"}

// SessionAuthorizer is the Authorizer of the running core (EDG-01, REC-03): the caller is the principal the edge's session check put in the
// request's context, and nothing else names one (INV-12, AGENT-10). The tenant is the token's tenant; the principal is "<kind>:<sub>" with the
// kind taken from the role, so a session whose subject is not a valid principal ID is refused by the handler like any other non-staff caller.
// Mount the handler behind session.Middleware and session.RequireRole(ops, audit); the handler does not rely on that gate for the role.
type SessionAuthorizer struct{}

var _ Authorizer = SessionAuthorizer{}

// Authenticate returns the caller of an authenticated request, or ErrUnauthenticated when the request carries no session principal.
func (SessionAuthorizer) Authenticate(r *http.Request) (Caller, error) {
	p, ok := session.FromContext(r.Context())
	if !ok || p.Tenant == "" || p.ID == "" {
		return Caller{}, ErrUnauthenticated
	}
	kind, ok := kindOfRole[p.Role]
	if !ok {
		kind = string(p.Role)
	}
	return Caller{TenantID: p.Tenant, Principal: kind + ":" + p.ID, Roles: []string{string(p.Role)}}, nil
}
