package injection

import (
	"context"
	"errors"
)

// ErrRecord means the advisory event could not be recorded: the Record refused it, or no recorder was given. The cause stays reachable with errors.Is and errors.As and is never in
// the message. It never means the read failed: detection is advisory, and the findings are returned with it.
var ErrRecord = errors.New(prefix + "the advisory event could not be recorded")

const prefix = "injection: "

type recordError struct{ cause error }

func (e *recordError) Error() string { return ErrRecord.Error() }
func (e *recordError) Unwrap() []error {
	if e.cause == nil {
		return []error{ErrRecord}
	}
	return []error{ErrRecord, e.cause}
}

// Subject is whose Record the event goes on: the tenant and the correlation of the read, and the object the screened strings belong to (for example the evidence item, evr_...).
// The tenant is also the login's, and the database refuses an event for any other (INV-12).
type Subject struct {
	TenantID      string
	CorrelationID string
	ObjectRef     string
}

// Recorder is the port the caller supplies: it appends SECURITY_EVENT of kind INJECTION_DETECTED for a Result that has a finding.
type Recorder interface {
	Record(ctx context.Context, s Subject, r Result) error
}

// Screener screens the strings of one read and records what it finds. It changes nothing else: it has no evidence status, classification or decision to change, and takes none.
type Screener struct {
	Detector *Detector
	Recorder Recorder
}

// Screen detects and, when there is a finding, records it once. The Result is always the detection's, even when the Record failed (the error is then an ErrRecord): a caller
// must not let that error block or alter the read, and should raise it as an operational alert. With no finding nothing is written. A screener with no detector returns an error and an
// empty Result.
func (s *Screener) Screen(ctx context.Context, sub Subject, fields map[string]string) (Result, error) {
	if s == nil || s.Detector == nil {
		return Result{}, errors.New(prefix + "the screener has no detector")
	}
	res := s.Detector.Scan(fields)
	if len(res.Findings) == 0 {
		return res, nil
	}
	if s.Recorder == nil {
		return res, &recordError{}
	}
	if err := s.Recorder.Record(ctx, sub, res); err != nil {
		return res, &recordError{cause: err}
	}
	return res, nil
}
