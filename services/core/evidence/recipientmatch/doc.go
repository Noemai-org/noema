// Package recipientmatch is the recipient matcher of core (A710; A695 and OD-P-117 approve its dependency and its manifest kind; REQ-AMB-003, REQ-CTX-006; INV-01, INV-14, INV-21; AGENT-13, AGENT-16).
//
// It scores how well a counterparty's name matches what the customer said, as an exact share in [0, 1], and returns only the share: the Candidates read takes it through its port
// (candidates_api.RecipientMatcher), so no name leaves core (A676). It reproduces the Python reference matcher exactly: the same tokens, the same tiers (exact, folded, alias, phonetic, initial), the
// same best one-to-one assignment, the same exact fractions in lowest terms, checked by the shared golden vectors (contracts/fixtures/recipient_match_vectors.json).
//
// Every piece of name knowledge is pack data: the tiers' scores, the folding map, the nickname and spelling-variant tables and the phonetic rules come from the data file the manifest pins (kind
// recipient_matcher). This package holds no name, no industry term and no number of its own (INV-14, AGENT-16).
//
// The phonetic tier is the voice path's. The request has no phonetic member, so the reader's port scores with the tier off: for that tier the port reports no match. Score takes the flag so the
// tier is implemented and tested against the reference; turning it on from a request is a contract question for the Architect.
//
// May import: the standard library, golang.org/x/text (unicode normalisation and case folding), evidence/candidates_api (the port's own Match type).
//
// Must never: read a file, the clock or the network; return, log or put in an error any part of a name; hold a default for a number.
package recipientmatch
