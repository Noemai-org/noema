package corpus

import (
	"errors"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// REQ-VER-002: the Go evaluator accepts and rejects exactly what the Python reference does, with the same error code.
func TestPredicateCorpus(t *testing.T) {
	scope, cases := loadCorpus(t)
	if len(cases) < 50 {
		t.Fatalf("corpus too small: %d cases", len(cases))
	}
	codes := map[predicate.Code]bool{}
	for _, c := range cases {
		t.Run(c.ID, func(t *testing.T) {
			prog, err := predicate.Compile(c.Expr, scope)
			if c.Error != "" {
				var pe *predicate.Error
				if !errors.As(err, &pe) {
					t.Fatalf("want error %s, got %v", c.Error, err)
				}
				if string(pe.Code) != c.Error {
					t.Fatalf("want %s, got %s (%s)", c.Error, pe.Code, pe.Message)
				}
				codes[pe.Code] = true
				return
			}
			if err != nil {
				t.Fatalf("want type %s, got error %v", c.Result, err)
			}
			if got := prog.Type().Kind.String(); got != c.Result {
				t.Fatalf("want type %s, got %s", c.Result, got)
			}
		})
	}
	for _, want := range []predicate.Code{predicate.CodeSyntax, predicate.CodeUnknownName, predicate.CodeUnknownEntity, predicate.CodeUnknownField,
		predicate.CodeUnknownEnumValue, predicate.CodeTypeMismatch, predicate.CodeNotBoolean} {
		if !codes[want] {
			t.Errorf("the corpus never produces %s", want)
		}
	}
}
