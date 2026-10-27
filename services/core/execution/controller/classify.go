package controller

// Classify is E10: the transport result decides the execution state, and silence is never success or failure.
//
//	2xx with an external ack            -> ACKNOWLEDGED
//	409 or 412 version mismatch         -> REJECTED_STALE_VERSION
//	409 duplicate, same key             -> ACKNOWLEDGED, the ack is the existing reference
//	4xx validation or business reject   -> REJECTED_BY_TARGET
//	create refused, subject already has a case (DUPLICATE_CREATE) -> REJECTED_BY_TARGET (a business rejection, not a stale version)
//	timeout, reset, 5xx, no response    -> OUTCOME_UNKNOWN
//	adapter-detected non-retryable defect -> FAILED_TERMINAL
//
// A 2xx without an acknowledgement, and any class this table does not list, is OUTCOME_UNKNOWN: an unknown
// answer is never read as a success (REQ-EXE-005, REQ-EXE-007, INV-07).
func Classify(r TransportResult) ExecutionState {
	switch r.Class {
	case TransportAck2xx, TransportDuplicate409:
		if r.ExternalAck == "" {
			return StateOutcomeUnknown
		}
		return StateAcknowledged
	case TransportVersionConflict:
		return StateRejectedStaleVersion
	case TransportRejected4xx, TransportDuplicateCreate:
		return StateRejectedByTarget
	case TransportDefect:
		return StateFailedTerminal
	case TransportTimeout, TransportConnectionReset, TransportServer5xx, TransportNoResponse:
		return StateOutcomeUnknown
	}
	return StateOutcomeUnknown
}
