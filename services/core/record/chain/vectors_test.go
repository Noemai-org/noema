package chain_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// The golden vectors of the Record chain (contracts/fixtures/golden_vectors.json, family record_event_chain_vectors_v3) are
// written by tools/record_chain_ref_v3.py, a standard-library reference written from PERD v1.17 section 32.1 and the v3 schema.
// This file replays them through the Go code, so the two implementations must agree byte for byte on genesis, payload hash and hash.
// The payload_hash_vectors family was produced by an independent implementation (tools/payload_hash_independent.js, Node) and is
// replayed here too: the Go canonicaliser must produce the recorded bytes and the recorded hash for each input.

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
			Actor       string `json:"actor"`
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
		Vectors []vector `json:"record_event_chain_vectors_v3"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber() // integers stay integers; the canonicaliser refuses a float64
	if err := dec.Decode(&file); err != nil {
		t.Fatal(err)
	}
	if len(file.Vectors) == 0 {
		t.Fatal("golden_vectors.json has no record_event_chain_vectors_v3")
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
				if got := body["actor"]; got != e.Expected.Actor {
					t.Fatalf("event %d: actor %v, want %s", i+1, got, e.Expected.Actor)
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

type payloadVectors struct {
	Hashes []struct {
		Name         string `json:"name"`
		PayloadJSON  string `json:"payload_json"`
		InputUTF8    string `json:"input_bytes_utf8"`
		InputHex     string `json:"input_bytes_hex"`
		ExpectedHash string `json:"expected_hash"`
	} `json:"payload_hash_vectors"`
	Rejected []struct {
		Name        string `json:"name"`
		PayloadJSON string `json:"payload_json"`
	} `json:"payload_rejected_vectors"`
}

func loadPayloadVectors(t testing.TB) payloadVectors {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/fixtures/golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var v payloadVectors
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func parseNumbers(t testing.TB, text string) any {
	t.Helper()
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber() // integers stay integers; a fraction stays a json.Number the canonicaliser refuses
	var p any
	if err := dec.Decode(&p); err != nil {
		t.Fatal(err)
	}
	return p
}

// PERD v1.17 section 32.1: payload_hash = SHA-256(JCS(payload)). The expected bytes and hashes come from an independent implementation
// (Node, RFC 8785 from ECMAScript's own serialisation); the Go code must reach the same bytes and the same hash for the empty object,
// a nested object, a Unicode string (with an astral-plane member name that sorts by UTF-16 code unit) and integers (with -0).
func TestPayloadHashVectorsFromAnIndependentImplementation(t *testing.T) {
	v := loadPayloadVectors(t)
	if len(v.Hashes) != 4 {
		t.Fatalf("%d payload vectors, want the four of the instruction (empty object, nested object, Unicode string, integers)", len(v.Hashes))
	}
	for _, c := range v.Hashes {
		t.Run(c.Name, func(t *testing.T) {
			hash, canonical, err := chain.PayloadHash(parseNumbers(t, c.PayloadJSON))
			if err != nil {
				t.Fatal(err)
			}
			if hash != c.ExpectedHash {
				t.Fatalf("payload_hash %s, want %s (input bytes %s)", hash, c.ExpectedHash, c.InputHex)
			}
			if c.PayloadJSON != "{}" { // {} is stored as no payload
				if canonical != c.InputUTF8 {
					t.Fatalf("canonical text %v, want %s", canonical, c.InputUTF8)
				}
			}
			raw, err := hex.DecodeString(c.InputHex)
			if err != nil || string(raw) != c.InputUTF8 {
				t.Fatalf("the recorded bytes are not the recorded text: %v", err)
			}
			sum := sha256.Sum256(raw)
			if "sha256:"+hex.EncodeToString(sum[:]) != c.ExpectedHash {
				t.Fatal("the recorded hash is not the SHA-256 of the recorded bytes")
			}
		})
	}
}

// An event that refers to no object has the payload {} (nil here), and its hash is the hash of the two bytes {}.
func TestAnEventWithoutAnObjectHasTheHashOfTheEmptyObject(t *testing.T) {
	nilHash, _, err := chain.PayloadHash(nil)
	if err != nil {
		t.Fatal(err)
	}
	emptyHash, _, err := chain.PayloadHash(map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if nilHash != emptyHash || nilHash != "sha256:44136fa355b3678a1146ad16f7e8649e94fb4fc21fe77e8310c060f61caaff8a" {
		t.Fatalf("nil payload %s, {} %s", nilHash, emptyHash)
	}
}

// PERD v1.17 section 32.1: integers only. A fraction, an exponent form or an integer beyond 2^53-1 is rejected before a hash is computed.
func TestNonIntegerNumbersAreRejectedInAPayload(t *testing.T) {
	v := loadPayloadVectors(t)
	if len(v.Rejected) < 6 {
		t.Fatalf("%d rejected vectors", len(v.Rejected))
	}
	for _, c := range v.Rejected {
		t.Run(c.Name, func(t *testing.T) {
			if hash, _, err := chain.PayloadHash(parseNumbers(t, c.PayloadJSON)); err == nil {
				t.Fatalf("%s was hashed to %s", c.PayloadJSON, hash)
			}
		})
	}
	if _, _, err := chain.PayloadHash(map[string]any{"x": mustFloat("1.5")}); err == nil {
		t.Fatal("a float64 was hashed")
	}
}
