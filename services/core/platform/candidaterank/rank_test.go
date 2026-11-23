package candidaterank

// REQ-AMB-003, INV-01, INV-14, INV-21, AGENT-16: the ranker's own rules, outside the shared vectors. All data is SYNTHETIC.

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func goodConfig() Config {
	return Config{
		Weights:       Weights{AmountBP: 3500, RecipientBP: 2500, TemporalBP: 2000, RelationshipBP: 1000, TypeDirectionBP: 1000},
		FloorBP:       7000,
		MarginBP:      3500,
		MaxAboveFloor: 4, AmountToleranceBP: 100, TimeToleranceSeconds: 7200, MaxRaw: 20,
	}
}

func TestAConfigWithEveryPartIsAccepted(t *testing.T) {
	if err := goodConfig().Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestAConfigThatIsNotUsableIsRefused(t *testing.T) {
	for name, mut := range map[string]func(*Config){
		"weights do not total the unit": func(c *Config) { c.Weights.AmountBP++ },
		"a negative weight":             func(c *Config) { c.Weights.AmountBP, c.Weights.TemporalBP = -1, c.Weights.TemporalBP+3501 },
		"floor above the unit":          func(c *Config) { c.FloorBP = 10001 },
		"negative floor":                func(c *Config) { c.FloorBP = -1 },
		"margin above the unit":         func(c *Config) { c.MarginBP = 10001 },
		"negative margin":               func(c *Config) { c.MarginBP = -1 },
		"no cap above the floor":        func(c *Config) { c.MaxAboveFloor = 0 },
		"no raw bound":                  func(c *Config) { c.MaxRaw = 0 },
		"negative amount tolerance":     func(c *Config) { c.AmountToleranceBP = -1 },
		"amount tolerance above unit":   func(c *Config) { c.AmountToleranceBP = 10001 },
		"negative time tolerance":       func(c *Config) { c.TimeToleranceSeconds = -1 },
	} {
		c := goodConfig()
		mut(&c)
		err := c.Validate()
		if !errors.Is(err, ErrConfig) {
			t.Errorf("%s: err = %v, want ErrConfig", name, err)
		}
	}
}

func TestTheZeroConfigRefusesEveryRank(t *testing.T) {
	var c Config
	if _, err := c.Rank(nil, Target{}, Context{}); !errors.Is(err, ErrConfig) {
		t.Fatalf("err = %v, want ErrConfig: a zero config must not rank", err)
	}
}

func TestRankRefusesDuplicateIdsTooManyRowsAndUnusableRows(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	row := func(id string) Candidate { return Candidate{ID: id, OccurredAt: at, Currency: "NGN"} }
	if _, err := c.Rank([]Candidate{row("cand_a"), row("cand_a")}, Target{}, Context{}); !errors.Is(err, ErrCandidates) {
		t.Errorf("duplicate ids: err = %v, want ErrCandidates", err)
	}
	var many []Candidate
	for i := 0; i <= c.MaxRaw; i++ {
		many = append(many, row("cand_"+string(rune('a'+i))))
	}
	if _, err := c.Rank(many, Target{}, Context{}); !errors.Is(err, ErrCandidates) {
		t.Errorf("past the raw bound: err = %v, want ErrCandidates", err)
	}
	for name, bad := range map[string]Candidate{
		"no id":                       {OccurredAt: at},
		"negative amount":             {ID: "cand_a", OccurredAt: at, AmountMinor: -1},
		"recipient score below 0":     {ID: "cand_a", OccurredAt: at, RecipientBP: -1},
		"recipient score above 10000": {ID: "cand_a", OccurredAt: at, RecipientBP: 10001},
		"no time":                     {ID: "cand_a"},
	} {
		if _, err := c.Rank([]Candidate{bad}, Target{}, Context{}); !errors.Is(err, ErrCandidates) {
			t.Errorf("%s: err = %v, want ErrCandidates", name, err)
		}
	}
	if _, err := c.Rank([]Candidate{row("cand_a")}, Target{HasWindow: true, From: at, To: at.Add(-time.Second)}, Context{}); !errors.Is(err, ErrCandidates) {
		t.Errorf("a window that ends before it starts: err = %v, want ErrCandidates", err)
	}
	if _, err := c.Rank([]Candidate{row("cand_a")}, Target{HasAmount: true, AmountMinor: -1}, Context{}); !errors.Is(err, ErrCandidates) {
		t.Errorf("a negative target amount: err = %v, want ErrCandidates", err)
	}
}

// The order the rows arrive in changes nothing (INV-21).
func TestTheOrderOfTheRowsChangesNothing(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	a := Candidate{ID: "cand_a", OccurredAt: at, RecipientBP: 8000, Currency: "NGN"}
	b := Candidate{ID: "cand_b", OccurredAt: at, RecipientBP: 8000, Currency: "NGN"}
	d := Candidate{ID: "cand_c", OccurredAt: at.Add(time.Minute), RecipientBP: 9000, Currency: "NGN"}
	r1, err1 := c.Rank([]Candidate{a, b, d}, Target{}, Context{})
	r2, err2 := c.Rank([]Candidate{d, b, a}, Target{}, Context{})
	if err1 != nil || err2 != nil {
		t.Fatal(err1, err2)
	}
	for i := range r1.Ranked {
		if r1.Ranked[i] != r2.Ranked[i] {
			t.Fatalf("rank %d differs by input order: %+v vs %+v", i, r1.Ranked[i], r2.Ranked[i])
		}
	}
	if r1.State != r2.State || r1.MarginBP != r2.MarginBP {
		t.Fatalf("the resolution differs by input order: %+v vs %+v", r1, r2)
	}
}

// Rank copies what it is given and returns its own slices: changing the result cannot change the input, and the reverse.
func TestRankNeitherChangesItsInputNorSharesItsResult(t *testing.T) {
	c := goodConfig()
	at := time.Date(2026, 11, 20, 9, 30, 0, 0, time.UTC)
	in := []Candidate{{ID: "cand_b", OccurredAt: at, RecipientBP: 1000, Currency: "NGN"}, {ID: "cand_a", OccurredAt: at, RecipientBP: 9000, Currency: "NGN"}}
	res, err := c.Rank(in, Target{}, Context{})
	if err != nil {
		t.Fatal(err)
	}
	if in[0].ID != "cand_b" {
		t.Fatal("Rank reordered the caller's slice")
	}
	res.Ranked[0].ID = "changed"
	again, _ := c.Rank(in, Target{}, Context{})
	if again.Ranked[0].ID == "changed" {
		t.Fatal("two results share a slice")
	}
}

func TestParseBPReadsAnExactDecimalOrFractionAndNothingElse(t *testing.T) {
	for in, want := range map[string]int64{"0": 0, "1": 10000, "0.35": 3500, "0.7": 7000, "0.70": 7000, "0.0001": 1, "7/20": 3500, "1/1": 10000, "0.01": 100} {
		got, err := ParseBP(in)
		if err != nil || got != want {
			t.Errorf("ParseBP(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", " ", ".5", "5.", "-0.1", "+0.1", "1.5", "2", "0.00001", "1e-1", "0x10", "1/3", "1/0", "0.35.1", "NaN", "Inf", "0,35", "1/2/3", "0.3 5"} {
		if _, err := ParseBP(in); !errors.Is(err, ErrNumber) {
			t.Errorf("ParseBP(%q): err = %v, want ErrNumber", in, err)
		}
	}
}

// The thresholds come from the pack and not from this package: the pack's own values, read as the decimal text they are written in (a JSON number is read as its text, never as a
// float), give the configuration the vectors use. A change of a pack value that the vectors do not follow fails here.
func TestThePacksOwnThresholdsAreTheOnesTheVectorsUse(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packs", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	var pack struct {
		IntentNodes []struct {
			Disambiguation *struct {
				Margin   json.Number `json:"auto_resolve_margin"`
				MaxAbove int         `json:"max_candidates_before_escalate"`
			} `json:"disambiguation"`
		} `json:"intent_nodes"`
		Understanding struct {
			Ranking struct {
				Weights   map[string]string `json:"weights"`
				Floor     string            `json:"absolute_floor"`
				MaxRaw    int               `json:"max_raw_candidates"`
				AmountTol string            `json:"amount_tolerance_ratio"`
				TimeTol   int64             `json:"time_tolerance_seconds"`
			} `json:"ranking"`
		} `json:"understanding"`
	}
	if err := dec.Decode(&pack); err != nil {
		t.Fatal(err)
	}
	rk := pack.Understanding.Ranking
	bp := func(s string) int64 {
		v, err := ParseBP(s)
		if err != nil {
			t.Fatalf("ParseBP(%q): %v", s, err)
		}
		return v
	}
	cfg := Config{
		Weights: Weights{AmountBP: bp(rk.Weights["amount"]), RecipientBP: bp(rk.Weights["recipient"]), TemporalBP: bp(rk.Weights["temporal"]), RelationshipBP: bp(rk.Weights["relationship"]), TypeDirectionBP: bp(rk.Weights["type_direction"])},
		FloorBP: bp(rk.Floor), AmountToleranceBP: bp(rk.AmountTol), TimeToleranceSeconds: rk.TimeTol, MaxRaw: rk.MaxRaw,
	}
	seen := 0
	for _, n := range pack.IntentNodes {
		if n.Disambiguation == nil {
			continue
		}
		seen++
		if got := bp(n.Disambiguation.Margin.String()); seen == 1 {
			cfg.MarginBP, cfg.MaxAboveFloor = got, n.Disambiguation.MaxAbove
		} else if got != cfg.MarginBP || n.Disambiguation.MaxAbove != cfg.MaxAboveFloor {
			t.Errorf("two intent nodes carry different disambiguation values")
		}
	}
	if seen == 0 {
		t.Fatal("no intent node carries disambiguation data")
	}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if cfg != vecConfig(t, loadVectors(t)) {
		t.Fatalf("the pack's thresholds %+v are not the vectors' %+v", cfg, vecConfig(t, loadVectors(t)))
	}
}

// INV-21, A560: no float in the package's code (type, literal or conversion).
func TestThePackageHoldsNoFloat(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			ast.Inspect(f, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.Ident:
					if x.Name == "float32" || x.Name == "float64" {
						t.Errorf("%s: the type %s", name, x.Name)
					}
				case *ast.BasicLit:
					if x.Kind == token.FLOAT {
						t.Errorf("%s: the float literal %s", name, x.Value)
					}
				case *ast.SelectorExpr:
					if id, ok := x.X.(*ast.Ident); ok && id.Name == "math" && (x.Sel.Name == "Floor" || x.Sel.Name == "Round" || x.Sel.Name == "Ceil") {
						t.Errorf("%s: math.%s", name, x.Sel.Name)
					}
				}
				return true
			})
		}
	}
}

// INV-01, INV-14, INV-21: the package imports only the standard library, and reads no clock.
func TestThePackageImportsOnlyTheStandardLibraryAndReadsNoClock(t *testing.T) {
	fset := token.NewFileSet()
	pkgs, err := parser.ParseDir(fset, ".", func(fi os.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	for _, pkg := range pkgs {
		for name, f := range pkg.Files {
			for _, imp := range f.Imports {
				path := strings.Trim(imp.Path.Value, `"`)
				if strings.Contains(path, ".") {
					t.Errorf("%s imports %s: only the standard library", name, path)
				}
			}
			ast.Inspect(f, func(n ast.Node) bool {
				if sel, ok := n.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && id.Name == "time" && (sel.Sel.Name == "Now" || sel.Sel.Name == "Since" || sel.Sel.Name == "Until") {
						t.Errorf("%s reads the clock with time.%s", name, sel.Sel.Name)
					}
				}
				return true
			})
		}
	}
}
