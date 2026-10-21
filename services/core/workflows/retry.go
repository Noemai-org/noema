package workflows

// Kind says whether an activity can change anything outside Noema's own reads.
type Kind string

// The two kinds. DEC-2026-10-20 D8: write activities run with MaximumAttempts 1; read activities retry with bounded backoff.
const (
	Write Kind = "WRITE"
	Read  Kind = "READ"
)

// RetryPolicy is the declaration a workflow turns into Temporal activity options. MaximumAttempts is never 0:
// in Temporal 0 means unlimited. BackoffSeconds are the waits between attempts of a read.
type RetryPolicy struct {
	MaximumAttempts int
	BackoffSeconds  []int
}

// Activity is one activity of the POC and how it retries.
type Activity struct {
	Name   string
	Kind   Kind
	Policy RetryPolicy
	// Why cites the source of the policy.
	Why string
}

// Activities is the table every workflow definition reads its options from. A write activity has one
// attempt: a further attempt goes back through Trust for a new grant after verify-by-key (REQ-EXE-006,
// REQ-RCV-002), never through a Temporal retry.
var Activities = []Activity{
	{Name: "ExecuteGrant", Kind: Write, Policy: RetryPolicy{MaximumAttempts: 1},
		Why: "spend-and-dispatch (PERD 27.1): E9 is the only external write; REQ-EXE-006, DEC-2026-10-20 D8"},
	{Name: "RecoverySweep", Kind: Write, Policy: RetryPolicy{MaximumAttempts: 1},
		Why: "writes receipts (PERD 27.1); the next sweep tick is the retry; DEC-2026-10-20 D8"},
	{Name: "VerifyRead", Kind: Read, Policy: RetryPolicy{MaximumAttempts: 3, BackoffSeconds: []int{1, 3, 9}},
		Why: "REQ-VER-001: three attempts with backoff of 1, 3 and 9 s of simulated time [ADOPTED DEC-2026-10-17]"},
}

// PolicyFor returns the policy of a named activity.
func PolicyFor(name string) (Activity, bool) {
	for _, a := range Activities {
		if a.Name == name {
			return a, true
		}
	}
	return Activity{}, false
}
