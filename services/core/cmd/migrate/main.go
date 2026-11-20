// Command migrate applies the database migrations once and exits (EXE-08; A484; REQ-DATA-001). See README.md.
//
//	migrate --dsn-file FILE --migrations-dir DIR
package main

import (
	"io"
	"os"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const exitCannot = 2

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, clock.System{})) }

func run(args []string, out, errw io.Writer, clk clock.Clock) int {
	return exitCannot
}
