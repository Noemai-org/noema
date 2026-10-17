package main

import (
	"strings"
	"unicode"
)

// coreForbiddenTerms are industry and country terms that never appear in core identifiers or
// string literals (INV-14, AGENT-04). Comments are not scanned: documentation may name what the
// core must not contain. Multi-word terms are written with underscores.
var coreForbiddenTerms = []string{"naira", "nibss", "nip", "bvn", "nuban", "kobo", "cbn", "ledger_balance", "icd", "hipaa", "msisdn"}

// benchmarkTerms mark a benchmark-aware branch in core (REQ-TST-006).
var benchmarkTerms = []string{"benchmark", "heldout", "held_out", "oracle", "bench_mode"}

// tokens splits an identifier or string into lower-case words: on non-alphanumerics, on
// lower-to-upper changes, and where an acronym ends ("NIPCode" gives "nip", "code").
func tokens(s string) []string {
	var out []string
	var cur []rune
	flush := func() {
		if len(cur) > 0 {
			out = append(out, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	runes := []rune(s)
	for i, r := range runes {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			flush()
			continue
		}
		if len(cur) > 0 && unicode.IsUpper(r) {
			prev := runes[i-1]
			nextLower := i+1 < len(runes) && unicode.IsLower(runes[i+1])
			if unicode.IsLower(prev) || unicode.IsDigit(prev) || (unicode.IsUpper(prev) && nextLower) {
				flush()
			}
		}
		cur = append(cur, r)
	}
	flush()
	return out
}

// matchTerm returns the first term found in s as a whole-word sequence.
func matchTerm(s string, terms []string) (string, bool) {
	words := tokens(s)
	for _, term := range terms {
		want := strings.Split(term, "_")
		for i := 0; i+len(want) <= len(words); i++ {
			if strings.Join(words[i:i+len(want)], "_") == term {
				return term, true
			}
		}
	}
	return "", false
}
