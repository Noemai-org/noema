package layer

import (
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// ReadClass is what the adapter and the Evidence Layer know about how one read ended (PERD §21.1).
type ReadClass string

const (
	ReadOK          ReadClass = "OK"
	ReadTimeout     ReadClass = "TIMEOUT"
	ReadServerError ReadClass = "SERVER_ERROR"
	ReadRateLimited ReadClass = "RATE_LIMITED_BEYOND_BUDGET"
	ReadCircuitOpen ReadClass = "CIRCUIT_OPEN"
	ReadNotFound    ReadClass = "NOT_FOUND"
	ReadMalformed   ReadClass = "MALFORMED"
	ReadOutOfScope  ReadClass = "OUT_OF_SCOPE"
)

// Read is one read as the status function sees it.
type Read struct {
	Class       ReadClass
	Terminal    bool      // a NOT_FOUND that the source marked terminal (recorded for the item; it never makes absence KNOWN)
	Determinate bool      // an OK read whose value can be determined (not an unknown enum, not "pending" without a terminal flag)
	ReadAt      time.Time // retrieval time, on the clock interface
}

// Authority is the source's authority for the fact, from the pack's evidence-source descriptor, never from adapter code.
type Authority string

const (
	AuthAuthoritative Authority = "AUTHORITATIVE"
	AuthCorroborating Authority = "CORROBORATING"
	AuthNone          Authority = "NONE" // a customer's own claim
)

// Source is the pack's descriptor for the source of one fact.
type Source struct {
	Authority    Authority
	SourceLimit  time.Duration // freshness_sla_seconds
	ProfileLimit time.Duration // the trust profile's max_evidence_age_seconds; zero means the profile sets none
}

// AssignStatus is the FactStatus of one read (REQ-CTX-002). It is pure and total: it reads no clock, never panics, and an
// unhandled case is UNKNOWN. It never returns INFERRED, AMBIGUOUS or CONTRADICTORY (those need more than one item) and never
// turns absence into a negative fact (INV-19).
func AssignStatus(r Read, s Source, now time.Time) contracts.EnumsV1FactStatus {
	switch r.Class {
	case ReadOutOfScope:
		return contracts.EnumsV1FactStatusUNAUTHORIZED
	case ReadTimeout, ReadServerError, ReadRateLimited, ReadCircuitOpen:
		return contracts.EnumsV1FactStatusUNAVAILABLE
	case ReadMalformed:
		return contracts.EnumsV1FactStatusUNKNOWN
	case ReadNotFound:
		// Absence is never a negative fact (INV-19). PERD §21.1 allows one exception, a source that declares AUTHORITATIVE_NEGATIVE,
		// but evidence-item.v1 requires a missing record to be UNAVAILABLE or UNKNOWN and evidence-source.v1 has no field with which
		// to declare it, and the POC fixtures declare none. So a not-found is UNKNOWN whatever the source and the terminal flag say
		// (the record is absent where absence is not meaningful). The exception needs a contract change first (AGENT-15).
		return contracts.EnumsV1FactStatusUNKNOWN
	case ReadOK:
		if !r.Determinate {
			return contracts.EnumsV1FactStatusUNKNOWN
		}
	default:
		return contracts.EnumsV1FactStatusUNKNOWN
	}
	if s.Authority != AuthAuthoritative {
		return contracts.EnumsV1FactStatusUNKNOWN // corroboration and a customer's claim never confer KNOWN on their own
	}
	limit, ok := effectiveLimit(s)
	if !ok || r.ReadAt.After(now) {
		return contracts.EnumsV1FactStatusUNKNOWN
	}
	if now.Sub(r.ReadAt) > limit {
		return contracts.EnumsV1FactStatusSTALE
	}
	return contracts.EnumsV1FactStatusKNOWN
}

// effectiveLimit is the stricter of the source's limit and the profile's (SoT §43.5, INV-23): a profile can only tighten. A source
// limit that is zero or negative, or a negative profile limit, is no usable limit.
func effectiveLimit(s Source) (time.Duration, bool) {
	if s.SourceLimit <= 0 || s.ProfileLimit < 0 {
		return 0, false
	}
	if s.ProfileLimit > 0 && s.ProfileLimit < s.SourceLimit {
		return s.ProfileLimit, true
	}
	return s.SourceLimit, true
}
