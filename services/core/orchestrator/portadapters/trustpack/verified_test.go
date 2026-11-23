package trustpack_test

// STM-01 compose, lane 1 (A333, A377 items 6 and 7, A510; interim, relayed, not yet committed): the pack the Trust adapters read is built from the BYTES the manifest verified, never
// from a second read of the directory, and it says which pack it is (`<id>@<version>`, the value of TrustAdapters.PackVersion). REQ-STM-001, REQ-TRU-004, REQ-TRU-005; INV-14,
// INV-16, INV-21, AGENT-16. The fintech pack under /packs is SYNTHETIC; no id of it is written in the code under test.

import (
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

// verifiedParts splits the pack's files the way packload does: the pack file's bytes, and each action graph by the path the pack names.
func verifiedParts(t testing.TB) ([]byte, map[string][]byte) {
	t.Helper()
	m := files(t)
	pack := m[packFileName]
	delete(m, packFileName)
	return pack, m
}

// ownRef reads the id and the version from the pack file itself, not through the package under test.
func ownRef(t testing.TB, pack []byte) string {
	t.Helper()
	var f struct {
		ID      string `json:"pack_id"`
		Version string `json:"pack_version"`
	}
	if err := json.Unmarshal(pack, &f); err != nil || f.ID == "" || f.Version == "" {
		t.Fatalf("the pack file has no id or version: %v", err)
	}
	return f.ID + "@" + f.Version
}

// The verified bytes give the same Pack as the directory does: the escalation set (the writes of the graph the setting names) and the capability registry answer alike. INV-21.
func TestLoadVerifiedAnswersLikeLoadPackOverTheSameBytes(t *testing.T) {
	pack, graphs := verifiedParts(t)
	got, err := trustpack.LoadVerified(pack, graphs)
	if err != nil || got == nil {
		t.Fatalf("LoadVerified = %v, %v", got, err)
	}
	want := mustLoadFiles(t, files(t))
	id := escalateGraph(t).ID
	gs, gerr := trustpack.EscalationSetFromPack(got, id)
	ws, werr := trustpack.EscalationSetFromPack(want, id)
	if gerr != nil || werr != nil || !reflect.DeepEqual(gs, ws) {
		t.Errorf("escalation set %v (%v), want %v (%v)", gs, gerr, ws, werr)
	}
	if _, err := trustpack.NewCapabilities(got, id); err != nil {
		t.Errorf("the capabilities adapter is not built over the verified pack: %v", err)
	}
	if _, err := trustpack.NewScope(got); err != nil {
		t.Errorf("the scope adapter is not built over the verified pack: %v", err)
	}
	if _, err := trustpack.NewAuthority(got); err != nil {
		t.Errorf("the authority adapter is not built over the verified pack: %v", err)
	}
}

// The graphs come from the verified bytes only: the loader is given no directory, so a graph the pack names and the bytes lack is refused whole, and a graph the pack does not name is
// refused too (A510: nothing is served that the manifest did not pin for this pack). INV-16.
func TestLoadVerifiedServesOnlyTheGraphsThePackNamesAndRefusesTheRest(t *testing.T) {
	pack, graphs := verifiedParts(t)
	cases := map[string]func(map[string][]byte){
		"a named graph is missing": func(m map[string][]byte) {
			for k := range m {
				delete(m, k)
				return
			}
		},
		"a graph the pack does not name": func(m map[string][]byte) { m["an_unnamed_graph.json"] = []byte(`{}`) },
		"a graph that does not decode": func(m map[string][]byte) {
			for k := range m {
				m[k] = []byte("not json")
				return
			}
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			m := map[string][]byte{}
			for k, v := range graphs {
				m[k] = v
			}
			mutate(m)
			p, err := trustpack.LoadVerified(pack, m)
			if !errors.Is(err, trustpack.ErrPack) || p != nil {
				t.Fatalf("LoadVerified = %v, %v; want a nil Pack and ErrPack", p, err)
			}
		})
	}
}

// No bytes, no pack: nothing is answered from nothing. INV-16.
func TestLoadVerifiedWithNoPackBytesIsErrConfig(t *testing.T) {
	_, graphs := verifiedParts(t)
	for name, b := range map[string][]byte{"nil": nil, "empty": {}} {
		if p, err := trustpack.LoadVerified(b, graphs); !errors.Is(err, trustpack.ErrConfig) || p != nil {
			t.Errorf("%s: LoadVerified = %v, %v; want ErrConfig", name, p, err)
		}
	}
}

// The pack says which pack it is: `<id>@<version>` exactly as the pack file carries them (A377 item 6). Both loaders give it; a nil pack and a zero Pack give none.
func TestThePackSaysWhichPackItIs(t *testing.T) {
	pack, graphs := verifiedParts(t)
	want := ownRef(t, pack)
	v, err := trustpack.LoadVerified(pack, graphs)
	if err != nil {
		t.Fatal(err)
	}
	if got := v.Ref(); got != want {
		t.Errorf("verified Ref() = %q, want %q", got, want)
	}
	if got := mustLoadFiles(t, files(t)).Ref(); got != want {
		t.Errorf("LoadPack Ref() = %q, want %q", got, want)
	}
	var nilPack *trustpack.Pack
	if got := nilPack.Ref(); got != "" {
		t.Errorf("nil pack Ref() = %q, want none", got)
	}
	if got := (&trustpack.Pack{}).Ref(); got != "" {
		t.Errorf("zero pack Ref() = %q, want none", got)
	}
}
