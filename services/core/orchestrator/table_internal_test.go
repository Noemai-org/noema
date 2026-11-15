package orchestrator

// REQ-STM-001: a transitions.yaml that this reader does not fully understand is an error, never a smaller or larger table. Synthetic inputs.

import "testing"

const head = "version: 1\nsource: \"PERD §28.2\"\nstates: [A, B]\ntransitions:\n"

func TestAMalformedTableFailsClosed(t *testing.T) {
	good := head + "  - {from: A, to: B, on: \"x\"}\n"
	if _, err := parseTable(good); err != nil {
		t.Fatalf("the control table must parse: %v", err)
	}
	cases := map[string]string{
		"an empty file":                "",
		"no transitions":               head,
		"no version":                   "source: \"PERD §28.2\"\nstates: [A, B]\ntransitions:\n  - {from: A, to: B, on: \"x\"}\n",
		"another source":               "version: 1\nsource: \"PERD §1\"\nstates: [A, B]\ntransitions:\n  - {from: A, to: B, on: \"x\"}\n",
		"an arrow to an unknown state": head + "  - {from: A, to: C, on: \"x\"}\n",
		"an arrow from an unknown one": head + "  - {from: C, to: B, on: \"x\"}\n",
		"a repeated row":               good + "  - {from: A, to: B, on: \"x\"}\n",
		"a repeated state":             "version: 1\nsource: \"PERD §28.2\"\nstates: [A, A]\ntransitions:\n  - {from: A, to: A, on: \"x\"}\n",
		"states after transitions":     good + "states: [A, B]\n",
		"transitions before states":    "version: 1\nsource: \"PERD §28.2\"\ntransitions:\n  - {from: A, to: B, on: \"x\"}\nstates: [A, B]\n",
		"a row of another shape":       good + "  - from: A\n",
		"a row with no trigger":        head + "  - {from: A, to: B}\n",
		"a lower-case state":           head + "  - {from: a, to: B, on: \"x\"}\n",
	}
	for name, src := range cases {
		if tab, err := parseTable(src); err == nil {
			t.Errorf("%s: parsed into %v", name, tab)
		}
	}
	// One arrow may carry two triggers; it is still one arrow.
	tab, err := parseTable(good + "  - {from: A, to: B, on: \"y\"}\n")
	if err != nil || len(tab.Exits("A")) != 1 {
		t.Fatalf("two triggers on one arrow: %v, exits %v", err, tab)
	}
}
