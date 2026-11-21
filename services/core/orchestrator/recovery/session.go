package recovery

import (
	"errors"
	"regexp"

	"github.com/Noemai-org/noema/services/core/orchestrator"
)

// recoveryText is the turn text of a recovery. ResumeWrite requires a non-empty text and never reads it again; no stored text exists, and a request's must not be used.
const recoveryText = "recovery"

var plainRe = regexp.MustCompile(`\A[^\s\x00-\x1f\x7f]+\z`)

// NewRecoverySession is the one place that builds a session for a recovery, from the stored investigation row alone: its tenant and the principal that started it
// (INV-12, AGENT-10, A149). Role, AuthnRef, PrincipalType and AssertedBy are the edge's and stay empty: nothing is borrowed from a request or invented, so Trust's
// technical-authority step refuses any decision a recovery would need to make afresh.
func NewRecoverySession(c Candidate) (orchestrator.Session, error) {
	if !tenantRe.MatchString(c.TenantID) || !plainRe.MatchString(c.PrincipalRef) {
		return orchestrator.Session{}, errors.New("recovery: the row names no valid tenant and principal")
	}
	return orchestrator.Session{TenantID: c.TenantID, PrincipalID: c.PrincipalRef}, nil
}
