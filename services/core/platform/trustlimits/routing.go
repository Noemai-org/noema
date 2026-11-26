package trustlimits

import (
	"regexp"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// The patterns and bounds of the four optional routing members of trust-limits.v2 (A544, interim). The generated type checks the enums and the shape, and not patterns, minimum list
// lengths or repeated entries, so Load does, as it does for pack_id. A member that is present is a non-empty list: absent means no route, and an empty list is not a way to say it.
var (
	reasonCodeShape = regexp.MustCompile(`\A[a-z][a-z0-9]*\.reason\.[a-z][a-z0-9_]*\z`)
	capabilityShape = regexp.MustCompile(`\A[a-z][a-z0-9]*\.cap\.[a-z][a-z0-9_]*\z`)
	tokenShape      = regexp.MustCompile(`\A[a-z][a-z0-9_]*\z`)
)

const (
	maxClassifications = 12
	maxFreezeCaps      = 32
	maxPhases          = 4
	maxAnchors         = 16
)

// checkRouting refuses a routing member that is not exactly the contract: an empty or over-long list, a code or token off its pattern, or two entries for one classification, phase or
// event. It never reads a value beyond that: what the codes mean is the pack's, checked by packcheck against the pack's own catalogue.
func checkRouting(d contracts.TrustLimitsV2) *Error {
	if l := d.EscalationReasons; l != nil {
		if len(*l) == 0 || len(*l) > maxClassifications {
			return refuse("escalation_reasons is not a list of one to twelve entries", nil)
		}
		seen := map[contracts.EnumsV1TransactionClassification]bool{}
		for _, e := range *l {
			if !reasonCodeShape.MatchString(e.ReasonCode) || seen[e.Classification] {
				return refuse("escalation_reasons is not exactly the contract", nil)
			}
			seen[e.Classification] = true
		}
	}
	if l := d.AllowedUnderFreeze; l != nil {
		if len(*l) == 0 || len(*l) > maxFreezeCaps {
			return refuse("allowed_under_freeze is not a list of one to thirty-two entries", nil)
		}
		seen := map[string]bool{}
		for _, id := range *l {
			if !capabilityShape.MatchString(id) || seen[id] {
				return refuse("allowed_under_freeze is not exactly the contract", nil)
			}
			seen[id] = true
		}
	}
	if l := d.PhaseRoutes; l != nil {
		if len(*l) == 0 || len(*l) > maxPhases {
			return refuse("phase_routes is not a list of one to four entries", nil)
		}
		seen := map[contracts.TrustLimitsV2PhaseRoutesItemPhase]bool{}
		for _, e := range *l {
			if !reasonCodeShape.MatchString(e.ReasonCode) || seen[e.Phase] {
				return refuse("phase_routes is not exactly the contract", nil)
			}
			seen[e.Phase] = true
		}
	}
	if l := d.AnchorEntities; l != nil {
		if len(*l) == 0 || len(*l) > maxAnchors {
			return refuse("anchor_entities is not a list of one to sixteen entries", nil)
		}
		seen := map[string]bool{}
		for _, e := range *l {
			if !tokenShape.MatchString(e.Event) || !tokenShape.MatchString(e.Entity) || !tokenShape.MatchString(e.Field) || seen[e.Event] {
				return refuse("anchor_entities is not exactly the contract", nil)
			}
			seen[e.Event] = true
		}
	}
	return nil
}
