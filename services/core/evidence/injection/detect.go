package injection

import (
	"sort"
	"strings"
	"unicode"
)

// Finding is one pattern that matched in one field. It names the field and the pattern and carries no part of the value (AGENT-13). Truncated says the field was longer than the
// cap, so the part beyond it was never screened.
type Finding struct {
	Field     string
	PatternID string
	Truncated bool
}

// Result is what one screen of a set of fields found: the pack that ran, the findings, and the names of every field that was cut to the cap, whether or not it matched.
type Result struct {
	Pack            string
	Findings        []Finding
	TruncatedFields []string
}

// Detect screens the fields and returns the findings: sorted by field name, and within a field in the pack's order, at most one per field and pattern. It never panics, never changes
// its argument, and returns nothing for a nil detector.
func (d *Detector) Detect(fields map[string]string) []Finding { return d.Scan(fields).Findings }

// Scan is Detect with the truncation of every field reported.
func (d *Detector) Scan(fields map[string]string) Result {
	if d == nil {
		return Result{}
	}
	res := Result{Pack: d.id}
	names := make([]string, 0, len(fields))
	for n := range fields {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		text, cut := prepare(fields[name], d.maxRunes)
		if cut {
			res.TruncatedFields = append(res.TruncatedFields, name)
		}
		for _, p := range d.patterns {
			if p.re.MatchString(text) {
				res.Findings = append(res.Findings, Finding{Field: name, PatternID: p.id, Truncated: cut})
			}
		}
	}
	return res
}

// prepare is what the patterns see: the value with its control characters removed and its white space made plain, cut to max characters. It stops reading as soon as it holds max
// characters and sees one more, so the work on a huge value is the work of its first max characters (and of any control characters before them), never of the value.
// Removing before cutting means a run of control characters cannot push a phrase out of view, and the detector sees at least what a prompt would hold.
func prepare(value string, max int) (string, bool) {
	var b strings.Builder
	kept := 0
	for _, r := range value {
		r = plain(r)
		if r < 0 {
			continue
		}
		if kept >= max {
			return b.String(), true
		}
		b.WriteRune(r)
		kept++
	}
	return b.String(), false
}

// plain maps a line break to '\n', any other white space to ' ', drops a control or format character (zero-width and direction marks included) and keeps the rest. It returns -1 for
// a character that is dropped.
func plain(r rune) rune {
	switch {
	case r == '\n' || r == '\r' || r == '\u2028' || r == '\u2029' || r == '\u0085':
		return '\n'
	case unicode.IsSpace(r):
		return ' '
	case unicode.IsControl(r) || unicode.Is(unicode.Cf, r):
		return -1
	}
	return r
}
