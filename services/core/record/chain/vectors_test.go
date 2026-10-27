package chain_test

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// The golden vectors of the Record chain (contracts/fixtures/golden_vectors.json, family record_event_chain_vectors) are
// written by tools/record_chain_ref.py, a standard-library reference written from PERD 32.1 and the v2 schema. This file
// replays them through the Go code, so the two implementations must agree byte for byte on genesis, payload hash and hash.

type vectorSpec struct {
	EventID   string `json:"event_id"`
	WrittenBy string `json:"written_by"`
	Actor     string `json:"actor"`
	EventType string `json:"event_type"`
	Kind      string `json:"kind"`
	Reason    string `json:"reason"`
	ObjectRef string `json:"object_ref"`
	At        string `json:"at"`
	Payload   any    `json:"payload"`
}

type vector struct {
	Name            string `json:"name"`
	TenantID        string `json:"tenant_id"`
	CorrelationID   string `json:"correlation_id"`
	ExpectedGenesis string `json:"expected_genesis_hash"`
	Events          []struct {
		Spec     vectorSpec `json:"spec"`
		Expected struct {
			Seq         int64  `json:"seq"`
			PrevHash    string `json:"prev_hash"`
			PayloadHash string `json:"payload_hash"`
			Hash        string `json:"hash"`
		} `json:"expected"`
	} `json:"events"`
}

func loadVectors(t testing.TB) []vector {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/fixtures/golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Vectors []vector `json:"record_event_chain_vectors"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // integers stay integers; the canonicaliser refuses a float64
	if err := dec.Decode(&file); err != nil {
		t.Fatal(err)
	}
	if len(file.Vectors) == 0 {
		t.Fatal("golden_vectors.json has no record_event_chain_vectors")
	}
	return file.Vectors
}

func (s vectorSpec) event(tenant, corr string) chain.Event {
	actor := s.Actor
	if actor == "" {
		actor = s.WrittenBy
	}
	return chain.Event{TenantID: tenant, CorrelationID: corr, WrittenBy: s.WrittenBy, Actor: actor, EventType: s.EventType, Kind: s.Kind, Reason: s.Reason,
		ObjectRef: s.ObjectRef, Payload: s.Payload}
}

// PERD 32.1: genesis, payload hash and event hash equal the reference implementation's, for every vector.
func TestGoldenVectorsMatchTheReferenceImplementation(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			if got := chain.GenesisHash(v.TenantID); got != v.ExpectedGenesis {
				t.Fatalf("genesis %s, want %s", got, v.ExpectedGenesis)
			}
			prev := chain.GenesisHash(v.TenantID)
			for i, e := range v.Events {
				at, err := time.Parse(time.RFC3339Nano, e.Spec.At)
				if err != nil {
					t.Fatal(err)
				}
				ph, _, err := chain.PayloadHash(e.Spec.Payload)
				if err != nil {
					t.Fatal(err)
				}
				body := chain.Body(e.Spec.event(v.TenantID, v.CorrelationID), e.Spec.EventID, int64(i+1), prev, ph, at)
				hash, err := chain.Hash(prev, body)
				if err != nil {
					t.Fatal(err)
				}
				if ph != e.Expected.PayloadHash || prev != e.Expected.PrevHash || hash != e.Expected.Hash || int64(i+1) != e.Expected.Seq {
					t.Fatalf("event %d (%s): seq %d prev %s payload %s hash %s; want seq %d prev %s payload %s hash %s",
						i+1, e.Spec.EventID, i+1, prev, ph, hash, e.Expected.Seq, e.Expected.PrevHash, e.Expected.PayloadHash, e.Expected.Hash)
				}
				prev = hash
			}
		})
	}
}

// scripted gives the appender the vector's own times and IDs, so the stored chain can be compared with the vector.
type scripted struct {
	times []time.Time
	ids   []string
	t, i  int
}

func (s *scripted) Now() time.Time { n := s.times[s.t]; s.t++; return n }
func (s *scripted) ID(string) (string, error) {
	n := s.ids[s.i]
	s.i++
	return n, nil
}

var _ clock.Clock = (*scripted)(nil)

// The same vectors through the real appender and Postgres: what is stored, and what Verify accepts, is the vector's chain.
func TestGoldenVectorsThroughTheAppenderAndVerify(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			w := newWorld(t, v.TenantID)
			sc := &scripted{}
			for _, e := range v.Events {
				at, err := time.Parse(time.RFC3339Nano, e.Spec.At)
				if err != nil {
					t.Fatal(err)
				}
				sc.times, sc.ids = append(sc.times, at), append(sc.ids, e.Spec.EventID)
			}
			w.app = &chain.Appender{Clock: sc, IDs: sc}
			for i, e := range v.Events {
				st := w.append(t, e.Spec.event(v.TenantID, v.CorrelationID))
				if st.Seq != e.Expected.Seq || st.PrevHash != e.Expected.PrevHash || st.Hash != e.Expected.Hash || st.EventID != e.Spec.EventID {
					t.Fatalf("event %d stored as %+v, want seq %d prev %s hash %s", i+1, st, e.Expected.Seq, e.Expected.PrevHash, e.Expected.Hash)
				}
			}
			if n, err := chain.Verify(ctx(t), w.admin, v.TenantID); err != nil || n != int64(len(v.Events)) {
				t.Fatalf("verify = %d, %v", n, err)
			}
		})
	}
}
