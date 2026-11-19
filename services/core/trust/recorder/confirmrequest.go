package recorder

// NewConfirmationRequest is the confirmation request a CONFIRM decision creates (SoT 43.2a, REQ-TRU-009). The first eight values are the binding; the writer computes the binding
// hash from them with trust/confirm.BindingHash and mints the cfr_ ID itself, so neither is ever taken from the caller. Requirement is the contract's approver requirement (SELF,
// MAKER_CHECKER or ROLE:<role>), RequesterID the principal that asked (the admitted intent's, INV-12) and ExpiresAt the expiry in Unix seconds.
type NewConfirmationRequest struct {
	PrincipalID     string
	IntentHash      string
	CapabilityID    string
	TargetRef       string
	ParamsHash      string
	EvidenceSetHash string
	PlanHash        string
	PolicyVersion   string
	Requirement     string
	RequesterID     string
	ExpiresAt       int64
}
