package layer_test

import (
	"math/rand"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/layer"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// REQ-CTX-002, REQ-CTX-001, INV-19, INV-23: the status of one read is assigned by one pure, total function, and absence is never a
// negative fact. Times are SYNTHETIC.

const (
	known        = contracts.EnumsV1FactStatusKNOWN
	stale        = contracts.EnumsV1FactStatusSTALE
	unavailable  = contracts.EnumsV1FactStatusUNAVAILABLE
	unknown      = contracts.EnumsV1FactStatusUNKNOWN
	unauthorized = contracts.EnumsV1FactStatusUNAUTHORIZED
	minute       = time.Minute
	fiveMinutes  = 5 * time.Minute
	oneMinuteSLA = time.Minute
)

var now = time.Date(2026, 11, 4, 9, 0, 0, 0, time.UTC) // SYNTHETIC

var authoritative = layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: fiveMinutes}

func ok(readAt time.Time) layer.Read {
	return layer.Read{Class: layer.ReadOK, Determinate: true, ReadAt: readAt}
}

// PERD §21.1 table, the rows AssignStatus can decide alone.
func TestEachDecidableRowOfTheStatusTable(t *testing.T) {
	for name, tc := range map[string]struct {
		read layer.Read
		src  layer.Source
		want contracts.EnumsV1FactStatus
	}{
		"an authoritative well-formed read within the limit is KNOWN":      {ok(now.Add(-time.Minute)), authoritative, known},
		"a read exactly at the limit is still KNOWN":                       {ok(now.Add(-fiveMinutes)), authoritative, known},
		"a read one nanosecond past the limit is STALE":                    {ok(now.Add(-fiveMinutes - time.Nanosecond)), authoritative, stale},
		"the profile can tighten the limit":                                {ok(now.Add(-2 * time.Minute)), layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: fiveMinutes, ProfileLimit: oneMinuteSLA}, stale},
		"the profile cannot loosen the limit":                              {ok(now.Add(-2 * time.Minute)), layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: oneMinuteSLA, ProfileLimit: fiveMinutes}, stale},
		"a timeout is UNAVAILABLE":                                         {layer.Read{Class: layer.ReadTimeout, ReadAt: now}, authoritative, unavailable},
		"a 5xx is UNAVAILABLE":                                             {layer.Read{Class: layer.ReadServerError, ReadAt: now}, authoritative, unavailable},
		"a 429 beyond the budget is UNAVAILABLE":                           {layer.Read{Class: layer.ReadRateLimited, ReadAt: now}, authoritative, unavailable},
		"an open circuit is UNAVAILABLE":                                   {layer.Read{Class: layer.ReadCircuitOpen, ReadAt: now}, authoritative, unavailable},
		"a malformed response is UNKNOWN (parse error recorded elsewhere)": {layer.Read{Class: layer.ReadMalformed, ReadAt: now}, authoritative, unknown},
		"an answer that cannot be determined is UNKNOWN":                   {layer.Read{Class: layer.ReadOK, Determinate: false, ReadAt: now}, authoritative, unknown},
		"a fact outside the requester's scope is UNAUTHORIZED":             {layer.Read{Class: layer.ReadOutOfScope, ReadAt: now}, authoritative, unauthorized},
		"a corroborating-only source never confers KNOWN (D1)":             {ok(now), layer.Source{Authority: layer.AuthCorroborating, SourceLimit: fiveMinutes}, unknown},
		"a customer's own claim is never KNOWN (D2)":                       {ok(now), layer.Source{Authority: layer.AuthNone, SourceLimit: fiveMinutes}, unknown},
		"a read from the future is UNKNOWN (D3)":                           {ok(now.Add(time.Second)), authoritative, unknown},
		"a zero source limit is UNKNOWN":                                   {ok(now), layer.Source{Authority: layer.AuthAuthoritative}, unknown},
		"a negative source limit is UNKNOWN":                               {ok(now), layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: -time.Second}, unknown},
		"a negative profile limit is UNKNOWN":                              {ok(now), layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: fiveMinutes, ProfileLimit: -time.Second}, unknown},
		"a read class nobody handles is UNKNOWN":                           {layer.Read{Class: "SOMETHING_NEW", ReadAt: now}, authoritative, unknown},
		"an empty read is UNKNOWN":                                         {layer.Read{}, authoritative, unknown},
	} {
		t.Run(name, func(t *testing.T) {
			if got := layer.AssignStatus(tc.read, tc.src, now); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}

// INV-19, EC-S11, EC-S19: a record that is not there is UNAVAILABLE or UNKNOWN, never KNOWN, for every kind of source, unless the
// descriptor declares AUTHORITATIVE_NEGATIVE and the read is a not-found with the terminal flag.
func TestAbsenceIsNeverANegativeFact(t *testing.T) {
	for _, auth := range []layer.Authority{layer.AuthAuthoritative, layer.AuthCorroborating, layer.AuthNone, "ANYTHING"} {
		for _, terminal := range []bool{false, true} {
			for _, readAt := range []time.Time{now, now.Add(-time.Hour), {}} {
				s := layer.Source{Authority: auth, SourceLimit: fiveMinutes}
				got := layer.AssignStatus(layer.Read{Class: layer.ReadNotFound, Terminal: terminal, Determinate: true, ReadAt: readAt}, s, now)
				if got != unavailable && got != unknown {
					t.Fatalf("authority %s, terminal %v: a missing record is %s", auth, terminal, got)
				}
			}
		}
	}
}

// P1 (Codex): evidence-item.v1 requires a missing record to be UNAVAILABLE or UNKNOWN, and evidence-source.v1 has no field with which
// a source could declare that absence is meaningful. So even a terminal not-found from an authoritative source that "declares"
// AUTHORITATIVE_NEGATIVE is never KNOWN here. (PERD §21.1 allows the exception [REC]; it needs a contract change first, AGENT-15.)
func TestNoDeclarationMakesAbsenceKnown(t *testing.T) {
	for range []struct{}{{}} {
		for _, terminal := range []bool{false, true} {
			for _, at := range []time.Time{now, now.Add(-time.Hour)} {
				s := layer.Source{Authority: layer.AuthAuthoritative, SourceLimit: fiveMinutes}
				got := layer.AssignStatus(layer.Read{Class: layer.ReadNotFound, Terminal: terminal, Determinate: true, ReadAt: at}, s, now)
				if got != unavailable && got != unknown {
					t.Fatalf("terminal %v, read at %s: a missing record is %s", terminal, at, got)
				}
			}
		}
	}
}

var classes = []layer.ReadClass{layer.ReadOK, layer.ReadTimeout, layer.ReadServerError, layer.ReadRateLimited, layer.ReadCircuitOpen,
	layer.ReadNotFound, layer.ReadMalformed, layer.ReadOutOfScope, "", "SOMETHING_NEW"}

var eight = map[contracts.EnumsV1FactStatus]bool{}

func init() {
	for _, s := range []contracts.EnumsV1FactStatus{known, contracts.EnumsV1FactStatusINFERRED, contracts.EnumsV1FactStatusAMBIGUOUS, unavailable, stale,
		contracts.EnumsV1FactStatusCONTRADICTORY, unknown, unauthorized} {
		eight[s] = true
	}
}

func generated(rng *rand.Rand) (layer.Read, layer.Source, time.Time) {
	span := func() time.Duration { return time.Duration(rng.Int63n(int64(2*time.Hour))) - time.Hour }
	at := now
	if rng.Intn(10) > 0 {
		at = now.Add(span())
	} else {
		at = time.Time{}
	}
	auths := []layer.Authority{layer.AuthAuthoritative, layer.AuthCorroborating, layer.AuthNone, "", "ANYTHING"}
	r := layer.Read{Class: classes[rng.Intn(len(classes))], Terminal: rng.Intn(2) == 0, Determinate: rng.Intn(2) == 0, ReadAt: at}
	s := layer.Source{Authority: auths[rng.Intn(len(auths))], SourceLimit: span(), ProfileLimit: span()}
	return r, s, now.Add(span())
}

// REQ-CTX-002: total over generated input. It never panics, always returns one of the eight values, never returns a status that needs
// more than one item, and is deterministic. A seeded generator (so a failure replays) and Go's own fuzzer.
func TestTotalDeterministicAndNeverMultiItemOverGeneratedInput(t *testing.T) {
	rng := rand.New(rand.NewSource(20261104)) // the seed is part of the test
	for i := 0; i < 50000; i++ {
		r, s, at := generated(rng)
		got := layer.AssignStatus(r, s, at)
		if !eight[got] {
			t.Fatalf("%+v %+v: %q is not a FactStatus", r, s, got)
		}
		if got == contracts.EnumsV1FactStatusINFERRED || got == contracts.EnumsV1FactStatusAMBIGUOUS || got == contracts.EnumsV1FactStatusCONTRADICTORY {
			t.Fatalf("%+v %+v: a single read cannot be %s", r, s, got)
		}
		if again := layer.AssignStatus(r, s, at); again != got {
			t.Fatalf("not deterministic: %s then %s", got, again)
		}
		if r.Class == layer.ReadNotFound && got != unavailable && got != unknown {
			t.Fatalf("%+v %+v: absence became KNOWN", r, s)
		}
		if got == known && (s.Authority != layer.AuthAuthoritative || s.SourceLimit <= 0 || s.ProfileLimit < 0 || r.ReadAt.After(at)) {
			t.Fatalf("%+v %+v: KNOWN without authority, a usable limit or a past read", r, s)
		}
	}
}

func FuzzAssignStatus(f *testing.F) {
	f.Add("OK", true, true, int64(0), int64(300), int64(0), "AUTHORITATIVE")
	f.Add("NOT_FOUND", true, true, int64(-5), int64(300), int64(60), "AUTHORITATIVE")
	f.Add("", false, false, int64(1<<62), int64(-1), int64(-1), "")
	f.Fuzz(func(t *testing.T, class string, terminal, determinate bool, ageSecs, srcSecs, profSecs int64, auth string) {
		clamp := func(v int64) time.Duration {
			if v > 1<<40 {
				v = 1 << 40
			}
			if v < -(1 << 40) {
				v = -(1 << 40)
			}
			return time.Duration(v) * time.Second
		}
		r := layer.Read{Class: layer.ReadClass(class), Terminal: terminal, Determinate: determinate, ReadAt: now.Add(-clamp(ageSecs))}
		s := layer.Source{Authority: layer.Authority(auth), SourceLimit: clamp(srcSecs), ProfileLimit: clamp(profSecs)}
		if got := layer.AssignStatus(r, s, now); !eight[got] {
			t.Fatalf("%q is not a FactStatus", got)
		}
	})
}
