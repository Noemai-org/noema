package payloadread_test

// ENV-04 slice 2 (the Reconciler of noema#596; REQ-CTX-001, REQ-AUD-001; INV-12, INV-19, INV-21): the payload the Reconciler reads is the payload the snapshot sealed. The read comes back
// as PostgreSQL's jsonb text, not as the canonical bytes the item's PayloadHash covers, so the reader decodes it, makes it canonical again and refuses an item whose hash differs.
// All data is SYNTHETIC. No database: the real reader is cmd/core's trustPayloads (A228); here it is a fake that answers what jsonb would.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
)

type fake struct {
	doc    []byte
	err    error
	tenant string
	ref    string
	calls  int
}

func (f *fake) Read(_ context.Context, tenantID, ref string) ([]byte, error) {
	f.calls++
	f.tenant, f.ref = tenantID, ref
	return f.doc, f.err
}

func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return c
}

// sealed is the hash the Evidence Layer stores for a payload: "sha256:" and the SHA-256 of its RFC 8785 form (layer.Builder.Record).
func sealed(t *testing.T, payload map[string]any) string {
	t.Helper()
	canon, err := crypto.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	return "sha256:" + hex.EncodeToString(sum[:])
}

var original = map[string]any{"entries": []any{map[string]any{"entry_type": "DEBIT", "posting_status": "POSTED", "amount_minor": 2000000}}, "semantic_status": "SUCCESS", "n": 7}

func item(t *testing.T, payload map[string]any) layer.Item {
	t.Helper()
	return layer.Item{EvidenceID: "evr_AAAAAAAAAAAAAAAAAAAAAAAAAA", PayloadRef: "pay_BBBBBBBBBBBBBBBBBBBBBBBBBB", PayloadHash: sealed(t, payload), Status: "KNOWN"}
}

// jsonbText is what PostgreSQL's jsonb gives back for the same value: other key order, spaces after the colons and commas.
const jsonbText = `{"n": 7, "entries": [{"amount_minor": 2000000, "entry_type": "DEBIT", "posting_status": "POSTED"}], "semantic_status": "SUCCESS"}`

// REQ-CTX-001: the payload the snapshot sealed comes back, whatever order and spacing the store gives it in.
func TestThePayloadTheSnapshotSealedIsReturnedWhateverItsKeyOrderAndSpacing(t *testing.T) {
	f := &fake{doc: []byte(jsonbText)}
	got, err := payloadread.Reader{Payloads: f}.Payload(ctx(t), "ten_a", item(t, original))
	if err != nil {
		t.Fatal(err)
	}
	if got["semantic_status"] != "SUCCESS" || len(got["entries"].([]any)) != 1 {
		t.Fatalf("payload = %v", got)
	}
	if f.calls != 1 || f.tenant != "ten_a" || f.ref != "pay_BBBBBBBBBBBBBBBBBBBBBBBBBB" {
		t.Fatalf("the read asked for (%q, %q), %d calls: it must ask for the caller's tenant and the item's reference, once", f.tenant, f.ref, f.calls)
	}
}

// INV-12: the tenant is the caller's; nothing in the item or the payload can change it.
func TestTheTenantOfTheReadIsTheCallersAndNeverTheItemsOrThePayloads(t *testing.T) {
	f := &fake{doc: []byte(`{"tenant_id": "ten_evil", "n": 1}`)}
	it := item(t, map[string]any{"tenant_id": "ten_evil", "n": 1})
	if _, err := (payloadread.Reader{Payloads: f}).Payload(ctx(t), "ten_a", it); err != nil {
		t.Fatal(err)
	}
	if f.tenant != "ten_a" {
		t.Fatalf("tenant asked for = %q", f.tenant)
	}
}

// INV-21, INV-19: a payload that is not the one the snapshot sealed never classifies.
func TestAPayloadWhoseHashDiffersFromTheItemsIsRefused(t *testing.T) {
	tampered := strings.Replace(jsonbText, `"SUCCESS"`, `"REJECTED"`, 1)
	got, err := payloadread.Reader{Payloads: &fake{doc: []byte(tampered)}}.Payload(ctx(t), "ten_a", item(t, original))
	if !errors.Is(err, payloadread.ErrHashMismatch) || got != nil {
		t.Fatalf("got %v, %v: a changed payload must be ErrHashMismatch and return nothing", got, err)
	}
}

func TestAnItemWhoseHashIsOfAnotherPayloadIsRefused(t *testing.T) {
	other := item(t, map[string]any{"semantic_status": "SUCCESS"})
	if _, err := (payloadread.Reader{Payloads: &fake{doc: []byte(jsonbText)}}).Payload(ctx(t), "ten_a", other); !errors.Is(err, payloadread.ErrHashMismatch) {
		t.Fatalf("err = %v", err)
	}
}

// Whatever the read says, the answer is "this item cannot be classified", never a pass and never a partial object (noema#596, EL's answer 2).
func TestAnyFailureOfTheReadIsUnavailableAndNothingIsReturned(t *testing.T) {
	for name, cause := range map[string]error{
		"not found": errors.New("no payload under this reference for this tenant"), "no pool": errors.New("no Trust login is configured for this tenant"),
		"unusable": errors.New("the payload read is not usable"), "driver": errors.New("the payload read failed"), "deadline": context.DeadlineExceeded,
	} {
		got, err := payloadread.Reader{Payloads: &fake{err: cause, doc: []byte(jsonbText)}}.Payload(ctx(t), "ten_a", item(t, original))
		if !errors.Is(err, payloadread.ErrUnavailable) || !errors.Is(err, cause) || got != nil {
			t.Errorf("%s: got %v, %v", name, got, err)
		}
	}
}

// The error names what failed and never carries the payload (AGENT-13: a payload can hold account data).
func TestNoErrorCarriesThePayload(t *testing.T) {
	secret := `{"account": "0123456789"}`
	for _, err := range []error{
		func() error {
			_, e := payloadread.Reader{Payloads: &fake{doc: []byte(secret)}}.Payload(ctx(t), "ten_a", item(t, original))
			return e
		}(),
		func() error {
			_, e := payloadread.Reader{Payloads: &fake{doc: []byte(`[` + secret + `]`)}}.Payload(ctx(t), "ten_a", item(t, original))
			return e
		}(),
		func() error {
			_, e := payloadread.Reader{Payloads: &fake{doc: []byte(secret + `x`)}}.Payload(ctx(t), "ten_a", item(t, original))
			return e
		}(),
	} {
		if err == nil || strings.Contains(err.Error(), "0123456789") {
			t.Errorf("err = %v", err)
		}
	}
}

// evidence-payload.v1: a payload is a JSON object.
func TestAPayloadThatIsNotAnObjectIsRefused(t *testing.T) {
	for name, doc := range map[string]string{"array": `[1,2]`, "null": `null`, "string": `"x"`, "number": `7`, "bool": `true`, "empty": ``, "spaces": `   `} {
		if _, err := (payloadread.Reader{Payloads: &fake{doc: []byte(doc)}}).Payload(ctx(t), "ten_a", item(t, original)); !errors.Is(err, payloadread.ErrNotObject) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestTrailingDataAfterTheObjectIsRefused(t *testing.T) {
	for name, doc := range map[string]string{"second object": jsonbText + `{}`, "garbage": jsonbText + ` x`, "comma": jsonbText + `,`} {
		if _, err := (payloadread.Reader{Payloads: &fake{doc: []byte(doc)}}).Payload(ctx(t), "ten_a", item(t, original)); !errors.Is(err, payloadread.ErrNotObject) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// crypto.CanonicalJSON refuses a fraction or an exponent: a payload that holds one cannot have been sealed, so it is not the sealed one.
func TestANumberTheContractsDoNotAllowIsRefusedAndNeverRounded(t *testing.T) {
	for name, doc := range map[string]string{"fraction": `{"n": 1.5}`, "exponent": `{"n": 1e2}`, "too large": `{"n": 9007199254740993}`} {
		if _, err := (payloadread.Reader{Payloads: &fake{doc: []byte(doc)}}).Payload(ctx(t), "ten_a", item(t, original)); !errors.Is(err, payloadread.ErrNotCanonical) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

// Numbers are read exactly: an integer above 2^53 would lose digits as a float64 and could hash as another number.
func TestAnIntegerIsReadExactlyAndNotAsAFloat(t *testing.T) {
	big := map[string]any{"n": 9007199254740991} // 2^53 - 1, the contracts' ceiling
	got, err := payloadread.Reader{Payloads: &fake{doc: []byte(`{"n": 9007199254740991}`)}}.Payload(ctx(t), "ten_a", item(t, big))
	if err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(got["n"]) != "9007199254740991" {
		t.Fatalf("n = %v", got["n"])
	}
}

func TestAnUnusableCallDoesNotReadAnything(t *testing.T) {
	good := item(t, original)
	noRef, noHash, badHash, noID := good, good, good, good
	noRef.PayloadRef, noHash.PayloadHash, badHash.PayloadHash, noID.EvidenceID = "", "", "sha256:xyz", ""
	noDeadline := context.Background()
	for name, c := range map[string]struct {
		ctx    context.Context
		tenant string
		item   layer.Item
		want   error
	}{
		"no deadline": {noDeadline, "ten_a", good, payloadread.ErrNoDeadline}, "no tenant": {ctx(t), "", good, payloadread.ErrUnusable},
		"no reference": {ctx(t), "ten_a", noRef, payloadread.ErrUnusable}, "no hash": {ctx(t), "ten_a", noHash, payloadread.ErrUnusable},
		"malformed hash": {ctx(t), "ten_a", badHash, payloadread.ErrUnusable}, "no evidence id": {ctx(t), "ten_a", noID, payloadread.ErrUnusable},
	} {
		f := &fake{doc: []byte(jsonbText)}
		if _, err := (payloadread.Reader{Payloads: f}).Payload(c.ctx, c.tenant, c.item); !errors.Is(err, c.want) || f.calls != 0 {
			t.Errorf("%s: err = %v, %d reads", name, err, f.calls)
		}
	}
}

func TestAReaderWithNoPayloadSourceIsRefusedAndNeverPanics(t *testing.T) {
	if _, err := (payloadread.Reader{}).Payload(ctx(t), "ten_a", item(t, original)); !errors.Is(err, payloadread.ErrUnusable) {
		t.Fatalf("err = %v", err)
	}
}

// INV-21: the same sealed payload gives the same answer, however the store orders and spaces it. Seeded and generated: nested objects and arrays of integers, strings, booleans and null.
func TestTheHashCheckAgreesWithTheBuildersHashOverGeneratedPayloads(t *testing.T) {
	rng := rand.New(rand.NewSource(20261116)) // #nosec G404 - a test seed, not a secret
	for i := 0; i < 300; i++ {
		payload := map[string]any{"root": generate(rng, 3)} // an object whatever the generator returns at the top
		canon, err := crypto.CanonicalJSON(payload)
		if err != nil {
			t.Fatal(err)
		}
		it := item(t, payload)
		for _, doc := range [][]byte{canon, respace(t, canon)} {
			got, err := payloadread.Reader{Payloads: &fake{doc: doc}}.Payload(ctx(t), "ten_a", it)
			if err != nil || got == nil {
				t.Fatalf("payload %s: %v", canon, err)
			}
		}
		// one changed byte of a string value is a different payload
		changed := []byte(strings.Replace(string(canon), `"`, `"x`, 1))
		if len(changed) != len(canon) {
			if _, err := (payloadread.Reader{Payloads: &fake{doc: changed}}).Payload(ctx(t), "ten_a", it); err == nil {
				t.Fatalf("a changed payload was accepted: %s", changed)
			}
		}
	}
}

func generate(rng *rand.Rand, depth int) any {
	if depth == 0 {
		switch rng.Intn(4) {
		case 0:
			return rng.Intn(1_000_000)
		case 1:
			return fmt.Sprintf("v%d \"q\" é", rng.Intn(100))
		case 2:
			return rng.Intn(2) == 0
		default:
			return nil
		}
	}
	if rng.Intn(3) == 0 {
		out := make([]any, rng.Intn(4))
		for i := range out {
			out[i] = generate(rng, depth-1)
		}
		return out
	}
	out := map[string]any{}
	for i := 0; i < 1+rng.Intn(4); i++ {
		out[fmt.Sprintf("k%d_%d", rng.Intn(50), i)] = generate(rng, depth-1)
	}
	return out
}

// respace writes the same JSON with a newline and an indent between members, as a store that pretty-prints would (the members stay in the order the standard encoder gives, sorted by key).
func respace(t *testing.T, canon []byte) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(canon))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	return out
}
