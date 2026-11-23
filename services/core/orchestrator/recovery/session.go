package recovery

import (
	"errors"
	"regexp"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// recoveryText is the turn text of a recovery. ResumeWrite requires a non-empty text and never reads it again; no stored text exists, and a request's must not be used.
const recoveryText = "recovery"

var (
	plainRe    = regexp.MustCompile(`\A[^\s\x00-\x1f\x7f]+\z`)
	authnRefRe = regexp.MustCompile(`\Ases_[0-9a-f]{32}\z`) // the SessionId shape of contracts common.v1; the edge writes nothing else
)

// NewRecoverySession is the one place that builds a session for a recovery, from the stored investigation row alone (INV-12, AGENT-10, A149, A490 interim, relayed): its tenant
// and principal, the stored principal_role and authn_ref, and the principal type the edge derives from that role. AssertedBy stays empty, because the verified issuer is not
// stored. Nothing is borrowed from a request, a default or the caller. A role outside the four of ruling A1, an authn reference that is not a session reference, or a stored
// principal that differs from the row's is an error: no role is ever invented.
func NewRecoverySession(c Candidate, rs investigations.RecordedSession) (orchestrator.Session, error) {
	if !tenantRe.MatchString(c.TenantID) || !plainRe.MatchString(c.PrincipalRef) {
		return orchestrator.Session{}, errors.New("recovery: the row names no valid tenant and principal")
	}
	if rs.PrincipalID != c.PrincipalRef || !authnRefRe.MatchString(rs.AuthnRef) {
		return orchestrator.Session{}, errors.New("recovery: the row's stored session is not a session of its principal")
	}
	pt, ok := session.PrincipalTypeOf(rs.PrincipalRole)
	if !ok {
		return orchestrator.Session{}, errors.New("recovery: the stored role is outside the roles of a session")
	}
	return orchestrator.Session{TenantID: c.TenantID, PrincipalID: c.PrincipalRef, Role: rs.PrincipalRole, AuthnRef: rs.AuthnRef, PrincipalType: pt}, nil
}
