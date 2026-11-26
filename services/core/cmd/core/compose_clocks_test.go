package main

// STM-01 compose, Clocks (A612, A684 step 1, A544, A447, A345 S3; interim, relayed, not yet committed). REQ-STM-001, REQ-TRU-006, REQ-OPS-003; INV-14, INV-16, INV-19, INV-21; AGENT-13,
// AGENT-16. The core loads the pack's trust limits (trust-limits.v2) from the bytes the manifest pinned and REFUSES TO START when they are not exactly the contract: it never falls back to
// a default. The Clocks adapter (T12) is built from the verified pack's clocks, the at-risk fraction of the limits, the anchor mapping of the limits over the sealed snapshot's lists, and
// the phase routes of the limits (none given: nothing routed). A part that is absent leaves Clocks a named gap. Every value is SYNTHETIC.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/evidence/payloadread"
	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust/intakesources"
	"github.com/Noemai-org/noema/services/core/platform/crypto"
	"github.com/Noemai-org/noema/services/core/platform/packload"
	"github.com/Noemai-org/noema/services/core/trust/clocks"
	"github.com/Noemai-org/noema/services/core/trust/intake"
	"github.com/Noemai-org/noema/services/core/trust/intakeadapters"
)

const (
	ckNodeWithClock = "fin.payments.exception.investigate"
	ckNodeNoClock   = "fin.meta.out_of_scope"
)

// ckPath is the pinned path of the trust-limits file in a verified pack (the test knows the pack; the core does not).
func ckPath(t *testing.T, v packload.Pack) string {
	t.Helper()
	for p := range v.Limits {
		if strings.HasSuffix(p, "trust_limits.json") {
			return p
		}
	}
	t.Fatal("the real pack pins no trust limits file")
	return ""
}

// ckWith returns the real verified pack with its limits map changed by mut (the map is copied first).
func ckWith(t *testing.T, mut func(m map[string][]byte, path string)) packload.Pack {
	t.Helper()
	v := tiVerified(t)
	path := ckPath(t, v)
	m := map[string][]byte{}
	for k, b := range v.Limits {
		m[k] = append([]byte(nil), b...)
	}
	mut(m, path)
	v.Limits = m
	return v
}

// ckEdit decodes the trust limits document, applies f and encodes it back.
func ckEdit(t *testing.T, b []byte, f func(d map[string]any)) []byte {
	t.Helper()
	var d map[string]any
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	f(d)
	out, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestTheLimitsOfTheRealPackLoadWithTheirAnchorMapping(t *testing.T) {
	l, doc, err := trustLimitsOf(tiVerified(t))
	if err != nil {
		t.Fatal(err)
	}
	if bp, err := l.ClockAtRiskRemainingBP(); err != nil || bp != 2500 {
		t.Errorf("at-risk %d, %v; want the pinned file's 2500", bp, err)
	}
	if doc.AnchorEntities == nil || len(*doc.AnchorEntities) != 1 || (*doc.AnchorEntities)[0].Event != "complaint_logged" || (*doc.AnchorEntities)[0].Entity != "complaint_ref" || (*doc.AnchorEntities)[0].Field != "effective_at" {
		t.Errorf("anchor mapping %+v; want complaint_logged to complaint_ref.effective_at", doc.AnchorEntities)
	}
}

// A612: a limits file that is not exactly trust-limits.v2, or not pinned, or ambiguous, is an error and no Limits; there is no default.
func TestALimitsFileThatIsNotExactlyV2IsRefused(t *testing.T) {
	cases := map[string]func(m map[string][]byte, path string){
		"no limits file is pinned": func(m map[string][]byte, path string) {
			for k := range m {
				delete(m, k)
			}
		},
		"the trust file is not pinned": func(m map[string][]byte, path string) { delete(m, path) },
		"a v1 document": func(m map[string][]byte, path string) {
			m[path] = []byte(`{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC"}`)
		},
		"malformed": func(m map[string][]byte, path string) { m[path] = []byte(`{`) },
		"no ceiling": func(m map[string][]byte, path string) {
			m[path] = ckEdit(t, m[path], func(d map[string]any) { delete(d, "confirmation_ttl_ceiling_ms") })
		},
		"an unknown member": func(m map[string][]byte, path string) {
			m[path] = ckEdit(t, m[path], func(d map[string]any) { d["extra"] = 1 })
		},
		"another pack's": func(m map[string][]byte, path string) {
			m[path] = ckEdit(t, m[path], func(d map[string]any) { d["pack_id"] = "other" })
		},
		"two files are exactly v2": func(m map[string][]byte, path string) { m["fintech/limits/second_trust.json"] = m[path] },
	}
	for name, mut := range cases {
		t.Run(name, func(t *testing.T) {
			v := ckWith(t, mut)
			l, _, err := trustLimitsOf(v)
			var ie *inputsError
			if !errors.As(err, &ie) || l.Loaded() {
				t.Fatalf("got %v, loaded=%v; want an *inputsError and no Limits", err, l.Loaded())
			}
			for p := range v.Limits {
				if strings.Contains(err.Error(), p) {
					t.Errorf("the error carries a path: %q", err)
				}
			}
		})
	}
}

// A612: the process does not start on a refusal. The pack is copied, its limits file replaced and the manifest resealed, so that the manifest accepts it and only the limits check can refuse.
func TestTheProcessDoesNotStartOnABadLimitsFile(t *testing.T) {
	good := ckEnv(t, nil)
	if _, _, closeAll, err := ckCompose(t, good); err != nil {
		t.Fatalf("a good pack must start: %v", err)
	} else {
		closeAll()
	}
	bad := map[string][]byte{
		"a v1 document":  []byte(`{"schema_version":"1.0","pack_id":"fin","label":"SYNTHETIC"}`),
		"not JSON":       []byte(`{`),
		"no ceiling":     nil,
		"an unknown key": nil,
	}
	for name, b := range bad {
		t.Run(name, func(t *testing.T) {
			e := ckEnv(t, func(dir string) {
				p := filepath.Join(dir, "fintech", "limits", "trust_limits.json")
				raw := b
				if raw == nil {
					orig, err := os.ReadFile(p)
					if err != nil {
						t.Fatal(err)
					}
					raw = ckEdit(t, orig, func(d map[string]any) {
						if name == "no ceiling" {
							delete(d, "confirmation_ttl_ceiling_ms")
						} else {
							d["extra"] = 1
						}
					})
				}
				if err := os.WriteFile(p, raw, 0o644); err != nil {
					t.Fatal(err)
				}
				psrcReseal(t, dir)
			})
			c, parts, closeAll, err := ckCompose(t, e)
			closeAll()
			var ie *inputsError
			if !errors.As(err, &ie) {
				t.Fatalf("error %v; want the limits refusal, an *inputsError", err)
			}
			if strings.Contains(err.Error(), e[EnvPackDir]) || strings.Contains(err.Error(), "trust_limits") {
				t.Errorf("the error carries a path: %q", err)
			}
			if c.Ports.Interpreter != nil || parts.Trust != nil {
				t.Errorf("components were returned with an error")
			}
		})
	}
}

// With no pack setting, or only some of them, there is no pack to check: the process starts and the named gaps say so (A469, A486).
func TestWithoutACompletePackSettingNothingIsCheckedHere(t *testing.T) {
	for name, e := range map[string]envMap{
		"none":     {},
		"partial":  {EnvPackDir: "x"},
		"no bound": {EnvPackDir: psrcPacks(), EnvPackManifest: psrcManifest},
	} {
		if err := checkPackLimits(Env{Getenv: e.get}); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

// The builder: a complete input builds Clocks from the pack's clocks and the at-risk fraction of the pinned limits; it is no gap and fill hands it to the composition.
func TestClocksIsBuiltFromThePackAndItsLimits(t *testing.T) {
	got, err := buildTrustInputs(tiInWithLists(t))
	if err != nil {
		t.Fatal(err)
	}
	if got.Clocks == nil || slices.Contains(got.Gaps, "Clocks") {
		t.Fatalf("Clocks is not built (gaps %v)", got.Gaps)
	}
	if a := got.fill(TrustAdapters{}); a.Clocks == nil {
		t.Fatal("fill did not give the composition the Clocks adapter")
	}
	ctx := tiDeadline(t)
	set, err := got.Clocks.Applicable(ctx, intake.ClockQuery{TenantID: "ten_aa", IntentNode: ckNodeNoClock})
	if err != nil || len(set.Clocks) != 0 || set.AtRisk != (clocks.Ratio{Num: 2500, Den: 10000}) {
		t.Errorf("a node with no clock: %+v, %v; want none and the limits' 2500 over 10000", set, err)
	}
	if _, err := got.Clocks.Applicable(ctx, intake.ClockQuery{TenantID: "ten_aa", IntentNode: ckNodeWithClock}); !errors.Is(err, intakeadapters.ErrQuery) {
		t.Errorf("a node with a clock and no snapshot: %v; want ErrQuery (the book is wired, nothing is assumed started)", err)
	}
	if _, err := got.Clocks.Applicable(ctx, intake.ClockQuery{TenantID: "ten_aa", IntentNode: "fin.nope"}); !errors.Is(err, intakeadapters.ErrQuery) {
		t.Errorf("an unknown node: %v; want ErrQuery", err)
	}
	r, err := got.Clocks.Route(ctx, intake.ClockQuery{TenantID: "ten_aa", IntentNode: ckNodeWithClock}, []clocks.Status{{Phase: clocks.Breached}})
	if err != nil || r.Escalate {
		t.Errorf("route %+v, %v; the pinned limits give no phase route, so nothing is routed here", r, err)
	}
}

func TestClocksWithAPartMissingIsANamedGap(t *testing.T) {
	cases := map[string]func(*trustInputsIn){
		"no list entity":      func(in *trustInputsIn) { in.ListEntities = nil },
		"no mapping":          func(in *trustInputsIn) { in.Mapping = nil },
		"no payload source":   func(in *trustInputsIn) { in.Payloads = nil },
		"no injection screen": func(in *trustInputsIn) { in.Screen.Alert = nil },
		"no verified pack":    func(in *trustInputsIn) { in.Verified.Pack = nil },
		"no anchor mapping": func(in *trustInputsIn) {
			p := ckPath(t, in.Verified)
			in.Verified = ckWith(t, func(m map[string][]byte, path string) {
				m[p] = ckEdit(t, m[p], func(d map[string]any) { delete(d, "anchor_entities") })
			})
		},
	}
	for name, mut := range cases {
		in := tiInWithLists(t)
		mut(&in)
		got, err := buildTrustInputs(in)
		if err != nil {
			t.Errorf("%s: a gap is not an error: %v", name, err)
			continue
		}
		if got.Clocks != nil || !slices.Contains(got.Gaps, "Clocks") {
			t.Errorf("%s: Clocks %v, gaps %v; want a named gap and no adapter", name, got.Clocks, got.Gaps)
		}
	}
}

// A limits file that is present and invalid is an error even when other parts are missing: it is checked first (A612).
func TestBadLimitsAreAnErrorOfTheBuilderWhateverElseIsMissing(t *testing.T) {
	in := tiIn(t) // no list entities
	in.Verified = ckWith(t, func(m map[string][]byte, path string) { m[path] = []byte(`{`) })
	if _, err := buildTrustInputs(in); err == nil {
		t.Fatal("bad limits were not an error")
	}
}

// An anchor mapping that names a list entity the lists do not serve is an error (present and invalid), never a gap.
func TestAnAnchorEntityTheListsDoNotServeIsAnError(t *testing.T) {
	in := tiInWithLists(t)
	in.ListEntities = map[string]intakesources.ListSpec{"case": in.ListEntities["case"]}
	if _, err := buildTrustInputs(in); err == nil {
		t.Fatal("an anchor entity that is not served was not an error")
	}
}

// ckItems is a snapshot with one complaint list item; ckPayloads serves its payload.
type ckItems struct{ hash string }

func (i ckItems) Items(context.Context, string, string) ([]intakesources.SnapshotItem, error) {
	return []intakesources.SnapshotItem{{EvidenceID: "evr_1", Source: "fin.src.case_system", ResourceScope: "complaint_ref:TX1", Status: "KNOWN", PayloadRef: "pay_1", PayloadHash: i.hash}}, nil
}

type ckPayloads struct{ doc []byte }

func (p ckPayloads) Read(context.Context, string, string) ([]byte, error) { return p.doc, nil }

var _ payloadread.PayloadReader = ckPayloads{}

func ckAdapter(t *testing.T, rows []any, limitsEdit func(d map[string]any)) intake.Clocks {
	t.Helper()
	v := ckWith(t, func(m map[string][]byte, path string) {
		if limitsEdit != nil {
			m[path] = ckEdit(t, m[path], limitsEdit)
		}
	})
	l, doc, err := trustLimitsOf(v)
	if err != nil {
		t.Fatal(err)
	}
	if rows == nil {
		rows = []any{}
	}
	payload := map[string]any{"items": rows, "complete": true}
	canon, err := crypto.CanonicalJSON(payload)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(canon)
	raw, _ := json.Marshal(payload)
	_, m := tiParts(t, v)
	sc := tiScreen()
	c, err := newClocksAdapter(clocksParts{Pack: v.Pack, Limit: l, Doc: doc, Lists: intakesources.EvidenceLists{
		Mapping: m, Items: ckItems{hash: "sha256:" + hex.EncodeToString(sum[:])}, Payloads: ckPayloads{doc: raw}, Screen: sc.Screen, Correlation: sc.Correlation, Alert: sc.Alert,
		Entities: map[string]intakesources.ListSpec{"complaint_ref": {Source: "fin.src.case_system", ScopePrefix: "complaint_ref:"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// The anchor is read from the list and field the limits name; a complete empty list is NOT_STARTED; the phase routes of the limits give the escalation reason.
func TestAnchorsAndRoutesComeFromTheLimits(t *testing.T) {
	q := intake.ClockQuery{TenantID: "ten_aa", IntentNode: ckNodeWithClock, SnapshotID: "evs_s1"}
	row := map[string]any{"complaint_ref": "cmp_1", "clock_started_at": "2026-11-10T09:00:00Z"}
	c := ckAdapter(t, []any{row}, func(d map[string]any) {
		d["phase_routes"] = []any{map[string]any{"phase": "BREACHED", "reason_code": "fin.reason.sla_breached"}}
	})
	set, err := c.Applicable(tiDeadline(t), q)
	want := time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC).Unix()
	if err != nil || len(set.Clocks) != 1 || set.Clocks[0].Anchor == nil || *set.Clocks[0].Anchor != want {
		t.Fatalf("got %+v, %v; want one clock anchored at %d", set, err, want)
	}
	if r, err := c.Route(tiDeadline(t), q, []clocks.Status{{Phase: clocks.Breached}}); err != nil || !r.Escalate || r.ReasonCode != "fin.reason.sla_breached" {
		t.Errorf("breached: %+v, %v; want the limits' reason code", r, err)
	}
	if r, err := c.Route(tiDeadline(t), q, []clocks.Status{{Phase: clocks.AtRisk}}); err != nil || r.Escalate {
		t.Errorf("at risk: %+v, %v; the limits route only BREACHED", r, err)
	}
	empty := ckAdapter(t, nil, nil)
	set, err = empty.Applicable(tiDeadline(t), q)
	if err != nil || len(set.Clocks) != 1 || set.Clocks[0].Anchor != nil {
		t.Errorf("a complete empty list: %+v, %v; want NOT_STARTED (no anchor)", set, err)
	}
}

// ckEnv copies the real pack, lets edit change it, and returns pack-only settings (no Trust, no Record: nothing is dialled).
func ckEnv(t *testing.T, edit func(dir string)) envMap {
	t.Helper()
	dir := psrcCopy(t)
	if edit != nil {
		edit(dir)
	}
	return envMap{EnvPackDir: dir, EnvPackManifest: psrcManifest, EnvPackMaxBytes: "1048576"}
}

func ckCompose(t *testing.T, e envMap) (Components, TrustParts, func(), error) {
	t.Helper()
	return mcBuild(t, e, composeDeps{})
}
