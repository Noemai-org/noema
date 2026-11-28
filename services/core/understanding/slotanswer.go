package understanding

import (
	"encoding/json"
	"errors"
	"time"
)

// A563 and A743 (interim, relayed; AMB-03; REQ-AMB-001; INV-02, INV-12; AGENT-09): an answered SLOT value joins the intent as Intent Object v(N+1) with `supersedes`, and the gate re-runs. Red stub.

// ErrSlotAlreadySet is returned when the answered slot already holds a value: an answer fills a slot the intent lacked and never overwrites one.
var ErrSlotAlreadySet = errors.New("understanding: the slot already holds a value")

// SupersedeWithSlot builds the raw document of the next version of an admitted intent that carries one slot value. It returns a document for the gate, not an admitted intent.
func SupersedeWithSlot(prev AdmittedIntent, slot string, value json.RawMessage, now time.Time) ([]byte, error) {
	return nil, ErrNotBuilt
}

// ErrNotBuilt is returned by a function that is not built yet.
var ErrNotBuilt = errors.New("understanding: not built")
