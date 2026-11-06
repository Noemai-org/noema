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
		if got := layer.ClassifyError(c.err); got != c.want {
			t.Errorf("%s: class = %s, want %s", name, got, c.want)
		}
	}
	// A wrapped read error is still the read error.
	wrapped := errors.Join(errors.New("context"), &adapters.ReadError{Failure: adapters.ReadUnavailable, RawCode: "503"})
	if got := layer.ClassifyError(wrapped); got != layer.ReadServerError {
		t.Errorf("a wrapped 503: class = %s", got)
	}
}

// timeoutErr is a net.Error that reports a timeout, as the transport returns when a connection or read times out.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }
