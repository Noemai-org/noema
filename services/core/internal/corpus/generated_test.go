package corpus

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/internal/predicate"
)

// gen builds well-typed predicates over the corpus scope. Every predicate it returns must compile as Boolean.
type gen struct{ r *rand.Rand }

func (g *gen) pick(xs ...string) string { return xs[g.r.Intn(len(xs))] }

func (g *gen) ref(bound string) string {
	opts := []string{"subject_resource", "intent_id", "case_id", "planned_queue", "'DISPUTES'", "'tx1'"}
	switch bound {
	case "case":
		opts = append(opts, "case.id", "case.subject", "case.created_by_intent", "case.owner_queue")
	case "attachment":
		opts = append(opts, "attachment.case")
	}
	return g.pick(opts...)
}

func (g *gen) integer(depth int, bound string) string {
	opts := []string{"epoch", "0", "1", "2", "5"}
	if bound == "case" {
		opts = append(opts, "case.epoch")
	}
	base := g.pick(opts...)
	if depth > 0 && g.r.Intn(4) == 0 {
		return base + " " + g.pick("+", "-") + " " + g.pick("1", "2", "epoch")
	}
	if depth > 0 && g.r.Intn(4) == 0 {
		return "count(" + g.entity() + " where true)"
	}
	return base
}

func (g *gen) entity() string { return g.pick("case", "attachment") }

func (g *gen) timestamp(bound string) string {
	opts := []string{"now()", "now() - 72h", "now() + 1d", "now() - 30s"}
	if bound == "case" {
		opts = append(opts, "case.created_at", "case.created_at + 1h")
	}
	return g.pick(opts...)
}

func (g *gen) duration(bound string) string {
	opts := []string{"settle_window", "30s", "2m", "1h", "1d", "settle_window + 1m"}
	if bound == "case" {
		opts = append(opts, "now() - case.created_at")
	}
	return g.pick(opts...)
}

func (g *gen) comparison(bound string, depth int) string {
	kind := g.r.Intn(8)
	switch kind {
	case 0:
		return g.ref(bound) + " " + g.pick("==", "!=") + " " + g.ref(bound)
	case 1:
		return g.integer(0, bound) + " " + g.pick("==", "!=", "<", "<=", ">", ">=") + " " + g.integer(0, bound)
	case 2:
		return g.timestamp(bound) + " " + g.pick("<", "<=", ">", ">=", "==") + " " + g.timestamp(bound)
	case 3:
		return g.duration(bound) + " " + g.pick("<", ">=", "==", "!=") + " " + g.duration(bound)
	case 4:
		if bound == "case" {
			return "case.status " + g.pick("==", "!=") + " '" + g.pick("OPEN", "ROUTED", "CLOSED") + "'"
		}
		return "subject_resource.status " + g.pick("==", "!=") + " '" + g.pick("RESOLVED", "UNRESOLVED", "AMBIGUOUS", "ABSENT") + "'"
	case 5:
		if bound == "case" {
			return "case.status " + g.pick("in", "not in") + " ['OPEN','ROUTED']"
		}
		return g.ref(bound) + " " + g.pick("in", "not in") + " ['DISPUTES','FRAUD_REVIEW']"
	case 6:
		return g.pick("evidence_packet_hash", "subject_resource", "case_id") + " " + g.pick("==", "!=") + " null"
	}
	if bound == "case" {
		return "case.escalated == " + g.pick("true", "false")
	}
	return g.pick("true", "false")
}

func (g *gen) boolean(depth int, bound string) string {
	if depth == 0 {
		return g.comparison(bound, 0)
	}
	switch g.r.Intn(6) {
	case 0:
		return "(" + g.boolean(depth-1, bound) + " and " + g.boolean(depth-1, bound) + ")"
	case 1:
		return "(" + g.boolean(depth-1, bound) + " or " + g.boolean(depth-1, bound) + ")"
	case 2:
		return "not " + g.boolean(depth-1, bound)
	case 3:
		e := g.entity()
		return "exists(" + e + " where " + g.boolean(depth-1, e) + ")"
	case 4:
		e := g.entity()
		return "count(" + e + " where " + g.boolean(depth-1, e) + ") " + g.pick("==", ">=", "<") + " " + g.pick("0", "1", "2")
	}
	return g.comparison(bound, depth)
}

func generate(n int) []string {
	g := &gen{r: rand.New(rand.NewSource(20261020))}
	out := make([]string, 0, n)
	for len(out) < n {
		out = append(out, g.boolean(1+g.r.Intn(3), ""))
	}
	return out
}

type generatedCase struct {
	ID     string `json:"id"`
	Expr   string `json:"expr"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// mutate breaks one thing in a well-typed predicate and names the single error it must now produce.
func mutate(expr string) (string, string, bool) {
	switch {
	case strings.Contains(expr, "case.epoch"):
		return strings.Replace(expr, "case.epoch", "case.epocx", 1), "UNKNOWN_FIELD", true
	case strings.Contains(expr, "'OPEN'"):
		return strings.Replace(expr, "'OPEN'", "'OPENX'", 1), "UNKNOWN_ENUM_VALUE", true
	case strings.Contains(expr, "settle_window"):
		return strings.Replace(expr, "settle_window", "settle_windw", 1), "UNKNOWN_NAME", true
	}
	return "", "", false
}

func TestGeneratedPredicatesCompile(t *testing.T) {
	sc, _ := loadCorpus(t)
	for i, expr := range generate(600) {
		prog, err := predicate.Compile(expr, sc)
		if err != nil {
			t.Fatalf("#%d %q: %v", i, expr, err)
		}
		if prog.Type().Kind != predicate.KindBoolean {
			t.Fatalf("#%d %q: type %s", i, expr, prog.Type().Kind)
		}
	}
}

// Type errors are caught when a pack loads, not while a request is being decided.
func TestMutatedPredicatesAreRejectedAtLoad(t *testing.T) {
	sc, _ := loadCorpus(t)
	seen := 0
	for i, expr := range generate(600) {
		bad, code, ok := mutate(expr)
		if !ok {
			continue
		}
		seen++
		_, err := predicate.Compile(bad, sc)
		var pe *predicate.Error
		if !errors.As(err, &pe) || string(pe.Code) != code {
			t.Fatalf("#%d %q: want %s, got %v", i, bad, code, err)
		}
	}
	if seen < 100 {
		t.Fatalf("only %d mutations exercised", seen)
	}
}

func randomEnv(r *rand.Rand, sc predicate.Scope) predicate.Env {
	maybe := func(v predicate.Value) predicate.Value {
		if r.Intn(7) == 0 {
			return predicate.Value{}
		}
		return v
	}
	refs := []string{"tx1", "tx2", "c1", "DISPUTES", "FRAUD_REVIEW"}
	rv := func() predicate.Value { return maybe(predicate.Ref(refs[r.Intn(len(refs))])) }
	env := predicate.Env{Now: 1792500000, Variables: map[string]predicate.Value{}, Entities: map[string][]predicate.Row{}}
	for name, ty := range sc.Variables {
		switch ty.Kind {
		case predicate.KindReference:
			env.Variables[name] = rv()
		case predicate.KindInteger:
			env.Variables[name] = maybe(predicate.Int(int64(r.Intn(4))))
		case predicate.KindDuration:
			env.Variables[name] = maybe(predicate.Dur(int64(r.Intn(4000))))
		case predicate.KindEnum:
			env.Variables[name] = maybe(predicate.Enum(ty.Values[r.Intn(len(ty.Values))]))
		case predicate.KindString:
			env.Variables[name] = maybe(predicate.Str("h" + strconv.Itoa(r.Intn(3))))
		}
	}
	for entity, fields := range sc.Entities {
		for i := r.Intn(4); i > 0; i-- {
			row := predicate.Row{}
			for f, ty := range fields {
				switch ty.Kind {
				case predicate.KindReference:
					row[f] = rv()
				case predicate.KindInteger:
					row[f] = maybe(predicate.Int(int64(r.Intn(4))))
				case predicate.KindTimestamp:
					row[f] = maybe(predicate.Time(1792500000 - int64(r.Intn(400000))))
				case predicate.KindEnum:
					row[f] = maybe(predicate.Enum(ty.Values[r.Intn(len(ty.Values))]))
				case predicate.KindString:
					row[f] = maybe(predicate.Str("h" + strconv.Itoa(r.Intn(3))))
				case predicate.KindBoolean:
					row[f] = maybe(predicate.Bool(r.Intn(2) == 0))
				}
			}
			env.Entities[entity] = append(env.Entities[entity], row)
		}
	}
	return env
}

// A predicate that compiled can fail at run time only on a null operand or an overflow, never on a type, and it is deterministic.
func TestCompiledPredicatesFailOnlyOnNullOrOverflow(t *testing.T) {
	sc, _ := loadCorpus(t)
	r := rand.New(rand.NewSource(7))
	ok, nullErr := 0, 0
	for i, expr := range generate(600) {
		prog, err := predicate.Compile(expr, sc)
		if err != nil {
			t.Fatal(err)
		}
		for k := 0; k < 5; k++ {
			env := randomEnv(r, sc)
			a, errA := prog.Eval(env)
			b, errB := prog.Eval(env)
			if a != b || (errA == nil) != (errB == nil) {
				t.Fatalf("#%d %q: not deterministic", i, expr)
			}
			if errA != nil {
				var pe *predicate.Error
				if !errors.As(errA, &pe) || (pe.Code != predicate.CodeNullOperand && pe.Code != predicate.CodeOverflow) {
					t.Fatalf("#%d %q: unexpected run-time error %v", i, expr, errA)
				}
				nullErr++
				continue
			}
			if a.Kind != predicate.KindBoolean {
				t.Fatalf("#%d %q: result %+v is not Boolean", i, expr, a)
			}
			ok++
		}
	}
	if ok < 1000 || nullErr == 0 {
		t.Fatalf("weak coverage: %d evaluations, %d null errors", ok, nullErr)
	}
}

// generated.json is the Go generator's output, checked in so the Python reference runs the same expressions.
func TestGeneratedCorpusIsCurrent(t *testing.T) {
	var cases []generatedCase
	sc, _ := loadCorpus(t)
	for i, expr := range generate(300) {
		cases = append(cases, generatedCase{ID: "gen_" + strconv.Itoa(1000+i), Expr: expr, Result: "Boolean"})
		if bad, code, ok := mutate(expr); ok {
			cases = append(cases, generatedCase{ID: "gen_" + strconv.Itoa(1000+i) + "_mutated", Expr: bad, Error: code})
			if _, err := predicate.Compile(bad, sc); err == nil {
				t.Fatalf("%q compiled", bad)
			}
		}
	}
	want, err := json.MarshalIndent(map[string]any{
		"description": "Generated by internal/corpus (go test ./internal/corpus -run TestGeneratedCorpusIsCurrent with UPDATE_PREDICATE_CORPUS=1). Well-typed predicates and one-defect mutations over corpus.json's scope; the Python reference must give the same type or code. SYNTHETIC.",
		"cases":       cases,
	}, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	want = append(want, '\n')
	path := filepath.Join(contractsDir, "predicates/tests/generated.json")
	if os.Getenv("UPDATE_PREDICATE_CORPUS") == "1" {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	have, err := os.ReadFile(path)
	if err != nil || string(have) != string(want) {
		t.Fatal("generated.json is out of date: run with UPDATE_PREDICATE_CORPUS=1")
	}
}
