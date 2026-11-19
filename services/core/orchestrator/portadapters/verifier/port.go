// Package verifierport adapts Outcome's verifier to orchestrator.Verifier (STM-03, A204, REQ-VER-001, AGENT-21). The verdict is Outcome's and only Outcome's: the
// port builds the verification request for the grant through a Requests source, asks the verifier, and maps its four verdicts to the contract enum. Anything else,
// a request of another tenant, and every failure is an error, never a verdict. It holds no write path and no credential.
package verifierport

import (
	"context"
	"errors"
	"fmt"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/orchestrator"
	"github.com/Noemai-org/noema/services/core/outcome/verifier"
)

// ErrNotUsable is returned for a port with a missing part, a session without a tenant or principal, or a call without a decision or a grant.
var ErrNotUsable = errors.New("verifierport: the port cannot be used")

// ErrBadAnswer is returned for a verdict outside the four Outcome produces, or a request that is not the session's tenant's.
var ErrBadAnswer = errors.New("verifierport: a part gave an answer outside its contract")

// Verifier is the part of Outcome the port calls.
type Verifier interface {
	Verify(ctx context.Context, r verifier.Request) (verifier.Result, error)
}

// Requests builds the verification request of a grant: the receipt the Execution Controller wrote for it, the pack's outcome definition and the bindings. The
// composition owns the implementation; it reads as the tenant.
type Requests interface {
	For(ctx context.Context, tenantID, grantID string) (verifier.Request, error)
}

// Port implements orchestrator.Verifier.
type Port struct {
	Verifier Verifier
	Requests Requests
}

var _ orchestrator.Verifier = Port{}

// Verify asks Outcome for the verdict of the grant's write, by the grant's key.
func (p Port) Verify(ctx context.Context, s orchestrator.Session, decisionID, grantID string) (contracts.EnumsV1Verdict, error) {
	if p.Verifier == nil || p.Requests == nil || s.TenantID == "" || s.PrincipalID == "" || decisionID == "" || grantID == "" {
		return "", ErrNotUsable
	}
	req, err := p.Requests.For(ctx, s.TenantID, grantID)
	if err != nil {
		return "", fmt.Errorf("verifierport: request for %s: %w", grantID, err)
	}
	if req.TenantID != s.TenantID {
		return "", fmt.Errorf("%w: the request is for another tenant", ErrBadAnswer)
	}
	res, err := p.Verifier.Verify(ctx, req)
	if err != nil {
		return "", fmt.Errorf("verifierport: verify %s: %w", grantID, err)
	}
	switch res.Verdict {
	case verifier.Achieved:
		return contracts.EnumsV1VerdictACHIEVED, nil
	case verifier.NotAchieved:
		return contracts.EnumsV1VerdictNOTACHIEVED, nil
	case verifier.PendingExternal:
		return contracts.EnumsV1VerdictPENDINGEXTERNAL, nil
	case verifier.Unknown:
		return contracts.EnumsV1VerdictUNKNOWN, nil
	}
	return "", fmt.Errorf("%w: verdict %q", ErrBadAnswer, res.Verdict)
}
