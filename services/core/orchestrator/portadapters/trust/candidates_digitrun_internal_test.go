package trustport

// A555 (REQ-AMB-004, AGENT-13; PERD 31.2, ruling A154): the longest run of digits in a label or a counterparty display is checked against the masking limit of the platform (four). The
// check counts a digit of any script, so Arabic-Indic, Devanagari or fullwidth digits cannot carry a number past the rule, and a run that changes script is still one run. The labels
// are SYNTHETIC; the Labeller that builds them is not built yet, so its output reaches this check through the CandidateLabeller port.

import "testing"

func TestCandDigitRunCountsADigitOfAnyScript(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"nothing", "", 0},
		{"no digit", "a transfer to the shop", 0},
		{"four ASCII digits", "ending 1234", 4},
		{"five ASCII digits", "ending 12345", 5},
		{"two runs, the longer wins", "12 and 34567", 5},
		{"a space ends the run", "123 45", 3},
		{"arabic-indic digits, five", "ending ١٢٣٤٥", 5},
		{"devanagari digits, five", "१२३४५", 5},
		{"fullwidth digits, five", "１２３４５", 5},
		{"a run that changes script is one run", "12٣٤٥", 5},
		{"four digits of another script", "١٢٣٤", 4},
		{"superscript digits count", "¹²³⁴⁵", 5},
		{"a separator between scripts ends the run", "123٤ ٥", 4},
	}
	for _, c := range cases {
		if got := candDigitRun(c.in); got != c.want {
			t.Errorf("%s: candDigitRun(%q) = %d, want %d", c.name, c.in, got, c.want)
		}
	}
}
