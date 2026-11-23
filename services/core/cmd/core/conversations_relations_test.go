package main

// CON-07, ruling A479 (interim, relayed, not yet committed; the follow-up of noema#815): the writer of the answer of POST /v1/conversations refuses an answer whose members name
// different investigations, with the same checks as tools/contracts/validate.py and on the same fixtures. A schema states each member alone, never the relations between them:
// investigation_id equals conversation_ref; stream_url is /v1/conversations/<investigation_id>/events; correlation_id is corr_ and the ULID of investigation_id. The real route derives all
// five members from one correlation ID, so it cannot produce a mismatch; the refusal is the writer's own, held here through a deliberately inconsistent derivation. REQ-API-001,
// REQ-UI-001, REQ-SEC-003; INV-12; AGENT-13. All values are SYNTHETIC.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const relationsDir = "../../../../contracts/fixtures"

func readAnswerFixture(t *testing.T, kind, name string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(relationsDir, kind, "conversation-response", name))
	if err != nil {
		t.Fatal(err)
	}
	var a map[string]any
	if err := json.Unmarshal(raw, &a); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestTheConsistentAnswerPassesTheWritersCheck(t *testing.T) { // REQ-API-001
	for _, name := range []string{"accepted.json", "ulid_of_the_highest_first_character.json", "ulid_with_a_run_of_digits_and_a_letter.json"} {
		if err := checkAnswer(readAnswerFixture(t, "valid", name)); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
}

func TestTheWriterRefusesEveryRelationFixtureTheValidatorRefuses(t *testing.T) { // A479: one set of fixtures, two checkers
	raw, err := os.ReadFile(filepath.Join(relationsDir, "invalid", "conversation-response", "expectations.json"))
	if err != nil {
		t.Fatal(err)
	}
	var expectations map[string]string
	if err := json.Unmarshal(raw, &expectations); err != nil {
		t.Fatal(err)
	}
	n := 0
	for name, why := range expectations {
		if why != "conversation_id_mismatch" {
			continue
		}
		n++
		if err := checkAnswer(readAnswerFixture(t, "invalid", name)); err == nil {
			t.Errorf("%s: the writer accepted an answer whose members name different investigations", name)
		}
	}
	if n != 4 {
		t.Fatalf("%d relation fixtures, want 4", n)
	}
}

func TestTheCheckNamesNoMemberAndRefusesAnythingThatIsNotTheShape(t *testing.T) { // AGENT-13, A347: a fixed text, no value
	a := readAnswerFixture(t, "invalid", "conversation_ref_other_investigation.json")
	err := checkAnswer(a)
	if err == nil {
		t.Fatal("accepted")
	}
	for _, v := range a {
		if s, ok := v.(string); ok && strings.Contains(err.Error(), s) {
			t.Errorf("the refusal repeats a value: %q", err.Error())
		}
	}
	good := readAnswerFixture(t, "valid", "accepted.json")
	for name, bad := range map[string]map[string]any{
		"nil":                nil,
		"empty":              {},
		"a member missing":   without(good, "stream_url"),
		"a member not text":  with(good, "investigation_id", 5),
		"a member is nil":    with(good, "correlation_id", nil),
		"an id with no inv_": with(good, "investigation_id", "x"),
	} {
		if checkAnswer(bad) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func with(a map[string]any, k string, v any) map[string]any {
	c := map[string]any{}
	for kk, vv := range a {
		c[kk] = vv
	}
	c[k] = v
	return c
}

func without(a map[string]any, k string) map[string]any {
	c := with(a, k, nil)
	delete(c, k)
	return c
}

// innerGateway answers as the gateway does on success: 202 and a correlation ID.
func innerGateway(corr string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"correlation_id":"` + corr + `"}`))
	})
}

func TestTheRouteRefusesAnInconsistentDerivationAndSendsNoAnswer(t *testing.T) { // A479, AGENT-13: the envelope of every other internal failure, nothing of the answer
	const corr = "corr_" + aULID
	const other = "01ARZ3NDEKTSV4RRFFQ69G5FAW"
	mismatches := map[string]func(string) (map[string]any, error){
		"conversation_ref": func(c string) (map[string]any, error) {
			a, err := conversationAnswer(c)
			a["conversation_ref"] = "inv_" + other
			return a, err
		},
		"stream_url": func(c string) (map[string]any, error) {
			a, err := conversationAnswer(c)
			a["stream_url"] = "/v1/conversations/inv_" + other + "/events"
			return a, err
		},
		"correlation_id": func(c string) (map[string]any, error) {
			a, err := conversationAnswer(c)
			a["correlation_id"] = "corr_" + other
			return a, err
		},
	}
	for name, derive := range mismatches {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			startConversationWith(innerGateway(corr), derive).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/conversations", nil))
			if rec.Code != http.StatusInternalServerError {
				t.Fatalf("status %d, want 500: %s", rec.Code, rec.Body.String())
			}
			for _, leak := range []string{"investigation_id", "stream_url", "inv_", other, aULID} {
				if strings.Contains(rec.Body.String(), leak) {
					t.Errorf("the refusal carries %q: %s", leak, rec.Body.String())
				}
			}
		})
	}
}

func TestTheRouteStillAnswersTheConsistentDerivation(t *testing.T) { // the seam changes nothing for the real derivation
	rec := httptest.NewRecorder()
	startConversationWith(innerGateway("corr_"+aULID), conversationAnswer).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/conversations", nil))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
}
