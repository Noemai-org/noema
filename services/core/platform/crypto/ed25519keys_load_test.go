package crypto

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
)

// REQ-TST-010, REQ-REC-008 (receipt key source), INV-11: the small-order table loads through an error, never a panic at start-up.
func TestShippedSmallOrderTableLoads(t *testing.T) {
	got, err := loadSmallOrderY(smallOrderHex)
	if err != nil || len(got) != len(smallOrderHex) {
		t.Fatalf("shipped table: %d entries, err %v", len(got), err)
	}
	if smallOrderErr != nil {
		t.Fatalf("package table did not load: %v", smallOrderErr)
	}
}

func TestMalformedSmallOrderTableIsRefused(t *testing.T) {
	for name, h := range map[string]string{
		"not hex":   strings.Repeat("zz", 32),
		"too short": strings.Repeat("00", 31),
		"too long":  strings.Repeat("00", 33),
		"empty":     "",
	} {
		if got, err := loadSmallOrderY([]string{h}); err == nil || got != nil {
			t.Errorf("%s: want a refusal, got %d entries and err %v", name, len(got), err)
		}
	}
}

// A table that failed to load must refuse every key, even a good one (fail closed).
func TestFailedTableLoadRefusesEveryKey(t *testing.T) {
	good, _, err := ed25519.GenerateKey(rand.Reader) // SYNTHETIC, random per run
	if err != nil {
		t.Fatal(err)
	}
	saved := smallOrderErr
	t.Cleanup(func() { smallOrderErr = saved })
	smallOrderErr = errSmallOrderTable
	if UsableEd25519PublicKey(good) {
		t.Fatal("a key was trusted although the small-order table did not load")
	}
}
