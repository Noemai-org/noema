package e2e_test

import (
	"flag"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// noema#525: the harness starts the mock-case on port 0 and reads the addresses the process reports, so no port is chosen before it is bound.

func TestParseListeningReadsTheAddressesTheProcessBound(t *testing.T) {
	line := `{"time":"2026-11-13T16:00:00Z","level":"INFO","msg":"mock-case listening (SYNTHETIC)","case":"127.0.0.1:41233","bench":"127.0.0.1:41234"}`
	c, b, ok, err := parseListening(line)
	if err != nil || !ok || c != "127.0.0.1:41233" || b != "127.0.0.1:41234" {
		t.Fatalf("got %q %q %v %v", c, b, ok, err)
	}
}

func TestParseListeningIgnoresEveryOtherLine(t *testing.T) {
	for _, line := range []string{"", "not json", `{"msg":"mock-case started"}`, `{"msg":"mock-case listening"}`, `{"msg":"mock-case stopped","err":"listen tcp 127.0.0.1:1: bind: address already in use"}`} {
		if c, b, ok, err := parseListening(line); ok || err != nil || c != "" || b != "" {
			t.Errorf("%q: %q %q %v %v, want it ignored", line, c, b, ok, err)
		}
	}
}

func TestParseListeningRefusesAnAddressThatIsNotALoopbackWithABoundPort(t *testing.T) {
	for name, addrs := range map[string][2]string{
		"every interface":  {"[::]:8090", "127.0.0.1:41234"},
		"a public address": {"0.0.0.0:8090", "127.0.0.1:41234"},
		"port zero":        {"127.0.0.1:0", "127.0.0.1:41234"},
		"no port":          {"127.0.0.1", "127.0.0.1:41234"},
		"bench not local":  {"127.0.0.1:41233", "10.0.0.1:41234"},
		"empty":            {"", ""},
	} {
		line := `{"msg":"mock-case listening (SYNTHETIC)","case":"` + addrs[0] + `","bench":"` + addrs[1] + `"}`
		if _, _, ok, err := parseListening(line); ok || err == nil {
			t.Errorf("%s: accepted (ok %v, err %v)", name, ok, err)
		}
	}
}

// parallelSlots is how many tests `go test` may run in parallel at once (the -parallel flag, GOMAXPROCS by default).
func parallelSlots(t *testing.T) int {
	t.Helper()
	f := flag.Lookup("test.parallel")
	if f == nil {
		t.Fatal("the test.parallel flag is not defined")
	}
	n, err := strconv.Atoi(f.Value.String())
	if err != nil || n < 1 {
		t.Fatalf("the test.parallel flag is %q", f.Value.String())
	}
	return n
}

// Several mock-cases started at once, all alive at the same time, each hold their own pair of listeners: the addresses are all different, and each process holds the port it
// reported (a second bind on it fails). A harness that picked ports before binding them could hand two processes the same one. The group is as large as the parallel slots
// allow, at most four, because its subtests wait for each other: with fewer slots than subtests the ones holding the slots would wait for the ones that cannot start and the
// run would hang until the test timeout (EL's finding on noema#526). The property needs two processes at once at least, so a limit of one fails loudly instead of hanging.
func TestMockCasesStartedAtOnceEachHoldTheirOwnListeners(t *testing.T) {
	n := parallelSlots(t)
	if n > 4 {
		n = 4
	}
	if n < 2 {
		t.Fatalf("this test needs `-parallel 2` or more (it has %d): it starts mock-cases at once and a limit of one cannot start two", n)
	}
	var (
		mu    sync.Mutex
		addrs = map[string]int{}
		alive sync.WaitGroup
	)
	alive.Add(n)
	t.Run("group", func(t *testing.T) {
		for i := 0; i < n; i++ {
			i := i
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				t.Parallel()
				var once sync.Once
				done := func() { once.Do(alive.Done) }
				defer done() // a failed start must not leave the others waiting
				mc := startMockCase(t)
				for _, u := range []string{mc.URL, mc.BenchURL} {
					addr := strings.TrimPrefix(u, "http://")
					mu.Lock()
					if other, dup := addrs[addr]; dup {
						t.Errorf("mock-case %d and %d report the same address %s", other-1, i, addr)
					}
					addrs[addr] = i + 1
					mu.Unlock()
					if l, err := net.Listen("tcp", addr); err == nil {
						_ = l.Close()
						t.Errorf("mock-case %d: %s is not held by the process that reported it", i, addr)
					}
				}
				done()
				alive.Wait() // every process is alive at once before any subtest ends and its cleanup kills its process
			})
		}
	})
}
