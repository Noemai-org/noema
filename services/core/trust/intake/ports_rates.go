package intake

import "context"

// The port of the per-principal rate check inside T11 (REQ-TRU-012; PERD 23.3 T11, REJECT RATE_LIMITED). It is defined here, by its consumer, so Trust imports no execution or pack
// package (INV-01, SoT 51). The adapter owns the limit (the capability descriptor's limits.rate_per_principal_per_hour, pack data) and the count; Trust holds no number.

// RateQuery asks whether one principal has used up the rate of one capability. The principal is the authenticated session's, never a value of the intent (INV-12).
type RateQuery struct {
	TenantID     string
	PrincipalID  string
	CapabilityID string
}

// Rates answers the rate question for a write (the optional field Service.Rates). A capability that declares no limit is not limited. An error is an error, never "not exceeded": T11 fails closed on it.
type Rates interface {
	Exceeded(ctx context.Context, q RateQuery) (bool, error)
}
