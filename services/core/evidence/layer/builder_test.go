package layer_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
	"github.com/Noemai-org/noema/services/core/record/chain"
)

// Tests for the item builder (EVD-01 slice 2b): REQ-CTX-001, REQ-CTX-002, REQ-AUD-001, INV-10, INV-19, INV-11, EC-E16. All data is SYNTHETIC.

var readAt = time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC) // SYNTHETIC

// failingIDs fails the nth Record event ID, so the append of that event fails (EC-E16).
type failingIDs struct {
	inner chain.IDSource
	fail  bool
}

func (f *failingIDs) ID(prefix string) (string, error) {
	if f.fail && prefix == "recev" {
		return "", errors.New("synthetic: the Record refused the event")
	}
	return f.inner.ID(prefix)
}

func newBuilder(t *testing.T, w *world, tenant string, failAppend bool) (*layer.Builder, *failingIDs) {
	t.Helper()
	clk := clock.NewManual(readAt.Add(time.Minute))
	gen := ids.New(clk, nil)
	rec := &failingIDs{inner: gen, fail: failAppend}
	return &layer.Builder{Pool: w.evidence(t, tenant), Clock: clk, IDs: gen, Rec: &chain.Appender{Clock: clk, IDs: rec}}, rec
}

func observation(tenant string, class layer.ReadClass) layer.Observation {
	return layer.Observation{
		TenantID: tenant, CorrelationID: "corr_synthetic01", Source: "fin.src.core_ledger", SourceRecordID: "rec-1", ReadBy: "TRUST",
		EffectiveAt: readAt.Add(-time.Minute), AuthorityClass: "AUTHORITATIVE_FOR:debit_posted_or_reversed",
		ResourceScope: "res_synthetic", PrincipalScope: "prn_synthetic", Sensitivity: "S3_financial_pii",
		Read:    layer.Read{Class: class, Determinate: true, ReadAt: readAt},
		Src:     layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: 300 * time.Second},
		Payload: map[string]any{"b": 2, "a": 1}, // key order must not matter
		RawHash: "sha256:" + strings.Repeat("ab", 32),
	}
}

func (w *world) rows(t *testing.T, q string, args ...any) []string {
	t.Helper()
	rs, err := w.admin.Query(ctx(t), q, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rs.Close()
	var out []string
	for rs.Next() {
		var s string
		if err := rs.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

// REQ-CTX-001, REQ-AUD-001: an item is persisted with its payload before use, and the read is on the Record by hash only.
func TestBuilderPersistsThePayloadTheItemAndTheRecordEvent(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, false)
	item, err := b.Record(ctx(t), observation(tenantA, layer.ReadOK))
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte(`{"a":1,"b":2}`)) // the canonical form: sorted keys, no spaces
	want := "sha256:" + hex.EncodeToString(sum[:])
	if item.PayloadHash != want || item.Status != "KNOWN" || !strings.HasPrefix(item.EvidenceID, "evr_") {
		t.Fatalf("item = %+v, want hash %s, KNOWN, an evr_ ID", item, want)
	}
	if got := w.rows(t, `SELECT fact_status || ' ' || payload_hash FROM evidence_item WHERE evidence_id = $1`, item.EvidenceID); len(got) != 1 || got[0] != "KNOWN "+want {
		t.Fatalf("stored item = %v", got)
	}
	if got := w.rows(t, `SELECT json::text FROM evidence_payload WHERE payload_ref = $1`, item.PayloadRef); len(got) != 1 || got[0] != `{"a": 1, "b": 2}` {
		t.Fatalf("stored payload = %v", got)
	}
	ev := w.rows(t, `SELECT event_type || ' ' || written_by FROM record_event WHERE tenant_id = $1 ORDER BY seq`, tenantA)
	if len(ev) != 1 || ev[0] != "EVIDENCE_READ core-evidence" {
		t.Fatalf("Record events = %v", ev)
	}
	if got := w.rows(t, `SELECT payload::text FROM record_event WHERE tenant_id = $1`, tenantA); strings.Contains(got[0], `"a"`) || !strings.Contains(got[0], want) {
		t.Fatalf("the event must carry the hash and not the payload: %v", got)
	}
}

// REQ-CTX-002, INV-19: the status written is the one AssignStatus gives, and a failed or empty read never becomes a negative fact.
func TestBuilderWritesTheStatusOfEachReadClass(t *testing.T) {
	cases := map[layer.ReadClass]string{
		layer.ReadOK: "KNOWN", layer.ReadTimeout: "UNAVAILABLE", layer.ReadServerError: "UNAVAILABLE", layer.ReadRateLimited: "UNAVAILABLE",
		layer.ReadCircuitOpen: "UNAVAILABLE", layer.ReadMalformed: "UNKNOWN", layer.ReadNotFound: "UNKNOWN", layer.ReadOutOfScope: "UNAUTHORIZED",
	}
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, false)
	for class, want := range cases {
		o := observation(tenantA, class)
		o.SourceRecordID = "rec-" + string(class)
		if class != layer.ReadOK {
			o.Payload = nil // no response body for a read that failed
			if class == layer.ReadMalformed {
				o.RawHash = "sha256:" + strings.Repeat("ab", 32) // a malformed body was received, so its hash is supplied
			}
		}
		item, err := b.Record(ctx(t), o)
		if err != nil {
			t.Fatalf("%s: %v", class, err)
		}
		if string(item.Status) != want {
			t.Errorf("%s: status = %s, want %s", class, item.Status, want)
		}
	}
	if n := w.count(t, `SELECT count(*) FROM evidence_item WHERE fact_status IN ('NOT_CREDITED')`); n != 0 {
		t.Fatalf("a negative fact was stored")
	}
	if n := w.count(t, `SELECT count(*) FROM record_event WHERE event_type = 'EVIDENCE_READ'`); n != len(cases) {
		t.Fatalf("EVIDENCE_READ events = %d, want %d (one per read)", n, len(cases))
	}
}

// REQ-AUD-001, EC-E16: if the Record append fails, no payload and no item is stored.
func TestBuilderStoresNothingWhenTheRecordAppendFails(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, true)
	if _, err := b.Record(ctx(t), observation(tenantA, layer.ReadOK)); err == nil {
		t.Fatal("the builder succeeded although the Record refused the event")
	}
	for _, q := range []string{`SELECT count(*) FROM evidence_payload`, `SELECT count(*) FROM evidence_item`, `SELECT count(*) FROM record_event`} {
		if n := w.count(t, q); n != 0 {
			t.Errorf("%s = %d after a failed append", q, n)
		}
	}
}

// INV-11, REQ-SEC-004: an observation for another tenant than the login's is refused by the database and stores nothing.
func TestBuilderCannotWriteForAnotherTenant(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, false)
	if _, err := b.Record(ctx(t), observation("ten_bb", layer.ReadOK)); err == nil {
		t.Fatal("a tenant A builder wrote for tenant B")
	}
	if n := w.count(t, `SELECT count(*) FROM evidence_item`) + w.count(t, `SELECT count(*) FROM record_event`); n != 0 {
		t.Fatalf("%d rows were stored", n)
	}
}

// REQ-CTX-002: a source without a usable freshness window cannot make an item; the contract requires a positive one.
func TestBuilderRefusesAnObservationWithoutAFreshnessWindow(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, false)
	o := observation(tenantA, layer.ReadOK)
	o.Src.SourceLimit = 0
	if _, err := b.Record(ctx(t), o); err == nil {
		t.Fatal("an observation with no freshness window was accepted")
	}
}

// REQ-CTX-001, INV-19 (Codex review of noema#172): a normalised response without the hash of the raw bytes it came from is refused, and an OK
// read without a response is refused; neither may be stored as KNOWN.
func TestBuilderRefusesAPresentResponseWithoutItsRawHashAndAnOKReadWithoutAResponse(t *testing.T) {
	cases := map[string]func(o *layer.Observation){
		"a response with no raw hash":        func(o *layer.Observation) { o.RawHash = "" },
		"a response with a malformed hash":   func(o *layer.Observation) { o.RawHash = "sha256:XYZ" },
		"an OK read with no response at all": func(o *layer.Observation) { o.Payload, o.RawHash = nil, "" },
	}
	for name, mutate := range cases {
		w := newWorld(t)
		b, _ := newBuilder(t, w, tenantA, false)
		o := observation(tenantA, layer.ReadOK)
		mutate(&o)
		if item, err := b.Record(ctx(t), o); err == nil {
			t.Errorf("%s was stored: %+v", name, item)
		}
		for _, q := range []string{`SELECT count(*) FROM evidence_payload`, `SELECT count(*) FROM evidence_item`, `SELECT count(*) FROM record_event`} {
			if n := w.count(t, q); n != 0 {
				t.Errorf("%s: %s = %d after a refusal", name, q, n)
			}
		}
	}
}

// REQ-CTX-001 (Codex review of noema#172, second round): a malformed 2xx body keeps the hash of the bytes that were received, a normalised
// payload must be a JSON object (evidence-payload.v1), and the authority class stored on an item must agree with the source's authority.
func TestBuilderKeepsTheRawHashOfAMalformedBody(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, false)
	o := observation(tenantA, layer.ReadMalformed)
	o.Payload = nil
	o.RawHash = "sha256:" + strings.Repeat("cd", 32)
	if _, err := b.Record(ctx(t), o); err != nil {
		t.Fatal(err)
	}
	if got := w.rows(t, `SELECT raw_hash FROM evidence_payload`); len(got) != 1 || got[0] != o.RawHash {
		t.Fatalf("stored raw hash = %v, want the supplied %s", got, o.RawHash)
	}
	// A malformed read received bytes (possibly zero of them): it must carry their hash, so SHA-256(empty) is never invented for it.
	none := observation(tenantA, layer.ReadMalformed)
	none.Payload, none.RawHash = nil, ""
	if _, err := b.Record(ctx(t), none); err == nil {
		t.Fatal("a malformed read without its raw-body hash was accepted")
	}
	// A supplied hash that is not a hash is still refused.
	bad := observation(tenantA, layer.ReadMalformed)
	bad.Payload, bad.RawHash = nil, "sha256:XYZ"
	if _, err := b.Record(ctx(t), bad); err == nil {
		t.Fatal("a malformed raw hash was accepted")
	}
}

func TestBuilderRefusesAPayloadThatIsNotAnObject(t *testing.T) {
	for name, payload := range map[string]any{"a scalar": "text", "a number": 7, "an array": []any{1, 2}} {
		w := newWorld(t)
		b, _ := newBuilder(t, w, tenantA, false)
		o := observation(tenantA, layer.ReadOK)
		o.Payload = payload
		if item, err := b.Record(ctx(t), o); err == nil {
			t.Errorf("%s was stored: %+v", name, item)
		}
		if n := w.count(t, `SELECT count(*) FROM evidence_payload`); n != 0 {
			t.Errorf("%s: %d payloads stored after a refusal", name, n)
		}
	}
}

func TestBuilderRefusesAuthorityMetadataThatDisagrees(t *testing.T) {
	cases := map[string]struct {
		authority layer.Authority
		class     string
		ok        bool
	}{
		"authoritative source, authoritative class":      {layer.AuthAuthoritative, "AUTHORITATIVE_FOR:debit_posted_or_reversed", true},
		"corroborating source, corroborating class":      {layer.AuthCorroborating, "CORROBORATING_FOR:debit_posted_or_reversed", true},
		"no authority, class NONE":                       {layer.AuthNone, "NONE", true},
		"authoritative source, class NONE":               {layer.AuthAuthoritative, "NONE", false},
		"authoritative source, corroborating class":      {layer.AuthAuthoritative, "CORROBORATING_FOR:debit_posted_or_reversed", false},
		"corroborating source, authoritative class":      {layer.AuthCorroborating, "AUTHORITATIVE_FOR:debit_posted_or_reversed", false},
		"no authority, authoritative class":              {layer.AuthNone, "AUTHORITATIVE_FOR:debit_posted_or_reversed", false},
		"authoritative source, a class that is no class": {layer.AuthAuthoritative, "SOMETHING", false},
	}
	for name, c := range cases {
		w := newWorld(t)
		b, _ := newBuilder(t, w, tenantA, false)
		o := observation(tenantA, layer.ReadOK)
		o.Src.Authority, o.AuthorityClass = c.authority, c.class
		_, err := b.Record(ctx(t), o)
		if c.ok && err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
		if !c.ok && err == nil {
			t.Errorf("%s: stored although the pair disagrees", name)
		}
	}
}

// AGENT-25 (Integration Lead's stand-in adapter): evidence from a synthetic source is labelled on the Record. The label rides in the
// EVIDENCE_READ event; an observation without one writes the event without it.
func TestBuilderLabelsEvidenceFromASyntheticSourceOnTheRecord(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, false)
	o := observation(tenantA, layer.ReadOK)
	o.DataSource, o.Synthetic = "stand-in", true
	if _, err := b.Record(ctx(t), o); err != nil {
		t.Fatal(err)
	}
	got := w.rows(t, `SELECT payload->>'data_source' || ' ' || (payload->>'synthetic') FROM record_event WHERE event_type = 'EVIDENCE_READ'`)
	if len(got) != 1 || got[0] != "stand-in true" {
		t.Fatalf("event label = %v, want \"stand-in true\"", got)
	}
}

// AGENT-25 (Codex review of noema#172, third round): the synthetic flag is persisted on its own, so synthetic data is never unlabelled
// because its source name was left empty.
func TestBuilderLabelsSyntheticEvidenceEvenWithoutASourceName(t *testing.T) {
	w := newWorld(t)
	b, _ := newBuilder(t, w, tenantA, false)
	o := observation(tenantA, layer.ReadOK)
	o.DataSource, o.Synthetic = "", true
	if _, err := b.Record(ctx(t), o); err != nil {
		t.Fatal(err)
	}
	got := w.rows(t, `SELECT coalesce(payload->>'synthetic', 'absent') FROM record_event WHERE event_type = 'EVIDENCE_READ'`)
	if len(got) != 1 || got[0] != "true" {
		t.Fatalf("synthetic label = %v, want true", got)
	}
}
