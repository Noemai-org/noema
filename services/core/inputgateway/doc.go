// Package inputgateway is the core's side of the Input Gateway (UND-02, ruling A161): it takes one request body and the identity of the authenticated session,
// writes INPUT_RECEIVED to the Record before anything else happens, deduplicates a repeated request_id, refuses a reused one, and hands the orchestrator the turn
// with the reference of the recorded event.
//
// Implements: REQ-INP-001 (a closed body, nothing but what a client can say), REQ-INP-002 and INV-12 (identity is an argument taken from the session, never
// read from the body, the query or a header), REQ-INP-003 (a repeated request_id within the window returns the original result; the same id with another body
// is REQUEST_ID_REUSED), REQ-AUD-001 and EC-E16 (the Record comes first: if an append fails the input is refused and nothing proceeds), INV-11.
//
// May import: record/chain (to append as core-edge), platform/crypto, internal/contracts (the error codes), platform/clock. It reads no pack file, holds no
// credential beyond the pools it is given (the core-edge emitter login and the tenant's record_read login), and normalises no text: the text rules (NFC,
// trim, 2,000 characters, is_final) are the model service's gateway (services/model, UND-02), which the orchestrator calls with the body this package hands over.
//
// Must never: take an identity from the request; start a turn whose INPUT_RECEIVED is not in the Record; answer a retry as if it had been recorded when its
// INPUT_DEDUPLICATED could not be appended; put the text of an input in a Record event, an error or a log line (AGENT-13).
package inputgateway
