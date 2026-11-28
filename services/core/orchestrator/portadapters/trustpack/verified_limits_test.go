package trustpack_test

// The pack the Trust adapters read keeps the verified limits files the manifest pinned (A544, A561), so the composition reads the confirmation TTL ceiling from the one verified pack.
// REQ-STM-001, REQ-TRU-007; INV-16, INV-21, AGENT-16. Every byte here is SYNTHETIC.

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/Noemai-org/noema/services/core/orchestrator/portadapters/trustpack"
)

func TestLoadVerifiedWithLimitsKeepsTheVerifiedLimitsFilesAsACopy(t *testing.T) {
	pack, graphs := verifiedParts(t)
	limits := map[string][]byte{"limits/a.json": []byte(`{"synthetic":1}`)}
	p, err := trustpack.LoadVerifiedWithLimits(pack, graphs, limits)
	if err != nil || p == nil {
		t.Fatalf("LoadVerifiedWithLimits = %v, %v", p, err)
	}
	got := p.LimitsFiles()
	if !reflect.DeepEqual(got, limits) {
		t.Fatalf("limits files %v; want the ones given %v", got, limits)
	}
	got["limits/a.json"][0] = 'X'
	delete(got, "limits/a.json")
	if again := p.LimitsFiles(); !bytes.Equal(again["limits/a.json"], []byte(`{"synthetic":1}`)) {
		t.Errorf("a caller changed the pack's limits through the returned map: %q", again["limits/a.json"])
	}
	limits["limits/a.json"][0] = 'Y'
	if again := p.LimitsFiles(); !bytes.Equal(again["limits/a.json"], []byte(`{"synthetic":1}`)) {
		t.Errorf("the pack shares the bytes the caller gave it: %q", again["limits/a.json"])
	}
}

func TestLoadVerifiedKeepsNoLimitsFilesAndTheWithLimitsFormRefusesWhatLoadVerifiedRefuses(t *testing.T) {
	pack, graphs := verifiedParts(t)
	p, err := trustpack.LoadVerified(pack, graphs)
	if err != nil || len(p.LimitsFiles()) != 0 {
		t.Errorf("LoadVerified: %v, limits %v; want none", err, p.LimitsFiles())
	}
	if q, err := trustpack.LoadVerifiedWithLimits(nil, graphs, nil); !errors.Is(err, trustpack.ErrConfig) || q != nil {
		t.Errorf("no pack bytes: %v, %v; want ErrConfig and no pack", q, err)
	}
	var zero *trustpack.Pack
	if len(zero.LimitsFiles()) != 0 {
		t.Errorf("a nil pack has limits files")
	}
}
