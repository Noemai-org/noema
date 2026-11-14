package inputgateway_test

// A184, the same rule at the seams, with no database: the gateway mints the object_ref from its ID source before the append, hands it to the recorder, never builds it from the
// request id, and passes the original input's object_ref on to a deduplication and a reuse. REQ-INP-002, REQ-AUD-001; AGENT-13, EC-E16. All data is SYNTHETIC.

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/Noemai-org/noema/services/core/inputgateway"
)

// countingIDs mints "<prefix>_<n>" and can refuse one prefix.
type countingIDs struct {
	mu     sync.Mutex
	n      int
	refuse string
	minted []string
}

func (c *countingIDs) ID(prefix string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if prefix == c.refuse {
		return "", errors.New("synthetic: no ID")
	}
	c.n++
	id := prefix + "_NOTADIGITRUN" + strings.Repeat("X", c.n)
	c.minted = append(c.minted, id)
	return id, nil
}

type seamRecorder struct {
	received     []inputgateway.Received
	deduplicated []inputgateway.Ref
	reused       []inputgateway.Ref
}

func (r *seamRecorder) Received(_ context.Context, _ inputgateway.Identity, _ string, in inputgateway.Received) (inputgateway.Ref, error) {
	r.received = append(r.received, in)
	return inputgateway.Ref{EventID: "recev_1", Seq: 1, Hash: "sha256:00", ObjectRef: in.ObjectRef}, nil
}

func (r *seamRecorder) Deduplicated(_ context.Context, _ inputgateway.Identity, _, _ string, original inputgateway.Ref) error {
	r.deduplicated = append(r.deduplicated, original)
	return nil
}

func (r *seamRecorder) Reused(_ context.Context, _ inputgateway.Identity, _, _, _ string, original inputgateway.Ref) error {
	r.reused = append(r.reused, original)
	return nil
}

type seamIndex struct{ prior *inputgateway.Prior }

func (i *seamIndex) Find(context.Context, inputgateway.Identity, string) (inputgateway.Prior, bool, error) {
	if i.prior == nil {
		return inputgateway.Prior{}, false, nil
	}
	return *i.prior, true, nil
}

func seamGateway(t *testing.T, rec *seamRecorder, idx *seamIndex, ids *countingIDs, started *int) *inputgateway.Gateway {
	t.Helper()
	g, err := inputgateway.New(inputgateway.Config{Recorder: rec, Index: idx, IDs: ids, MaxBody: 64 << 10, HashKey: hashKey(),
		Start: func(context.Context, inputgateway.Turn) error { *started++; return nil }})
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// The recorder is handed the object_ref the gateway minted with the "req" prefix, and it is not the request id's digits.
func TestTheGatewayMintsTheObjectRefAndHandsItToTheRecorder(t *testing.T) {
	rec, ids, started := &seamRecorder{}, &countingIDs{}, 0
	g := seamGateway(t, rec, &seamIndex{}, ids, &started)
	res, err := g.Accept(context.Background(), identity(tenantA, "usr_88213"), body(digitsRequestID, "where is my money"))
	if err != nil {
		t.Fatal(err)
	}
	if len(rec.received) != 1 {
		t.Fatalf("%d INPUT_RECEIVED appends, want 1", len(rec.received))
	}
	got := rec.received[0].ObjectRef
	var want string
	for _, m := range ids.minted {
		if strings.HasPrefix(m, "req_") {
			want = m
		}
	}
	if want == "" || got != want {
		t.Errorf("the recorder was handed object_ref %q, want the one minted with the req prefix %q (minted: %v)", got, want, ids.minted)
	}
	if strings.Contains(got, "123456789012") {
		t.Errorf("object_ref %q carries the client's digits", got)
	}
	if res.Input.ObjectRef != got || started != 1 {
		t.Errorf("result %+v, started %d: the turn must be handed the recorded reference once", res.Input, started)
	}
}

// If no object_ref can be minted nothing is appended and nothing starts (Record first, EC-E16).
func TestWhenNoObjectRefCanBeMintedTheGatewayAppendsAndStartsNothing(t *testing.T) {
	rec, ids, started := &seamRecorder{}, &countingIDs{refuse: "req"}, 0
	g := seamGateway(t, rec, &seamIndex{}, ids, &started)
	if _, err := g.Accept(context.Background(), identity(tenantA, "usr_88213"), body(reqID, "where is my money")); !errors.Is(err, inputgateway.ErrRecord) {
		t.Fatalf("Accept: %v, want ErrRecord", err)
	}
	if len(rec.received) != 0 || started != 0 {
		t.Fatalf("%d appends and %d starts, want none", len(rec.received), started)
	}
}

// A retry and a reuse name the original input's object_ref: the gateway hands the prior's Ref, object_ref included, to the recorder as it found it, and mints nothing for them.
func TestARetryAndAReuseCarryTheOriginalObjectRefAndMintNone(t *testing.T) {
	first0 := &seamRecorder{}
	s0 := 0
	first, err := seamGateway(t, first0, &seamIndex{}, &countingIDs{}, &s0).Accept(context.Background(), identity(tenantA, "usr_88213"), body(reqID, "where is my money"))
	if err != nil || first.Input.ObjectRef == "" {
		t.Fatalf("the first delivery: %+v, %v", first, err)
	}
	prior := &inputgateway.Prior{Ref: first.Input, CorrelationID: first.CorrelationID, InputHash: first0.received[0].InputHash}

	rec, ids, started := &seamRecorder{}, &countingIDs{}, 0
	g := seamGateway(t, rec, &seamIndex{prior: prior}, ids, &started)
	if res, err := g.Accept(context.Background(), identity(tenantA, "usr_88213"), body(reqID, "where is my money")); err != nil || !res.Deduplicated {
		t.Fatalf("the retry: %+v, %v", res, err)
	}
	if len(rec.deduplicated) != 1 || rec.deduplicated[0].ObjectRef != first.Input.ObjectRef {
		t.Fatalf("the deduplication was given %+v, want the original input's object_ref %q", rec.deduplicated, first.Input.ObjectRef)
	}
	if _, err := g.Accept(context.Background(), identity(tenantA, "usr_88213"), body(reqID, "something else")); !errors.Is(err, inputgateway.ErrRequestIDReused) {
		t.Fatalf("the reuse: %v", err)
	}
	if len(rec.reused) != 1 || rec.reused[0].ObjectRef != first.Input.ObjectRef {
		t.Fatalf("the reuse was given %+v, want the original input's object_ref %q", rec.reused, first.Input.ObjectRef)
	}
	if len(ids.minted) != 0 {
		t.Errorf("a retry and a reuse minted %v, want nothing", ids.minted)
	}
}
