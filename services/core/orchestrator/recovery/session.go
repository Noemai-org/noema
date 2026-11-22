package recovery

import (
	"errors"
	"regexp"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/orchestrator/investigations"
)

// recoveryText is the turn text of a recovery. ResumeWrite requires a non-empty text and never reads it again; no stored text exists, and a request's must not be used.
const recoveryText = "recovery"

var plainRe = regexp.MustCompile(`\A[^\s\x00-\x1f\x7f]+\z`)

// NewRecoverySession is the one place that builds a session for a recovery (STUB: red commit).
func NewRecoverySession(c Candidate, rs investigations.RecordedSession) (orchestrator.Session, error) {
	if !tenantRe.MatchString(c.TenantID) || !plainRe.MatchString(c.PrincipalRef) {
		return orchestrator.Session{}, errors.New("recovery: the row names no valid tenant and principal")
	}
	return orchestrator.Session{TenantID: c.TenantID, PrincipalID: c.PrincipalRef}, nil
}
