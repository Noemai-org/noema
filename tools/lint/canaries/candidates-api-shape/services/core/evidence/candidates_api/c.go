// Package candidates_api is a canary: a response carries a field outside the masked candidate shape.
package candidates_api

// Candidate carries a full account number, which the masked shape forbids.
type Candidate struct {
	CandidateID       string
	AmountMinor       int64
	AccountNumberFull string
}

// Response lists candidates.
type Response struct {
	Candidates []Candidate
}
