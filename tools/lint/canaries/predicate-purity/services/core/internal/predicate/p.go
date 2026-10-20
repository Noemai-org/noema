// Package predicate is a canary: the predicate language reads the environment.
package predicate

import "os"

// Eval is impure: it performs I/O.
func Eval() string { return os.Getenv("X") }
