package candidaterank

// AuditPayload is the payload of the CANDIDATES_RESOLVED Record event (PERD 32.2; REQ-AMB-002: "IDs and scores, masked"; REQ-AMB-003: "Scores and feature values recorded"). It is a
// typed value so that nothing but the keys below can reach the Record from the ranker: identifiers, scores in basis points and the ranking's own result. It has no name, no amount, no
// time, no account digit and no direction of a candidate (AGENT-13, A676), and every number in it is an integer, as PERD 32.1 requires of a payload; a score is whole basis points
// (floor division, A560), so it is exact. Two resolutions that are equal give equal bytes (INV-21).
//
// The ranker writes nothing: it makes this value, and the caller that appends the event (the Record's appender, as the identity the Architect names) marshals it.
type AuditPayload struct {
	State          string   `json:"state"`
	WhyNot         []string `json:"why_not"`
	CandidateCount int      `json:"candidate_count"`
	AboveFloor     int      `json:"above_floor"`
	TopBP          int64    `json:"top_bp"`
	SecondBP       int64    `json:"second_bp"`
	MarginBP       int64    `json:"margin_bp"`
	// ResolvedCandidateID is named only when the ranking resolved on its own (AUTO_RESOLVED); in every other state it is left out.
	ResolvedCandidateID string           `json:"resolved_candidate_id,omitempty"`
	Candidates          []AuditCandidate `json:"candidates"`
}

// AuditCandidate is one candidate's identifier, reported score and the five feature values it was made from, all in basis points, in the ranked order.
type AuditCandidate struct {
	CandidateID string        `json:"candidate_id"`
	ScoreBP     int64         `json:"score_bp"`
	Features    AuditFeatures `json:"features"`
}

// AuditFeatures are the five feature values of one candidate in basis points (10000 is a full match).
type AuditFeatures struct {
	AmountBP        int64 `json:"amount_bp"`
	RecipientBP     int64 `json:"recipient_bp"`
	TemporalBP      int64 `json:"temporal_bp"`
	RelationshipBP  int64 `json:"relationship_bp"`
	TypeDirectionBP int64 `json:"type_direction_bp"`
}

// AuditPayload is a stub for the red run: it returns the zero payload, so the tests that assert its content fail on their assertions and not on the build.
func (r Resolution) AuditPayload() AuditPayload { return AuditPayload{} }
