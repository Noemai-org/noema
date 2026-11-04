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
	Terminal    bool      // a NOT_FOUND that the source marked terminal
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

// Absence is a source's declared meaning of "not found" for a fact. The POC fixtures declare none.
type Absence string

const (
	AbsenceNone                  Absence = ""
	AbsenceAuthoritativeNegative Absence = "AUTHORITATIVE_NEGATIVE"
)

// Source is the pack's descriptor for the source of one fact.
type Source struct {
	Authority    Authority
	Absence      Absence
	SourceLimit  time.Duration // freshness_sla_seconds
	ProfileLimit time.Duration // the trust profile's max_evidence_age_seconds; zero means the profile sets none
}

// AssignStatus is the FactStatus of one read (REQ-CTX-002). It is pure and total: it reads no clock, never panics, and an
// unhandled case is UNKNOWN. It never returns INFERRED, AMBIGUOUS or CONTRADICTORY (those need more than one item) and never
// turns absence into a negative fact (INV-19).
func AssignStatus(r Read, s Source, now time.Time) contracts.EnumsV1FactStatus {
	return contracts.EnumsV1FactStatusUNKNOWN
}
