package layer

import (
	"context"
	"errors"
	"net"
	"strings"

	"github.com/Noemai-org/noema/services/core/adapters"
)

// ClassifyError maps the error of an adapter read to the ReadClass the status function understands (PERD §21.1, REQ-CTX-002). A nil error
// is an OK read. An unavailable source is UNAVAILABLE whatever the cause; a refused credential is read as out of scope; a failure that is
// not one of the adapter's own is MALFORMED, which is UNKNOWN: nothing here can make a read KNOWN.
func ClassifyError(err error) ReadClass {
	if err == nil {
		return ReadOK
	}
	var re *adapters.ReadError
	if !errors.As(err, &re) {
		// A deadline that expires inside a call, or a network timeout the adapter did not wrap, means the source could not be read in time:
		// UNAVAILABLE, not an unreadable answer.
		var ne net.Error
		if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &ne) && ne.Timeout()) {
			return ReadTimeout
		}
		return ReadMalformed
	}
	switch re.Failure {
	case adapters.ReadUnavailable:
		switch {
		case strings.HasPrefix(re.RawCode, "TIMEOUT"):
			return ReadTimeout
		case strings.HasPrefix(re.RawCode, "429"), re.RetryAfter > 0:
			return ReadRateLimited
		default:
			return ReadServerError
		}
	case adapters.ReadNotFound:
		return ReadNotFound
	case adapters.ReadUnauthorized:
		return ReadOutOfScope
	default: // INVALID, MALFORMED and any failure added later
		return ReadMalformed
	}
}

// BodyOf says what the response bytes of a failed read were: the hash the adapter recorded for the bytes it received (adapters.ReadError.RawHash), or noBytes when none
// arrived (a timeout, a reset, a deadline that expired inside a call, an error the adapter did not produce). A nil error has neither. It is the pair the Builder needs for
// a failed read: exactly one of a body hash and an explicit no-bytes.
func BodyOf(err error) (rawHash string, noBytes bool) {
	if err == nil {
		return "", false
	}
	var re *adapters.ReadError
	if errors.As(err, &re) && re.RawHash != "" {
		return re.RawHash, false
	}
	return "", true
}
