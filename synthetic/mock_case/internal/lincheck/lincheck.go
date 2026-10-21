// Package lincheck decides whether a concurrent history of operations is linearizable: whether the operations
// can be put in one sequential order that respects real time and obeys a sequential specification (Herlihy and
// Wing, 1990). It is a Porcupine-style checker in the Wing and Gong tradition with memoisation, written against
// the standard library only (PERD §27.3, DEC-2026-10-20 D7).
//
// An operation whose response was never seen (a timeout, a dropped connection, a 5xx) is Pending. It may have
// taken effect at any time after it was invoked, or never: the checker tries both. That is what lets a history
// recorded under commit-then-drop faults be checked honestly, without guessing what the lost response said.
package lincheck

import (
	"encoding/binary"
	"sort"
)

// Operation is one invocation with its response.
type Operation struct {
	ClientID int
	Input    any
	Output   any   // ignored when Pending
	Call     int64 // when the invocation was sent; all Call and Return values must be distinct
	Return   int64 // when the response arrived; ignored when Pending
	Pending  bool
}

// Model is a sequential specification. States must be comparable through Key.
type Model struct {
	Init func() any
	// Step applies one operation to a state. For a completed operation, ok reports whether the recorded output is
	// what the specification gives; for a Pending one the output is unknown and Step returns the state the
	// operation would leave if it took effect, with ok true.
	Step func(state, input, output any, pending bool) (next any, ok bool)
	Key  func(state any) string
}

// Result is the verdict for one history.
type Result struct {
	Linearizable bool
	Inconclusive bool  // the node budget ran out before a verdict
	Nodes        int   // steps tried
	Deepest      []int // the longest linearization prefix found, as indexes into the history (for diagnosis)
}

type bitset []uint64

func newBitset(n int) bitset    { return make(bitset, (n+63)/64) }
func (b bitset) set(i int)      { b[i/64] |= 1 << (uint(i) % 64) }
func (b bitset) clear(i int)    { b[i/64] &^= 1 << (uint(i) % 64) }
func (b bitset) has(i int) bool { return b[i/64]&(1<<(uint(i)%64)) != 0 }
func (b bitset) key(stateKey string) string {
	buf := make([]byte, 8*len(b), 8*len(b)+len(stateKey))
	for i, w := range b {
		binary.LittleEndian.PutUint64(buf[8*i:], w)
	}
	return string(append(buf, stateKey...))
}

const never = int64(1) << 62

// Check searches for a linearization. maxNodes bounds the work (0 means 5 million steps).
func Check(m Model, history []Operation, maxNodes int) Result {
	if maxNodes <= 0 {
		maxNodes = 5_000_000
	}
	ops := append([]Operation(nil), history...)
	sort.SliceStable(ops, func(i, j int) bool { return ops[i].Call < ops[j].Call })
	n := len(ops)
	ret := make([]int64, n)
	for i, op := range ops {
		ret[i] = op.Return
		if op.Pending {
			ret[i] = never
		}
	}
	res := Result{}
	done := newBitset(n)
	failed := map[string]bool{}
	path := make([]int, 0, n)

	var search func(state any, left int) bool // left: completed operations not yet linearized
	search = func(state any, left int) bool {
		if left == 0 {
			return true // every remaining operation is Pending, and a Pending operation may never have happened
		}
		if res.Nodes > maxNodes {
			res.Inconclusive = true
			return false
		}
		memo := done.key(m.Key(state))
		if failed[memo] {
			return false
		}
		minReturn := never
		for i := 0; i < n; i++ {
			if !done.has(i) && ret[i] < minReturn {
				minReturn = ret[i]
			}
		}
		for i := 0; i < n; i++ {
			if done.has(i) || ops[i].Call > minReturn {
				continue
			}
			res.Nodes++
			next, ok := m.Step(state, ops[i].Input, ops[i].Output, ops[i].Pending)
			if !ok {
				continue
			}
			done.set(i)
			path = append(path, i)
			if len(path) > len(res.Deepest) {
				res.Deepest = append(res.Deepest[:0], path...)
			}
			rest := left
			if !ops[i].Pending {
				rest--
			}
			if search(next, rest) {
				return true
			}
			path = path[:len(path)-1]
			done.clear(i)
			if res.Inconclusive {
				return false
			}
		}
		failed[memo] = true
		return false
	}

	left := 0
	for _, op := range ops {
		if !op.Pending {
			left++
		}
	}
	res.Linearizable = search(m.Init(), left)
	return res
}

// CheckPartitioned checks each partition of a history on its own, as Porcupine does: operations on different
// keys do not constrain each other, so each key's history is checked alone. It returns the first failing key.
func CheckPartitioned(m Model, history []Operation, key func(Operation) string, maxNodes int) (failedKey string, r Result) {
	groups := map[string][]Operation{}
	var order []string
	for _, op := range history {
		k := key(op)
		if _, seen := groups[k]; !seen {
			order = append(order, k)
		}
		groups[k] = append(groups[k], op)
	}
	sort.Strings(order)
	r = Result{Linearizable: true}
	for _, k := range order {
		one := Check(m, groups[k], maxNodes)
		r.Nodes += one.Nodes
		if !one.Linearizable {
			return k, one
		}
	}
	return "", r
}
