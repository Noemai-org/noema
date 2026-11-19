// Command policypublish publishes the policy version of a pack for one tenant (TRU-02; REQ-TRU-005, REQ-OPS-003; INV-09). Stub: the RED commit.
package main

import (
	"io"
	"os"

	"github.com/Noemai-org/noema/services/core/platform/clock"
)

const (
	exitDone    = 0
	exitRefused = 1
	exitCannot  = 2
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr, clock.System{})) }

func run(args []string, out, errw io.Writer, clk clock.Clock) int {
	_, _, _, _ = args, out, errw, clk
	return exitCannot
}
