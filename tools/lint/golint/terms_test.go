package main

import (
	"reflect"
	"testing"
)

func TestTokens(t *testing.T) {
	cases := map[string][]string{
		"NIPCode":        {"nip", "code"},
		"ledger_balance": {"ledger", "balance"},
		"isBenchmark":    {"is", "benchmark"},
		"unipolar":       {"unipolar"},
	}
	for in, want := range cases {
		if got := tokens(in); !reflect.DeepEqual(got, want) {
			t.Errorf("tokens(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestMatchTermIsWholeWord(t *testing.T) {
	if _, ok := matchTerm("unipolar", coreForbiddenTerms); ok {
		t.Error("'nip' inside 'unipolar' must not match")
	}
	if term, ok := matchTerm("ledgerBalance", coreForbiddenTerms); !ok || term != "ledger_balance" {
		t.Errorf("want ledger_balance, got %q %v", term, ok)
	}
}
