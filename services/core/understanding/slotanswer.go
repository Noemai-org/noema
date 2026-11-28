package understanding

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

// A563 and A743 (interim, relayed; AMB-03; REQ-AMB-001; INV-02, INV-12; AGENT-09): an answered SLOT value joins the intent as Intent Object v(N+1) with `supersedes`, and the gate re-runs. The
// next version is built here, by the core, from the stored intent; the model is not asked and the answer is data: the gate judges the slot's name and type, the principal and the signature
// are the session's and the core's, never the answer's.

// ErrSlotAlreadySet is returned when the answered slot already holds a value: an answer fills a slot the intent lacked and never overwrites one.
var ErrSlotAlreadySet = errors.New("understanding: the slot already holds a value")

// ErrSlotAnswer is returned when the answered value, or the stored intent, is not a JSON object the next version can be built from.
var ErrSlotAnswer = errors.New("understanding: the slot answer is not a JSON object")

// SupersedeWithSlot builds the raw document of the next version of an admitted intent that carries one slot value, ready for Gate.Admit: the version is one more, `supersedes` names the previous
// version, `created_at` is now, the model markers stand in for the principal's type and assertion and for the signature (the gate refuses a model that fills them, and the core overwrites
// them with the session's values after it admits the document). It returns a document for the gate, not an admitted intent: nothing is admitted until the gate admits it.
func SupersedeWithSlot(prev AdmittedIntent, slot string, value json.RawMessage, now time.Time) ([]byte, error) {
	val, err := objectOf(value)
	if err != nil {
		return nil, err
	}
	doc, err := objectOf(prev.JSON())
	if err != nil {
		return nil, err
	}
	slots, ok := doc["slots"].(map[string]any)
	if !ok {
		return nil, ErrSlotAnswer
	}
	if _, held := slots[slot]; held {
		return nil, ErrSlotAlreadySet
	}
	slots[slot] = val
	o := prev.Object()
	next := int64(o.Version) + 1
	doc["version"] = json.Number(fmt.Sprint(next))
	doc["supersedes"] = fmt.Sprintf("%s@%d", o.IntentId, o.Version)
	doc["created_at"] = now.UTC().Format("2006-01-02T15:04:05Z")
	doc["sig"] = map[string]any{"key_id": unsignedMarker, "value": unsignedMarker}
	if pr, ok := doc["principal"].(map[string]any); ok {
		pr["principal_type"], pr["asserted_by"] = unsetMarker, unsetMarker
	} else {
		return nil, ErrSlotAnswer
	}
	out, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSlotAnswer, err)
	}
	return out, nil
}

// objectOf decodes exactly one JSON object, keeping numbers as written.
func objectOf(raw []byte) (map[string]any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil || m == nil {
		return nil, ErrSlotAnswer
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, ErrSlotAnswer
	}
	return m, nil
}
