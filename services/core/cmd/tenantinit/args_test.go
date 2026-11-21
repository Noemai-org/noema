package main

// TRU-02 (rulings A484, A489): the arguments of the command. Every bad argument is a usage refusal (exit 2) before any file is read or connection is opened, with one fixed line on
// stderr that repeats no value (AGENT-13, A347). REQ-SEC-004, INV-12 (no identity flag: nothing but the session's login decides who writes). The command takes no tenant argument: tenant
// rows have one creator, create-tenants.sh (A353, A489).

import "testing"

func TestBadArgumentsAreUsageRefusalsBeforeAnyConnection(t *testing.T) { // AGENT-13, A347
	w := newWorld(t)
	good := func() []string { return w.args("1") }
	cases := map[string][]string{
		"a tenant flag":                    append(good(), "--tenant", tenantA+"=SECRET-NAME"), // A489: tenants are not this command's
		"a tenant flag in the equals form": append(good(), "--tenant="+tenantA+"=SECRET-NAME"),
		"a tenant flag without a value":    append(good(), "--tenant"),
		"overlap 0":                        w.args("0"),
		"overlap -1":                       w.args("-1"),
		"overlap 17":                       w.args("17"),
		"overlap not a number":             w.args("abc"),
		"overlap empty":                    w.args(""),
		"overlap a fraction":               w.args("1.5"),
		"overlap beyond an int":            w.args("99999999999999999999"),
		"an identity flag":                 append(good(), "--published-by", "adm_ops01"),
		"an unknown flag":                  append(good(), "--tenant-name", "x"),
		"an unknown boolean flag":          append(good(), "--force"),
		"a positional argument":            append(good(), "extra"),
		"a positional argument after --":   append(good(), "--", "extra"),
		"help":                             {"--help"},
		"short help":                       {"-h"},
		"nothing":                          nil,
		"a repeated dsn file":              append(good(), "--dsn-file", "/other"),
		"a repeated key dir":               append(good(), "--key-dir", "/other"),
		"a repeated overlap":               append(good(), "--key-max-active", "1"),
		"an empty dsn file":                {"--dsn-file", "", "--key-dir", w.keyDir, "--key-max-active", "1"},
		"an empty key dir":                 {"--dsn-file", w.dsnPath, "--key-dir", "", "--key-max-active", "1"},
		"no dsn file":                      {"--key-dir", w.keyDir, "--key-max-active", "1"},
		"no key dir":                       {"--dsn-file", w.dsnPath, "--key-max-active", "1"},
		"no overlap":                       {"--dsn-file", w.dsnPath, "--key-dir", w.keyDir},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			r := w.run(args...)
			if r.code != exitUsage || r.out != "" || r.err != usageLine+"\n" {
				t.Fatalf("code %d, stdout %q, stderr %q; want 2, nothing and %q", r.code, r.out, r.err, usageLine+"\n")
			}
			mustContainNone(t, "stderr", r.err, "SECRET", w.keyDir, w.dsnPath)
			if w.dials != 0 {
				t.Fatalf("%d connections opened for a bad command line, want none", w.dials)
			}
			w.nothingWritten()
		})
	}
}

func TestAValidCommandLineInTheEqualsFormIsAccepted(t *testing.T) { // the flag package's own forms stay usable for a Compose command
	w := newWorld(t)
	r := w.run("--dsn-file="+w.dsnPath, "--key-dir="+w.keyDir, "--key-max-active=1")
	if r.code != exitDone || r.out != outLine(1, 0) {
		t.Fatalf("code %d, stdout %q, stderr %q; want 0 and %q", r.code, r.out, r.err, outLine(1, 0))
	}
}
