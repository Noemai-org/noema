package clocks

import (
	"errors"
	"fmt"
	"math"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// Phase is where a clock stands (PERD 23.5).
type Phase string

const (
	NotStarted Phase = "NOT_STARTED"
	Running    Phase = "RUNNING"
	AtRisk     Phase = "AT_RISK"
	Breached   Phase = "BREACHED"
)

// ErrInvalid is returned for inputs that cannot be evaluated: the caller turns it into UNKNOWN (INV-16). Nothing is guessed.
var ErrInvalid = errors.New("clocks: invalid input")

// Ratio is a fraction of the clock's duration, as integers (INV-21). The at-risk fraction comes from configuration; this package
// holds no threshold (REQ-OPS-003).
type Ratio struct {
	Num, Den int64
}

// Status is the result of EVALUATE_SLA_STATUS (REQ-TRU-006). Times are Unix seconds.
type Status struct {
	ClockID    contracts.CommonV1PackId
	Label      contracts.ClockV1Label
	StartedAt  *int64
	DeadlineAt *int64
	Elapsed    int64
	Remaining  *int64
	Phase      Phase
}

// Evaluate derives the status of one clock at now. anchor is the time of the clock's anchor event, or nil when the event has not
// happened: that is NOT_STARTED, never assumed. AT_RISK means remaining is strictly below the at-risk fraction of the duration;
// a deadline reached or passed is BREACHED with nothing remaining. It reads no clock and calls nothing (INV-21).
func Evaluate(c contracts.ClockV1, anchor *int64, now int64, atRisk Ratio) (Status, error) {
	st := Status{ClockID: c.ClockId, Label: c.Label, Phase: NotStarted}
	if c.Label != contracts.ClockV1LabelSYNTHETIC && c.Label != contracts.ClockV1LabelVALIDATED {
		return Status{}, fmt.Errorf("%w: clock %s has label %q", ErrInvalid, c.ClockId, c.Label)
	}
	if c.DurationSeconds <= 0 {
		return Status{}, fmt.Errorf("%w: clock %s has duration %d", ErrInvalid, c.ClockId, c.DurationSeconds)
	}
	if atRisk.Den <= 0 || atRisk.Num < 0 || atRisk.Num > atRisk.Den {
		return Status{}, fmt.Errorf("%w: at-risk fraction %d/%d", ErrInvalid, atRisk.Num, atRisk.Den)
	}
	if anchor == nil {
		return st, nil
	}
	if *anchor > now {
		return Status{}, fmt.Errorf("%w: clock %s anchor %d is after now %d", ErrInvalid, c.ClockId, *anchor, now)
	}
	if *anchor < 0 || *anchor > math.MaxInt64-c.DurationSeconds || c.DurationSeconds > math.MaxInt64/atRisk.Den {
		return Status{}, fmt.Errorf("%w: clock %s anchor or duration out of range", ErrInvalid, c.ClockId)
	}
	started := *anchor
	deadline := started + c.DurationSeconds
	st.StartedAt, st.DeadlineAt = &started, &deadline
	st.Elapsed = now - started
	remaining := deadline - now
	if remaining <= 0 {
		remaining = 0
		st.Phase = Breached
		st.Remaining = &remaining
		return st, nil
	}
	st.Remaining = &remaining
	st.Phase = Running
	if remaining*atRisk.Den < c.DurationSeconds*atRisk.Num {
		st.Phase = AtRisk
	}
	return st, nil
}
