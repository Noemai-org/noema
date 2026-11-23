package interpreterport_test

// UND-16 (A439, A460): the builder of the G1 canonical-input fixture. It is test code on purpose: the fixture is made by the real Interpreter port (the core's code, with a synthetic verified session, a fixed
// clock and an Interpreter that only keeps the request it is given), and nothing of the build is shipped. The locations of the real files are given to the generator on the command line (see
// TestWriteG1Fixture); no path of them is written in this package, because the core holds no benchmark-aware string (REQ-TST-006).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/orchestrator"
	interpreterport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/interpreter"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/understanding"
	"github.com/Noemai-org/noema/services/core/understanding/interpreter"
)

const (
	g1Tenant       = "ten_synthetic"
	g1Session      = "ses_synthetic"
	g1Locale       = "en-NG" // A460: the locale of all five cases (what the UI sends); the port sends no hint
	g1FixtureLabel = "SYNTHETIC canonical inputs: what the core's Interpreter port builds for the five inputs of the G1 inputs file (A460); regenerate with the generator named in the interpreter package's README"
)

var (
	g1InputsPath  = flag.String("g1-inputs", "", "TestWriteG1Fixture: the G1 inputs file to read")
	g1FixturePath = flag.String("g1-fixture", "", "TestWriteG1Fixture: the fixture file to write")

	errCaptured = errors.New("g1 fixture: the request was captured")

	g1Top       = map[string]bool{"label": true, "suite": true, "cases": true, "inputs_sha256": true}
	g1TopOption = map[string]bool{"canonical_locale_rule": true}
	g1CaseKeys  = map[string]bool{"case_id": true, "case_sha256": true, "channel": true, "locale_mix": true, "canonical_locale": true, "principal": true, "candidates": true, "utterances": true}
	g1Principal = map[string]bool{"principal_id": true, "roles": true}
	g1World     = map[string]bool{"beneficiaries": true, "transactions": true}
	g1Utterance = map[string]bool{"turn": true, "text": true}
)

// closedKeys reports whether the object has every key of `want` and none beyond `want` and `option`.
func closedKeys(obj map[string]any, want, option map[string]bool) bool {
	for k := range want {
		if _, ok := obj[k]; !ok {
			return false
		}
	}
	for k := range obj {
		if !want[k] && !option[k] {
			return false
		}
	}
	return true
}

func asObject(v any) (map[string]any, bool) {
	o, ok := v.(map[string]any)
	return o, ok
}

func encodeSorted(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// g1Digest is the rule of the exporter: the document without its own digest, keys sorted, two-space indent, not HTML-escaped.
func g1Digest(doc map[string]any) (string, error) {
	body := map[string]any{}
	for k, v := range doc {
		if k != "inputs_sha256" {
			body[k] = v
		}
	}
	raw, err := encodeSorted(body)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(bytes.TrimRight(raw, "\n"))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// g1ID derives a stable id from the case: the same every time (INV-21). The request id is a lower-case UUID with at least one letter; the correlation id is `corr_` and twenty letters, so no id of a request
// holds a digit run (AGENT-13).
func g1ID(caseID string) (requestID, correlationID string) {
	sum := sha256.Sum256([]byte("record-only-request:" + caseID))
	h := hex.EncodeToString(sum[:])
	nibble, err := strconv.ParseUint(h[16:17], 16, 8)
	if err != nil {
		panic(err) // a hex digit of a hex string: unreachable
	}
	variant := "89ab"[nibble%4]
	requestID = fmt.Sprintf("%s-%s-4%s-%c%s-%s", h[:8], h[8:12], h[13:16], variant, h[17:20], h[20:32])
	if strings.Trim(requestID, "0123456789-") == "" {
		requestID = "a" + requestID[1:]
	}
	letters := make([]byte, 0, 20)
	corr := sha256.Sum256([]byte("record-only-correlation:" + caseID))
	for i := 0; i < 20; i++ {
		letters = append(letters, byte('a'+int(corr[i])%26))
	}
	return requestID, "corr_" + string(letters)
}

type g1Input struct {
	CaseID    string
	Principal string
	Text      string
}

// readG1Inputs holds the file to the closed shape of the exporter and to its digest, and returns what the port needs of each case. An error names a code, never a piece of the file (AGENT-13).
func readG1Inputs(raw []byte) (string, []g1Input, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc map[string]any
	if err := dec.Decode(&doc); err != nil || doc == nil {
		return "", nil, errors.New("g1 inputs: NOT_JSON")
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return "", nil, errors.New("g1 inputs: NOT_JSON")
	}
	if !closedKeys(doc, g1Top, g1TopOption) {
		return "", nil, errors.New("g1 inputs: SHAPE")
	}
	stored, _ := doc["inputs_sha256"].(string)
	if want, err := g1Digest(doc); err != nil || want != stored {
		return "", nil, errors.New("g1 inputs: DIGEST")
	}
	cases, ok := doc["cases"].([]any)
	if !ok || len(cases) == 0 {
		return "", nil, errors.New("g1 inputs: SHAPE")
	}
	var out []g1Input
	for _, c := range cases {
		obj, ok := asObject(c)
		if !ok || !closedKeys(obj, g1CaseKeys, nil) {
			return "", nil, errors.New("g1 inputs: SHAPE")
		}
		if obj["canonical_locale"] != g1Locale {
			return "", nil, errors.New("g1 inputs: LOCALE")
		}
		caseID, _ := obj["case_id"].(string)
		pr, ok := asObject(obj["principal"])
		if !ok || !closedKeys(pr, g1Principal, nil) {
			return "", nil, errors.New("g1 inputs: SHAPE")
		}
		if w, ok := asObject(obj["candidates"]); !ok || !closedKeys(w, g1World, nil) {
			return "", nil, errors.New("g1 inputs: SHAPE")
		}
		us, ok := obj["utterances"].([]any)
		if !ok || len(us) != 1 {
			return "", nil, errors.New("g1 inputs: SHAPE")
		}
		u, ok := asObject(us[0])
		if !ok || !closedKeys(u, g1Utterance, nil) {
			return "", nil, errors.New("g1 inputs: SHAPE")
		}
		text, _ := u["text"].(string)
		principal, _ := pr["principal_id"].(string)
		if caseID == "" || principal == "" || strings.TrimSpace(text) == "" {
			return "", nil, errors.New("g1 inputs: SHAPE")
		}
		out = append(out, g1Input{CaseID: caseID, Principal: principal, Text: text})
	}
	return stored, out, nil
}

// buildG1Fixture turns the inputs file into the fixture: one canonical input per case, as the real port builds it from a verified session (the case's principal, a synthetic tenant and session) and the turn
// (the case's text), at the compile acceptance goldens' fixed instant, by an Interpreter that keeps the request it is given and stops. The fixture carries BO's digest verbatim.
func buildG1Fixture(t *testing.T, raw []byte) ([]byte, error) {
	t.Helper()
	digest, inputs, err := readG1Inputs(raw)
	if err != nil {
		return nil, err
	}
	required, err := interpreterport.RequiredFromPack(pack(t))
	if err != nil {
		t.Fatal(err)
	}
	instant := time.Date(2026, 11, 16, 12, 30, 0, 0, time.UTC)
	cases := make([]any, 0, len(inputs))
	for _, in := range inputs {
		var captured []byte
		sess := &sessions{out: interpreterport.Resolved{
			Session:   understanding.Session{PrincipalID: in.Principal, TenantID: g1Tenant, AuthnRef: g1Session, PrincipalType: "end_user", AssertedBy: "noema-poc-idp"},
			ActorKind: "customer",
		}}
		requestID, correlationID := g1ID(in.CaseID)
		// The port confirms each turn's recorded input (UND-17, A219). The stub confirms exactly this turn; the object_ref goes to the Interpreter, never into the canonical input.
		adm := g1Admissions{admission: orchestrator.Admission{EventType: "INPUT_RECEIVED", TenantID: g1Tenant, PrincipalID: in.Principal, CorrelationID: correlationID, RequestID: requestID, ObjectRef: g1ObjectRef}}
		port, err := interpreterport.New(interpreterport.Config{Sessions: sess, Interpreter: fnInterpreter(func(_ context.Context, i interpreter.Input) (interpreter.Result, error) {
			captured = append([]byte(nil), i.Request.CanonicalInput...)
			return interpreter.Result{}, errCaptured
		}), Clock: clock.NewManual(instant), PackRef: packRef, Required: required, Admissions: adm})
		if err != nil {
			t.Fatal(err)
		}
		_, err = port.Interpret(context.Background(), orchestrator.Session{TenantID: g1Tenant, PrincipalID: in.Principal, Role: "customer"}, orchestrator.Turn{
			InvestigationID: "inv_synthetic", CorrelationID: correlationID, Text: in.Text, InputReceivedRef: "recev_synthetic", RequestID: requestID})
		if !errors.Is(err, errCaptured) || len(captured) == 0 {
			return nil, errors.New("g1 fixture: the port did not build a request")
		}
		var ci map[string]any
		if err := json.Unmarshal(captured, &ci); err != nil {
			return nil, errors.New("g1 fixture: the request is not JSON")
		}
		cases = append(cases, map[string]any{"case_id": in.CaseID, "canonical_input": ci})
	}
	return encodeSorted(map[string]any{"label": g1FixtureLabel, "inputs_sha256": digest, "pack_ref": packRef, "cases": cases})
}

// TestWriteG1Fixture is the generator: `go test ./orchestrator/portadapters/interpreter/ -run TestWriteG1Fixture -g1-inputs <file> -g1-fixture <file>` reads the inputs file, builds the fixture and writes it. Without
// the two flags it does nothing, so a plain `go test ./...` neither reads nor writes anything outside the package.
func TestWriteG1Fixture(t *testing.T) {
	if *g1InputsPath == "" || *g1FixturePath == "" {
		t.Skip("the generator: pass -g1-inputs and -g1-fixture to write the fixture")
	}
	raw, err := os.ReadFile(*g1InputsPath)
	if err != nil {
		t.Fatal(err)
	}
	out, err := buildG1Fixture(t, raw)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(*g1FixturePath, out, 0o644); err != nil {
		t.Fatal(err)
	}
}

// g1ObjectRef is a SYNTHETIC minted input id (`req_` and 26 Crockford characters), the shape the Interpreter requires of the recorded input's object_ref.
const g1ObjectRef = "req_01J9X3A1B2C3D4E5F6G7H8J9K0"

// g1Admissions answers the one lookup of the turn the generator builds.
type g1Admissions struct{ admission orchestrator.Admission }

func (a g1Admissions) Lookup(context.Context, orchestrator.Session, string) (orchestrator.Admission, error) {
	return a.admission, nil
}
