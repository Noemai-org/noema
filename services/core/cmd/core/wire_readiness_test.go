package main

// STM-01 (the batch of 19 Nov, item 10): /readyz names every component the process has not built, in a stable order, and is green exactly when none is missing. A component
// that is missing is named by the one name the composition root gives it; the body carries those names and nothing else (AGENT-13). REQ-API-004, REQ-STM-001, REQ-SEC-004,
// INV-12. Keys and DSNs are SYNTHETIC.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// readinessOrder is the stable order in which /readyz names what is missing: every component of Components, the ports first. It is written out here, not derived from the
// code under test, so a reordering or a rename is a failure of this test.
var readinessOrder = []string{"interpreter", "evidence", "trust", "explainer", "recorder", "admissions", "investigations", "edge chain", "record reader", "input gateway"}

// readinessNameOfField maps each field of Components and of Ports to the name /readyz gives it. AccessLog is a logger the Record reader needs (buildAPI refuses a reader
// without it); it is not a component, and "edge chain" is the protected handler, which is an argument, not a field.
var readinessNameOfField = map[string]string{
	"Ports.Interpreter": "interpreter", "Ports.Evidence": "evidence", "Ports.Trust": "trust", "Ports.Explainer": "explainer", "Ports.Recorder": "recorder",
	"Ports.Admissions": "admissions", "Investigations": "investigations", "Records": "record reader", "Gateway": "input gateway",
}

func wantReason(names ...string) string { return "not configured: " + strings.Join(names, ", ") }

// zeroField sets one field of Components, named as in readinessNameOfField, to its zero value.
func zeroField(t *testing.T, c *Components, path string) {
	t.Helper()
	v := reflect.ValueOf(c).Elem()
	for _, part := range strings.Split(path, ".") {
		v = v.FieldByName(part)
		if !v.IsValid() {
			t.Fatalf("no field %s", path)
		}
	}
	v.Set(reflect.Zero(v.Type()))
}

// Every field of Components and of Ports is either named by /readyz or listed as not a component: a field added without a name would let /readyz turn green with the
// component unbuilt.
func TestEveryComponentFieldHasAReadinessName(t *testing.T) {
	var fields []string
	ct := reflect.TypeOf(Components{})
	for i := 0; i < ct.NumField(); i++ {
		switch f := ct.Field(i); f.Name {
		case "Ports":
			pt := f.Type
			for j := 0; j < pt.NumField(); j++ {
				fields = append(fields, "Ports."+pt.Field(j).Name)
			}
		case "AccessLog":
		default:
			fields = append(fields, f.Name)
		}
	}
	for _, f := range fields {
		if readinessNameOfField[f] == "" {
			t.Errorf("%s has no readiness name: /readyz would be green with it unbuilt", f)
		}
	}
	if len(fields) != len(readinessNameOfField) {
		t.Errorf("fields %v, names %v: they differ", fields, readinessNameOfField)
	}
	if len(readinessOrder) != len(readinessNameOfField)+1 { // plus the edge chain
		t.Errorf("order %v does not hold every name", readinessOrder)
	}
}

// Nothing built: every component is named, in the one stable order, however often it is asked.
func TestReadinessNamesEveryUnbuiltComponentInAStableOrder(t *testing.T) {
	want := wantReason(readinessOrder...)
	for i := 0; i < 3; i++ {
		ok, reason := composeReadiness(Components{}, nil)()
		if ok || reason != want {
			t.Fatalf("ask %d: ready %v, reason %q; want not ready and %q", i, ok, reason, want)
		}
	}
	if app, err := compose(Components{}, nil); err == nil || app != nil {
		t.Fatalf("composed with nothing built: %v", err)
	} else {
		for _, name := range readinessOrder {
			if !strings.Contains(err.Error(), name) {
				t.Errorf("the refusal %q does not name %s", err.Error(), name)
			}
		}
	}
}

// Built with fakes, the composition is green; take any one component away and the response names that one and no other.
func TestReadinessIsGreenOnlyWhenNothingIsMissing(t *testing.T) {
	if ok, reason := composeReadiness(full(), okAPI)(); !ok || reason != "" {
		t.Fatalf("a full composition: ready %v, reason %q", ok, reason)
	}
	for path, name := range readinessNameOfField {
		c := full()
		zeroField(t, &c, path)
		ok, reason := composeReadiness(c, okAPI)()
		if ok || reason != wantReason(name) {
			t.Errorf("%s removed: ready %v, reason %q; want not ready and %q", path, ok, reason, wantReason(name))
		}
		if app, err := compose(c, okAPI); err == nil || app != nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s removed: composed (%v); want a refusal that names %s", path, err, name)
		}
	}
	if ok, reason := composeReadiness(full(), nil)(); ok || reason != wantReason("edge chain") {
		t.Errorf("no protected handler: ready %v, reason %q", ok, reason)
	}
}

// Several missing at once are all named, in the stable order, whatever order they were removed in.
func TestSeveralMissingComponentsAreAllNamedInOrder(t *testing.T) {
	c := full()
	zeroField(t, &c, "Gateway")
	zeroField(t, &c, "Ports.Explainer")
	zeroField(t, &c, "Ports.Evidence")
	ok, reason := composeReadiness(c, nil)()
	if want := wantReason("evidence", "explainer", "edge chain", "input gateway"); ok || reason != want {
		t.Fatalf("ready %v, reason %q; want %q", ok, reason, want)
	}
}

func readyz(t *testing.T, check readiness) (int, map[string]string, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	newMux(check, nil).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the body %q is not a JSON object of strings: %v", rec.Body.String(), err)
	}
	return rec.Code, body, rec.Body.String()
}

// The response of /readyz for the real production build, with settings the build accepts: it names what is not built (the ports with no adapter yet, the edge chain, the
// gateway) and does not name what is; and nothing of a DSN, a key or a directory is in the body (AGENT-13).
func TestReadyzOfTheProductionBuildNamesWhatIsMissingAndLeaksNothing(t *testing.T) {
	rd, _ := dsnDir(t, map[string]string{"ten_aa": synthDSN("record_read_aa")})
	o := newOrchDirs(t, "ten_aa")
	c, closer, err := buildAll(t, envMap{EnvRecordReadDSNDir: rd, EnvOrchestratorDSNDir: o.dsn, EnvOrchestratorKeyDir: o.keys, EnvOrchestratorKeyMaxActive: "2", EnvReadTimeoutMS: "2000"})
	if err != nil {
		t.Fatal(err)
	}
	defer closer()
	code, body, raw := readyz(t, composeReadiness(c, nil))
	want := wantReason("interpreter", "evidence", "trust", "explainer", "edge chain", "input gateway")
	if code != http.StatusServiceUnavailable || body["status"] != "not_ready" || body["reason"] != want || len(body) != 2 {
		t.Fatalf("status %d, body %s; want 503, not_ready and %q", code, raw, want)
	}
	for _, secret := range []string{synthPassword, "postgres", "workflows_", "record_read_", rd, o.dsn, o.keys, "ed25519"} {
		if secret != "" && strings.Contains(raw, secret) {
			t.Errorf("the body %s carries %q", raw, secret)
		}
	}
	for _, built := range []string{"recorder", "admissions", "investigations", "record reader"} {
		if strings.Contains(body["reason"], built) {
			t.Errorf("%s is built but /readyz names it: %s", built, body["reason"])
		}
	}
}

// A fully built composition answers 200 and nothing else.
func TestReadyzIs200OnlyForAFullComposition(t *testing.T) {
	code, body, raw := readyz(t, composeReadiness(full(), okAPI))
	if code != http.StatusOK || body["status"] != "ready" || len(body) != 1 {
		t.Fatalf("status %d, body %s", code, raw)
	}
}
