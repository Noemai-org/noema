//go:build !unix

package main

import "os"

// fileOwner says nothing on a platform without a Unix owner, so the DSN file is refused there (fail closed).
func fileOwner(os.FileInfo) (uint32, bool) { return 0, false }

// openDSN refuses on a platform without no-follow semantics (fail closed, as fileOwner does).
func openDSN(string) (*os.File, error) { return nil, errDSNUnusable }
