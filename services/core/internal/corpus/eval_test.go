package corpus

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

type evalCase struct {
	ID     string `json:"id"`
	Expr   string `json:"expr"`
	Result any    `json:"result"`
	Error  string `json:"error"`
	Env    struct {
		Now       int64                       `json:"now"`
		Variables map[string]any              `json:"variables"`
		Entities  map[string][]map[string]any `json:"entities"`
	} `json:"env"`
}

func loadEval(t *testing.T) []evalCase {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(contractsDir, "predicates/tests/eval.json"))
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Cases []evalCase `json:"cases"`
	}
	if err := json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	return f.Cases
}

// value converts a JSON value to a runtime Value using the declared type, so 1 can be an Integer, a Timestamp or a Duration.
func value(t *testing.T, ty predicate.Type, raw any) predicate.Value {
	t.Helper()
	switch v := raw.(type) {
	case nil:
		return predicate.Value{}
	case bool:
		return predicate.Bool(v)
	case string:
		switch ty.Kind {
		case predicate.KindReference:
			return predicate.Ref(v)
		case predicate.KindEnum:
			return predicate.Enum(v)
		}
		return predicate.Str(v)
	case float64:
		switch ty.Kind {
		case predicate.KindTimestamp:
			return predicate.Time(int64(v))
		case predicate.KindDuration:
			return predicate.Dur(int64(v))
		}
		return predicate.Int(int64(v))
	}
	t.Fatalf("unsupported JSON value %v", raw)
	return predicate.Value{}
}

func buildEnv(t *testing.T, sc predicate.Scope, c evalCase) predicate.Env {
	t.Helper()
	env := predicate.Env{Now: c.Env.Now, Variables: map[string]predicate.Value{}, Entities: map[string][]predicate.Row{}}
	for name, raw := range c.Env.Variables {
		env.Variables[name] = value(t, sc.Variables[name], raw)
	}
	for entity, rows := range c.Env.Entities {
		for _, r := range rows {
			row := predicate.Row{}
			for field, raw := range r {
				row[field] = value(t, sc.Entities[entity][field], raw)
			}
			env.Entities[entity] = append(env.Entities[entity], row)
		}
	}
	return env
}

func matches(want any, got predicate.Value) bool {
	switch w := want.(type) {
	case bool:
		return got.Kind == predicate.KindBoolean && got.B == w
	case float64:
		return got.I == int64(w)
	}
	return false
}

// SoT 15.3 and 43.7 golden examples, plus null and overflow behaviour.
func TestPredicateEvalGolden(t *testing.T) {
	sc, _ := loadCorpus(t)
	for _, c := range loadEval(t) {
		t.Run(c.ID, func(t *testing.T) {
			prog, err := predicate.Compile(c.Expr, sc)
			if err != nil {
				t.Fatalf("compile: %v", err)
			}
			got, err := prog.Eval(buildEnv(t, sc, c))
			if c.Error != "" {
				var pe *predicate.Error
				if !errors.As(err, &pe) || string(pe.Code) != c.Error {
					t.Fatalf("want error %s, got %v", c.Error, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("eval: %v", err)
			}
			if !matches(c.Result, got) {
				t.Fatalf("want %v, got %+v", c.Result, got)
			}
		})
	}
}

// INV-21: the same program and the same environment give the same answer, whatever order the rows arrive in.
func TestEvalIgnoresRowOrder(t *testing.T) {
	sc, _ := loadCorpus(t)
	rng := rand.New(rand.NewSource(20261020))
	for _, c := range loadEval(t) {
		prog, err := predicate.Compile(c.Expr, sc)
		if err != nil {
			t.Fatal(err)
		}
		env := buildEnv(t, sc, c)
		want, wantErr := prog.Eval(env)
		for i := 0; i < 25; i++ {
			shuffled := predicate.Env{Now: env.Now, Variables: env.Variables, Entities: map[string][]predicate.Row{}}
			for name, rows := range env.Entities {
				cp := append([]predicate.Row(nil), rows...)
				rng.Shuffle(len(cp), func(a, b int) { cp[a], cp[b] = cp[b], cp[a] })
				shuffled.Entities[name] = cp
			}
			got, gotErr := prog.Eval(shuffled)
			if (gotErr == nil) != (wantErr == nil) || (gotErr != nil && gotErr.Error() != wantErr.Error()) || got != want {
				t.Fatalf("%s: row order changed the result: %+v/%v vs %+v/%v", c.ID, want, wantErr, got, gotErr)
			}
		}
	}
}
