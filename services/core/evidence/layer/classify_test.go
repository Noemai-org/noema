package layer_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
)

// REQ-CTX-002, INV-19: every way an adapter read can end maps to a read class, and an error nobody foresaw is UNKNOWN, never KNOWN.
// The faults come from real FaultPlans in execution/e2e (AGENT-12); this table pins the mapping itself.
func TestClassifyErrorMapsEveryAdapterFailure(t *testing.T) {
	for _, f := range classifierFailures(layer.ClassifyError) {
		t.Error(f)
	}
}

// classifierFailures runs the mapping table against a classifier and returns every disagreement. The real classifier must produce none; the
// mutants of TestTheClassifierTableCatchesEveryMutant must each produce at least one.
func classifierFailures(classify func(error) layer.ReadClass) []string {
	var failures []string
	re := func(f adapters.ReadFailure, raw string, after time.Duration) error {
		return &adapters.ReadError{Failure: f, RawCode: raw, RetryAfter: after}
	}
	cases := map[string]struct {
		err  error
		want layer.ReadClass
	}{
		"no error":                   {nil, layer.ReadOK},
		"a timeout":                  {re(adapters.ReadUnavailable, "TIMEOUT", 0), layer.ReadTimeout},
		"a 503":                      {re(adapters.ReadUnavailable, "503", 0), layer.ReadServerError},
		"a 500 with a code":          {re(adapters.ReadUnavailable, "500:INTERNAL", 0), layer.ReadServerError},
		"a 429":                      {re(adapters.ReadUnavailable, "429", 2*time.Second), layer.ReadRateLimited},
		"a 429 without Retry-After":  {re(adapters.ReadUnavailable, "429", 0), layer.ReadRateLimited},
		"a reset connection":         {re(adapters.ReadUnavailable, "CONNECTION_RESET", 0), layer.ReadServerError},
		"no response":                {re(adapters.ReadUnavailable, "NO_RESPONSE", 0), layer.ReadServerError},
		"not found":                  {re(adapters.ReadNotFound, "404", 0), layer.ReadNotFound},
		"an invalid query":           {re(adapters.ReadInvalid, "400", 0), layer.ReadMalformed},
		"a refused credential":       {re(adapters.ReadUnauthorized, "403", 0), layer.ReadOutOfScope},
		"a body the adapter rejects": {re(adapters.ReadMalformed, "200", 0), layer.ReadMalformed},
		"a failure nobody foresaw":   {re("SOMETHING_NEW", "?", 0), layer.ReadMalformed},
		"an error that is not one":   {errors.New("boom"), layer.ReadMalformed},
		// A deadline that expires inside a call is a timeout, whether the adapter wrapped it or not (review of noema#174, AGENT-11).
		"a bare context deadline":    {context.DeadlineExceeded, layer.ReadTimeout},
		"a wrapped context deadline": {fmt.Errorf("get case: %w", context.DeadlineExceeded), layer.ReadTimeout},
		"a network timeout":          {timeoutErr{}, layer.ReadTimeout},
	}
	for name, c := range cases {
		if got := classify(c.err); got != c.want {
			failures = append(failures, fmt.Sprintf("%s: class = %s, want %s", name, got, c.want))
		}
	}
	// A wrapped read error is still the read error.
	wrapped := errors.Join(errors.New("context"), &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503"})
	if got := classify(wrapped); got != layer.ReadServerError {
		failures = append(failures, fmt.Sprintf("a wrapped 503: class = %s", got))
	}
	return failures
}

// Mutation check (review of noema#174; this test was NOT written failing-first: the classifier and its table were written together, so the table
// was never seen red). Each mutant is the real classifier with one mapping broken; the table must go red on every one of them. A mutant that
// survives means the table does not pin that mapping.
func TestTheClassifierTableCatchesEveryMutant(t *testing.T) {
	readErr := func(err error) *adapters.ReadError {
		var re *adapters.ReadError
		if errors.As(err, &re) {
			return re
		}
		return nil
	}
	mutants := map[string]func(error) layer.ReadClass{
		"a refused credential is read as OK": func(err error) layer.ReadClass {
			if re := readErr(err); re != nil && re.Failure == adapters.ReadUnauthorized {
				return layer.ReadOK
			}
			return layer.ClassifyError(err)
		},
		"a refused credential is no longer out of scope": func(err error) layer.ReadClass {
			if re := readErr(err); re != nil && re.Failure == adapters.ReadUnauthorized {
				return layer.ReadMalformed
			}
			return layer.ClassifyError(err)
		},
		"a 429 without Retry-After is a server error": func(err error) layer.ReadClass {
			if re := readErr(err); re != nil && re.Failure == adapters.ReadUnavailable && re.RetryAfter == 0 && re.RawCode == "429" {
				return layer.ReadServerError
			}
			return layer.ClassifyError(err)
		},
		"a timeout is read as OK": func(err error) layer.ReadClass {
			if re := readErr(err); re != nil && re.RawCode == "TIMEOUT" {
				return layer.ReadOK
			}
			return layer.ClassifyError(err)
		},
		"not found is read as OK": func(err error) layer.ReadClass {
			if re := readErr(err); re != nil && re.Failure == adapters.ReadNotFound {
				return layer.ReadOK
			}
			return layer.ClassifyError(err)
		},
		"an unforeseen failure is read as OK": func(err error) layer.ReadClass {
			if re := readErr(err); re != nil && re.Failure == "SOMETHING_NEW" {
				return layer.ReadOK
			}
			return layer.ClassifyError(err)
		},
		"a plain error is read as OK": func(err error) layer.ReadClass {
			if err != nil && readErr(err) == nil && !errors.Is(err, context.DeadlineExceeded) {
				if _, isNet := err.(timeoutErr); !isNet {
					return layer.ReadOK
				}
			}
			return layer.ClassifyError(err)
		},
		"a bare context deadline is malformed": func(err error) layer.ReadClass {
			if errors.Is(err, context.DeadlineExceeded) {
				return layer.ReadMalformed
			}
			return layer.ClassifyError(err)
		},
		"a network timeout is malformed": func(err error) layer.ReadClass {
			if _, ok := err.(timeoutErr); ok {
				return layer.ReadMalformed
			}
			return layer.ClassifyError(err)
		},
		"a nil error is not OK": func(err error) layer.ReadClass {
			if err == nil {
				return layer.ReadMalformed
			}
			return layer.ClassifyError(err)
		},
		"a wrapped read error is lost": func(err error) layer.ReadClass {
			if err != nil && readErr(err) != nil && err != error(readErr(err)) {
				return layer.ReadMalformed
			}
			return layer.ClassifyError(err)
		},
	}
	if got := classifierFailures(layer.ClassifyError); len(got) != 0 {
		t.Fatalf("the real classifier already fails its own table: %v", got)
	}
	for name, mutant := range mutants {
		if len(classifierFailures(mutant)) == 0 {
			t.Errorf("mutant survived (the table does not pin it): %s", name)
		}
	}
}

// timeoutErr is a net.Error that reports a timeout, as the transport returns when a connection or read times out.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// REQ-CTX-001, INV-19 (noema#262 gives the adapter error its RawHash): the body hash an adapter error carries, or an explicit "no bytes arrived" when it carries none or
// the error is not the adapter's own (a deadline that expired inside a call). A nil error has neither: an OK read supplies its own response.
func TestBodyOfTakesTheAdaptersHashAndSaysNoBytesWhenThereIsNone(t *testing.T) {
	h := "sha256:" + "ab"
	cases := map[string]struct {
		err      error
		wantHash string
		noBytes  bool
	}{
		"a non-2xx body":                     {&adapters.ReadError{Failure: adapters.ReadNotFound, RawCode: "404", RawHash: h}, h, false},
		"a wrapped adapter error":            {fmt.Errorf("case read: %w", &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503", RawHash: h}), h, false},
		"an adapter timeout (no bytes)":      {&adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "TIMEOUT"}, "", true},
		"a deadline inside a call":           {context.DeadlineExceeded, "", true},
		"an error nobody foresaw (no bytes)": {errors.New("boom"), "", true},
		"a nil error":                        {nil, "", false},
	}
	for name, c := range cases {
		hash, noBytes := layer.BodyOf(c.err)
		if hash != c.wantHash || noBytes != c.noBytes {
			t.Errorf("%s: BodyOf = (%q, %v), want (%q, %v)", name, hash, noBytes, c.wantHash, c.noBytes)
		}
	}
}
