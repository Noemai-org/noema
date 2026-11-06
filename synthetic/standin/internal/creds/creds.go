// Package creds creates the stand-in's credential files (bench token, runtime token, principal key) the first time a volume is used. The values are
// random, are never printed, and an existing value is never replaced. Nothing is written into the repository.
package creds

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Init creates each missing file with 48 random bytes as hex. The files are read-only for everyone: the service and its callers run as different
// users and the volume is local.
func Init(paths []string) error {
	if len(paths) == 0 {
		return errors.New("give at least one -file")
	}
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			return fmt.Errorf("%s is named twice; each credential needs its own file", p)
		}
		seen[p] = true
	}
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Size() > 0 {
			continue
		}
		var raw [48]byte
		if _, err := rand.Read(raw[:]); err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, []byte(hex.EncodeToString(raw[:])+"\n"), 0o444); err != nil {
			return err
		}
	}
	return nil
}
