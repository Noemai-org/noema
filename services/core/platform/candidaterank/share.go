package candidaterank

import "errors"

// ErrShare is returned by ShareBP for a share that is not in [0, 1].
var ErrShare = errors.New("candidaterank: not a share of one or less")

// WrittenBy is the identity that appends CANDIDATES_RESOLVED (A725 stub for the red run).
const WrittenBy = ""

// EventType is the Record event type of the payload (A725 stub for the red run).
const EventType = ""

// ShareBP is a stub for the red run.
func ShareBP(numerator, denominator int64) (int64, error) { return 0, nil }
