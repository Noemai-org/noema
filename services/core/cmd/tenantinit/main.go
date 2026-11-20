// Command tenantinit creates the tenant rows and registers the core-trust public keys, once, before the stack serves (TRU-02; ruling A484; INV-11, INV-12; REQ-SEC-004, REQ-SEC-005).
// See README.md.
//
//	tenantinit --dsn-file FILE --key-dir DIR --key-max-active N --tenant ten_ID=NAME [--tenant ten_ID=NAME ...]
//
// SCAFFOLD: names and stubs only. The behaviour follows in the next commit.
package main

import (
	"context"
	"io"
	"os"

	"github.com/jackc/pgx/v5"
)

const (
	exitDone    = 0
	exitRefused = 1
	exitUsage   = 2
	exitFailure = 3
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// dialFunc opens the one connection of a run from the DSN file. Tests pass a counting wrapper of connect.
type dialFunc func(ctx context.Context, dsnFile string) (*pgx.Conn, error)

func run(args []string, out, errw io.Writer) int { return runWith(args, out, errw, connect) }

func runWith(args []string, out, errw io.Writer, dial dialFunc) int {
	_, _, _, _ = args, out, errw, dial
	return exitDone
}

// connect reads the DSN from its file and opens one connection.
func connect(ctx context.Context, path string) (*pgx.Conn, error) {
	_, _ = ctx, path
	return nil, nil
}
