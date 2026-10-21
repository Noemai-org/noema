package controller_test

import (
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/Noemai-org/noema/services/core/execution/controller"
)

var secret = []byte("synthetic-simulation-key-not-a-secret")

// controllerFor wires a controller to the world. boundary may be nil.
func controllerFor(w *World, boundary func(controller.Point) error) *controller.Controller {
	return &controller.Controller{Clock: w.Clock, Keys: w.Keys, ReceiptKeyID: KeyID, Grants: w, Bind: w, Reval: w, Ledger: w, Out: w, IDs: w, Boundary: boundary}
}

func deadline() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// actor is one goroutine under the scheduler. Exactly one actor runs at a time, so a run is a pure function
// of its seed.
type actor struct {
	resume   chan bool // true means: crash at this boundary
	parked   chan struct{}
	finished chan struct{}
	crashes  bool
}

func newActor(crashes bool) *actor {
	return &actor{resume: make(chan bool), parked: make(chan struct{}), finished: make(chan struct{}), crashes: crashes}
}

// yield parks the actor and reports whether the scheduler decided to crash it here.
func (a *actor) yield() bool {
	a.parked <- struct{}{}
	return <-a.resume
}

func (a *actor) boundary(controller.Point) error {
	if a.yield() {
		return controller.ErrCrash
	}
	return nil
}

// schedule runs the actors to completion in the order the rng picks. crashOneIn is the chance, per boundary,
// that a crashable actor dies there.
func schedule(rng *rand.Rand, actors []*actor, bodies []func(), crashOneIn int) {
	var wg sync.WaitGroup
	for i := range actors {
		wg.Add(1)
		go func(a *actor, body func()) {
			defer wg.Done()
			<-a.resume // the gate: nothing runs until chosen
			body()
			close(a.finished)
		}(actors[i], bodies[i])
	}
	live := append([]*actor(nil), actors...)
	for len(live) > 0 {
		i := rng.IntN(len(live))
		a := live[i]
		crash := a.crashes && crashOneIn > 0 && rng.IntN(crashOneIn) == 0
		a.resume <- crash
		select {
		case <-a.parked:
		case <-a.finished:
			live = append(live[:i], live[i+1:]...)
		}
	}
	wg.Wait()
}

// Run describes one simulated run.
type Run struct {
	Seed       uint64
	Violations []string
	World      *World
}

func (r *Run) fail(format string, args ...any) {
	r.Violations = append(r.Violations, fmt.Sprintf("seed %d: ", r.Seed)+fmt.Sprintf(format, args...))
}

// Kinds of grant the seed may present.
const (
	grantValid = iota
	grantTampered
	grantExpired
	grantKinds
)

// RunSeed runs the protocol under a seeded scheduler, a fault-injecting network and clock, with concurrent
// executors of one grant, a recovery sweeper and a state mutator, then checks the safety properties of
// REQ-EXE-001 to REQ-EXE-008 and, when nothing blocked the write, convergence to exactly one effect.
func RunSeed(seed uint64) *Run {
	run := &Run{Seed: seed}
	rng := rand.New(rand.NewPCG(seed, 0x9e3779b97f4a7c15))
	w := NewWorld(secret)
	run.World = w
	w.SetFaults(func() FaultMode {
		if rng.IntN(5) < 2 {
			return FaultNone
		}
		return FaultMode(rng.IntN(int(faultCount)))
	})
	now := w.Clock.Now()
	kind := grantValid
	if rng.IntN(8) == 0 {
		kind = grantKinds - 1 - rng.IntN(2) // tampered or expired
	}
	g := w.IssueGrant(1, now.Add(-time.Minute), now.Add(10*time.Minute))
	switch kind {
	case grantTampered:
		g.Grant.PlanStep = 2 // changed after signing
	case grantExpired:
		g = w.IssueGrant(1, now.Add(-20*time.Minute), now.Add(-10*time.Minute))
	}
	w.Store(g)
	if rng.IntN(10) == 0 {
		w.KillSwitch = true
	}
	startKill := w.KillSwitch

	var actors []*actor
	var bodies []func()
	execs := 2 + rng.IntN(3)
	for i := 0; i < execs; i++ {
		a := newActor(true)
		c := controllerFor(w, a.boundary)
		actors = append(actors, a)
		bodies = append(bodies, func() {
			ctx, cancel := deadline()
			defer cancel()
			_, _ = c.Execute(ctx, TenantID, g.Grant.GrantID)
		})
	}
	sw := newActor(false)
	swc := controllerFor(w, nil)
	actors = append(actors, sw)
	bodies = append(bodies, func() {
		for i := 0; i < 2; i++ {
			sw.yield()
			ctx, cancel := deadline()
			_, _ = swc.Recover(ctx, TenantID)
			cancel()
		}
	})
	mutated := false
	if rng.IntN(3) == 0 {
		mutated = true
		m := newActor(false)
		which := rng.IntN(6)
		actors = append(actors, m)
		bodies = append(bodies, func() {
			m.yield()
			w.Mutate(func(w *World) {
				switch which {
				case 0:
					w.KillSwitch = true
				case 1:
					w.DecisionAllowed = false
				case 2:
					w.IntentHash = hashOf("intent-2")
				case 3:
					w.TargetVersion = "v_2"
				case 4:
					w.RevalChanged = append(w.RevalChanged, "ownership")
				case 5:
					w.Clock.Advance(20 * time.Minute)
				}
			})
		})
	}
	schedule(rng, actors, bodies, 14)

	key := keyOf(w)
	before := w.Snapshot()
	ctx, cancel := deadline()
	_, _ = controllerFor(w, nil).Recover(ctx, TenantID)
	cancel()
	after := w.Snapshot()
	if len(after.Dispatches) != len(before.Dispatches) {
		run.fail("the sweeper dispatched (%d -> %d)", len(before.Dispatches), len(after.Dispatches))
	}
	if after.Pending != 0 {
		run.fail("%d spent attempts still have no receipt after the sweeper", after.Pending)
	}
	checkSafety(run, w.Snapshot(), kind, startKill)

	if kind == grantValid && !startKill && !mutated {
		converge(run, w, key, g)
		checkSafety(run, w.Snapshot(), kind, startKill)
		if n := w.Truth(key); n != 1 {
			run.fail("after recovery the target has %d effects, want exactly 1", n)
		}
	}
	return run
}

func keyOf(w *World) string {
	k := w.PlanFor().KeyFields
	return keyFor(k)
}

// converge stands in for Outcome and Trust: it reads the target's truth and, while there is no effect, records
// a not-achieved verdict and presents a new grant (or the same one, if it was never spent).
func converge(run *Run, w *World, key string, first controller.SignedGrant) {
	next := 2
	for round := 0; round < 4; round++ {
		ctx, cancel := deadline()
		_, _ = controllerFor(w, nil).Recover(ctx, TenantID)
		cancel()
		if w.Truth(key) == 1 {
			return
		}
		w.SetFaults(func() FaultMode { return FaultNone })
		snap := w.Snapshot()
		grantID := first.Grant.GrantID
		if snap.Spent[grantID] || snap.Voids[grantID] != "" {
			w.RecordVerdict(key, "NOT_ACHIEVED")
			now := w.Clock.Now()
			ng := w.IssueGrant(next, now.Add(-time.Minute), now.Add(10*time.Minute))
			w.Store(ng)
			grantID = ng.Grant.GrantID
			next++
		}
		ctx, cancel = deadline()
		_, _ = controllerFor(w, nil).Execute(ctx, TenantID, grantID)
		cancel()
	}
}
