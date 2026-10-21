package faultplan

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The loader and contracts/schemas/fault-plan.v1.json describe one format. Every valid fixture of the schema must
// load, and every invalid one must be refused, except the ones whose only fault is a rule the loader leaves to the
// schema because it concerns another component or a JSON Schema detail (listed, with the reason).
const fixtures = "../../../../contracts/fixtures/"

// Rejected by the schema alone: they describe faults for other components, whose parameters their producer checks.
// Rejected by the schema alone: each is a fault for another component, whose parameters and pairing its producer
// (the hostility proxy, Tushe's fault controller) checks. mock-case counts such a fault and ignores it.
var schemaOnly = map[string]string{
	"schema_drift_unknown_change.json":        "params of another component's effect",
	"clock_skew_not_integer.json":             "params of another component's effect",
	"paginate_page_size_zero.json":            "params of another component's effect",
	"stale_read_without_seconds.json":         "params of another component's effect",
	"state_change_from_the_proxy.json":        "effect and source pairing of another component's fault",
	"proxy_source_with_a_tushe_effect.json":   "effect and source pairing of another component's fault",
	"proxy_source_with_semantic_pending.json": "effect and source pairing of another component's fault",
	"impossible_state_without_fixture.json":   "params of another component's effect",
	"impossible_state_fixture_with_path.json": "params of another component's effect",
}

func TestContractFixturesValid(t *testing.T) {
	files, _ := filepath.Glob(fixtures + "valid/fault-plan/*.json")
	if len(files) < 5 {
		t.Fatalf("found %d valid fixtures: the path is wrong", len(files))
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(data); err != nil {
			t.Errorf("%s: the schema accepts it but the loader refuses it: %v", filepath.Base(f), err)
		}
	}
}

func TestContractFixturesInvalid(t *testing.T) {
	files, _ := filepath.Glob(fixtures + "invalid/fault-plan/*.json")
	if len(files) < 20 {
		t.Fatalf("found %d invalid fixtures: the path is wrong", len(files))
	}
	for _, f := range files {
		name := filepath.Base(f)
		if name == "expectations.json" {
			continue
		}
		data, _ := os.ReadFile(f)
		_, err := Parse(data)
		if _, skip := schemaOnly[name]; skip {
			continue
		}
		if err == nil {
			t.Errorf("%s: the schema refuses it but the loader accepts it", name)
		}
	}
	// The exemption list names real files.
	for name := range schemaOnly {
		if _, err := os.Stat(fixtures + "invalid/fault-plan/" + name); err != nil {
			t.Errorf("schemaOnly names %s, which does not exist", name)
		}
	}
}

// The effects, targets and sources the loader knows are exactly the schema's.
func TestEnumsMatchTheSchema(t *testing.T) {
	raw, err := os.ReadFile(fixtures + "../schemas/fault-plan.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for name := range allEffects {
		if !strings.Contains(text, `"`+name+`"`) {
			t.Errorf("effect %s is in the loader but not in the schema", name)
		}
	}
	for name := range targets {
		if !strings.Contains(text, `"`+name+`"`) {
			t.Errorf("target %s is in the loader but not in the schema", name)
		}
	}
	for _, op := range []string{OpCreateCase, OpFindCases, OpGetCase, OpRouteCase, OpCreateComplaint, OpFindComplaints, OpGetComplaint, OpAppendEvidence, OpFindEvidence, OpGetEvidence} {
		if !strings.Contains(text, `"`+op+`"`) {
			t.Errorf("operation %s is in the loader but not in the schema", op)
		}
	}
}
