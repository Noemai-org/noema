//go:build !unix

package main

import "os"

// fileOwner says nothing on a platform without a Unix owner, so the DSN file is refused there (fail closed).
func fileOwner(os.FileInfo) (uint32, bool) { return 0, false }
