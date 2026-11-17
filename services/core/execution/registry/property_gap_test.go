package registry_test

// TST-01 gap 4 (INV-05, INV-18, REQ-CAP-002, BD-08). Everything here is SYNTHETIC.
//
// TestProhibitedIDsAreNeverCallable draws its "prohibited" IDs from the pack it loads, so a pack that stops marking a capability prohibited makes that
// capability allowed and the property still passes. These tests pin the eight IDs of SoT 34.3, cross-check the trust profiles' own prohibited lists against the
// descriptors' flags, and state the loader's refusal of an unmarked monetary write as a property over every combination of flags.

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/Noemai-org/noema/services/core/execution/registry"
	"github.com/Noemai-org/noema/services/core/internal/contracts"
)

// The eight PROHIBITED capabilities of SoT 34.3, as the fintech pack names them.
var sotProhibited = []string{
	"fin.cap.transfer_funds", "fin.cap.reverse_transaction", "fin.cap.adjust_balance", "fin.cap.initiate_refund",
	"fin.cap.close_or_resolve_case", "fin.cap.modify_sla_or_regulatory_clock", "fin.cap.edit_or_delete_evidence", "fin.cap.downgrade_case_priority",
}

func TestINV05ExactlyTheEightProhibitedCapabilitiesOfSoT343AreRegisteredAndNeverCallable(t *testing.T) {
	r := loadFintech(t)
	got := r.Prohibited()
	want := append([]string(nil), sotProhibited...)
	sort.Strings(want)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("the pack prohibits %v, SoT 34.3 prohibits %v", got, want)
	}
	for _, id := range want {
		c, err := r.Callable(id)
		if code(err) != registry.CodeProhibited || c.ID() != "" || r.Registered(id) || !r.IsProhibited(id) {
			t.Errorf("%s: Callable = (%q, %v), Registered %v, IsProhibited %v", id, c.ID(), err, r.Registered(id), r.IsProhibited(id))
		}
	}
}

// A trust profile's prohibited_capabilities and the descriptors' prohibited flags are two statements of one fact; if they differ, one of them is wrong and the
// registry (which reads the flags) would let an ID through that a profile calls prohibited.
func TestINV05TheTrustProfilesProhibitedListsAgreeWithTheRegistry(t *testing.T) {
	b, err := os.ReadFile(fintechPack)
	if err != nil {
		t.Fatal(err)
	}
	var p contracts.PackV1
	if err := json.Unmarshal(b, &p); err != nil {
		t.Fatal(err)
	}
	r := loadFintech(t)
	if len(p.TrustProfiles) == 0 {
		t.Fatal("no trust profiles in the pack")
	}
	for _, tp := range p.TrustProfiles {
		for _, id := range tp.ProhibitedCapabilities {
			if !r.IsProhibited(string(id)) {
				t.Errorf("trust profile %s prohibits %s, and the registry does not", tp.ProfileId, id)
			}
		}
	}
}

// Whatever combination of prohibited flags a pack carries: a monetary write (R4) that is not prohibited refuses the whole load; otherwise a handle exists exactly for
// the descriptors that are not prohibited.
func TestINV05NoCombinationOfFlagsMakesAProhibitedOrMonetaryCapabilityCallable(t *testing.T) {
	base := descriptors(t)
	var monetary []int
	for i, d := range base {
		if strings.HasPrefix(d.RiskClass, "R4") {
			monetary = append(monetary, i)
		}
	}
	if len(monetary) == 0 {
		t.Fatal("no monetary-write descriptor in the pack to test")
	}
	rapid.Check(t, func(rt *rapid.T) {
		ds := append([]contracts.CapabilityDescriptorV1(nil), base...)
		// Every other descriptor takes any flag; the monetary ones are marked, except that one of them may be left unmarked.
		unmark := rapid.IntRange(-1, len(monetary)-1).Draw(rt, "unmarked monetary write (-1: none)")
		for i := range ds {
			ds[i].Prohibited = rapid.Bool().Draw(rt, "prohibited")
		}
		for n, i := range monetary {
			ds[i].Prohibited = n != unmark
		}
		unmarkedMonetary := unmark >= 0
		r, err := registry.New(ds)
		if unmarkedMonetary {
			if err == nil || code(err) != registry.CodeSafetyBlock {
				rt.Fatalf("a monetary write that is not prohibited was accepted (%v)", err)
			}
			return
		}
		if err != nil {
			rt.Fatalf("a valid pack was refused: %v", err)
		}
		for _, d := range ds {
			id := string(d.CapabilityId)
			c, cerr := r.Callable(id)
			if d.Prohibited != (cerr != nil) || d.Prohibited != (c.ID() == "") || d.Prohibited != r.IsProhibited(id) {
				rt.Fatalf("%s: prohibited %v, Callable = (%q, %v), IsProhibited %v", id, d.Prohibited, c.ID(), cerr, r.IsProhibited(id))
			}
		}
	})
}
