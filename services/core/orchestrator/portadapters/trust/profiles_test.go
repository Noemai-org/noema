package trustport_test

// TRU-07, the production `SourceProfiles` of the Evidence adapter (T9): the evidence rules of the trust profile that governs an intent node, read from the pack's data.
// Proves REQ-TRU-001, REQ-CTX-003, REQ-NFR-002; INV-14, INV-16, INV-23; AGENT-16. No test needs a database. Every helper starts with `spr` so that the other adapters' tests cannot
// collide with it. All data is SYNTHETIC (AGENT-25); the one real-pack test reads packs/fintech_v1.json and compares with an independent reader of the same file.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Noemai-org/noema/services/core/internal/contracts"
	trustport "github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trust"
	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	sprTenant = "ten_aa"
	sprNode   = "syn.node.one"
	sprNode2  = "syn.node.two"
	sprBare   = "syn.node.unprofiled"
	sprProf   = "syn.trust.one"
	sprProf2  = "syn.trust.two"
	sprSrcA   = "syn.src.a"
	sprSrcB   = "syn.src.b"
)

func sprCtx(t testing.TB) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	return c
}

func sprStr(s string) *string { return &s }

func sprNodeOf(id string, profile *string) contracts.IntentNodeV1 {
	return contracts.IntentNodeV1{NodeId: contracts.CommonV1PackId(id), TrustProfile: profile}
}

func sprProfile(id string, maxAge int64, required ...string) contracts.TrustProfileV1 {
	ids := make([]contracts.CommonV1PackId, len(required))
	for i, r := range required {
		ids[i] = contracts.CommonV1PackId(r)
	}
	return contracts.TrustProfileV1{ProfileId: contracts.CommonV1PackId(id), EvidenceRequired: ids, MaxEvidenceAgeSeconds: maxAge}
}

func sprBuild(t testing.TB) trustport.PackProfiles {
	t.Helper()
	p, err := trustport.NewPackProfiles(
		[]contracts.IntentNodeV1{sprNodeOf(sprNode, sprStr(sprProf)), sprNodeOf(sprNode2, sprStr(sprProf2)), sprNodeOf(sprBare, nil)},
		[]contracts.TrustProfileV1{sprProfile(sprProf, 300, sprSrcA, sprSrcB), sprProfile(sprProf2, 45, sprSrcB)})
	if err != nil {
		t.Fatalf("NewPackProfiles: %v", err)
	}
	return p
}

// REQ-TRU-001, INV-23: the rules are the profile's, per node, as data.
func TestSprRulesAreTheProfilesOfTheNode(t *testing.T) {
	p := sprBuild(t)
	cases := map[string]trustport.SourceRules{
		sprNode:  {RequiredSources: []string{sprSrcA, sprSrcB}, MaxAgeSeconds: 300},
		sprNode2: {RequiredSources: []string{sprSrcB}, MaxAgeSeconds: 45},
	}
	for node, want := range cases {
		got, err := p.SourceRules(sprCtx(t), sprTenant, node)
		if err != nil {
			t.Fatalf("%s: %v", node, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s: got %+v, want %+v", node, got, want)
		}
	}
}

// INV-16: an unknown node, a node with no trust profile and an empty node are errors, never an empty set of rules.
func TestSprUnknownOrUnprofiledNodeIsAnError(t *testing.T) {
	p := sprBuild(t)
	for name, node := range map[string]string{"unknown": "syn.node.nobody", "no profile": sprBare, "empty": "", "near miss": sprNode + " "} {
		got, err := p.SourceRules(sprCtx(t), sprTenant, node)
		if !errors.Is(err, trustport.ErrProfileNotFound) {
			t.Errorf("%s: err = %v, want ErrProfileNotFound", name, err)
		}
		if !reflect.DeepEqual(got, trustport.SourceRules{}) {
			t.Errorf("%s: an error came with rules %+v", name, got)
		}
	}
}

// INV-16: the zero value, a missing tenant and a dead context answer nothing.
func TestSprRefusesWhatItCannotAnswer(t *testing.T) {
	var zero trustport.PackProfiles
	if _, err := zero.SourceRules(sprCtx(t), sprTenant, sprNode); !errors.Is(err, trustport.ErrConfig) {
		t.Errorf("zero value: err = %v, want ErrConfig", err)
	}
	p := sprBuild(t)
	for _, tenant := range []string{"", "tenant", "ten_", "TEN_AA"} {
		if _, err := p.SourceRules(sprCtx(t), tenant, sprNode); !errors.Is(err, trustport.ErrSession) {
			t.Errorf("tenant %q: err = %v, want ErrSession", tenant, err)
		}
	}
	dead, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := p.SourceRules(dead, sprTenant, sprNode); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled context: err = %v, want context.Canceled", err)
	}
}

// INV-14: the pack is data and the same for every tenant; the tenant ID only has to be a tenant.
func TestSprTenantDoesNotChangeTheRules(t *testing.T) {
	p := sprBuild(t)
	a, errA := p.SourceRules(sprCtx(t), "ten_aa", sprNode)
	b, errB := p.SourceRules(sprCtx(t), "ten_bb", sprNode)
	if errA != nil || errB != nil || !reflect.DeepEqual(a, b) {
		t.Errorf("rules differ by tenant: %+v %v / %+v %v", a, errA, b, errB)
	}
}

// AGENT-16, INV-16: what a caller does to an answer never changes the next one.
func TestSprAnswersAreCopies(t *testing.T) {
	p := sprBuild(t)
	first, err := p.SourceRules(sprCtx(t), sprTenant, sprNode)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.RequiredSources) == 0 {
		t.Fatal("the first answer has no required source")
	}
	first.RequiredSources[0] = "syn.src.tampered"
	first.MaxAgeSeconds = 9999
	second, err := p.SourceRules(sprCtx(t), sprTenant, sprNode)
	if err != nil {
		t.Fatal(err)
	}
	want := trustport.SourceRules{RequiredSources: []string{sprSrcA, sprSrcB}, MaxAgeSeconds: 300}
	if !reflect.DeepEqual(second, want) {
		t.Errorf("second answer %+v, want %+v", second, want)
	}
}

// AGENT-16: slices the pack was built from belong to the adapter after the build.
func TestSprBuildCopiesItsInput(t *testing.T) {
	prof := []contracts.TrustProfileV1{sprProfile(sprProf, 300, sprSrcA, sprSrcB)}
	p, err := trustport.NewPackProfiles([]contracts.IntentNodeV1{sprNodeOf(sprNode, sprStr(sprProf))}, prof)
	if err != nil {
		t.Fatal(err)
	}
	prof[0].EvidenceRequired[0] = "syn.src.tampered"
	prof[0].MaxEvidenceAgeSeconds = 1
	got, err := p.SourceRules(sprCtx(t), sprTenant, sprNode)
	if err != nil || !reflect.DeepEqual(got, trustport.SourceRules{RequiredSources: []string{sprSrcA, sprSrcB}, MaxAgeSeconds: 300}) {
		t.Errorf("after the input changed: %+v, %v", got, err)
	}
}

// INV-16: pack data that cannot give a sound answer is refused whole, at the build, with ErrProfilesData.
func TestSprBuildRefusesUnsoundData(t *testing.T) {
	node := sprNodeOf(sprNode, sprStr(sprProf))
	ok := sprProfile(sprProf, 300, sprSrcA)
	cases := map[string]struct {
		nodes    []contracts.IntentNodeV1
		profiles []contracts.TrustProfileV1
	}{
		"repeated node":                {[]contracts.IntentNodeV1{node, node}, []contracts.TrustProfileV1{ok}},
		"node with no ID":              {[]contracts.IntentNodeV1{sprNodeOf("", sprStr(sprProf))}, []contracts.TrustProfileV1{ok}},
		"repeated profile":             {[]contracts.IntentNodeV1{node}, []contracts.TrustProfileV1{ok, ok}},
		"profile with no ID":           {[]contracts.IntentNodeV1{node}, []contracts.TrustProfileV1{sprProfile("", 300, sprSrcA)}},
		"node names a missing profile": {[]contracts.IntentNodeV1{sprNodeOf(sprNode, sprStr("syn.trust.missing"))}, []contracts.TrustProfileV1{ok}},
		"node names an empty profile":  {[]contracts.IntentNodeV1{sprNodeOf(sprNode, sprStr(""))}, []contracts.TrustProfileV1{ok}},
		"max age zero":                 {[]contracts.IntentNodeV1{node}, []contracts.TrustProfileV1{sprProfile(sprProf, 0, sprSrcA)}},
		"max age negative":             {[]contracts.IntentNodeV1{node}, []contracts.TrustProfileV1{sprProfile(sprProf, -5, sprSrcA)}},
		"no required source":           {[]contracts.IntentNodeV1{node}, []contracts.TrustProfileV1{sprProfile(sprProf, 300)}},
		"empty required source":        {[]contracts.IntentNodeV1{node}, []contracts.TrustProfileV1{sprProfile(sprProf, 300, sprSrcA, "")}},
		"repeated required source":     {[]contracts.IntentNodeV1{node}, []contracts.TrustProfileV1{sprProfile(sprProf, 300, sprSrcA, sprSrcA)}},
		"no nodes":                     {nil, []contracts.TrustProfileV1{ok}},
	}
	for name, c := range cases {
		p, err := trustport.NewPackProfiles(c.nodes, c.profiles)
		if !errors.Is(err, trustport.ErrProfilesData) {
			t.Errorf("%s: err = %v, want ErrProfilesData", name, err)
		}
		if _, err := p.SourceRules(sprCtx(t), sprTenant, sprNode); err == nil {
			t.Errorf("%s: a refused build still answered", name)
		}
	}
}

// AGENT-13: an error names the rule, never a node, a profile or a source of the pack.
func TestSprErrorsNameNoPackContent(t *testing.T) {
	secret := "syn.secret.marker"
	_, err := trustport.NewPackProfiles([]contracts.IntentNodeV1{sprNodeOf(secret, sprStr("syn.trust.absent."+secret))}, nil)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("build error %v names the pack's content", err)
	}
	p := sprBuild(t)
	if _, err := p.SourceRules(sprCtx(t), sprTenant, secret); err == nil || strings.Contains(err.Error(), secret) {
		t.Errorf("lookup error %v names the node asked", err)
	}
}

// INV-23, REQ-TRU-001: composed with the Evidence adapter, the profile's cap tightens a source's limit and a required source with no item is reported unavailable.
func TestSprComposesWithTheEvidenceAdapter(t *testing.T) {
	p, err := trustport.NewPackProfiles(
		[]contracts.IntentNodeV1{sprNodeOf(evdNode, sprStr(sprProf))},
		[]contracts.TrustProfileV1{sprProfile(sprProf, 30, evdLedger, evdSwitch)})
	if err != nil {
		t.Fatal(err)
	}
	ev, err := trustport.NewEvidence(evdStoreOf(evdWorld(t)), p, clock.NewManual(evdNow))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ev.SnapshotFreshness(evdCtx(t), evdTenant, evdSnap)
	if err != nil {
		t.Fatalf("SnapshotFreshness: %v", err)
	}
	for _, it := range got.Items {
		if it.Freshness.EffectiveLimitSeconds > 30 {
			t.Errorf("item %s: limit %d exceeds the profile's cap of 30", it.Freshness.EvidenceID, it.Freshness.EffectiveLimitSeconds)
		}
	}
}

// INV-14, INV-16: the real pack. The expected values come from an independent reader of the file, not from the adapter's types.
func TestSprRealPackProfilesAgreeWithTheFile(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "..", "packs", "fintech_v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Nodes []struct {
			ID      string  `json:"node_id"`
			Profile *string `json:"trust_profile"`
		} `json:"intent_nodes"`
		Profiles []struct {
			ID       string   `json:"profile_id"`
			Required []string `json:"evidence_required"`
			MaxAge   int64    `json:"max_evidence_age_seconds"`
		} `json:"trust_profiles"`
	}
	if err := json.Unmarshal(raw, &file); err != nil {
		t.Fatal(err)
	}
	var doc contracts.PackV1
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	p, err := trustport.NewPackProfiles(doc.IntentNodes, doc.TrustProfiles)
	if err != nil {
		t.Fatalf("the real pack is refused: %v", err)
	}
	checked := 0
	for _, n := range file.Nodes {
		got, err := p.SourceRules(sprCtx(t), sprTenant, n.ID)
		if n.Profile == nil {
			if !errors.Is(err, trustport.ErrProfileNotFound) {
				t.Errorf("node %s has no profile: err = %v, want ErrProfileNotFound", n.ID, err)
			}
			continue
		}
		for _, pr := range file.Profiles {
			if pr.ID != *n.Profile {
				continue
			}
			checked++
			if err != nil || got.MaxAgeSeconds != pr.MaxAge || !reflect.DeepEqual(got.RequiredSources, pr.Required) {
				t.Errorf("node %s: got %+v, %v; the file says %v and %d", n.ID, got, err, pr.Required, pr.MaxAge)
			}
		}
	}
	if checked == 0 {
		t.Fatal("no profiled node was compared: the test proves nothing")
	}
}
