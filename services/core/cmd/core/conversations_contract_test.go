package main

// CON-07 (noema#807), ruling A234 (interim, relayed, not yet committed), A446 (interim, relayed, not yet committed): the success answer of POST /v1/conversations is a public contract,
// contracts/schemas/conversation-response.v1.json. These tests hold the handler's REAL answers (the route of conversations.go behind the edge, with the Input Gateway, and the
// real ID minter for the correlation ID) against that schema and against the generated type, and hold a changed answer against both. Go's regexp (RE2) has no look-ahead, so the one
// look-ahead the schema's ID patterns carry (a letter somewhere in the ULID, as the Record guard's ID rule) is read out of the pattern and checked as code, and the rest of the
// pattern is compiled as written; a test requires that exact form, so a changed pattern fails here. REQ-API-001, REQ-UI-001, REQ-SEC-003, INV-12; AGENT-13. Fakes are SYNTHETIC.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/edge/edgetest"
	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/inputgateway"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
	"github.com/Noemai-org/noema/services/core/platform/clock"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

// uiLimitBytes is A446: no single response or frame above 65,536 bytes.
const uiLimitBytes = 65536

// lookahead is the one look-ahead of the schema's ID patterns: after the prefix, digits and then a letter (an ID is never all digits).
const lookahead = "(?=[0-9]*[A-Za-z])"

type convSchema struct {
	Description string `json:"description"`
	Properties  map[string]struct {
		Type    string `json:"type"`
		Pattern string `json:"pattern"`
		Const   string `json:"const"`
	} `json:"properties"`
	Required             []string `json:"required"`
	AdditionalProperties *bool    `json:"additionalProperties"`
}

func loadConvSchema(t *testing.T) convSchema {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "schemas", "conversation-response.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s convSchema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// convPattern turns a schema pattern into what Go can run: the RE2 body, and whether the look-ahead was present (it must be, for the four ID-bearing members).
func convPattern(t *testing.T, name, pattern string) (*regexp.Regexp, bool) {
	t.Helper()
	if pattern == "" {
		t.Fatalf("%s has no pattern in the schema", name)
	}
	had := strings.Contains(pattern, lookahead)
	re, err := regexp.Compile(strings.Replace(pattern, lookahead, "", 1))
	if err != nil {
		t.Fatalf("%s: pattern %q: %v", name, pattern, err)
	}
	return re, had
}

// convAccepts reports whether the schema admits this member value: the pattern as written (the look-ahead as code), the type, or the constant.
func convAccepts(t *testing.T, s convSchema, name string, v any) bool {
	t.Helper()
	p, ok := s.Properties[name]
	if !ok {
		return false
	}
	str, isStr := v.(string)
	if !isStr || p.Type != "string" {
		return false
	}
	if p.Const != "" {
		return str == p.Const
	}
	re, had := convPattern(t, name, p.Pattern)
	if !re.MatchString(str) {
		return false
	}
	if had {
		body := strings.TrimPrefix(strings.TrimPrefix(str, "/v1/conversations/"), "inv_")
		body = strings.TrimPrefix(strings.TrimPrefix(body, "corr_"), "inv_")
		if i := strings.IndexByte(body, '/'); i >= 0 {
			body = body[:i]
		}
		if !strings.ContainsAny(body, "ABCDEFGHJKMNPQRSTVWXYZ") {
			return false
		}
	}
	return true
}

// convValidates is the schema read as a validator over a decoded object: closed, all members required, each member accepted.
func convValidates(t *testing.T, s convSchema, doc map[string]any) bool {
	t.Helper()
	if s.AdditionalProperties == nil || *s.AdditionalProperties {
		return false
	}
	for k := range doc {
		if _, ok := s.Properties[k]; !ok {
			return false
		}
	}
	for _, r := range s.Required {
		if _, ok := doc[r]; !ok {
			return false
		}
	}
	for k, v := range doc {
		if !convAccepts(t, s, k, v) {
			return false
		}
	}
	return true
}

func realMinterAPI(t *testing.T, iss *edgetest.Issuer, clk *clock.Manual) http.Handler {
	t.Helper()
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	g, err := inputgateway.New(inputgateway.Config{Recorder: m, Index: m, Start: m.start, IDs: ids.New(clk, nil), MaxBody: 16384, HashKey: []byte(strings.Repeat("k", 32))})
	if err != nil {
		t.Fatal(err)
	}
	return conversationAPI(t, iss, Components{Gateway: g})
}

func decodeObject(t *testing.T, raw []byte) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("%s: %v", raw, err)
	}
	return doc
}

// The contract describes the handler: the schema is closed, requires the five members the handler writes, and holds the three limits of A234 and A446 in its own words.
func TestTheSchemaIsTheClosedFiveMemberShapeOfA234(t *testing.T) {
	s := loadConvSchema(t)
	want := []string{"investigation_id", "conversation_ref", "state", "stream_url", "correlation_id"}
	if len(s.Properties) != len(want) || len(s.Required) != len(want) {
		t.Fatalf("members %d, required %d; want exactly %v", len(s.Properties), len(s.Required), want)
	}
	for _, m := range want {
		if _, ok := s.Properties[m]; !ok {
			t.Errorf("no member %s", m)
		}
	}
	if s.AdditionalProperties == nil || *s.AdditionalProperties {
		t.Error("the schema is not closed")
	}
	if s.Properties["state"].Const != publicStageUnderstanding {
		t.Errorf("state is %q; A234 says %q", s.Properties["state"].Const, publicStageUnderstanding)
	}
	if !strings.Contains(s.Description, "65,536") {
		t.Error("the description does not state the UI limit of 65,536 bytes (A446)")
	}
}

// A234, A168: the real answer of the route, first (202) and retried (200), is a valid conversation-response.v1: the generated type reads it, the schema admits it, and
// the relations a schema cannot state hold (the two IDs are one value, the URL names that ID, the correlation ID shares the ULID).
func TestTheRealAnswerValidatesAgainstTheContract(t *testing.T) {
	s := loadConvSchema(t)
	iss := edgetest.NewIssuer(t)
	clk := clock.NewManual(apiEpoch)
	h := realMinterAPI(t, iss, clk)
	auth := bearer(iss, session.RoleCustomer, convTenant)
	for i := 0; i < 64; i++ {
		clk.Advance(1700 * time.Millisecond)
		reqID := "3f2a9c1e-5b7d-4e8f-9a1c-2d4b6e8f" + strings.Repeat("0", 4-len(strconv.Itoa(i))) + strconv.Itoa(i)
		for _, want := range []int{http.StatusAccepted, http.StatusOK} {
			rec := post(h, auth, startBody(reqID, "where is my money"))
			if rec.Code != want {
				t.Fatalf("request %d: status %d, want %d: %s", i, rec.Code, want, rec.Body.String())
			}
			var typed contracts.ConversationResponseV1
			if err := json.Unmarshal(rec.Body.Bytes(), &typed); err != nil {
				t.Fatalf("the generated type refuses the real answer %s: %v", rec.Body.String(), err)
			}
			doc := decodeObject(t, rec.Body.Bytes())
			if !convValidates(t, s, doc) {
				t.Fatalf("the schema refuses the real answer %s", rec.Body.String())
			}
			id, _ := doc["investigation_id"].(string)
			ulid := strings.TrimPrefix(id, "inv_")
			if doc["conversation_ref"] != id || doc["stream_url"] != "/v1/conversations/"+id+"/events" || doc["correlation_id"] != "corr_"+ulid || doc["state"] != "understanding" {
				t.Fatalf("the members do not agree with one another: %s", rec.Body.String())
			}
			if rec.Body.Len() >= uiLimitBytes {
				t.Fatalf("the answer is %d bytes; the UI limit is %d (A446)", rec.Body.Len(), uiLimitBytes)
			}
		}
	}
}

// The answer of the route with the fixed test ULID, byte for byte the members of A234, validates (the same ULID as the fixture contracts/fixtures/valid/conversation-response/accepted.json).
func TestTheFixtureAnswerIsTheRoutesAnswer(t *testing.T) {
	iss := edgetest.NewIssuer(t)
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	h := conversationAPI(t, iss, Components{Gateway: newGateway(t, m)})
	rec := post(h, bearer(iss, session.RoleCustomer, convTenant), startBody(aRequestID, "where is my money"))
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "contracts", "fixtures", "valid", "conversation-response", "accepted.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !jsonEqual(t, rec.Body.Bytes(), raw) {
		t.Fatalf("the route answers %s; the fixture holds %s", rec.Body.String(), raw)
	}
}

func jsonEqual(t *testing.T, a, b []byte) bool {
	t.Helper()
	x, _ := json.Marshal(decodeObject(t, a))
	y, _ := json.Marshal(decodeObject(t, b))
	return string(x) == string(y)
}

// A changed answer is refused by the schema and, where shape can say it, by the generated type: the negative of the test above.
func TestAChangedAnswerIsRefused(t *testing.T) {
	s := loadConvSchema(t)
	iss := edgetest.NewIssuer(t)
	m := &memGateway{prior: map[string]inputgateway.Prior{}}
	h := conversationAPI(t, iss, Components{Gateway: newGateway(t, m)})
	rec := post(h, bearer(iss, session.RoleCustomer, convTenant), startBody(aRequestID, "where is my money"))
	base := decodeObject(t, rec.Body.Bytes())
	if !convValidates(t, s, base) {
		t.Fatalf("the real answer is not valid: %s", rec.Body.String())
	}
	with := func(k string, v any) map[string]any {
		d := map[string]any{}
		for a, b := range base {
			d[a] = b
		}
		if v == nil {
			delete(d, k)
		} else {
			d[k] = v
		}
		return d
	}
	type tc struct {
		name  string
		doc   map[string]any
		shape bool // the generated type must refuse it too
	}
	cases := []tc{
		{"state is another stage", with("state", "working"), true},
		{"an internal member", with("event_id", "recev_"+aULID), true},
		{"the tenant", with("tenant_id", convTenant), true},
		{"a missing correlation ID", with("correlation_id", nil), true},
		{"a missing stream URL", with("stream_url", nil), true},
		{"the investigation ID has the correlation prefix", with("investigation_id", "corr_"+aULID), false},
		{"the investigation ID is not a ULID", with("investigation_id", "inv_1"), false},
		{"the ID is all digits", with("investigation_id", "inv_"+strings.Repeat("1", 26)), false},
		{"the reference has another prefix", with("conversation_ref", "conv_"+aULID), false},
		{"the URL is absolute", with("stream_url", "https://example.invalid/v1/conversations/inv_"+aULID+"/events"), false},
		{"the URL carries a query", with("stream_url", "/v1/conversations/inv_"+aULID+"/events?token=x"), false},
		{"the correlation ID is not a ULID", with("correlation_id", "corr_ABC"), false},
	}
	for _, c := range cases {
		if convValidates(t, s, c.doc) {
			t.Errorf("%s: the schema admits it", c.name)
		}
		raw, _ := json.Marshal(c.doc)
		if c.shape {
			if err := json.Unmarshal(raw, new(contracts.ConversationResponseV1)); err == nil {
				t.Errorf("%s: the generated type reads it", c.name)
			}
		}
	}
}

// A446: the largest answer the schema admits is a few hundred bytes, and the handler's answers are never near the limit.
func TestTheLargestAnswerIsFarBelowTheUILimit(t *testing.T) {
	z := strings.Repeat("Z", 26)
	doc := map[string]any{"investigation_id": "inv_" + z, "conversation_ref": "inv_" + z, "state": "understanding", "stream_url": "/v1/conversations/inv_" + z + "/events", "correlation_id": "corr_" + z}
	raw, _ := json.Marshal(doc)
	if len(raw) >= 512 || len(raw) >= uiLimitBytes {
		t.Fatalf("%d bytes", len(raw))
	}
}

// Failure is not a success answer: the error the route writes when it cannot turn the gateway's answer into an investigation is not a conversation-response.
func TestTheInternalErrorIsNotAConversationResponse(t *testing.T) {
	s := loadConvSchema(t)
	rec := httptest.NewRecorder()
	failStart(rec, httptest.NewRequest(http.MethodPost, "/v1/conversations", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status %d", rec.Code)
	}
	if convValidates(t, s, decodeObject(t, rec.Body.Bytes())) {
		t.Fatal("the error envelope validates as a conversation response")
	}
	if err := json.Unmarshal(rec.Body.Bytes(), new(contracts.ConversationResponseV1)); err == nil {
		t.Fatal("the generated type reads the error envelope")
	}
}
