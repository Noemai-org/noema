package inputgateway_test

// A184, the client's request ID (REQ-INP-002, REQ-INP-003; AGENT-13): the request ID is a lower-case UUID the client chooses, and it is written to the Record (INPUT_RECEIVED,
// INPUT_DEDUPLICATED, REQUEST_ID_REUSED payloads). A UUID has 32 hex characters; one with none of the letters a to f is 32 digits, which is a card-number-sized run that the
// Record's digit-run rule (ruling A184; noema#536 exempts a UUID only when it holds a letter) would refuse at the append, after the input was accepted. The gateway refuses
// it at the door, as a body that is not valid, before anything is recorded or started. A UUID with at least one letter is not refused here. What a single letter does not
// stop (a long digit run inside such a UUID) is the append guard's and the Architect's, not this gateway's. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/inputgateway"
)

func TestARequestIdOfOnlyDigitsIsRefusedBeforeAnythingIsRecordedOrStarted(t *testing.T) {
	for name, id := range map[string]string{
		"all zeros":                "00000000-0000-4000-8000-000000000000",
		"a card-number-sized run":  "41111111-1111-4111-8111-111111111111",
		"counting":                 "12345678-1234-4123-8123-123456789012",
		"version and variant only": "00000000-0000-4000-9000-000000000000",
	} {
		rec, ids, started := &seamRecorder{}, &countingIDs{}, 0
		g := seamGateway(t, rec, &seamIndex{}, ids, &started)
		_, err := g.Accept(context.Background(), identity(tenantA, "usr_88213"), body(id, "where is my money"))
		if !errors.Is(err, inputgateway.ErrInvalidInput) {
			t.Errorf("%s: Accept: %v, want ErrInvalidInput", name, err)
		}
		if len(rec.received) != 0 || len(rec.deduplicated) != 0 || len(rec.reused) != 0 || started != 0 || len(ids.minted) != 0 {
			t.Errorf("%s: %d appends, %d starts, %d IDs minted; want none: a refused input leaves nothing behind", name, len(rec.received), started, len(ids.minted))
		}
	}
}

func TestARequestIdWithAtLeastOneLetterIsStillAccepted(t *testing.T) {
	for name, id := range map[string]string{
		"one letter, the last character": "41111111-1111-4111-8111-11111111111a",
		"one letter, the first":          "a1111111-1111-4111-8111-111111111111",
		"the usual shape":                digitsRequestID,
		"all letters":                    "abcdefab-cdef-4bcd-8bcd-efabcdefabcd",
	} {
		rec, ids, started := &seamRecorder{}, &countingIDs{}, 0
		g := seamGateway(t, rec, &seamIndex{}, ids, &started)
		if _, err := g.Accept(context.Background(), identity(tenantA, "usr_88213"), body(id, "where is my money")); err != nil {
			t.Errorf("%s: Accept: %v, want it accepted", name, err)
		}
		if len(rec.received) != 1 || started != 1 {
			t.Errorf("%s: %d appends and %d starts, want one of each", name, len(rec.received), started)
		}
	}
}
