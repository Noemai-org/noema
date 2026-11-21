package decide_test

// A275 (REQ-AMB-004, REQ-AMB-005, INV-12; PERD section 39.2): the core mints the ID of the "None of these" choice when it builds a clarification, carries it in the Open
// clarification, and Bind reads it as the none-of-these answer. The UI submits it as option_id and never invents one. All data is SYNTHETIC; the random draws use a fixed seed.

import (
	"encoding/json"
	"errors"
	"math/rand"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/Noemai-org/noema/services/core/trust/decide"
)

// The contract pattern of an option_id and a none_option_id (clarification.v1). Go's regexp has no look-ahead, so the digit rule is read from the file and applied here by hand.
const optionIDPattern = `^(?![a-z_]*(?:[0-9][a-z_]*){5})[a-z0-9_]{1,32}$`

var optionIDBody = regexp.MustCompile(`^[a-z0-9_]{1,32}$`)

func matchesOptionID(id string) bool {
	digits := 0
	for _, c := range id {
		if c >= '0' && c <= '9' {
			digits++
		}
	}
	return optionIDBody.MatchString(id) && digits <= 4
}

func candidates(n int) []decide.Candidate {
	out := make([]decide.Candidate, 0, n)
	for i := 0; i < n; i++ {
		id := "t" + strconv.Itoa(i)
		out = append(out, decide.Candidate{ID: id, Display: "option " + string(rune('a'+i%26)) + strconv.Itoa(i), Attrs: map[string]string{}})
	}
	return out
}

func TestTheMintedNoneIDHasTheFormOfTheContractsOptionID(t *testing.T) {
	raw, err := os.ReadFile("../../../../contracts/schemas/clarification.v1.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Properties struct {
			NoneOptionID struct {
				Pattern string `json:"pattern"`
			} `json:"none_option_id"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if doc.Properties.NoneOptionID.Pattern != optionIDPattern {
		t.Fatalf("clarification.v1 holds the pattern %q, this test mirrors %q", doc.Properties.NoneOptionID.Pattern, optionIDPattern)
	}
	if !matchesOptionID(decide.NoneOptionID) {
		t.Fatalf("NoneOptionID %q does not match the contract's option_id pattern", decide.NoneOptionID)
	}
}

func TestEveryClarificationCarriesTheMintedNoneIDWhateverTheCandidateCount(t *testing.T) {
	for max := 2; max <= 40; max++ {
		for n := 0; n <= max+1; n++ {
			q, err := decide.BuildClarification(candidates(n), order, "k", decide.Limits{MaxOptions: max, MaxDigitRun: 4})
			if n < 2 || n > max {
				if err == nil {
					t.Fatalf("n=%d max=%d: a question was built where none may be asked", n, max)
				}
				continue
			}
			if err != nil {
				t.Fatalf("n=%d max=%d: %v", n, max, err)
			}
			if q.NoneOptionID == "" || q.NoneOptionID != decide.NoneOptionID {
				t.Fatalf("n=%d max=%d: none id %q, want the one the core mints", n, max, q.NoneOptionID)
			}
			if !matchesOptionID(q.NoneOptionID) {
				t.Fatalf("n=%d: none id %q is not an option_id", n, q.NoneOptionID)
			}
			for _, o := range q.Options {
				if o.ID == q.NoneOptionID {
					t.Fatalf("n=%d max=%d: the none id equals option %q", n, max, o.ID)
				}
				if !matchesOptionID(o.ID) {
					t.Fatalf("n=%d max=%d: option id %q is not an option_id", n, max, o.ID)
				}
			}
		}
	}
}

func TestBindReadsTheNoneIDAsNoneOfTheseForEveryRoundAndCandidateSet(t *testing.T) {
	r := rand.New(rand.NewSource(1275))
	for i := 0; i < 500; i++ {
		n := 2 + r.Intn(3)
		q, err := decide.BuildClarification(candidates(n), order, "k", decide.Limits{MaxOptions: 4, MaxDigitRun: 4})
		if err != nil {
			t.Fatal(err)
		}
		round, maxRounds := 1+r.Intn(3), 1+r.Intn(3)
		open := q.Open("clr_1", 2, 1000, round)
		viaID, err := decide.Bind(open, decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, Selected: open.NoneOptionID}, 10, maxRounds)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		viaFlag, err := decide.Bind(open, decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, None: true}, 10, maxRounds)
		if err != nil {
			t.Fatal(err)
		}
		if viaID.CandidateID != "" || len(viaID.Remaining) != n {
			t.Fatalf("the none id resolved or narrowed: %+v", viaID)
		}
		if viaID.Kind != viaFlag.Kind || len(viaID.Remaining) != len(viaFlag.Remaining) || len(viaID.Constraints) != 0 {
			t.Fatalf("the none id gave %+v, the flag gave %+v", viaID, viaFlag)
		}
		if round >= maxRounds && viaID.Kind != decide.EscalateRound || round < maxRounds && viaID.Kind != decide.NoneOfThese {
			t.Fatalf("round %d of %d: kind %s", round, maxRounds, viaID.Kind)
		}
	}
}

func TestAClarificationOpensWithTheNoneIDItWasBuiltWith(t *testing.T) {
	q, err := decide.BuildClarification(candidates(3), order, "k", decide.Limits{MaxOptions: 4, MaxDigitRun: 4})
	if err != nil {
		t.Fatal(err)
	}
	open := q.Open("clr_7", 3, 500, 1)
	if open.NoneOptionID == "" || open.NoneOptionID != q.NoneOptionID {
		t.Fatalf("open none id %q, clarification %q", open.NoneOptionID, q.NoneOptionID)
	}
	if open.ID != "clr_7" || open.CandidateSetVersion != 3 || open.ExpiresAt != 500 || open.Round != 1 || len(open.Options) != 3 {
		t.Fatalf("%+v", open)
	}
	for i, id := range open.Candidates {
		if id != q.Options[i].CandidateID {
			t.Fatalf("candidate %d is %q, want %q", i, id, q.Options[i].CandidateID)
		}
	}
}

func TestBindRefusesTheNoneIDWhereItDoesNotBelong(t *testing.T) {
	open := openClarification()
	base := decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2}
	rejected := base
	rejected.Rejected = []string{"opt_1", "opt_none"}
	if _, err := decide.Bind(open, rejected, 10, 2); !errors.Is(err, decide.ErrNoneRejected) {
		t.Fatalf("the none id among the rejected options: %v", err)
	}
	withRejection := base
	withRejection.Selected, withRejection.Rejected = "opt_none", []string{"opt_1"}
	if _, err := decide.Bind(open, withRejection, 10, 2); !errors.Is(err, decide.ErrNoneCombined) {
		t.Fatalf("none of these with a rejection: %v", err)
	}
	withOption := base
	withOption.Selected, withOption.None = "opt_2", true
	if _, err := decide.Bind(open, withOption, 10, 2); !errors.Is(err, decide.ErrNoneCombined) {
		t.Fatalf("an option with the none flag: %v", err)
	}
	notOffered := base
	notOffered.Selected = "opt_9"
	if _, err := decide.Bind(open, notOffered, 10, 2); !errors.Is(err, decide.ErrUnknownOption) {
		t.Fatalf("an option that was not offered: %v", err)
	}
	both := base
	both.Selected, both.None = "opt_none", true
	if b, err := decide.Bind(open, both, 10, 2); err != nil || b.Kind != decide.NoneOfThese {
		t.Fatalf("the none id with the none flag is the same answer twice: %+v %v", b, err)
	}
}

func TestBindRefusesAnOpenClarificationThatHasNoNoneIDOrWhoseNoneIDIsAnOption(t *testing.T) {
	a := decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2, None: true}
	open := openClarification()
	open.NoneOptionID = ""
	if _, err := decide.Bind(open, a, 10, 2); !errors.Is(err, decide.ErrNoNoneOption) {
		t.Fatalf("an open clarification with no none id: %v", err)
	}
	open.NoneOptionID = "opt_2" // equals an offered option
	if _, err := decide.Bind(open, a, 10, 2); !errors.Is(err, decide.ErrNoneOptionCollision) {
		t.Fatalf("a none id equal to an option id: %v", err)
	}
}

func TestTheNewErrorsHaveFixedTextsThatNameNothing(t *testing.T) {
	for _, err := range []error{decide.ErrNoNoneOption, decide.ErrNoneRejected, decide.ErrNoneCombined, decide.ErrNoneOptionCollision} {
		if got := err.Error(); !strings.HasPrefix(got, "decide: ") || strings.Contains(got, "clr_") || strings.Contains(got, "opt_") {
			t.Fatalf("%q is not a fixed text that names nothing", got)
		}
	}
}

func TestClarifyMintsTheNoneIDEvenWhenThereAreNoOptionsAndNeverTakesItFromTheCaller(t *testing.T) {
	reason, err := decide.Core("AMBIGUOUS_COUNTERPARTY")
	if err != nil {
		t.Fatal(err)
	}
	for name, q := range map[string]decide.Clarification{
		"no options, no id":     {Mode: decide.ModeOptions, QuestionKey: "k", AllowNone: true},
		"no options, forged id": {Mode: decide.ModeOptions, QuestionKey: "k", AllowNone: true, NoneOptionID: "opt_forged"},
		"options, forged id": {Mode: decide.ModeOptions, QuestionKey: "k", AllowNone: true, NoneOptionID: "opt_forged",
			Options: []decide.Option{{ID: "opt_1", Label: "a", CandidateID: "t1"}, {ID: "opt_2", Label: "b", CandidateID: "t2"}}},
	} {
		d, err := unresolved().Clarify(q, reason)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := d.Clarification(); got == nil || got.NoneOptionID != decide.NoneOptionID {
			t.Fatalf("%s: the decision carries %+v, want the id the core mints", name, got)
		}
	}
	collides := decide.Clarification{Mode: decide.ModeOptions, QuestionKey: "k", AllowNone: true, Options: []decide.Option{{ID: decide.NoneOptionID, Label: "a", CandidateID: "t1"}}}
	if d, err := unresolved().Clarify(collides, reason); !errors.Is(err, decide.ErrNoneOptionCollision) || d.Clarification() != nil {
		t.Fatalf("a clarification with an option named like the none id: %v", err)
	}
}

// A458 (clarifies A432; REQ-AMB-004, REQ-AMB-005, INV-12): none of these is one answer on its own. The none flag with a rejected option used to be accepted and the rejection was
// dropped; ErrNoneCombined covers none, as the flag or as the minted id, combined with a selected option or with a rejected one.
func TestBindRefusesNoneCombinedWithARejectedOptionWhateverTheForm(t *testing.T) {
	open := openClarification()
	base := decide.Answer{ClarificationID: "clr_1", CandidateSetVersion: 2}
	forms := map[string]decide.Answer{}
	flagRejected := base
	flagRejected.None, flagRejected.Rejected = true, []string{"opt_1"}
	forms["the none flag with a rejected option"] = flagRejected
	flagRejectedTwo := base
	flagRejectedTwo.None, flagRejectedTwo.Rejected = true, []string{"opt_1", "opt_2"}
	forms["the none flag with two rejected options"] = flagRejectedTwo
	bothRejected := base
	bothRejected.None, bothRejected.Selected, bothRejected.Rejected = true, "opt_none", []string{"opt_3"}
	forms["the none flag and the none id with a rejected option"] = bothRejected
	allThree := base
	allThree.None, allThree.Selected, allThree.Rejected = true, "opt_2", []string{"opt_1"}
	forms["the none flag, a selected option and a rejected one"] = allThree
	idRejected := base
	idRejected.Selected, idRejected.Rejected = "opt_none", []string{"opt_1"}
	forms["the none id with a rejected option"] = idRejected
	for name, a := range forms {
		if b, err := decide.Bind(open, a, 10, 2); !errors.Is(err, decide.ErrNoneCombined) {
			t.Errorf("%s: got %+v %v, want ErrNoneCombined", name, b, err)
		}
	}
}

// Property (seeded): over random clarifications and answers, an answer that names none (flag or minted id) together with any rejected option or any other selected option is
// ErrNoneCombined, and none on its own (flag, id or both) is NoneOfThese with no constraint carried over. Nothing is ever bound from an answer that is refused.
func TestBindNoneIsAloneOrRefusedForRandomAnswers(t *testing.T) {
	rng := rand.New(rand.NewSource(458))
	for i := 0; i < 400; i++ {
		n := 2 + rng.Intn(6)
		open := openOf(n)
		a := decide.Answer{ClarificationID: open.ID, CandidateSetVersion: open.CandidateSetVersion}
		none := rng.Intn(2) == 0
		noneID := rng.Intn(2) == 0
		if !none && !noneID {
			none = true
		}
		a.None = none
		if noneID {
			a.Selected = open.NoneOptionID
		}
		rejected := rng.Intn(3) == 0
		if rejected {
			a.Rejected = []string{open.Options[rng.Intn(len(open.Options))].ID}
		}
		other := !noneID && rng.Intn(3) == 0
		if other {
			a.Selected = open.Options[rng.Intn(len(open.Options))].ID
		}
		b, err := decide.Bind(open, a, open.ExpiresAt-1, 5)
		combined := rejected || other
		if a.Selected != "" && a.Rejected != nil && a.Selected == a.Rejected[0] && !noneID {
			continue // a contradictory answer has its own error; not this property
		}
		switch {
		case combined && !errors.Is(err, decide.ErrNoneCombined):
			t.Fatalf("round %d: %+v bound %+v %v, want ErrNoneCombined", i, a, b, err)
		case !combined && (err != nil || b.Kind != decide.NoneOfThese || len(b.Constraints) != 0):
			t.Fatalf("round %d: none on its own gave %+v %v", i, b, err)
		}
	}
}

func openOf(n int) decide.Open {
	o := decide.Open{ID: "clr_1", CandidateSetVersion: 2, ExpiresAt: 1000, Round: 1, NoneOptionID: "opt_none"}
	for i := 1; i <= n; i++ {
		o.Options = append(o.Options, decide.Option{ID: "opt_" + strconv.Itoa(i), CandidateID: "t" + strconv.Itoa(i)})
		o.Candidates = append(o.Candidates, "t"+strconv.Itoa(i))
	}
	return o
}
