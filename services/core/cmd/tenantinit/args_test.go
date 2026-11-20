package main

// TRU-02 (ruling A484): the arguments of the command. Every bad argument is a usage refusal (exit 2) before any file is read or connection is opened, with one fixed line on stderr
// that repeats no value (AGENT-13, A347). REQ-SEC-004, INV-12 (no identity flag: nothing but the session's login decides who writes).

import (
	"fmt"
	"strings"
	"testing"
)

func TestBadArgumentsAreUsageRefusalsBeforeAnyConnection(t *testing.T) { // AGENT-13, A347
	w := newWorld(t)
	good := func(tenants ...string) []string { return w.args("1", tenants...) }
	const ok = tenantA + "=Synthetic Bank A"
	var seventeen []string
	for i := 0; i < 17; i++ {
		seventeen = append(seventeen, fmt.Sprintf("ten_c%02d=Name", i))
	}
	cases := map[string][]string{
		"no tenant":                       good(),
		"seventeen tenants":               good(seventeen...),
		"id too short":                    good("ten_a=Name"),
		"id upper-case prefix":            good("TEN_aa=Name"),
		"id with a symbol":                good("ten_aa!=Name"),
		"id with a space":                 good("ten_a b=Name"),
		"id of 33 characters":             good("ten_" + strings.Repeat("a", 33) + "=Name"),
		"no prefix":                       good("aa=Name"),
		"empty id":                        good("=Name"),
		"no equals sign":                  good(tenantA),
		"empty name":                      good(tenantA + "="),
		"name of 65 characters":           good(tenantA + "=" + strings.Repeat("n", 65)),
		"name of 65 multibyte characters": good(tenantA + "=" + strings.Repeat("é", 65)),
		"name with a newline":             good(tenantA + "=SECRET-NAME\nsecond line"),
		"name with a tab":                 good(tenantA + "=SECRET\tNAME"),
		"name with a bell":                good(tenantA + "=SECRET\aNAME"),
		"name with a delete":              good(tenantA + "=SECRET\x7fNAME"),
		"name with a carriage return":     good(tenantA + "=SECRET\rNAME"),
		"name that is not UTF-8":          good(tenantA + "=SECRET\xffNAME"),
		"name with a leading space":       good(tenantA + "= SECRET-NAME"),
		"name with a trailing space":      good(tenantA + "=SECRET-NAME "),
		"the same id twice":               good(ok, ok),
		"the same id under two names":     good(tenantA+"=One", tenantA+"=Two"),
		"overlap 0":                       w.args("0", ok),
		"overlap -1":                      w.args("-1", ok),
		"overlap 17":                      w.args("17", ok),
		"overlap not a number":            w.args("abc", ok),
		"overlap empty":                   w.args("", ok),
		"overlap a fraction":              w.args("1.5", ok),
		"overlap beyond an int":           w.args("99999999999999999999", ok),
		"an identity flag":                append(good(ok), "--published-by", "adm_ops01"),
		"an unknown flag":                 append(good(ok), "--tenant-name", "x"),
		"an unknown boolean flag":         append(good(ok), "--force"),
		"a positional argument":           append(good(ok), "extra"),
		"a positional argument after --":  append(good(ok), "--", "extra"),
		"help":                            {"--help"},
		"short help":                      {"-h"},
		"nothing":                         nil,
		"a repeated dsn file":             append(good(ok), "--dsn-file", "/other"),
		"a repeated key dir":              append(good(ok), "--key-dir", "/other"),
		"a repeated overlap":              append(good(ok), "--key-max-active", "1"),
		"an empty dsn file":               {"--dsn-file", "", "--key-dir", w.keyDir, "--key-max-active", "1", "--tenant", ok},
		"an empty key dir":                {"--dsn-file", w.dsnPath, "--key-dir", "", "--key-max-active", "1", "--tenant", ok},
		"no dsn file":                     {"--key-dir", w.keyDir, "--key-max-active", "1", "--tenant", ok},
		"no key dir":                      {"--dsn-file", w.dsnPath, "--key-max-active", "1", "--tenant", ok},
		"no overlap":                      {"--dsn-file", w.dsnPath, "--key-dir", w.keyDir, "--tenant", ok},
		"a tenant flag without a value":   {"--dsn-file", w.dsnPath, "--key-dir", w.keyDir, "--key-max-active", "1", "--tenant"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			r := w.run(args...)
			if r.code != exitUsage || r.out != "" || r.err != usageLine+"\n" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 2, nothing and %q", r.code, r.out, r.err, usageLine+"\n")
			}
			mustContainNone(t, "stderr", r.err, "SECRET", "Name", w.keyDir, w.dsnPath)
			if w.dials != 0 {
				t.Fatalf("%d connections opened for a bad command line, want none", w.dials)
			}
			w.nothingWritten()
		})
	}
}

func TestAValidCommandLineInTheEqualsFormIsAccepted(t *testing.T) { // the flag package's own forms stay usable for a Compose command
	w := newWorld(t)
	r := w.run("--dsn-file="+w.dsnPath, "--key-dir="+w.keyDir, "--key-max-active=1", "--tenant="+tenantA+"=Synthetic Bank A")
	if r.code != exitDone || r.out != outLine(1, 0, 1, 0) {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and %q", r.code, r.out, r.err, outLine(1, 0, 1, 0))
	}
}
