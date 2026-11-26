package adapters

import (
	"context"
	"encoding/json"
	"time"
)

// The two Tushe reads that delta v1.3 (the addendum of 15 Nov 2026, adopted by ruling A248; interim, A310) names field by field: the payout transition log and the event feed. These are
// the neutral types and interfaces; the client is in synthetic_tushe. SYNTHETIC test environment (AGENT-25). Nothing here asserts Tushe's behaviour: the fields are the ones the delta lists,
// and what the delta does not say (the envelope of a list, the type of a field, what a state means) is a parameter or left raw, never guessed.

// PayoutTransition is one row of a payout's transition log, oldest first (delta v1.3 section 3). States are Tushe's own strings, kept raw: the meaning of a state is not in any governing
// document, so none is mapped here (a state outside a known set is UNKNOWN to the caller, never a guess).
type PayoutTransition struct {
	Seq            int64
	Lifecycle      string
	FromState      string // empty when the row has none (the first transition): the delta does not say; the caller's fold decides what an empty from-state means
	ToState        string
	Source         string
	SourceRef      string
	Inferred       bool
	Injected       bool
	Reason         string
	OccurredAt     time.Time
	ObservedAt     time.Time
	Actor          string
	RequestID      string
	IdempotencyKey string
}

// TransitionPage is the whole transition log of one payout as one read returned it, with the hash of the bytes received.
type TransitionPage struct {
	Items   []PayoutTransition
	RawHash string
}

// TransitionLog is the read of a payout's transition log. A failure is a *ReadError: UNKNOWN and UNAVAILABLE are real answers, never "no transitions".
type TransitionLog interface {
	PayoutTransitions(ctx context.Context, accountID, payoutID string) (TransitionPage, error)
}

// FeedEvent is one event of the feed. The delta names `id`, `sequence` and `traceId`; every other member is kept in Raw untouched.
type FeedEvent struct {
	ID       string
	Sequence int64
	TraceID  string
	Raw      json.RawMessage
}

// FeedPage is one page of the event feed read forward from a cursor.
type FeedPage struct {
	Items   []FeedEvent
	RawHash string
}

// EventFeed reads the feed forward on `sequence`, never on a timestamp (two events in the same millisecond have no defined order, delta v1.3 section 3).
type EventFeed interface {
	// EventsAfter reads one page after the sequence `after` (limit at most the feed's own page size).
	EventsAfter(ctx context.Context, after int64, limit int) (FeedPage, error)
	// ReadFeed reads forward from `after` until a page is empty (the feed is caught up), following the sequence of the last event, for at most maxPages pages. A page that does not advance
	// the sequence, a repeated or lower sequence, or the page limit reached is an error, never a silent truncation.
	ReadFeed(ctx context.Context, after int64, limit, maxPages int) ([]FeedEvent, error)
}

// LifecycleStates folds a payout's transition log into the current state of each lifecycle: the toState of the last row of that lifecycle (delta v1.3 section 3: the log is oldest first and
// a state is "fold the transitions and take toState"). Lifecycles are folded on their own and never from one another: Tushe's response of 7 Oct 2026 section 4.1 says the three lifecycles are
// written independently and not synchronised, so a lifecycle with no row has no entry rather than a state borrowed from another. The names and the states are Tushe's own strings, kept raw
// and mapped to nothing here (A310). A log that is not strictly increasing on seq, or a row with no lifecycle or no toState, is MALFORMED and yields no states, never a partial fold.
// An empty log yields no states and no error: what "no log" means is the caller's to decide.
func LifecycleStates(p TransitionPage) (map[string]string, error) {
	out := map[string]string{}
	var last int64
	for i, row := range p.Items {
		if row.Seq <= 0 || (i > 0 && row.Seq <= last) || row.Lifecycle == "" || row.ToState == "" {
			return nil, &ReadError{Failure: ReadMalformed, RawCode: "TRANSITION_LOG_ROW", RawHash: p.RawHash}
		}
		last = row.Seq
		out[row.Lifecycle] = row.ToState
	}
	return out, nil
}
