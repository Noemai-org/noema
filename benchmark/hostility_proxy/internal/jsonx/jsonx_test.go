package jsonx

import (
	"strings"
	"testing"
)

func mustDecode(t *testing.T, s string) any {
	t.Helper()
	v, err := Decode([]byte(s))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestDecodeKeepsNumbersAndRefusesTrailingData(t *testing.T) {
	v := mustDecode(t, `{"amount_minor":9007199254740991,"ratio":0.35,"big":12345678901234567890}`)
	if got := string(Encode(v)); got != `{"amount_minor":9007199254740991,"big":12345678901234567890,"ratio":0.35}` {
		t.Fatalf("numbers must survive exactly: %s", got)
	}
	if _, err := Decode([]byte(`{} {}`)); err == nil {
		t.Fatal("trailing data accepted")
	}
	if _, err := Decode([]byte(`{`)); err == nil {
		t.Fatal("truncated JSON accepted")
	}
}

func TestEncodeDoesNotEscapeHTML(t *testing.T) {
	if got := string(Encode(mustDecode(t, `{"a":"<b>&"}`))); got != `{"a":"<b>&"}` {
		t.Fatal(got)
	}
}

func TestMapStringsReachesKeysAndNestedValues(t *testing.T) {
	v := MapStrings(mustDecode(t, `{"k":["x",{"y":"x"}],"x":"v"}`), func(s string) string { return strings.ToUpper(s) })
	if got := string(Encode(v)); got != `{"K":["X",{"Y":"X"}],"X":"V"}` {
		t.Fatal(got)
	}
}

func TestDrift(t *testing.T) {
	cases := []struct{ change, field, in, out, changed string }{
		{"unknown_enum", "semantic_status", `{"switch":{"semantic_status":"SUCCESS"},"a":1}`, `{"a":1,"switch":{"semantic_status":"UNKNOWN_ENUM_VALUE"}}`, "semantic_status"},
		{"remove", "is_terminal", `{"is_terminal":true,"raw_code":"00"}`, `{"raw_code":"00"}`, "is_terminal"},
		{"rename", "raw_code", `{"raw_code":"00"}`, `{"raw_code_renamed":"00"}`, "raw_code"},
		{"add", "", `{"a":1}`, `{"a":1,"unexpected_field":"synthetic_extra_value"}`, "unexpected_field"},
		{"add", "extra", `{"a":1}`, `{"a":1,"extra":"synthetic_extra_value"}`, "extra"},
		{"remove", "", `{"b":1,"a":2}`, `{"b":1}`, "a"}, // no field: the first key by sorted order
		{"unknown_enum", "missing", `{"a":"x"}`, `{"a":"x"}`, ""},
		{"unknown_enum", "n", `{"n":5}`, `{"n":5}`, ""}, // not a string: nothing to turn into an unknown enum
		{"remove", "", `[1,2]`, `[1,2]`, ""},
	}
	for _, c := range cases {
		got, field := Drift(mustDecode(t, c.in), c.change, c.field)
		if string(Encode(got)) != c.out || field != c.changed {
			t.Errorf("%s %q on %s: got %s (%q), want %s (%q)", c.change, c.field, c.in, Encode(got), field, c.out, c.changed)
		}
	}
}

func TestSkewShiftsTimestampsOnlyAndKeepsTheirShape(t *testing.T) {
	in := `{"at":"2026-10-01T09:40:00+01:00","frac":"2026-10-01T09:40:00.250Z","not":"2026-10-01","id":"2026-10-01T09:40:00","n":5}`
	got, n := Skew(mustDecode(t, in), 90)
	want := `{"at":"2026-10-01T09:41:30+01:00","frac":"2026-10-01T09:41:30.250Z","id":"2026-10-01T09:40:00","n":5,"not":"2026-10-01"}`
	if string(Encode(got)) != want || n != 2 {
		t.Fatalf("%d shifted: %s", n, Encode(got))
	}
	back, _ := Skew(got, -90)
	if string(Encode(back)) != string(Encode(mustDecode(t, in))) {
		t.Fatalf("skew then un-skew must round trip: %s", Encode(back))
	}
}

func TestInject(t *testing.T) {
	v, field := Inject(mustDecode(t, `{"beneficiary":{"name":"Tunde A"}}`), "name", 1)
	if field != "name" || !strings.Contains(string(Encode(v)), "SYNTHETIC INJECTION TEST") {
		t.Fatalf("%s %q", Encode(v), field)
	}
	if _, field := Inject(mustDecode(t, `{"name":5}`), "name", 0); field != "" {
		t.Fatal("only a string can be replaced")
	}
	for i := -3; i < 9; i++ { // any pick is in range
		Inject(mustDecode(t, `{"name":"x"}`), "name", i)
	}
	for _, s := range Injections {
		if !strings.Contains(s, "[SYNTHETIC INJECTION TEST]") {
			t.Errorf("an injection string is not labelled: %q", s)
		}
	}
}

func TestEventArray(t *testing.T) {
	doc := mustDecode(t, `{"events":[1,2],"next":"c"}`)
	a, ok := EventArray(doc, "events")
	if !ok || len(a) != 2 {
		t.Fatal("events not found")
	}
	if got := string(Encode(SetEventArray(doc, "events", []any{"x"}))); got != `{"events":["x"],"next":"c"}` {
		t.Fatal(got)
	}
	top := mustDecode(t, `[1]`)
	if a, ok := EventArray(top, ""); !ok || len(a) != 1 {
		t.Fatal("top-level array")
	}
	if _, ok := EventArray(mustDecode(t, `{"events":"no"}`), "events"); ok {
		t.Fatal("a non-array is not an event list")
	}
}
