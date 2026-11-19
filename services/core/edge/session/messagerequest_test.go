package session_test

// INV-12, AGENT-10, ruling A1, REQ-API-001: message-request.v1 (contracts/schemas), the body of a customer's message, names no identity. The edge's body guard refuses
// any member named like one (identityKeys); this holds the schema's member names against the guard, so a member added to the contract that the guard would refuse
// (or one that is an identity in all but name) fails here before it reaches a route. The fixtures are SYNTHETIC.

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/edge/session"
	"github.com/Noemai-org/noema/services/core/platform/ids"
)

var contractsDir = filepath.Join("..", "..", "..", "..", "contracts")

func messageRequestMembers(t *testing.T) []string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contractsDir, "schemas", "message-request.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Properties map[string]json.RawMessage `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(s.Properties))
	for n := range s.Properties {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func messageGuard(t *testing.T, k keys) (http.Handler, *int) {
	t.Helper()
	v, clk := verifier(t, k, epoch)
	idg := ids.New(clk, nil)
	guard, err := session.RefuseBodyIdentity(idg, 16384)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	h := session.Middleware(v, idg)(guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.WriteHeader(http.StatusNoContent)
	})))
	return h, &calls
}

func TestNoMemberOfAMessageRequestIsOneTheBodyGuardRefusesAsAnIdentity(t *testing.T) {
	members := messageRequestMembers(t)
	if len(members) == 0 {
		t.Fatal("message-request.v1 has no members")
	}
	k := newKeys(t)
	h, calls := messageGuard(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	for _, name := range members {
		before := *calls
		rec := post(h, tok, "application/json", `{"`+name+`":"x"}`)
		if rec.Code != http.StatusNoContent || *calls != before+1 {
			t.Errorf("member %q: %d, handler calls %d->%d: the guard refuses it as an identity (INV-12)", name, rec.Code, before, *calls)
		}
	}
}

func TestEveryValidMessageRequestFixtureReachesTheHandlerAndEveryIdentityFixtureDoesNot(t *testing.T) {
	k := newKeys(t)
	h, calls := messageGuard(t, k)
	tok := "Bearer " + mint(t, k.priv, goodHeader(), goodClaims(epoch))
	valid, err := filepath.Glob(filepath.Join(contractsDir, "fixtures", "valid", "message-request", "*.json"))
	if err != nil || len(valid) == 0 {
		t.Fatalf("no valid fixtures: %v", err)
	}
	for _, f := range valid {
		body, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		before := *calls
		if rec := post(h, tok, "application/json", string(body)); rec.Code != http.StatusNoContent || *calls != before+1 {
			t.Errorf("%s: %d, want it to reach the handler", filepath.Base(f), rec.Code)
		}
	}
	refused := 0
	for _, name := range []string{"tenant_id_in_body", "principal_in_body", "session_in_body", "role_in_body"} {
		body, err := os.ReadFile(filepath.Join(contractsDir, "fixtures", "invalid", "message-request", name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		before := *calls
		rec := post(h, tok, "application/json", string(body))
		if rec.Code != http.StatusBadRequest || *calls != before || strings.Contains(rec.Body.String(), "ten_demo") {
			t.Errorf("%s: %d, handler calls %d->%d; want 400 and no handler call", name, rec.Code, before, *calls)
		}
		refused++
	}
	if refused != 4 {
		t.Fatalf("checked %d identity fixtures, want 4", refused)
	}
}
