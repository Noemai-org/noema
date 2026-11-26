package readhostility_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/portread"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

// The harness: the recorded exchanges of the real hostility proxy are replayed to the real StandinClient, which feeds the real portread.Reader. The replay
// server answers each request with exactly what the proxy answered: the same status, Content-Type, Retry-After and bytes, after the hold the effect asked
// for, or with the connection cut when the proxy gave no answer. It counts the requests it receives, so a test can say how many reads one fault cost.

const recordings = "testdata/read_hostility_recordings.json"

type exchange struct {
	Scenario string          `json:"scenario"`
	Read     string          `json:"read"`
	Family   string          `json:"family"`
	Effect   string          `json:"effect"`
	Params   json.RawMessage `json:"params"`
	Request  struct {
		Path string `json:"path"`
	} `json:"request"`
	Outcome  string `json:"outcome"`
	HoldMS   int    `json:"hold_ms"`
	Served   string `json:"served"`
	Response struct {
		Status      int    `json:"status"`
		ContentType string `json:"content_type"`
		RetryAfter  string `json:"retry_after"`
		Body        string `json:"body"`
	} `json:"response"`
}

func (e exchange) hold() time.Duration { return time.Duration(e.HoldMS) * time.Millisecond }

func (e exchange) bodyHash() string {
	sum := sha256.Sum256([]byte(e.Response.Body))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func load(t testing.TB) []exchange {
	t.Helper()
	raw, err := os.ReadFile(recordings)
	if err != nil {
		t.Fatalf("%v: the recordings are made by the hostility proxy's own test (see the README of this package for the command)", err)
	}
	var doc struct {
		Exchanges []exchange `json:"exchanges"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	return doc.Exchanges
}

// replay serves one recorded exchange per path.
type replay struct {
	srv  *httptest.Server
	mu   sync.Mutex
	hits map[string]int
	seen []http.Header
}

func newReplay(t testing.TB, byPath map[string]exchange) *replay {
	t.Helper()
	rp := &replay{hits: map[string]int{}}
	rp.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rp.mu.Lock()
		rp.hits[r.URL.Path]++
		rp.seen = append(rp.seen, r.Header.Clone())
		rp.mu.Unlock()
		ex, ok := byPath[r.URL.Path]
		if !ok || r.Method != http.MethodGet {
			t.Errorf("%s %s: the reader asked for something the proxy was never recorded answering", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		if ex.HoldMS > 0 {
			select {
			case <-time.After(ex.hold()):
			case <-r.Context().Done():
				return
			}
		}
		if ex.Outcome == "dropped" {
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				_ = conn.Close() // the proxy cut the connection with no answer
			}
			return
		}
		w.Header().Set("Content-Type", ex.Response.ContentType)
		if ex.Response.RetryAfter != "" {
			w.Header().Set("Retry-After", ex.Response.RetryAfter)
		}
		w.WriteHeader(ex.Response.Status)
		_, _ = w.Write([]byte(ex.Response.Body))
	}))
	t.Cleanup(rp.srv.Close)
	return rp
}

func (rp *replay) hitsOn(path string) int {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.hits[path]
}

var (
	scope = adapters.Scope{TenantID: "ten_aa", PrincipalID: "usr_ab12"}
	epoch = time.Date(2026, 11, 26, 9, 0, 0, 0, time.UTC) // SYNTHETIC
)

const (
	runtimeToken = "runtime-token-runtime-token-runtime-token-0001" // SYNTHETIC, the values the adapter's own tests use
	principalKey = "principal-principal-principal-principal-0003"   // SYNTHETIC
)

func newClient(t testing.TB, rp *replay) *synthetic_tushe.StandinClient {
	t.Helper()
	dir := t.TempDir()
	tok, key := filepath.Join(dir, "token"), filepath.Join(dir, "key")
	if err := os.WriteFile(tok, []byte(runtimeToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte(principalKey), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := synthetic_tushe.NewStandinClient(synthetic_tushe.StandinOptions{BaseURL: rp.srv.URL, TokenFile: tok, PrincipalKeyFile: key, Now: func() time.Time { return epoch }})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// recorder keeps what the reader hands to the Evidence Layer. layer.Builder.Record turns an Observation into an item with layer.AssignStatus, the function the
// tests apply, so a status here is the status the item would carry. (The Builder's own database tests are the layer package's.)
type recorder struct{ got []layer.Observation }

func (r *recorder) Record(_ context.Context, o layer.Observation) (layer.Item, error) {
	r.got = append(r.got, o)
	return layer.Item{EvidenceID: "evr_synthetic"}, nil
}

func sources() portread.Sources {
	mk := func(id, fact string) portread.Source {
		return portread.Source{ID: id, Fact: fact, Authority: layer.AuthAuthoritative, Limit: 300 * time.Second, Sensitivity: "S3_financial_pii", ResourceKind: "fin.transaction"}
	}
	return portread.Sources{Ledger: mk("fin.src.core_ledger", "debit_posted_or_reversed"), Switch: mk("fin.src.switch", "switch_processing_result"),
		Destination: mk("fin.src.destination", "beneficiary_credited")}
}

// readOnce reads the transaction once with the given per-call timeout and returns what was recorded, in the order ledger, switch, destination.
func readOnce(t testing.TB, rp *replay, readTimeout time.Duration) []layer.Observation {
	t.Helper()
	c := newClient(t, rp)
	rec := &recorder{}
	r := &portread.Reader{Ledger: c, Switch: c, Destination: c, Sources: sources(), Rec: rec, Clock: clock.NewManual(epoch), ReadBy: "TRUST", ReadTimeout: readTimeout,
		DataSource: "stand-in", Synthetic: true}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	items, err := r.ReadTransaction(ctx, scope, "corr_synthetic", "TX1")
	if err != nil || len(items) != 3 || len(rec.got) != 3 {
		t.Fatalf("items %d, observations %d, err %v: a fault on one read must not stop the others", len(items), len(rec.got), err)
	}
	return rec.got
}

func status(o layer.Observation) string { return string(layer.AssignStatus(o.Read, o.Src, epoch)) }
