// Package signingtest makes SYNTHETIC core-execution signing keys for tests (AGENT-25): a ring on disk, as the deploy step would write it, and
// the registration of its public keys in a test database, as the deploy step's key_register call does (DEC-2026-11-01 D3a). Nothing here is
// used outside tests, and no key it makes is a secret.
package signingtest

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"syscall"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Noemai-org/noema/services/core/execution/signing"
)

// Identity is the one identity that signs receipts (D2).
const Identity = "core-execution"

// keyFileMode is 0400, as the deploy step writes a key file (the same expression execution/signing uses).
var keyFileMode = os.FileMode(syscall.S_IRUSR)

// Ring writes one key file per version into a temporary directory (mode 0400, as the deploy step does) and loads them. The seeds are derived from the
// test name and the version, so a run is repeatable and no two tests share a key.
func Ring(t testing.TB, versions ...int) *signing.Ring {
	t.Helper()
	r, err := RingAt(t.TempDir(), t.Name(), versions...)
	if err != nil {
		t.Fatalf("signingtest: %v", err)
	}
	return r
}

// RingAt is Ring for a caller that holds one ring for a whole package (a simulation that builds thousands of controllers): it writes into dir,
// which must exist and be private to the caller, and derives the seeds from label.
func RingAt(dir, label string, versions ...int) (*signing.Ring, error) {
	for _, v := range versions {
		sum := sha256.Sum256([]byte(fmt.Sprintf("SYNTHETIC %s %d", label, v)))
		path := filepath.Join(dir, fmt.Sprintf("%s.ed25519.%d.key", Identity, v))
		if err := os.WriteFile(path, []byte(hex.EncodeToString(sum[:])+"\n"), keyFileMode); err != nil {
			return nil, err
		}
	}
	return signing.LoadRing(Identity, dir, signing.Options{MaxActive: max(len(versions), 1)})
}

// Register adds every public key of the ring to signing_key through key_register, using a pool that may call it (the migration role).
func Register(t testing.TB, admin *pgxpool.Pool, r *signing.Ring) {
	t.Helper()
	pubs := r.PublicKeys()
	versions := make([]int, 0, len(pubs))
	byVersion := map[int]ed25519.PublicKey{}
	for id, pub := range pubs {
		var version int
		if _, err := fmt.Sscanf(id, Identity+".ed25519.%d", &version); err != nil {
			t.Fatalf("signingtest: key id %q: %v", id, err)
		}
		versions = append(versions, version)
		byVersion[version] = pub
	}
	sort.Ints(versions) // key_register wants each version newer than the last
	for _, v := range versions {
		if _, err := admin.Exec(contextTimeout(t), `SELECT key_register($1, $2, $3)`, Identity, v, []byte(byVersion[v])); err != nil {
			t.Fatalf("signingtest: key_register %s.ed25519.%d: %v", Identity, v, err)
		}
	}
}
