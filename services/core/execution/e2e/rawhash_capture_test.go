package e2e_test

// noema#390 (asked by C2, built by IL in noema#401): the raw hash of a successful case read is the SHA-256 of the response bytes the adapter RECEIVED. noema#401 proves it
// at the adapter with a capturing proxy; this file proves it in the end-to-end stack too, against the real mock-case, so the hash that the Evidence Layer stores and the Record
// carries (INV-19) is compared with the bytes on the wire and not with itself. REQ-CTX-001, REQ-CTX-002, INV-19. All data is SYNTHETIC.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/adapters"
	"github.com/Noemai-org/noema/services/core/adapters/synthetic_tushe"
)

// captureProxy is a reverse proxy in front of the real mock-case that keeps the exact bytes of every response body it relays, in order. It changes nothing on the way: the
// body is read and handed on as it was, and compression is off so the bytes are the ones the case system wrote.
type captureProxy struct {
	mu     sync.Mutex
	bodies [][]byte
}

func newCaptureProxy(t testing.TB, upstream string) (*httptest.Server, *captureProxy) {
	t.Helper()
	u, err := url.Parse(upstream)
	if err != nil {
		t.Fatal(err)
	}
	c := &captureProxy{}
	rp := httputil.NewSingleHostReverseProxy(u)
	rp.Transport = &http.Transport{DisableCompression: true}
	rp.ModifyResponse = func(resp *http.Response) error {
		b, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			return err
		}
		c.mu.Lock()
		c.bodies = append(c.bodies, b)
		c.mu.Unlock()
		resp.Body = io.NopCloser(bytes.NewReader(b))
		return nil
	}
	srv := httptest.NewServer(rp)
	t.Cleanup(srv.Close)
	return srv, c
}

// reset forgets what was captured so far, so the next read can be told from the setup around it.
func (c *captureProxy) reset() { c.mu.Lock(); c.bodies = nil; c.mu.Unlock() }

// only is the bytes of the single response captured since the last reset; any other count fails the test, so a read that made two calls is not hashed as if it made one.
func (c *captureProxy) only(t testing.TB) []byte {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.bodies) != 1 {
		t.Fatalf("the proxy captured %d responses since the reset, want exactly 1", len(c.bodies))
	}
	return c.bodies[0]
}

func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// noema#390: through the real mock-case and a capturing proxy, the RawHash of GetCase and of FindCases is the SHA-256 of the exact bytes the case system sent. A hash of the
// decoded record, of a re-encoding or of anything the adapter built itself would differ from it.
func TestTheAdaptersRawHashIsTheSHA256OfTheBytesTheCaseSystemSent(t *testing.T) {
	mc := startMockCase(t)
	proxy, wire := newCaptureProxy(t, mc.URL)
	cases, err := synthetic_tushe.NewCaseClient(proxy.URL, mc.TokenFile, nil)
	if err != nil {
		t.Fatal(err)
	}
	created := cases.CreateCase(bg(t), adapters.CreateCaseInput{IdempotencyKey: "idk_rawhash_wire", TenantID: tenant, PrincipalID: principal, TransactionRef: txRef,
		ReasonCode: reasonCode, CreatedByIntent: "int_rawhash_wire", Epoch: 1, Timeout: 5 * time.Second})
	if created.Class != adapters.ClassAck2xx {
		t.Fatalf("create: %+v", created)
	}
	wire.reset()
	page, err := cases.FindCases(bg(t), adapters.CaseQuery{TenantID: tenant, PrincipalID: principal, TransactionRef: txRef})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("find: %+v %v", page, err)
	}
	if want := sha256Hex(wire.only(t)); page.RawHash != want {
		t.Errorf("FindCases RawHash %q, want the SHA-256 of the response bytes %q", page.RawHash, want)
	}
	wire.reset()
	rec, err := cases.GetCase(bg(t), tenant, page.Items[0].CaseID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if want := sha256Hex(wire.only(t)); rec.RawHash != want {
		t.Errorf("GetCase RawHash %q, want the SHA-256 of the response bytes %q", rec.RawHash, want)
	}
}
