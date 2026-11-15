package verifier

import "errors"

// Point is a step boundary of the verification at which the crash harness can kill the process (TST-05, rulings A30 and A40; RESIST S9).
type Point int

// PointAfterVerdictStored is "after verify, before the note": the verdict, its RecoveryRequest, its watch and their Record events are committed in
// one transaction, and the caller has not yet been handed the result to write the note or explanation. A crash here leaves exactly the stored
// verdict; a redelivery of the verification finds it (INV-04) and stores nothing new.
const PointAfterVerdictStored Point = iota

// Points lists every boundary.
func Points() []Point { return []Point{PointAfterVerdictStored} }

func (p Point) String() string {
	if p == PointAfterVerdictStored {
		return "AFTER_VERDICT_STORED"
	}
	return "UNKNOWN_POINT"
}

// ErrCrash is returned by a Boundary hook to stand for the process dying at that point: Verify returns at once, without cleanup, and only what
// was committed survives. Any error from the hook is returned as it is.
var ErrCrash = errors.New("verifier: crash injected")

func (v *Verifier) at(p Point) error {
	if v.cfg.Boundary == nil {
		return nil
	}
	return v.cfg.Boundary(p)
}
