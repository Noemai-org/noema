package main

import (
	"errors"
	"flag"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Noemai-org/noema/services/core/record/chain"
)

const (
	// maxTenants and maxKeyFiles are operating limits of this command, not safety thresholds: a seeding run names a handful of tenants and mounts a handful of keys.
	maxTenants  = 16
	maxKeyFiles = 16
	maxNameLen  = 64

	usage = "usage: tenantinit --dsn-file FILE --key-dir DIR --key-max-active N --tenant ten_ID=NAME [--tenant ten_ID=NAME ...]"
)

// errArgument marks an argument value the command refuses. The flag package's own error would quote the value, so it is never printed: the caller prints the usage line only.
var errArgument = errors.New("tenantinit: an argument is refused")

// tenantSpec is one --tenant value: the ID and the name of a tenant row.
type tenantSpec struct{ id, name string }

// settings are the validated arguments of a run.
type settings struct {
	dsnFile, keyDir string
	keyMaxActive    int
	tenants         []tenantSpec
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

// tenantList collects the --tenant flags: each is ten_ID=NAME, the ID is split at the first "=", an ID is given once, and there are at most maxTenants.
type tenantList []tenantSpec

func (l *tenantList) String() string { return "" }

func (l *tenantList) Set(v string) error {
	id, name, found := strings.Cut(v, "=")
	if !found || !chain.ValidTenantID(id) || !validName(name) || len(*l) >= maxTenants {
		return errArgument
	}
	for _, t := range *l {
		if t.id == id {
			return errArgument
		}
	}
	*l = append(*l, tenantSpec{id: id, name: name})
	return nil
}

// validName reports whether name is 1 to maxNameLen printable characters, valid UTF-8, with no control character, no line break and no space at either end.
func validName(name string) bool {
	if !utf8.ValidString(name) {
		return false
	}
	if n := utf8.RuneCountInString(name); n < 1 || n > maxNameLen {
		return false
	}
	if name[0] == ' ' || name[len(name)-1] == ' ' {
		return false
	}
	for _, r := range name {
		if !unicode.IsPrint(r) {
			return false
		}
	}
	return true
}

// parseArgs validates the arguments. false is a usage refusal; nothing about the arguments is reported beyond that.
func parseArgs(args []string) (settings, bool) {
	var dsn, keyDir onceString
	var maxActive onceCount
	var tenants tenantList
	fs := flag.NewFlagSet("tenantinit", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Var(&dsn, "dsn-file", "a file holding the DSN of the operator's superuser login")
	fs.Var(&keyDir, "key-dir", "the directory of the core-trust signing keys")
	fs.Var(&maxActive, "key-max-active", "how many signing keys may be mounted at once (no default)")
	fs.Var(&tenants, "tenant", "ten_ID=NAME; repeat for each tenant")
	if err := fs.Parse(args); err != nil || fs.NArg() != 0 {
		return settings{}, false
	}
	if !dsn.set || !keyDir.set || !maxActive.set || len(tenants) == 0 {
		return settings{}, false
	}
	return settings{dsnFile: dsn.val, keyDir: keyDir.val, keyMaxActive: maxActive.val, tenants: tenants}, true
}
