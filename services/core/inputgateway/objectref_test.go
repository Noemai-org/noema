package inputgateway_test

// A184 (relayed; the Architect, 13 Nov 2026): the object_ref of INPUT_RECEIVED is minted by the core, never derived from anything the client sent. Before it, the
// gateway wrote "req_" plus the hex digits of the client's request_id, so up to twelve digits a client chose came back, as written, in an object_ref (which the
// reconstruction serves by its contract pattern, unmasked). REQ-INP-002, REQ-AUD-001, REQ-SEC-003; AGENT-13, INV-12. All data is SYNTHETIC.

import (
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// mintedRef is "req_" and a ULID: 26 Crockford base32 characters, the first 0 to 7 (PERD 32.3).
var mintedRef = regexp.MustCompile(`^req_[0-7][0-9A-HJKMNP-TV-Z]{25}$`)

// idsFailingFor fails the IDs of one prefix and mints every other.
type idsFailingFor struct {
	mu     sync.Mutex
	inner  chain.IDSource
	prefix string
	fail   bool
}

func (f *idsFailingFor) ID(prefix string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail && prefix == f.prefix {
		return "", errors.New("synthetic: no ID can be minted")
	}
	return f.inner.ID(prefix)
}

func (f *idsFailingFor) set(v bool) { f.mu.Lock(); f.fail = v; f.mu.Unlock() }

// A request id whose last group is twelve digits: every digit of it is the client's choice.
const digitsRequestID = "0b8f3c1e-52a4-4d7e-9a61-123456789012" // SYNTHETIC

// A184: the object_ref of INPUT_RECEIVED is a core-minted ID, in the shape of PERD 32.3, and holds none of the request id's characters beyond what chance puts in a ULID.
func TestTheObjectRefOfAnInputIsMintedByTheCoreNotDerivedFromTheRequestId(t *testing.T) {
	w := newWorld(t)
	g := w.gateway(tenantA, &spy{})
	for _, rid := range []string{reqID, digitsRequestID} {
		if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(rid, "where is my money")); err != nil {
			t.Fatal(err)
		}
	}
	got := w.events("INPUT_RECEIVED")
	if len(got) != 2 {
		t.Fatalf("the Record holds %d INPUT_RECEIVED events, want 2", len(got))
	}
	for i, rid := range []string{reqID, digitsRequestID} {
		ref := got[i].ObjectRef
		if !mintedRef.MatchString(ref) {
			t.Errorf("object_ref %q of the input with request id %s is not a minted ID (req_ and a ULID)", ref, rid)
		}
		if hex := strings.ReplaceAll(rid, "-", ""); strings.Contains(ref, hex) || strings.Contains(ref, hex[len(hex)-12:]) {
			t.Errorf("object_ref %q carries the client's request id digits %q", ref, hex)
		}
	}
	if got[0].ObjectRef == got[1].ObjectRef {
		t.Errorf("two inputs share the object_ref %q", got[0].ObjectRef)
	}
	if got[1].Payload["request_id"] != digitsRequestID {
		t.Errorf("the request_id is still in the payload as the client sent it: %v", got[1].Payload["request_id"])
	}
}

// A deduplication and a reuse are events about the original input, so they name the original input's object_ref: not a new one, and not one derived from the request id.
func TestADeduplicationAndAReuseNameTheOriginalInputsObjectRef(t *testing.T) {
	w := newWorld(t)
	g := w.gateway(tenantA, &spy{})
	id := identity(tenantA, "usr_88213")
	if _, err := g.Accept(ctx(t), id, body(digitsRequestID, "where is my money")); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Accept(ctx(t), id, body(digitsRequestID, "where is my money")); err != nil {
		t.Fatal(err)
	}
	if _, err := g.Accept(ctx(t), id, body(digitsRequestID, "something else")); !errors.Is(err, inputgateway.ErrRequestIDReused) {
		t.Fatalf("reuse: %v", err)
	}
	original := w.events("INPUT_RECEIVED")
	if len(original) != 1 || !mintedRef.MatchString(original[0].ObjectRef) {
		t.Fatalf("the original input: %+v", original)
	}
	for _, e := range append(w.events("INPUT_DEDUPLICATED"), w.events("SECURITY_EVENT")...) {
		if e.ObjectRef != original[0].ObjectRef {
			t.Errorf("%s names the object_ref %q, want the original input's %q", e.EventType, e.ObjectRef, original[0].ObjectRef)
		}
	}
	if n := len(w.events("INPUT_DEDUPLICATED")) + len(w.events("SECURITY_EVENT")); n != 2 {
		t.Fatalf("%d follow-up events, want one deduplication and one security event", n)
	}
}

// Record first (EC-E16): if the object_ref cannot be minted the input is refused as the Record's failure, nothing is appended and nothing starts.
func TestWhenTheObjectRefCannotBeMintedNothingIsRecordedAndNothingStarts(t *testing.T) {
	w := newWorld(t)
	sp := &spy{}
	sp.w = w
	f := &idsFailingFor{inner: w.gen, prefix: "req"}
	g, err := inputgateway.New(inputgateway.Config{
		Recorder: &inputgateway.RecordRecorder{Pool: w.edge[tenantA], Appender: &chain.Appender{Clock: w.clk, IDs: w.gen, Signers: w.sign}, Timeout: 5 * time.Second},
		Index:    &inputgateway.RecordIndex{Pool: w.read[tenantA], Clock: w.clk, Window: 24 * time.Hour, Timeout: 5 * time.Second},
		Start:    sp.Start,
		IDs:      f,
		MaxBody:  64 << 10,
		HashKey:  hashKey(),
	})
	if err != nil {
		t.Fatal(err)
	}
	f.set(true)
	if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money")); !errors.Is(err, inputgateway.ErrRecord) {
		t.Fatalf("Accept with no object_ref to mint: %v, want ErrRecord", err)
	}
	if n := len(w.events()); n != 0 {
		t.Fatalf("%d events in the Record, want none", n)
	}
	if len(sp.turns) != 0 {
		t.Fatalf("the orchestrator was handed %d turns, want none", len(sp.turns))
	}
	f.set(false)
	if _, err := g.Accept(ctx(t), identity(tenantA, "usr_88213"), body(reqID, "where is my money")); err != nil {
		t.Fatalf("the same input once an ID can be minted: %v", err)
	}
}
