package server

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/store"
	"github.com/Noemai-org/noema/synthetic/webhook_relay/internal/verify"
)

var (
	now = time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC) // SYNTHETIC
	key = []byte("0123456789abcdef0123456789abcdef")
)

type rig struct {
	t   *testing.T
	ts  *httptest.Server
	v   *verify.Verifier
	st  *store.Store
	tok string
}

func newRig(t *testing.T, path string) *rig {
	t.Helper()
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	v := verify.New(key)
	tok := verify.PullToken(key)
	ts := httptest.NewServer(New(v, st, tok, func() time.Time { return now }).Handler())
	t.Cleanup(ts.Close)
	return &rig{t, ts, v, st, tok}
}

func (r *rig) post(id string, ts int64, body, sig string) int {
	req, _ := http.NewRequest("POST", r.ts.URL+"/webhooks", strings.NewReader(body))
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(ts, 10))
	req.Header.Set("webhook-signature", sig)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}

func (r *rig) signed(id string, ts int64, body string) int {
	return r.post(id, ts, body, r.v.Sign(id, ts, []byte(body)))
}

func (r *rig) get(path, token string) (int, []byte) {
	req, _ := http.NewRequest("GET", r.ts.URL+path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, b
}

func TestRefusedDeliveriesAreNeverStored(t *testing.T) { // REQ-API-008, DEC-2026-10-16 E6
	r := newRig(t, filepath.Join(t.TempDir(), "s"))
	ts := now.Unix()
	body := `{"type":"case.updated"}`
	forged := verify.New([]byte("ffffffffffffffffffffffffffffffff")).Sign("m1", ts, []byte(body))
	cases := []struct {
		name string
		code int
	}{
		{"forged signature", r.post("m1", ts, body, forged)},
		{"tampered body", r.post("m1", ts, body+" ", r.v.Sign("m1", ts, []byte(body)))},
		{"no signature", r.post("m1", ts, body, "")},
		{"six minutes old", r.signed("m2", ts-360, body)},
		{"six minutes ahead", r.signed("m3", ts+360, body)},
		{"bad id", r.signed("bad id!", ts, body)},
	}
	for _, c := range cases {
		if c.code != 400 {
			t.Errorf("%s: status %d, want 400", c.name, c.code)
		}
	}
	if r.st.Len() != 0 {
		t.Fatalf("a refused delivery was stored (%d)", r.st.Len())
	}
	_, b := r.get("/v1/stats", r.tok)
	var stats struct{ Counters map[string]int64 }
	json.Unmarshal(b, &stats)
	for _, k := range []string{"refused_bad_signature", "refused_timestamp_too_old", "refused_timestamp_in_the_future", "refused_missing_header", "refused_bad_id"} {
		if stats.Counters[k] == 0 {
			t.Errorf("counter %s is zero: %v", k, stats.Counters)
		}
	}
}

func TestBoundaryToleranceIsAccepted(t *testing.T) {
	r := newRig(t, filepath.Join(t.TempDir(), "s"))
	if c := r.signed("old", now.Unix()-300, "{}"); c != 200 {
		t.Errorf("exactly 5 min old: %d", c)
	}
	if c := r.signed("new", now.Unix()+300, "{}"); c != 200 {
		t.Errorf("exactly 5 min ahead: %d", c)
	}
}

func TestReplayStoredOnceAndIDConflictRefused(t *testing.T) {
	r := newRig(t, filepath.Join(t.TempDir(), "s"))
	ts := now.Unix()
	for i := 0; i < 3; i++ {
		if c := r.signed("m1", ts, `{"n":1}`); c != 200 {
			t.Fatalf("replay %d: %d", i, c)
		}
	}
	if r.st.Len() != 1 {
		t.Fatalf("a replayed delivery was stored %d times", r.st.Len())
	}
	if c := r.signed("m1", ts, `{"n":2}`); c != 409 {
		t.Fatalf("id reuse with another body: %d, want 409", c)
	}
	if r.st.Len() != 1 {
		t.Fatal("the conflicting delivery was stored")
	}
}

func TestPersistsAcrossRestart(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s")
	r := newRig(t, p)
	r.signed("m1", now.Unix(), `{"n":1}`)
	r.st.Close()
	r2 := newRig(t, p)
	code, b := r2.get("/v1/deliveries", r2.tok)
	if code != 200 || !strings.Contains(string(b), `"id":"m1"`) {
		t.Fatalf("after restart: %d %s", code, b)
	}
	if c := r2.signed("m1", now.Unix(), `{"n":1}`); c != 200 || r2.st.Len() != 1 {
		t.Fatalf("replay after restart: %d len %d", c, r2.st.Len())
	}
}

func TestPullNeedsTheDerivedToken(t *testing.T) {
	r := newRig(t, filepath.Join(t.TempDir(), "s"))
	r.signed("m1", now.Unix(), `{}`)
	for _, path := range []string{"/v1/deliveries", "/v1/stats"} {
		for _, tok := range []string{"", "wrong", string(key)} { // the signing secret itself is not the pull token
			if c, _ := r.get(path, tok); c != 401 {
				t.Errorf("%s with %q: %d, want 401", path, tok, c)
			}
		}
		if c, _ := r.get(path, r.tok); c != 200 {
			t.Errorf("%s with the pull token: %d", path, c)
		}
	}
	if c, _ := r.get("/healthz", ""); c != 200 {
		t.Errorf("healthz: %d", c)
	}
}

func TestPagingAfterCursor(t *testing.T) {
	r := newRig(t, filepath.Join(t.TempDir(), "s"))
	for i := 1; i <= 5; i++ {
		r.signed("m"+strconv.Itoa(i), now.Unix(), `{"i":`+strconv.Itoa(i)+`}`)
	}
	var page struct {
		Deliveries []store.Entry `json:"deliveries"`
		NextAfter  int64         `json:"next_after"`
	}
	_, b := r.get("/v1/deliveries?limit=2", r.tok)
	json.Unmarshal(b, &page)
	if len(page.Deliveries) != 2 || page.NextAfter != 2 {
		t.Fatalf("page 1: %+v", page)
	}
	_, b = r.get("/v1/deliveries?after=2&limit=10", r.tok)
	json.Unmarshal(b, &page)
	if len(page.Deliveries) != 3 || page.Deliveries[0].ID != "m3" || page.NextAfter != 5 || string(page.Deliveries[0].Body) != `{"i":3}` {
		t.Fatalf("page 2: %+v", page)
	}
	_, b = r.get("/v1/deliveries?after=5", r.tok)
	json.Unmarshal(b, &page)
	if len(page.Deliveries) != 0 || page.NextAfter != 5 {
		t.Fatalf("empty page: %+v", page)
	}
	for _, q := range []string{"after=-1", "after=x", "limit=0", "limit=501"} {
		if c, _ := r.get("/v1/deliveries?"+q, r.tok); c != 400 {
			t.Errorf("%s: %d, want 400", q, c)
		}
	}
}
