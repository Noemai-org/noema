package api_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// Contract tests for every row of PERD §34.4 that mock-case serves (REQ-ENV-008, REQ-TST-003).
// Row: "Cases · Create with idempotency key".

func TestCreateCaseReturnsTheFinCasePayload(t *testing.T) {
	e := newEnv(t)
	r := e.createCase("idk_1")
	if r.status != 201 {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	for _, f := range []string{"case_id", "reference", "status", "owner_queue", "priority", "idempotency_key", "version_token", "created_at"} {
		if r.str(f) == "" {
			t.Errorf("fin.case field %q missing: %s", f, r.raw)
		}
	}
	if r.str("status") != "OPEN" || r.str("owner_queue") != "PAYMENTS_OPERATIONS" || r.str("priority") != "NORMAL" ||
		r.str("idempotency_key") != "idk_1" || r.str("reference") != "CMP-"+r.str("created_at")[:4]+"-00001" {
		t.Fatalf("unexpected case: %s", r.raw)
	}
	if r.header.Get("X-Mock-Case-Synthetic") != "true" {
		t.Error("every response must carry the SYNTHETIC marker")
	}
}

func TestSameKeySamePayloadReturnsTheOriginal(t *testing.T) {
	e := newEnv(t)
	first := e.createCase("idk_1")
	again := e.createCase("idk_1")
	if again.status != 200 || again.header.Get("Idempotent-Replay") != "true" || again.str("case_id") != first.str("case_id") || again.str("created_at") != first.str("created_at") {
		t.Fatalf("%d %s", again.status, again.raw)
	}
	if n := e.count("cases"); n != 1 {
		t.Fatalf("%d cases", n)
	}
}

func TestSameKeyDifferentPayloadIsADeterministicConflict(t *testing.T) {
	e := newEnv(t)
	first := e.createCase("idk_1")
	other := caseBody()
	other["transaction_ref"] = "TX99999"
	var codes, bodies []string
	for i := 0; i < 3; i++ {
		r := e.runtime("POST", "/v1/cases", newKey("idk_1"), other)
		if r.status != 409 || r.errCode() != "IDEMPOTENCY_CONFLICT" {
			t.Fatalf("%d %s", r.status, r.raw)
		}
		codes = append(codes, r.errCode())
		bodies = append(bodies, string(r.raw))
	}
	if bodies[0] != bodies[1] || bodies[1] != bodies[2] {
		t.Fatal("a conflict must be deterministic")
	}
	if strings.Contains(bodies[0], first.str("case_id")) {
		t.Fatal("a conflict must not leak the existing case")
	}
	if e.count("cases") != 1 {
		t.Fatal("a conflict changes nothing")
	}
}

func TestDefaultsAreEffectivePayload(t *testing.T) {
	e := newEnv(t)
	e.createCase("idk_1")
	explicit := caseBody()
	explicit["owner_queue"], explicit["priority"] = "PAYMENTS_OPERATIONS", "NORMAL"
	if r := e.runtime("POST", "/v1/cases", newKey("idk_1"), explicit); r.status != 200 {
		t.Fatalf("naming the defaults is the same payload: %d %s", r.status, r.raw)
	}
}

// Row: "Cases · Find by transaction or principal; find by idempotency key — every read returns a version token".
func TestFindCasesAndVersionTokensOnEveryRead(t *testing.T) {
	e := newEnv(t)
	c1 := e.createCase("idk_1")
	b := caseBody()
	b["transaction_ref"] = "TX2"
	e.runtime("POST", "/v1/cases", newKey("idk_2"), b)
	b2 := caseBody()
	b2["principal_id"] = "usr_2"
	e.runtime("POST", "/v1/cases", newKey("idk_3"), b2)

	byKey := e.runtime("GET", "/v1/cases?tenant_id=ten_a&idempotency_key=idk_1", nil, nil)
	if byKey.status != 200 || len(byKey.items()) != 1 || byKey.items()[0]["case_id"] != c1.str("case_id") || byKey.json["complete"] != true {
		t.Fatalf("by key: %s", byKey.raw)
	}
	none := e.runtime("GET", "/v1/cases?tenant_id=ten_a&idempotency_key=idk_missing", nil, nil)
	if none.status != 200 || len(none.items()) != 0 || !strings.Contains(string(none.raw), `"items":[]`) {
		t.Fatalf("absence is an empty list, not an error: %s", none.raw)
	}
	byPrincipal := e.runtime("GET", "/v1/cases?tenant_id=ten_a&principal_id=usr_1", nil, nil)
	if len(byPrincipal.items()) != 2 {
		t.Fatalf("principal scope: %s", byPrincipal.raw)
	}
	byTx := e.runtime("GET", "/v1/cases?tenant_id=ten_a&principal_id=usr_1&transaction_ref=TX2", nil, nil)
	if len(byTx.items()) != 1 || byTx.items()[0]["transaction_ref"] != "TX2" {
		t.Fatalf("transaction filter: %s", byTx.raw)
	}
	for _, list := range []resp{byKey, byPrincipal, byTx} {
		for _, it := range list.items() {
			if s, _ := it["version_token"].(string); s == "" {
				t.Fatalf("a read without a version token: %v", it)
			}
		}
	}
	get := e.runtime("GET", "/v1/cases/"+c1.str("case_id")+"?tenant_id=ten_a", nil, nil)
	if get.status != 200 || get.str("version_token") != "v1" {
		t.Fatalf("get: %s", get.raw)
	}
}

// Confused deputy (REQ-AMB-002, REQ-CTX-006 as a property of the case system): never a global search.
func TestCasesAreNeverSearchedGloballyOrAcrossTenants(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	for _, q := range []string{
		"/v1/cases?tenant_id=ten_a",                                // no key, no principal
		"/v1/cases?tenant_id=ten_a&transaction_ref=TX88213",        // a transaction alone is not a scope
		"/v1/cases?principal_id=usr_1",                             // no tenant
		"/v1/cases?tenant_id=ten_a&principal_id=usr_1&unknown=1",   // unknown parameter
		"/v1/cases?tenant_id=ten_a&principal_id=usr_1&limit=0",     //
		"/v1/cases?tenant_id=ten_a&principal_id=usr_1&limit=201",   //
		"/v1/cases?tenant_id=ten_a&principal_id=usr_1&cursor=nope", //
	} {
		if r := e.runtime("GET", q, nil, nil); r.status != 400 || r.errCode() != "INVALID_REQUEST" {
			t.Errorf("%s: %d %s", q, r.status, r.raw)
		}
	}
	if r := e.runtime("GET", "/v1/cases?tenant_id=ten_a&principal_id=usr_2", nil, nil); len(r.items()) != 0 {
		t.Fatalf("another principal sees nothing: %s", r.raw)
	}
	if r := e.runtime("GET", "/v1/cases?tenant_id=ten_b&idempotency_key=idk_1", nil, nil); len(r.items()) != 0 {
		t.Fatalf("another tenant sees nothing: %s", r.raw)
	}
	if r := e.runtime("GET", "/v1/cases/"+c.str("case_id")+"?tenant_id=ten_b", nil, nil); r.status != 404 {
		t.Fatalf("another tenant gets not found: %d", r.status)
	}
}

// Row: "Cases · Route with expected version — mismatch returns a distinct error and applies nothing".
func TestRouteWithExpectedVersion(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	path := "/v1/cases/" + c.str("case_id") + "/route"
	stale := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v9"})
	if stale.status != 409 || stale.errCode() != "VERSION_MISMATCH" {
		t.Fatalf("%d %s", stale.status, stale.raw)
	}
	if stale.errCode() == "IDEMPOTENCY_CONFLICT" {
		t.Fatal("the mismatch error must be distinct from the idempotency conflict")
	}
	if !strings.Contains(string(stale.raw), `"current_version_token"`) || !strings.Contains(string(stale.raw), `"v1"`) {
		t.Fatalf("the mismatch names the current token: %s", stale.raw)
	}
	read := e.runtime("GET", "/v1/cases/"+c.str("case_id")+"?tenant_id=ten_a", nil, nil)
	if read.str("owner_queue") != "PAYMENTS_OPERATIONS" || read.str("version_token") != "v1" {
		t.Fatalf("a mismatch applies nothing: %s", read.raw)
	}
	ok := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v1"})
	if ok.status != 200 || ok.str("owner_queue") != "DISPUTES" || ok.str("version_token") != "v2" {
		t.Fatalf("%d %s", ok.status, ok.raw)
	}
	again := e.runtime("POST", path, nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v1"})
	if again.status != 409 || again.errCode() != "VERSION_MISMATCH" {
		t.Fatalf("a retry of a landed route must be refused as stale: %d %s", again.status, again.raw)
	}
}

func TestRouteValidation(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	path := "/v1/cases/" + c.str("case_id") + "/route"
	bad := map[string]map[string]any{
		"queue not allowed": {"tenant_id": "ten_a", "owner_queue": "ANYWHERE", "expected_version_token": "v1"},
		"token missing":     {"tenant_id": "ten_a", "owner_queue": "DISPUTES"},
		"token malformed":   {"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "1"},
		"token zero":        {"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v0"},
		"token padded":      {"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v01"},
		"unknown field":     {"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v1", "priority": "LOW"},
		"no tenant":         {"owner_queue": "DISPUTES", "expected_version_token": "v1"},
	}
	for name, body := range bad {
		if r := e.runtime("POST", path, nil, body); r.status != 400 {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	if r := e.runtime("POST", "/v1/cases/case_missing/route", nil, map[string]any{"tenant_id": "ten_a", "owner_queue": "DISPUTES", "expected_version_token": "v1"}); r.status != 404 {
		t.Errorf("unknown case: %d", r.status)
	}
}

func TestCreateValidation(t *testing.T) {
	e := newEnv(t)
	mutate := func(f func(m map[string]any)) map[string]any {
		m := caseBody()
		f(m)
		return m
	}
	cases := map[string]struct {
		hdr  map[string]string
		body any
	}{
		"no key":            {ifAbsent(nil), caseBody()},
		"key too long":      {newKey(strings.Repeat("k", 129)), caseBody()},
		"key with space":    {newKey("idk 1"), caseBody()},
		"no tenant":         {newKey("idk_1"), mutate(func(m map[string]any) { delete(m, "tenant_id") })},
		"no principal":      {newKey("idk_1"), mutate(func(m map[string]any) { delete(m, "principal_id") })},
		"no transaction":    {newKey("idk_1"), mutate(func(m map[string]any) { delete(m, "transaction_ref") })},
		"no reason":         {newKey("idk_1"), mutate(func(m map[string]any) { delete(m, "reason_code") })},
		"queue not allowed": {newKey("idk_1"), mutate(func(m map[string]any) { m["owner_queue"] = "ANYWHERE" })},
		"priority unknown":  {newKey("idk_1"), mutate(func(m map[string]any) { m["priority"] = "URGENT" })},
		"unknown field":     {newKey("idk_1"), mutate(func(m map[string]any) { m["status"] = "CLOSED" })},
		"status in body":    {newKey("idk_1"), mutate(func(m map[string]any) { m["case_id"] = "case_x" })},
		"not json":          {newKey("idk_1"), "{nope"},
		"trailing data":     {newKey("idk_1"), `{"tenant_id":"ten_a"} {}`},
		"array":             {newKey("idk_1"), `[]`},
	}
	for name, tc := range cases {
		if r := e.runtime("POST", "/v1/cases", tc.hdr, tc.body); r.status != 400 || r.errCode() != "INVALID_REQUEST" {
			t.Errorf("%s: %d %s", name, r.status, r.raw)
		}
	}
	r, err := e.send(e.client, "POST", e.caseURL+"/v1/cases", runtimeToken, map[string]string{"Idempotency-Key": "idk_1", "If-None-Match": "*", "Content-Type": "text/plain"}, `{}`)
	if err != nil || r.status != 415 {
		t.Errorf("content type: %v %d", err, r.status)
	}
	big := strings.Repeat("a", 2<<20)
	if r := e.runtime("POST", "/v1/cases/case_x/evidence", key("idk_1"), map[string]any{"tenant_id": "ten_a", "payload": big}); r.status != 413 {
		t.Errorf("oversized body: %d", r.status)
	}
	if e.count("cases") != 0 {
		t.Fatal("no refused request may create a case")
	}
}

// Row: "Complaints · Create reference; read back — same idempotency and readback rules as cases".
func TestComplaintReferences(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	path := "/v1/cases/" + c.str("case_id") + "/complaints"
	body := map[string]any{"tenant_id": "ten_a"}
	first := e.runtime("POST", path, key("idk_c1"), body)
	if first.status != 201 {
		t.Fatalf("%d %s", first.status, first.raw)
	}
	for _, f := range []string{"complaint_ref", "case_id", "idempotency_key", "logged_at", "clock_started_at"} {
		if first.str(f) == "" {
			t.Errorf("fin.complaint_ref field %q missing: %s", f, first.raw)
		}
	}
	again := e.runtime("POST", path, key("idk_c1"), body)
	if again.status != 200 || again.str("complaint_ref") != first.str("complaint_ref") || again.header.Get("Idempotent-Replay") != "true" {
		t.Fatalf("replay: %d %s", again.status, again.raw)
	}
	other := e.createCaseFor("idk_2", 2)
	conflict := e.runtime("POST", "/v1/cases/"+other.str("case_id")+"/complaints", key("idk_c1"), body)
	if conflict.status != 409 || conflict.errCode() != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("same key, different case: %d %s", conflict.status, conflict.raw)
	}
	read := e.runtime("GET", "/v1/complaints/"+first.str("complaint_ref")+"?tenant_id=ten_a", nil, nil)
	if read.status != 200 || read.str("case_id") != c.str("case_id") {
		t.Fatalf("read back: %d %s", read.status, read.raw)
	}
	byKey := e.runtime("GET", "/v1/complaints?tenant_id=ten_a&idempotency_key=idk_c1", nil, nil)
	byCase := e.runtime("GET", "/v1/complaints?tenant_id=ten_a&case_id="+c.str("case_id"), nil, nil)
	if len(byKey.items()) != 1 || len(byCase.items()) != 1 {
		t.Fatalf("find: %s %s", byKey.raw, byCase.raw)
	}
	if r := e.runtime("POST", "/v1/cases/case_missing/complaints", key("idk_c9"), body); r.status != 404 {
		t.Fatalf("unknown case: %d", r.status)
	}
	if r := e.runtime("GET", "/v1/complaints/"+first.str("complaint_ref")+"?tenant_id=ten_b", nil, nil); r.status != 404 {
		t.Fatalf("another tenant: %d", r.status)
	}
	if r := e.runtime("GET", "/v1/complaints?tenant_id=ten_a", nil, nil); r.status != 400 {
		t.Fatalf("a complaint list needs a scope: %d", r.status)
	}
}

// Row: "Evidence · Append attachment; read back — append-only, with a payload hash".
func TestEvidenceAppendOnlyWithPayloadHash(t *testing.T) {
	e := newEnv(t)
	c := e.createCase("idk_1")
	path := "/v1/cases/" + c.str("case_id") + "/evidence"
	payload := `{"note":"synthetic investigation note"}`
	first := e.runtime("POST", path, key("idk_e1"), map[string]any{"tenant_id": "ten_a", "payload": payload})
	if first.status != 201 {
		t.Fatalf("%d %s", first.status, first.raw)
	}
	for _, f := range []string{"attachment_id", "case_id", "payload_hash", "idempotency_key", "attached_at"} {
		if first.str(f) == "" {
			t.Errorf("fin.evidence_attachment field %q missing: %s", f, first.raw)
		}
	}
	if !strings.HasPrefix(first.str("payload_hash"), "sha256:") || len(first.str("payload_hash")) != 71 {
		t.Fatalf("hash: %s", first.str("payload_hash"))
	}
	// A client that hashes its planned payload gets the same value back (the pack compares payload_hash).
	withHash := e.runtime("POST", path, key("idk_e1"), map[string]any{"tenant_id": "ten_a", "payload": payload, "payload_hash": first.str("payload_hash")})
	if withHash.status != 200 || withHash.str("attachment_id") != first.str("attachment_id") {
		t.Fatalf("replay with the hash: %d %s", withHash.status, withHash.raw)
	}
	wrong := e.runtime("POST", path, key("idk_e2"), map[string]any{"tenant_id": "ten_a", "payload": payload, "payload_hash": "sha256:" + strings.Repeat("0", 64)})
	if wrong.status != 422 || wrong.errCode() != "PAYLOAD_HASH_MISMATCH" {
		t.Fatalf("%d %s", wrong.status, wrong.raw)
	}
	edited := e.runtime("POST", path, key("idk_e1"), map[string]any{"tenant_id": "ten_a", "payload": payload + " edited"})
	if edited.status != 409 || edited.errCode() != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("same key, new payload is a conflict, never an edit: %d %s", edited.status, edited.raw)
	}
	read := e.runtime("GET", "/v1/evidence/"+first.str("attachment_id")+"?tenant_id=ten_a", nil, nil)
	if read.status != 200 || read.str("payload") != payload || read.str("payload_hash") != first.str("payload_hash") {
		t.Fatalf("read back: %s", read.raw)
	}
	if n := e.count("evidence"); n != 1 {
		t.Fatalf("%d attachments", n)
	}
	for _, q := range []string{"idempotency_key=idk_e1", "case_id=" + c.str("case_id")} {
		if r := e.runtime("GET", "/v1/evidence?tenant_id=ten_a&"+q, nil, nil); len(r.items()) != 1 {
			t.Errorf("find %s: %s", q, r.raw)
		}
	}
	if r := e.runtime("POST", path, key("idk_e3"), map[string]any{"tenant_id": "ten_a", "payload": ""}); r.status != 400 {
		t.Errorf("empty payload: %d", r.status)
	}
}

func TestPagination(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.createCaseFor(fmt.Sprintf("idk_%d", i), i)
	}
	var refs []string
	cursor := ""
	for page := 0; page < 10; page++ {
		q := "/v1/cases?tenant_id=ten_a&principal_id=usr_1&limit=2"
		if cursor != "" {
			q += "&cursor=" + cursor
		}
		r := e.runtime("GET", q, nil, nil)
		for _, it := range r.items() {
			refs = append(refs, it["reference"].(string))
		}
		if r.json["complete"] == true {
			if _, has := r.json["next_cursor"]; has {
				t.Fatal("a complete page has no next_cursor")
			}
			break
		}
		cursor, _ = r.json["next_cursor"].(string)
		if cursor == "" {
			t.Fatal("an incomplete page must carry next_cursor")
		}
	}
	if len(refs) != 5 {
		t.Fatalf("saw %v", refs)
	}
}

func TestErrorEnvelopeAndCorrelation(t *testing.T) {
	e := newEnv(t)
	r := e.runtime("GET", "/v1/cases/nope?tenant_id=ten_a", map[string]string{"Noema-Correlation-Id": "corr_01ABCDEF"}, nil)
	if r.status != 404 || r.errCode() != "NOT_FOUND" || r.str("error", "correlation_id") != "corr_01ABCDEF" {
		t.Fatalf("%d %s", r.status, r.raw)
	}
	errObj := r.json["error"].(map[string]any)
	if _, ok := errObj["retryable"].(bool); !ok || errObj["message"] == "" {
		t.Fatalf("envelope: %s", r.raw)
	}
	bad := e.runtime("GET", "/v1/cases/nope?tenant_id=ten_a", map[string]string{"Noema-Correlation-Id": "not valid"}, nil)
	if bad.str("error", "correlation_id") != "" {
		t.Fatal("a malformed correlation ID is not echoed")
	}
}

func TestHealthzNeedsNoCredentialAndNothingElseDoes(t *testing.T) {
	e := newEnv(t)
	if r := e.do("GET", e.caseURL+"/healthz", "", nil, nil); r.status != 200 {
		t.Fatalf("healthz: %d", r.status)
	}
	if r := e.do("GET", e.caseURL+"/v1/cases?tenant_id=ten_a&principal_id=usr_1", "", nil, nil); r.status != 401 {
		t.Fatalf("%d", r.status)
	}
	if r := e.do("POST", e.caseURL+"/healthz", "", nil, nil); r.status != 401 {
		t.Fatalf("only GET /healthz is open: %d", r.status)
	}
	_ = http.StatusOK
}
