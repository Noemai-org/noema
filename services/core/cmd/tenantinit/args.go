package main

import (
	"errors"
	"flag"
	"io"
	"strconv"
)

const (
	// maxKeyFiles is an operating limit of this command, not a safety threshold: a seeding run mounts a handful of keys.
	maxKeyFiles = 16

	usage = "usage: tenantinit --dsn-file FILE --key-dir DIR --key-max-active N"
)

// errArgument marks an argument value the command refuses. The flag package's own error would quote the value, so it is never printed: the caller prints the usage line only.
var errArgument = errors.New("tenantinit: an argument is refused")

// settings are the validated arguments of a run.
type settings struct {
	dsnFile, keyDir string
	keyMaxActive    int
}

// onceString is a single-value flag: empty is refused, and so is a second occurrence.
type onceString struct {
	set bool
	val string
}

func (o *onceString) String() string { return o.val }

func (o *onceString) Set(v string) error {
	if o.set || v == "" {
		return errArgument
	}
	o.set, o.val = true, v
	return nil
}

// onceCount is the --key-max-active flag: one integer from 1 to maxKeyFiles, given once. The database's own guard limits how many keys are active (migration 0013); this bound only stops a typo.
type onceCount struct {
	set bool
	val int
}

func (o *onceCount) String() string { return strconv.Itoa(o.val) }

func (o *onceCount) Set(v string) error {
	n, err := strconv.Atoi(v)
	if o.set || err != nil || n < 1 || n > maxKeyFiles {
		return errArgument
	}
	o.set, o.val = true, n
	return nil
}

// parseArgs validates the arguments. false is a usage refusal; nothing about the arguments is reported beyond that.
func parseArgs(args []string) (settings, bool) {
	var dsn, keyDir onceString
	var maxActive onceCount
	fs := flag.NewFlagSet("tenantinit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&dsn, "dsn-file", "a file holding the DSN of the operator's superuser login")
	fs.Var(&keyDir, "key-dir", "the directory of the core-trust signing keys")
	fs.Var(&maxActive, "key-max-active", "how many signing keys may be mounted at once (no default)")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return settings{}, false
	}
	if !dsn.set || !keyDir.set || !maxActive.set {
		return settings{}, false
	}
	return settings{dsnFile: dsn.val, keyDir: keyDir.val, keyMaxActive: maxActive.val}, true
}
