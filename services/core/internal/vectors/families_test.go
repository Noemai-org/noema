package vectors_test

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/trust/confirm"
)

// CON-04 (REQ-API-001, REQ-EXE-002, REQ-TRU-008; INV-18, INV-21): the intent_hash, plan_hash and binding_hash families of contracts/fixtures/golden_vectors.json, which no Go test
// read before. The first two are the JCS-primitive vectors the plan names (their inputs are kept as they are; schema-valid ones follow CON-01): the RFC 8785 form of the input is the
// expected one, byte for byte, and its SHA-256 is the expected hash. The third is the real constructor of the confirmation binding. The Python side is
// tools/check_vectors.py, which `make contracts-validate` runs through tools/contracts/test_check_vectors_negatives.py (test_the_committed_fixture_passes).

type hashFamily struct {
	Name        string          `json:"name"`
	Input       json.RawMessage `json:"input"`
	ExpectedJCS string          `json:"expected_jcs"`
	IntentHash  string          `json:"expected_intent_hash"`
	PlanHash    string          `json:"expected_plan_hash"`
	BindingHash string          `json:"expected_binding_hash"`
}

func families(t *testing.T) map[string][]hashFamily {
	t.Helper()
	raw, err := os.ReadFile("../../../../contracts/fixtures/golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	out := map[string][]hashFamily{}
	for _, k := range []string{"intent_hash_vectors", "plan_hash_vectors", "binding_hash_vectors"} {
		var list []hashFamily
		if err := json.Unmarshal(all[k], &list); err != nil || len(list) == 0 {
			t.Fatalf("%s: %v (%d vectors)", k, err, len(list))
		}
		out[k] = list
	}
	return out
}

func jcsOf(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		t.Fatal(err)
	}
	b, err := crypto.CanonicalJSON(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestIntentHashAndPlanHashVectors(t *testing.T) {
	f := families(t)
	for _, c := range f["intent_hash_vectors"] {
		got := jcsOf(t, c.Input)
		if got != c.ExpectedJCS || crypto.Hash([]byte(got)) != c.IntentHash {
			t.Errorf("intent_hash %s: %s", c.Name, got)
		}
	}
	for _, c := range f["plan_hash_vectors"] {
		got := jcsOf(t, c.Input)
		if got != c.ExpectedJCS || crypto.Hash([]byte(got)) != c.PlanHash {
			t.Errorf("plan_hash %s: %s", c.Name, got)
		}
	}
}

func TestBindingHashVectors(t *testing.T) {
	for _, c := range families(t)["binding_hash_vectors"] {
		var in struct {
			PrincipalID     string `json:"principal_id"`
			IntentHash      string `json:"intent_hash"`
			CapabilityID    string `json:"capability_id"`
			TargetRef       string `json:"target_ref"`
			ParamsHash      string `json:"params_hash"`
			EvidenceSetHash string `json:"evidence_set_hash"`
			PlanHash        string `json:"plan_hash"`
			PolicyVersion   string `json:"policy_version"`
		}
		if err := json.Unmarshal(c.Input, &in); err != nil {
			t.Fatal(err)
		}
		got, err := confirm.BindingHash(confirm.BindingInput{PrincipalID: in.PrincipalID, IntentHash: in.IntentHash, CapabilityID: in.CapabilityID, TargetRef: in.TargetRef,
			ParamsHash: in.ParamsHash, EvidenceSetHash: in.EvidenceSetHash, PlanHash: in.PlanHash, PolicyVersion: in.PolicyVersion})
		if err != nil || got != c.BindingHash {
			t.Errorf("binding_hash %s: %s %v", c.Name, got, err)
		}
		if jcsOf(t, c.Input) != c.ExpectedJCS {
			t.Errorf("binding_hash %s: the JCS of the input is not the vector's", c.Name)
		}
	}
}

// Every family of the vector file has a Go test that reads it: a family added to the file without one fails here, and the file named for a family must hold its key.
func TestEveryVectorFamilyHasAGoTest(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/fixtures/golden_vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(raw, &all); err != nil {
		t.Fatal(err)
	}
	tested := map[string]string{
		"jcs_vectors":                        "vectors_test.go",
		"intent_hash_vectors":                "families_test.go",
		"plan_hash_vectors":                  "families_test.go",
		"evidence_set_hash_vectors":          "../../evidence/snapshot/hash_test.go",
		"evidence_set_hash_negative_vectors": "../../evidence/snapshot/hash_test.go",
		"binding_hash_vectors":               "families_test.go",
		"idempotency_key_vectors":            "vectors_test.go",
		"grant_signing_vectors":              "vectors_test.go",
		"record_event_chain_vectors":         "../../record/chain/vectors_test.go",
		"payload_hash_vectors":               "../../record/chain/vectors_test.go",
		"payload_rejected_vectors":           "../../record/chain/vectors_test.go",
		"record_event_chain_vectors_v3":      "../../record/chain/vectors_test.go",
	}
	for k := range all {
		if k == "description" || k == "version" {
			continue
		}
		file, ok := tested[k]
		if !ok {
			t.Errorf("the vector family %s has no Go test (add one and name it here)", k)
			continue
		}
		src, err := os.ReadFile(file)
		if err != nil || !strings.Contains(string(src), k) {
			t.Errorf("%s: %s does not read the family %s (%v)", k, file, k, err)
		}
	}
}
